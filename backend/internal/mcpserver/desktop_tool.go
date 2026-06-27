// desktop_tool.go exposes the sandbox's X11 desktop (Xvfb on DISPLAY=:99,
// fluxbox wm, xdotool + scrot) as MCP tools. The sandbox already boots Xvfb
// + Chrome + supervisord services; these tools wrap the four operations an
// agent needs to drive arbitrary desktop apps:
//
//   - desktop_screenshot: capture screen + OCR-detected text elements + window list
//   - desktop_click:      mouse click at pixel coords (model picks from screenshot)
//   - desktop_type:       type text into the focused window (xdotool type)
//   - desktop_key:        press a key combo like "Return", "ctrl+c", "alt+Tab"
//
// Why pixel coords for click (not text-based search)? Two reasons:
// (1) OCR text positions are non-deterministic across runs — clicking "by
//     text" via a cached index would drift. Pixel coords from the latest
//     screenshot are stable.
// (2) Icons, checkboxes, and other non-text UI have no text label at all.
//     The model needs to click by visual position regardless.
//
// The pattern is: call desktop_screenshot, look at the returned elements
// (each has cx/cy center coords), pass those coords to desktop_click.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// displayEnv is the X11 display the sandbox uses. Set as an env-prefix on
// every xdotool command so the tools work even if the exec path doesn't
// inherit the Dockerfile's ENV (defense in depth — costs nothing).
const displayEnv = "DISPLAY=:99"

// DesktopToolConfig tunes desktop tool behavior. Zero-value defaults are
// sensible; the only knob operators typically touch is timeout.
type DesktopToolConfig struct {
	// Timeout caps each xdotool round-trip. Default 15s — desktop ops are
	// fast (<1s each); generous budget covers VNC/slow-VM scenarios.
	Timeout time.Duration
}

// desktopBase is the shared scaffolding for all desktop tools.
type desktopBase struct {
	exec    SandboxExecutor
	timeout time.Duration
}

func newDesktopBase(exec SandboxExecutor, cfg DesktopToolConfig) desktopBase {
	t := cfg.Timeout
	if t == 0 {
		t = 15 * time.Second
	}
	return desktopBase{exec: exec, timeout: t}
}

// run executes a command in the sandbox with a timeout. Returns stdout +
// error (if any). On timeout, returns a Go error mentioning the operation.
func (b *desktopBase) run(ctx context.Context, op, cmd string) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	out, err := b.exec.Exec(execCtx, cmd)
	if execCtx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("desktop %s: timed out after %v", op, b.timeout)
	}
	if err != nil {
		return out, fmt.Errorf("desktop %s: exec failed: %w (output: %s)",
			op, err, tailOutput(out, 400))
	}
	return out, nil
}

// ── Tool 1: desktop_screenshot ────────────────────────────────────────────

// DesktopScreenshotTool captures the current X11 screen and runs OCR +
// window-list extraction via desktop_observe.py (shipped with the sandbox
// image). Returns base64 PNG + text elements (with center coords) + window
// list + resolution. This is the desktop analog of browser_screenshot.
type DesktopScreenshotTool struct{ base desktopBase }

func NewDesktopScreenshotTool(exec SandboxExecutor, cfg DesktopToolConfig) *DesktopScreenshotTool {
	return &DesktopScreenshotTool{base: newDesktopBase(exec, cfg)}
}
func (t *DesktopScreenshotTool) Name() string { return "desktop_screenshot" }
func (t *DesktopScreenshotTool) Description() string {
	return "Capture the sandbox desktop (X11 DISPLAY=:99) as a base64 PNG " +
		"with OCR-detected text elements + window list. " +
		"Each element has cx/cy (center pixel coords) — pass those to desktop_click. " +
		"Use this to orient before clicking or to read on-screen text the model can't see."
}
func (t *DesktopScreenshotTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *DesktopScreenshotTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// desktop_observe.py writes JSON to stdout. Runs in-container; DISPLAY
	// is set via the Dockerfile ENV so we don't need to prefix it here.
	// (Adding the prefix anyway is fine — keeps the call self-contained.)
	cmd := displayEnv + " python3 /usr/local/bin/desktop_observe.py"
	out, err := t.base.run(ctx, "screenshot", cmd)
	if err != nil {
		// desktop_observe.py exits 1 on screenshot failure (rare — Xvfb is
		// always up in the sandbox). Treat as a Go error so the model sees
		// isError:true and can react.
		return nil, err
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("desktop_screenshot: malformed JSON: %w (raw=%q)",
			err, tailOutput(out, 400))
	}
	// If the script set an "error" field, surface it but keep isError:false
	// so the model gets the partial result (some scenarios produce both).
	if e, ok := parsed["error"].(string); ok && e != "" {
		parsed["ok"] = false
	} else {
		parsed["ok"] = true
	}
	return parsed, nil
}

// ── Tool 2: desktop_click ─────────────────────────────────────────────────

// DesktopClickTool clicks at pixel coordinates. The model picks coords from
// a prior desktop_screenshot's element.cx/element.cy or from the visible
// image. Optional button: 1=left (default), 2=middle, 3=right.
type DesktopClickTool struct{ base desktopBase }

