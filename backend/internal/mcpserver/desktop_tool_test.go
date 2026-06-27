package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"
)

// (fakeSandboxExec + newFakeExec live in sandbox_python_test.go — shared
// across all MCP tool tests.)

// desktopSpecForName returns the spec entry with the given tool name, or
// panics. Used by newDesktopTool + the timeout test (which needs a custom
// cfg, not the default the helper applies).
func desktopSpecForName(name string) desktopSpec {
	for _, s := range desktopSpecs {
		if s.name == name {
			return s
		}
	}
	panic("unknown desktop spec: " + name)
}

// newDesktopTool looks up a spec by tool name and constructs a DesktopTool
// backed by exec. Replaces the 4 per-tool constructors we used to ship.
func newDesktopTool(specName string, exec SandboxExecutor) *DesktopTool {
	return &DesktopTool{
		spec: desktopSpecForName(specName),
		base: newDesktopBase(exec, DesktopToolConfig{}),
	}
}

const sampleObserveJSON = `{
  "image_b64": "iVBORw0KGgoAAAANSUhEUg==",
  "elements": [
    {"id": 1, "text": "File", "x": 10, "y": 5, "w": 30, "h": 15, "cx": 25, "cy": 12},
    {"id": 2, "text": "Save", "x": 10, "y": 30, "w": 40, "h": 18, "cx": 30, "cy": 39}
  ],
  "windows": [{"id": "0x04a00013", "name": "Untitled - Editor"}],
  "resolution": {"width": 1280, "height": 720},
  "ocr_available": true
}`

func TestDesktopScreenshotParsesObserve(t *testing.T) {
	fake := newFakeExec(sampleObserveJSON)
	tool := newDesktopTool("desktop_screenshot", fake)

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	m := result.(map[string]any)
	if m["ok"] != true {
		t.Errorf("ok = %v, want true (full: %v)", m["ok"], m)
	}
	if m["image_b64"] == "" {
		t.Error("image_b64 missing")
	}
	elements, ok := m["elements"].([]any)
	if !ok || len(elements) != 2 {
		t.Errorf("elements wrong: %v", m["elements"])
	}
	if !strings.Contains(fake.lastCmd, "desktop_observe.py") {
		t.Errorf("command missing desktop_observe.py: %q", fake.lastCmd)
	}
	if !strings.Contains(fake.lastCmd, "DISPLAY=:99") {
		t.Errorf("command missing DISPLAY env: %q", fake.lastCmd)
	}
}

func TestDesktopScreenshotMalformedJSON(t *testing.T) {
	fake := newFakeExec("not json at all")
	tool := newDesktopTool("desktop_screenshot", fake)

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected Go error on malformed JSON")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("error should mention malformed: %v", err)
	}
}

func TestDesktopScreenshotExecFailure(t *testing.T) {
	fake := &fakeSandboxExec{err: errFake("exec exited with code 1")}
	tool := newDesktopTool("desktop_screenshot", fake)

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected Go error on exec failure")
	}
	if !strings.Contains(err.Error(), "desktop screenshot") {
		t.Errorf("error should mention operation: %v", err)
	}
}

func TestDesktopScreenshotTimeout(t *testing.T) {
	fake := &fakeSandboxExec{out: "", delay: 5 * time.Second}
	// Construct directly with 1s timeout — the spec-lookup helper uses
	// the default 15s, which the 5s fake delay wouldn't trip.
	tool := &DesktopTool{
		spec: desktopSpecForName("desktop_screenshot"),
		base: newDesktopBase(fake, DesktopToolConfig{Timeout: 1 * time.Second}),
	}

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected Go error on timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should mention timeout: %v", err)
	}
}

func TestDesktopClickByCoords(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_click", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"x": 100,
		"y": 200,
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "xdotool mousemove --sync 100 200 click 1") {
		t.Errorf("command wrong: %q", fake.lastCmd)
	}
	if !strings.Contains(fake.lastCmd, "DISPLAY=:99") {
		t.Errorf("command missing DISPLAY env: %q", fake.lastCmd)
	}
}

func TestDesktopClickDefaultButton(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_click", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"x": 50,
		"y": 60,
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "click 1") {
		t.Errorf("default button should be 1 (left): %q", fake.lastCmd)
	}
}

func TestDesktopClickRightButton(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_click", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"x":      10,
		"y":      20,
		"button": 3,
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "click 3") {
		t.Errorf("right button should be 3: %q", fake.lastCmd)
	}
}

func TestDesktopClickInvalidButton(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_click", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"x":      10,
		"y":      20,
		"button": 4,
	})
	if err == nil {
		t.Fatal("expected Go error on button=4")
	}
}

func TestDesktopClickMissingX(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_click", fake)

	_, err := tool.Execute(context.Background(), map[string]any{"y": 20})
	if err == nil {
		t.Fatal("expected Go error when x missing")
	}
}

func TestDesktopClickFloatCoords(t *testing.T) {
	// JSON unmarshal produces float64 for integer fields. Verify the parser.
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_click", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"x": float64(42),
		"y": float64(99),
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "mousemove --sync 42 99") {
		t.Errorf("float coords not coerced to int: %q", fake.lastCmd)
	}
}

func TestDesktopTypeWithClear(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_type", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"text": "hello world",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "key ctrl+a Delete") {
		t.Errorf("clear should prepend ctrl+a Delete: %q", fake.lastCmd)
	}
	if !strings.Contains(fake.lastCmd, "type --clearmodifiers 'hello world'") {
		t.Errorf("type cmd wrong: %q", fake.lastCmd)
	}
}

func TestDesktopTypeWithoutClear(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_type", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"text":  "append me",
		"clear": false,
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if strings.Contains(fake.lastCmd, "ctrl+a") {
		t.Errorf("clear=false should NOT prepend ctrl+a: %q", fake.lastCmd)
	}
}

func TestDesktopTypeShellEscaping(t *testing.T) {
	// Verify shQ handles single quotes — the POSIX idiom '\'' must be applied
	// so a model-supplied string with a single quote doesn't break out.
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_type", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"text":  "it's a test",
		"clear": false,
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, `'it'\''s a test'`) {
		t.Errorf("single quote not escaped properly: %q", fake.lastCmd)
	}
}

func TestDesktopTypeEmpty(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_type", fake)

	_, err := tool.Execute(context.Background(), map[string]any{"text": ""})
	if err == nil {
		t.Fatal("expected Go error when text empty")
	}
}

func TestDesktopKeyBasic(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_key", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"keys": "Return",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "xdotool key 'Return'") {
		t.Errorf("key cmd wrong: %q", fake.lastCmd)
	}
}

func TestDesktopKeyChord(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_key", fake)

	_, err := tool.Execute(context.Background(), map[string]any{
		"keys": "ctrl+c",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	if !strings.Contains(fake.lastCmd, "key 'ctrl+c'") {
		t.Errorf("chord cmd wrong: %q", fake.lastCmd)
	}
}

func TestDesktopKeyEmpty(t *testing.T) {
	fake := newFakeExec("")
	tool := newDesktopTool("desktop_key", fake)

	_, err := tool.Execute(context.Background(), map[string]any{"keys": ""})
	if err == nil {
		t.Fatal("expected Go error when keys empty")
	}
}
