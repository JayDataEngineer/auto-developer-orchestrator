package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/mcp"
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
	EventTypeSubAgentStart    AgentEventType = "subagent_start"
	EventTypeSubAgentEnd      AgentEventType = "subagent_end"
	EventTypeApprovalRequest  AgentEventType = "approval_request"
	EventTypeCompactionStart  AgentEventType = "compaction_start"
	EventTypeCompactionEnd    AgentEventType = "compaction_end"
	EventTypeToolUpdate       AgentEventType = "tool_update"
)

// subscriberKey is the context key for injecting the SSE subscriber channel
// into tool execution contexts. This lets tools like ask_user send events
// to the frontend without the executor needing a direct subscriber reference.
type subscriberKeyType struct{}

var subscriberKeyTypeVal = subscriberKeyType{}

// ContextWithSubscriber injects the SSE subscriber channel into a context.
func ContextWithSubscriber(ctx context.Context, ch chan<- AgentEvent) context.Context {
	return context.WithValue(ctx, subscriberKeyTypeVal, ch)
}

// SubscriberFromContext retrieves the SSE subscriber channel from a context.
func SubscriberFromContext(ctx context.Context) chan<- AgentEvent {
	ch, _ := ctx.Value(subscriberKeyTypeVal).(chan<- AgentEvent)
	return ch
}

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

// ToolExecutorStreaming is an optional interface for tools that stream partial results.
// The agent loop checks for this interface and uses it when available.
type ToolExecutorStreaming interface {
	ToolExecutor
	ExecuteStreaming(ctx context.Context, toolName string, args map[string]interface{}, onUpdate func(string)) (interface{}, error)
}

// TranscriptSaver persists pre-compaction message snapshots.
// Implemented by the handler layer to avoid importing storage into the llama package.
type TranscriptSaver interface {
	SaveTranscript(messagesJSON []byte, reason string, tokenCount int)
}

// ApprovalResponse is the user's response to an approval/question request.
type ApprovalResponse struct {
	Action  string // "approve", "deny", "answer"
	Message string // User's text response (for "answer" action)
}

