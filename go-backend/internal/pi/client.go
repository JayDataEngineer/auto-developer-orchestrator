package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// PiClient manages a single Pi agent subprocess for one project.
//
// Sandbox/Namespace Architecture:
// Each project gets its own OpenShell namespace (sandbox) for isolation:
//   - Filesystem: Landlock-enforced isolation per namespace
//   - Network: Each namespace has its own network stack (can bind same ports)
//   - Process: seccomp-protected, isolated process trees
//
// Performance characteristics per namespace:
//   - Startup: ~100ms
//   - Memory: ~50-100MB overhead (K3s pod)
//   - Storage: ~500MB-1GB (container image + workspace)
//   - CPU: Minimal when idle
//
// Multiple agents within the same project share the same namespace,
// but different projects have complete kernel-level isolation.
type PiClient struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	logger     *zap.Logger
	projectDir string
	agentId    string
	cancel     context.CancelFunc
	ctx        context.Context
	sandboxed  bool
	namespace  string // OpenShell namespace name (per-project for isolation)

	// Event subscribers
	subscribersMu sync.RWMutex
	subscribers   map[string]chan AgentEvent

	// State tracking
	state     SessionState
	stateMu   sync.RWMutex
	running   bool
	startTime time.Time
}

// NewPiClient creates and starts a new Pi subprocess for the given project directory.
// The agentId uniquely identifies this agent within the project.
// The namespace is derived from the project name for per-project isolation.
func NewPiClient(projectDir string, agentId string, logger *zap.Logger) (*PiClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Derive namespace from project directory name for per-project isolation
	// This ensures each project runs in its own OpenShell sandbox namespace
	namespace := filepath.Base(projectDir)

	c := &PiClient{
		logger:      logger,
		projectDir:  projectDir,
		agentId:     agentId,
		cancel:      cancel,
		ctx:         ctx,
		subscribers: make(map[string]chan AgentEvent),
		startTime:   time.Now(),
		namespace:   namespace,
	}

	if err := c.start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start pi: %w", err)
	}

	return c, nil
}

// rewriteLocalhost replaces localhost/127.0.0.1 URLs with host.docker.internal
// so that processes inside a sandbox can reach host services.
func rewriteLocalhost(url string) string {
	if url == "" {
		return url
	}
	imports := []string{"http://localhost:", "http://127.0.0.1:"}
	for _, prefix := range imports {
		if len(url) > len(prefix) && url[:len(prefix)] == prefix {
			return "http://host.docker.internal:" + url[len(prefix):]
		}
	}
	return url
}

// start launches the Pi subprocess, optionally inside an OpenShell sandbox namespace.
//
// OpenShell Namespace Isolation:
// Each project runs in its own namespace (e.g., "my-project", "sandbox").
// This provides:
//   - Isolated filesystem (Landlock)
//   - Isolated network stack (can bind localhost:8080 in multiple projects)
//   - Isolated process tree (seccomp)
//
// The namespace name is derived from the project directory basename.
// Multiple agents in the same project share the namespace (same isolation boundary).
func (c *PiClient) start() error {
	openshellPath, errSandbox := exec.LookPath("openshell")
	piPath, err := exec.LookPath("pi")
	if err != nil {
		return fmt.Errorf("pi binary not found in PATH: %w", err)
	}

	if errSandbox == nil && openshellPath != "" {
		c.logger.Info("OpenShell detected — running Pi inside per-project namespace",
			zap.String("namespace", c.namespace),
			zap.String("projectDir", c.projectDir),
		)
		c.sandboxed = true

		// Use per-project namespace for isolation
		// Command: openshell sandbox exec <namespace> -- pi --mode rpc
		// This creates/reuses a sandbox named after the project
		c.cmd = exec.CommandContext(
			c.ctx, openshellPath,
			"sandbox", "exec", c.namespace, "--",
			piPath, "--mode", "rpc",
		)
	} else {
		c.logger.Info("OpenShell not found — running Pi directly (unsandboxed)",
			zap.String("namespace", c.namespace),
		)
		c.sandboxed = false
		c.cmd = exec.CommandContext(c.ctx, piPath,
			"--mode", "rpc",
		)
	}

	c.cmd.Dir = c.projectDir

	env := os.Environ()

	if c.sandboxed {
		// Rewrite localhost URLs to reach host services from inside the sandbox
		for i, e := range env {
			if len(e) > len("LITELLM_PROXY_URL=") && e[:len("LITELLM_PROXY_URL=")] == "LITELLM_PROXY_URL=" {
				val := e[len("LITELLM_PROXY_URL="):]
				env[i] = "LITELLM_PROXY_URL=" + rewriteLocalhost(val)
			}
		}
	}

	c.cmd.Env = env

	c.cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
		Setpgid:   true,
	}

	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	c.stdin = stdin

	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	c.stdout = stdout

	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	c.stderr = stderr

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start pi process: %w", err)
	}

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	go c.readStderr()
	go c.readEvents()

	c.logger.Info("Pi subprocess started",
		zap.String("projectDir", c.projectDir),
		zap.String("namespace", c.namespace),
		zap.Int("pid", c.cmd.Process.Pid),
	)

	return nil
}

