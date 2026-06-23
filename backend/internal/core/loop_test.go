package core

import (
	"context"
	"errors"
	"testing"
)

var errTestError = errors.New("test error")

func TestNewAgentLoop_Defaults(t *testing.T) {
	l := NewAgentLoop(nil, nil, nil, AgentLoopConfig{})
	if l.config.MaxRetriesPerTool != 3 {
		t.Errorf("expected default MaxRetriesPerTool 3, got %d", l.config.MaxRetriesPerTool)
	}
	if l.config.MaxConsecutiveFails != 5 {
		t.Errorf("expected default MaxConsecutiveFails 5, got %d", l.config.MaxConsecutiveFails)
	}
	if l.config.ToolExecTimeoutSec != 0 {
		t.Errorf("expected default ToolExecTimeoutSec 0 (no timeout), got %d", l.config.ToolExecTimeoutSec)
	}
	if l.config.MaxProviderRetries != 5 {
		t.Errorf("expected default MaxProviderRetries 5, got %d", l.config.MaxProviderRetries)
	}
}

func TestNewAgentLoop_CustomValues(t *testing.T) {
	l := NewAgentLoop(nil, nil, nil, AgentLoopConfig{
		MaxRetriesPerTool:   5,
		MaxConsecutiveFails: 10,
		ToolExecTimeoutSec:  60,
		MaxProviderRetries:  3,
	})
	if l.config.MaxRetriesPerTool != 5 {
		t.Errorf("expected MaxRetriesPerTool 5, got %d", l.config.MaxRetriesPerTool)
	}
	if l.config.MaxConsecutiveFails != 10 {
		t.Errorf("expected MaxConsecutiveFails 10, got %d", l.config.MaxConsecutiveFails)
	}
	if l.config.ToolExecTimeoutSec != 60 {
		t.Errorf("expected ToolExecTimeoutSec 60, got %d", l.config.ToolExecTimeoutSec)
	}
	if l.config.MaxProviderRetries != 3 {
		t.Errorf("expected MaxProviderRetries 3, got %d", l.config.MaxProviderRetries)
	}
}

func TestAgentLoop_IsRunning(t *testing.T) {
	l := NewAgentLoop(nil, nil, nil, AgentLoopConfig{})
	if l.IsRunning() {
		t.Error("new loop should not be running")
	}
}

func TestAgentLoop_Abort_Idempotent(t *testing.T) {
	l := NewAgentLoop(nil, nil, nil, AgentLoopConfig{})
	// Should not panic when aborting a non-running loop
	// (cancel is nil, so Abort should handle it gracefully)
	l.Abort()
}

