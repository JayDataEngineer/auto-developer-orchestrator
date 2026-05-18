package core

import (
	"context"
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
	if EventTypeDecisionRequest != "decision_request" {
		t.Errorf("EventTypeDecisionRequest = %q, want %q", EventTypeDecisionRequest, "decision_request")
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

func TestSubscriberKeyContextRoundTrip(t *testing.T) {
	// Verify that storing chan<- AgentEvent in context and extracting
	// with the correct type assertion works. This was a real bug:
	// context stored chan<- T but assertion used chan T (different types).
	ch := make(chan AgentEvent, 8)
	var sendOnly chan<- AgentEvent = ch

	ctx := context.Background()
	ctx = context.WithValue(ctx, SubscriberKey{}, sendOnly)

	// Must assert to chan<- AgentEvent, NOT chan AgentEvent
	extracted, ok := ctx.Value(SubscriberKey{}).(chan<- AgentEvent)
	if !ok {
		t.Fatal("type assertion to chan<- AgentEvent failed — subscriber would be nil in tools")
	}
	if extracted == nil {
		t.Fatal("extracted subscriber is nil")
	}

	// Verify SendEvent works through the extracted channel
	SendEvent(extracted, AgentEvent{Type: EventTypeTextDelta, Data: AgentEventData{Text: "test"}})
	select {
	case evt := <-ch:
		if evt.Data.Text != "test" {
			t.Errorf("got %q, want %q", evt.Data.Text, "test")
		}
	default:
		t.Fatal("event not received on channel")
	}
}
