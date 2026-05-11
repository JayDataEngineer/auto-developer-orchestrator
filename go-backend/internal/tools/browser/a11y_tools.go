package browser

import (
	"context"
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

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
	if sbID == "" {
		return nil, core.NewToolError("find_element", "no sandbox available")
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
	if sbID == "" {
		return nil, core.NewToolError("snapshot_a11y", "no sandbox available")
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
	if sbID == "" {
		return nil, core.NewToolError("get_cookies", "no sandbox available")
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
	if sbID == "" {
		return nil, core.NewToolError("set_cookie", "no sandbox available")
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
	if sbID == "" {
		return nil, core.NewToolError("clear_cookies", "no sandbox available")
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
	if sbID == "" {
		return nil, core.NewToolError("get_storage", "no sandbox available")
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
	if sbID == "" {
		return nil, core.NewToolError("set_storage", "no sandbox available")
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
	if sbID == "" {
		return nil, core.NewToolError("clear_storage", "no sandbox available")
	}
	return t.provider.ClearStorage(ctx, sbID)
}

// RegisterBrowserTools creates all browser provider tool wrappers and appends to tools slice.
func RegisterBrowserTools(tools []core.Tool, p BrowserProvider, sandboxID func() string) []core.Tool {
	if p == nil {
		return tools
	}
	return append(tools,
		NewFindElementTool(p, sandboxID),
		NewA11ySnapshotTool(p, sandboxID),
		NewGetCookiesTool(p, sandboxID),
		NewSetCookieTool(p, sandboxID),
		NewClearCookiesTool(p, sandboxID),
		NewGetStorageTool(p, sandboxID),
		NewSetStorageTool(p, sandboxID),
		NewClearStorageTool(p, sandboxID),
	)
}
