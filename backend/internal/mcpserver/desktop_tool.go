// desktop_tool.go exposes the sandbox's X11 desktop (Xvfb on DISPLAY=:99,
// fluxbox wm, xdotool + scrot) as MCP tools. The sandbox already boots Xvfb
// + Chrome + supervisord services; these tools wrap the four operations an
// agent needs to drive arbitrary desktop apps.
//
// All four endpoints share the same shape: build a shell command, exec it
// in the sandbox, synthesize a result. Only the command-construction and
// result-synthesis logic vary. We encode that variation declaratively as
// a slice of desktopSpec structs and dispatch through one DesktopTool type.
//
// Pixel-coord contract (NOT text labels): OCR text positions drift across
// runs (tesseract is non-deterministic at the pixel level). The model picks
// element.cx/element.cy from desktop_screenshot and passes those to
// desktop_click. Icons/checkboxes have no text label at all — pixel coords
// cover both cases.
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

// ── Spec-driven dispatch ──────────────────────────────────────────────

// desktopSpec declaratively describes one desktop_* tool. The spec slice
// below is the source of truth — to add a new desktop tool, append here.
type desktopSpec struct {
	name        string
	description string
	schema      json.RawMessage
	op          string // for error messages ("click", "type", "key", "screenshot")
	// build produces the full xdotool/observe command from args. Returns
	// core.ToolError for invalid args (button out of range, missing x, etc.).
	build func(args map[string]any) (cmd string, err error)
	// result synthesizes the MCP result after a successful exec. `raw` is
	// the exec stdout — desktop_screenshot parses it; click/type/key ignore
	// it and synthesize from args.
	result func(args map[string]any, raw string) (any, error)
}

// DesktopTool is the single dispatcher type for the whole desktop family.
type DesktopTool struct {
	spec desktopSpec
	base desktopBase
}

func (t *DesktopTool) Name() string            { return t.spec.name }
func (t *DesktopTool) Description() string     { return t.spec.description }
func (t *DesktopTool) Schema() json.RawMessage { return t.spec.schema }

func (t *DesktopTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	cmd, err := t.spec.build(args)
	if err != nil {
		return nil, err
	}
	out, err := t.base.run(ctx, t.spec.op, cmd)
	if err != nil {
		return nil, err
	}
	return t.spec.result(args, out)
}

