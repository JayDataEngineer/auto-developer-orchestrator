package browser

import (
	"context"
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Driver abstracts CDP/browser automation.
// Implementations use Chrome DevTools Protocol.
type Driver interface {
	Navigate(ctx context.Context, url string) (*PageContext, error)
	ReadPage(ctx context.Context) (*PageContext, error)
	ClickElement(ctx context.Context, elementID int, description string) (*PageContext, error)
	TypeText(ctx context.Context, elementID int, text string, submit bool) (*PageContext, error)
	Scroll(ctx context.Context, direction string) (*PageContext, error)
	Observe(ctx context.Context) (*PageContext, error)
	Search(ctx context.Context, query string) (*PageContext, error)
	Scrape(ctx context.Context, url string) (*PageContext, error)
}

// PageContext represents the state of a web page.
type PageContext struct {
	URL      string   `json:"url"`
	Title    string   `json:"title"`
	Elements []Element `json:"elements,omitempty"`
	Content  string   `json:"content,omitempty"`
	Vision   string   `json:"vision,omitempty"`
	Screenshot string `json:"screenshot,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// Element represents a labeled interactive element on a page.
type Element struct {
	ID   int    `json:"id"`
	Tag  string `json:"tag"`
	Text string `json:"text"`
	Zone string `json:"zone"`
}

// NavigateTool implements core.Tool for browsing to a URL.
type NavigateTool struct {
	driver Driver
}

func NewNavigateTool(d Driver) *NavigateTool {
	return &NavigateTool{driver: d}
}

func (t *NavigateTool) Name() string        { return "browse_to" }
func (t *NavigateTool) Description() string { return "Navigate to a URL and return page contents" }

func (t *NavigateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "Full URL to navigate to"}
		},
		"required": ["url"]
	}`)
}

func (t *NavigateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	url, _ := args["url"].(string)
	if url == "" {
		return nil, core.NewToolError("browse_to", "missing required parameter 'url'")
	}
	pc, err := t.driver.Navigate(ctx, url)
	if err != nil {
		return nil, err
	}
	return pc, nil
}

// ClickTool implements core.Tool for clicking page elements.
type ClickTool struct {
	driver Driver
}

func NewClickTool(d Driver) *ClickTool {
	return &ClickTool{driver: d}
}

func (t *ClickTool) Name() string        { return "click_element" }
func (t *ClickTool) Description() string { return "Click an interactive element on the page" }

func (t *ClickTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"element": {"type": "integer", "description": "Element ID to click (from page elements list)"},
			"element_description": {"type": "string", "description": "Description of the element to click (e.g. 'Submit button')"}
		},
		"required": ["element"]
	}`)
}

func (t *ClickTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	elementID := 0
	desc := ""
	if id, ok := args["element"].(float64); ok {
		elementID = int(id)
	}
	if id, ok := args["element"].(int); ok {
		elementID = id
	}
	if d, ok := args["element_description"].(string); ok {
		desc = d
	}
	if elementID <= 0 {
		return nil, core.NewToolError("click_element", "missing required parameter 'element'")
	}
	pc, err := t.driver.ClickElement(ctx, elementID, desc)
	if err != nil {
		return nil, err
	}
	return pc, nil
}

// TypeTool implements core.Tool for typing into form elements.
type TypeTool struct {
	driver Driver
}

func NewTypeTool(d Driver) *TypeTool {
	return &TypeTool{driver: d}
}

func (t *TypeTool) Name() string        { return "type_text" }
func (t *TypeTool) Description() string { return "Type text into an input element" }

func (t *TypeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"element": {"type": "integer", "description": "Element ID of the input field"},
			"text": {"type": "string", "description": "Text to type"},
			"submit": {"type": "boolean", "description": "Press Enter after typing"}
		},
		"required": ["element", "text"]
	}`)
}

func (t *TypeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	elementID := 0
	if id, ok := args["element"].(float64); ok {
		elementID = int(id)
	}
	if id, ok := args["element"].(int); ok {
		elementID = id
	}
	text, _ := args["text"].(string)
	if elementID <= 0 || text == "" {
		return nil, core.NewToolError("type_text", "missing required parameters 'element' and 'text'")
	}
	submit := false
	if s, ok := args["submit"].(bool); ok {
		submit = s
	}
	pc, err := t.driver.TypeText(ctx, elementID, text, submit)
	if err != nil {
		return nil, err
	}
	return pc, nil
}

// ReadPageTool implements core.Tool for re-reading current page elements.
type ReadPageTool struct {
	driver Driver
}

func NewReadPageTool(d Driver) *ReadPageTool {
	return &ReadPageTool{driver: d}
}

func (t *ReadPageTool) Name() string        { return "read_page" }
func (t *ReadPageTool) Description() string { return "Re-read the current page elements" }

func (t *ReadPageTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ReadPageTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pc, err := t.driver.ReadPage(ctx)
	if err != nil {
		return nil, err
	}
	return pc, nil
}

// ScrollTool implements core.Tool for scrolling the page.
type ScrollTool struct {
	driver Driver
}

func NewScrollTool(d Driver) *ScrollTool {
	return &ScrollTool{driver: d}
}

func (t *ScrollTool) Name() string        { return "scroll_page" }
func (t *ScrollTool) Description() string { return "Scroll the current page up or down" }

func (t *ScrollTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"direction": {"type": "string", "description": "Direction: 'up' or 'down'"}
		}
	}`)
}

func (t *ScrollTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	direction := "down"
	if d, ok := args["direction"].(string); ok {
		direction = d
	}
	pc, err := t.driver.Scroll(ctx, direction)
	if err != nil {
		return nil, err
	}
	return pc, nil
}

// ObserveTool implements core.Tool for taking a screenshot and analyzing.
type ObserveTool struct {
	driver Driver
}

func NewObserveTool(d Driver) *ObserveTool {
	return &ObserveTool{driver: d}
}

func (t *ObserveTool) Name() string        { return "observe" }
func (t *ObserveTool) Description() string { return "Capture screenshot + elements + AI vision description" }

func (t *ObserveTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ObserveTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	pc, err := t.driver.Observe(ctx)
	if err != nil {
		return nil, err
	}
	return pc, nil
}

// SearchWebTool implements core.Tool for searching the web.
type SearchWebTool struct {
	driver Driver
}

func NewSearchWebTool(d Driver) *SearchWebTool {
	return &SearchWebTool{driver: d}
}

func (t *SearchWebTool) Name() string        { return "search_web" }
func (t *SearchWebTool) Description() string { return "Search the web and return results" }

func (t *SearchWebTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query"}
		},
		"required": ["query"]
	}`)
}

func (t *SearchWebTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return nil, core.NewToolError("search_web", "missing required parameter 'query'")
	}
	pc, err := t.driver.Search(ctx, query)
	if err != nil {
		return nil, err
	}
	return pc, nil
}
