package browser

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ensureBrowserReady checks that a sandbox is available and auto-provisions
// the browser (Chrome/CDP) if the provider supports it. Returns nil if ready.
func ensureBrowserReady(ctx context.Context, provider BrowserProvider, sandboxID, toolName string) error {
	if sandboxID == "" {
		return core.NewToolError(toolName, "no sandbox available — Docker may be unavailable")
	}
	if ensurer, ok := provider.(SandboxEnsurer); ok {
		if err := ensurer.EnsureReady(ctx, sandboxID); err != nil {
			return core.NewToolError(toolName, fmt.Sprintf("browser not ready: %v", err))
		}
	}
	return nil
}

// ── Navigate Tool (BrowserProvider) ──

type NavigateProviderTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewNavigateProviderTool(p BrowserProvider, sandboxID func() string) *NavigateProviderTool {
	return &NavigateProviderTool{provider: p, sandboxID: sandboxID}
}

func (t *NavigateProviderTool) Name() string { return "browse_to" }
func (t *NavigateProviderTool) Description() string {
	return "Navigate the browser to a URL. Returns page title, URL, and a preview of page content. ALWAYS use this to navigate — do NOT use bash+curl."
}
func (t *NavigateProviderTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "The full URL to navigate to"}
		},
		"required": ["url"]
	}`)
}

func (t *NavigateProviderTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "browse_to"); err != nil {
		return nil, err
	}
	url, _ := args["url"].(string)
	if url == "" {
		return nil, core.NewToolError("browse_to", "missing required parameter 'url'")
	}
	return t.provider.Navigate(ctx, sbID, url)
}

// ── Find Element Tool ──

type FindElementTool struct {
	provider   BrowserProvider
	sandboxID  func() string // lazy resolution — sandbox may not exist at creation time
}

func NewFindElementTool(p BrowserProvider, sandboxID func() string) *FindElementTool {
	return &FindElementTool{provider: p, sandboxID: sandboxID}
}

func (t *FindElementTool) Name() string { return "find_element" }
func (t *FindElementTool) Description() string {
	return "Find a page element by semantic criteria (role, name, label, placeholder, CSS selector) and optionally click or type into it. Use this instead of SoM index numbers for more stable automation."
}

func (t *FindElementTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"role": {"type": "string", "description": "ARIA role: button, link, textbox, combobox, checkbox, etc."},
			"name": {"type": "string", "description": "Accessible name (substring match)"},
			"label": {"type": "string", "description": "aria-label or associated label text"},
			"text": {"type": "string", "description": "Visible text content"},
			"placeholder": {"type": "string", "description": "Placeholder attribute"},
			"selector": {"type": "string", "description": "CSS selector (direct lookup, bypasses a11y tree)"},
			"action": {"type": "string", "description": "Optional action: 'click' or 'type'. Omit for find-only."},
			"type_text": {"type": "string", "description": "Text to type (required when action=type)"},
			"submit": {"type": "boolean", "description": "Press Enter after typing"}
		}
	}`)
}

func (t *FindElementTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "find_element"); err != nil {
		return nil, err
	}

	req := map[string]interface{}{}
	for _, key := range []string{"role", "name", "label", "text", "placeholder", "selector", "action", "type_text"} {
		if v, ok := args[key].(string); ok && v != "" {
			req[key] = v
		}
	}
	if v, ok := args["submit"].(bool); ok {
		req["submit"] = v
	}

	return t.provider.FindElement(ctx, sbID, req)
}

// ── Browser Screenshot Tool ──

type BrowserScreenshotTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewBrowserScreenshotTool(p BrowserProvider, sandboxID func() string) *BrowserScreenshotTool {
	return &BrowserScreenshotTool{provider: p, sandboxID: sandboxID}
}

func (t *BrowserScreenshotTool) Name() string { return "browser_screenshot" }
func (t *BrowserScreenshotTool) Description() string {
	return "Take a screenshot of the current browser page. Returns a base64-encoded image that is automatically described by the vision system. Use this to visually verify page state, check layouts, or see what the user sees."
}
func (t *BrowserScreenshotTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *BrowserScreenshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "browser_screenshot"); err != nil {
		return nil, err
	}
	return t.provider.BrowserScreenshot(ctx, sbID)
}

// ── A11y Snapshot Tool ──

type A11ySnapshotTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewA11ySnapshotTool(p BrowserProvider, sandboxID func() string) *A11ySnapshotTool {
	return &A11ySnapshotTool{provider: p, sandboxID: sandboxID}
}

