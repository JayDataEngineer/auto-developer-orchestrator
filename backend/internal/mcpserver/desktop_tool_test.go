package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"
)

// desktopFakeExec returns a canned desktop_observe.py JSON envelope. For
// the click/type/key tests we just need to verify the command shape —
// empty out is fine since success only checks err.
func newDesktopFake(out string) *fakeSandboxExec { return &fakeSandboxExec{out: out} }

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
	fake := newDesktopFake(sampleObserveJSON)
	tool := NewDesktopScreenshotTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("not json at all")
	tool := NewDesktopScreenshotTool(fake, DesktopToolConfig{})

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
	tool := NewDesktopScreenshotTool(fake, DesktopToolConfig{})

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
	tool := NewDesktopScreenshotTool(fake, DesktopToolConfig{Timeout: 1 * time.Second})

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected Go error on timeout")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should mention timeout: %v", err)
	}
}

func TestDesktopClickByCoords(t *testing.T) {
	fake := newDesktopFake("")
	tool := NewDesktopClickTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopClickTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopClickTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopClickTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopClickTool(fake, DesktopToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{"y": 20})
	if err == nil {
		t.Fatal("expected Go error when x missing")
	}
}

func TestDesktopClickFloatCoords(t *testing.T) {
	// JSON unmarshal produces float64 for integer fields. Verify the parser.
	fake := newDesktopFake("")
	tool := NewDesktopClickTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopTypeTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopTypeTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopTypeTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopTypeTool(fake, DesktopToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{"text": ""})
	if err == nil {
		t.Fatal("expected Go error when text empty")
	}
}

func TestDesktopKeyBasic(t *testing.T) {
	fake := newDesktopFake("")
	tool := NewDesktopKeyTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopKeyTool(fake, DesktopToolConfig{})

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
	fake := newDesktopFake("")
	tool := NewDesktopKeyTool(fake, DesktopToolConfig{})

	_, err := tool.Execute(context.Background(), map[string]any{"keys": ""})
	if err == nil {
		t.Fatal("expected Go error when keys empty")
	}
}
