package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// PiClient manages a single Pi agent subprocess for one project.
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
func NewPiClient(projectDir string, agentId string, logger *zap.Logger) (*PiClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	c := &PiClient{
		logger:      logger,
		projectDir:  projectDir,
		agentId:     agentId,
		cancel:      cancel,
		ctx:         ctx,
		subscribers: make(map[string]chan AgentEvent),
		startTime:   time.Now(),
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
	// Fast path: nothing to rewrite
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

// start launches the Pi subprocess, optionally inside an OpenShell sandbox.
//
// Sandbox architecture:
//
//	Pi (inside openshell sandbox) → OpenShell (stdout) → Go Backend → React Frontend (SSE)
//
// If openshell is available, Pi runs in a filesystem-jailed sandbox where:
//   - It cannot see the host filesystem (only the project directory)
//   - Network access is governed by OpenShell policies
//   - A hallucinated `rm -rf /` only destroys the sandbox, not the host
//
// If openshell is not available, Pi runs directly (for dev/local use).
func (c *PiClient) start() error {
	// Build the command — sandboxed if openshell is available
	openshellPath, errSandbox := exec.LookPath("openshell")
	piPath, err := exec.LookPath("pi")
	if err != nil {
		return fmt.Errorf("pi binary not found in PATH: %w", err)
	}

	if errSandbox == nil && openshellPath != "" {
		c.logger.Info("OpenShell detected — running Pi inside sandbox")
		c.sandboxed = true
		c.cmd = exec.CommandContext(
			c.ctx, openshellPath,
			"exec", "claw", "--",
			piPath, "--mode", "rpc", "--no-session",
		)
	} else {
		c.logger.Info("OpenShell not found — running Pi directly (unsandboxed)")
		c.sandboxed = false
		c.cmd = exec.CommandContext(c.ctx, piPath, "--mode", "rpc", "--no-session")
	}

	c.cmd.Dir = c.projectDir

	// Build environment: pass through API keys + sandbox networking fixes
	env := os.Environ()

	// Inside an OpenShell sandbox, localhost refers to the sandbox itself.
	// To reach services on the host (e.g. LiteLLM), rewrite localhost URLs
	// to use the host bridge address.
	if c.sandboxed {
		for i, e := range env {
			// Rewrite LITELLM_PROXY_URL if it points to localhost
			if len(e) > len("LITELLM_PROXY_URL=") && e[:len("LITELLM_PROXY_URL=")] == "LITELLM_PROXY_URL=" {
				val := e[len("LITELLM_PROXY_URL="):]
				env[i] = "LITELLM_PROXY_URL=" + rewriteLocalhost(val)
			}
		}
	}

	c.cmd.Env = env

	// CRITICAL: Kill Pi if the Go parent process dies unexpectedly.
	// Prevents zombie Node.js processes from consuming resources.
	c.cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
		Setpgid:   true, // New process group so we can kill Pi + its children
	}

	// Create stdin/stdout pipes
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

	// Read stderr for logging
	go c.readStderr()

	// Read stdout events in background
	go c.readEvents()

	c.logger.Info("Pi subprocess started",
		zap.String("projectDir", c.projectDir),
		zap.Int("pid", c.cmd.Process.Pid),
	)

	return nil
}

// readStderr logs Pi's stderr output.
func (c *PiClient) readStderr() {
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		c.logger.Debug("Pi stderr", zap.String("line", scanner.Text()))
	}
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

		var event AgentEvent
		if err := json.Unmarshal(line, &event); err != nil {
			c.logger.Debug("Failed to parse Pi event", zap.String("line", string(line)), zap.Error(err))
			continue
		}

		// Update internal state based on event
		c.updateState(event)

		// Dispatch to all subscribers
		c.subscribersMu.RLock()
		for _, ch := range c.subscribers {
			select {
			case ch <- event:
			default:
				// Drop event if subscriber is slow
				c.logger.Warn("Dropping event for slow subscriber", zap.String("type", event.Type))
			}
		}
		c.subscribersMu.RUnlock()
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

	_, err = c.stdin.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to pi stdin: %w", err)
	}

	return nil
}

// SendPrompt sends a coding prompt to Pi.
func (c *PiClient) SendPrompt(message string, model string, thinkingLevel string) error {
	cmd := RpcCommand{
		Type:          CmdPrompt,
		Message:       message,
		Model:         model,
		ThinkingLevel: thinkingLevel,
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

// SetModel switches the active model.
func (c *PiClient) SetModel(provider string, modelId string) error {
	return c.SendCommand(RpcCommand{
		Type:     CmdSetModel,
		Provider: provider,
		Model:    modelId,
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
func (c *PiClient) Close() error {
	c.cancel()

	// Close stdin to signal Pi to exit gracefully
	if c.stdin != nil {
		c.stdin.Close()
	}

	// Close all subscriber channels
	c.subscribersMu.Lock()
	for id, ch := range c.subscribers {
		delete(c.subscribers, id)
		close(ch)
	}
	c.subscribersMu.Unlock()

	// Kill the entire process group (Pi + any child processes it spawned)
	if c.cmd != nil && c.cmd.Process != nil {
		// Send SIGKILL to the process group (negative PID)
		syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)

		done := make(chan error, 1)
		go func() {
			done <- c.cmd.Wait()
		}()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			// Fallback: direct kill if process group kill didn't work
			c.cmd.Process.Kill()
			<-done
		}
	}

	// Close pipes
	if c.stdout != nil {
		c.stdout.Close()
	}
	if c.stderr != nil {
		c.stderr.Close()
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	c.logger.Info("Pi subprocess closed", zap.String("projectDir", c.projectDir))
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