func (t *A11ySnapshotTool) Name() string { return "snapshot_a11y" }
func (t *A11ySnapshotTool) Description() string {
	return "Get the accessibility tree of the current page — lists all interactive elements with their ARIA role, name, and CSS selector. More stable than SoM labels across layout changes."
}

func (t *A11ySnapshotTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *A11ySnapshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "snapshot_a11y"); err != nil {
		return nil, err
	}
	return t.provider.A11ySnapshot(ctx, sbID)
}

// ── Cookie Tools ──

type GetCookiesTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewGetCookiesTool(p BrowserProvider, sandboxID func() string) *GetCookiesTool {
	return &GetCookiesTool{provider: p, sandboxID: sandboxID}
}

func (t *GetCookiesTool) Name() string { return "get_cookies" }
func (t *GetCookiesTool) Description() string {
	return "Get browser cookies for the current page or a specific URL"
}

func (t *GetCookiesTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "Optional URL to filter cookies"}
		}
	}`)
}

func (t *GetCookiesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "get_cookies"); err != nil {
		return nil, err
	}
	urls := []string{}
	if u, ok := args["url"].(string); ok && u != "" {
		urls = []string{u}
	}
	return t.provider.GetCookies(ctx, sbID, urls)
}

type SetCookieTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewSetCookieTool(p BrowserProvider, sandboxID func() string) *SetCookieTool {
	return &SetCookieTool{provider: p, sandboxID: sandboxID}
}

func (t *SetCookieTool) Name() string { return "set_cookie" }
func (t *SetCookieTool) Description() string {
	return "Set a browser cookie"
}

func (t *SetCookieTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Cookie name"},
			"value": {"type": "string", "description": "Cookie value"},
			"domain": {"type": "string", "description": "Cookie domain"},
			"path": {"type": "string", "description": "Cookie path (default /)"},
			"secure": {"type": "boolean", "description": "Secure flag"},
			"http_only": {"type": "boolean", "description": "HTTP-only flag"}
		},
		"required": ["name", "value", "domain"]
	}`)
}

func (t *SetCookieTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "set_cookie"); err != nil {
		return nil, err
	}
	cookie := map[string]interface{}{}
	for _, key := range []string{"name", "value", "domain", "path"} {
		if v, ok := args[key].(string); ok {
			cookie[key] = v
		}
	}
	if v, ok := args["secure"].(bool); ok {
		cookie["secure"] = v
	}
	if v, ok := args["http_only"].(bool); ok {
		cookie["http_only"] = v
	}
	return t.provider.SetCookie(ctx, sbID, cookie)
}

type ClearCookiesTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewClearCookiesTool(p BrowserProvider, sandboxID func() string) *ClearCookiesTool {
	return &ClearCookiesTool{provider: p, sandboxID: sandboxID}
}

func (t *ClearCookiesTool) Name() string { return "clear_cookies" }
func (t *ClearCookiesTool) Description() string {
	return "Clear all browser cookies"
}

func (t *ClearCookiesTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ClearCookiesTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "clear_cookies"); err != nil {
		return nil, err
	}
	return t.provider.ClearCookies(ctx, sbID)
}

// ── Storage Tools ──

type GetStorageTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewGetStorageTool(p BrowserProvider, sandboxID func() string) *GetStorageTool {
	return &GetStorageTool{provider: p, sandboxID: sandboxID}
}

func (t *GetStorageTool) Name() string { return "get_storage" }
func (t *GetStorageTool) Description() string {
	return "Get localStorage contents for the current page"
}

func (t *GetStorageTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *GetStorageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "get_storage"); err != nil {
		return nil, err
	}
	return t.provider.GetStorage(ctx, sbID)
}

type SetStorageTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewSetStorageTool(p BrowserProvider, sandboxID func() string) *SetStorageTool {
	return &SetStorageTool{provider: p, sandboxID: sandboxID}
}

func (t *SetStorageTool) Name() string { return "set_storage" }
func (t *SetStorageTool) Description() string {
	return "Set a localStorage item"
}

