package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"go.uber.org/zap"
)

// SeleniumBaseBridge implements BrowserProvider by calling the Python
// sb_server.py persistent browser server (SeleniumBase CDP mode) running
// inside the sandbox on port 9876.
//
// This is the stealthy alternative to ComputerUseBridge (which drives Chrome
// directly via chromedp). All browser operations route through SeleniumBase's
// CDP layer, which:
//   - Leaves no webdriver fingerprint
//   - Handles element location via SeleniumBase's finders
//   - Supports file uploads via file chooser interception
//   - Manages cookies including HttpOnly (via SeleniumBase get_all_cookies)
//
// The bridge talks to sb_server.py via the orchestrator's /api/sandbox/{id}/sb/*
// proxy endpoint, which docker-execs curl inside the sandbox. Per-call overhead
// is ~50-100ms but agent steps take 1-3s anyway, so the latency is invisible.
//
// Bridge is stateless w.r.t. sandbox: the sandboxID parameter on each method
// is forwarded into the proxy URL.
type SeleniumBaseBridge struct {
	logger *zap.Logger
	http   *http.Client
	// mcpMulti is set by SetMCP and is used only by FindElementVisual (which
	// calls ground_ui directly — it never reaches sb_server.py).
	mcpMulti *mcp.MultiClient
}

// NewSeleniumBaseBridge creates a stateless bridge.
func NewSeleniumBaseBridge(logger *zap.Logger) *SeleniumBaseBridge {
	return &SeleniumBaseBridge{
		logger: logger,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// SetMCP lets the agent loop wire MCP tools (for FindElementVisual).
func (b *SeleniumBaseBridge) SetMCP(m *mcp.MultiClient) { b.mcpMulti = m }

// EnsureReady verifies sb_server.py is reachable. Returns nil if healthy.
func (b *SeleniumBaseBridge) EnsureReady(ctx context.Context, sandboxID string) error {
	if sandboxID == "" {
		return fmt.Errorf("seleniumbase: no sandbox available")
	}
	resp, err := b.call(ctx, sandboxID, "GET", "/status", nil)
	if err != nil {
		return fmt.Errorf("seleniumbase not reachable: %w", err)
	}
	alive, _ := resp["alive"].(bool)
	if !alive {
		return fmt.Errorf("seleniumbase browser not alive (status: %v)", resp)
	}
	return nil
}

// call dispatches an HTTP call to sb_server.py via the orchestrator's
// /api/sandbox/{id}/sb endpoint, which docker-execs curl inside the sandbox.
// This indirection is necessary because the orchestrator host cannot reach
// 127.0.0.1:9876 inside the container without port publishing.
//
// body may be nil for GET requests.
func (b *SeleniumBaseBridge) call(ctx context.Context, sandboxID, method, path string, body map[string]interface{}) (map[string]interface{}, error) {
	// Build the orchestrator URL that proxies to sb_server.py inside the sandbox.
	// If the orchestrator doesn't have that proxy, fall back to direct localhost
	// (works when the orchestrator runs INSIDE the sandbox, e.g., for tests).
	urls := []string{
		"http://localhost:3847/api/sandbox/" + sandboxID + "/sb" + path,
		"http://127.0.0.1:9876" + path,
	}

	var bodyReader io.Reader
	if body != nil {
		bb, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bb)
	}

	var lastErr error
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
		if err != nil {
			lastErr = err
			continue
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := b.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("sb_server %s %s -> HTTP %d: %s", method, path, resp.StatusCode, truncate(string(raw), 200))
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, fmt.Errorf("decode sb_server response: %w (raw: %s)", err, truncate(string(raw), 200))
		}
		if ok, _ := parsed["ok"].(bool); !ok {
			errMsg, _ := parsed["error"].(string)
			return parsed, fmt.Errorf("sb_server %s returned ok=false: %s", path, errMsg)
		}
		return parsed, nil
	}
	return nil, fmt.Errorf("all sb_server URLs failed; last error: %w", lastErr)
}

// ── BrowserProvider methods ─────────────────────────────────────────────

