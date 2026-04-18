package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

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
	// Resolve actual sandbox ID from project name
	sandboxID := e.SandboxID
	if e.Manager != nil {
		if sb := e.Manager.FindSandboxByProject(sandboxID); sb != nil {
			sandboxID = sb.ID
		}
	}

	// Normalize common tool name aliases
	switch toolName {
	case "bash_execute", "execute_bash", "run_command", "shell", "execute",
		"terminal_use", "terminal", "run", "command", "run_bash", "run_shell",
		"execute_command", "execute_command_in_terminal", "terminal_command":
		toolName = "bash"
	case "computer_use_exec", "exec":
		toolName = "bash"
	case "navigate", "browser_navigate", "go_to", "open_url", "goto",
		"open", "browse", "visit", "visit_url", "open_page", "go_to_url",
		"browse_to_and_read", "go_to_page", "load_page":
		toolName = "browse_to"
		// Extract URL from various arg formats
		if _, ok := args["url"]; !ok {
			if u, ok := args["URL"]; ok {
				args["url"] = u
			}
		}
		if _, ok := args["url"]; !ok {
			if raw, ok := args["raw"].(string); ok {
				for _, key := range []string{"url", "URL", "link", "website", "address"} {
					if v := extractJSONStringValue(raw, key); v != "" {
						args["url"] = v
						break
					}
				}
			}
		}
	case "click", "browser_click", "click_button", "click_at", "mouse_click":
		toolName = "click_element"
		// Copy element ID from various arg names
		for _, k := range []string{"element", "element_id", "id", "ref"} {
			if v, ok := args[k]; ok {
				args["element"] = v
				break
			}
		}
	case "type", "browser_type", "fill", "enter_text",
		"input_text", "keyboard_type", "type_into":
		toolName = "type_text"
		// Copy element ID and text from various arg names
		for _, k := range []string{"element", "element_id", "id", "ref"} {
			if v, ok := args[k]; ok {
				args["element"] = v
				break
			}
		}
		for _, k := range []string{"text", "value", "content", "input"} {
			if v, ok := args[k]; ok {
				args["text"] = v
				break
			}
		}
	case "scroll", "browser_scroll", "scroll_page", "scroll_down", "scroll_up":
		toolName = "computer_use_act"
		if _, ok := args["action"]; !ok {
			args["action"] = "scroll"
		}
	case "screenshot", "take_screenshot", "browser_screenshot",
		"capture_screenshot", "screen_capture", "capture_screen":
		toolName = "computer_use_screenshot"
	case "snapshot", "get_snapshot", "get_elements", "browser_snapshot",
		"get_page_elements", "page_snapshot", "inspect_page", "get_page_structure":
		toolName = "computer_use_snapshot"
	case "enable", "enable_desktop", "start_browser", "enable_browser",
		"enable_computer_use", "setup_browser", "init_browser":
		toolName = "computer_use_enable"
	default:
		// Fuzzy matching for common patterns
		lower := strings.ToLower(toolName)
		if strings.Contains(lower, "bash") || strings.Contains(lower, "shell") ||
			strings.Contains(lower, "terminal") || strings.Contains(lower, "command") {
			toolName = "bash"
		} else if strings.Contains(lower, "navigat") || strings.Contains(lower, "go_to") ||
			strings.Contains(lower, "open_url") || strings.Contains(lower, "visit") {
			toolName = "computer_use_act"
			if _, ok := args["action"]; !ok {
				args["action"] = "navigate"
			}
		} else if strings.Contains(lower, "screenshot") || strings.Contains(lower, "capture") {
			toolName = "computer_use_screenshot"
		} else if strings.Contains(lower, "snapshot") || strings.Contains(lower, "element") {
			toolName = "computer_use_snapshot"
		} else if strings.Contains(lower, "click") {
			toolName = "computer_use_act"
			if _, ok := args["action"]; !ok {
				args["action"] = "click"
			}
		} else if strings.Contains(lower, "type") || strings.Contains(lower, "input") ||
			strings.Contains(lower, "fill") || strings.Contains(lower, "enter") {
			toolName = "computer_use_act"
			if _, ok := args["action"]; !ok {
				args["action"] = "type"
			}
		} else if strings.Contains(lower, "enable") || strings.Contains(lower, "start_browser") {
			toolName = "computer_use_enable"
		}
	}

	// Bash: run command inside sandbox container
	if toolName == "bash" {
		cmd, _ := args["command"].(string)
		// Also try common aliases: "code", "cmd", "script"
		if cmd == "" {
			cmd, _ = args["code"].(string)
		}
		if cmd == "" {
			cmd, _ = args["cmd"].(string)
		}
		// If still empty, try extracting from raw malformed JSON
		if cmd == "" {
			if raw, ok := args["raw"].(string); ok {
				for _, key := range []string{"command", "code", "cmd", "script"} {
					cmd = extractJSONStringValue(raw, key)
					if cmd != "" {
						break
					}
				}
			}
		}
		if cmd == "" {
			return nil, fmt.Errorf("missing 'command' argument")
		}
		output, err := e.Manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", cmd})
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
	// ── Macro Tools (high-level, combine multiple steps) ──

	case "browse_to":
		// Macro: ensure browser running → navigate → return page description
		return e.macroBrowseTo(ctx, sandboxID, args)

	case "read_page":
		// Macro: screenshot + describe current page
		return e.macroReadPage(ctx, sandboxID)

	case "click_element":
		// Macro: snapshot → click element by ID
		return e.macroClickElement(ctx, sandboxID, args)

	case "type_text":
		// Macro: type text into element, optionally submit
		return e.macroTypeText(ctx, sandboxID, args)

	// ── Low-level tools (exposed for advanced use) ──

	case "computer_use_enable":
		return e.CU.Enable(ctx, sandboxID)

	case "computer_use_screenshot":
		describe := true
		if d, ok := args["describe"]; ok {
			if b, ok := d.(bool); ok {
				describe = b
			}
		}
		return e.CU.Screenshot(ctx, sandboxID, describe)

	case "computer_use_snapshot":
		return e.CU.Snapshot(ctx, sandboxID)

	case "computer_use_act":
		action, _ := args["action"].(string)
		// Extract action from raw args if JSON parse failed
		if action == "" {
			if raw, ok := args["raw"].(string); ok {
				action = extractJSONStringValue(raw, "action")
			}
		}
		if action == "" {
			return nil, fmt.Errorf("missing 'action' argument")
		}
		// Extract url from raw args if not already set
		if _, ok := args["url"]; !ok {
			if raw, ok := args["raw"].(string); ok {
				url := extractJSONStringValue(raw, "url")
				if url != "" {
					args["url"] = url
				}
			}
		}
		return e.CU.Act(ctx, sandboxID, action, args)

	case "desktop_screenshot":
		return e.CU.DesktopScreenshot(ctx, sandboxID)

	case "desktop_click":
		x, _ := args["x"].(float64)
		y, _ := args["y"].(float64)
		button := 1
		if b, ok := args["button"]; ok {
			if f, ok := b.(float64); ok {
				button = int(f)
			}
		}
		return e.CU.DesktopClick(ctx, sandboxID, x, y, button)

	case "desktop_type":
		text, _ := args["text"].(string)
		if text == "" {
			return nil, fmt.Errorf("missing 'text' argument")
		}
		return e.CU.DesktopType(ctx, sandboxID, text)

	case "desktop_key":
		key, _ := args["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("missing 'key' argument")
		}
		return e.CU.DesktopKey(ctx, sandboxID, key)

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
	opts.MaxTokens = loop.config.MaxTokens // Always use config's value

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
	opts.MaxTokens = loop.config.MaxTokens // Always use config's value

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
			if round >= loop.config.MaxToolRounds {
				loop.logger.Warn("Max tool rounds reached, stopping agent loop",
					zap.Int("round", round),
					zap.Int("maxRounds", loop.config.MaxToolRounds),
				)
			}
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
				// Truncate large results to save context space
				if len(resultStr) > 2000 {
					resultStr = resultStr[:2000] + "...[truncated]"
				}
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
		// Strip any text AFTER tool calls to prevent the model from "remembering"
		// its own post-tool explanation and continuing in explanatory mode.
		cleanOutput := output
		if len(toolCalls) > 0 {
			// Only keep text up to and including the last tool call
			lastEnd := 0
			for _, tc := range toolCalls {
				if tc.End > lastEnd {
					lastEnd = tc.End
				}
			}
			if lastEnd < len(output) {
				cleanOutput = output[:lastEnd]
			}
		}
		combinedResult := strings.Join(toolResults, "\n")
		// Re-inject the user's original goal as a reminder so the model
		// doesn't lose track of what it's doing after seeing tool results.
		goalReminder := "\nReminder: Continue working toward the user's original request. Call the next tool now."
		nextCh, err := loop.session.FeedResult(cleanOutput, combinedResult, goalReminder, opts)
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

// macroBrowseTo is a macro tool: ensures browser is running → navigates to URL → returns page description.
// Hides the complexity of enable/navigate/screenshot from the model.
func (e *SandboxToolExecutor) macroBrowseTo(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("missing 'url' argument for browse_to")
	}

	// Step 1: Ensure browser is enabled. Try a lightweight check first.
	// If it fails, enable and wait for readiness.
	if err := e.ensureBrowserReady(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	// Step 2: Navigate to the URL
	navArgs := map[string]interface{}{
		"action": "navigate",
		"url":    url,
	}
	_, err := e.CU.Act(ctx, sandboxID, "navigate", navArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to navigate to %s: %w", url, err)
	}

	// Step 3: Take a screenshot with description to get page content
	screenshotResult, err := e.CU.Screenshot(ctx, sandboxID, true)
	if err != nil {
		return map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Navigated to %s but failed to get page description: %v", url, err),
		}, nil
	}

	// Strip image data — it's huge base64 and useless to the model
	desc := extractPageSummary(screenshotResult)

	return map[string]interface{}{
		"success":      true,
		"navigated_to": url,
		"page_summary": desc,
	}, nil
}

