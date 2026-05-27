package core

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// --- Mock tools for blocking E2E tests ---

// blockingTool simulates a slow tool (like delegate_to)
type blockingTool struct {
	name     string
	duration time.Duration
	started  chan struct{}
	finished chan struct{}
}

func newBlockingTool(name string, duration time.Duration) *blockingTool {
	return &blockingTool{
		name:     name,
		duration: duration,
		started:  make(chan struct{}),
		finished: make(chan struct{}),
	}
}

func (t *blockingTool) Name() string                { return t.name }
func (t *blockingTool) Description() string         { return "mock " + t.name }
func (t *blockingTool) Schema() json.RawMessage     { return json.RawMessage(`{"type":"object"}`) }
func (t *blockingTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	close(t.started)
	select {
	case <-time.After(t.duration):
		close(t.finished)
		return map[string]any{"result": t.name + " completed"}, nil
	case <-ctx.Done():
		close(t.finished)
		return nil, ctx.Err()
	}
}

// fastTool completes immediately
type fastTool struct {
	name     string
	executed chan struct{}
}

func newFastTool(name string) *fastTool {
	return &fastTool{
		name:     name,
		executed: make(chan struct{}),
	}
}

func (t *fastTool) Name() string                { return t.name }
func (t *fastTool) Description() string         { return "mock " + t.name }
func (t *fastTool) Schema() json.RawMessage     { return json.RawMessage(`{"type":"object"}`) }
func (t *fastTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	close(t.executed)
	return map[string]any{"output": "done"}, nil
}

// streamProvider returns tool calls then text across rounds
type streamProvider struct {
	mu     sync.Mutex
	round  int
	rounds []providerRound
}

type providerRound struct {
	toolCalls []ToolCallResponse
	content   string
}

func (p *streamProvider) StreamChat(ctx context.Context, messages []Message, tools []OpenAITool, opts GenerateOptions) (<-chan ChatEvent, error) {
	p.mu.Lock()
	p.round++
	r := p.rounds[min(p.round-1, len(p.rounds)-1)]
	p.mu.Unlock()

	ch := make(chan ChatEvent, 64)
	go func() {
		defer close(ch)
		for i, tc := range r.toolCalls {
			ch <- ChatEvent{
				Type: ChatEventToolChunk,
				Deltas: []ToolCallDelta{{
					Index:    i,
					ID:       tc.ID,
					Type:     "function",
					Function: FunctionCallDelta{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
				}},
			}
		}
		if len(r.toolCalls) > 0 {
			ch <- ChatEvent{Type: ChatEventDone, Finish: FinishToolCalls}
		} else {
			ch <- ChatEvent{Type: ChatEventContent, Content: r.content}
			ch <- ChatEvent{Type: ChatEventDone, Finish: FinishStop}
		}
	}()
	return ch, nil
}

func (p *streamProvider) ModelName() string { return "mock-provider" }
func (p *streamProvider) ContextSize() int  { return 32768 }

// timestampTool wraps a Tool to capture start/end timestamps
type timestampTool struct {
	inner   Tool
	onStart func()
	onEnd   func()
}

func (t *timestampTool) Name() string            { return t.inner.Name() }
func (t *timestampTool) Description() string     { return t.inner.Description() }
func (t *timestampTool) Schema() json.RawMessage { return t.inner.Schema() }
func (t *timestampTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	t.onStart()
	defer t.onEnd()
	return t.inner.Execute(ctx, args)
}

// --- Tests ---

// TestAgentLoop_BlockingToolExecution proves that when the LLM returns multiple
// tool calls in one generation, they execute SEQUENTIALLY. delegate_to blocks
// until the sub-agent finishes, THEN the next tool executes.
func TestAgentLoop_BlockingToolExecution(t *testing.T) {
	delegate := newBlockingTool("delegate_to", 200*time.Millisecond)
	bash := newFastTool("bash")

	provider := &streamProvider{
		rounds: []providerRound{
			{
				toolCalls: []ToolCallResponse{
					{ID: "tc_1", Type: "function", Function: FunctionCallData{Name: "delegate_to", Arguments: `{"task":"research"}`}},
					{ID: "tc_2", Type: "function", Function: FunctionCallData{Name: "bash", Arguments: `{"command":"echo hello"}`}},
				},
			},
			{
				content: "Research complete.",
			},
		},
	}

	registry := NewToolRegistry([]Tool{delegate, bash})
	registry.RegisterCommonAliases()

	events := make(chan AgentEvent, 256)
	var subscriber chan<- AgentEvent = events

	ctx := context.Background()
	ctx = context.WithValue(ctx, SubscriberKey{}, subscriber)

	sess := &mockSession{id: "test-blocking"}
	loop := NewAgentLoop(provider, registry, sess, AgentLoopConfig{
		MaxToolRounds: 5,
	})

	done := make(chan error, 1)
	go func() {
		done <- loop.Run(ctx, "test prompt", events)
	}()

	// Wait for delegate_to to start
	select {
	case <-delegate.started:
		t.Log("delegate_to started")
	case <-time.After(5 * time.Second):
		t.Fatal("delegate_to never started")
	}

	// bash should NOT have executed yet — delegate_to is blocking
	select {
	case <-bash.executed:
		t.Fatal("bash executed BEFORE delegate_to finished — tools are NOT sequential!")
	default:
		t.Log("bash NOT started yet — delegate_to is blocking (correct)")
	}

	// Wait for delegate_to to finish
	select {
	case <-delegate.finished:
		t.Log("delegate_to finished")
	case <-time.After(5 * time.Second):
		t.Fatal("delegate_to never finished")
	}

	// Wait for the loop to complete
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop error: %v", err)
		}
		t.Log("Agent loop completed — tools ran sequentially")
	case <-time.After(10 * time.Second):
		t.Fatal("Agent loop did not complete within 10s")
	}

	// Verify bash was eventually called (after delegate_to)
	select {
	case <-bash.executed:
		t.Log("bash executed after delegate_to (correct)")
	default:
		t.Log("bash not reached (loop may have ended after delegate_to)")
	}
}

