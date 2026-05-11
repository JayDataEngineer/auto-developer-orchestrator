package core

import (
	"context"
	"testing"
	"time"
)

func TestNoopHook_Name(t *testing.T) {
	h := NoopHook{}
	if h.Name() != "noop" {
		t.Errorf("Name() = %q, want %q", h.Name(), "noop")
	}
}

func TestNoopHook_OnAgentStart(t *testing.T) {
	h := NoopHook{}
	if err := h.OnAgentStart(context.Background(), &LoopState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNoopHook_OnBeforeTurn(t *testing.T) {
	h := NoopHook{}
	msgs, err := h.OnBeforeTurn(context.Background(), &LoopState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil messages, got %v", msgs)
	}
}

func TestNoopHook_OnBeforeModel(t *testing.T) {
	h := NoopHook{}
	original := []Message{{Role: "user", Content: "hello"}}
	modified, err := h.OnBeforeModel(context.Background(), &LoopState{}, original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modified) != 1 {
		t.Fatalf("expected 1 message, got %d", len(modified))
	}
	if modified[0].Content != "hello" {
		t.Errorf("expected 'hello', got %q", modified[0].Content)
	}
}

func TestNoopHook_OnAfterModel(t *testing.T) {
	h := NoopHook{}
	resp := &GenerateResponse{Content: "response", Finish: "stop"}
	if err := h.OnAfterModel(context.Background(), &LoopState{}, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNoopHook_OnAfterToolCall(t *testing.T) {
	h := NoopHook{}
	if err := h.OnAfterToolCall(context.Background(), &LoopState{}, "bash", map[string]any{"cmd": "ls"}, `{"output":"ok"}`, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNoopHook_OnAgentEnd(t *testing.T) {
	h := NoopHook{}
	if err := h.OnAgentEnd(context.Background(), &LoopState{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoopState_Defaults(t *testing.T) {
	s := LoopState{}
	if s.SessionID != "" {
		t.Errorf("expected empty SessionID")
	}
	if s.Round != 0 {
		t.Errorf("expected Round 0")
	}
	if s.ConsecutiveFails != 0 {
		t.Errorf("expected ConsecutiveFails 0")
	}
	if s.FailCounts != nil {
		t.Errorf("expected nil FailCounts")
	}
}

func TestLoopState_Full(t *testing.T) {
	now := time.Now()
	s := LoopState{
		SessionID:        "sess_1",
		ProjectDir:       "/home/project",
		SandboxID:        "sb_1",
		Round:            3,
		ContentLength:    500,
		ToolResults:      []ToolResult{{ToolCallID: "c1", ToolName: "bash", Content: "ok"}},
		FailCounts:       map[string]int{"bash": 1},
		ConsecutiveFails: 2,
		TotalInputTokens:  100,
		TotalOutputTokens: 200,
		TurnInputTokens:   50,
		TurnOutputTokens:  100,
		TurnModel:         "llama",
		StartedAt:         now,
	}
	if s.SessionID != "sess_1" {
		t.Errorf("SessionID = %q, want %q", s.SessionID, "sess_1")
	}
	if s.Round != 3 {
		t.Errorf("Round = %d, want %d", s.Round, 3)
	}
	if len(s.ToolResults) != 1 {
		t.Errorf("expected 1 tool result, got %d", len(s.ToolResults))
	}
	if s.FailCounts["bash"] != 1 {
		t.Errorf("FailCounts[bash] = %d, want 1", s.FailCounts["bash"])
	}
	if s.ConsecutiveFails != 2 {
		t.Errorf("ConsecutiveFails = %d, want %d", s.ConsecutiveFails, 2)
	}
	if s.TurnModel != "llama" {
		t.Errorf("TurnModel = %q, want %q", s.TurnModel, "llama")
	}
	if !s.StartedAt.Equal(now) {
		t.Errorf("StartedAt mismatch")
	}
}

func TestGenerateResponse(t *testing.T) {
	usage := &StreamUsage{PromptTokens: 10, CompletionTokens: 20}
	resp := GenerateResponse{
		Content:   "Hello",
		Thinking:  "thinking...",
		ToolCalls: []ToolCallResponse{{ID: "call_1"}},
		Finish:    "stop",
		Usage:     usage,
	}
	if resp.Content != "Hello" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello")
	}
	if resp.Thinking != "thinking..." {
		t.Errorf("Thinking = %q, want %q", resp.Thinking, "thinking...")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.Finish != "stop" {
		t.Errorf("Finish = %q, want %q", resp.Finish, "stop")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("Usage.PromptTokens = %d, want %d", resp.Usage.PromptTokens, 10)
	}
}
