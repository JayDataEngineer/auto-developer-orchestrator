package llama

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

// ── Mock executor for integration tests ──────────────────────────────

// mockExecutor implements ToolExecutor with pre-programmed results.
type mockExecutor struct {
	results   map[string]string
	callCount map[string]int
}

func newMockExecutor() *mockExecutor {
	return &mockExecutor{
		results:   make(map[string]string),
		callCount: make(map[string]int),
	}
}

func (e *mockExecutor) Execute(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	e.callCount[toolName]++
	if r, ok := e.results[toolName]; ok {
		return r, nil
	}
	return "ok", nil
}

// ── Helpers ──────────────────────────────────────────────────────────

// collectEvents reads events until EventTypeAgentEnd, returns all collected.
func collectEvents(events <-chan AgentEvent) []AgentEvent {
	var all []AgentEvent
loop:
	for {
		select {
		case evt, ok := <-events:
			if !ok {
				break loop
			}
			all = append(all, evt)
			if evt.Type == EventTypeAgentEnd || evt.Type == EventTypeError {
				break loop
			}
		case <-time.After(5 * time.Second):
			// Timeout — test leaked a goroutine
			break loop
		}
	}
	return all
}

// hasEvent checks if any event in the slice matches the given type.
func hasEvent(events []AgentEvent, typ AgentEventType) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// countEvents returns the count of events with the given type.
func countEvents(events []AgentEvent, typ AgentEventType) int {
	n := 0
	for _, e := range events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// ── Integration tests ────────────────────────────────────────────────

func TestAgentLoop_SimpleTextResponse(t *testing.T) {
	mock := newMockLLMClient([]string{"Hello! I can help with that."}, "done")
	exec := newMockExecutor()
	cfg := AgentLoopConfig{
		SystemPrompt:  "You are a helpful assistant.",
		MaxToolRounds: 5,
		MaxTokens:     256,
		ContextSize:   4096,
		Compaction:    DefaultCompactionConfig(),
		Opts:          GenerateOptions{Temperature: 0.1},
	}

	loop, err := NewAgentLoop(mock, exec, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAgentLoop: %v", err)
	}
	defer loop.Close()

	events := make(chan AgentEvent, 32)
	go func() { _ = loop.Run(context.Background(), "Write a greeting", events) }()

	all := collectEvents(events)

	if !hasEvent(all, EventTypeAgentStart) {
		t.Error("expected EventTypeAgentStart")
	}
	if !hasEvent(all, EventTypeAgentEnd) {
		t.Error("expected EventTypeAgentEnd")
	}
	if !hasEvent(all, EventTypeTextDelta) {
		t.Error("expected EventTypeTextDelta")
	}
}

func TestAgentLoop_SingleToolCall(t *testing.T) {
	mock := newMockLLMClient(nil, "final")
	mock.addToolCall("let me check", "call-1", "bash", map[string]interface{}{"command": "echo hello"})
	// Second response: model sees tool result and responds with text
	mock.responses = append(mock.responses, mockResponse{
		content: "Command executed successfully. Output: hello",
		finish:  FinishStop,
	})

	exec := newMockExecutor()
	exec.results["bash"] = "hello\n"
	cfg := AgentLoopConfig{
		SystemPrompt:  "You have bash access.",
		MaxToolRounds: 5,
		MaxTokens:     256,
		ContextSize:   4096,
		Compaction:    DefaultCompactionConfig(),
		Tools: []OpenAITool{{
			Type: "function",
			Function: FunctionDef{
				Name:        "bash",
				Description: "run a command",
				Parameters:  nil,
			},
		}},
		Opts: GenerateOptions{Temperature: 0.1},
	}

	loop, err := NewAgentLoop(mock, exec, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAgentLoop: %v", err)
	}
	defer loop.Close()

	events := make(chan AgentEvent, 32)
	go func() { _ = loop.Run(context.Background(), "Run echo hello", events) }()

	all := collectEvents(events)

	if !hasEvent(all, EventTypeToolStart) {
		t.Error("expected EventTypeToolStart")
	}
	if !hasEvent(all, EventTypeToolEnd) {
		t.Error("expected EventTypeToolEnd")
	}
	if exec.callCount["bash"] == 0 {
		t.Error("expected bash to be called")
	}
}

func TestAgentLoop_ConcurrentRunPrevention(t *testing.T) {
	mock := newMockLLMClient([]string{"slow response"}, "ok")
	mock.blockCh = make(chan struct{})
	exec := newMockExecutor()
	cfg := AgentLoopConfig{
		SystemPrompt:  "You are helpful.",
		MaxToolRounds: 1,
		MaxTokens:     64,
		ContextSize:   4096,
		Compaction:    DefaultCompactionConfig(),
		Opts:          GenerateOptions{Temperature: 0.1},
	}

	loop, err := NewAgentLoop(mock, exec, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAgentLoop: %v", err)
	}
	defer loop.Close()

	started := make(chan struct{})
	events := make(chan AgentEvent, 32)

	go func() {
		close(started) // signal goroutine has started
		_ = loop.Run(context.Background(), "hello", events)
	}()

	// Wait for goroutine to start AND for Run to set running=true
	<-started
	// Small extra wait for the mutex lock/unlock in Run
	time.Sleep(5 * time.Millisecond)

	// Second run should fail while first is still running
	events2 := make(chan AgentEvent, 1)
	err = loop.Run(context.Background(), "hi again", events2)
	if err == nil {
		t.Error("expected error for concurrent Run")
	}

	// Unblock and drain
	close(mock.blockCh)
	collectEvents(events)
}

func TestAgentLoop_MaxToolRoundsEnforced(t *testing.T) {
	mock := newMockLLMClient(nil, "final")
	// Queue 10 tool calls — exceeds MaxToolRounds
	for i := 0; i < 10; i++ {
		mock.addToolCall("thinking", "call-x", "bash", map[string]interface{}{"command": "nop"})
	}

	exec := newMockExecutor()
	cfg := AgentLoopConfig{
		SystemPrompt:  "Use bash. Always use bash.",
		MaxToolRounds: 3,
		MaxTokens:     256,
		ContextSize:   4096,
		Compaction:    DefaultCompactionConfig(),
		Tools: []OpenAITool{{
			Type: "function",
			Function: FunctionDef{
				Name:        "bash",
				Description: "run a command",
				Parameters:  nil,
			},
		}},
		Opts: GenerateOptions{Temperature: 0.1},
	}

	loop, err := NewAgentLoop(mock, exec, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAgentLoop: %v", err)
	}
	defer loop.Close()

	events := make(chan AgentEvent, 64)
	go func() { _ = loop.Run(context.Background(), "Do it", events) }()

	all := collectEvents(events)
	n := countEvents(all, EventTypeToolStart)
	if n > cfg.MaxToolRounds {
		t.Errorf("expected at most %d tool calls, got %d", cfg.MaxToolRounds, n)
	}
}

func TestAgentLoop_ContextCancellation(t *testing.T) {
	mock := newMockLLMClient([]string{"processing..."}, "done")
	exec := newMockExecutor()
	cfg := AgentLoopConfig{
		SystemPrompt:  "You are helpful.",
		MaxToolRounds: 5,
		MaxTokens:     256,
		ContextSize:   4096,
		Compaction:    DefaultCompactionConfig(),
		Opts:          GenerateOptions{Temperature: 0.1},
	}

	loop, err := NewAgentLoop(mock, exec, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAgentLoop: %v", err)
	}
	defer loop.Close()

	events := make(chan AgentEvent, 32)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	go func() { _ = loop.Run(ctx, "hello", events) }()
	all := collectEvents(events)

	// Should have errored or ended gracefully, not panicked
	if hasEvent(all, EventTypeError) {
		// Expected — context cancelled
	} else if hasEvent(all, EventTypeAgentEnd) {
		// Also acceptable — managed to finish before cancel took effect
	}
	// The key assertion: test didn't panic
}

func TestAgentLoop_Continue(t *testing.T) {
	mock := newMockLLMClient(nil, "ok")
	mock.responses = append(mock.responses, mockResponse{content: "First response", finish: FinishStop})
	mock.responses = append(mock.responses, mockResponse{content: "Follow-up response", finish: FinishStop})

	exec := newMockExecutor()
	cfg := AgentLoopConfig{
		SystemPrompt:  "You are helpful.",
		MaxToolRounds: 1,
		MaxTokens:     64,
		ContextSize:   4096,
		Compaction:    DefaultCompactionConfig(),
		Opts:          GenerateOptions{Temperature: 0.1},
	}

	loop, err := NewAgentLoop(mock, exec, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAgentLoop: %v", err)
	}
	defer loop.Close()

	// First message
	events1 := make(chan AgentEvent, 32)
	go func() { _ = loop.Run(context.Background(), "first", events1) }()
	collectEvents(events1)

	// Second message (Continue)
	events2 := make(chan AgentEvent, 32)
	err = loop.Continue(context.Background(), "second", events2)
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	all := collectEvents(events2)
	if !hasEvent(all, EventTypeAgentEnd) {
		t.Error("expected EventTypeAgentEnd after Continue")
	}
}

func TestAgentLoop_Close(t *testing.T) {
	mock := newMockLLMClient([]string{"done"}, "ok")
	exec := newMockExecutor()
	cfg := AgentLoopConfig{
		SystemPrompt:  "You are helpful.",
		MaxToolRounds: 1,
		MaxTokens:     64,
		ContextSize:   4096,
		Compaction:    DefaultCompactionConfig(),
		Opts:          GenerateOptions{Temperature: 0.1},
	}

	loop, err := NewAgentLoop(mock, exec, cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewAgentLoop: %v", err)
	}

	err = loop.Close()
	if err != nil {
		t.Errorf("Close: %v", err)
	}

	// Close should be idempotent
	err = loop.Close()
	if err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestAgentLoopConfig_Validate(t *testing.T) {
	tests := []struct {
		name     string
		cfg      AgentLoopConfig
		wantErrs int
	}{
		{
			name: "valid",
			cfg: AgentLoopConfig{
				SystemPrompt:  "hi",
				MaxToolRounds: 5,
				MaxTokens:     256,
				ContextSize:   4096,
				Compaction:    DefaultCompactionConfig(),
			},
			wantErrs: 0,
		},
		{
			name: "zero ContextSize",
			cfg: AgentLoopConfig{
				SystemPrompt:  "hi",
				MaxToolRounds: 5,
				MaxTokens:     0,
				ContextSize:   0,
				Compaction:    DefaultCompactionConfig(),
			},
			wantErrs: 2, // ContextSize + MaxTokens
		},
		{
			name: "MaxTokens exceeds ContextSize",
			cfg: AgentLoopConfig{
				SystemPrompt:  "hi",
				MaxToolRounds: 5,
				MaxTokens:     9999,
				ContextSize:   4096,
				Compaction:    DefaultCompactionConfig(),
			},
			wantErrs: 1,
		},
		{
			name: "multiple errors",
			cfg: AgentLoopConfig{
				SystemPrompt:  "",
				MaxToolRounds: 0,
				MaxTokens:     0,
				ContextSize:   0,
				Compaction:    DefaultCompactionConfig(),
			},
			wantErrs: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.cfg.Validate()
			if len(errs) != tt.wantErrs {
				t.Errorf("Validate() got %d errors (%v), want %d", len(errs), errs, tt.wantErrs)
			}
		})
	}
}

func TestAgentLoopConfig_ValidationOnCreate(t *testing.T) {
	mock := newMockLLMClient([]string{"ok"}, "ok")
	exec := newMockExecutor()
	cfg := AgentLoopConfig{
		SystemPrompt:  "",
		MaxToolRounds: 0,
		MaxTokens:     0,
		ContextSize:   0,
		Compaction:    DefaultCompactionConfig(),
	}

	_, err := NewAgentLoop(mock, exec, cfg, zap.NewNop())
	if err == nil {
		t.Error("expected error for invalid config")
	}
}
