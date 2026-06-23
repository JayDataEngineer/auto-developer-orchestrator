package extensions

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/auto-developer-orchestrator/backend/internal/mcp"
)

// ManagedExtension tracks a running extension subprocess.
type ManagedExtension struct {
	Extension
	cmd  *exec.Cmd
	port int
}

// Manager starts and stops extension subprocesses, capturing their MCP server ports.
type Manager struct {
	mu              sync.Mutex
	exts            []*ManagedExtension
	startupResults  []StartupResult
	logger          *zap.Logger
}

// NewManager creates a new extension process manager.
func NewManager(logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{logger: logger}
}

// StartAll discovers extensions in the given directories and starts their MCP servers.
// Each directory is scanned; extensions from later directories override earlier ones by name.
// Returns the number of extensions successfully started.
//
// Boot-time only. Holds the write lock for the whole spawn sequence — pre-warm
// and health-monitor goroutines block until boot finishes. This is correct:
// those goroutines assume the boot-time set is in place.
func (m *Manager) StartAll(ctx context.Context, dirs ...string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Collect extensions from all directories (last wins for same name)
	seen := make(map[string]*Extension)
	for _, dir := range dirs {
		exts, err := Discover(dir)
		if err != nil {
			m.logger.Warn("extension discovery failed", zap.String("dir", dir), zap.Error(err))
			continue
		}
		for i := range exts {
			seen[exts[i].Name] = &exts[i]
		}
	}

	started := 0
	m.startupResults = nil
	for _, ext := range seen {
		port, cmd, err := m.startOne(ctx, ext)
		if err != nil {
			m.logger.Warn("extension start failed",
				zap.String("name", ext.Name),
				zap.String("dir", ext.Dir),
				zap.Error(err))
			m.startupResults = append(m.startupResults, StartupResult{
				Name:    ext.Name,
				Version: ext.Version,
				Success: false,
				Error:   err.Error(),
			})
			continue
		}
		m.exts = append(m.exts, &ManagedExtension{
			Extension: *ext,
			cmd:       cmd,
			port:      port,
		})
		m.startupResults = append(m.startupResults, StartupResult{
			Name:    ext.Name,
			Version: ext.Version,
			Success: true,
		})
		m.logger.Info("extension started",
			zap.String("name", ext.Name),
			zap.Int("port", port),
			zap.String("dir", ext.Dir))
		started++
	}

	return started
}

// StopAll stops all running extension subprocesses.
//
// Holds the write lock for the whole stop sequence so pre-warm / restart
// goroutines can't append to m.exts while we're tearing down.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ext := range m.exts {
		if ext.cmd != nil && ext.cmd.Process != nil {
			m.logger.Info("stopping extension", zap.String("name", ext.Name))
			ext.cmd.Process.Signal(os.Interrupt)
			// Give it a moment, then kill
			done := make(chan error, 1)
			go func() {
				done <- ext.cmd.Wait()
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				ext.cmd.Process.Kill()
			}
		}
	}
	m.exts = nil
}

// Restart stops and re-starts the extension with the given prefix (extension Name).
// Returns an error if no extension matches. Used by the MCP HealthMonitor when a
// subprocess-backed MCP server fails N consecutive health probes. The new port is
// returned so callers (HealthMonitor) can re-register the client with the new URL.
// Restarts obey the extension's Restart policy: "no" → ErrRestartDisabled.
//
// Holds the write lock for the duration. Other extensions' restarts serialize,
// which is acceptable — N-strike events are rare (one per ~10min when something
// is actually wrong), not a hot path.
func (m *Manager) Restart(ctx context.Context, prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, ext := range m.exts {
		if ext.Name != prefix {
			continue
		}
		if ext.Server.Restart == "no" {
			return 0, ErrRestartDisabled
		}
		// Stop the existing subprocess.
		if ext.cmd != nil && ext.cmd.Process != nil {
			m.logger.Info("restarting extension", zap.String("name", ext.Name))
			ext.cmd.Process.Signal(os.Interrupt)
			done := make(chan error, 1)
			go func() {
				done <- ext.cmd.Wait()
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				ext.cmd.Process.Kill()
			}
		}
		// Start a fresh subprocess on the same extension config.
		port, cmd, err := m.startOne(ctx, &ext.Extension)
		if err != nil {
			m.logger.Error("extension restart failed",
				zap.String("name", ext.Name), zap.Error(err))
			return 0, fmt.Errorf("restart %s: %w", ext.Name, err)
		}
		m.exts[i].cmd = cmd
		m.exts[i].port = port
		m.logger.Info("extension restarted",
			zap.String("name", ext.Name), zap.Int("port", port))
		return port, nil
	}
	return 0, ErrUnknownExtension
}

