package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// fakeProvider returns a scripted sequence of event batches, one per round.
// Each round = one entry in batches. The fake is single-use (matches Loop
// semantics). Useful for verifying the loop's Plan/Act/Observe cycle.
type fakeProvider struct {
	model    string
	batches  [][]core.ChatEvent // index 0 = round 1
	calls    int                // number of StreamChat invocations
	mu       sync.Mutex
	lastMsgs []core.Message
	lastTools []core.OpenAITool
}

func newFakeProvider(batches ...[]core.ChatEvent) *fakeProvider {
	return &fakeProvider{model: "fake-model", batches: batches}
}

func (p *fakeProvider) StreamChat(_ context.Context, msgs []core.Message, tools []core.OpenAITool, _ core.GenerateOptions) (<-chan core.ChatEvent, error) {
	p.mu.Lock()
	p.calls++
	idx := p.calls - 1
	p.lastMsgs = msgs
	p.lastTools = tools
	batch := []core.ChatEvent{{Type: core.ChatEventDone, Finish: core.FinishStop}}
	if idx < len(p.batches) {
		batch = p.batches[idx]
	}
	p.mu.Unlock()

	out := make(chan core.ChatEvent, len(batch))
	for _, e := range batch {
		out <- e
	}
	close(out)
	return out, nil
}
func (p *fakeProvider) ModelName() string  { return p.model }
func (p *fakeProvider) ContextSize() int   { return 200_000 }

// recordingExecutor captures every Execute call for assertions.
type recordingExecutor struct {
	mu     sync.Mutex
	calls  []execCall
	result any // returned for every call (override via setResult)
	err    error
}

type execCall struct {
	tool string
	args map[string]any
}

func (e *recordingExecutor) Execute(_ context.Context, tool string, args map[string]any) (any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, execCall{tool: tool, args: args})
	if e.err != nil {
		return nil, e.err
	}
	if e.result != nil {
		return e.result, nil
	}
	return "ok-" + tool, nil
}