// readStderr logs Pi's stderr output.
func (c *PiClient) readStderr() {
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		c.logger.Info("Pi stderr", zap.String("line", scanner.Text()))
	}
}

// truncStr truncates a string to maxLen runes.
func truncStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// readEvents reads JSONL events from Pi's stdout and dispatches to subscribers.
func (c *PiClient) readEvents() {
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		lineStr := truncStr(string(line), 300)
		c.logger.Info("Pi stdout line", zap.String("line", lineStr))

		var event AgentEvent
		if err := json.Unmarshal(line, &event); err != nil {
			c.logger.Warn("Failed to parse Pi event", zap.String("line", lineStr), zap.Error(err))
			continue
		}

		c.logger.Info("Pi parsed event", zap.String("type", event.Type))

		// Update internal state based on event
		c.updateState(event)

		// Dispatch to all subscribers
		c.subscribersMu.RLock()
		subCount := len(c.subscribers)
		for id, ch := range c.subscribers {
			select {
			case ch <- event:
				c.logger.Info("Event dispatched", zap.String("subscriber", id), zap.String("eventType", event.Type))
			default:
				c.logger.Warn("Dropping event for slow subscriber", zap.String("subscriber", id), zap.String("type", event.Type))
			}
		}
		c.subscribersMu.RUnlock()

		c.logger.Info("Dispatch complete", zap.String("eventType", event.Type), zap.Int("subscriberCount", subCount))
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	c.logger.Info("Pi event stream ended", zap.String("projectDir", c.projectDir))
}

// updateState tracks session state from streaming events.
func (c *PiClient) updateState(event AgentEvent) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	switch event.Type {
	case RpcEventAgentStart:
		c.state.Streaming = true
	case RpcEventAgentEnd:
		c.state.Streaming = false
		if event.Data.Model != "" {
			c.state.Model = event.Data.Model
		}
	case RpcEventStateUpdate, RpcEventResponse:
		if event.Data.Model != "" {
			c.state.Model = event.Data.Model
		}
		if event.Data.Input > 0 {
			c.state.Input = event.Data.Input
		}
		if event.Data.Output > 0 {
			c.state.Output = event.Data.Output
		}
		if event.Data.Cache > 0 {
			c.state.Cache = event.Data.Cache
		}
	}
}

// Subscribe registers a channel to receive agent events.
func (c *PiClient) Subscribe(id string) chan AgentEvent {
	ch := make(chan AgentEvent, 256)
	c.subscribersMu.Lock()
	c.subscribers[id] = ch
	c.subscribersMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (c *PiClient) Unsubscribe(id string) {
	c.subscribersMu.Lock()
	if ch, ok := c.subscribers[id]; ok {
		delete(c.subscribers, id)
		close(ch)
	}
	c.subscribersMu.Unlock()
}

// SendCommand sends an RPC command to the Pi subprocess.
func (c *PiClient) SendCommand(cmd RpcCommand) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return fmt.Errorf("pi subprocess is not running")
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	data = append(data, '\n')

	c.logger.Info("Sending command to Pi stdin", zap.String("command", truncStr(string(data), 200)))

	_, err = c.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to pi stdin: %w", err)
	}

	return nil
}

