package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// AgentEventType identifies the type of agent event.
type AgentEventType string

const (
	EventTypeTextDelta     AgentEventType = "text_delta"
	EventTypeThinkingDelta AgentEventType = "thinking_delta"
	EventTypeToolStart     AgentEventType = "tool_execution_start"
	EventTypeToolEnd       AgentEventType = "tool_execution_end"
	EventTypeAgentStart    AgentEventType = "agent_start"
	EventTypeAgentEnd      AgentEventType = "agent_end"
	EventTypeError         AgentEventType = "error"
)

// AgentEvent is an event emitted by the agent loop.
// These map directly to the SSE events the frontend expects.
type AgentEvent struct {
	Type AgentEventType   `json:"type"`
	Data AgentEventData   `json:"data"`
	Raw  json.RawMessage  `json:"-"` // Optional raw data for agent_end messages
}

// AgentEventData holds the payload of an agent event.
type AgentEventData struct {
	Text     string                 `json:"text,omitempty"`
	ToolName string                 `json:"toolName,omitempty"`
	ToolArgs map[string]interface{} `json:"args,omitempty"`
	ToolID   string                 `json:"toolId,omitempty"`
	Result   interface{}            `json:"result,omitempty"`
	Error    string                 `json:"error,omitempty"`
	Input    float64                `json:"input,omitempty"`
	Output   float64                `json:"output,omitempty"`
	Cache    float64                `json:"cache,omitempty"`
	Model    string                 `json:"model,omitempty"`
}

// ToolExecutor executes a tool and returns its result.
// This interface decouples the agent loop from specific tool implementations.
type ToolExecutor interface {
	Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error)
}

// ComputerUseProvider provides computer use / desktop automation capabilities.
// Implemented by handlers via ComputerUseBridge.
type ComputerUseProvider interface {
	// Browser automation (CDP)
	Enable(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	Screenshot(ctx context.Context, sandboxID string, describe bool) (map[string]interface{}, error)
	Snapshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	Act(ctx context.Context, sandboxID string, action string, args map[string]interface{}) (map[string]interface{}, error)

	// Desktop automation (X11)
	DesktopScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	DesktopClick(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error)
	DesktopType(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error)
	DesktopKey(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error)
}

// SandboxToolExecutor executes tools via the sandbox manager and computer use provider.
type SandboxToolExecutor struct {
	SandboxID string
	Manager   *sandbox.Manager
	CU        ComputerUseProvider
	Logger    *zap.Logger
}

// Execute runs a tool in the sandbox and returns the result.
func (e *SandboxToolExecutor) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	// Normalize common tool name aliases
	switch toolName {
	case "bash_execute", "execute_bash", "run_command", "shell", "execute":
		toolName = "bash"
	case "computer_use_exec", "exec":
		toolName = "bash"
	case "navigate", "browser_navigate", "go_to":
		toolName = "computer_use_act"
		if _, ok := args["action"]; !ok {
			args["action"] = "navigate"
		}
	case "click", "browser_click":
		toolName = "computer_use_act"
		if _, ok := args["action"]; !ok {
			args["action"] = "click"
		}
	case "type", "browser_type", "fill":
		toolName = "computer_use_act"
		if _, ok := args["action"]; !ok {
			args["action"] = "type"
		}
	case "scroll", "browser_scroll":
		toolName = "computer_use_act"
		if _, ok := args["action"]; !ok {
			args["action"] = "scroll"
		}
	case "screenshot", "take_screenshot", "browser_screenshot":
		toolName = "computer_use_screenshot"
	case "snapshot", "get_snapshot", "get_elements", "browser_snapshot":
		toolName = "computer_use_snapshot"
	case "enable", "enable_desktop", "start_browser":
		toolName = "computer_use_enable"
	}

	// Bash: run command inside sandbox container
	if toolName == "bash" {
		cmd, _ := args["command"].(string)
		// If JSON parse failed and we got a raw string, try to extract the command
		if cmd == "" {
			if raw, ok := args["raw"].(string); ok {
				// Try to extract command value from raw string like {command: "ls"}
				cmd = extractJSONStringValue(raw, "command")
			}
		}
		if cmd == "" {
			return nil, fmt.Errorf("missing 'command' argument")
		}
		output, err := e.Manager.ExecInSandbox(ctx, e.SandboxID, []string{"bash", "-c", cmd})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"output": output}, nil
	}

	// Computer use tools — require ComputerUseProvider
	if e.CU == nil {
		return nil, fmt.Errorf("computer use not available: %s", toolName)
	}

	switch toolName {
	case "computer_use_enable":
		return e.CU.Enable(ctx, e.SandboxID)

	case "computer_use_screenshot":
		describe := true
		if d, ok := args["describe"]; ok {
			if b, ok := d.(bool); ok {
				describe = b
			}
		}
		return e.CU.Screenshot(ctx, e.SandboxID, describe)

	case "computer_use_snapshot":
		return e.CU.Snapshot(ctx, e.SandboxID)

	case "computer_use_act":
		action, _ := args["action"].(string)
		if action == "" {
			return nil, fmt.Errorf("missing 'action' argument")
		}
		return e.CU.Act(ctx, e.SandboxID, action, args)

	case "desktop_screenshot":
		return e.CU.DesktopScreenshot(ctx, e.SandboxID)

	case "desktop_click":
		x, _ := args["x"].(float64)
		y, _ := args["y"].(float64)
		button := 1
		if b, ok := args["button"]; ok {
			if f, ok := b.(float64); ok {
				button = int(f)
			}
		}
		return e.CU.DesktopClick(ctx, e.SandboxID, x, y, button)

	case "desktop_type":
		text, _ := args["text"].(string)
		if text == "" {
			return nil, fmt.Errorf("missing 'text' argument")
		}
		return e.CU.DesktopType(ctx, e.SandboxID, text)

	case "desktop_key":
		key, _ := args["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("missing 'key' argument")
		}
		return e.CU.DesktopKey(ctx, e.SandboxID, key)

	default:
		return nil, fmt.Errorf("unsupported tool: %s", toolName)
	}
}