// TestLoop_TextOnlyRound verifies the simplest path: provider returns text
// only, no tool calls. Loop should append the assistant turn and return
// the content immediately, in exactly one round.
func TestLoop_TextOnlyRound(t *testing.T) {
	prov := newFakeProvider([]core.ChatEvent{
		{Type: core.ChatEventContent, Content: "Hello, I'm done."},
		{Type: core.ChatEventDone, Finish: core.FinishStop},
	})
	exec := &recordingExecutor{}

	loop, err := NewLoop(LoopConfig{
		Provider:     prov,
		Executor:     exec,
		SystemPrompt: "You are a test CTO.",
	})
	if err != nil {
		t.Fatalf("NewLoop: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := loop.Run(ctx, "say hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "Hello, I'm done." {
		t.Errorf("output = %q, want %q", out, "Hello, I'm done.")
	}
	if prov.calls != 1 {
		t.Errorf("provider calls = %d, want 1", prov.calls)
	}
	if len(exec.calls) != 0 {
		t.Errorf("exec calls = %d, want 0", len(exec.calls))
	}
}

// TestLoop_ToolDispatch verifies the round-trip: provider requests a tool,
// executor responds, provider finishes with text. Loop should call the
// tool exactly once and return the second-round content.
func TestLoop_ToolDispatch(t *testing.T) {
	prov := newFakeProvider(
		// Round 1: tool call
		[]core.ChatEvent{
			{Type: core.ChatEventContent, Content: "Running bash."},
			{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{{
				Index:    0,
				ID:       "toolu_1",
				Function: core.FunctionCallDelta{Name: "bash", Arguments: `{"cmd":"ls"}`},
			}}},
			{Type: core.ChatEventDone, Finish: core.FinishToolCalls},
		},
		// Round 2: final text
		[]core.ChatEvent{
			{Type: core.ChatEventContent, Content: "Done."},
			{Type: core.ChatEventDone, Finish: core.FinishStop},
		},
	)
	exec := &recordingExecutor{result: "file1\nfile2"}

	loop, _ := NewLoop(LoopConfig{
		Provider:     prov,
		Executor:     exec,
		SystemPrompt: "cto",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := loop.Run(ctx, "list files")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "Done." {
		t.Errorf("output = %q, want %q", out, "Done.")
	}
	if prov.calls != 2 {
		t.Errorf("provider calls = %d, want 2", prov.calls)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(exec.calls))
	}
	if exec.calls[0].tool != "bash" || exec.calls[0].args["cmd"] != "ls" {
		t.Errorf("exec.calls[0] = %+v, want bash/ls", exec.calls[0])
	}

	// Verify the conversation history includes the tool_result we appended.
	// Tool results preserve the string body verbatim (no JSON re-encoding
	// for string returns — see renderResult).
	msgs := loop.messagesForTest()
	var sawToolResult bool
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "toolu_1" && m.Content == "file1\nfile2" {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Errorf("expected tool_result message in history; got %v", msgs)
	}
}

// TestLoop_ToolErrorDoesNotKillLoop verifies tool errors are surfaced to
// the model as text content rather than failing the loop. The model gets
// to see the error and decide whether to retry or stop.
func TestLoop_ToolErrorDoesNotKillLoop(t *testing.T) {
	prov := newFakeProvider(
		[]core.ChatEvent{
			{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{{
				Index: 0, ID: "tu", Function: core.FunctionCallDelta{Name: "bash"},
			}}},
			{Type: core.ChatEventDone, Finish: core.FinishToolCalls},
		},
		[]core.ChatEvent{
			{Type: core.ChatEventContent, Content: "Recovered from error."},
			{Type: core.ChatEventDone, Finish: core.FinishStop},
		},
	)
	exec := &recordingExecutor{err: errors.New("command not found")}

	loop, _ := NewLoop(LoopConfig{Provider: prov, Executor: exec, SystemPrompt: "cto"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := loop.Run(ctx, "test")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "Recovered from error." {
		t.Errorf("output = %q", out)
	}

	msgs := loop.messagesForTest()
	var sawErrBody bool
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "tu" {
			if !contains(m.Content, "[error]") || !contains(m.Content, "command not found") {
				t.Errorf("tool result = %q, want [error] command not found", m.Content)
			}
			sawErrBody = true
		}
	}
	if !sawErrBody {
		t.Errorf("expected tool_result with error body; got %v", msgs)
	}
}

// TestLoop_MaxRounds verifies ErrMaxRounds when the provider never returns
// finish=stop (always wants more tool calls).
func TestLoop_MaxRounds(t *testing.T) {
	// Each round returns the same tool call. With MaxRounds=3, the loop
	// should exit after 3 attempts with ErrMaxRounds.
	toolBatch := []core.ChatEvent{
		{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{{
			Index: 0, ID: "tu", Function: core.FunctionCallDelta{Name: "bash"},
		}}},
		{Type: core.ChatEventDone, Finish: core.FinishToolCalls},
	}
	// 5 identical batches — should never reach the end.
	prov := newFakeProvider(toolBatch, toolBatch, toolBatch, toolBatch, toolBatch)
	exec := &recordingExecutor{}

	loop, _ := NewLoop(LoopConfig{
		Provider: prov, Executor: exec, SystemPrompt: "cto",
		MaxRounds: 3,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := loop.Run(ctx, "loop forever")
	if !errors.Is(err, ErrMaxRounds) {
		t.Errorf("err = %v, want ErrMaxRounds", err)
	}
	if prov.calls != 3 {
		t.Errorf("provider calls = %d, want 3 (matches MaxRounds)", prov.calls)
	}
}

// TestLoop_StreamErrorPropagates verifies errors from the provider stream
// (ChatEventError) fail the loop immediately with the upstream error.
func TestLoop_StreamErrorPropagates(t *testing.T) {
	prov := newFakeProvider([]core.ChatEvent{
		{Type: core.ChatEventError, Err: errors.New("upstream API failure")},
	})
	exec := &recordingExecutor{}

	loop, _ := NewLoop(LoopConfig{Provider: prov, Executor: exec, SystemPrompt: "cto"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := loop.Run(ctx, "test")
	if err == nil || !contains(err.Error(), "upstream API failure") {
		t.Errorf("err = %v, want upstream API failure", err)
	}
}

// TestLoop_ParallelToolDispatch verifies multiple tool calls in one round
// execute concurrently. The fake tool sleeps briefly; total round time
// should be < sum of sleeps.
func TestLoop_ParallelToolDispatch(t *testing.T) {
	prov := newFakeProvider(
		[]core.ChatEvent{
			{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{
				{Index: 0, ID: "t1", Function: core.FunctionCallDelta{Name: "slow_a"}},
				{Index: 1, ID: "t2", Function: core.FunctionCallDelta{Name: "slow_b"}},
				{Index: 2, ID: "t3", Function: core.FunctionCallDelta{Name: "slow_c"}},
			}},
			{Type: core.ChatEventDone, Finish: core.FinishToolCalls},
		},
		[]core.ChatEvent{
			{Type: core.ChatEventContent, Content: "All done."},
			{Type: core.ChatEventDone, Finish: core.FinishStop},
		},
	)
	exec := &slowExecutor{delay: 100 * time.Millisecond}

	loop, _ := NewLoop(LoopConfig{Provider: prov, Executor: exec, SystemPrompt: "cto"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	out, err := loop.Run(ctx, "test")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "All done." {
		t.Errorf("output = %q", out)
	}
	// 3 tools × 100ms in serial = 300ms; in parallel ~100ms. Allow generous
	// headroom for test-env jitter but assert we're well under serial.
	if elapsed > 250*time.Millisecond {
		t.Errorf("elapsed = %v, want <250ms (parallel dispatch)", elapsed)
	}
}

type slowExecutor struct{ delay time.Duration }

func (e *slowExecutor) Execute(_ context.Context, _ string, _ map[string]any) (any, error) {
	time.Sleep(e.delay)
	return "ok", nil
}

// TestLoop_StatusUpdates verifies the Status sink gets round counter +
// transcript tail updates as the loop progresses.
func TestLoop_StatusUpdates(t *testing.T) {
	status := &Status{}
	prov := newFakeProvider(
		[]core.ChatEvent{
			{Type: core.ChatEventContent, Content: "First thought."},
			{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{{
				Index: 0, ID: "t1", Function: core.FunctionCallDelta{Name: "bash"},
			}}},
			{Type: core.ChatEventDone, Finish: core.FinishToolCalls},
		},
		[]core.ChatEvent{
			{Type: core.ChatEventContent, Content: "Final answer."},
			{Type: core.ChatEventDone, Finish: core.FinishStop},
		},
	)
	exec := &recordingExecutor{}
	loop, _ := NewLoop(LoopConfig{
		Provider: prov, Executor: exec, SystemPrompt: "cto",
		Status: status,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := loop.Run(ctx, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := status.Round(); got != 2 {
		t.Errorf("status.Round() = %d, want 2", got)
	}
	tail := status.Tail()
	if len(tail) != 2 || tail[0] != "First thought." || tail[1] != "Final answer." {
		t.Errorf("status.Tail() = %v, want [First thought., Final answer.]", tail)
	}
}

// TestNewLoop_Validation covers the constructor's required-field checks.
func TestNewLoop_Validation(t *testing.T) {
	if _, err := NewLoop(LoopConfig{Executor: &recordingExecutor{}, SystemPrompt: "x"}); err == nil {
		t.Error("missing Provider: expected error")
	}
	if _, err := NewLoop(LoopConfig{Provider: newFakeProvider(), SystemPrompt: "x"}); err == nil {
		t.Error("missing Executor: expected error")
	}
	if _, err := NewLoop(LoopConfig{Provider: newFakeProvider(), Executor: &recordingExecutor{}}); err == nil {
		t.Error("missing SystemPrompt: expected error")
	}
}

// TestDrainStream_ToolCallAccumulation verifies the partial-JSON chunks
// stitch correctly by Index. This is the load-bearing detail of the
// accumulator — a regression here breaks the loop's tool dispatch.
func TestDrainStream_ToolCallAccumulation(t *testing.T) {
	events := make(chan core.ChatEvent, 5)
	go func() {
		events <- core.ChatEvent{Type: core.ChatEventContent, Content: "Will do it."}
		events <- core.ChatEvent{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{
			{Index: 1, ID: "toolu_b", Function: core.FunctionCallDelta{Name: "bash"}},
		}}
		events <- core.ChatEvent{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{
			{Index: 0, ID: "toolu_a", Function: core.FunctionCallDelta{Name: "file_read"}},
		}}
		events <- core.ChatEvent{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{
			{Index: 1, Function: core.FunctionCallDelta{Arguments: `{"cmd":`}},
		}}
		events <- core.ChatEvent{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{
			{Index: 0, Function: core.FunctionCallDelta{Arguments: `{"path":"/etc/hosts"}`}},
		}}
		events <- core.ChatEvent{Type: core.ChatEventToolChunk, Deltas: []core.ToolCallDelta{
			{Index: 1, Function: core.FunctionCallDelta{Arguments: ` "ls"}`}},
		}}
		events <- core.ChatEvent{Type: core.ChatEventDone, Finish: core.FinishToolCalls}
		close(events)
	}()

	content, _, calls, finish, err := drainStream(events)
	if err != nil {
		t.Fatalf("drainStream: %v", err)
	}
	if content != "Will do it." {
		t.Errorf("content = %q", content)
	}
	if finish != core.FinishToolCalls {
		t.Errorf("finish = %q", finish)
	}
	if len(calls) != 2 {
		t.Fatalf("calls len = %d, want 2", len(calls))
	}
	// Sorted by index: file_read (0), bash (1).
	if calls[0].Function.Name != "file_read" || calls[0].ID != "toolu_a" {
		t.Errorf("calls[0] = %+v", calls[0])
	}
	if calls[0].Function.Arguments != `{"path":"/etc/hosts"}` {
		t.Errorf("calls[0].args = %q", calls[0].Function.Arguments)
	}
	if calls[1].Function.Name != "bash" || calls[1].ID != "toolu_b" {
		t.Errorf("calls[1] = %+v", calls[1])
	}
	if calls[1].Function.Arguments != `{"cmd": "ls"}` {
		t.Errorf("calls[1].args = %q", calls[1].Function.Arguments)
	}
}

// contains is a tiny helper to avoid pulling in strings just for one test.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
