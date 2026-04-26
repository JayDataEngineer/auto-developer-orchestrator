package llama

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// ── Browser macro tools ─────────────────────────────────────────────

// macroBrowseTo ensures browser running, navigates to URL, returns page info.
func (e *SandboxToolExecutor) macroBrowseTo(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return nil, fmt.Errorf("missing 'url' argument for browse_to")
	}

	if err := e.ensureBrowserReady(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	navResult, err := e.CU.Act(ctx, sandboxID, "navigate", map[string]interface{}{"action": "navigate", "url": rawURL})
	if err != nil {
		return nil, fmt.Errorf("failed to navigate to %s: %w", rawURL, err)
	}

	// Wait 500ms for page state stabilization (from Google CUA pattern)
	time.Sleep(500 * time.Millisecond)

	resp := map[string]interface{}{
		"success":      true,
		"navigated_to": rawURL,
		"page_summary": e.extractPageSummary(navResult),
	}

	if vision := e.visionSummary(ctx, sandboxID); vision != "" {
		resp["vision"] = vision
	}

	return resp, nil
}

// macroReadPage returns current page elements and info.
func (e *SandboxToolExecutor) macroReadPage(ctx context.Context, sandboxID string) (interface{}, error) {
	result, err := e.CU.Snapshot(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("failed to read page: %w", err)
	}

	resp := map[string]interface{}{
		"success":      true,
		"page_summary": e.extractPageSummary(result),
	}

	if vision := e.visionSummary(ctx, sandboxID); vision != "" {
		resp["vision"] = vision
	}

	return resp, nil
}

// macroObserve combines screenshot + DOM snapshot + vision description in one call.
// Stagehand pattern: observe the page state without acting.
func (e *SandboxToolExecutor) macroObserve(ctx context.Context, sandboxID string) (interface{}, error) {
	snapshotResult, err := e.CU.Snapshot(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("observe: snapshot failed: %w", err)
	}

	resp := map[string]interface{}{
		"success":      true,
		"page_summary": e.extractPageSummary(snapshotResult),
	}

	screenshotResult, err := e.CU.Screenshot(ctx, sandboxID, true)
	if err == nil {
		if url, ok := screenshotResult["url"]; ok {
			resp["screenshot_url"] = url
		}
		if desc, ok := screenshotResult["description"]; ok {
			resp["vision"] = desc
		}
	}

	if _, ok := resp["vision"]; !ok {
		if vision := e.visionSummary(ctx, sandboxID); vision != "" {
			resp["vision"] = vision
		}
	}

	return resp, nil
}

// macroClickElement clicks an element by ID and returns updated page info.
func (e *SandboxToolExecutor) macroClickElement(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	normalizeElementArg(args)
	elementID, err := e.resolveElement(args)
	if err != nil {
		if healedID, healErr := e.selfHealElement(ctx, sandboxID, 0, fmt.Sprintf("%v", args["element"])); healErr == nil {
			elementID = healedID
			e.Logger.Debug("Self-healed element for click", zap.Int("newID", healedID))
		} else {
			return nil, fmt.Errorf("click_element: %w (self-heal also failed: %v)", err, healErr)
		}
	}

	clickArgs := map[string]interface{}{"action": "click", "element": elementID}
	result, err := e.CU.Act(ctx, sandboxID, "click", clickArgs)
	if err != nil {
		if healedID, healErr := e.selfHealElement(ctx, sandboxID, elementID, ""); healErr == nil {
			clickArgs["element"] = healedID
			result, err = e.CU.Act(ctx, sandboxID, "click", clickArgs)
			if err != nil {
				return nil, fmt.Errorf("failed to click element %d (self-healed to %d): %w", elementID, healedID, err)
			}
			elementID = healedID
			e.Logger.Debug("Self-healed click after Act failure", zap.Int("oldID", elementID), zap.Int("newID", healedID))
		} else {
			return nil, fmt.Errorf("failed to click element %d: %w", elementID, err)
		}
	}

	resp := map[string]interface{}{
		"success":          true,
		"clicked_element":  elementID,
		"page_after_click": e.extractPageSummary(result),
	}

	// Cache successful element resolution (Stagehand pattern)
	if desc, ok := args["element"].(string); ok && desc != "" {
		e.cacheSave(sandboxID, desc, elementID)
	}

	if vision := e.visionSummary(ctx, sandboxID); vision != "" {
		resp["vision"] = vision
	}

	return resp, nil
}