// extractPageSummary extracts the useful text from a screenshot/action result,
// stripping out massive base64 image data that would overwhelm the model's context.
func extractPageSummary(result map[string]interface{}) string {
	if result == nil {
		return ""
	}
	var parts []string
	if url, ok := result["url"].(string); ok && url != "" {
		parts = append(parts, "URL: "+url)
	}
	if title, ok := result["title"].(string); ok && title != "" {
		parts = append(parts, "Title: "+title)
	}
	if desc, ok := result["description"].(string); ok && desc != "" {
		parts = append(parts, "Description: "+desc)
	}
	if len(parts) == 0 {
		// Fallback: return JSON without the image field
		delete(result, "image")
		b, _ := json.Marshal(result)
		return string(b)
	}
	return strings.Join(parts, "\n")
}

// ensureBrowserReady ensures the browser CDP client is connected and responsive.
// It tries a screenshot first. If that fails, it enables the browser and waits
// for the background CDP setup to complete by polling screenshots.
func (e *SandboxToolExecutor) ensureBrowserReady(ctx context.Context, sandboxID string) error {
	// Fast path: try screenshot — if it works, browser is already running
	_, err := e.CU.Screenshot(ctx, sandboxID, true)
	if err == nil {
		return nil // Browser ready
	}

	// Browser not running — enable it (returns immediately, sets up CDP in background)
	_, err = e.CU.Enable(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("enable failed: %w", err)
	}

	// Poll until the CDP setup completes and screenshot works.
	// The background setup creates the CDP client and connects it.
	// Wait up to 20 seconds.
	for i := 0; i < 40; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		// Small delay to let background setup progress
		time.Sleep(500 * time.Millisecond)
		_, err := e.CU.Screenshot(ctx, sandboxID, true)
		if err == nil {
			return nil // Browser ready
		}
	}

	return fmt.Errorf("browser did not become ready after 20 seconds")
}