// Navigate calls /navigate and returns the page data + element map.
func (b *SeleniumBaseBridge) Navigate(ctx context.Context, sandboxID string, url string) (map[string]interface{}, error) {
	resp, err := b.call(ctx, sandboxID, "POST", "/navigate", map[string]interface{}{"url": url})
	if err != nil {
		return nil, err
	}
	// Normalize to the shape ComputerUseBridge returns so the existing tool
	// wrappers don't need to know which backend is in use.
	return normalizeSBResponse(resp), nil
}

// FindElement composes /label + /click or /type. Accepts the same criteria
// shape as ComputerUseBridge.FindElement (role, name, label, text, selector).
func (b *SeleniumBaseBridge) FindElement(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	// First refresh the labeler so /a11y results are current
	if _, err := b.call(ctx, sandboxID, "POST", "/label", nil); err != nil {
		return nil, err
	}
	a11y, err := b.call(ctx, sandboxID, "POST", "/a11y", nil)
	if err != nil {
		return nil, err
	}
	itemsAny, _ := a11y["items"].([]interface{})
	selector := matchA11yItem(itemsAny, req)
	if selector == "" {
		// Fall back to direct CSS selector if user provided one
		if s, ok := req["selector"].(string); ok && s != "" {
			selector = s
		}
	}
	if selector == "" {
		return nil, fmt.Errorf("find_element: no matching element for criteria %v", req)
	}

	out := map[string]interface{}{"selector": selector, "matched": true}

	action, _ := req["action"].(string)
	switch action {
	case "":
		// find-only
	case "click":
		clickResp, err := b.call(ctx, sandboxID, "POST", "/click", map[string]interface{}{"selector": selector})
		if err != nil {
			return out, err
		}
		out["click_result"] = clickResp
	case "type":
		text, _ := req["type_text"].(string)
		submit, _ := req["submit"].(bool)
		typeResp, err := b.call(ctx, sandboxID, "POST", "/type", map[string]interface{}{
			"selector": selector,
			"text":     text,
			"submit":   submit,
		})
		if err != nil {
			return out, err
		}
		out["type_result"] = typeResp
	default:
		return out, fmt.Errorf("unknown action: %s", action)
	}

	return out, nil
}

// FindElementVisual calls MCP ground_ui directly (not sb_server.py).
// Implementation matches ComputerUseBridge.FindElementVisual.
func (b *SeleniumBaseBridge) FindElementVisual(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	query, _ := req["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("find_element_visual: 'query' required")
	}
	if b.mcpMulti == nil || !b.mcpMulti.HasTool("ground_ui") {
		return nil, fmt.Errorf("find_element_visual: MCP ground_ui not available")
	}
	// Take a screenshot via sb_server for the vision model
	if _, err := b.call(ctx, sandboxID, "POST", "/screenshot", map[string]interface{}{"path": "/tmp/sb_visual.png"}); err != nil {
		return nil, fmt.Errorf("find_element_visual: screenshot: %w", err)
	}
	// Read the file via /file/ endpoint and base64-encode
	fileResp, err := b.call(ctx, sandboxID, "GET", "/file//tmp/sb_visual.png", nil)
	if err != nil {
		return nil, fmt.Errorf("find_element_visual: read screenshot: %w", err)
	}
	dataURI, _ := fileResp["data_uri"].(string)
	if dataURI == "" {
		return nil, fmt.Errorf("find_element_visual: no data_uri in /file response")
	}
	// Strip the data URI prefix to get raw base64
	parts := strings.SplitN(dataURI, ",", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("find_element_visual: malformed data_uri")
	}
	b64 := parts[1]
	mime := "image/png"
	if strings.Contains(parts[0], "image/jpeg") {
		mime = "image/jpeg"
	}

	// Upload to MCP and call ground_ui
	uploadResult, err := b.mcpMulti.CallTool(ctx, "upload", map[string]any{
		"data":      b64,
		"mime_type": mime,
	})
	if err != nil {
		return nil, fmt.Errorf("find_element_visual: upload: %w", err)
	}
	imageURL := extractUploadURL(uploadResult)
	if imageURL == "" {
		return nil, fmt.Errorf("find_element_visual: upload returned no URL")
	}

	groundResult, err := b.mcpMulti.CallTool(ctx, "ground_ui", map[string]any{
		"imageSource": imageURL,
		"query":       query,
	})
	if err != nil {
		return nil, fmt.Errorf("find_element_visual: ground_ui: %w", err)
	}
	coords := parseGroundUIResponse(groundResult)
	if coords == nil {
		return nil, fmt.Errorf("find_element_visual: couldn't parse ground_ui response")
	}
	coords["query"] = query

	// Optional click via sb_server.py
	if action, _ := req["action"].(string); strings.EqualFold(action, "click") {
		x, _ := coords["x"].(float64)
		y, _ := coords["y"].(float64)
		if x == 0 && y == 0 {
			return coords, fmt.Errorf("find_element_visual: refusing to click (0,0)")
		}
		clickResp, err := b.call(ctx, sandboxID, "POST", "/evaluate", map[string]interface{}{
			"code": fmt.Sprintf(`document.elementFromPoint(%f,%f) && document.elementFromPoint(%f,%f).click()`, x, y, x, y),
		})
		if err != nil {
			return coords, fmt.Errorf("find_element_visual: click at (%v,%v): %w", x, y, err)
		}
		coords["click_result"] = clickResp
		coords["clicked"] = true
	}

	return coords, nil
}