// AgentLoopConfig holds configuration for the agent loop.
type AgentLoopConfig struct {
	SystemPrompt  string
	MaxToolRounds int           // Maximum tool call rounds before forcing end (default: 20)
	MaxTokens     int           // Max tokens per generation (default: 4096)
	ContextSize   int           // KV cache context size (default: 8192)
	Opts          GenerateOptions
}

// DefaultAgentLoopConfig returns sensible defaults.
func DefaultAgentLoopConfig() AgentLoopConfig {
	return AgentLoopConfig{
		SystemPrompt:  "You are a coding assistant with access to tools.",
		MaxToolRounds: 20,
		MaxTokens:     4096,
		ContextSize:   8192,
		Opts:          DefaultGenerateOptions(),
	}
}

// AgentLoop runs the full agent loop: generate → parse tool calls → execute → feed back.
// It emits events to the subscriber channel in the same format as Pi's RPC events,
// so the SSE handler can stream them to the frontend without modification.
type AgentLoop struct {
	engine   *Engine
	session  *Session
	executor ToolExecutor
	config   AgentLoopConfig
	logger   *zap.Logger
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
}

// NewAgentLoop creates a new agent loop bound to an engine.
func NewAgentLoop(engine *Engine, executor ToolExecutor, cfg AgentLoopConfig, logger *zap.Logger) (*AgentLoop, error) {
	if !engine.IsLoaded() {
		return nil, fmt.Errorf("engine model not loaded")
	}

	session, err := engine.NewSession(cfg.ContextSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &AgentLoop{
		engine:   engine,
		session:  session,
		executor: executor,
		config:   cfg,
		logger:   logger,
	}, nil
}

// Run starts the agent loop for a user message and emits events to the subscriber channel.
// Blocks until the loop completes or the context is cancelled.
// The subscriber channel must have buffer capacity (recommended: 256+).
func (loop *AgentLoop) Run(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	loop.mu.Lock()
	if loop.running {
		loop.mu.Unlock()
		return fmt.Errorf("agent loop already running")
	}
	loop.running = true
	ctx, loop.cancel = context.WithCancel(ctx)
	loop.mu.Unlock()

	defer func() {
		loop.mu.Lock()
		loop.running = false
		loop.mu.Unlock()
	}()

	// Emit agent_start
	subscriber <- AgentEvent{Type: EventTypeAgentStart}

	// First generation: system prompt + user message
	opts := loop.config.Opts
	if opts.MaxTokens == 0 {
		opts.MaxTokens = loop.config.MaxTokens
	}

	tokenCh, err := loop.session.ChatWithSystem(loop.config.SystemPrompt, userMsg, opts)
	if err != nil {
		subscriber <- AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}}
		subscriber <- AgentEvent{Type: EventTypeAgentEnd}
		return err
	}

	return loop.runLoop(ctx, tokenCh, subscriber, opts)
}

