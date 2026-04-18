package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

	// Change detection: caches last seen element signatures per sandbox
	lastElements map[string]map[string]bool // sandboxID → set of "tag:text" signatures
}

// normalizeElementArg finds the element ID in args using any key that contains
// "element" (element, element_id, target_element_id, etc.) and normalizes it to args["element"].
func normalizeElementArg(args map[string]interface{}) {
	// Explicit aliases first
	for _, k := range []string{"element", "element_id", "id", "ref"} {
		if v, ok := args[k]; ok {
			args["element"] = v
			return
		}
	}
	// Fuzzy: any key containing "element"
	for k, v := range args {
		if strings.Contains(strings.ToLower(k), "element") {
			args["element"] = v
			return
		}
	}
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
		normalizeElementArg(args)
	case "type", "browser_type", "fill", "enter_text",
		"input_text", "keyboard_type", "type_into":
		toolName = "type_text"
		normalizeElementArg(args)
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
		} else if strings.Contains(lower, "search") || strings.Contains(lower, "google") ||
			strings.Contains(lower, "query") || strings.Contains(lower, "look_up") ||
			strings.Contains(lower, "find_info") {
			toolName = "search_web"
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

	case "search_web":
		// Macro: browse to search engine → type query → return results (all-in-one)
		return e.macroSearchWeb(ctx, sandboxID, args)

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
		ContextSize:   32768,
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
	// Track consecutive failures per tool to prevent infinite retry loops
	failCounts := make(map[string]int)
	const maxRetriesPerTool = 3

	for {
		modelResponse.Reset()

		// Phase 1: Stream tokens until generation completes
		var thinkingActive bool
		var pendingStart bool // true if we saw "<|channel>" but not "thought" yet
		// Repetition detection: track recent output to break loops
		var recentOutput string
		const maxRepeatLen = 100 // check last 100 chars for repetition
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

			// Repetition detection: if the last 100 chars repeat 4+ times, force-stop
			recentOutput += evt.Token
			if len(recentOutput) > maxRepeatLen*4 {
				tail := recentOutput[len(recentOutput)-maxRepeatLen*4:]
				chunk := tail[len(tail)-maxRepeatLen:]
				if strings.Count(tail, chunk) >= 4 {
					loop.logger.Warn("Repetition loop detected, forcing stop",
						zap.Int("round", round),
					)
					break
				}
				// Keep buffer bounded
				recentOutput = recentOutput[len(recentOutput)-maxRepeatLen*4:]
			}

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

			// Log the tool call
			argsJSON, _ := json.Marshal(tc.Args)
			loop.logger.Info("AGENT TOOL CALL",
				zap.Int("round", round),
				zap.String("tool", tc.Name),
				zap.String("args", string(argsJSON)),
			)

			// Check retry limit — if this tool has failed 3 times, skip it
			if failCounts[tc.Name] >= maxRetriesPerTool {
				resultStr := fmt.Sprintf("[SYSTEM: Tool '%s' has failed %d times. Do NOT retry it. Use a completely different approach.]", tc.Name, maxRetriesPerTool)
				loop.logger.Warn("Tool retry limit reached, forcing strategy change",
					zap.String("tool", tc.Name),
					zap.Int("failCount", failCounts[tc.Name]),
				)
				toolResults = append(toolResults, resultStr)
				subscriber <- AgentEvent{
					Type: EventTypeToolEnd,
					Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Error: resultStr},
				}
				continue
			}

			// Execute the tool
			startTime := time.Now()
			result, err := loop.executor.Execute(ctx, tc.Name, tc.Args)
			elapsed := time.Since(startTime)

			var resultStr string
			if err != nil {
				failCounts[tc.Name]++
				resultStr = fmt.Sprintf("Error (attempt %d/%d): %v", failCounts[tc.Name], maxRetriesPerTool, err)
				loop.logger.Error("AGENT TOOL ERROR",
					zap.String("tool", tc.Name),
					zap.Duration("elapsed", elapsed),
					zap.Int("failCount", failCounts[tc.Name]),
					zap.Error(err),
				)
			} else {
				// Success — reset fail count for this tool
				delete(failCounts, tc.Name)
				resultBytes, _ := json.Marshal(result)
				resultStr = string(resultBytes)
				if len(resultStr) > 6000 {
					resultStr = resultStr[:6000] + "...[truncated]"
				}
				loop.logger.Info("AGENT TOOL RESULT",
					zap.String("tool", tc.Name),
					zap.Duration("elapsed", elapsed),
					zap.Int("resultLen", len(resultStr)),
				)
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
		goalReminder := "\nIMPORTANT: The user's task is NOT done yet. You MUST call another tool RIGHT NOW. Do NOT explain. Just call the next tool."
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

// macroBrowseTo is a macro tool: ensures browser is running → navigates to URL → returns page info.
// Hides the complexity of enable/navigate from the model.
func (e *SandboxToolExecutor) macroBrowseTo(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("missing 'url' argument for browse_to")
	}

	// Step 1: Ensure browser is enabled
	if err := e.ensureBrowserReady(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	// Step 2: Navigate to the URL — returns PageInfo with URL, Title, Elements
	navArgs := map[string]interface{}{
		"action": "navigate",
		"url":    url,
	}
	navResult, err := e.CU.Act(ctx, sandboxID, "navigate", navArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to navigate to %s: %w", url, err)
	}

	return map[string]interface{}{
		"success":      true,
		"navigated_to": url,
		"page_summary": e.extractPageSummary(navResult),
	}, nil
}

// interactiveTags are tags the model can meaningfully interact with.
var interactiveTags = map[string]bool{
	"a": true, "button": true, "input": true, "textarea": true,
	"select": true, "option": true, "details": true, "summary": true,
}

// spatialZone returns a human-readable position label for an element based on its Y coordinate.
// Divides the page into top/middle/bottom zones.
func spatialZone(y, h int) string {
	if y < 100 {
		return "top"
	}
	if y < 400 {
		return "mid-top"
	}
	if y < 700 {
		return "center"
	}
	return "bottom"
}

// extractPageSummary extracts useful text from a page action result.
// Uses XML tree-style formatting grouped by parent containers (form, nav, etc.)
// for better spatial understanding by the 26B model. Marks NEW elements with [*NEW].
func (e *SandboxToolExecutor) extractPageSummary(result map[string]interface{}) string {
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
	if elements, ok := result["elements"].([]interface{}); ok && len(elements) > 0 {
		// Build current element signatures for change detection
		newSigs := make(map[string]bool)

		// Parse elements into structured data
		type elemInfo struct {
			id    int
			tag   string
			text  string
			zone  string
			isNew bool
		}
		// Group by parent container
		grouped := make(map[string][]elemInfo) // parent → elements
		var ungrouped []elemInfo

		for _, el := range elements {
			m, ok := el.(map[string]interface{})
			if !ok {
				continue
			}
			tag, _ := m["tag"].(string)
			if !interactiveTags[tag] {
				continue
			}
			id, _ := m["id"].(float64)
			text, _ := m["text"].(string)
			text = truncate(text, 50)
			if text == "" {
				continue
			}
			sig := fmt.Sprintf("%s:%s", tag, text)
			newSigs[sig] = true

			isNew := false
			if e.lastElements != nil {
				if prev, ok := e.lastElements[e.SandboxID]; ok {
					if !prev[sig] {
						isNew = true
					}
				}
			}

			y, _ := m["y"].(float64)
			parent, _ := m["parent"].(string)
			ei := elemInfo{id: int(id), tag: tag, text: text, zone: spatialZone(int(y), 0), isNew: isNew}

			if parent != "" {
				grouped[parent] = append(grouped[parent], ei)
			} else {
				ungrouped = append(ungrouped, ei)
			}
		}

		// Cache current signatures
		if e.lastElements == nil {
			e.lastElements = make(map[string]map[string]bool)
		}
		e.lastElements[e.SandboxID] = newSigs

		// Output as XML tree
		totalCount := len(ungrouped)
		for _, elems := range grouped {
			totalCount += len(elems)
		}
		parts = append(parts, fmt.Sprintf("Page elements (%d):", totalCount))

		// Output grouped elements with XML container tags
		for parent, elems := range grouped {
			parts = append(parts, fmt.Sprintf("<%s>", parent))
			for _, ei := range elems {
				newMarker := ""
				if ei.isNew {
					newMarker = " *NEW*"
				}
				parts = append(parts, fmt.Sprintf("  [%d] <%s> %s \"%s\"%s", ei.id, ei.tag, ei.zone, ei.text, newMarker))
			}
			parts = append(parts, fmt.Sprintf("</%s>", parent))
		}
		// Output ungrouped elements
		if len(ungrouped) > 0 {
			parts = append(parts, "<page>")
			for _, ei := range ungrouped {
				newMarker := ""
				if ei.isNew {
					newMarker = " *NEW*"
				}
				parts = append(parts, fmt.Sprintf("  [%d] <%s> %s \"%s\"%s", ei.id, ei.tag, ei.zone, ei.text, newMarker))
			}
			parts = append(parts, "</page>")
		}
	}
	if len(parts) == 0 {
		delete(result, "image")
		delete(result, "screenshot")
		b, _ := json.Marshal(result)
		return string(b)
	}
	return strings.Join(parts, "\n")
}

// truncate shortens a string to maxLen characters.
func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	// Collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ensureBrowserReady ensures the browser CDP client is connected and ready.
// Uses Screenshot (simple CDP command, no page load) for readiness checking.
func (e *SandboxToolExecutor) ensureBrowserReady(ctx context.Context, sandboxID string) error {
	// Fast path: try Screenshot — if it works, CDP is connected
	_, err := e.CU.Screenshot(ctx, sandboxID, false)
	if err == nil {
		return nil // Browser ready
	}

	// Browser not running — enable it (returns immediately, sets up CDP in background)
	_, err = e.CU.Enable(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("enable failed: %w", err)
	}

	// Wait for background setup to complete (sandbox + CDP connect + X11 tools + landing page).
	// Total: ~30-40 seconds for fresh sandbox, ~5s for already-running.
	// Poll with Screenshot every 2 seconds.
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		time.Sleep(2 * time.Second)
		_, err := e.CU.Screenshot(ctx, sandboxID, false)
		if err == nil {
			return nil // CDP connected and responsive
		}
	}

	return fmt.Errorf("browser did not become ready after 60 seconds")
}

// macroReadPage is a macro tool: returns the current page elements and info.
func (e *SandboxToolExecutor) macroReadPage(ctx context.Context, sandboxID string) (interface{}, error) {
	result, err := e.CU.Snapshot(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("failed to read page: %w", err)
	}
	return map[string]interface{}{
		"success":      true,
		"page_summary": e.extractPageSummary(result),
	}, nil
}

// macroClickElement is a macro tool: clicks an element and returns updated page info.
func (e *SandboxToolExecutor) macroClickElement(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	elementID, _ := args["element"].(float64) // JSON numbers are float64
	if elementID == 0 {
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

	return map[string]interface{}{
		"success":         true,
		"clicked_element": int(elementID),
		"page_after_click": e.extractPageSummary(result),
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
	result, err := e.CU.Act(ctx, sandboxID, "type", typeArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to type text: %w", err)
	}

	resp := map[string]interface{}{
		"success":      true,
		"typed_text":   text,
		"into_element": int(elementID),
		"submitted":    submit,
	}
	if submit {
		resp["page_after_submit"] = e.extractPageSummary(result)
	}
	return resp, nil
}

// macroSearchWeb is an all-in-one macro: browse to Google → type query → submit → return results.
// The model only needs ONE tool call to complete a search task.
func (e *SandboxToolExecutor) macroSearchWeb(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	query, _ := args["query"].(string)
	if query == "" {
		query, _ = args["text"].(string)
	}
	if query == "" {
		query, _ = args["q"].(string)
	}
	if query == "" {
		query, _ = args["search"].(string)
	}
	if query == "" {
		return nil, fmt.Errorf("missing 'query' argument for search_web")
	}

	// Step 1: Navigate directly to Google search URL
	searchURL := fmt.Sprintf("https://www.google.com/search?q=%s", url.QueryEscape(query))
	if err := e.ensureBrowserReady(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	navArgs := map[string]interface{}{"action": "navigate", "url": searchURL}
	navResult, err := e.CU.Act(ctx, sandboxID, "navigate", navArgs)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	return map[string]interface{}{
		"success":       true,
		"query":         query,
		"search_url":    searchURL,
		"page_summary":  e.extractPageSummary(navResult),
	}, nil
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
