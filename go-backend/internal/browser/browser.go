package browser

import (
	"context"
)

// LabeledElement represents an interactive element on the page with its position.
type LabeledElement struct {
	ID         int    `json:"id"`
	Tag        string `json:"tag"`
	DisplayTag string `json:"display_tag,omitempty"` // annotated tag with |SCROLL| and + markers
	Text       string `json:"text"`
	Role       string `json:"role,omitempty"`
	Aria       string `json:"aria,omitempty"` // ARIA annotations (role=, label=, labelledby=)
	Selector   string `json:"selector"`
	X          int    `json:"x,omitempty"`      // bounding box left
	Y          int    `json:"y,omitempty"`      // bounding box top
	W          int    `json:"w,omitempty"`      // bounding box width
	H          int    `json:"h,omitempty"`      // bounding box height
	Parent     string `json:"parent,omitempty"` // nearest container tag (form, nav, main, etc.)
}

// AccessibleElement represents an interactive element discovered through
// the accessibility tree. More stable than SoM labels across layout changes.
type AccessibleElement struct {
	Ref         string `json:"ref"`                   // @e1, @e2 style ref
	Role        string `json:"role"`                  // ARIA role (button, link, textbox, etc.)
	Name        string `json:"name"`                  // accessible name
	Description string `json:"description,omitempty"` // accessible description
	Tag         string `json:"tag"`                   // HTML tag name
	Selector    string `json:"selector"`              // CSS selector for interaction
	Value       string `json:"value,omitempty"`       // current value (input)
	Placeholder string `json:"placeholder,omitempty"` // placeholder text
	Level       int    `json:"level,omitempty"`       // heading level (1-6)
}

// A11ySnapshot is an accessibility-tree representation of the page.
type A11ySnapshot struct {
	URL      string              `json:"url"`
	Title    string              `json:"title"`
	Elements []AccessibleElement `json:"elements"`
}

// FindCriteria specifies how to locate an element semantically.
type FindCriteria struct {
	Role        string `json:"role,omitempty"`        // button, link, textbox, combobox, etc.
	Name        string `json:"name,omitempty"`        // accessible name (exact or substring)
	Label       string `json:"label,omitempty"`       // aria-label or associated label element text
	Text        string `json:"text,omitempty"`         // visible text content
	Placeholder string `json:"placeholder,omitempty"` // placeholder attribute value
	Alt         string `json:"alt,omitempty"`         // alt text of images/inputs
	Title       string `json:"title,omitempty"`       // title attribute
	TestID      string `json:"test_id,omitempty"`     // data-testid attribute
	Selector    string `json:"selector,omitempty"`    // raw CSS selector (fallback)
}

// FoundElement is the result of a semantic find operation.
type FoundElement struct {
	Ref      string `json:"ref"`      // element ref for use with click/type
	Role     string `json:"role"`     // ARIA role
	Name     string `json:"name"`     // accessible name
	Selector string `json:"selector"` // CSS selector for direct interaction
	Text     string `json:"text"`     // visible text
	Tag      string `json:"tag"`      // HTML tag
	Count    int    `json:"count"`    // number of matching elements found
	Action   string `json:"action"`   // action that will be applied
}

// Cookie represents a browser cookie.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path,omitempty"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"http_only"`
	SameSite string  `json:"same_site,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
}

// AuthProfile holds saved authentication state.
type AuthProfile struct {
	Name      string   `json:"name"`
	URL       string   `json:"url,omitempty"`
	Cookies   []Cookie `json:"cookies,omitempty"`
	Username  string   `json:"username,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

// PageInfo contains the result of a browser action
type PageInfo struct {
	URL         string              `json:"url"`
	Title       string              `json:"title"`
	Elements    []LabeledElement    `json:"elements"`
	A11yElements []AccessibleElement `json:"a11y_elements,omitempty"`
	ImageURLs   []string            `json:"image_urls,omitempty"`
	Screenshot  string              `json:"screenshot,omitempty"` // base64 PNG
}

// Session holds state for one browser tab
type Session struct {
	ID         string
	Ctx        context.Context // persistent chromedp tab context
	Cancel     context.CancelFunc
	URL        string
	Title      string
	Elements   []LabeledElement
	Screenshot []byte // raw PNG
}
