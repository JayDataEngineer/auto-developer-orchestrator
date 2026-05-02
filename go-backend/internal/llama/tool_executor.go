package llama

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// normalizeToolName normalizes common tool name aliases to canonical names.
func normalizeToolName(name string, args map[string]interface{}) string {
	if strings.HasPrefix(name, "mcp_") {
		return name
	}
	if strings.HasPrefix(name, "app_") {
		return name
	}
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
	case "scroll", "browser_scroll", "scroll_down", "scroll_up":
		if _, ok := args["action"]; !ok {
			args["action"] = "scroll"
		}
		return "scroll_page"
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
	case "undo", "undo_edit", "revert", "revert_edit":
		return "undo_edit"
	case "grep", "search", "search_files", "rg":
		return "file_grep"
	case "glob", "find_files", "list_files", "ls_files":
		return "file_glob"
	case "code_search", "find_references", "find_definition", "list_symbols":
		return "code_search"
	}
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
	if strings.Contains(lower, "snapshot") || strings.Contains(lower, "page_elements") {
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

// normalizeElementArg finds the element ID in args using any key containing
// "element" and normalizes it to args["element"].
func normalizeElementArg(args map[string]interface{}) {
	for _, k := range []string{"element", "element_id", "id", "ref", "target"} {
		if v, ok := args[k]; ok {
			args["element"] = v
			return
		}
	}
	for k, v := range args {
		if strings.Contains(strings.ToLower(k), "element") {
			args["element"] = v
			return
		}
	}
}

// Execute runs a tool in the sandbox and returns the result.
func (e *SandboxToolExecutor) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	sandboxID := e.SandboxID
	if e.Manager != nil {
		if sb := e.Manager.FindSandboxByProject(sandboxID); sb != nil {
			sandboxID = sb.ID
		}
	}

	if !e.credsLoaded && e.Manager != nil {
		e.credsLoaded = true
		if output, err := e.Manager.ExecInSandbox(ctx, sandboxID, []string{"cat", "/sandbox/workspace/passwords.txt"}); err == nil {
			if creds := LoadFromText(output); !creds.IsEmpty() {
				e.Creds = creds
				e.Logger.Info("Loaded credentials from sandbox", zap.String("sandbox", sandboxID))
			}
		}
	}

	if e.Creds != nil && !e.Creds.IsEmpty() {
		args = e.Creds.Resolve(args)
	}

	toolName = normalizeToolName(toolName, args)

	if toolName == "bash" {
		cmd, _ := args["command"].(string)
		if cmd == "" {
			cmd, _ = args["code"].(string)
		}
		if cmd == "" {
			cmd, _ = args["cmd"].(string)
		}
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

	if toolName == "ask_user" {
		question, _ := args["question"].(string)
		if question == "" {
			return nil, fmt.Errorf("missing 'question' argument")
		}
		if e.ApprovalMgr == nil {
			return nil, fmt.Errorf("ask_user is not available in non-interactive sessions")
		}
		requestID := fmt.Sprintf("ask-%d", time.Now().UnixMilli())
		subscriber := SubscriberFromContext(ctx)
		if subscriber != nil {
			sendEvent(subscriber, AgentEvent{
				Type: EventTypeApprovalRequest,
				Data: AgentEventData{
					ToolName: "ask_user",
					ToolID:   requestID,
					ToolArgs: map[string]interface{}{"question": question},
					Result:   map[string]interface{}{"requestId": requestID, "type": "question", "message": question},
				},
			})
		}
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

	if e.CU == nil {
		return nil, fmt.Errorf("computer use not available: %s", toolName)
	}

	switch toolName {
	case "browse_to":
		return e.macroBrowseTo(ctx, sandboxID, args)
	case "read_page":
		return e.macroReadPage(ctx, sandboxID)
	case "click_element":
		return e.macroClickElement(ctx, sandboxID, args)
	case "type_text":
		return e.macroTypeText(ctx, sandboxID, args)
	case "search_web":
		return e.macroSearchWeb(ctx, sandboxID, args)
	case "scrape":
		return e.macroScrape(ctx, sandboxID, args)
	case "mcp_call":
		return e.macroMCPCall(ctx, args)
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
		return e.cuAct(ctx, sandboxID, args)
	case "desktop_screenshot":
		return e.CU.DesktopScreenshot(ctx, sandboxID)
	case "desktop_click":
		return e.desktopClick(ctx, sandboxID, args)
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
	case "observe":
		return e.macroObserve(ctx, sandboxID)
	case "wait":
		seconds := 2
		if s, ok := args["seconds"]; ok {
			if f, ok := s.(float64); ok && f > 0 && f <= 30 {
				seconds = int(f)
			}
		}
		time.Sleep(time.Duration(seconds) * time.Second)
		return map[string]interface{}{"output": fmt.Sprintf("Waited %d seconds", seconds)}, nil
	case "scroll_page":
		direction := "down"
		if d, ok := args["direction"].(string); ok {
			direction = d
		}
		return e.CU.Act(ctx, sandboxID, "scroll", map[string]interface{}{"action": "scroll", "direction": direction})
	case "file_read":
		return e.executeFileRead(ctx, sandboxID, args)
	case "file_write":
		return e.executeFileWrite(ctx, sandboxID, args)
	case "file_edit":
		return e.executeFileEdit(ctx, sandboxID, args)
	case "undo_edit":
		return e.executeUndoEdit(ctx, sandboxID, args)
	case "file_grep":
		return e.executeFileGrep(ctx, sandboxID, args)
	case "file_glob":
		return e.executeFileGlob(ctx, sandboxID, args)
	case "code_search":
		return e.executeCodeSearch(ctx, sandboxID, args)
	case "image_read":
		return e.executeImageRead(ctx, sandboxID, args)
	case "http_request":
		return e.executeHTTPRequest(ctx, sandboxID, args)
	case "create_tool":
		return e.executeCreateTool(ctx, sandboxID, args)
	case "list_tools":
		return e.executeListTools(ctx, sandboxID)
	case "run_tool":
		return e.executeRunTool(ctx, sandboxID, args)
	default:
		if strings.HasPrefix(toolName, "mcp_") {
			return e.dispatchMCPTool(ctx, toolName, args)
		}
		if strings.HasPrefix(toolName, "app_") {
			return e.dispatchAppTool(ctx, sandboxID, toolName, args)
		}
		return nil, fmt.Errorf("unsupported tool: %s", toolName)
	}
}

// cuAct handles the computer_use_act tool with raw JSON fallback.
func (e *SandboxToolExecutor) cuAct(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	action, _ := args["action"].(string)
	if action == "" {
		if raw, ok := args["raw"].(string); ok {
			action = extractJSONStringValue(raw, "action")
		}
	}
	if action == "" {
		return nil, fmt.Errorf("missing 'action' argument")
	}
	if _, ok := args["url"]; !ok {
		if raw, ok := args["raw"].(string); ok {
			if u := extractJSONStringValue(raw, "url"); u != "" {
				args["url"] = u
			}
		}
	}
	return e.CU.Act(ctx, sandboxID, action, args)
}

// desktopClick handles desktop_click with coordinate normalization (CUA pattern).
func (e *SandboxToolExecutor) desktopClick(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	x, _ := args["x"].(float64)
	y, _ := args["y"].(float64)
	button := 1
	if b, ok := args["button"]; ok {
		if f, ok := b.(float64); ok {
			button = int(f)
		}
	}
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
					zap.Int("screenW", screenW), zap.Int("screenH", screenH))
			}
		}
	}
	return e.CU.DesktopClick(ctx, sandboxID, x, y, button)
}

