package core

import (
	"testing"
)

func TestEventTypeConstants(t *testing.T) {
	if EventTypeTextDelta != "text_delta" {
		t.Errorf("EventTypeTextDelta = %q, want %q", EventTypeTextDelta, "text_delta")
	}
	if EventTypeThinkingDelta != "thinking_delta" {
		t.Errorf("EventTypeThinkingDelta = %q, want %q", EventTypeThinkingDelta, "thinking_delta")
	}
	if EventTypeToolStart != "tool_execution_start" {
		t.Errorf("EventTypeToolStart = %q, want %q", EventTypeToolStart, "tool_execution_start")
	}
	if EventTypeToolEnd != "tool_execution_end" {
		t.Errorf("EventTypeToolEnd = %q, want %q", EventTypeToolEnd, "tool_execution_end")
	}
	if EventTypeApprovalRequest != "approval_request" {
		t.Errorf("EventTypeApprovalRequest = %q, want %q", EventTypeApprovalRequest, "approval_request")
	}
	if EventTypeError != "error" {
		t.Errorf("EventTypeError = %q, want %q", EventTypeError, "error")
	}
}

func TestAgentEventData_Fields(t *testing.T) {
	data := AgentEventData{
		Text:     "hello",
		ToolName: "bash",
		ToolID:   "t-1",
		ToolArgs: map[string]any{"cmd": "echo hi"},
		Result:   "output",
		Error:    "",
		Round:    1,
	}
	if data.Text != "hello" {
		t.Errorf("Text = %q, want %q", data.Text, "hello")
	}
	if data.ToolName != "bash" {
		t.Errorf("ToolName = %q, want %q", data.ToolName, "bash")
	}
	if data.ToolID != "t-1" {
		t.Errorf("ToolID = %q, want %q", data.ToolID, "t-1")
	}
	if data.Round != 1 {
		t.Errorf("Round = %d, want %d", data.Round, 1)
	}
}

func TestChatEventTypeValues(t *testing.T) {
	if ChatEventContent != 0 {
		t.Errorf("ChatEventContent should be 0 (iota), got %d", ChatEventContent)
	}
	if ChatEventThinking != 1 {
		t.Errorf("ChatEventThinking should be 1 (iota), got %d", ChatEventThinking)
	}
	if ChatEventToolChunk != 2 {
		t.Errorf("ChatEventToolChunk should be 2 (iota), got %d", ChatEventToolChunk)
	}
	if ChatEventDone != 3 {
		t.Errorf("ChatEventDone should be 3 (iota), got %d", ChatEventDone)
	}
	if ChatEventError != 4 {
		t.Errorf("ChatEventError should be 4 (iota), got %d", ChatEventError)
	}
}

func TestAgentEvent_WrapsData(t *testing.T) {
	evt := AgentEvent{
		Type: EventTypeTextDelta,
		Data: AgentEventData{
			Text: "hello",
		},
	}
	if string(evt.Type) != "text_delta" {
		t.Errorf("expected type text_delta, got %q", evt.Type)
	}
}

func TestSubscriberKey(t *testing.T) {
	var key SubscriberKey
	if key != (SubscriberKey{}) {
		t.Error("SubscriberKey should be a zero-value struct")
	}
}