func TestAgentLoop_Close_NilSession(t *testing.T) {
	l := NewAgentLoop(nil, nil, nil, AgentLoopConfig{})
	err := l.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentLoop_Close_WithSession(t *testing.T) {
	sess := &mockSession{id: "test-session"}
	l := NewAgentLoop(nil, nil, sess, AgentLoopConfig{})
	err := l.Close()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAgentLoop_Close_Idempotent(t *testing.T) {
	l := NewAgentLoop(nil, nil, nil, AgentLoopConfig{})
	testutil_AssertNoError(t, l.Close())
	testutil_AssertNoError(t, l.Close())
}

func testutil_AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// mockSession implements core.Session for testing
type mockSession struct {
	id        string
	messages  []Message
	closed    bool
	ctx       []Message // cached build context
}

func (m *mockSession) ID() string { return m.id }
func (m *mockSession) AppendMessage(msg Message) error {
	m.messages = append(m.messages, msg)
	m.ctx = buildTestContext(m.messages)
	return nil
}
func (m *mockSession) BuildContext(ctx context.Context) ([]Message, error) {
	return m.ctx, nil
}
func (m *mockSession) Navigate(nodeID string) error { return nil }
func (m *mockSession) Branch(label string) (string, error) { return "branch", nil }
func (m *mockSession) Fork(nodeID string) (Session, error) { return nil, nil }
func (m *mockSession) Compact(ctx context.Context, summary string) (string, error) {
	return "", nil
}
func (m *mockSession) TruncateToolResults(keep int) (int, error) { return 0, nil }
func (m *mockSession) ReplaceToolResults(replace func(i int, name, content string) string, keep int) (int, error) {
	return 0, nil
}
func (m *mockSession) GetTree() *TreeNode { return nil }
func (m *mockSession) GetCurrentNode() string { return "root" }
func (m *mockSession) GetUserCheckpoints() []Checkpoint { return nil }
func (m *mockSession) Close() error {
	m.closed = true
	return nil
}

func buildTestContext(msgs []Message) []Message {
	result := make([]Message, len(msgs))
	copy(result, msgs)
	return result
}

// mockProvider implements core.LLMProvider for testing
type mockProvider struct {
	modelName  string
	ctxSize    int
	eventsCh   chan ChatEvent
	eventsErr  error
	callCount  int
}

func (m *mockProvider) StreamChat(ctx context.Context, messages []Message, tools []OpenAITool, opts GenerateOptions) (<-chan ChatEvent, error) {
	m.callCount++
	if m.eventsErr != nil {
		return nil, m.eventsErr
	}
	return m.eventsCh, nil
}

func (m *mockProvider) ModelName() string {
	if m.modelName == "" {
		return "mock-model"
	}
	return m.modelName
}

func (m *mockProvider) ContextSize() int {
	if m.ctxSize == 0 {
		return 4096
	}
	return m.ctxSize
}

// mockExecutor implements core.ToolExecutor for testing
type mockExecutor struct {
	results  map[string]func(args map[string]any) (any, error) // tool name -> handler
	callLog  []string
}

func (m *mockExecutor) Execute(ctx context.Context, toolName string, args map[string]any) (any, error) {
	m.callLog = append(m.callLog, toolName)
	if m.results != nil {
		if fn, ok := m.results[toolName]; ok {
			return fn(args)
		}
	}
	return map[string]any{"output": "mock result"}, nil
}

func TestAgentLoop_Run_AppendsMessages(t *testing.T) {
	ch := make(chan ChatEvent, 10)
	ch <- ChatEvent{Type: ChatEventDone, Finish: "stop"}
	close(ch)

	provider := &mockProvider{eventsCh: ch}
	session := &mockSession{id: "sess-1", ctx: []Message{
		{Role: "system", Content: "Be helpful."},
	}}
	executor := &mockExecutor{}

	l := NewAgentLoop(provider, executor, session, AgentLoopConfig{
		SystemPrompt:  "Be helpful.",
		MaxToolRounds: 0, // unlimited
	})

	subscriber := make(chan AgentEvent, 20)
	err := l.Run(context.Background(), "hello", subscriber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(subscriber)

	// Verify events were emitted
	var eventTypes []AgentEventType
	for evt := range subscriber {
		eventTypes = append(eventTypes, evt.Type)
	}

	foundStart := false
	foundEnd := false
	for _, et := range eventTypes {
		if et == EventTypeAgentStart {
			foundStart = true
		}
		if et == EventTypeAgentEnd {
			foundEnd = true
		}
	}
	if !foundStart {
		t.Error("expected AgentStart event")
	}
	if !foundEnd {
		t.Error("expected AgentEnd event")
	}

	// Verify session has system + user + assistant messages
	if len(session.messages) < 2 {
		t.Fatalf("expected at least 2 messages in session, got %d", len(session.messages))
	}
	// First appended message should be user
	if session.messages[0].Role != "user" || session.messages[0].Content != "hello" {
		t.Errorf("expected user message 'hello', got role=%q content=%q", session.messages[0].Role, session.messages[0].Content)
	}
	// Second should be assistant
	if session.messages[1].Role != "assistant" {
		t.Errorf("expected assistant message, got role=%q", session.messages[1].Role)
	}
}

func TestAgentLoop_Continue(t *testing.T) {
	ch := make(chan ChatEvent, 10)
	ch <- ChatEvent{Type: ChatEventDone, Finish: "stop"}
	close(ch)

	provider := &mockProvider{eventsCh: ch}
	session := &mockSession{id: "sess-1", ctx: []Message{
		{Role: "system", Content: "Be helpful."},
	}}
	executor := &mockExecutor{}

	l := NewAgentLoop(provider, executor, session, AgentLoopConfig{
		SystemPrompt:  "Be helpful.",
		MaxToolRounds: 10,
	})

	subscriber := make(chan AgentEvent, 20)
	err := l.Continue(context.Background(), "follow up", subscriber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(subscriber)

	// Continue should NOT prepend system prompt
	// Messages: user (follow up) + assistant (from loop response)
	if len(session.messages) != 2 {
		t.Fatalf("expected 2 messages (user+assistant), got %d", len(session.messages))
	}
	if session.messages[0].Content != "follow up" {
		t.Errorf("expected 'follow up', got %q", session.messages[0].Content)
	}
	if session.messages[1].Role != "assistant" {
		t.Errorf("expected assistant message, got role=%q", session.messages[1].Role)
	}
}

func TestAgentLoop_Run_AlreadyRunning(t *testing.T) {
	ch := make(chan ChatEvent, 10)
	close(ch)

	l := NewAgentLoop(&mockProvider{eventsCh: ch}, nil, &mockSession{id: "sess"}, AgentLoopConfig{})

	// Start first run
	sub1 := make(chan AgentEvent, 20)
	err1 := l.Run(context.Background(), "first", sub1)
	if err1 != nil {
		t.Fatalf("first Run should succeed: %v", err1)
	}
	close(sub1)

	// Second run should succeed since first is done
	sub2 := make(chan AgentEvent, 20)
	err2 := l.Run(context.Background(), "second", sub2)
	if err2 != nil {
		t.Fatalf("second Run should succeed: %v", err2)
	}
	close(sub2)
}

func TestAgentLoop_Continue_AlreadyRunning(t *testing.T) {
	l := NewAgentLoop(nil, nil, nil, AgentLoopConfig{})

	// Can't Continue without a prior Run (no session context with user msg)
	// but the "already running" check should work:
	l.mu.Lock()
	l.running = true
	l.mu.Unlock()

	err := l.Continue(context.Background(), "msg", make(chan AgentEvent, 10))
	if err == nil {
		t.Fatal("expected error when loop is already running")
	}
	if err.Error() != "agent loop already running" {
		t.Errorf("expected 'agent loop already running', got %v", err)
	}
}

func TestAgentLoop_Run_ProviderError(t *testing.T) {
	provider := &mockProvider{eventsErr: errTestError}
	session := &mockSession{id: "sess", ctx: []Message{
		{Role: "system", Content: "sys"},
	}}

	l := NewAgentLoop(provider, nil, session, AgentLoopConfig{
		SystemPrompt:  "sys",
		MaxProviderRetries: 0,
	})

	subscriber := make(chan AgentEvent, 20)
	err := l.Run(context.Background(), "hello", subscriber)
	if err == nil {
		t.Fatal("expected error from provider")
	}
	close(subscriber)
}

func TestAgentLoop_WithToolCalls(t *testing.T) {
	// Simulate model generating a tool call, then finishing
	toolCallJSON := `{"name":"bash","arguments":"{\"cmd\":\"ls\"}"}`
	ch := make(chan ChatEvent, 10)
	ch <- ChatEvent{
		Type: ChatEventDone,
		Finish: "tool_calls",
		Deltas: []ToolCallDelta{
			{
				Index: 0,
				ID:    "call_1",
				Type:  "function",
				Function: FunctionCallDelta{
					Name:      "bash",
					Arguments: toolCallJSON,
				},
			},
		},
	}
	close(ch)

	session := &mockSession{id: "sess", ctx: []Message{
		{Role: "system", Content: "You are a helpful assistant."},
	}}

	executor := &mockExecutor{
		results: map[string]func(args map[string]any) (any, error){
			"bash": func(args map[string]any) (any, error) {
				return map[string]any{"output": "ok"}, nil
			},
		},
	}

	l := NewAgentLoop(&mockProvider{eventsCh: ch}, executor, session, AgentLoopConfig{
		SystemPrompt:  "sys",
		MaxToolRounds: 5,
	})

	subscriber := make(chan AgentEvent, 20)
	err := l.Run(context.Background(), "list files", subscriber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(subscriber)

	// Verify tool was called
	if len(executor.callLog) != 1 || executor.callLog[0] != "bash" {
		t.Errorf("expected bash tool call, got %v", executor.callLog)
	}

	// Verify tool result was appended to session
	foundToolResult := false
	for _, m := range session.messages {
		if m.Role == "tool" {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Error("expected tool result message in session")
	}
}

func TestAgentLoop_HooksRun(t *testing.T) {
	ch := make(chan ChatEvent, 10)
	ch <- ChatEvent{Type: ChatEventDone, Finish: "stop"}
	close(ch)

	session := &mockSession{id: "sess", ctx: []Message{
		{Role: "system", Content: "sys"},
	}}

	var hookCalls []string
	hook := &recordingHook{onStart: func() { hookCalls = append(hookCalls, "start") }}

	l := NewAgentLoop(&mockProvider{eventsCh: ch}, &mockExecutor{}, session, AgentLoopConfig{
		SystemPrompt:  "sys",
		MaxToolRounds: 5,
		Hooks:         []LoopHook{hook},
	})

	subscriber := make(chan AgentEvent, 20)
	err := l.Run(context.Background(), "hi", subscriber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(subscriber)
}

type recordingHook struct {
	NoopHook
	onStart func()
}

func (h *recordingHook) Name() string { return "recorder" }
func (h *recordingHook) OnAgentStart(ctx context.Context, state *LoopState) error {
	if h.onStart != nil {
		h.onStart()
	}
	return nil
}

func TestAgentLoop_Session(t *testing.T) {
	l := NewAgentLoop(nil, nil, nil, AgentLoopConfig{})
	if l.Session() != nil {
		t.Error("expected nil session for uninitialized loop")
	}
}

func TestReorderSystemFirst_AlreadyFirst(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "assistant1"},
		{Role: "user", Content: "user2"},
	}
	result := reorderSystemFirst(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("first message should be system, got %q", result[0].Role)
	}
	if result[1].Role != "user" || result[1].Content != "user1" {
		t.Errorf("expected second message user1, got %q", result[1].Content)
	}
}

func TestReorderSystemFirst_MovesToFront(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "user1"},
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "user2"},
	}
	result := reorderSystemFirst(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0].Role != "system" || result[0].Content != "sys" {
		t.Errorf("first message should be system, got role=%q content=%q", result[0].Role, result[0].Content)
	}
	if result[1].Content != "user1" {
		t.Errorf("second message should be user1, got %q", result[1].Content)
	}
	if result[2].Content != "user2" {
		t.Errorf("third message should be user2, got %q", result[2].Content)
	}
}

func TestReorderSystemFirst_MultipleSystem(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "user1"},
		{Role: "system", Content: "sys1"},
		{Role: "assistant", Content: "assistant1"},
		{Role: "system", Content: "sys2"},
	}
	result := reorderSystemFirst(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	for i, m := range result[:2] {
		if m.Role != "system" {
			t.Errorf("result[%d] should be system, got %q", i, m.Role)
		}
	}
}

func TestReorderSystemFirst_NoSystem(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "assistant1"},
	}
	result := reorderSystemFirst(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
}