// dispatchMCPTool handles a dynamically-registered MCP tool call.
func (e *SandboxToolExecutor) dispatchMCPTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	mcpName := strings.TrimPrefix(toolName, "mcp_")

	// Try multi-client first (routes to correct server)
	if e.MCPMulti != nil && e.MCPMulti.HasTool(mcpName) {
		result, err := e.MCPMulti.CallTool(ctx, mcpName, args)
		if err != nil {
			e.Logger.Warn("MCP multi tool call failed", zap.String("tool", mcpName), zap.Error(err))
			return nil, fmt.Errorf("MCP %s failed: %w", mcpName, err)
		}
		e.Logger.Info("MCP multi tool call succeeded", zap.String("tool", mcpName))
		return map[string]interface{}{
			"success": true,
			"tool":    mcpName,
			"source":  "mcp",
			"result":  result,
		}, nil
	}

	// Fallback to single client
	if e.MCPClient == nil {
		return nil, fmt.Errorf("MCP tool %s failed: MCP client not configured", toolName)
	}
	result, err := e.MCPClient.CallTool(ctx, mcpName, args)
	if err != nil {
		e.Logger.Warn("MCP tool call failed", zap.String("tool", mcpName), zap.Error(err))
		return nil, fmt.Errorf("MCP %s failed: %w", mcpName, err)
	}
	e.Logger.Info("MCP tool call succeeded", zap.String("tool", mcpName))
	return map[string]interface{}{
		"success": true,
		"tool":    mcpName,
		"source":  "mcp",
		"result":  result,
	}, nil
}