func NewDesktopClickTool(exec SandboxExecutor, cfg DesktopToolConfig) *DesktopClickTool {
	return &DesktopClickTool{base: newDesktopBase(exec, cfg)}
}
func (t *DesktopClickTool) Name() string { return "desktop_click" }
func (t *DesktopClickTool) Description() string {
	return "Click at pixel coordinates on the sandbox desktop. " +
		"Pick coords from desktop_screenshot's element.cx/element.cy or the visible image. " +
		"Optional button: 1=left (default), 2=middle, 3=right."
}
func (t *DesktopClickTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["x", "y"],
		"properties": {
			"x": {"type": "integer", "description": "X pixel coordinate (0 = left edge)"},
			"y": {"type": "integer", "description": "Y pixel coordinate (0 = top edge)"},
			"button": {"type": "integer", "description": "Mouse button: 1=left (default), 2=middle, 3=right", "default": 1}
		}
	}`)
}
func (t *DesktopClickTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	x, ok := getIntArg(args, "x")
	if !ok {
		return nil, core.NewToolError("desktop_click", "x is required (integer)")
	}
	y, ok := getIntArg(args, "y")
	if !ok {
		return nil, core.NewToolError("desktop_click", "y is required (integer)")
	}
	button, _ := getIntArg(args, "button")
	if button == 0 {
		button = 1
	}
	if button < 1 || button > 3 {
		return nil, core.NewToolError("desktop_click", "button must be 1, 2, or 3")
	}
	// mousemove + click in one xdotool invocation. --sync blocks until the
	// move actually happens (avoids race with subsequent type).
	cmd := fmt.Sprintf("%s xdotool mousemove --sync %d %d click %d",
		displayEnv, x, y, button)
	if _, err := t.base.run(ctx, "click", cmd); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":     true,
		"x":      x,
		"y":      y,
		"button": button,
	}, nil
}

// ── Tool 3: desktop_type ──────────────────────────────────────────────────

// DesktopTypeTool types text into the focused window. Optional `clear`
// (default true) clears the field first via Ctrl+A + Delete to avoid
// appending to existing text.
type DesktopTypeTool struct{ base desktopBase }

func NewDesktopTypeTool(exec SandboxExecutor, cfg DesktopToolConfig) *DesktopTypeTool {
	return &DesktopTypeTool{base: newDesktopBase(exec, cfg)}
}
func (t *DesktopTypeTool) Name() string { return "desktop_type" }
func (t *DesktopTypeTool) Description() string {
	return "Type text into the focused desktop window via xdotool. " +
		"Optional clear (default true) Ctrl+A + Delete's existing field content first. " +
		"Characters are sent as real X11 key events — works in any app."
}
func (t *DesktopTypeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["text"],
		"properties": {
			"text": {"type": "string", "description": "Text to type"},
			"clear": {"type": "boolean", "description": "Clear field first (default true)", "default": true}
		}
	}`)
}
func (t *DesktopTypeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	text, _ := args["text"].(string)
	if text == "" {
		return nil, core.NewToolError("desktop_type", "text is required")
	}
	clear := true
	if c, ok := args["clear"].(bool); ok {
		clear = c
	}
	// Build: optional clear, then type. Use shQ for the text — single
	// quotes are POSIX-safe even with backticks, double quotes, etc.
	// --clearmodifiers releases Ctrl/Alt during type so they don't combine
	// with the typed chars.
	parts := []string{displayEnv, "xdotool"}
	if clear {
		parts = append(parts, "key ctrl+a Delete")
	}
	parts = append(parts, "type", "--clearmodifiers", shQ(text))
	cmd := strings.Join(parts, " ")
	if _, err := t.base.run(ctx, "type", cmd); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":    true,
		"text":  text,
		"clear": clear,
	}, nil
}

// ── Tool 4: desktop_key ───────────────────────────────────────────────────

// DesktopKeyTool presses a key combo via xdotool key. Examples: "Return",
// "ctrl+c", "alt+Tab", "super". Use for keystrokes that aren't text input
// — modifiers + special keys. For text, use desktop_type.
type DesktopKeyTool struct{ base desktopBase }

func NewDesktopKeyTool(exec SandboxExecutor, cfg DesktopToolConfig) *DesktopKeyTool {
	return &DesktopKeyTool{base: newDesktopBase(exec, cfg)}
}
func (t *DesktopKeyTool) Name() string { return "desktop_key" }
func (t *DesktopKeyTool) Description() string {
	return "Press a key combo on the sandbox desktop via xdotool key. " +
		"Examples: 'Return', 'ctrl+c', 'alt+Tab', 'Escape', 'super'. " +
		"For text input use desktop_type instead."
}
func (t *DesktopKeyTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"required": ["keys"],
		"properties": {
			"keys": {"type": "string", "description": "xdotool key combo (e.g. 'Return', 'ctrl+c', 'alt+Tab')"}
		}
	}`)
}
func (t *DesktopKeyTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	keys, _ := args["keys"].(string)
	if keys == "" {
		return nil, core.NewToolError("desktop_key", "keys is required")
	}
	// xdotool key takes the combo as a single arg. shQ guards against
	// shell-special chars (the combo string itself uses xdotool's own
	// syntax — '+' for chord, not shell).
	cmd := fmt.Sprintf("%s xdotool key %s", displayEnv, shQ(keys))
	if _, err := t.base.run(ctx, "key", cmd); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "keys": keys}, nil
}

// getIntArg pulls an integer from args, tolerating both json.Number and
// float64 (JSON unmarshal default). Returns false if absent or non-numeric.
func getIntArg(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, false
		}
		return i, true
	}
	return 0, false
}

// interface assertions
var (
	_ core.Tool = (*DesktopScreenshotTool)(nil)
	_ core.Tool = (*DesktopClickTool)(nil)
	_ core.Tool = (*DesktopTypeTool)(nil)
	_ core.Tool = (*DesktopKeyTool)(nil)
)
