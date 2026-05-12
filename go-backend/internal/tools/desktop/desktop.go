package desktop

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ── Screenshot Tool ────────────────────────────────────────────────────────

type ScreenshotTool struct {
	provider  DesktopProvider
	sandboxID func() string
}

func NewScreenshotTool(p DesktopProvider, sandboxID func() string) *ScreenshotTool {
	return &ScreenshotTool{provider: p, sandboxID: sandboxID}
}

func (t *ScreenshotTool) Name() string        { return "desktop_screenshot" }
func (t *ScreenshotTool) Description() string { return "Capture a screenshot of the X11 desktop" }

func (t *ScreenshotTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ScreenshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if sbID == "" {
		return nil, core.NewToolError("desktop_screenshot", "no sandbox available")
	}
	return t.provider.DesktopScreenshot(ctx, sbID)
}

// ── Click Tool ─────────────────────────────────────────────────────────────

type ClickTool struct {
	provider  DesktopProvider
	sandboxID func() string
}

func NewClickTool(p DesktopProvider, sandboxID func() string) *ClickTool {
	return &ClickTool{provider: p, sandboxID: sandboxID}
}

func (t *ClickTool) Name() string        { return "desktop_click" }
func (t *ClickTool) Description() string { return "Click at desktop coordinates (0-1000 normalized or raw pixels)" }

func (t *ClickTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"x": {"type": "number", "description": "X coordinate (0-1000 normalized)"},
			"y": {"type": "number", "description": "Y coordinate (0-1000 normalized)"},
			"button": {"type": "integer", "description": "Mouse button: 1=left, 2=middle, 3=right"}
		},
		"required": ["x", "y"]
	}`)
}

func (t *ClickTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if sbID == "" {
		return nil, core.NewToolError("desktop_click", "no sandbox available")
	}

	x, _ := args["x"].(float64)
	y, _ := args["y"].(float64)
	button := 1
	if b, ok := args["button"].(float64); ok {
		button = int(b)
	}

	// Coordinate normalization (CUA pattern): if values are 0-1000, scale to screen resolution
	if x >= 0 && x <= 1000 && y >= 0 && y <= 1000 {
		if res, err := t.provider.Resolution(ctx, sbID); err == nil {
			if sw := parseIntField(res, "width"); sw > 0 {
				if sh := parseIntField(res, "height"); sh > 0 {
					x = x * float64(sw) / 1000.0
					y = y * float64(sh) / 1000.0
				}
			}
		}
	}

	return t.provider.DesktopClick(ctx, sbID, x, y, button)
}

// ── Type Tool ──────────────────────────────────────────────────────────────

type TypeTool struct {
	provider  DesktopProvider
	sandboxID func() string
}

func NewTypeTool(p DesktopProvider, sandboxID func() string) *TypeTool {
	return &TypeTool{provider: p, sandboxID: sandboxID}
}

func (t *TypeTool) Name() string        { return "desktop_type" }
func (t *TypeTool) Description() string { return "Type text via the desktop (xdotool)" }

func (t *TypeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"text": {"type": "string", "description": "Text to type"}
		},
		"required": ["text"]
	}`)
}

func (t *TypeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if sbID == "" {
		return nil, core.NewToolError("desktop_type", "no sandbox available")
	}

	text, _ := args["text"].(string)
	if text == "" {
		return nil, core.NewToolError("desktop_type", "missing required parameter 'text'")
	}
	return t.provider.DesktopType(ctx, sbID, text)
}

// ── Key Tool ───────────────────────────────────────────────────────────────

type KeyTool struct {
	provider  DesktopProvider
	sandboxID func() string
}

func NewKeyTool(p DesktopProvider, sandboxID func() string) *KeyTool {
	return &KeyTool{provider: p, sandboxID: sandboxID}
}

func (t *KeyTool) Name() string        { return "desktop_key" }
func (t *KeyTool) Description() string { return "Press a key via the desktop (Return, Escape, ctrl+c, etc.)" }

func (t *KeyTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"key": {"type": "string", "description": "Key to press (Return, Escape, ctrl+c, alt+F4, etc.)"}
		},
		"required": ["key"]
	}`)
}

func (t *KeyTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if sbID == "" {
		return nil, core.NewToolError("desktop_key", "no sandbox available")
	}

	key, _ := args["key"].(string)
	if key == "" {
		return nil, core.NewToolError("desktop_key", "missing required parameter 'key'")
	}
	return t.provider.DesktopKey(ctx, sbID, key)
}

// ── Observe Tool ──────────────────────────────────────────────────────────

type ObserveTool struct {
	provider  DesktopProvider
	sandboxID func() string
}

func NewObserveTool(p DesktopProvider, sandboxID func() string) *ObserveTool {
	return &ObserveTool{provider: p, sandboxID: sandboxID}
}

func (t *ObserveTool) Name() string { return "desktop_observe" }
func (t *ObserveTool) Description() string {
	return "Capture screenshot with OCR element detection and window list. Returns image + elements (id, text, x, y, w, h, cx, cy) + windows. Use this instead of desktop_screenshot when you need to identify clickable elements."
}

func (t *ObserveTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ObserveTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sbID := t.sandboxID()
	if sbID == "" {
		return nil, core.NewToolError("desktop_observe", "no sandbox available")
	}
	return t.provider.DesktopObserve(ctx, sbID)
}

// ── Registration ───────────────────────────────────────────────────────────

// RegisterDesktopTools creates all desktop tool wrappers and appends to tools slice.
func RegisterDesktopTools(tools []core.Tool, p DesktopProvider, sandboxID func() string) []core.Tool {
	if p == nil {
		return tools
	}
	return append(tools,
		NewScreenshotTool(p, sandboxID),
		NewClickTool(p, sandboxID),
		NewTypeTool(p, sandboxID),
		NewKeyTool(p, sandboxID),
		NewObserveTool(p, sandboxID),
	)
}

// ── Helpers ────────────────────────────────────────────────────────────────

// parseIntField extracts an integer from a map[string]interface{} value.
// The X11 resolution handler returns string values, so this handles both string and float64.
func parseIntField(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case string:
		i, _ := strconv.Atoi(n)
		return i
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
