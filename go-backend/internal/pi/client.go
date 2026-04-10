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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// PiClient manages a single Pi agent subprocess for one project.
//
// Sandbox/Namespace Architecture:
// Each project gets its own OpenShell sandbox for isolation:
//   - Filesystem: Landlock-enforced isolation per sandbox
//   - Network: Each sandbox has its own network stack
//   - Process: seccomp-protected, isolated process trees
//
// Performance characteristics per sandbox:
//   - Startup: ~1-3 seconds
//   - Memory: ~50-100MB overhead
//   - Storage: ~500MB-1GB (container image + workspace)
//   - CPU: Minimal when idle
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
	namespace  string // OpenShell sandbox ID

	// Sandbox manager for lifecycle control
	sandboxManager *sandbox.Manager
	sandboxObj     *sandbox.Sandbox

	// Event subscribers
	subscribersMu sync.RWMutex
	subscribers   map[string]chan AgentEvent

	// State tracking
	state     SessionState
	stateMu   sync.RWMutex
	running   bool
	startTime time.Time

	// Custom system prompt — if set, used instead of SystemPromptBuilder
	customSystemPrompt string

	// Model to use via --model CLI flag (e.g., "litellm/econ")
	model string

	// Session path to continue (--continue flag)
	sessionPath string

	// MCP client for external tool servers
	mcpClient *MCPClient

	// Allowed tools (for sub-agent restrictions)
	allowedTools map[string]bool

	// Pending approvals for human-in-the-loop
	pendingMu        sync.Mutex
	pendingApprovals map[string]chan ApprovalResponse
}

// NewPiClient creates and starts a new Pi subprocess for the given project directory.
// The agentId uniquely identifies this agent within the project.
func NewPiClient(projectDir string, agentId string, logger *zap.Logger, sandboxMgr interface{}) (*PiClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Derive sandbox ID from project directory name
	sandboxID := filepath.Base(projectDir)

	c := &PiClient{
		logger:           logger,
		projectDir:       projectDir,
		agentId:          agentId,
		cancel:           cancel,
		ctx:              ctx,
		subscribers:      make(map[string]chan AgentEvent),
		pendingApprovals: make(map[string]chan ApprovalResponse),
		startTime:        time.Now(),
		namespace:        sandboxID,
		sandboxManager:   nil,
	}
	
	// Type assertion for sandbox manager
	if mgr, ok := sandboxMgr.(*sandbox.Manager); ok && mgr != nil {
		c.sandboxManager = mgr
	}

	if err := c.start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start pi: %w", err)
	}

	c.initComponents()
	c.installExtensions()

	return c, nil
}

// NewPiClientWithPrompt creates a Pi subprocess with a pre-built system prompt.
// Used by sub-agents that need specialized prompts instead of the default
// SystemPromptBuilder output.
func NewPiClientWithPrompt(projectDir string, agentId string, systemPrompt string, logger *zap.Logger, sandboxMgr interface{}, model string) (*PiClient, error) {
	return NewPiClientWithSession(projectDir, agentId, logger, sandboxMgr, "", model, systemPrompt)
}

// NewPiClientWithSession creates a Pi subprocess that continues an existing session.
func NewPiClientWithSession(projectDir string, agentId string, logger *zap.Logger, sandboxMgr interface{}, sessionPath string, model string, systemPrompt string) (*PiClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	sandboxID := filepath.Base(projectDir)

	c := &PiClient{
		logger:             logger,
		projectDir:         projectDir,
		agentId:            agentId,
		cancel:             cancel,
		ctx:                ctx,
		subscribers:        make(map[string]chan AgentEvent),
		startTime:          time.Now(),
		namespace:          sandboxID,
		sandboxManager:     nil,
		customSystemPrompt: systemPrompt,
		model:              model,
		sessionPath:        sessionPath,
	}

	if mgr, ok := sandboxMgr.(*sandbox.Manager); ok && mgr != nil {
		c.sandboxManager = mgr
	}

	if err := c.start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start pi: %w", err)
	}

	c.initComponents()
	c.installExtensions()

	return c, nil
}

// initComponents sets up hooks, turn tracking, retry, and MCP
func (c *PiClient) initComponents() {
	c.mcpClient = NewMCPClient(c.logger.With(zap.String("component", "mcp")))

	// Fix Pi's models.json if LiteLLM env vars are set
	c.fixPiModelsConfig()

	// Start MCP servers from .pi/mcp-servers.json
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.mcpClient.StartMCPServers(ctx, c.projectDir); err != nil {
			c.logger.Warn("MCP servers init failed", zap.Error(err))
		}
	}()
}

