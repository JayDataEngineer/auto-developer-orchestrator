package vision

import (
	"context"
	"encoding/json"
	"testing"
)

// ── DetectImage tests ────────────────────────────────────────────────

func TestDetectImage_BrowserScreenshot(t *testing.T) {
	pc := map[string]any{
		"url":       "https://example.com",
		"title":     "Example",
		"screenshot": "iVBORw0KGgo=",
	}
	b, _ := json.Marshal(pc)

	d := DetectImage("browse_to", string(b))
	if d == nil {
		t.Fatal("expected detection for browser tool with screenshot")
	}
	if !d.HasImage {
		t.Error("HasImage should be true")
	}
	if d.Base64Data != "iVBORw0KGgo=" {
		t.Errorf("Base64Data = %q, want raw base64", d.Base64Data)
	}
	if d.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q, want image/png", d.MIMEType)
	}
	if d.AlreadyDescribed {
		t.Error("AlreadyDescribed should be false when Vision is empty")
	}
}

func TestDetectImage_BrowserAlreadyDescribed(t *testing.T) {
	pc := map[string]any{
		"url":       "https://example.com",
		"screenshot": "iVBORw0KGgo=",
		"vision":    "A web page with a blue header",
	}
	b, _ := json.Marshal(pc)

	d := DetectImage("browse_to", string(b))
	if d == nil {
		t.Fatal("expected detection")
	}
	if !d.AlreadyDescribed {
		t.Error("AlreadyDescribed should be true when Vision is populated")
	}
}

func TestDetectImage_BrowserNoScreenshot(t *testing.T) {
	pc := map[string]any{
		"url":   "https://example.com",
		"title": "Example",
	}
	b, _ := json.Marshal(pc)

	d := DetectImage("browse_to", string(b))
	if d != nil {
		t.Error("expected nil for browser tool without screenshot")
	}
}

func TestDetectImage_DesktopFrame(t *testing.T) {
	df := map[string]any{
		"width":     1920,
		"height":    1080,
		"image_b64": "aGVsbG8=",
	}
	b, _ := json.Marshal(df)

	d := DetectImage("desktop_screenshot", string(b))
	if d == nil {
		t.Fatal("expected detection for desktop tool with image_b64")
	}
	if !d.HasImage {
		t.Error("HasImage should be true")
	}
	if d.Base64Data != "aGVsbG8=" {
		t.Errorf("Base64Data = %q, want aGVsbG8=", d.Base64Data)
	}
}

func TestDetectImage_DesktopEmptyImage(t *testing.T) {
	df := map[string]any{
		"width":  1920,
		"height": 1080,
	}
	b, _ := json.Marshal(df)

	d := DetectImage("desktop_screenshot", string(b))
	if d != nil {
		t.Error("expected nil for desktop frame without image_b64")
	}
}

func TestDetectImage_NonImageTool(t *testing.T) {
	d := DetectImage("bash", `{"output": "hello world"}`)
	if d != nil {
		t.Error("expected nil for non-image tool")
	}
}

func TestDetectImage_ClickElement(t *testing.T) {
	pc := map[string]any{
		"screenshot": "data123",
	}
	b, _ := json.Marshal(pc)

	d := DetectImage("click_element", string(b))
	if d == nil {
		t.Fatal("expected detection for click_element (browser tool)")
	}
}

func TestDetectImage_InvalidJSON(t *testing.T) {
	d := DetectImage("browse_to", "not json at all")
	if d != nil {
		t.Error("expected nil for invalid JSON")
	}
}

// ── FallbackChain tests ─────────────────────────────────────────────

type mockProvider struct {
	name      string
	available bool
	result    string
	err       error
}

func (m *mockProvider) Name() string                                                { return m.name }
func (m *mockProvider) IsAvailable(_ context.Context) bool                          { return m.available }
func (m *mockProvider) Describe(_ context.Context, _ ImageInput) (Description, error) {
	return Description{Text: m.result, Provider: m.name}, m.err
}