// macroTypeText types text into an element, optionally submitting.
func (e *SandboxToolExecutor) macroTypeText(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	text, _ := args["text"].(string)
	if text == "" {
		return nil, fmt.Errorf("missing 'text' argument for type_text")
	}

	normalizeElementArg(args)
	elementID, err := e.resolveElement(args)
	if err != nil {
		if healedID, healErr := e.selfHealElement(ctx, sandboxID, 0, fmt.Sprintf("%v", args["element"])); healErr == nil {
			elementID = healedID
			e.Logger.Debug("Self-healed element for type_text", zap.Int("newID", healedID))
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
		if healedID, healErr := e.selfHealElement(ctx, sandboxID, elementID, text); healErr == nil {
			typeArgs["element"] = healedID
			result, err = e.CU.Act(ctx, sandboxID, "type", typeArgs)
			if err != nil {
				return nil, fmt.Errorf("failed to type into element %d (self-healed to %d): %w", elementID, healedID, err)
			}
			elementID = healedID
			e.Logger.Debug("Self-healed type_text after Act failure", zap.Int("newID", healedID))
		} else {
			return nil, fmt.Errorf("failed to type into element %d: %w", elementID, err)
		}
	}

	resp := map[string]interface{}{
		"success":      true,
		"typed_text":   text,
		"into_element": elementID,
		"submitted":    submit,
		"page_summary": e.extractPageSummary(result),
	}
	// Cache successful element resolution (Stagehand pattern)
	if desc, ok := args["element"].(string); ok && desc != "" {
		e.cacheSave(sandboxID, desc, elementID)
	}
	if vision := e.visionSummary(ctx, sandboxID); vision != "" {
		resp["vision"] = vision
	}
	return resp, nil
}

// macroSearchWeb searches via MCP research server, falling back to browser Google.
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

	if e.MCPClient != nil {
		result, err := e.MCPClient.Research(ctx, query, 3)
		if err == nil && result != "" {
			e.Logger.Info("search via MCP research server", zap.String("query", query))
			return map[string]interface{}{
				"success": true, "query": query, "source": "mcp_research", "results": result,
			}, nil
		}
		e.Logger.Warn("MCP research failed, falling back to browser search", zap.Error(err))
	}

	if e.CU == nil {
		return nil, fmt.Errorf("search unavailable: MCP research failed and no browser bridge (cloud-only mode)")
	}

	searchURL := fmt.Sprintf("https://www.google.com/search?q=%s", url.QueryEscape(query))
	browserCtx, browserCancel := context.WithTimeout(ctx, 30*time.Second)
	defer browserCancel()

	if err := e.ensureBrowserReady(browserCtx, sandboxID); err != nil {
		return nil, fmt.Errorf("search unavailable: MCP timed out and browser not ready: %w", err)
	}

	navResult, err := e.CU.Act(browserCtx, sandboxID, "navigate", map[string]interface{}{"action": "navigate", "url": searchURL})
	if err != nil {
		return nil, fmt.Errorf("search failed (MCP timed out, browser also failed): %w", err)
	}

	resp := map[string]interface{}{
		"success":      true,
		"query":        query,
		"source":       "browser_google",
		"search_url":   searchURL,
		"page_summary": e.extractPageSummary(navResult),
	}

	if vision := e.visionSummary(browserCtx, sandboxID); vision != "" {
		resp["vision"] = vision
	}

	return resp, nil
}

