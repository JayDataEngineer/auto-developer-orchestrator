package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

type mockDriver struct {
	navigateFn    func(ctx context.Context, url string) (*PageContext, error)
	readPageFn    func(ctx context.Context) (*PageContext, error)
	clickElFn     func(ctx context.Context, elementID int, description string) (*PageContext, error)
	typeTextFn    func(ctx context.Context, elementID int, text string, submit bool) (*PageContext, error)
	scrollFn      func(ctx context.Context, direction string) (*PageContext, error)
	observeFn     func(ctx context.Context) (*PageContext, error)
	searchFn      func(ctx context.Context, query string) (*PageContext, error)
	scrapeFn      func(ctx context.Context, url string) (*PageContext, error)
}

func (m *mockDriver) Navigate(ctx context.Context, url string) (*PageContext, error) {
	return m.navigateFn(ctx, url)
}
func (m *mockDriver) ReadPage(ctx context.Context) (*PageContext, error) {
	return m.readPageFn(ctx)
}
func (m *mockDriver) ClickElement(ctx context.Context, elementID int, description string) (*PageContext, error) {
	return m.clickElFn(ctx, elementID, description)
}
func (m *mockDriver) TypeText(ctx context.Context, elementID int, text string, submit bool) (*PageContext, error) {
	return m.typeTextFn(ctx, elementID, text, submit)
}
func (m *mockDriver) Scroll(ctx context.Context, direction string) (*PageContext, error) {
	return m.scrollFn(ctx, direction)
}
func (m *mockDriver) Observe(ctx context.Context) (*PageContext, error) {
	return m.observeFn(ctx)
}
func (m *mockDriver) Search(ctx context.Context, query string) (*PageContext, error) {
	return m.searchFn(ctx, query)
}
func (m *mockDriver) Scrape(ctx context.Context, url string) (*PageContext, error) {
	return m.scrapeFn(ctx, url)
}

func defaultPage() *PageContext {
	return &PageContext{
		URL:   "https://example.com",
		Title: "Example",
		Elements: []Element{
			{ID: 1, Tag: "a", Text: "Click me", Zone: "center"},
		},
	}
}

func defaultDriver() *mockDriver {
	return &mockDriver{
		navigateFn: func(ctx context.Context, url string) (*PageContext, error) { return defaultPage(), nil },
		readPageFn: func(ctx context.Context) (*PageContext, error) { return defaultPage(), nil },
		clickElFn:  func(ctx context.Context, id int, desc string) (*PageContext, error) { return defaultPage(), nil },
		typeTextFn: func(ctx context.Context, id int, text string, submit bool) (*PageContext, error) { return defaultPage(), nil },
		scrollFn:   func(ctx context.Context, dir string) (*PageContext, error) { return defaultPage(), nil },
		observeFn:  func(ctx context.Context) (*PageContext, error) { return defaultPage(), nil },
		searchFn:   func(ctx context.Context, query string) (*PageContext, error) { return defaultPage(), nil },
		scrapeFn:   func(ctx context.Context, url string) (*PageContext, error) { return defaultPage(), nil },
	}
}

func TestNavigateTool(t *testing.T) {
	tool := NewNavigateTool(defaultDriver())
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "browse_to" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "browse_to")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://example.com",
	})
	testutil.AssertNoError(t, err)
	pc := result.(*PageContext)
	if pc.URL != "https://example.com" {
		t.Errorf("expected URL 'https://example.com', got %q", pc.URL)
	}
}

func TestNavigateTool_MissingURL(t *testing.T) {
	tool := NewNavigateTool(defaultDriver())
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
	var toolErr *core.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T", err)
	}
}

func TestClickTool(t *testing.T) {
	tool := NewClickTool(defaultDriver())
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "click_element" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "click_element")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"element": float64(3),
	})
	testutil.AssertNoError(t, err)
	_ = result
}

func TestClickTool_IntElement(t *testing.T) {
	var capturedID int
	d := defaultDriver()
	d.clickElFn = func(ctx context.Context, id int, desc string) (*PageContext, error) {
		capturedID = id
		return defaultPage(), nil
	}
	tool := NewClickTool(d)

	_, err := tool.Execute(context.Background(), map[string]any{
		"element": 5,
	})
	testutil.AssertNoError(t, err)
	if capturedID != 5 {
		t.Errorf("expected element ID 5, got %d", capturedID)
	}
}

func TestClickTool_WithDescription(t *testing.T) {
	var capturedID int
	var capturedDesc string
	d := defaultDriver()
	d.clickElFn = func(ctx context.Context, id int, desc string) (*PageContext, error) {
		capturedID = id
		capturedDesc = desc
		return defaultPage(), nil
	}
	tool := NewClickTool(d)

	_, err := tool.Execute(context.Background(), map[string]any{
		"element":             float64(1),
		"element_description": "Submit button",
	})
	testutil.AssertNoError(t, err)
	if capturedID != 1 {
		t.Errorf("expected element ID 1, got %d", capturedID)
	}
	if capturedDesc != "Submit button" {
		t.Errorf("expected desc 'Submit button', got %q", capturedDesc)
	}
}