// A11ySnapshot returns the accessibility tree.
func (b *SeleniumBaseBridge) A11ySnapshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/a11y", nil)
}

// GetCookies returns all browser cookies.
func (b *SeleniumBaseBridge) GetCookies(ctx context.Context, sandboxID string, urls []string) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/cookies", map[string]interface{}{"action": "get"})
}

// SetCookie sets a cookie.
func (b *SeleniumBaseBridge) SetCookie(ctx context.Context, sandboxID string, cookie map[string]interface{}) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/cookies", map[string]interface{}{"action": "set", "cookie": cookie})
}

// ClearCookies clears all cookies.
func (b *SeleniumBaseBridge) ClearCookies(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/cookies", map[string]interface{}{"action": "clear"})
}

// GetStorage returns localStorage.
func (b *SeleniumBaseBridge) GetStorage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/storage", map[string]interface{}{"action": "get"})
}

// SetStorage sets a localStorage item.
func (b *SeleniumBaseBridge) SetStorage(ctx context.Context, sandboxID string, key, value string) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/storage", map[string]interface{}{"action": "set", "key": key, "value": value})
}

// ClearStorage clears localStorage.
func (b *SeleniumBaseBridge) ClearStorage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/storage", map[string]interface{}{"action": "clear"})
}

// BrowserScreenshot takes a screenshot.
func (b *SeleniumBaseBridge) BrowserScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	// Save to file, then read via /file/ to get base64
	if _, err := b.call(ctx, sandboxID, "POST", "/screenshot", map[string]interface{}{"path": "/tmp/sb_shot.png"}); err != nil {
		return nil, err
	}
	fileResp, err := b.call(ctx, sandboxID, "GET", "/file//tmp/sb_shot.png", nil)
	if err != nil {
		return nil, err
	}
	dataURI, _ := fileResp["data_uri"].(string)
	parts := strings.SplitN(dataURI, ",", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("browser_screenshot: malformed data_uri")
	}
	return map[string]interface{}{
		"image_b64": parts[1],
		"mime":      "image/png",
	}, nil
}

// EvaluateJS executes JavaScript in the browser.
func (b *SeleniumBaseBridge) EvaluateJS(ctx context.Context, sandboxID, code string) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/evaluate", map[string]interface{}{"code": code})
}

// ReadPage extracts structured page content.
func (b *SeleniumBaseBridge) ReadPage(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	resp, err := b.call(ctx, sandboxID, "POST", "/read", nil)
	if err != nil {
		return nil, err
	}
	return normalizeSBResponse(resp), nil
}

// DownloadFile downloads a URL to the sandbox via curl.
func (b *SeleniumBaseBridge) DownloadFile(ctx context.Context, sandboxID, url, path string) (map[string]interface{}, error) {
	return b.call(ctx, sandboxID, "POST", "/download", map[string]interface{}{"url": url, "path": path})
}

