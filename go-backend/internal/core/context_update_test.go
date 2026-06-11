package core

import (
	"context"
	"testing"
)

// TestContextUpdateData_TypeAndFields proves the payload struct works correctly.
func TestContextUpdateData_TypeAndFields(t *testing.T) {
	cud := ContextUpdateData{
		ContextTokens: 15000,
		ContextWindow: 32768,
		ContextUtil:   0.4577,
		Round:         3,
	}
	if cud.ContextTokens != 15000 {
		t.Errorf("ContextTokens = %d, want 15000", cud.ContextTokens)
	}
	if cud.ContextWindow != 32768 {
		t.Errorf("ContextWindow = %d, want 32768", cud.ContextWindow)
	}
	if cud.Round != 3 {
		t.Errorf("Round = %d, want 3", cud.Round)
	}
}

// TestEventTypeContextUpdate proves the constant has the correct wire value.
func TestEventTypeContextUpdate(t *testing.T) {
	if EventTypeContextUpdate != "context_update" {
		t.Errorf("EventTypeContextUpdate = %q, want %q", EventTypeContextUpdate, "context_update")
	}
}

// TestContextUpdateEvent_EmittedAfterLLMCall proves the agent loop emits
// a context_update event after every LLM API call with real usage data.
// This is the critical fix: before, the TUI indicator only updated at agent_end.
func TestContextUpdateEvent_EmittedAfterLLMCall(t *testing.T) {
	// Simulate a single-round agent call that returns with usage data
	ch := make(chan ChatEvent, 10)
	ch <- ChatEvent{Type: ChatEventDone, Finish: "stop", Usage: &StreamUsage{
		PromptTokens:     8500,
		CompletionTokens: 200,
	}}
	close(ch)

	provider := &mockProvider{eventsCh: ch, ctxSize: 32768}
	session := &mockSession{id: "sess-ctx-test", ctx: []Message{
		{Role: "system", Content: "Be helpful."},
	}}
	executor := &mockExecutor{}

	l := NewAgentLoop(provider, executor, session, AgentLoopConfig{
		SystemPrompt: "Be helpful.",
	})

	subscriber := make(chan AgentEvent, 50)
	err := l.Run(context.Background(), "test context metrics", subscriber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(subscriber)

	// Collect all events
	var events []AgentEvent
	for evt := range subscriber {
		events = append(events, evt)
	}

	// PROVE: context_update event exists in the stream
	var contextUpdates []ContextUpdateData
	for _, evt := range events {
		if evt.Type == EventTypeContextUpdate {
			cud, ok := evt.Data.(ContextUpdateData)
			if !ok {
				t.Fatalf("context_update payload is %T, not ContextUpdateData", evt.Data)
			}
			contextUpdates = append(contextUpdates, cud)
		}
	}

	if len(contextUpdates) == 0 {
		t.Fatal("no context_update events emitted — indicator would be stale during this round")
	}

	// PROVE: the event has real token data from the API, not zero
	cu := contextUpdates[0]
	if cu.ContextTokens != 8500 {
		t.Errorf("ContextTokens = %d, want 8500 (real API prompt_tokens)", cu.ContextTokens)
	}
	if cu.ContextWindow != 32768 {
		t.Errorf("ContextWindow = %d, want 32768 (from provider)", cu.ContextWindow)
	}
	if cu.Round != 1 {
		t.Errorf("Round = %d, want 1", cu.Round)
	}
}

// TestContextUpdateEvent_MultiRound proves context_update fires after EACH
// round in a multi-round tool loop, not just at agent_end.
func TestContextUpdateEvent_MultiRound(t *testing.T) {
	// Round 1: model wants to call a tool
	ch1 := make(chan ChatEvent, 10)
	ch1 <- ChatEvent{Type: ChatEventToolChunk, Deltas: []ToolCallDelta{{
		ID:    "tc-1",
		Index: 0,
		Function: FunctionCallDelta{Name: "bash", Arguments: `{"command":"ls"}`},
	}}}
	ch1 <- ChatEvent{Type: ChatEventDone, Finish: "tool_calls", Usage: &StreamUsage{
		PromptTokens:     5000,
		CompletionTokens: 50,
	}}
	close(ch1)

	// Round 2: model responds after tool result
	ch2 := make(chan ChatEvent, 10)
	ch2 <- ChatEvent{Type: ChatEventContent, Content: "Here are the files."}
	ch2 <- ChatEvent{Type: ChatEventDone, Finish: "stop", Usage: &StreamUsage{
		PromptTokens:     12000,
		CompletionTokens: 100,
	}}
	close(ch2)

	// We need a custom mock that returns different channels per call
	customProvider := &multiCallProvider{
		channels: []chan ChatEvent{ch1, ch2},
		ctxSize:  32768,
	}

	session := &mockSession{id: "sess-multi", ctx: []Message{
		{Role: "system", Content: "Be helpful."},
	}}
	executor := &mockExecutor{}

	l := NewAgentLoop(customProvider, executor, session, AgentLoopConfig{
		SystemPrompt:  "Be helpful.",
		MaxToolRounds: 5,
	})

	subscriber := make(chan AgentEvent, 100)
	err := l.Run(context.Background(), "list files", subscriber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(subscriber)

	// Collect context_update events
	var contextUpdates []ContextUpdateData
	for evt := range subscriber {
		if evt.Type == EventTypeContextUpdate {
			cud, ok := evt.Data.(ContextUpdateData)
			if !ok {
				t.Fatalf("context_update payload is %T", evt.Data)
			}
			contextUpdates = append(contextUpdates, cud)
		}
	}

	// PROVE: we got context_update for EACH round (2 rounds = 2 updates)
	if len(contextUpdates) < 2 {
		t.Fatalf("expected at least 2 context_update events (one per round), got %d", len(contextUpdates))
	}

	// PROVE: round 1 has 5000 tokens (before tool execution)
	if contextUpdates[0].ContextTokens != 5000 {
		t.Errorf("round 1 ContextTokens = %d, want 5000", contextUpdates[0].ContextTokens)
	}
	if contextUpdates[0].Round != 1 {
		t.Errorf("round 1 Round = %d, want 1", contextUpdates[0].Round)
	}

	// PROVE: round 2 has 12000 tokens (after tool result added)
	if contextUpdates[1].ContextTokens != 12000 {
		t.Errorf("round 2 ContextTokens = %d, want 12000", contextUpdates[1].ContextTokens)
	}
	if contextUpdates[1].Round != 2 {
		t.Errorf("round 2 Round = %d, want 2", contextUpdates[1].Round)
	}
}

// TestContextUpdateEvent_NoEmitWithoutUsage proves we don't emit context_update
// when the provider doesn't return usage data (avoids misleading zero values).
func TestContextUpdateEvent_NoEmitWithoutUsage(t *testing.T) {
	// Done event with NO usage data
	ch := make(chan ChatEvent, 10)
	ch <- ChatEvent{Type: ChatEventDone, Finish: "stop", Usage: nil}
	close(ch)

	provider := &mockProvider{eventsCh: ch, ctxSize: 32768}
	session := &mockSession{id: "sess-no-usage", ctx: []Message{
		{Role: "system", Content: "Be helpful."},
	}}
	executor := &mockExecutor{}

	l := NewAgentLoop(provider, executor, session, AgentLoopConfig{
		SystemPrompt: "Be helpful.",
	})

	subscriber := make(chan AgentEvent, 50)
	err := l.Run(context.Background(), "test", subscriber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(subscriber)

	// PROVE: no context_update events when usage is nil
	for evt := range subscriber {
		if evt.Type == EventTypeContextUpdate {
			t.Fatal("context_update should NOT be emitted when provider returns no usage")
		}
	}
}

// TestContextUpdateEvent_ContextMetricsFunc proves that when ContextMetricsFunc
// returns a higher estimate than API tokens, the context_update uses that value.
// This handles providers (DeepSeek) that undercount due to prompt caching.
func TestContextUpdateEvent_ContextMetricsFunc(t *testing.T) {
	ch := make(chan ChatEvent, 10)
	ch <- ChatEvent{Type: ChatEventDone, Finish: "stop", Usage: &StreamUsage{
		PromptTokens: 3000, // API says 3K (undercounted due to caching)
		CompletionTokens: 100,
	}}
	close(ch)

	provider := &mockProvider{eventsCh: ch, ctxSize: 32768}
	session := &mockSession{id: "sess-cmfunc", ctx: []Message{
		{Role: "system", Content: "Be helpful."},
	}}
	executor := &mockExecutor{}

	l := NewAgentLoop(provider, executor, session, AgentLoopConfig{
		SystemPrompt: "Be helpful.",
		ContextMetricsFunc: func() ContextMetricsSnapshot {
			return ContextMetricsSnapshot{
				EstimatedTokens: 12000, // Context manager says 12K (more accurate)
				ContextSize:     32768,
			}
		},
	})

	subscriber := make(chan AgentEvent, 50)
	err := l.Run(context.Background(), "test", subscriber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(subscriber)

	// Find the context_update event
	for evt := range subscriber {
		if evt.Type == EventTypeContextUpdate {
			cud := evt.Data.(ContextUpdateData)
			// PROVE: takes the max of API tokens and context manager estimate
			if cud.ContextTokens != 12000 {
				t.Errorf("ContextTokens = %d, want 12000 (max of API=3000, CM=12000)", cud.ContextTokens)
			}
			return
		}
	}
	t.Fatal("no context_update event found")
}

// ── Multi-call provider mock ──

type multiCallProvider struct {
	channels  []chan ChatEvent
	callIndex int
	ctxSize   int
}

func (m *multiCallProvider) StreamChat(ctx context.Context, messages []Message, tools []OpenAITool, opts GenerateOptions) (<-chan ChatEvent, error) {
	if m.callIndex >= len(m.channels) {
		ch := make(chan ChatEvent, 1)
		ch <- ChatEvent{Type: ChatEventDone, Finish: "stop"}
		close(ch)
		return ch, nil
	}
	ch := m.channels[m.callIndex]
	m.callIndex++
	return ch, nil
}

func (m *multiCallProvider) ModelName() string { return "multi-call-mock" }

func (m *multiCallProvider) ContextSize() int {
	if m.ctxSize == 0 {
		return 32768
	}
	return m.ctxSize
}
