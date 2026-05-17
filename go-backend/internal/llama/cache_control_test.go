package llama

import (
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestInjectCacheBreakpoints_SplitsAtBoundary(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "Stable content\n" + dynamicBoundary + "\nDynamic content"},
		{Role: "user", Content: "hello"},
	}

	result := injectCacheBreakpoints(msgs)

	// Should split into 3 messages: stable system (cached) + dynamic system + user
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	if result[0].Role != "system" {
		t.Error("first message should be system")
	}
	if result[0].Content != "Stable content" {
		t.Errorf("stable content wrong: %q", result[0].Content)
	}
	if result[0].CacheControl == nil || result[0].CacheControl.Type != "ephemeral" {
		t.Error("stable system message should have cache_control: ephemeral")
	}

	if result[1].Role != "system" {
		t.Error("second message should be system")
	}
	if result[1].Content != "Dynamic content" {
		t.Errorf("dynamic content wrong: %q", result[1].Content)
	}
	if result[1].CacheControl != nil {
		t.Error("dynamic system message should NOT have cache_control")
	}

	if result[2].Role != "user" {
		t.Error("third message should be user")
	}
}

func TestInjectCacheBreakpoints_NoBoundary(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "No boundary here"},
		{Role: "user", Content: "hello"},
	}

	result := injectCacheBreakpoints(msgs)

	// Should return unchanged
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (unchanged), got %d", len(result))
	}
	if result[0].CacheControl != nil {
		t.Error("should not add cache_control without boundary")
	}
}

func TestInjectCacheBreakpoints_NoSystemMessage(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	result := injectCacheBreakpoints(msgs)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages (unchanged), got %d", len(result))
	}
}

func TestInjectCacheBreakpoints_OnlyBoundary(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: dynamicBoundary},
	}

	result := injectCacheBreakpoints(msgs)

	// Boundary with empty content on both sides — should still work
	if len(result) != 0 {
		t.Fatalf("expected 0 messages (all empty), got %d", len(result))
	}
}

func TestLLMClient_SupportsCaching(t *testing.T) {
	tests := []struct {
		model    string
		expected bool
	}{
		{"claude-3-opus", true},
		{"anthropic/claude-3", true},
		{"deepseek-v4-flash", true},
		{"gemini-3-flash", false},
		{"qwen3-27b", false},
		{"gemma-4-26b", false},
	}

	for _, tt := range tests {
		client := &LLMClient{modelName: tt.model}
		// apiKey must be set for IsCloud to return true
		client.apiKey = "test-key"
		if got := client.supportsCaching(); got != tt.expected {
			t.Errorf("supportsCaching(%q) = %v, want %v", tt.model, got, tt.expected)
		}
	}
}

func TestSanitizeRequest_InjectsCacheControl(t *testing.T) {
	client := &LLMClient{
		modelName: "claude-3-opus",
		apiKey:    "test-key",
	}

	req := ChatCompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "Stable rules\n" + dynamicBoundary + "\nSandbox: abc"},
			{Role: "user", Content: "hello"},
		},
	}

	result := client.sanitizeRequest(req)

	// System should be split
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].CacheControl == nil {
		t.Error("stable system message should have cache_control")
	}
	if result.Messages[0].CacheControl.Type != "ephemeral" {
		t.Error("cache_control type should be ephemeral")
	}
}

func TestSanitizeRequest_NoCacheControlForLocalProvider(t *testing.T) {
	client := &LLMClient{
		modelName: "gemma-4-26b",
		// no apiKey — local provider
	}

	req := ChatCompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "Stable rules\n" + dynamicBoundary + "\nSandbox: abc"},
			{Role: "user", Content: "hello"},
		},
	}

	result := client.sanitizeRequest(req)

	// Local provider returns unchanged (still has boundary)
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages (unchanged for local), got %d", len(result.Messages))
	}
}

// Verify CacheControl type alias works
func TestCacheControlType(t *testing.T) {
	cc := &CacheControl{Type: "ephemeral"}
	if cc.Type != "ephemeral" {
		t.Error("CacheControl type alias broken")
	}
	// Verify it's the core type
	var _ *core.CacheControl = cc
}