// desktopSpecs is the declarative registry of desktop tools.
var desktopSpecs = []desktopSpec{
	{
		name:        "desktop_screenshot",
		description: "Capture the sandbox desktop (X11 DISPLAY=:99) as a base64 PNG with OCR-detected text elements + window list. Each element has cx/cy (center pixel coords) — pass those to desktop_click. Use this to orient before clicking or to read on-screen text the model can't see.",
		schema:     json.RawMessage(`{"type":"object","properties":{}}`),
		op:         "screenshot",
		build: func(_ map[string]any) (string, error) {
			return displayEnv + " python3 /usr/local/bin/desktop_observe.py", nil
		},
		result: func(_ map[string]any, raw string) (any, error) {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				return nil, fmt.Errorf("desktop_screenshot: malformed JSON: %w (raw=%q)",
					err, tailOutput(raw, 400))
			}
			if e, ok := parsed["error"].(string); ok && e != "" {
				parsed["ok"] = false
			} else {
				parsed["ok"] = true
			}
			return parsed, nil
		},
	},
	{
		name:        "desktop_click",
		description: "Click at pixel coordinates on the sandbox desktop. Pick coords from desktop_screenshot's element.cx/element.cy or the visible image. Optional button: 1=left (default), 2=middle, 3=right.",
		schema: json.RawMessage(`{
			"type": "object",
			"required": ["x", "y"],
			"properties": {
				"x": {"type": "integer", "description": "X pixel coordinate (0 = left edge)"},
				"y": {"type": "integer", "description": "Y pixel coordinate (0 = top edge)"},
				"button": {"type": "integer", "description": "Mouse button: 1=left (default), 2=middle, 3=right", "default": 1}
			}
		}`),
		op: "click",
		build: func(args map[string]any) (string, error) {
			x, ok := getIntArg(args, "x")
			if !ok {
				return "", core.NewToolError("desktop_click", "x is required (integer)")
			}
			y, ok := getIntArg(args, "y")
			if !ok {
				return "", core.NewToolError("desktop_click", "y is required (integer)")
			}
			button, _ := getIntArg(args, "button")
			if button == 0 {
				button = 1
			}
			if button < 1 || button > 3 {
				return "", core.NewToolError("desktop_click", "button must be 1, 2, or 3")
			}
			return fmt.Sprintf("%s xdotool mousemove --sync %d %d click %d",
				displayEnv, x, y, button), nil
		},
		result: func(args map[string]any, _ string) (any, error) {
			x, _ := getIntArg(args, "x")
			y, _ := getIntArg(args, "y")
			button, _ := getIntArg(args, "button")
			if button == 0 {
				button = 1
			}
			return map[string]any{"ok": true, "x": x, "y": y, "button": button}, nil
		},
	},
	{
		name:        "desktop_type",
		description: "Type text into the focused desktop window via xdotool. Optional clear (default true) Ctrl+A + Delete's existing field content first. Characters are sent as real X11 key events — works in any app.",
		schema: json.RawMessage(`{
			"type": "object",
			"required": ["text"],
			"properties": {
				"text": {"type": "string", "description": "Text to type"},
				"clear": {"type": "boolean", "description": "Clear field first (default true)", "default": true}
			}
		}`),
		op: "type",
		build: func(args map[string]any) (string, error) {
			text, _ := args["text"].(string)
			if text == "" {
				return "", core.NewToolError("desktop_type", "text is required")
			}
			clear := true
			if c, ok := args["clear"].(bool); ok {
				clear = c
			}
			parts := []string{displayEnv, "xdotool"}
			if clear {
				parts = append(parts, "key ctrl+a Delete")
			}
			parts = append(parts, "type", "--clearmodifiers", shQ(text))
			return strings.Join(parts, " "), nil
		},
		result: func(args map[string]any, _ string) (any, error) {
			text, _ := args["text"].(string)
			clear := true
			if c, ok := args["clear"].(bool); ok {
				clear = c
			}
			return map[string]any{"ok": true, "text": text, "clear": clear}, nil
		},
	},
	{
		name:        "desktop_key",
		description: "Press a key combo on the sandbox desktop via xdotool key. Examples: 'Return', 'ctrl+c', 'alt+Tab', 'Escape', 'super'. For text input use desktop_type instead.",
		schema: json.RawMessage(`{
			"type": "object",
			"required": ["keys"],
			"properties": {
				"keys": {"type": "string", "description": "xdotool key combo (e.g. 'Return', 'ctrl+c', 'alt+Tab')"}
			}
		}`),
		op: "key",
		build: func(args map[string]any) (string, error) {
			keys, _ := args["keys"].(string)
			if keys == "" {
				return "", core.NewToolError("desktop_key", "keys is required")
			}
			return fmt.Sprintf("%s xdotool key %s", displayEnv, shQ(keys)), nil
		},
		result: func(args map[string]any, _ string) (any, error) {
			keys, _ := args["keys"].(string)
			return map[string]any{"ok": true, "keys": keys}, nil
		},
	},
}

// RegisterDesktopTools installs all desktop_* tools from the spec slice.
// Called from main.go.
func RegisterDesktopTools(srv *Server, exec SandboxExecutor, cfg DesktopToolConfig) {
	base := newDesktopBase(exec, cfg)
	for _, spec := range desktopSpecs {
		srv.RegisterTool(&DesktopTool{spec: spec, base: base})
	}
}

var _ core.Tool = (*DesktopTool)(nil)