// dispatchAppTool handles a dynamically-registered app tool call.
// It resolves the handler template, substitutes parameters from args,
// and executes the command in the current sandbox.
func (e *SandboxToolExecutor) dispatchAppTool(ctx context.Context, sandboxID, toolName string, args map[string]interface{}) (interface{}, error) {
	reg := LookupAppTool(toolName)
	if reg == nil {
		return nil, fmt.Errorf("app tool %s not found in registry", toolName)
	}

	// Resolve the handler template with args
	cmd := resolveHandlerTemplate(reg.Handler, args)

	e.Logger.Info("Dispatching app tool",
		zap.String("tool", toolName),
		zap.String("project", reg.ProjectName),
		zap.String("command", cmd),
	)

	// Execute in the current sandbox
	if e.Manager == nil {
		return nil, fmt.Errorf("app tool %s failed: sandbox manager not available", toolName)
	}

	output, err := e.Manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", cmd})
	if err != nil {
		e.Logger.Warn("App tool execution failed",
			zap.String("tool", toolName),
			zap.String("command", cmd),
			zap.Error(err),
		)
		return nil, fmt.Errorf("app tool %s failed: %w", toolName, err)
	}

	e.Logger.Info("App tool succeeded",
		zap.String("tool", toolName),
		zap.Int("output_len", len(output)),
	)

	return map[string]interface{}{
		"success": true,
		"tool":    toolName,
		"source":  "app",
		"project": reg.ProjectName,
		"output":  output,
	}, nil
}

// ── Element resolution helpers ──────────────────────────────────────

// indexedElement stores a page element for description-based ID lookup.
type indexedElement struct {
	ID   int
	Tag  string
	Text string
	Zone string
}