// Continue sends a follow-up message within an existing session.
// Uses FeedContinue to properly close the previous model turn before appending
// the new user message, so the KV cache persists correctly.
func (loop *AgentLoop) Continue(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	loop.mu.Lock()
	if loop.running {
		loop.mu.Unlock()
		return fmt.Errorf("agent loop already running")
	}
	loop.running = true
	ctx, loop.cancel = context.WithCancel(ctx)
	loop.mu.Unlock()

	defer func() {
		loop.mu.Lock()
		loop.running = false
		loop.mu.Unlock()
	}()

	subscriber <- AgentEvent{Type: EventTypeAgentStart}

	opts := loop.config.Opts
	if opts.MaxTokens == 0 {
		opts.MaxTokens = loop.config.MaxTokens
	}

	// Get the last model response from history to close the turn properly
	var lastModelResponse string
	hist := loop.session.History()
	for i := len(hist) - 1; i >= 0; i-- {
		if hist[i].Role == "model" {
			lastModelResponse = hist[i].Content
			break
		}
	}

	var tokenCh <-chan TokenEvent
	var err error
	if lastModelResponse != "" {
		// AppendUserTurn closes the model turn and appends user message
		// The model response is already in KV cache — only new tokens processed
		tokenCh, err = loop.session.AppendUserTurn(lastModelResponse, userMsg, opts)
	} else {
		// No previous model response — just chat
		tokenCh, err = loop.session.Chat(userMsg, opts)
	}
	if err != nil {
		subscriber <- AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}}
		subscriber <- AgentEvent{Type: EventTypeAgentEnd}
		return err
	}

	return loop.runLoop(ctx, tokenCh, subscriber, opts)
}

