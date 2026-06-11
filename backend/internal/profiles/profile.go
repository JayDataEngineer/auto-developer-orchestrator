package profiles

// Profile defines how to interact with an application through semantic actions.
// Stored as YAML in ~/.pux/profiles/ or <project>/profiles/.
type Profile struct {
	App    string           `yaml:"app"`
	Type   string           `yaml:"type"` // "game", "desktop", "browser"
	Detect DetectConfig     `yaml:"detect"`
	Actions map[string]Action `yaml:"actions"`
	Layout  *LayoutConfig   `yaml:"layout,omitempty"`
}

// DetectConfig describes how to identify the app is running.
type DetectConfig struct {
	WindowTitle  string `yaml:"window_title,omitempty"`
	WindowClass  string `yaml:"window_class,omitempty"`
	Process      string `yaml:"process,omitempty"`
}

// Action maps a semantic action name to input primitives.
type Action struct {
	// Simple: single key press
	Key string `yaml:"key,omitempty"`

	// Key with hold (movement keys, etc.)
	Hold *bool `yaml:"hold,omitempty"`

	// Mouse action: "left", "right", "middle"
	Mouse string `yaml:"mouse,omitempty"`

	// Mouse hold (for drag, etc.)
	MouseHold *bool `yaml:"mouse_hold,omitempty"`

	// Shortcut: key combo like "Ctrl+F", "Alt+Tab"
	Shortcut string `yaml:"shortcut,omitempty"`

	// Type: text to type (supports {param} interpolation)
	Type string `yaml:"type,omitempty"`

	// Compound: sequence of primitives
	Steps []Step `yaml:"steps,omitempty"`

	// Parameters for interpolation in steps
	Params map[string]ParamDef `yaml:"params,omitempty"`

	// Wait after execution (ms)
	Wait int `yaml:"wait,omitempty"`

	// Release a previously held key (used with hold actions)
	Release bool `yaml:"release,omitempty"`
}

// Step is a single primitive in a compound action sequence.
type Step struct {
	Key      string `yaml:"key,omitempty"`
	Shortcut string `yaml:"shortcut,omitempty"`
	Type     string `yaml:"type,omitempty"`
	Mouse    string `yaml:"mouse,omitempty"`
	Click    string `yaml:"click,omitempty"`    // "left", "right", "middle"
	Wait     int    `yaml:"wait,omitempty"`     // ms
	Duration int    `yaml:"duration,omitempty"` // ms to hold key

	// Interpolation: {param_name} gets replaced
}

// ParamDef describes a parameter for action interpolation.
type ParamDef struct {
	Type  string `yaml:"type"`            // "string", "int"
	Min   *int   `yaml:"min,omitempty"`
	Max   *int   `yaml:"max,omitempty"`
	Range []int  `yaml:"range,omitempty"` // [min, max]
}

// LayoutConfig describes the visual layout for future vision integration.
type LayoutConfig struct {
	Type    string            `yaml:"type"` // "first_person", "top_down", "desktop", "browser"
	Regions map[string]Region `yaml:"regions,omitempty"`
}

// Region describes a named area on screen.
type Region struct {
	Where string `yaml:"where"`  // e.g. "bottom_left", "center", "top"
	Looks string `yaml:"looks"`  // e.g. "hearts", "hotbar", "crosshair"
	Slots int    `yaml:"slots,omitempty"`
}