// resolveElement resolves an element ID from the model's args.
// Handles numeric IDs and description-based lookups (text, zone, tag matching).
func (e *SandboxToolExecutor) resolveElement(args map[string]interface{}) (int, error) {
	if id, ok := args["element"].(float64); ok && id > 0 {
		return int(id), nil
	}
	if id, ok := args["element"].(int); ok && id > 0 {
		return id, nil
	}
	var desc string
	for _, k := range []string{"element", "element_id", "element_description", "element_desc",
		"text_element_description", "target_element", "target", "selector"} {
		if v, ok := args[k].(string); ok && v != "" {
			desc = v
			break
		}
	}
	if desc == "" {
		return 0, fmt.Errorf("no element ID or description provided")
	}
	if id, err := strconv.Atoi(strings.TrimSpace(desc)); err == nil && id > 0 {
		return id, nil
	}
	elems, ok := e.elemIndex[e.SandboxID]
	if !ok || len(elems) == 0 {
		return 0, fmt.Errorf("no page elements indexed yet — navigate to a page first")
	}

	// Check action cache: if we've successfully clicked this description before,
	// return the cached element ID (Stagehand pattern).
	cacheKey := cacheKey(e.SandboxID, desc)
	if e.actionCache == nil {
		e.actionCache = make(map[string]int)
	}
	if cachedID, ok := e.actionCache[cacheKey]; ok {
		// Verify the cached element still exists in the current snapshot
		for _, el := range elems {
			if el.ID == cachedID {
				return cachedID, nil
			}
		}
		// Stale cache entry — element no longer on page
		delete(e.actionCache, cacheKey)
	}

	descLower := strings.ToLower(desc)
	bestID := 0
	bestScore := 0
	for _, el := range elems {
		score := 0
		elLower := strings.ToLower(el.Text)
		if elLower == descLower {
			score = 100
		} else if strings.Contains(descLower, elLower) || strings.Contains(elLower, descLower) {
			score = 50
		}
		if el.Zone != "" && strings.Contains(descLower, el.Zone) {
			score += 20
		}
		if strings.Contains(descLower, el.Tag) {
			score += 10
		}
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
func (e *SandboxToolExecutor) selfHealElement(ctx context.Context, sandboxID string, originalID int, hint string) (int, error) {
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
	snapResult, err := e.CU.Snapshot(ctx, sandboxID)
	if err != nil {
		return 0, fmt.Errorf("self-heal: re-snapshot failed: %w", err)
	}
	_ = e.extractPageSummary(snapResult) // indexes elements as side effect
	newElems, ok := e.elemIndex[sandboxID]
	if !ok || len(newElems) == 0 {
		return 0, fmt.Errorf("self-heal: no elements in fresh snapshot")
	}
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
		// Position scoring: elements at similar Y position are more likely replacements
		if origTag != "" && el.Zone != "" {
			zoneMatch := false
			for _, origEl := range e.elemIndex[sandboxID] {
				if origEl.ID == originalID && origEl.Zone == el.Zone {
					zoneMatch = true
					break
				}
			}
			if zoneMatch {
				score += 15
			}
		}
		if score > bestScore {
			bestScore = score
			bestID = el.ID
		}
	}
	if bestID > 0 && bestScore >= 30 {
		// Cache the healed element for future lookups (Stagehand pattern)
		if hint != "" {
			e.cacheSave(sandboxID, hint, bestID)
		}
		return bestID, nil
	}
	return 0, fmt.Errorf("self-heal: no similar element found (best score: %d)", bestScore)
}

// ── Action caching (Stagehand pattern) ────────────────────────────────

// cacheKey creates a hash key from sandboxID and description.
func cacheKey(sandboxID, desc string) string {
	h := sha256.Sum256([]byte(sandboxID + "|" + desc))
	return fmt.Sprintf("%x", h[:8])
}

// cacheSave stores a successful element resolution for future lookups.
func (e *SandboxToolExecutor) cacheSave(sandboxID, desc string, elementID int) {
	if e.actionCache == nil {
		e.actionCache = make(map[string]int)
	}
	e.actionCache[cacheKey(sandboxID, desc)] = elementID
}

// ensureSandbox resolves credentials and sandboxID lazily. Exported for file/tool helpers.
func (e *SandboxToolExecutor) ensureSandbox(ctx context.Context) (string, error) {
	sandboxID := e.SandboxID
	if e.Manager != nil {
		if sb := e.Manager.FindSandboxByProject(sandboxID); sb != nil {
			sandboxID = sb.ID
		}
	}
	if !e.credsLoaded && e.Manager != nil {
		e.credsLoaded = true
		if output, err := e.Manager.ExecInSandbox(ctx, sandboxID, []string{"cat", "/sandbox/workspace/passwords.txt"}); err == nil {
			if creds := LoadFromText(output); !creds.IsEmpty() {
				e.Creds = creds
				e.Logger.Info("Loaded credentials from sandbox", zap.String("sandbox", sandboxID))
			}
		}
	}
	return sandboxID, nil
}


