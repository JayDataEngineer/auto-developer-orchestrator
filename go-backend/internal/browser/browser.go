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

// PageInfo contains the result of a browser action
type PageInfo struct {
	URL        string           `json:"url"`
	Title      string           `json:"title"`
	Elements   []LabeledElement `json:"elements"`
	ImageURLs  []string         `json:"image_urls,omitempty"`
	Screenshot string           `json:"screenshot,omitempty"` // base64 PNG
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