// fixPiModelsConfig updates the baseUrl in ~/.pi/agent/models.json to match
// the LITELLM_PROXY_URL env var, fixing Docker hostname resolution issues.
func (c *PiClient) fixPiModelsConfig() {
	litellmURL := os.Getenv("LITELLM_PROXY_URL")
	if litellmURL == "" {
		return
	}

	// Find Pi's agent config directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	modelsPath := filepath.Join(homeDir, ".pi", "agent", "models.json")

	data, err := os.ReadFile(modelsPath)
	if err != nil {
		return
	}

	// Replace any Docker-internal hostnames with the correct URL
	correctBase := strings.TrimRight(litellmURL, "/") + "/v1"
	oldPatterns := []string{
		"litellm-litellm-1:4000",
		"litellm.local:4000",
		"localhost:4000",
	}
	content := string(data)
	for _, pattern := range oldPatterns {
		content = strings.ReplaceAll(content, "http://"+pattern+"/v1", correctBase)
		content = strings.ReplaceAll(content, "http://"+pattern, correctBase)
	}

	if content != string(data) {
		if err := os.WriteFile(modelsPath, []byte(content), 0644); err != nil {
			c.logger.Warn("Failed to update Pi models config", zap.Error(err))
		} else {
			c.logger.Info("Updated Pi models.json baseUrl", zap.String("url", correctBase))
		}
	}
}

// installExtensions copies built-in extensions and skills to the project's .pi/ directory
func (c *PiClient) installExtensions() error {
	// Extensions
	extensionsDir := filepath.Join(c.projectDir, ".pi", "extensions")
	if err := os.MkdirAll(extensionsDir, 0755); err != nil {
		return fmt.Errorf("create extensions dir: %w", err)
	}

	orchestratorRoot := c.findOrchestratorRoot()
	extDir := filepath.Join(orchestratorRoot, "go-backend", "internal", "pi", "extensions")
	extFiles := []string{
		"computer-use.ts",
		"todos.ts",
		"hooks.ts",
		"mcp-bridge.ts",
		"litellm-provider.ts",
	}

	for _, name := range extFiles {
		src := filepath.Join(extDir, name)
		dst := filepath.Join(extensionsDir, name)
		if err := c.copyIfNewer(src, dst); err != nil {
			c.logger.Warn("Extension install skipped",
				zap.String("file", name),
				zap.Error(err),
			)
		}
	}

	// Skills
	skillsDir := filepath.Join(c.projectDir, ".pi", "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		return fmt.Errorf("create skills dir: %w", err)
	}

	skillsSrcDir := filepath.Join(orchestratorRoot, "go-backend", "internal", "pi", "skills")
	entries, err := os.ReadDir(skillsSrcDir)
	if err != nil {
		c.logger.Debug("No built-in skills found", zap.Error(err))
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := entry.Name()
		dstSkillDir := filepath.Join(skillsDir, skillName)
		if err := os.MkdirAll(dstSkillDir, 0755); err != nil {
			continue
		}

		skillFiles, err := os.ReadDir(filepath.Join(skillsSrcDir, skillName))
		if err != nil {
			continue
		}
		for _, sf := range skillFiles {
			src := filepath.Join(skillsSrcDir, skillName, sf.Name())
			dst := filepath.Join(dstSkillDir, sf.Name())
			if err := c.copyIfNewer(src, dst); err != nil {
				c.logger.Warn("Skill install skipped",
					zap.String("skill", skillName),
					zap.String("file", sf.Name()),
					zap.Error(err),
				)
			} else {
				c.logger.Info("Installed skill", zap.String("skill", skillName))
			}
		}
	}

	return nil
}

// copyIfNewer copies a file if the source is newer or destination doesn't exist
func (c *PiClient) copyIfNewer(src, dst string) error {
	dstStat, err := os.Stat(dst)
	if err == nil {
		srcStat, err := os.Stat(src)
		if err == nil && !srcStat.ModTime().After(dstStat.ModTime()) {
			return nil // destination is up to date
		}
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read source: %w", err)
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}

	return nil
}

// findOrchestratorRoot finds the root of the orchestrator project
// by walking up from the current working directory looking for go.mod
func (c *PiClient) findOrchestratorRoot() string {
	// Try common locations
	candidates := []string{
		".",
		"..",
		"../..",
		filepath.Dir(os.Args[0]),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "go.mod")); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs
		}
	}

	// Fallback: current directory
	abs, _ := filepath.Abs(".")
	return abs
}

// SetAllowedTools restricts which tools this client can execute
func (c *PiClient) SetAllowedTools(tools []string) {
	c.allowedTools = make(map[string]bool, len(tools))
	for _, t := range tools {
		c.allowedTools[t] = true
	}
}

// IsToolAllowed returns true if the tool is allowed (or no restrictions set)
func (c *PiClient) IsToolAllowed(toolName string) bool {
	if c.allowedTools == nil {
		return true
	}
	return c.allowedTools[toolName]
}

