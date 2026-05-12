package desktop

import (
	"context"
	"errors"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

type mockProvider struct {
	screenshotFn func(ctx context.Context, sandboxID string) (map[string]interface{}, error)
	clickFn      func(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error)
	typeFn       func(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error)
	keyFn        func(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error)
	resolutionFn func(ctx context.Context, sandboxID string) (map[string]interface{}, error)
}

func (m *mockProvider) DesktopScreenshot(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return m.screenshotFn(ctx, sandboxID)
}

func (m *mockProvider) DesktopClick(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error) {
	return m.clickFn(ctx, sandboxID, x, y, button)
}

func (m *mockProvider) DesktopType(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error) {
	return m.typeFn(ctx, sandboxID, text)
}

func (m *mockProvider) DesktopKey(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error) {
	return m.keyFn(ctx, sandboxID, key)
}

func (m *mockProvider) Resolution(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
	return m.resolutionFn(ctx, sandboxID)
}

func defaultProvider() *mockProvider {
	return &mockProvider{
		screenshotFn: func(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
			return map[string]interface{}{"image": "img"}, nil
		},
		clickFn: func(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error) {
			return map[string]interface{}{"success": true}, nil
		},
		typeFn: func(ctx context.Context, sandboxID string, text string) (map[string]interface{}, error) {
			return map[string]interface{}{"success": true}, nil
		},
		keyFn: func(ctx context.Context, sandboxID string, key string) (map[string]interface{}, error) {
			return map[string]interface{}{"success": true}, nil
		},
		resolutionFn: func(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
			return map[string]interface{}{"width": "1920", "height": "1080"}, nil
		},
	}
}

const testSandboxID = "test-sandbox"
var sandboxIDFn = func() string { return testSandboxID }

func TestScreenshotTool(t *testing.T) {
	tool := NewScreenshotTool(defaultProvider(), sandboxIDFn)
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "desktop_screenshot" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "desktop_screenshot")
	}

	result, err := tool.Execute(context.Background(), nil)
	testutil.AssertNoError(t, err)
	m := result.(map[string]interface{})
	if m["image"] != "img" {
		t.Errorf("expected image in result, got %v", m)
	}
}

func TestScreenshotTool_Error(t *testing.T) {
	p := defaultProvider()
	p.screenshotFn = func(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
		return nil, errors.New("screenshot failed")
	}
	tool := NewScreenshotTool(p, sandboxIDFn)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestScreenshotTool_NoSandbox(t *testing.T) {
	tool := NewScreenshotTool(defaultProvider(), func() string { return "" })
	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for no sandbox")
	}
}

func TestClickTool(t *testing.T) {
	tool := NewClickTool(defaultProvider(), sandboxIDFn)
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "desktop_click" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "desktop_click")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"x": float64(500),
		"y": float64(500),
	})
	testutil.AssertNoError(t, err)
	_ = result
}

func TestClickTool_CoordinateNormalization(t *testing.T) {
	var capturedX, capturedY float64
	var capturedButton int
	p := defaultProvider()
	p.clickFn = func(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error) {
		capturedX = x
		capturedY = y
		capturedButton = button
		return map[string]interface{}{"success": true}, nil
	}
	tool := NewClickTool(p, sandboxIDFn)

	_, err := tool.Execute(context.Background(), map[string]any{
		"x":      float64(500),
		"y":      float64(500),
		"button": float64(2),
	})
	testutil.AssertNoError(t, err)

	if capturedX != 960 || capturedY != 540 {
		t.Errorf("expected normalized (960, 540), got (%f, %f)", capturedX, capturedY)
	}
	if capturedButton != 2 {
		t.Errorf("expected button 2, got %d", capturedButton)
	}
}

func TestClickTool_NoCoordNormalization(t *testing.T) {
	// Values > 1000 should be treated as raw pixels
	var capturedX, capturedY float64
	p := defaultProvider()
	p.clickFn = func(ctx context.Context, sandboxID string, x, y float64, button int) (map[string]interface{}, error) {
		capturedX = x
		capturedY = y
		return map[string]interface{}{"success": true}, nil
	}
	// Override resolution to ensure we can detect when normalization happens
	p.resolutionFn = func(ctx context.Context, sandboxID string) (map[string]interface{}, error) {
		return map[string]interface{}{"width": "3840", "height": "2160"}, nil
	}
	tool := NewClickTool(p, sandboxIDFn)

	_, err := tool.Execute(context.Background(), map[string]any{
		"x": float64(1500),
		"y": float64(800),
	})
	testutil.AssertNoError(t, err)

	if capturedX != 1500 || capturedY != 800 {
		t.Errorf("expected raw (1500, 800), got (%f, %f)", capturedX, capturedY)
	}
}

func TestTypeTool(t *testing.T) {
	tool := NewTypeTool(defaultProvider(), sandboxIDFn)
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "desktop_type" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "desktop_type")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"text": "hello",
	})
	testutil.AssertNoError(t, err)
	_ = result
}

func TestTypeTool_EmptyText(t *testing.T) {
	tool := NewTypeTool(defaultProvider(), sandboxIDFn)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
	var toolErr *core.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T", err)
	}
}

func TestKeyTool(t *testing.T) {
	tool := NewKeyTool(defaultProvider(), sandboxIDFn)
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "desktop_key" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "desktop_key")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"key": "Return",
	})
	testutil.AssertNoError(t, err)
	_ = result
}

func TestKeyTool_EmptyKey(t *testing.T) {
	tool := NewKeyTool(defaultProvider(), sandboxIDFn)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestRegisterDesktopTools(t *testing.T) {
	tools := []core.Tool{}
	result := RegisterDesktopTools(tools, defaultProvider(), sandboxIDFn)
	if len(result) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(result))
	}

	names := map[string]bool{}
	for _, tool := range result {
		names[tool.Name()] = true
	}
	for _, name := range []string{"desktop_screenshot", "desktop_click", "desktop_type", "desktop_key"} {
		if !names[name] {
			t.Errorf("expected tool %q in registered tools", name)
		}
	}
}

func TestRegisterDesktopTools_NilProvider(t *testing.T) {
	tools := []core.Tool{}
	result := RegisterDesktopTools(tools, nil, sandboxIDFn)
	if len(result) != 0 {
		t.Fatalf("expected 0 tools with nil provider, got %d", len(result))
	}
}

func TestParseIntField(t *testing.T) {
	tests := []struct {
		m   map[string]interface{}
		key string
		want int
	}{
		{map[string]interface{}{"w": "1920"}, "w", 1920},
		{map[string]interface{}{"w": float64(1920)}, "w", 1920},
		{map[string]interface{}{"w": 1920}, "w", 1920},
		{map[string]interface{}{"w": "bad"}, "w", 0},
		{map[string]interface{}{}, "w", 0},
	}
	for _, tt := range tests {
		got := parseIntField(tt.m, tt.key)
		if got != tt.want {
			t.Errorf("parseIntField(%v, %q) = %d, want %d", tt.m, tt.key, got, tt.want)
		}
	}
}
