package desktop

import (
	"context"
	"errors"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

type mockDriver struct {
	screenshotFn func(ctx context.Context) (*DesktopFrame, error)
	clickFn      func(ctx context.Context, x, y float64, button int) (*DesktopFrame, error)
	typeFn       func(ctx context.Context, text string) (*DesktopFrame, error)
	keyFn        func(ctx context.Context, key string) (*DesktopFrame, error)
	resolutionFn func(ctx context.Context) (int, int, error)
}

func (m *mockDriver) Screenshot(ctx context.Context) (*DesktopFrame, error) {
	return m.screenshotFn(ctx)
}
func (m *mockDriver) Click(ctx context.Context, x, y float64, button int) (*DesktopFrame, error) {
	return m.clickFn(ctx, x, y, button)
}
func (m *mockDriver) Type(ctx context.Context, text string) (*DesktopFrame, error) {
	return m.typeFn(ctx, text)
}
func (m *mockDriver) Key(ctx context.Context, key string) (*DesktopFrame, error) {
	return m.keyFn(ctx, key)
}
func (m *mockDriver) Resolution(ctx context.Context) (int, int, error) {
	return m.resolutionFn(ctx)
}

func defaultDriver() *mockDriver {
	return &mockDriver{
		screenshotFn: func(ctx context.Context) (*DesktopFrame, error) {
			return &DesktopFrame{Width: 1920, Height: 1080, ImageB64: "img"}, nil
		},
		clickFn: func(ctx context.Context, x, y float64, button int) (*DesktopFrame, error) {
			return &DesktopFrame{Width: 1920, Height: 1080}, nil
		},
		typeFn: func(ctx context.Context, text string) (*DesktopFrame, error) {
			return &DesktopFrame{Width: 1920, Height: 1080}, nil
		},
		keyFn: func(ctx context.Context, key string) (*DesktopFrame, error) {
			return &DesktopFrame{Width: 1920, Height: 1080}, nil
		},
		resolutionFn: func(ctx context.Context) (int, int, error) {
			return 1920, 1080, nil
		},
	}
}

func TestScreenshotTool(t *testing.T) {
	tool := NewScreenshotTool(defaultDriver())
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "desktop_screenshot" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "desktop_screenshot")
	}

	result, err := tool.Execute(context.Background(), nil)
	testutil.AssertNoError(t, err)
	frame := result.(*DesktopFrame)
	if frame.Width != 1920 {
		t.Errorf("expected width 1920, got %d", frame.Width)
	}
}

func TestScreenshotTool_Error(t *testing.T) {
	d := defaultDriver()
	d.screenshotFn = func(ctx context.Context) (*DesktopFrame, error) {
		return nil, errors.New("screenshot failed")
	}
	tool := NewScreenshotTool(d)

	_, err := tool.Execute(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClickTool(t *testing.T) {
	tool := NewClickTool(defaultDriver())
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
	d := defaultDriver()
	d.clickFn = func(ctx context.Context, x, y float64, button int) (*DesktopFrame, error) {
		capturedX = x
		capturedY = y
		capturedButton = button
		return &DesktopFrame{}, nil
	}
	tool := NewClickTool(d)

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
	d := defaultDriver()
	d.clickFn = func(ctx context.Context, x, y float64, button int) (*DesktopFrame, error) {
		capturedX = x
		capturedY = y
		return &DesktopFrame{}, nil
	}
	// Override resolution to ensure we can detect when normalization happens
	d.resolutionFn = func(ctx context.Context) (int, int, error) {
		return 3840, 2160, nil
	}
	tool := NewClickTool(d)

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
	tool := NewTypeTool(defaultDriver())
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
	tool := NewTypeTool(defaultDriver())
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
	tool := NewKeyTool(defaultDriver())
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
	tool := NewKeyTool(defaultDriver())
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty key")
	}
}