// ApprovalManager manages pending approval/question requests.
// Interface to decouple the llama package from the approval package.
type ApprovalManager interface {
	Register(requestID string) <-chan ApprovalResponse
	Resolve(requestID string, resp ApprovalResponse) bool
	Cleanup(requestID string)
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
	IsReady(sandboxID string) bool
	Enable(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	Screenshot(ctx context.Context, sandboxID string, describe bool) (map[string]interface{}, error)
	Snapshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	Act(ctx context.Context, sandboxID string, action string, args map[string]interface{}) (map[string]interface{}, error)

	// Desktop automation (X11)
	DesktopScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	DesktopClick(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error)
	DesktopType(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error)
	DesktopKey(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error)

	// Resolution query (for coordinate normalization)
	Resolution(ctx context.Context, sandboxID string) (map[string]interface{}, error)
}

// SandboxToolExecutor executes tools via the sandbox manager and computer use provider.
type SandboxToolExecutor struct {
	SandboxID string
	Manager   *sandbox.Manager
	CU        ComputerUseProvider
	Logger    *zap.Logger
	Creds     *CredentialStore // optional: resolve/redact sensitive data
	MCPClient *mcp.Client      // optional: MCP research server for search/scrape
	ApprovalMgr ApprovalManager // optional: nil for scheduled jobs (fire-and-forget)

	// Vision in the loop: after page-changing actions, automatically capture a
	// screenshot and describe it via the vision model. Gives the agent spatial
	// understanding that pure DOM element lists can't provide (layout, colors,
	// images, error messages rendered as text, etc.)
	VisionEnabled bool

	// Change detection: caches last seen element signatures per sandbox
	lastElements map[string]map[string]bool       // sandboxID → set of "tag:text" signatures (for change detection)
	elemIndex    map[string][]indexedElement       // sandboxID → ordered list of elements (for description→ID lookup)
	credsLoaded  bool                             // true after first attempt to load credentials

	// Page fingerprinting for loop/stagnation detection
	pageFingerprints map[string][]pageFingerprint // sandboxID → recent fingerprints (last 10)

	// File operations (lazy-initialized on first file tool call)
	fileOps *SandboxFileOps
}

// pageFingerprint is a compact hash of page state for detecting stagnation.
type pageFingerprint struct {
	url     string
	elCount int
	sig     string // first 16 chars of element signature hash
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
	case "read", "cat", "read_file", "view":
		return "file_read"
	case "write", "write_file", "create_file":
		return "file_write"
	case "edit", "replace", "file_edit", "sed_replace":
		return "file_edit"
	case "grep", "search", "search_files", "rg":
		return "file_grep"
	case "glob", "find_files", "list_files", "ls_files":
		return "file_glob"
	case "code_search", "find_references", "find_definition", "list_symbols":
		return "code_search"
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

// selfHealElement attempts to find a replacement element when the original is stale.
// Stagehand pattern: re-snapshot the page and match by tag + text similarity.
// Returns the new element ID, or 0 if no suitable match found.
func (e *SandboxToolExecutor) selfHealElement(ctx context.Context, sandboxID string, originalID int, hint string) (int, error) {
	// Look up the original element's tag/text from the index
	var origTag, origText string
	if elems, ok := e.elemIndex[sandboxID]; ok {
		for _, el := range elems {
			if el.ID == originalID {
				origTag = el.Tag
				origText = el.Text
				break
			}
		}
	}

	// Re-snapshot to get fresh elements
	snapResult, err := e.CU.Snapshot(ctx, sandboxID)
	if err != nil {
		return 0, fmt.Errorf("self-heal: re-snapshot failed: %w", err)
	}

	// Parse the new snapshot and find the best match
	summary := e.extractPageSummary(snapResult)
	_ = summary // Elements are indexed by extractPageSummary

	newElems, ok := e.elemIndex[sandboxID]
	if !ok || len(newElems) == 0 {
		return 0, fmt.Errorf("self-heal: no elements in fresh snapshot")
	}

	// Score by similarity to original element
	bestID := 0
	bestScore := 0
	for _, el := range newElems {
		score := 0
		if origTag != "" && el.Tag == origTag {
			score += 50
		}
		if origText != "" {
			origLower := strings.ToLower(origText)
			elLower := strings.ToLower(el.Text)
			if origLower == elLower {
				score += 60
			} else if strings.Contains(origLower, elLower) || strings.Contains(elLower, origLower) {
				score += 30
			}
		}
		if hint != "" {
			hintLower := strings.ToLower(hint)
			elLower := strings.ToLower(el.Text)
			if strings.Contains(hintLower, elLower) || strings.Contains(elLower, hintLower) {
				score += 20
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
	return 0, fmt.Errorf("self-heal: no similar element found (best score: %d)", bestScore)
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

	// ask_user: ask the user a question and wait for response (interactive sessions only)
	if toolName == "ask_user" {
		question, _ := args["question"].(string)
		if question == "" {
			return nil, fmt.Errorf("missing 'question' argument")
		}
		if e.ApprovalMgr == nil {
			return nil, fmt.Errorf("ask_user is not available in non-interactive sessions")
		}

		// Generate unique request ID
		requestID := fmt.Sprintf("ask-%d", time.Now().UnixMilli())

		// Send approval_request SSE event via subscriber in context
		subscriber := SubscriberFromContext(ctx)
		if subscriber != nil {
			sendEvent(subscriber, AgentEvent{
				Type: EventTypeApprovalRequest,
				Data: AgentEventData{
					ToolName: "ask_user",
					ToolID:   requestID,
					ToolArgs: map[string]interface{}{"question": question},
					Result: map[string]interface{}{
						"requestId": requestID,
						"type":      "question",
						"message":   question,
					},
				},
			})
		}

		// Register and wait for user response
		ch := e.ApprovalMgr.Register(requestID)
		defer e.ApprovalMgr.Cleanup(requestID)

		select {
		case resp := <-ch:
			if resp.Action == "answer" {
				return map[string]interface{}{"answer": resp.Message}, nil
			}
			return nil, fmt.Errorf("user declined to answer")
		case <-ctx.Done():
			return nil, fmt.Errorf("ask_user timed out: context cancelled")
		}
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
		// Try MCP research server first, fall back to browser-based Google search
		return e.macroSearchWeb(ctx, sandboxID, args)

	case "scrape":
		// Scrape a URL via MCP server (returns clean markdown)
		return e.macroScrape(ctx, args)

	case "mcp_call":
		// Generic MCP tool passthrough — call any tool on the MCP research server
		return e.macroMCPCall(ctx, args)

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
		// Coordinate normalization: if x,y are in 0-1000 range (CUA pattern),
		// convert to actual screen pixels
		if x >= 0 && x <= 1000 && y >= 0 && y <= 1000 {
			if res, err := e.CU.Resolution(ctx, sandboxID); err == nil {
				screenW := atoiFromInterface(res["width"])
				screenH := atoiFromInterface(res["height"])
				if screenW > 0 && screenH > 0 {
					xInt, yInt := NormalizeCoords(x, y, screenW, screenH)
					x = float64(xInt)
					y = float64(yInt)
					e.Logger.Debug("Normalized desktop coordinates",
						zap.Float64("normX", x), zap.Float64("normY", y),
						zap.Int("screenW", screenW), zap.Int("screenH", screenH),
					)
				}
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

	// ── New unified tools ──

	case "observe":
		// Stagehand pattern: screenshot + DOM snapshot + vision description in one call
		return e.macroObserve(ctx, sandboxID)

	case "wait":
		// Explicit wait between actions (from CUA/browser-use)
		seconds := 2
		if s, ok := args["seconds"]; ok {
			if f, ok := s.(float64); ok && f > 0 && f <= 30 {
				seconds = int(f)
			}
		}
		time.Sleep(time.Duration(seconds) * time.Second)
		return map[string]interface{}{"output": fmt.Sprintf("Waited %d seconds", seconds)}, nil

	case "scroll_page":
		// Browser page scroll (from browser-use)
		direction := "down"
		if d, ok := args["direction"].(string); ok {
			direction = d
		}
		scrollArgs := map[string]interface{}{"action": "scroll", "direction": direction}
		return e.CU.Act(ctx, sandboxID, "scroll", scrollArgs)

	// ── File tools (Claude Code pattern: read before edit) ──

	case "file_read":
		return e.executeFileRead(ctx, sandboxID, args)
	case "file_write":
		return e.executeFileWrite(ctx, sandboxID, args)
	case "file_edit":
		return e.executeFileEdit(ctx, sandboxID, args)
	case "file_grep":
		return e.executeFileGrep(ctx, sandboxID, args)
	case "file_glob":
		return e.executeFileGlob(ctx, sandboxID, args)

	case "code_search":
		return e.executeCodeSearch(ctx, sandboxID, args)

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
		Opts: GenerateOptions{
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
			TopP:        cfg.TopP,
			TopK:        cfg.TopK,
		},
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

	// Context management
	consecutiveCompactionFailures int
	saver                        TranscriptSaver
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
	// Inject subscriber into context so tools (ask_user) can send SSE events
	ctx = ContextWithSubscriber(ctx, subscriber)

	round := 0
	failCounts := make(map[string]int)
	consecutiveTotalFails := 0
	const maxConsecutiveTotalFails = 5
	cycleDetector := NewCycleDetector(10)

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

		// Phase 3: Execute tool calls
		// Partition into delegate_to (can run concurrently) and everything else (sequential).
		var toolResults []ToolResult
		var delegateCalls, sequentialCalls []ToolCallResponse
		for _, tcr := range toolCalls {
			if tcr.Function.Name == "delegate_to" {
				delegateCalls = append(delegateCalls, tcr)
			} else {
				sequentialCalls = append(sequentialCalls, tcr)
			}
		}

		// Execute sequential tool calls
		for _, tcr := range sequentialCalls {
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

			// Execute the tool with timeout to prevent hanging
			startTime := time.Now()

			// Check if the executor supports streaming for this tool
			var streamer ToolExecutorStreaming
			if s, ok := loop.executor.(ToolExecutorStreaming); ok {
				streamer = s
			}

			result, err := func() (interface{}, error) {
				timeout := time.Duration(cfg.ToolExecTimeoutSec) * time.Second
				useStreaming := streamer != nil

				if timeout <= 0 {
					if useStreaming {
						return streamer.ExecuteStreaming(ctx, tc.Name, tc.Args, func(update string) {
							sendEvent(subscriber, AgentEvent{
								Type: EventTypeToolUpdate,
								Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Text: update},
							})
						})
					}
					return loop.executor.Execute(ctx, tc.Name, tc.Args)
				}

				toolCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()

				type toolResult struct {
					val interface{}
					err error
				}
				ch := make(chan toolResult, 1)
				go func() {
					var val interface{}
					var err error
					if useStreaming {
						val, err = streamer.ExecuteStreaming(toolCtx, tc.Name, tc.Args, func(update string) {
							sendEvent(subscriber, AgentEvent{
								Type: EventTypeToolUpdate,
								Data: AgentEventData{ToolName: tc.Name, ToolID: tc.ID, Text: update},
							})
						})
					} else {
						val, err = loop.executor.Execute(toolCtx, tc.Name, tc.Args)
					}
					ch <- toolResult{val, err}
				}()
				select {
				case r := <-ch:
					return r.val, r.err
				case <-toolCtx.Done():
					return nil, fmt.Errorf("tool '%s' timed out after %ds", tc.Name, cfg.ToolExecTimeoutSec)
				}
			}()
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
				retryCtx, retryCancel := context.WithTimeout(ctx, time.Duration(cfg.ToolExecTimeoutSec)*time.Second)
				result, err = loop.executor.Execute(retryCtx, tc.Name, tc.Args)
				retryCancel()
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

			// Cycle detection: record this tool call and check for loops
			if cycleDetector.Record(tc.Name, tc.Args, resultStr, round) {
				loop.logger.Warn("Cycle detected, injecting nudge",
					zap.String("tool", tc.Name),
					zap.Int("round", round),
				)
				toolResults = append(toolResults, ToolResult{
					ToolCallID: "__cycle_nudge__",
					ToolName:   "system",
					Content:    CycleNudge(),
				})
			}

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

		// Execute delegate_to calls concurrently
		if len(delegateCalls) > 0 {
			loop.logger.Info("Executing delegate_to calls concurrently",
				zap.Int("count", len(delegateCalls)),
				zap.Int("round", round),
			)
			delegateResults := loop.executeDelegatesConcurrently(ctx, delegateCalls, subscriber, failCounts, round)
			toolResults = append(toolResults, delegateResults...)
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

		// Context compaction — size-based with circuit breaker
		if loop.consecutiveCompactionFailures < cfg.MaxCompactionFailures {
			needMicro, needFull := ShouldCompact(loop.session)
			if needFull {
				if err := loop.compactSession(subscriber); err != nil {
					loop.consecutiveCompactionFailures++
					loop.logger.Warn("Full compaction failed",
						zap.Error(err),
						zap.Int("consecutiveFailures", loop.consecutiveCompactionFailures))
				} else {
					loop.consecutiveCompactionFailures = 0
				}
			} else if needMicro {
				// Micro-compact: clear old tool results (cheap, no LLM call)
				MicroCompactInPlace(loop.session, 4)
				if subscriber != nil {
					sendEvent(subscriber, AgentEvent{
						Type: EventTypeCompactionEnd,
						Data: AgentEventData{
							Result: map[string]interface{}{
								"type": "micro",
							},
						},
					})
				}
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

// SetTranscriptSaver configures the transcript saver for pre-compaction snapshots.
func (loop *AgentLoop) SetTranscriptSaver(saver TranscriptSaver) {
	loop.saver = saver
}

// compactSession performs extractive compaction by creating a new session
// with compacted history, freeing the old KV cache.
func (loop *AgentLoop) compactSession(subscriber chan<- AgentEvent) error {
	messages := loop.session.Messages()
	systemPrompt := loop.config.SystemPrompt
	compCfg := loop.config.Compaction

	// Notify frontend: compaction starting
	if subscriber != nil {
		used, capacity := loop.session.ContextUsage()
		sendEvent(subscriber, AgentEvent{
			Type: EventTypeCompactionStart,
			Data: AgentEventData{
				Result: map[string]interface{}{
					"type":         "full",
					"messageCount": len(messages),
					"usedTokens":   used,
					"capacity":     capacity,
				},
			},
		})
	}

	// Save transcript before compacting (best-effort)
	if loop.saver != nil {
		if msgsJSON, err := json.Marshal(messages); err == nil {
			used, _ := loop.session.ContextUsage()
			loop.saver.SaveTranscript(msgsJSON, "full_compaction", used)
		}
	}

	// Try LLM-based summarization, fall back to extractive
	newMessages := CompactWithSummary(messages, systemPrompt, loop.engine, compCfg.KeepLastTurns)

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
			Type: EventTypeCompactionEnd,
			Data: AgentEventData{
				Result: map[string]interface{}{
					"type":       "full",
					"oldMessages": len(messages),
					"newMessages": len(newMessages),
				},
			},
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

	resp := map[string]interface{}{
		"success":      true,
		"navigated_to": url,
		"page_summary": e.extractPageSummary(navResult),
	}

	// Vision: describe what the page looks like after navigation
	if vision := e.visionSummary(ctx, sandboxID); vision != "" {
		resp["vision"] = vision
	}

	return resp, nil
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

		// Track page fingerprint for stagnation detection
		fp := pageFingerprint{
			url:     parts[0], // URL: ...
			elCount: len(newSigs),
			sig:     fingerprintSig(newSigs),
		}
		if e.pageFingerprints == nil {
			e.pageFingerprints = make(map[string][]pageFingerprint)
		}
		fps := e.pageFingerprints[e.SandboxID]
		fps = append(fps, fp)
		if len(fps) > 10 {
			fps = fps[len(fps)-10:]
		}
		e.pageFingerprints[e.SandboxID] = fps

		// Add stagnation warning if page hasn't changed for 3+ consecutive steps
		if stagnantCount := countStagnant(fps); stagnantCount >= 3 {
			parts = append(parts, fmt.Sprintf("[WARNING: Page unchanged for %d steps. Try a DIFFERENT action — scroll, click elsewhere, or navigate to a new URL.]", stagnantCount))
		}

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

// visionSummary captures a screenshot and describes it via the vision model.
// Returns a text description of what's visible on the page, or empty string on failure.
// This gives the agent spatial understanding beyond the DOM element list — layout,
// colors, images, error banners, popups, etc.
func (e *SandboxToolExecutor) visionSummary(ctx context.Context, sandboxID string) string {
	if !e.VisionEnabled {
		return ""
	}

	result, err := e.CU.Screenshot(ctx, sandboxID, true)
	if err != nil {
		e.Logger.Debug("vision screenshot failed", zap.Error(err))
		return ""
	}

	desc, ok := result["description"].(string)
	if !ok || desc == "" {
		return ""
	}

	// Truncate to prevent context bloat — 500 chars is enough for page overview
	if len(desc) > 500 {
		desc = desc[:497] + "..."
	}
	return desc
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
// Uses IsReady (fast in-memory check) for polling instead of Screenshot
// (which is slow and can block for 30s on CDP timeout).
func (e *SandboxToolExecutor) ensureBrowserReady(ctx context.Context, sandboxID string) error {
	// Fast path: IsReady checks if CDP client exists and is connected (~1ms)
	if e.CU.IsReady(sandboxID) {
		return nil
	}

	// Browser not running — enable it (returns immediately, sets up CDP in background)
	_, err := e.CU.Enable(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("enable failed: %w", err)
	}

	// Poll IsReady every second until CDP client connects or timeout.
	// IsReady is a cheap in-memory check — no CDP calls, no image capture.
	setupCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for i := 0; i < 90; i++ {
		select {
		case <-setupCtx.Done():
			return fmt.Errorf("browser setup timed out after 90s")
		case <-ctx.Done():
			return fmt.Errorf("agent cancelled while waiting for browser: %w", ctx.Err())
		default:
		}

		time.Sleep(1 * time.Second)
		if e.CU.IsReady(sandboxID) {
			e.Logger.Info("Browser ready", zap.Int("polls", i+1))
			return nil
		}
		e.Logger.Debug("Browser not ready yet", zap.Int("poll", i+1))
	}

	return fmt.Errorf("browser did not become ready after 90 seconds")
}

// macroReadPage is a macro tool: returns the current page elements and info.
func (e *SandboxToolExecutor) macroReadPage(ctx context.Context, sandboxID string) (interface{}, error) {
	result, err := e.CU.Snapshot(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("failed to read page: %w", err)
	}

	resp := map[string]interface{}{
		"success":      true,
		"page_summary": e.extractPageSummary(result),
	}

	// Vision: describe the current page state
	if vision := e.visionSummary(ctx, sandboxID); vision != "" {
		resp["vision"] = vision
	}

	return resp, nil
}

// macroObserve is a macro tool: screenshot + DOM snapshot + vision description in one call.
// Combines read_page + screenshot into a single "observe the page" operation (Stagehand pattern).
func (e *SandboxToolExecutor) macroObserve(ctx context.Context, sandboxID string) (interface{}, error) {
	// Get DOM snapshot with element IDs
	snapshotResult, err := e.CU.Snapshot(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("observe: snapshot failed: %w", err)
	}

	resp := map[string]interface{}{
		"success":      true,
		"page_summary": e.extractPageSummary(snapshotResult),
	}

	// Get screenshot with vision description
	screenshotResult, err := e.CU.Screenshot(ctx, sandboxID, true)
	if err == nil {
		if url, ok := screenshotResult["url"]; ok {
			resp["screenshot_url"] = url
		}
		if desc, ok := screenshotResult["description"]; ok {
			resp["vision"] = desc
		}
	}

	// Vision summary as fallback
	if _, ok := resp["vision"]; !ok {
		if vision := e.visionSummary(ctx, sandboxID); vision != "" {
			resp["vision"] = vision
		}
	}

	return resp, nil
}

// macroClickElement is a macro tool: clicks an element and returns updated page info.
func (e *SandboxToolExecutor) macroClickElement(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	normalizeElementArg(args)
	elementID, err := e.resolveElement(args)
	if err != nil {
		// Self-heal: re-snapshot and try to find a matching element
		if healedID, healErr := e.selfHealElement(ctx, sandboxID, 0, fmt.Sprintf("%v", args["element"])); healErr == nil {
			elementID = healedID
			e.Logger.Debug("Self-healed element for click",
				zap.Int("newID", healedID),
			)
		} else {
			return nil, fmt.Errorf("click_element: %w (self-heal also failed: %v)", err, healErr)
		}
	}

	clickArgs := map[string]interface{}{
		"action":  "click",
		"element": elementID,
	}
	result, err := e.CU.Act(ctx, sandboxID, "click", clickArgs)
	if err != nil {
		// Self-heal: element may have gone stale after page change
		if healedID, healErr := e.selfHealElement(ctx, sandboxID, elementID, ""); healErr == nil {
			clickArgs["element"] = healedID
			result, err = e.CU.Act(ctx, sandboxID, "click", clickArgs)
			if err != nil {
				return nil, fmt.Errorf("failed to click element %d (self-healed to %d): %w", elementID, healedID, err)
			}
			elementID = healedID
			e.Logger.Debug("Self-healed click after Act failure",
				zap.Int("oldID", elementID),
				zap.Int("newID", healedID),
			)
		} else {
			return nil, fmt.Errorf("failed to click element %d: %w", elementID, err)
		}
	}

	resp := map[string]interface{}{
		"success":          true,
		"clicked_element":  elementID,
		"page_after_click": e.extractPageSummary(result),
	}

	// Vision: describe page state after click (dialog? new page? error?)
	if vision := e.visionSummary(ctx, sandboxID); vision != "" {
		resp["vision"] = vision
	}

	return resp, nil
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
		// Self-heal: re-snapshot and try to find a matching element
		if healedID, healErr := e.selfHealElement(ctx, sandboxID, 0, fmt.Sprintf("%v", args["element"])); healErr == nil {
			elementID = healedID
			e.Logger.Debug("Self-healed element for type_text",
				zap.Int("newID", healedID),
			)
		} else {
			return nil, fmt.Errorf("type_text: %w (self-heal also failed: %v)", err, healErr)
		}
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

		// Vision: describe page after form submission (results? error? redirect?)
		if vision := e.visionSummary(ctx, sandboxID); vision != "" {
			resp["vision"] = vision
		}
	}
	return resp, nil
}

// macroSearchWeb searches the web. Uses MCP research server if available,
// falls back to browser-based Google search otherwise.
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

	// Try MCP research server first (much faster, no browser needed)
	if e.MCPClient != nil {
		result, err := e.MCPClient.Research(ctx, query, 3)
		if err == nil && result != "" {
			e.Logger.Info("search via MCP research server", zap.String("query", query))
			return map[string]interface{}{
				"success": true,
				"query":   query,
				"source":  "mcp_research",
				"results": result,
			}, nil
		}
		e.Logger.Warn("MCP research failed, falling back to browser search", zap.Error(err))
	}

	// Fallback: browser-based Google search
	searchURL := fmt.Sprintf("https://www.google.com/search?q=%s", url.QueryEscape(query))
	if err := e.ensureBrowserReady(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	navArgs := map[string]interface{}{"action": "navigate", "url": searchURL}
	navResult, err := e.CU.Act(ctx, sandboxID, "navigate", navArgs)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	resp := map[string]interface{}{
		"success":      true,
		"query":        query,
		"source":       "browser_google",
		"search_url":   searchURL,
		"page_summary": e.extractPageSummary(navResult),
	}

	if vision := e.visionSummary(ctx, sandboxID); vision != "" {
		resp["vision"] = vision
	}

	return resp, nil
}

// macroScrape fetches a URL and returns its content as clean markdown via MCP.
func (e *SandboxToolExecutor) macroScrape(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	targetURL, _ := args["url"].(string)
	if targetURL == "" {
		return nil, fmt.Errorf("missing 'url' argument for scrape")
	}

	// MCP scrape (primary path)
	if e.MCPClient != nil {
		result, err := e.MCPClient.Scrape(ctx, targetURL)
		if err == nil && result != "" {
			e.Logger.Info("scrape via MCP server", zap.String("url", targetURL))
			return map[string]interface{}{
				"success": true,
				"url":     targetURL,
				"source":  "mcp_scrape",
				"content": result,
			}, nil
		}
		e.Logger.Warn("MCP scrape failed", zap.Error(err))
	}

	return nil, fmt.Errorf("scrape failed: MCP server unavailable and no browser fallback for scrape")
}

// macroMCPCall is a generic passthrough to any tool on the MCP research server.
func (e *SandboxToolExecutor) macroMCPCall(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	toolName, _ := args["tool"].(string)
	if toolName == "" {
		return nil, fmt.Errorf("missing 'tool' argument for mcp_call. Specify the MCP tool name, e.g. 'research', 'scrape', 'extract', 'map', 'crawl'")
	}

	arguments := make(map[string]any)
	if raw, ok := args["arguments"].(map[string]interface{}); ok {
		for k, v := range raw {
			arguments[k] = v
		}
	} else if raw, ok := args["arguments"].(map[string]any); ok {
		arguments = raw
	}

	if e.MCPClient == nil {
		return nil, fmt.Errorf("mcp_call failed: MCP client not configured")
	}

	result, err := e.MCPClient.CallTool(ctx, toolName, arguments)
	if err != nil {
		e.Logger.Warn("mcp_call failed", zap.String("tool", toolName), zap.Error(err))
		return nil, fmt.Errorf("mcp_call %s failed: %w", toolName, err)
	}

	e.Logger.Info("mcp_call succeeded", zap.String("tool", toolName))
	return map[string]interface{}{
		"success": true,
		"tool":    toolName,
		"source":  "mcp",
		"result":  result,
	}, nil
}

// ErrorClass categorizes tool execution errors for appropriate retry behavior.
type ErrorClass int

const (
	ErrorTransient ErrorClass = iota // network timeout, CDP disconnect, rate limit
	ErrorPermanent                    // invalid tool, bad args, permission denied
	ErrorUnknown
)

// executeDelegatesConcurrently runs multiple delegate_to calls in parallel goroutines.
// For local engines, concurrency is capped at MaxConcurrentAgents-1 to protect VRAM.
// For cloud engines, all delegates run simultaneously.
func (loop *AgentLoop) executeDelegatesConcurrently(
	ctx context.Context,
	delegateCalls []ToolCallResponse,
	subscriber chan<- AgentEvent,
	failCounts map[string]int,
	round int,
) []ToolResult {
	maxConcurrent := cfg.MaxConcurrentAgents - 1 // -1 for the orchestrator session
	if loop.engine.IsCloud() {
		maxConcurrent = len(delegateCalls)
	}
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	results := make([]ToolResult, len(delegateCalls))

	for i, tcr := range delegateCalls {
		wg.Add(1)
		sem <- struct{}{} // acquire slot
		go func(idx int, call ToolCallResponse) {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			tc := call.ToToolCall()

			sendEvent(subscriber, AgentEvent{
				Type: EventTypeToolStart,
				Data: AgentEventData{
					ToolName: tc.Name,
					ToolArgs: tc.Args,
					ToolID:   tc.ID,
				},
			})

			argsJSON, _ := json.Marshal(tc.Args)
			loop.logger.Info("CONCURRENT DELEGATE",
				zap.Int("round", round),
				zap.Int("index", idx),
				zap.String("args", string(argsJSON)),
			)

			// Execute the delegate
			var result interface{}
			var err error
			if cfg.ToolExecTimeoutSec <= 0 {
				result, err = loop.executor.Execute(ctx, tc.Name, tc.Args)
			} else {
				toolCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ToolExecTimeoutSec)*time.Second)
				result, err = loop.executor.Execute(toolCtx, tc.Name, tc.Args)
				cancel()
			}

			var resultStr string
			if err != nil {
				failCounts[tc.Name]++
				resultStr = fmt.Sprintf("<tool_use_error>%s</tool_use_error>", err.Error())
				loop.logger.Error("CONCURRENT DELEGATE ERROR",
					zap.Int("index", idx),
					zap.Error(err),
				)
			} else {
				delete(failCounts, tc.Name)
				resultBytes, _ := json.Marshal(result)
				resultStr = string(resultBytes)
				if len(resultStr) > cfg.ToolResultMaxChars {
					resultStr = resultStr[:cfg.ToolResultMaxChars] + "...[truncated]"
				}
				loop.logger.Info("CONCURRENT DELEGATE DONE",
					zap.Int("index", idx),
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

			results[idx] = ToolResult{
				ToolCallID: call.ID,
				ToolName:   tc.Name,
				Content:    resultStr,
			}
		}(i, tcr)
	}

	wg.Wait()
	return results
}

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

// fingerprintSig creates a compact signature from element signatures for page fingerprinting.
// Uses sorted signatures to produce a deterministic hash of page state.
func fingerprintSig(sigs map[string]bool) string {
	keys := make([]string, 0, len(sigs))
	for k := range sigs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	combined := strings.Join(keys, "|")
	if len(combined) > 64 {
		combined = combined[:64]
	}
	return combined
}

// countStagnant returns how many of the most recent fingerprints are identical.
// A stagnant page (same URL + same element signature) suggests the agent is stuck.
func countStagnant(fps []pageFingerprint) int {
	if len(fps) < 2 {
		return 0
	}
	last := fps[len(fps)-1]
	count := 0
	for i := len(fps) - 1; i >= 0; i-- {
		if fps[i].url == last.url && fps[i].sig == last.sig {
			count++
		} else {
			break
		}
	}
	return count
}

// Session returns the underlying session for inspection.
func (loop *AgentLoop) Session() *Session {
	return loop.session
}

// ── File tool handlers (SandboxToolExecutor methods) ────────────────

// ensureFileOps lazy-initializes the SandboxFileOps for this executor.
func (e *SandboxToolExecutor) ensureFileOps(sandboxID string) *SandboxFileOps {
	if e.fileOps == nil {
		e.fileOps = NewSandboxFileOps(e.Manager, sandboxID, e.Logger)
	}
	return e.fileOps
}

func (e *SandboxToolExecutor) executeFileRead(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	path, _ := args["file_path"].(string)
	if path == "" {
		return nil, fmt.Errorf("missing 'file_path' argument")
	}
	var offset, limit int
	if v, ok := args["offset"].(float64); ok {
		offset = int(v)
	}
	if v, ok := args["limit"].(float64); ok {
		limit = int(v)
	}

	content, lineCount, err := e.ensureFileOps(sandboxID).ReadFile(ctx, path, offset, limit)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"content":    content,
		"lineCount":  lineCount,
		"file_path":  path,
	}, nil
}

func (e *SandboxToolExecutor) executeFileWrite(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	path, _ := args["file_path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return nil, fmt.Errorf("missing 'file_path' argument")
	}
	if content == "" {
		return nil, fmt.Errorf("missing 'content' argument")
	}

	if err := e.ensureFileOps(sandboxID).WriteFile(ctx, path, content); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success":   true,
		"file_path": path,
		"size":      len(content),
	}, nil
}

func (e *SandboxToolExecutor) executeFileEdit(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	path, _ := args["file_path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	replaceAll := false
	if v, ok := args["replace_all"].(bool); ok {
		replaceAll = v
	}

	if path == "" || oldString == "" {
		return nil, fmt.Errorf("missing 'file_path' or 'old_string' argument")
	}

	count, err := e.ensureFileOps(sandboxID).EditFile(ctx, path, oldString, newString, replaceAll)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success":      true,
		"file_path":    path,
		"replacements": count,
	}, nil
}

func (e *SandboxToolExecutor) executeFileGrep(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("missing 'pattern' argument")
	}
	path, _ := args["path"].(string)
	glob, _ := args["glob"].(string)
	outputMode, _ := args["output_mode"].(string)
	if outputMode == "" {
		outputMode = "content"
	}
	var contextLines int
	if v, ok := args["context_lines"].(float64); ok {
		contextLines = int(v)
	}
	caseInsensitive := false
	if v, ok := args["case_insensitive"].(bool); ok {
		caseInsensitive = v
	}
	var headLimit int
	if v, ok := args["head_limit"].(float64); ok {
		headLimit = int(v)
	}

	result, err := e.ensureFileOps(sandboxID).Grep(ctx, pattern, path, glob, outputMode, contextLines, caseInsensitive, headLimit)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success": true,
		"pattern": pattern,
		"results": result,
	}, nil
}

func (e *SandboxToolExecutor) executeFileGlob(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, fmt.Errorf("missing 'pattern' argument")
	}
	path, _ := args["path"].(string)

	files, err := e.ensureFileOps(sandboxID).Glob(ctx, pattern, path)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success": true,
		"pattern": pattern,
		"files":   files,
		"count":   len(files),
	}, nil
}

func (e *SandboxToolExecutor) executeCodeSearch(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	operation, _ := args["operation"].(string)
	if operation == "" {
		return nil, fmt.Errorf("missing 'operation' argument (use: find_references, find_definition, list_symbols, hover)")
	}
	symbol, _ := args["symbol"].(string)
	path, _ := args["path"].(string)
	fileType, _ := args["file_type"].(string)

	ops := NewCodeSearchOps(e.ensureFileOps(sandboxID))
	result, err := ops.Search(ctx, operation, symbol, path, fileType)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success":   true,
		"operation": operation,
		"symbol":    symbol,
		"results":   result,
	}, nil
}