// SendPrompt sends a coding prompt to Pi.
// Per pi RPC protocol: prompt only accepts message/images/streamingBehavior.
// If model is specified, sends set_model command first (fire-and-forget since
// pi processes commands in order on the same stdin pipe).
func (c *PiClient) SendPrompt(message string, model string, thinkingLevel string) error {
	// Set model first if specified (pi processes stdin commands sequentially)
	if model != "" {
		if err := c.SetModel("litellm", model); err != nil {
			c.logger.Warn("Failed to set model before prompt (non-fatal)", zap.Error(err))
		}
		time.Sleep(500 * time.Millisecond) // Let pi process set_model before prompt
	}

	cmd := RpcCommand{
		Type:    CmdPrompt,
		Message: message,
	}
	return c.SendCommand(cmd)
}

// Abort cancels the current Pi operation.
func (c *PiClient) Abort() error {
	return c.SendCommand(RpcCommand{Type: CmdAbort})
}

// GetState returns the current session state.
func (c *PiClient) GetState() SessionState {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

// Compact triggers context compaction.
func (c *PiClient) Compact() error {
	return c.SendCommand(RpcCommand{Type: CmdCompact})
}

// SetModel switches the active model using pi's set_model RPC command.
// Per pi RPC protocol: { type: "set_model", provider: "litellm", modelId: "fast" }
func (c *PiClient) SetModel(provider string, modelId string) error {
	return c.SendCommand(RpcCommand{
		Type:     CmdSetModel,
		Provider: provider,
		ModelId:  modelId,
	})
}

// GetAvailableModels requests model list from Pi.
func (c *PiClient) GetAvailableModels() error {
	return c.SendCommand(RpcCommand{Type: CmdGetModels})
}

// GetMessages requests full conversation history.
func (c *PiClient) GetMessages() error {
	return c.SendCommand(RpcCommand{Type: CmdGetMessages})
}

// Steer sends a follow-up message while Pi is working.
func (c *PiClient) Steer(message string) error {
	return c.SendCommand(RpcCommand{Type: CmdSteer, Message: message})
}

// ListSessions requests saved sessions list.
func (c *PiClient) ListSessions() error {
	return c.SendCommand(RpcCommand{Type: CmdListSessions})
}

// SwitchSession switches to a different session.
func (c *PiClient) SwitchSession(sessionId string) error {
	return c.SendCommand(RpcCommand{Type: CmdSwitchSession, SessionId: sessionId})
}

// IsRunning returns whether the Pi subprocess is alive.
func (c *PiClient) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// Close shuts down the Pi subprocess cleanly.
// When running in a sandbox, the namespace persists after the agent exits
// (OpenShell manages namespace lifecycle separately).
func (c *PiClient) Close() error {
	c.cancel()

	if c.stdin != nil {
		c.stdin.Close()
	}

	c.subscribersMu.Lock()
	for id, ch := range c.subscribers {
		delete(c.subscribers, id)
		close(ch)
	}
	c.subscribersMu.Unlock()

	if c.cmd != nil && c.cmd.Process != nil {
		syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)

		done := make(chan error, 1)
		go func() {
			done <- c.cmd.Wait()
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			c.cmd.Process.Kill()
			<-done
		}
	}

	if c.stdout != nil {
		c.stdout.Close()
	}
	if c.stderr != nil {
		c.stderr.Close()
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	c.logger.Info("Pi subprocess closed",
		zap.String("projectDir", c.projectDir),
		zap.String("namespace", c.namespace),
	)
	return nil
}

// ProjectDir returns the project directory for this client.
func (c *PiClient) ProjectDir() string {
	return c.projectDir
}

// AgentId returns the agent ID for this client.
func (c *PiClient) AgentId() string {
	return c.agentId
}

// Namespace returns the OpenShell namespace for this client.
// This is used for per-project isolation.
func (c *PiClient) Namespace() string {
	return c.namespace
}
