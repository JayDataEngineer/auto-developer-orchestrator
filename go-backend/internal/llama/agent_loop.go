package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// AgentEventType identifies the type of agent event.
type AgentEventType string

const (
	EventTypeTextDelta       AgentEventType = "text_delta"
	EventTypeThinkingDelta   AgentEventType = "thinking_delta"
	EventTypeToolStart       AgentEventType = "tool_execution_start"
	EventTypeToolEnd         AgentEventType = "tool_execution_end"
	EventTypeAgentStart      AgentEventType = "agent_start"
	EventTypeAgentEnd        AgentEventType = "agent_end"
	EventTypeError           AgentEventType = "error"
	EventTypeArtifactCreated AgentEventType = "artifact_created"
	EventTypeArtifactUpdated AgentEventType = "artifact_updated"
	EventTypePlanCreated     AgentEventType = "plan_created"
	EventTypePlanUpdated     AgentEventType = "plan_updated"
	EventTypeSubAgentStart   AgentEventType = "subagent_start"
	EventTypeSubAgentEnd     AgentEventType = "subagent_end"
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

// sendEvent sends an event to the subscriber channel without blocking.
// If the channel is full (subscriber not reading fast enough), the event is dropped.
// This prevents the agent loop from hanging if the SSE handler disconnects.
func sendEvent(ch chan<- AgentEvent, evt AgentEvent) {
	select {
	case ch <- evt:
	default:
		// Channel full — drop event to prevent deadlock
	}
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
	Creds     *CredentialStore // optional: resolve/redact sensitive data

	// Change detection: caches last seen element signatures per sandbox
	lastElements map[string]map[string]bool       // sandboxID → set of "tag:text" signatures (for change detection)
	elemIndex    map[string][]indexedElement       // sandboxID → ordered list of elements (for description→ID lookup)
	credsLoaded  bool                             // true after first attempt to load credentials
}

// normalizeToolName normalizes common tool name aliases to canonical names.
// Shared by SandboxToolExecutor and PersonaAwareExecutor.
func normalizeToolName(name string, args map[string]interface{}) string {
	switch name {
	case "bash_execute", "execute_bash", "run_command", "shell", "execute",
		"terminal_use", "terminal", "run", "command", "run_bash", "run_shell",
		"execute_command", "execute_command_in_terminal", "terminal_command",
		"computer_use_exec", "exec":
		return "bash"
	case "navigate", "browser_navigate", "go_to", "open_url", "goto",
		"open", "browse", "visit", "visit_url", "open_page", "go_to_url",
		"browse_to_and_read", "go_to_page", "load_page":
		if _, ok := args["url"]; !ok {
			if u, ok := args["URL"]; ok {
				args["url"] = u
			}
		}
		return "browse_to"
	case "click", "browser_click", "click_button", "click_at", "mouse_click":
		normalizeElementArg(args)
		return "click_element"
	case "type", "browser_type", "fill", "enter_text",
		"input_text", "keyboard_type", "type_into":
		normalizeElementArg(args)
		for _, k := range []string{"text", "value", "content", "input"} {
			if v, ok := args[k]; ok {
				args["text"] = v
				break
			}
		}
		return "type_text"
	case "scroll", "browser_scroll", "scroll_page", "scroll_down", "scroll_up":
		if _, ok := args["action"]; !ok {
			args["action"] = "scroll"
		}
		return "computer_use_act"
	case "screenshot", "take_screenshot", "browser_screenshot",
		"capture_screenshot", "screen_capture", "capture_screen":
		return "computer_use_screenshot"
	case "snapshot", "get_snapshot", "get_elements", "browser_snapshot",
		"get_page_elements", "page_snapshot", "inspect_page", "get_page_structure":
		return "computer_use_snapshot"
	case "enable", "enable_desktop", "start_browser", "enable_browser",
		"enable_computer_use", "setup_browser", "init_browser":
		return "computer_use_enable"
	}

	// Fuzzy matching for common patterns
	lower := strings.ToLower(name)
	if strings.Contains(lower, "bash") || strings.Contains(lower, "shell") ||
		strings.Contains(lower, "terminal") || strings.Contains(lower, "command") {
		return "bash"
	}
	if strings.Contains(lower, "navigat") || strings.Contains(lower, "go_to") ||
		strings.Contains(lower, "open_url") || strings.Contains(lower, "visit") {
		if _, ok := args["action"]; !ok {
			args["action"] = "navigate"
		}
		return "browse_to"
	}
	if strings.Contains(lower, "search") || strings.Contains(lower, "google") ||
		strings.Contains(lower, "query") || strings.Contains(lower, "look_up") {
		return "search_web"
	}
	if strings.Contains(lower, "screenshot") || strings.Contains(lower, "capture") {
		return "computer_use_screenshot"
	}
	if strings.Contains(lower, "snapshot") || strings.Contains(lower, "element") {
		return "computer_use_snapshot"
	}
	if strings.Contains(lower, "click") {
		if _, ok := args["action"]; !ok {
			args["action"] = "click"
		}
		return "click_element"
	}
	if strings.Contains(lower, "type") || strings.Contains(lower, "input") ||
		strings.Contains(lower, "fill") || strings.Contains(lower, "enter") {
		return "type_text"
	}
	if strings.Contains(lower, "enable") || strings.Contains(lower, "start_browser") {
		return "computer_use_enable"
	}

	return name
}

// normalizeElementArg finds the element ID in args using any key that contains
// "element" (element, element_id, target_element_id, etc.) and normalizes it to args["element"].
func normalizeElementArg(args map[string]interface{}) {
	// Explicit aliases first
	for _, k := range []string{"element", "element_id", "id", "ref", "target"} {
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

// indexedElement stores a page element for description-based ID lookup.
type indexedElement struct {
	ID   int
	Tag  string
	Text string
	Zone string
}

// resolveElement resolves an element ID from the model's args.
// Handles both numeric element IDs and description-based lookups.
// When the model sends a description string like "top \"custname\"", this finds
// the matching element by text/zone/tag and returns its numeric ID.
func (e *SandboxToolExecutor) resolveElement(args map[string]interface{}) (int, error) {
	// Check if element is already a numeric ID
	if id, ok := args["element"].(float64); ok && id > 0 {
		return int(id), nil
	}
	if id, ok := args["element"].(int); ok && id > 0 {
		return id, nil
	}

	// Build the description string from various keys the model might use
	var desc string
	for _, k := range []string{
		"element", "element_id", "element_description", "element_desc",
		"text_element_description", "target_element",
		"target", "selector",
	} {
		if v, ok := args[k].(string); ok && v != "" {
			desc = v
			break
		}
	}

	if desc == "" {
		return 0, fmt.Errorf("no element ID or description provided")
	}

	// Try to parse as a numeric string
	if id, err := strconv.Atoi(strings.TrimSpace(desc)); err == nil && id > 0 {
		return id, nil
	}

	// Look up by description in the element index
	elems, ok := e.elemIndex[e.SandboxID]
	if !ok || len(elems) == 0 {
		return 0, fmt.Errorf("no page elements indexed yet — navigate to a page first")
	}

	descLower := strings.ToLower(desc)

	// Score each element by how well it matches
	bestID := 0
	bestScore := 0
	for _, el := range elems {
		score := 0
		elLower := strings.ToLower(el.Text)

		// Exact text match (strongest signal)
		if elLower == descLower {
			score = 100
		} else if strings.Contains(descLower, elLower) || strings.Contains(elLower, descLower) {
			// Partial text match
			score = 50
		}

		// Zone match (e.g. "top" in description matches element zone "top")
		if el.Zone != "" && strings.Contains(descLower, el.Zone) {
			score += 20
		}

		// Tag match (e.g. "input" in description)
		if strings.Contains(descLower, el.Tag) {
			score += 10
		}

		// Quoted text match: desc like `top "custname"` — extract quoted part
		if idx := strings.Index(desc, "\""); idx >= 0 {
			quoted := strings.ToLower(desc[idx+1:])
			if end := strings.Index(quoted, "\""); end >= 0 {
				quoted = quoted[:end]
			}
			if quoted != "" && strings.Contains(elLower, quoted) {
				score += 40
			}
		}

		if score > bestScore {
			bestScore = score
			bestID = el.ID
		}
	}

	if bestID > 0 && bestScore >= 30 {
		return bestID, nil
	}

	return 0, fmt.Errorf("could not resolve element description %q to an ID (best match score: %d)", desc, bestScore)
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

	// Lazy-load credentials from sandbox on first execution
	if !e.credsLoaded && e.Manager != nil {
		e.credsLoaded = true
		if output, err := e.Manager.ExecInSandbox(ctx, sandboxID, []string{"cat", "/sandbox/workspace/passwords.txt"}); err == nil {
			if creds := LoadFromText(output); !creds.IsEmpty() {
				e.Creds = creds
				e.Logger.Info("Loaded credentials from sandbox", zap.String("sandbox", sandboxID))
			}
		}
	}

	// Resolve credential placeholders before execution
	if e.Creds != nil && !e.Creds.IsEmpty() {
		args = e.Creds.Resolve(args)
	}

	// Normalize common tool name aliases (shared with PersonaAwareExecutor)
	toolName = normalizeToolName(toolName, args)

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
	ContextSize   int           // KV cache context size (default from ModelConfig: 32K)
	Tools         []OpenAITool  // Tool definitions for native tool calling
	Opts          GenerateOptions
	Compaction    CompactionConfig // zero-value disables compaction
}

// DefaultAgentLoopConfig returns sensible defaults from ModelConfig.
func DefaultAgentLoopConfig() AgentLoopConfig {
	return AgentLoopConfig{
		SystemPrompt:  "You are a coding assistant with access to tools.",
		MaxToolRounds: cfg.DefaultMaxToolRounds,
		MaxTokens:     cfg.MaxTokens,
		ContextSize:   cfg.DefaultContextSize,
		Opts:          DefaultGenerateOptions(),
	}
}

// AgentLoop runs the full agent loop: generate → parse tool calls → execute → feed back.
// It emits events to the subscriber channel in the same format as Pi's RPC events,
// so the SSE handler can stream them to the frontend without modification.
type AgentLoop struct {
	engine   *HTTPEngine
	session  *Session
	executor ToolExecutor
	config   AgentLoopConfig
	logger   *zap.Logger
	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
}

// NewAgentLoop creates a new agent loop bound to an engine.
func NewAgentLoop(engine *HTTPEngine, executor ToolExecutor, cfg AgentLoopConfig, logger *zap.Logger) (*AgentLoop, error) {
	if !engine.IsLoaded() {
		return nil, fmt.Errorf("engine model not loaded")
	}

	session, err := engine.NewSession(cfg.ContextSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Set tool definitions on session
	if len(cfg.Tools) > 0 {
		session.SetTools(cfg.Tools)
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
	sendEvent(subscriber, AgentEvent{Type: EventTypeAgentStart})

	// First generation: system prompt + user message + tools
	opts := loop.config.Opts
	opts.MaxTokens = loop.config.MaxTokens

	chatCh, err := loop.session.ChatWithTools(loop.config.SystemPrompt, userMsg, loop.config.Tools, opts)
	if err != nil {
		sendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
		sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
		return err
	}

	return loop.runLoop(ctx, chatCh, subscriber, opts)
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

	sendEvent(subscriber, AgentEvent{Type: EventTypeAgentStart})

	opts := loop.config.Opts
	opts.MaxTokens = loop.config.MaxTokens

	chatCh, err := loop.session.FeedUserMessage(userMsg, opts)
	if err != nil {
		sendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
		sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
		return err
	}

	return loop.runLoop(ctx, chatCh, subscriber, opts)
}

// runLoop is the core generation → execute tools → feed-back cycle.
// Uses native structured tool calling via /v1/chat/completions.
func (loop *AgentLoop) runLoop(ctx context.Context, chatCh <-chan ChatEvent, subscriber chan<- AgentEvent, opts GenerateOptions) error {
	round := 0
	failCounts := make(map[string]int)
	consecutiveTotalFails := 0
	const maxConsecutiveTotalFails = 5

	for {
		var contentBuf strings.Builder
		var finishReason FinishReason

		// Phase 1: Stream ChatEvents until generation completes
		for evt := range chatCh {
			if ctx.Err() != nil {
				sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return ctx.Err()
			}

			switch evt.Type {
			case ChatEventError:
				loop.logger.Error("Generation error", zap.Error(evt.Err))
				sendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: evt.Err.Error()}})
				sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
				return evt.Err

			case ChatEventDone:
				finishReason = evt.Finish

			case ChatEventContent:
				contentBuf.WriteString(evt.Content)
				sendEvent(subscriber, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: evt.Content}})

			case ChatEventThinking:
				sendEvent(subscriber, AgentEvent{Type: EventTypeThinkingDelta, Data: AgentEventData{Text: evt.Content}})
			}

			if evt.Type == ChatEventDone {
				break
			}
		}

		// Phase 2: Check if we got tool calls
		// The session's last assistant message has the accumulated tool calls
		msgs := loop.session.Messages()
		var toolCalls []ToolCallResponse
		if len(msgs) > 0 {
			lastMsg := msgs[len(msgs)-1]
			if lastMsg.Role == "assistant" {
				toolCalls = lastMsg.ToolCalls
			}
		}

		// No tool calls, max rounds, or normal stop → done
		if len(toolCalls) == 0 || finishReason != FinishToolCalls || round >= loop.config.MaxToolRounds {
			if round >= loop.config.MaxToolRounds {
				loop.logger.Warn("Max tool rounds reached, stopping agent loop",
					zap.Int("round", round),
					zap.Int("maxRounds", loop.config.MaxToolRounds),
				)
			}

			inputTokens, outputTokens := loop.session.TokenCounts()
			sendEvent(subscriber, AgentEvent{
				Type: EventTypeAgentEnd,
				Data: AgentEventData{
					Input:  float64(inputTokens),
					Output: float64(outputTokens),
					Model:  "llama-server/gemma-4-26b",
				},
			})
			return nil
		}

		round++
		loop.logger.Info("Tool calls detected",
			zap.Int("count", len(toolCalls)),
			zap.Int("round", round),
		)

		// Phase 3: Execute each tool call
		var toolResults []ToolResult
		for _, tcr := range toolCalls {
			tc := tcr.ToToolCall()

			sendEvent(subscriber, AgentEvent{
				Type: EventTypeToolStart,
				Data: AgentEventData{
					ToolName: tc.Name,
					ToolArgs: tc.Args,
					ToolID:   tc.ID,
				},
			})

			argsJSON, _ := json.Marshal(tc.Args)
			loop.logger.Info("AGENT TOOL CALL",
				zap.Int("round", round),
				zap.String("tool", tc.Name),
				zap.String("args", string(argsJSON)),
			)

			// Check retry limit
			if failCounts[tc.Name] >= cfg.MaxRetriesPerTool {
				resultStr := fmt.Sprintf(
					"[SYSTEM: Tool '%s' has failed %d times. Do NOT retry it. Use a COMPLETELY DIFFERENT approach or tool.]",
					tc.Name, cfg.MaxRetriesPerTool,
				)
				loop.logger.Warn("Tool retry limit reached, forcing strategy change",
					zap.String("tool", tc.Name),
					zap.Int("failCount", failCounts[tc.Name]),
				)
				toolResults = append(toolResults, ToolResult{
					ToolCallID: tcr.ID,
					ToolName:   tc.Name,
					Content:    resultStr,
				})
				sendEvent(subscriber, AgentEvent{
					Type: EventTypeToolEnd,
					Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Error: resultStr},
				})
				continue
			}

			// Execute the tool
			startTime := time.Now()
			result, err := loop.executor.Execute(ctx, tc.Name, tc.Args)
			elapsed := time.Since(startTime)

			// Auto-retry transient errors
			if err != nil && classifyError(err) == ErrorTransient {
				backoff := time.Duration(min(500*time.Millisecond*time.Duration(1<<min(failCounts[tc.Name], 4)), 10*time.Second))
				loop.logger.Warn("Transient error, retrying with backoff",
					zap.String("tool", tc.Name),
					zap.Duration("backoff", backoff),
					zap.Error(err),
				)
				time.Sleep(backoff)
				result, err = loop.executor.Execute(ctx, tc.Name, tc.Args)
				elapsed = time.Since(startTime)
			}

			var resultStr string
			if err != nil {
				failCounts[tc.Name]++
				consecutiveTotalFails++

				errMsg := err.Error()
				resultStr = fmt.Sprintf("<tool_use_error>%s</tool_use_error>", errMsg)

				if consecutiveTotalFails >= maxConsecutiveTotalFails {
					resultStr += "\n\n[SYSTEM: Too many consecutive failures. Call yield_artifact{\"output\":\"Failed: ...\"} to end.]"
				}

				loop.logger.Error("AGENT TOOL ERROR",
					zap.String("tool", tc.Name),
					zap.Duration("elapsed", elapsed),
					zap.Int("failCount", failCounts[tc.Name]),
					zap.Int("totalConsecFails", consecutiveTotalFails),
					zap.Error(err),
				)
			} else {
				delete(failCounts, tc.Name)
				consecutiveTotalFails = 0
				resultBytes, _ := json.Marshal(result)
				resultStr = string(resultBytes)
				if len(resultStr) > cfg.ToolResultMaxChars {
					resultStr = resultStr[:cfg.ToolResultMaxChars] + "...[truncated]"
				}
				loop.logger.Info("AGENT TOOL RESULT",
					zap.String("tool", tc.Name),
					zap.Duration("elapsed", elapsed),
					zap.Int("resultLen", len(resultStr)),
				)
			}

			sendEvent(subscriber, AgentEvent{
				Type: EventTypeToolEnd,
				Data: AgentEventData{
					ToolName: tc.Name,
					ToolID:   tc.ID,
					Result:   result,
					Error:    func() string { if err != nil { return err.Error() }; return "" }(),
				},
			})

			toolResults = append(toolResults, ToolResult{
				ToolCallID: tcr.ID,
				ToolName:   tc.Name,
				Content:    resultStr,
			})

			// yield_artifact is a terminal signal
			if tc.Name == "yield_artifact" {
				loop.logger.Info("Sub-agent yielded artifact, terminating loop",
					zap.String("tool", tc.Name),
					zap.Int("round", round),
				)
				inputTokens, outputTokens := loop.session.TokenCounts()
				sendEvent(subscriber, AgentEvent{
					Type: EventTypeAgentEnd,
					Data: AgentEventData{
						Input:  float64(inputTokens),
						Output: float64(outputTokens),
						Model:  "llama-server/gemma-4-26b",
					},
				})
				return nil
			}
		}

		// Phase 4: Goal nudge
		budgetWarning := round >= int(float64(loop.config.MaxToolRounds)*0.75) && round < loop.config.MaxToolRounds
		goalReminder, _ := RenderTemplate("goal_nudge", GoalNudgeData{
			Round:         round,
			MaxRounds:     loop.config.MaxToolRounds,
			StepsLeft:     loop.config.MaxToolRounds - round,
			BudgetWarning: budgetWarning,
		})

		// Redact sensitive data
		if ste, ok := loop.executor.(*SandboxToolExecutor); ok && ste.Creds != nil {
			for i := range toolResults {
				toolResults[i].Content = ste.Creds.Redact(toolResults[i].Content)
			}
		}

		// Context compaction
		if loop.config.Compaction.TriggerAfterTurns > 0 && ShouldCompactMessages(loop.session.Messages(), loop.config.Compaction) {
			if err := loop.compactSession(subscriber); err != nil {
				loop.logger.Warn("Compaction failed, continuing with full history", zap.Error(err))
			}
		}

		// Phase 5: Feed tool results back
		// Build the assistant message with tool calls
		assistantMsg := Message{
			Role:      "assistant",
			Content:   contentBuf.String(),
			ToolCalls: toolCalls,
		}

		nextCh, err := loop.session.FeedToolResults(assistantMsg, toolResults, goalReminder, opts)
		if err != nil {
			sendEvent(subscriber, AgentEvent{Type: EventTypeError, Data: AgentEventData{Error: err.Error()}})
			sendEvent(subscriber, AgentEvent{Type: EventTypeAgentEnd})
			return err
		}
		chatCh = nextCh
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

// compactSession performs extractive compaction by creating a new session
// with compacted history, freeing the old KV cache.
func (loop *AgentLoop) compactSession(subscriber chan<- AgentEvent) error {
	messages := loop.session.Messages()
	systemPrompt := loop.config.SystemPrompt
	compCfg := loop.config.Compaction

	newMessages := CompactMessages(messages, systemPrompt, compCfg)

	loop.logger.Info("Compacting session",
		zap.Int("oldMessages", len(messages)),
		zap.Int("newMessages", len(newMessages)),
	)

	// Close old session (free VRAM)
	if err := loop.session.Close(); err != nil {
		return fmt.Errorf("failed to close old session: %w", err)
	}

	// Create fresh session
	newSession, err := loop.engine.NewSession(loop.config.ContextSize)
	if err != nil {
		return fmt.Errorf("failed to create new session: %w", err)
	}

	// Set compacted messages and tools on new session
	newSession.SetMessages(newMessages)
	newSession.SetTools(loop.config.Tools)

	loop.session = newSession
	loop.logger.Info("Session compaction complete")

	// Notify frontend
	if subscriber != nil {
		sendEvent(subscriber, AgentEvent{
			Type: EventTypeAgentEnd,
			Data: AgentEventData{Model: "compacted"},
		})
		sendEvent(subscriber, AgentEvent{
			Type: EventTypeAgentStart,
		})
	}

	return nil
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

		// Build element index for description→ID resolution
		var idx []indexedElement
		for _, elems := range grouped {
			for _, ei := range elems {
				idx = append(idx, indexedElement{ID: ei.id, Tag: ei.tag, Text: ei.text, Zone: ei.zone})
			}
		}
		for _, ei := range ungrouped {
			idx = append(idx, indexedElement{ID: ei.id, Tag: ei.tag, Text: ei.text, Zone: ei.zone})
		}
		if e.elemIndex == nil {
			e.elemIndex = make(map[string][]indexedElement)
		}
		e.elemIndex[e.SandboxID] = idx

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
	normalizeElementArg(args)
	elementID, err := e.resolveElement(args)
	if err != nil {
		return nil, fmt.Errorf("click_element: %w", err)
	}

	clickArgs := map[string]interface{}{
		"action":  "click",
		"element": elementID,
	}
	result, err := e.CU.Act(ctx, sandboxID, "click", clickArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to click element %d: %w", elementID, err)
	}

	return map[string]interface{}{
		"success":          true,
		"clicked_element":  elementID,
		"page_after_click": e.extractPageSummary(result),
	}, nil
}

// macroTypeText is a macro tool: types text into an element, optionally submitting.
func (e *SandboxToolExecutor) macroTypeText(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	text, _ := args["text"].(string)
	if text == "" {
		return nil, fmt.Errorf("missing 'text' argument for type_text")
	}

	normalizeElementArg(args)
	elementID, err := e.resolveElement(args)
	if err != nil {
		return nil, fmt.Errorf("type_text: %w", err)
	}

	submit := false
	if s, ok := args["submit"].(bool); ok {
		submit = s
	}

	typeArgs := map[string]interface{}{
		"action":  "type",
		"text":    text,
		"element": elementID,
		"submit":  submit,
	}
	result, err := e.CU.Act(ctx, sandboxID, "type", typeArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to type text: %w", err)
	}

	resp := map[string]interface{}{
		"success":      true,
		"typed_text":   text,
		"into_element": elementID,
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

// ErrorClass categorizes tool execution errors for appropriate retry behavior.
type ErrorClass int

const (
	ErrorTransient ErrorClass = iota // network timeout, CDP disconnect, rate limit
	ErrorPermanent                    // invalid tool, bad args, permission denied
	ErrorUnknown
)

// classifyError categorizes an error for retry decisions.
func classifyError(err error) ErrorClass {
	msg := strings.ToLower(err.Error())
	// Transient: things that might succeed on retry
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "connection") ||
		strings.Contains(msg, "reset") || strings.Contains(msg, "temporarily") ||
		strings.Contains(msg, "refused") || strings.Contains(msg, "eof") ||
		strings.Contains(msg, "context canceled") {
		return ErrorTransient
	}
	// Permanent: things that won't succeed on retry
	if strings.Contains(msg, "not found") || strings.Contains(msg, "denied") ||
		strings.Contains(msg, "invalid") || strings.Contains(msg, "unknown persona") ||
		strings.Contains(msg, "missing") || strings.Contains(msg, "not available") {
		return ErrorPermanent
	}
	return ErrorUnknown
}

// extractJSONStringValue extracts a value from a loosely-formatted JSON string.
// Handles cases like {command: "ls -la"} where keys aren't quoted.
// Also handles single-quoted values like {command: 'ls -la'}.
func extractJSONStringValue(raw, key string) string {
	// Try double-quoted value with quoted/unquoted key
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
	// Try single-quoted value with quoted/unquoted key
	singlePatterns := []string{
		fmt.Sprintf(`"%s"\s*:\s*'([^']*)'`, key),
		fmt.Sprintf(`%s\s*:\s*'([^']*)'`, key),
	}
	for _, p := range singlePatterns {
		re := regexp.MustCompile(p)
		m := re.FindStringSubmatch(raw)
		if len(m) >= 2 {
			return m[1]
		}
	}
	return ""
}

// Session returns the underlying session for inspection.
func (loop *AgentLoop) Session() *Session {
	return loop.session
}
