package llama

import (
	"testing"
)

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		want     string
	}{
		// Direct matches — safe with nil args
		{"bash", nil, "bash"},
		{"browse_to", nil, "browse_to"},
		{"mcp_call", nil, "mcp_call"},

		// MCP-prefixed tools pass through unchanged
		{"mcp_research", nil, "mcp_research"},
		{"mcp_scrape", nil, "mcp_scrape"},

		// Common aliases (need non-nil args for fallback writes)
		{"bash_execute", nil, "bash"},
		{"execute_bash", nil, "bash"},
		{"run_command", nil, "bash"},
		{"shell", nil, "bash"},

		// Navigation aliases
		{"navigate", map[string]interface{}{"url": "https://example.com"}, "browse_to"},
		{"open_url", map[string]interface{}{"url": "https://example.com"}, "browse_to"},
		{"go_to", map[string]interface{}{"url": "https://example.com"}, "browse_to"},

		// Screenshot aliases
		{"screenshot", nil, "computer_use_screenshot"},
		{"take_screenshot", nil, "computer_use_screenshot"},
		{"capture_screenshot", nil, "computer_use_screenshot"},

		// Click-element aliases (need writable args)
		{"click", map[string]interface{}{}, "click_element"},
		{"browser_click", map[string]interface{}{}, "click_element"},
		{"click_button", map[string]interface{}{}, "click_element"},

		// Snapshot aliases
		{"snapshot", nil, "computer_use_snapshot"},
		{"page_snapshot", nil, "computer_use_snapshot"},

		// Type aliases (need writable args for element normalization)
		{"type", map[string]interface{}{}, "type_text"},
		{"fill", map[string]interface{}{}, "type_text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeToolName(tt.name, tt.args)
			if got != tt.want {
				t.Errorf("normalizeToolName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestNormalizeToolName_NavigateCopiesURL(t *testing.T) {
	args := map[string]interface{}{"URL": "https://example.com"}
	normalizeToolName("navigate", args)
	if u, ok := args["url"]; !ok || u != "https://example.com" {
		t.Errorf("expected url to be copied from URL: %v", args)
	}
}