func TestFallbackChain_FirstAvailable(t *testing.T) {
	chain := NewFallbackChain(
		&mockProvider{name: "p1", available: true, result: "desc from p1"},
	)

	desc, err := chain.Describe(context.Background(), ImageInput{Source: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc.Text != "desc from p1" {
		t.Errorf("Text = %q, want %q", desc.Text, "desc from p1")
	}
	if desc.Provider != "p1" {
		t.Errorf("Provider = %q, want p1", desc.Provider)
	}
}

func TestFallbackChain_FallbackToSecond(t *testing.T) {
	chain := NewFallbackChain(
		&mockProvider{name: "p1", available: false},
		&mockProvider{name: "p2", available: true, result: "desc from p2"},
	)

	desc, err := chain.Describe(context.Background(), ImageInput{Source: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc.Provider != "p2" {
		t.Errorf("Provider = %q, want p2", desc.Provider)
	}
}

func TestFallbackChain_ErrorFallsThrough(t *testing.T) {
	chain := NewFallbackChain(
		&mockProvider{name: "p1", available: true, err: ErrNoProvider},
		&mockProvider{name: "p2", available: true, result: "fallback desc"},
	)

	desc, err := chain.Describe(context.Background(), ImageInput{Source: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if desc.Provider != "p2" {
		t.Errorf("Provider = %q, want p2", desc.Provider)
	}
}

func TestFallbackChain_NoProvider(t *testing.T) {
	chain := NewFallbackChain(
		&mockProvider{name: "p1", available: false},
		&mockProvider{name: "p2", available: false},
	)

	_, err := chain.Describe(context.Background(), ImageInput{Source: "test"})
	if err != ErrNoProvider {
		t.Errorf("error = %v, want ErrNoProvider", err)
	}
}

// ── VisionAwareExecutor tests ───────────────────────────────────────

type mockExecutor struct {
	result any
	err    error
}

func (m *mockExecutor) Execute(_ context.Context, _ string, _ map[string]any) (any, error) {
	return m.result, m.err
}

func TestExecutor_NoImage(t *testing.T) {
	inner := &mockExecutor{result: map[string]any{"output": "hello"}}
	chain := NewFallbackChain(&mockProvider{name: "mcp", available: true, result: "should not be called"})

	exec := NewVisionAwareExecutor(inner, chain, nil)
	result, err := exec.Execute(context.Background(), "bash", map[string]any{"cmd": "echo hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map", result)
	}
	if m["output"] != "hello" {
		t.Errorf("output = %v, want hello", m["output"])
	}
}

func TestExecutor_WithBrowserImage(t *testing.T) {
	pageResult := map[string]any{
		"url":       "https://example.com",
		"screenshot": "iVBORw0KGgo=",
		"vision":    "",
	}
	inner := &mockExecutor{result: pageResult}
	chain := NewFallbackChain(&mockProvider{name: "mcp", available: true, result: "A page with a header"})

	exec := NewVisionAwareExecutor(inner, chain, nil)
	result, err := exec.Execute(context.Background(), "browse_to", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map", result)
	}
	vision, _ := m["vision"].(string)
	if vision != "A page with a header" {
		t.Errorf("vision = %q, want vision description", vision)
	}
}

func TestExecutor_SkipAlreadyDescribed(t *testing.T) {
	pageResult := map[string]any{
		"url":       "https://example.com",
		"screenshot": "iVBORw0KGgo=",
		"vision":    "Already described",
	}
	inner := &mockExecutor{result: pageResult}
	chain := NewFallbackChain(&mockProvider{name: "mcp", available: true, result: "NEW DESC"})

	exec := NewVisionAwareExecutor(inner, chain, nil)
	result, err := exec.Execute(context.Background(), "browse_to", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map", result)
	}
	vision, _ := m["vision"].(string)
	if vision != "Already described" {
		t.Errorf("vision = %q, should not be overwritten", vision)
	}
}

func TestExecutor_ChainFails(t *testing.T) {
	pageResult := map[string]any{
		"url":       "https://example.com",
		"screenshot": "iVBORw0KGgo=",
	}
	inner := &mockExecutor{result: pageResult}
	chain := NewFallbackChain(&mockProvider{name: "mcp", available: false})

	exec := NewVisionAwareExecutor(inner, chain, nil)
	result, err := exec.Execute(context.Background(), "browse_to", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return original result when vision fails (graceful)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map", result)
	}
	if m["url"] != "https://example.com" {
		t.Errorf("url = %v, should be preserved", m["url"])
	}
}

func TestExecutor_InnerError(t *testing.T) {
	inner := &mockExecutor{err: ErrNoProvider}
	chain := NewFallbackChain(&mockProvider{name: "mcp", available: true})

	exec := NewVisionAwareExecutor(inner, chain, nil)
	result, err := exec.Execute(context.Background(), "bash", map[string]any{})
	if err != ErrNoProvider {
		t.Errorf("error = %v, want ErrNoProvider", err)
	}
	if result != nil {
		t.Errorf("result = %v, want nil on error", result)
	}
}

// ── promptForTool tests ─────────────────────────────────────────────

func TestPromptForTool(t *testing.T) {
	browserPrompt := promptForTool("browse_to")
	if browserPrompt == "" {
		t.Error("browser prompt should not be empty")
	}
	if !contains(browserPrompt, "web page") {
		t.Error("browser prompt should mention 'web page'")
	}

	desktopPrompt := promptForTool("desktop_screenshot")
	if !contains(desktopPrompt, "desktop") {
		t.Error("desktop prompt should mention 'desktop'")
	}

	genericPrompt := promptForTool("unknown_tool")
	if genericPrompt == "" {
		t.Error("generic prompt should not be empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