func TestClickTool_ZeroElement(t *testing.T) {
	tool := NewClickTool(defaultDriver())
	_, err := tool.Execute(context.Background(), map[string]any{
		"element": float64(0),
	})
	if err == nil {
		t.Fatal("expected error for zero element ID")
	}
}

func TestTypeTool(t *testing.T) {
	tool := NewTypeTool(defaultDriver())
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "type_text" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "type_text")
	}

	var capturedID int
	var capturedText string
	var capturedSubmit bool
	d := defaultDriver()
	d.typeTextFn = func(ctx context.Context, id int, text string, submit bool) (*PageContext, error) {
		capturedID = id
		capturedText = text
		capturedSubmit = submit
		return defaultPage(), nil
	}
	tool = NewTypeTool(d)

	result, err := tool.Execute(context.Background(), map[string]any{
		"element": float64(2),
		"text":    "hello",
		"submit":  true,
	})
	testutil.AssertNoError(t, err)
	_ = result

	if capturedID != 2 {
		t.Errorf("expected element ID 2, got %d", capturedID)
	}
	if capturedText != "hello" {
		t.Errorf("expected text 'hello', got %q", capturedText)
	}
	if !capturedSubmit {
		t.Error("expected submit=true")
	}
}

func TestTypeTool_IntElement(t *testing.T) {
	var capturedID int
	d := defaultDriver()
	d.typeTextFn = func(ctx context.Context, id int, text string, submit bool) (*PageContext, error) {
		capturedID = id
		return defaultPage(), nil
	}
	tool := NewTypeTool(d)

	_, err := tool.Execute(context.Background(), map[string]any{
		"element": 3,
		"text":    "hi",
	})
	testutil.AssertNoError(t, err)
	if capturedID != 3 {
		t.Errorf("expected element ID 3, got %d", capturedID)
	}
}

func TestTypeTool_MissingParams(t *testing.T) {
	tool := NewTypeTool(defaultDriver())
	_, err := tool.Execute(context.Background(), map[string]any{
		"element": float64(0),
		"text":    "",
	})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestReadPageTool(t *testing.T) {
	tool := NewReadPageTool(defaultDriver())
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "read_page" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "read_page")
	}

	result, err := tool.Execute(context.Background(), nil)
	testutil.AssertNoError(t, err)
	pc := result.(*PageContext)
	if len(pc.Elements) != 1 {
		t.Errorf("expected 1 element, got %d", len(pc.Elements))
	}
}

func TestScrollTool(t *testing.T) {
	tool := NewScrollTool(defaultDriver())
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "scroll_page" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "scroll_page")
	}

	var capturedDir string
	d := defaultDriver()
	d.scrollFn = func(ctx context.Context, dir string) (*PageContext, error) {
		capturedDir = dir
		return defaultPage(), nil
	}
	tool = NewScrollTool(d)

	_, err := tool.Execute(context.Background(), map[string]any{
		"direction": "up",
	})
	testutil.AssertNoError(t, err)
	if capturedDir != "up" {
		t.Errorf("expected direction 'up', got %q", capturedDir)
	}
}

func TestScrollTool_DefaultDirection(t *testing.T) {
	var capturedDir string
	d := defaultDriver()
	d.scrollFn = func(ctx context.Context, dir string) (*PageContext, error) {
		capturedDir = dir
		return defaultPage(), nil
	}
	tool := NewScrollTool(d)

	_, err := tool.Execute(context.Background(), map[string]any{})
	testutil.AssertNoError(t, err)
	if capturedDir != "down" {
		t.Errorf("expected default direction 'down', got %q", capturedDir)
	}
}

func TestObserveTool(t *testing.T) {
	tool := NewObserveTool(defaultDriver())
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "observe" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "observe")
	}

	result, err := tool.Execute(context.Background(), nil)
	testutil.AssertNoError(t, err)
	_ = result
}

func TestSearchWebTool(t *testing.T) {
	tool := NewSearchWebTool(defaultDriver())
	testutil.AssertValidSchema(t, tool)

	if tool.Name() != "search_web" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "search_web")
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"query": "golang testing",
	})
	testutil.AssertNoError(t, err)
	_ = result
}

func TestSearchWebTool_MissingQuery(t *testing.T) {
	tool := NewSearchWebTool(defaultDriver())
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestToolSchemas(t *testing.T) {
	d := defaultDriver()
	tools := []core.Tool{
		NewNavigateTool(d),
		NewClickTool(d),
		NewTypeTool(d),
		NewReadPageTool(d),
		NewScrollTool(d),
		NewObserveTool(d),
		NewSearchWebTool(d),
	}
	testutil.AssertValidSchemas(t, tools)
}