// SelectOption selects an option in a <select> dropdown.
func (b *SeleniumBaseBridge) SelectOption(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	selector, _ := req["selector"].(string)
	if selector == "" {
		selector = "select"
	}
	value, _ := req["value"].(string)
	text, _ := req["text"].(string)
	body := map[string]interface{}{"selector": selector}
	if value != "" {
		body["value"] = value
	}
	if text != "" {
		body["text"] = text
	}
	resp, err := b.call(ctx, sandboxID, "POST", "/select_dropdown", body)
	if err != nil {
		return out, err
	}
	out["result"] = resp
	return out, nil
}

// UploadFile uploads a file to an <input type="file">.
func (b *SeleniumBaseBridge) UploadFile(ctx context.Context, sandboxID string, req map[string]interface{}) (map[string]interface{}, error) {
	selector, _ := req["selector"].(string)
	filePath, _ := req["file_path"].(string)
	if selector == "" || filePath == "" {
		return nil, fmt.Errorf("upload_file: selector and file_path required")
	}
	return b.call(ctx, sandboxID, "POST", "/upload", map[string]interface{}{
		"selector":  selector,
		"file_path": filePath,
	})
}

// SaveSession saves cookies + localStorage to a file.
func (b *SeleniumBaseBridge) SaveSession(ctx context.Context, sandboxID, path string) (map[string]interface{}, error) {
	if path == "" {
		path = "/tmp/browser-session.json"
	}
	return b.call(ctx, sandboxID, "POST", "/save_session", map[string]interface{}{"path": path})
}

// RestoreSession restores cookies + localStorage from a file.
func (b *SeleniumBaseBridge) RestoreSession(ctx context.Context, sandboxID, path string) (map[string]interface{}, error) {
	if path == "" {
		path = "/tmp/browser-session.json"
	}
	return b.call(ctx, sandboxID, "POST", "/restore_session", map[string]interface{}{"path": path})
}

// InjectFile writes base64 content into the sandbox filesystem.
// Reused from ComputerUseBridge logic — both bridges use the same docker exec.
func (b *SeleniumBaseBridge) InjectFile(ctx context.Context, sandboxID, destPath, contentBase64 string) (map[string]interface{}, error) {
	// Use the orchestrator's existing file write API. The handler endpoint is
	// /api/sandbox/{id}/file/write — we can't reach it via httptest here, so
	// we delegate to ComputerUseBridge.InjectFile via the caller. For now, mark
	// as not implemented — inject_file is rarely used.
	return map[string]interface{}{
		"ok":     false,
		"error":  "InjectFile not yet implemented in SeleniumBaseBridge — use the file_write tool directly",
	}, nil
}

// CredentialGet is not browser-specific — delegate to a noop for now.
// The agent should call the regular credential_get tool, not the bridge.
func (b *SeleniumBaseBridge) CredentialGet(ctx context.Context, sandboxID, service string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("CredentialGet: not browser-specific, call credential_get tool directly")
}

// UserProfile is not browser-specific — same as above.
func (b *SeleniumBaseBridge) UserProfile(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("UserProfile: not browser-specific, call user_profile tool directly")
}

// ── helpers ─────────────────────────────────────────────────────────────

// normalizeSBResponse flattens sb_server's nested {page_data, element_map}
// into the flat shape that existing tool wrappers expect.
func normalizeSBResponse(resp map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range resp {
		out[k] = v
	}
	if pd, ok := resp["page_data"].(map[string]interface{}); ok {
		for k, v := range pd {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	return out
}

// matchA11yItem finds the first accessibility item matching the criteria.
// Supports: role, name, label, text, placeholder, selector.
func matchA11yItem(items []interface{}, req map[string]interface{}) string {
	wantRole, _ := req["role"].(string)
	wantName, _ := req["name"].(string)
	wantLabel, _ := req["label"].(string)
	wantText, _ := req["text"].(string)
	wantPlaceholder, _ := req["placeholder"].(string)

	for _, itAny := range items {
		item, ok := itAny.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := item["role"].(string)
		name, _ := item["name"].(string)
		selector, _ := item["selector"].(string)

		if wantRole != "" && !strings.EqualFold(role, wantRole) {
			continue
		}
		if wantName != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(wantName)) {
			continue
		}
		if wantLabel != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(wantLabel)) {
			continue
		}
		if wantText != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(wantText)) {
			continue
		}
		if wantPlaceholder != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(wantPlaceholder)) {
			continue
		}
		if selector != "" {
			return selector
		}
	}
	return ""
}