// runLoop is the core generation → parse → execute → feed-back cycle.
func (loop *AgentLoop) runLoop(ctx context.Context, tokenCh <-chan TokenEvent, subscriber chan<- AgentEvent, opts GenerateOptions) error {
	round := 0
	var modelResponse strings.Builder

	for {
		modelResponse.Reset()

		// Phase 1: Stream tokens until generation completes
		// Gemma 4 thinking: output starts with <|channel|>thought\n...<|channel|>answer
		// The tokenizer may split this as: ["<|channel>", "thought", "\n", ...]
		// or: ["thought", "\n", "<channel|>", ...]
		// We track state via a simple buffer approach.
		var thinkingActive bool
		var pendingStart bool // true if we saw "<|channel>" but not "thought" yet
		for evt := range tokenCh {
			if ctx.Err() != nil {
				subscriber <- AgentEvent{Type: EventTypeAgentEnd}
				return ctx.Err()
			}
			if evt.Err != nil {
				loop.logger.Error("Generation error", zap.Error(evt.Err))
				subscriber <- AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: evt.Err.Error()}}
				subscriber <- AgentEvent{Type: EventTypeAgentEnd}
				return evt.Err
			}
			if evt.Done {
				break
			}
			// Accumulate raw output for tool call parsing
			modelResponse.WriteString(evt.Token)

			token := evt.Token

			// Detect thinking start patterns
			// Pattern 1: <|channel|>thought (combined in one token)
			if strings.Contains(token, "<|channel") && strings.Contains(token, "thought") {
				thinkingActive = true
				cleaned := strings.ReplaceAll(token, "<|channel|>thought", "")
				cleaned = strings.ReplaceAll(cleaned, "<|channel>thought", "")
				cleaned = strings.TrimSpace(cleaned)
				if cleaned != "" {
					subscriber <- AgentEvent{Type: EventTypeThinkingDelta, Data: AgentEventData{Text: cleaned}}
				}
				continue
			}

			// Pattern 2: <|channel> alone — might be start or end
			if strings.Contains(token, "<|channel") || strings.Contains(token, "<channel") {
				if thinkingActive {
					// End of thinking block
					thinkingActive = false
				} else if !pendingStart {
					// Start of thinking block — next token should be "thought"
					pendingStart = true
				}
				// Strip the marker
				cleaned := strings.ReplaceAll(token, "<|channel|>", "")
				cleaned = strings.ReplaceAll(cleaned, "<|channel>", "")
				cleaned = strings.ReplaceAll(cleaned, "<channel|>", "")
				cleaned = strings.TrimSpace(cleaned)
				if cleaned != "" && !thinkingActive {
					subscriber <- AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: cleaned}}
				}
				continue
			}

			// Pattern 3: "thought" token following <|channel> marker
			if pendingStart && token == "thought" {
				pendingStart = false
				thinkingActive = true
				continue // skip the "thought" marker itself
			}
			pendingStart = false

			if thinkingActive {
				subscriber <- AgentEvent{Type: EventTypeThinkingDelta, Data: AgentEventData{Text: token}}
			} else {
				subscriber <- AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: token}}
			}
		}

		output := modelResponse.String()

		// Phase 2: Parse tool calls from the accumulated output
		toolCalls, _ := ParseToolCalls(output)

		if len(toolCalls) == 0 || round >= loop.config.MaxToolRounds {
			// No tool calls — the model is done (or we hit the round limit)
			// Track the model response in session history for multi-turn
			loop.session.TrackModelResponse(output)

			// Emit agent_end with token counts

			// Emit agent_end with token counts
			inputTokens, outputTokens := loop.session.TokenCounts()
			subscriber <- AgentEvent{
				Type: EventTypeAgentEnd,
				Data: AgentEventData{
					Input:  float64(inputTokens),
					Output: float64(outputTokens),
					Model:  "llama-go/gemma-4-26b",
				},
			}
			return nil
		}

		round++
		loop.logger.Info("Tool calls detected",
			zap.Int("count", len(toolCalls)),
			zap.Int("round", round),
		)

		// Phase 3: Execute each tool call
		var toolResults []string
		for _, tc := range toolCalls {
			// Emit tool_execution_start
			subscriber <- AgentEvent{
				Type: EventTypeToolStart,
				Data: AgentEventData{
					ToolName: tc.Name,
					ToolArgs: tc.Args,
					ToolID:   tc.ID,
				},
			}

			// Execute the tool
			result, err := loop.executor.Execute(ctx, tc.Name, tc.Args)

			var resultStr string
			if err != nil {
				resultStr = fmt.Sprintf("Error: %v", err)
				loop.logger.Error("Tool execution failed",
					zap.String("tool", tc.Name),
					zap.Error(err),
				)
			} else {
				resultBytes, _ := json.Marshal(result)
				resultStr = string(resultBytes)
			}

			// Emit tool_execution_end
			subscriber <- AgentEvent{
				Type: EventTypeToolEnd,
				Data: AgentEventData{
					ToolName: tc.Name,
					ToolID:   tc.ID,
					Result:   result,
					Error:    func() string { if err != nil { return err.Error() }; return "" }(),
				},
			}

			toolResults = append(toolResults, resultStr)
		}

		// Phase 4: Feed tool results back into the session (incremental — KV cache persists!)
		combinedResult := strings.Join(toolResults, "\n")
		nextCh, err := loop.session.FeedResult(output, combinedResult, "", opts)
		if err != nil {
			subscriber <- AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}}
			subscriber <- AgentEvent{Type: EventTypeAgentEnd}
			return err
		}
		tokenCh = nextCh
	}
}

// Abort cancels the running agent loop.
func (loop *AgentLoop) Abort() {
	loop.mu.Lock()
	defer loop.mu.Unlock()
	if loop.cancel != nil {
		loop.cancel()
	}
}

// Close releases the session and frees VRAM.
func (loop *AgentLoop) Close() error {
	if loop.cancel != nil {
		loop.cancel()
	}
	return loop.session.Close()
}

// IsRunning returns whether the loop is currently active.
func (loop *AgentLoop) IsRunning() bool {
	loop.mu.Lock()
	defer loop.mu.Unlock()
	return loop.running
}

// extractJSONStringValue extracts a value from a loosely-formatted JSON string.
// Handles cases like {command: "ls -la"} where keys aren't quoted.
func extractJSONStringValue(raw, key string) string {
	// Try quoted key: "key": "value"
	patterns := []string{
		fmt.Sprintf(`"%s"\s*:\s*"((?:[^"\\]|\\.)*)"`, key),
		fmt.Sprintf(`%s\s*:\s*"((?:[^"\\]|\\.)*)"`, key),
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		m := re.FindStringSubmatch(raw)
		if len(m) >= 2 {
			return strings.ReplaceAll(m[1], `\"`, `"`)
		}
	}
	return ""
}

// Session returns the underlying session for inspection.
func (loop *AgentLoop) Session() *Session {
	return loop.session
}