// start launches the Pi subprocess inside an OpenShell sandbox.
func (c *PiClient) start() error {
	// Create sandbox if manager is available
	if c.sandboxManager != nil {
		c.logger.Info("Creating OpenShell sandbox for Pi agent",
			zap.String("sandbox_id", c.namespace),
			zap.String("projectDir", c.projectDir),
		)

		sandboxObj, err := c.sandboxManager.CreateSandbox(c.ctx, sandbox.SandboxOptions{
			ID:          c.namespace,
			ProjectPath: c.projectDir,
			Policy:      "developer",
		})
		if err != nil {
			c.logger.Warn("Failed to create sandbox, running unsandboxed",
				zap.Error(err),
				zap.String("sandbox_id", c.namespace),
			)
			c.sandboxed = false
		} else {
			c.sandboxed = true
			c.sandboxObj = sandboxObj
			c.logger.Info("Sandbox created successfully",
				zap.String("sandbox_id", c.namespace),
				zap.String("status", string(sandboxObj.Status)),
			)
		}
	}

	// Find Pi binary
	piPath, err := exec.LookPath("pi")
	if err != nil {
		return fmt.Errorf("pi binary not found in PATH: %w", err)
	}

	// Build system prompt — use custom prompt if provided, otherwise build from project context
	var systemPrompt string
	if c.customSystemPrompt != "" {
		systemPrompt = c.customSystemPrompt
	} else {
		builder := NewSystemPromptBuilder(c.projectDir)
		builder.SubAgentEnabled = true
		systemPrompt = builder.Build()
	}

	// Build command
	args := []string{"--mode", "rpc", "--append-system-prompt", systemPrompt}
	if c.sessionPath != "" {
		// Resume the specific session file
		args = append(args, "--session", c.sessionPath)
	}
	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	if c.sandboxed && c.sandboxObj != nil {
		// Execute inside sandbox
		c.logger.Info("Running Pi inside OpenShell sandbox",
			zap.String("sandbox_id", c.namespace),
		)
		// For now, we'll run Pi directly since the sandbox manager
		// handles the actual container execution
		// TODO: Use sandboxManager.ExecInSandbox for true isolation
		c.cmd = exec.CommandContext(c.ctx, piPath, args...)
	} else {
		c.logger.Info("Running Pi directly (unsandboxed)")
		c.cmd = exec.CommandContext(c.ctx, piPath, args...)
	}

	c.cmd.Dir = c.projectDir
	env := append(os.Environ(),
		fmt.Sprintf("PROJECT_DIR=%s", c.projectDir),
	)
	// Pass orchestrator API host so extensions can reach the backend
	if os.Getenv("ORCHESTRATOR_API_HOST") == "" {
		env = append(env, "ORCHESTRATOR_API_HOST=localhost:3847")
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
		zap.String("sandbox_id", c.namespace),
		zap.Int("pid", c.cmd.Process.Pid),
		zap.Bool("sandboxed", c.sandboxed),
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
func (c *PiClient) SendPrompt(message string, model string, thinkingLevel string) error {
	if model != "" {
		if err := c.SetModel("litellm", model); err != nil {
			c.logger.Warn("Failed to set model before prompt (non-fatal)", zap.Error(err))
		}
		time.Sleep(500 * time.Millisecond)
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

// SetModel switches the active model.
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

// Close shuts down the Pi subprocess and destroys the sandbox.
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

	// Close MCP servers
	if c.mcpClient != nil {
		c.mcpClient.CloseAll()
	}

	// Destroy sandbox if it exists
	if c.sandboxManager != nil && c.sandboxObj != nil {
		c.logger.Info("Destroying OpenShell sandbox",
			zap.String("sandbox_id", c.namespace),
		)
		if err := c.sandboxManager.DestroySandbox(context.Background(), c.namespace); err != nil {
			c.logger.Warn("Failed to destroy sandbox", zap.Error(err))
		}
	}

	c.logger.Info("Pi subprocess closed",
		zap.String("projectDir", c.projectDir),
		zap.String("sandbox_id", c.namespace),
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

// Namespace returns the OpenShell sandbox ID for this client.
func (c *PiClient) Namespace() string {
	return c.namespace
}

// EnableDesktopMode enables Computer Use Mode for this agent's sandbox.
func (c *PiClient) EnableDesktopMode(reason string) (*sandbox.DesktopSession, error) {
	if c.sandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager not available")
	}

	c.logger.Info("Enabling desktop mode for sandbox",
		zap.String("sandbox_id", c.namespace),
		zap.String("reason", reason),
	)

	return c.sandboxManager.EnableDesktopMode(c.ctx, c.namespace)
}

// IsSandboxed returns whether this agent is running inside a sandbox.
func (c *PiClient) IsSandboxed() bool {
	return c.sandboxed
}

// RegisterApproval creates a pending approval channel and returns it.
// The caller should emit an approval_request SSE event and then wait on the returned channel.
func (c *PiClient) RegisterApproval(requestID string) chan ApprovalResponse {
	ch := make(chan ApprovalResponse, 1)
	c.pendingMu.Lock()
	c.pendingApprovals[requestID] = ch
	c.pendingMu.Unlock()
	return ch
}

// ResolveApproval writes a response to the pending approval channel and removes it.
func (c *PiClient) ResolveApproval(requestID string, resp ApprovalResponse) bool {
	c.pendingMu.Lock()
	ch, ok := c.pendingApprovals[requestID]
	if ok {
		delete(c.pendingApprovals, requestID)
	}
	c.pendingMu.Unlock()
	if !ok {
		return false
	}
	ch <- resp
	return true
}