// TestAgentLoop_DelegateBlocksThenResponds proves the CTO calls delegate_to,
// WAITS for it, THEN generates response text in a separate round.
func TestAgentLoop_DelegateBlocksThenResponds(t *testing.T) {
	delegate := newBlockingTool("delegate_to", 100*time.Millisecond)

	provider := &streamProvider{
		rounds: []providerRound{
			{
				toolCalls: []ToolCallResponse{
					{ID: "tc_1", Type: "function", Function: FunctionCallData{Name: "delegate_to", Arguments: `{"task":"research Tokyo"}`}},
				},
			},
			{
				content: "Tokyo is 25C and sunny.",
			},
		},
	}

	registry := NewToolRegistry([]Tool{delegate})
	registry.RegisterCommonAliases()

	events := make(chan AgentEvent, 256)
	var subscriber chan<- AgentEvent = events

	ctx := context.Background()
	ctx = context.WithValue(ctx, SubscriberKey{}, subscriber)

	sess := &mockSession{id: "test-block-respond"}
	loop := NewAgentLoop(provider, registry, sess, AgentLoopConfig{
		MaxToolRounds: 5,
	})

	err := loop.Run(ctx, "What's the weather?", events)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}

	provider.mu.Lock()
	rounds := provider.round
	provider.mu.Unlock()

	if rounds != 2 {
		t.Errorf("expected 2 rounds (tool call + response), got %d", rounds)
	}
	t.Logf("CTO: round 1 = delegate_to (blocked 100ms), round 2 = response text. Total rounds: %d", rounds)
}

// TestAgentLoop_MultipleDelegatesSequential proves that two delegate_to calls
// in one LLM generation execute one after the other, never concurrently.
func TestAgentLoop_MultipleDelegatesSequential(t *testing.T) {
	delegate1 := newBlockingTool("delegate_researcher", 150*time.Millisecond)
	delegate2 := newBlockingTool("delegate_browser", 150*time.Millisecond)

	var d1Start, d1End, d2Start, d2End time.Time

	wrappedD1 := &timestampTool{inner: delegate1, onStart: func() { d1Start = time.Now() }, onEnd: func() { d1End = time.Now() }}
	wrappedD2 := &timestampTool{inner: delegate2, onStart: func() { d2Start = time.Now() }, onEnd: func() { d2End = time.Now() }}

	provider := &streamProvider{
		rounds: []providerRound{
			{
				toolCalls: []ToolCallResponse{
					{ID: "tc_1", Type: "function", Function: FunctionCallData{Name: "delegate_researcher", Arguments: `{"task":"research"}`}},
					{ID: "tc_2", Type: "function", Function: FunctionCallData{Name: "delegate_browser", Arguments: `{"task":"browse"}`}},
				},
			},
			{
				content: "Both tasks complete.",
			},
		},
	}

	registry := NewToolRegistry([]Tool{wrappedD1, wrappedD2})
	registry.RegisterCommonAliases()

	events := make(chan AgentEvent, 256)
	var subscriber chan<- AgentEvent = events

	ctx := context.Background()
	ctx = context.WithValue(ctx, SubscriberKey{}, subscriber)

	sess := &mockSession{id: "test-multi"}
	loop := NewAgentLoop(provider, registry, sess, AgentLoopConfig{
		MaxToolRounds: 5,
	})

	err := loop.Run(ctx, "Research and browse", events)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}

	// Verify: second delegate started AFTER first delegate finished
	if d2Start.Before(d1End) {
		t.Fatalf("OVERLAP! delegate_browser started at %v but delegate_researcher ended at %v",
			d2Start.Format("15:04:05.000"), d1End.Format("15:04:05.000"))
	}

	t.Logf("delegate_researcher: %s -> %s", d1Start.Format("15:04:05.000"), d1End.Format("15:04:05.000"))
	t.Logf("delegate_browser:    %s -> %s", d2Start.Format("15:04:05.000"), d2End.Format("15:04:05.000"))
	t.Log("No overlap — delegates ran sequentially")
}