// PortFor returns the current MCP port for an extension by prefix, or 0 if unknown.
// Callers build the URL themselves (http://127.0.0.1:<port>/mcp). Used by
// HealthMonitor after Restart so it can re-register the client with MultiClient.
func (m *Manager) PortFor(prefix string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ext := range m.exts {
		if ext.Name == prefix {
			return ext.port
		}
	}
	return 0
}

// ErrRestartDisabled signals the extension declared `restart: "no"` in its
// extension.yaml and must not be auto-restarted by the HealthMonitor.
var ErrRestartDisabled = fmt.Errorf("extension restart disabled by policy")

// ErrUnknownExtension signals no extension is registered under the requested prefix.
var ErrUnknownExtension = fmt.Errorf("unknown extension")

// Clients returns MCP clients for all running extensions, keyed by extension name (prefix).
func (m *Manager) Clients() map[string]*mcp.Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	clients := make(map[string]*mcp.Client)
	for _, ext := range m.exts {
		endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", ext.port)
		clients[ext.Name] = mcp.NewClient(ext.Name, endpoint, m.logger)
	}
	return clients
}

// StartupResults returns results from the most recent StartAll call.
func (m *Manager) StartupResults() []StartupResult {
	return m.startupResults
}

// Extensions returns info about all managed extensions.
func (m *Manager) Extensions() []Extension {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Extension, len(m.exts))
	for i, ext := range m.exts {
		result[i] = ext.Extension
	}
	return result
}

// startOne spawns a single extension subprocess and waits for it to print its port.
// Lock-free: does not touch m.exts. Callers coordinate locking around mutations
// to m.exts. StartAll and Restart hold m.mu for their whole sequence so they can
// call this with consistent state; CloneAndStart spawns without the lock (spawn
// can take 30s+ for slow MCP servers) and only takes m.mu around the append.
func (m *Manager) startOne(ctx context.Context, ext *Extension) (int, *exec.Cmd, error) {
	timeout := time.Duration(ext.Server.Timeout) * time.Second
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	cmd := exec.CommandContext(ctx, ext.Server.Command, ext.Server.Args...)
	cmd.Dir = ext.Dir
	cmd.Stderr = os.Stderr // extension logs go to our stderr

	// Capture stdout to find the port line
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, fmt.Errorf("pipe stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("start process: %w", err)
	}

	// Scan stdout for PUX_EXT_PORT:<port>
	portCh := make(chan int, 1)
	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if after, ok := strings.CutPrefix(line, "PUX_EXT_PORT:"); ok {
				port, err := strconv.Atoi(strings.TrimSpace(after))
				if err != nil {
					errCh <- fmt.Errorf("invalid port in %q: %w", line, err)
					return
				}
				portCh <- port
				return
			}
		}
		// If scanner stops without finding port, check if process exited
		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("reading stdout: %w", err)
		} else {
			errCh <- fmt.Errorf("extension exited without printing PUX_EXT_PORT")
		}
	}()

	// Wait for port or timeout
	select {
	case port := <-portCh:
		return port, cmd, nil
	case err := <-errCh:
		cmd.Process.Kill()
		return 0, nil, err
	case <-time.After(timeout):
		cmd.Process.Kill()
		return 0, nil, fmt.Errorf("startup timeout (%v)", timeout)
	}
}