func (t *SetStorageTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"key": {"type": "string", "description": "Storage key"},
			"value": {"type": "string", "description": "Storage value"}
		},
		"required": ["key", "value"]
	}`)
}

func (t *SetStorageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "set_storage"); err != nil {
		return nil, err
	}
	key, _ := args["key"].(string)
	value, _ := args["value"].(string)
	if key == "" || value == "" {
		return nil, core.NewToolError("set_storage", "key and value are required")
	}
	return t.provider.SetStorage(ctx, sbID, key, value)
}

type ClearStorageTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewClearStorageTool(p BrowserProvider, sandboxID func() string) *ClearStorageTool {
	return &ClearStorageTool{provider: p, sandboxID: sandboxID}
}

func (t *ClearStorageTool) Name() string { return "clear_storage" }
func (t *ClearStorageTool) Description() string {
	return "Clear all localStorage data"
}

func (t *ClearStorageTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ClearStorageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "clear_storage"); err != nil {
		return nil, err
	}
	return t.provider.ClearStorage(ctx, sbID)
}

// RegisterBrowserTools creates all browser provider tool wrappers and appends to tools slice.
func RegisterBrowserTools(tools []core.Tool, p BrowserProvider, sandboxID func() string) []core.Tool {
	if p == nil {
		return tools
	}
	return append(tools,
		NewNavigateProviderTool(p, sandboxID),
		NewBrowserScreenshotTool(p, sandboxID),
		NewFindElementTool(p, sandboxID),
		NewA11ySnapshotTool(p, sandboxID),
		NewGetCookiesTool(p, sandboxID),
		NewSetCookieTool(p, sandboxID),
		NewClearCookiesTool(p, sandboxID),
		NewGetStorageTool(p, sandboxID),
		NewSetStorageTool(p, sandboxID),
		NewClearStorageTool(p, sandboxID),
		NewEvaluateJSTool(p, sandboxID),
		NewReadPageProviderTool(p, sandboxID),
		NewDownloadFileTool(p, sandboxID),
	)
}

// ── Evaluate JS Tool ──

type EvaluateJSTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewEvaluateJSTool(p BrowserProvider, sandboxID func() string) *EvaluateJSTool {
	return &EvaluateJSTool{provider: p, sandboxID: sandboxID}
}

func (t *EvaluateJSTool) Name() string { return "evaluate_js" }
func (t *EvaluateJSTool) Description() string {
	return "Execute JavaScript in the browser and return the result. Use this to extract data from the page (e.g., image URLs, form values, DOM queries) that isn't available through other tools."
}
func (t *EvaluateJSTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"code": {"type": "string", "description": "JavaScript code to evaluate. Must be a single expression or IIFE. Example: \"Array.from(document.querySelectorAll('img')).map(i=>i.src)\""}
		},
		"required": ["code"]
	}`)
}

func (t *EvaluateJSTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "evaluate_js"); err != nil {
		return nil, err
	}
	code, _ := args["code"].(string)
	if code == "" {
		return nil, core.NewToolError("evaluate_js", "missing required parameter 'code'")
	}
	return t.provider.EvaluateJS(ctx, sbID, code)
}

// ── Read Page Tool ──

type ReadPageProviderTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewReadPageProviderTool(p BrowserProvider, sandboxID func() string) *ReadPageProviderTool {
	return &ReadPageProviderTool{provider: p, sandboxID: sandboxID}
}

func (t *ReadPageProviderTool) Name() string { return "read_page" }
func (t *ReadPageProviderTool) Description() string {
	return "Extract structured content from the current browser page: title, URL, visible text, images (with src and alt), and links (with text and URL). Use this instead of screenshots when you need page data for processing."
}
func (t *ReadPageProviderTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ReadPageProviderTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "read_page"); err != nil {
		return nil, err
	}
	return t.provider.ReadPage(ctx, sbID)
}

// ── Download File Tool ──

type DownloadFileTool struct {
	provider  BrowserProvider
	sandboxID func() string
}

func NewDownloadFileTool(p BrowserProvider, sandboxID func() string) *DownloadFileTool {
	return &DownloadFileTool{provider: p, sandboxID: sandboxID}
}

func (t *DownloadFileTool) Name() string { return "download_file" }
func (t *DownloadFileTool) Description() string {
	return "Download a file to the sandbox workspace. Returns the file path and size. Use this instead of bash+curl for downloading images, PDFs, or any file."
}
func (t *DownloadFileTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "URL of the file to download"},
			"path": {"type": "string", "description": "Destination path in the sandbox (default: /sandbox/workspace/<filename>)"}
		},
		"required": ["url"]
	}`)
}

func (t *DownloadFileTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if err := ensureBrowserReady(ctx, t.provider, sbID, "download_file"); err != nil {
		return nil, err
	}
	url, _ := args["url"].(string)
	if url == "" {
		return nil, core.NewToolError("download_file", "missing required parameter 'url'")
	}
	path, _ := args["path"].(string)
	return t.provider.DownloadFile(ctx, sbID, url, path)
}