// macroScrape fetches a URL and returns its content as clean markdown via MCP.
func (e *SandboxToolExecutor) macroScrape(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	targetURL, _ := args["url"].(string)
	if targetURL == "" {
		return nil, fmt.Errorf("missing 'url' argument for scrape")
	}

	if e.scrapedURLs == nil {
		e.scrapedURLs = make(map[string]string)
	}
	if cached, ok := e.scrapedURLs[targetURL]; ok {
		e.Logger.Info("scrape cache hit", zap.String("url", targetURL))
		return map[string]interface{}{"success": true, "url": targetURL, "source": "cache", "content": cached}, nil
	}

	if e.MCPClient != nil {
		result, err := e.MCPClient.Scrape(ctx, targetURL)
		if err == nil && result != "" {
			e.Logger.Info("scrape via MCP server", zap.String("url", targetURL))
			e.scrapedURLs[targetURL] = result
			return map[string]interface{}{"success": true, "url": targetURL, "source": "mcp_scrape", "content": result}, nil
		}
		e.Logger.Warn("MCP scrape failed, trying browser fallback", zap.Error(err))
	}

	if sandboxID != "" {
		text, err := e.browserScrapeFallback(ctx, sandboxID, targetURL)
		if err == nil && text != "" {
			e.Logger.Info("scrape via browser fallback", zap.String("url", targetURL))
			e.scrapedURLs[targetURL] = text
			return map[string]interface{}{"success": true, "url": targetURL, "source": "browser_fallback", "content": text}, nil
		}
		e.Logger.Warn("browser scrape fallback also failed", zap.Error(err))
	}

	return nil, fmt.Errorf("scrape failed: MCP server unavailable and no browser fallback")
}

// browserScrapeFallback uses the sandbox browser to navigate and extract text content.
func (e *SandboxToolExecutor) browserScrapeFallback(ctx context.Context, sandboxID, targetURL string) (string, error) {
	if e.CU == nil {
		return "", fmt.Errorf("no computer use handler available")
	}
	if err := e.ensureBrowserReady(ctx, sandboxID); err != nil {
		return "", fmt.Errorf("failed to start browser: %w", err)
	}

	_, err := e.CU.Act(ctx, sandboxID, "navigate", map[string]interface{}{"action": "navigate", "url": targetURL})
	if err != nil {
		return "", fmt.Errorf("navigate failed: %w", err)
	}

	if e.MCPClient != nil {
		rawHTML, err := e.CU.ExtractPageContent(ctx, sandboxID, true)
		if err == nil && rawHTML != "" {
			clean, mcpErr := e.MCPClient.ProcessHTML(ctx, rawHTML)
			if mcpErr == nil && clean != "" {
				e.Logger.Info("scrape via browser + MCP process_html", zap.String("url", targetURL))
				return clean, nil
			}
			e.Logger.Warn("MCP process_html failed, falling back to innerText", zap.Error(mcpErr))
		}
	}

	text, err := e.CU.ExtractPageContent(ctx, sandboxID, false)
	if err != nil {
		return "", fmt.Errorf("extract content: %w", err)
	}
	if text == "" {
		return "", fmt.Errorf("page returned empty content")
	}
	return text, nil
}

// macroMCPCall is a generic passthrough to any tool on the MCP research server.
func (e *SandboxToolExecutor) macroMCPCall(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	toolName, _ := args["tool"].(string)
	if toolName == "" {
		return nil, fmt.Errorf("missing 'tool' argument for mcp_call")
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
	return map[string]interface{}{"success": true, "tool": toolName, "source": "mcp", "result": result}, nil
}

// ensureBrowserReady ensures the browser CDP client is connected and ready.
func (e *SandboxToolExecutor) ensureBrowserReady(ctx context.Context, sandboxID string) error {
	if e.CU.IsReady(sandboxID) {
		return nil
	}
	_, err := e.CU.Enable(ctx, sandboxID)
	if err != nil {
		return fmt.Errorf("enable failed: %w", err)
	}
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

// ShellEscape is forwarded from sandbox package for use in tool implementations.
var ShellEscape = sandbox.ShellEscape
