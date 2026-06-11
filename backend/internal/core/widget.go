package core

// WidgetColumnType classifies how a column value should be rendered.
type WidgetColumnType string

const (
	WidgetColText    WidgetColumnType = "text"
	WidgetColBadge   WidgetColumnType = "badge"   // colored pill
	WidgetColBoolean WidgetColumnType = "boolean" // check/x icon
	WidgetColDate    WidgetColumnType = "date"    // relative time
	WidgetColMono    WidgetColumnType = "mono"    // monospace text
	WidgetColStatus  WidgetColumnType = "status"  // icon + text (idle/running/error)
)

// WidgetColumn describes one column in a list or detail widget.
type WidgetColumn struct {
	Key      string           `json:"key"`
	Label    string           `json:"label"`
	Type     WidgetColumnType `json:"type"`
	ColorMap map[string]string `json:"colorMap,omitempty"` // value → tailwind class
}

// WidgetAction describes a clickable button bound to a REST endpoint.
type WidgetAction struct {
	Label   string `json:"label"`
	Icon    string `json:"icon,omitempty"`   // lucide icon name
	Method  string `json:"method"`           // HTTP method: POST, PUT, DELETE
	URL     string `json:"url"`              // may contain {id} placeholder
	Confirm string `json:"confirm,omitempty"` // if set, show confirmation dialog
	Variant string `json:"variant,omitempty"` // "destructive"
}

// WidgetResult is the declarative UI contract.
// Tools return this as a "widget" key in their result map.
// extractArtifact() picks it up and delivers it to the frontend
// via the existing artifact pipeline — no SSE changes needed.
//
// The frontend has ONE generic renderer that reads this structure
// and renders the appropriate widget (list, detail, or confirmation).
// No per-tool frontend code required.
type WidgetResult struct {
	Type    string            `json:"type"`              // "list" | "detail" | "confirm"
	Title   string            `json:"title,omitempty"`   // header text
	Icon    string            `json:"icon,omitempty"`    // lucide icon name for header
	Columns []WidgetColumn    `json:"columns,omitempty"` // column definitions
	Rows    []map[string]any  `json:"rows,omitempty"`    // data rows (for list)
	Item    map[string]any    `json:"item,omitempty"`    // single item (for detail)
	Actions []WidgetAction    `json:"actions,omitempty"` // per-row/per-item actions
	Message string            `json:"message,omitempty"` // confirmation text
	Empty   string            `json:"empty,omitempty"`   // empty state text
}
