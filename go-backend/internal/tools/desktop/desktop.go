package desktop

import (
	"context"
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Driver abstracts desktop/X11 automation.
type Driver interface {
	Screenshot(ctx context.Context) (*DesktopFrame, error)
	Click(ctx context.Context, x, y float64, button int) (*DesktopFrame, error)
	Type(ctx context.Context, text string) (*DesktopFrame, error)
	Key(ctx context.Context, key string) (*DesktopFrame, error)
	Resolution(ctx context.Context) (int, int, error)
}

// DesktopFrame represents the state of a desktop screenshot.
type DesktopFrame struct {
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	ImageB64 string `json:"image_b64,omitempty"`
	Error    string `json:"error,omitempty"`
}

// ScreenshotTool implements core.Tool for capturing desktop screenshots.
type ScreenshotTool struct {
	driver Driver
}

func NewScreenshotTool(d Driver) *ScreenshotTool {
	return &ScreenshotTool{driver: d}
}

func (t *ScreenshotTool) Name() string        { return "desktop_screenshot" }
func (t *ScreenshotTool) Description() string { return "Capture a screenshot of the X11 desktop" }

func (t *ScreenshotTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ScreenshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	frame, err := t.driver.Screenshot(ctx)
	if err != nil {
		return nil, err
	}
	return frame, nil
}

// ClickTool implements core.Tool for clicking at desktop coordinates.
type ClickTool struct {
	driver Driver
}

func NewClickTool(d Driver) *ClickTool {
	return &ClickTool{driver: d}
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
	x, _ := args["x"].(float64)
	y, _ := args["y"].(float64)
	button := 1
	if b, ok := args["button"].(float64); ok {
		button = int(b)
	}

	// Coordinate normalization (CUA pattern): if values are 0-1000, scale to screen resolution
	if x >= 0 && x <= 1000 && y >= 0 && y <= 1000 {
		if sw, sh, err := t.driver.Resolution(ctx); err == nil {
			x = x * float64(sw) / 1000.0
			y = y * float64(sh) / 1000.0
		}
	}

	frame, err := t.driver.Click(ctx, x, y, button)
	if err != nil {
		return nil, err
	}
	return frame, nil
}

// TypeTool implements core.Tool for typing text via desktop.
type TypeTool struct {
	driver Driver
}

func NewTypeTool(d Driver) *TypeTool {
	return &TypeTool{driver: d}
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
	text, _ := args["text"].(string)
	if text == "" {
		return nil, core.NewToolError("desktop_type", "missing required parameter 'text'")
	}
	frame, err := t.driver.Type(ctx, text)
	if err != nil {
		return nil, err
	}
	return frame, nil
}

// KeyTool implements core.Tool for pressing keys via desktop.
type KeyTool struct {
	driver Driver
}

func NewKeyTool(d Driver) *KeyTool {
	return &KeyTool{driver: d}
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
	key, _ := args["key"].(string)
	if key == "" {
		return nil, core.NewToolError("desktop_key", "missing required parameter 'key'")
	}
	frame, err := t.driver.Key(ctx, key)
	if err != nil {
		return nil, err
	}
	return frame, nil
}