func TestReorderSystemFirst_Empty(t *testing.T) {
	result := reorderSystemFirst(nil)
	if result != nil {
		t.Errorf("expected nil for nil input")
	}
}

func TestReorderSystemFirst_SingleSystem(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
	}
	result := reorderSystemFirst(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Content != "sys" {
		t.Errorf("expected sys, got %q", result[0].Content)
	}
}

func TestDeduplicateToolCalls_ByID(t *testing.T) {
	calls := []ToolCallResponse{
		{ID: "call_1", Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"echo hi"}`}},
		{ID: "call_1", Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"echo hi"}`}},
		{ID: "call_2", Function: FunctionCallData{Name: "read", Arguments: `{"file":"f"}`}},
	}
	result := deduplicateToolCalls(calls)
	if len(result) != 2 {
		t.Fatalf("expected 2 deduplicated calls, got %d", len(result))
	}
	if result[0].ID != "call_1" {
		t.Errorf("expected first call id call_1, got %q", result[0].ID)
	}
	if result[1].ID != "call_2" {
		t.Errorf("expected second call id call_2, got %q", result[1].ID)
	}
}

func TestDeduplicateToolCalls_ByNameArgs(t *testing.T) {
	// Two calls with same name+args but different IDs must BOTH be kept.
	// Dropping by name+args leaves the assistant message referencing an ID
	// that has no tool result → HTTP 400 on strict providers (DeepSeek, OpenAI).
	calls := []ToolCallResponse{
		{ID: "call_a", Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"ls"}`}},
		{ID: "call_b", Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"ls"}`}},
		{ID: "call_c", Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"pwd"}`}},
	}
	result := deduplicateToolCalls(calls)
	if len(result) != 3 {
		t.Fatalf("expected 3 distinct-ID calls preserved, got %d", len(result))
	}
}

func TestDeduplicateToolCalls_AllDifferent(t *testing.T) {
	calls := []ToolCallResponse{
		{ID: "call_1", Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"ls"}`}},
		{ID: "call_2", Function: FunctionCallData{Name: "read", Arguments: `{"file":"f"}`}},
	}
	result := deduplicateToolCalls(calls)
	if len(result) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(result))
	}
}

func TestDeduplicateToolCalls_EmptyIDButDifferent(t *testing.T) {
	calls := []ToolCallResponse{
		{Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"ls"}`}},
		{Function: FunctionCallData{Name: "bash", Arguments: `{"cmd":"pwd"}`}},
	}
	result := deduplicateToolCalls(calls)
	if len(result) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(result))
	}
}

func TestDeduplicateToolCalls_Empty(t *testing.T) {
	result := deduplicateToolCalls(nil)
	if result == nil {
		t.Error("expected non-nil slice for nil input")
	}
}
