package extensions

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
	exts   []*ManagedExtension
	logger *zap.Logger
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
func (m *Manager) StartAll(ctx context.Context, dirs ...string) int {
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
	for _, ext := range seen {
		port, cmd, err := m.startOne(ctx, ext)
		if err != nil {
			m.logger.Warn("extension start failed",
				zap.String("name", ext.Name),
				zap.String("dir", ext.Dir),
				zap.Error(err))
			continue
		}
		m.exts = append(m.exts, &ManagedExtension{
			Extension: *ext,
			cmd:       cmd,
			port:      port,
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
func (m *Manager) StopAll() {
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

// Clients returns MCP clients for all running extensions, keyed by extension name (prefix).
func (m *Manager) Clients() map[string]*mcp.Client {
	clients := make(map[string]*mcp.Client)
	for _, ext := range m.exts {
		endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", ext.port)
		clients[ext.Name] = mcp.NewClient(endpoint, m.logger)
	}
	return clients
}

// Extensions returns info about all managed extensions.
func (m *Manager) Extensions() []Extension {
	result := make([]Extension, len(m.exts))
	for i, ext := range m.exts {
		result[i] = ext.Extension
	}
	return result
}

// startOne starts a single extension subprocess and waits for it to print its port.
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
