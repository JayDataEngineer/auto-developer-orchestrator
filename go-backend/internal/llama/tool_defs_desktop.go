package llama

func init() {
	allTools = append(allTools,
		ToolSpec{
			Name:             "computer_use_enable",
			Category:         CategoryDesktop,
			Description:      "start the desktop environment",
			Schema:           `{}`,
			Returns:          "Starts desktop if not running. Returns CDP port.",
			ParametersSchema: `{"type":"object","properties":{}}`,
		},
		ToolSpec{
			Name:             "computer_use_screenshot",
			Category:         CategoryDesktop,
			Description:      "take screenshot",
			Schema:           `{} or {"describe": true}`,
			Returns:          "Takes screenshot. With describe:true, returns AI description.",
			ParametersSchema: `{"type":"object","properties":{"describe":{"type":"boolean","description":"Return AI description of screenshot"}}}`,
		},
		ToolSpec{
			Name:             "computer_use_snapshot",
			Category:         CategoryDesktop,
			Description:      "get interactive elements with IDs",
			Schema:           `{}`,
			Returns:          `Returns page elements: [ID] <tag> "text"`,
			ParametersSchema: `{"type":"object","properties":{}}`,
		},
		ToolSpec{
			Name:             "computer_use_act",
			Category:         CategoryDesktop,
			Description:      "click, type, navigate, scroll",
			Schema:           `{"action": "navigate", "url": "https://example.com"}`,
			Returns:          "Actions: navigate, click, type, scroll",
			ParametersSchema: `{"type":"object","properties":{"action":{"type":"string","description":"Action to perform: navigate, click, type, scroll","enum":["navigate","click","type","scroll"]},"url":{"type":"string","description":"URL for navigate action"},"element":{"type":"integer","description":"Element ID for click/type"},"text":{"type":"string","description":"Text for type action"},"direction":{"type":"string","description":"Scroll direction: up or down"}},"required":["action"]}`,
		},
		ToolSpec{
			Name:             "desktop_screenshot",
			Category:         CategoryDesktop,
			Description:      "X11 desktop screenshot",
			Schema:           `{}`,
			ParametersSchema: `{"type":"object","properties":{}}`,
		},
		ToolSpec{
			Name:             "desktop_click",
			Category:         CategoryDesktop,
			Description:      "X11 desktop click at coordinates",
			Schema:           `{"x": 100, "y": 200}`,
			ParametersSchema: `{"type":"object","properties":{"x":{"type":"integer","description":"X coordinate"},"y":{"type":"integer","description":"Y coordinate"}},"required":["x","y"]}`,
		},
		ToolSpec{
			Name:             "desktop_type",
			Category:         CategoryDesktop,
			Description:      "X11 desktop type text",
			Schema:           `{"text": "hello"}`,
			ParametersSchema: `{"type":"object","properties":{"text":{"type":"string","description":"Text to type"}},"required":["text"]}`,
		},
		ToolSpec{
			Name:             "desktop_key",
			Category:         CategoryDesktop,
			Description:      "X11 desktop press key",
			Schema:           `{"key": "Return"}`,
			ParametersSchema: `{"type":"object","properties":{"key":{"type":"string","description":"Key name to press"}},"required":["key"]}`,
		},
	)
}