// macroReadPage is a macro tool: takes a screenshot and returns the page description.
func (e *SandboxToolExecutor) macroReadPage(ctx context.Context, sandboxID string) (interface{}, error) {
	result, err := e.CU.Screenshot(ctx, sandboxID, true)
	if err != nil {
		return nil, fmt.Errorf("failed to read page: %w", err)
	}
	return map[string]interface{}{
		"success":      true,
		"page_summary": extractPageSummary(result),
	}, nil
}

// macroClickElement is a macro tool: takes a snapshot first, then clicks the element.
func (e *SandboxToolExecutor) macroClickElement(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	elementID, _ := args["element"].(float64) // JSON numbers are float64
	if elementID == 0 {
		// Try int
		if v, ok := args["element"].(int); ok {
			elementID = float64(v)
		}
	}
	if elementID == 0 {
		return nil, fmt.Errorf("missing 'element' argument for click_element")
	}

	clickArgs := map[string]interface{}{
		"action":  "click",
		"element": int(elementID),
	}
	result, err := e.CU.Act(ctx, sandboxID, "click", clickArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to click element %d: %w", int(elementID), err)
	}

	// Take a screenshot after clicking to show the result
	screenshotResult, _ := e.CU.Screenshot(ctx, sandboxID, true)

	return map[string]interface{}{
		"success":         true,
		"clicked_element": int(elementID),
		"action_result":   extractPageSummary(result),
		"page_after_click": extractPageSummary(screenshotResult),
	}, nil
}

// macroTypeText is a macro tool: types text into an element, optionally submitting.
func (e *SandboxToolExecutor) macroTypeText(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	text, _ := args["text"].(string)
	if text == "" {
		return nil, fmt.Errorf("missing 'text' argument for type_text")
	}

	elementID, _ := args["element"].(float64)
	submit := false
	if s, ok := args["submit"].(bool); ok {
		submit = s
	}

	typeArgs := map[string]interface{}{
		"action":  "type",
		"text":    text,
		"element": int(elementID),
		"submit":  submit,
	}
	_, err := e.CU.Act(ctx, sandboxID, "type", typeArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to type text: %w", err)
	}

	// If submit was pressed, screenshot the result
	var pageSummary string
	if submit {
		if ss, _ := e.CU.Screenshot(ctx, sandboxID, true); ss != nil {
			pageSummary = extractPageSummary(ss)
		}
	}

	resp := map[string]interface{}{
		"success":      true,
		"typed_text":   text,
		"into_element": int(elementID),
		"submitted":    submit,
	}
	if pageSummary != "" {
		resp["page_after_submit"] = pageSummary
	}
	return resp, nil
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
