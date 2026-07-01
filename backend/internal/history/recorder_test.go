package history

import (
	"context"
	"regexp"
	"testing"
	"time"
)

// newTestRecorder opens a Recorder at a fresh temp dir and returns the
// recorder + the dir (so tests can also open a Query against the same
// database). t.TempDir() handles directory tear-down.
//
// Write errors route through t.Errorf — the production recorder swallows
// them silently (best-effort), but tests must surface them or failures
// look like "missing rows" instead of the underlying insert error.
func newTestRecorder(t *testing.T) (*Recorder, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir, func(format string, args ...any) {
		t.Errorf("history write error: "+format, args...)
	})
	if err != nil {
		t.Fatalf("history.Open(%q): %v", dir, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &Recorder{store: s}, dir
}

func openQueryForTest(t *testing.T, dir string) *Query {
	t.Helper()
	q, err := OpenQuery(dir)
	if err != nil {
		t.Fatalf("OpenQuery(%q): %v", dir, err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

// TestRecorder_FullTaskLifecycle exercises the TaskObserver contract:
// pending → running → complete fires three row states, all readable
// back via GetTask.
func TestRecorder_FullTaskLifecycle(t *testing.T) {
	rec, dir := newTestRecorder(t)
	ctx := context.Background()
	start := time.Now().Truncate(time.Millisecond)

	rec.OnTaskPending(ctx, "tsk_1", "_demo", "do the thing", start)
	rec.OnTaskRunning(ctx, "tsk_1")

	q := openQueryForTest(t, dir)
	got, err := q.GetTask(ctx, "tsk_1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("status: got %q want running", got.Status)
	}
	if got.Org != "_demo" || got.Task != "do the thing" {
		t.Errorf("task fields: got %+v", got)
	}
	if !got.StartedAt.Equal(start) {
		t.Errorf("started_at: got %v want %v", got.StartedAt, start)
	}
	if !got.FinishedAt.IsZero() {
		t.Errorf("finished_at should be zero for running task, got %v", got.FinishedAt)
	}

	finish := time.Now().Truncate(time.Millisecond)
	rec.OnTaskComplete(ctx, "tsk_1", "all done", finish)

	got, _ = q.GetTask(ctx, "tsk_1")
	if got.Status != "complete" {
		t.Errorf("status after complete: got %q want complete", got.Status)
	}
	if got.Result != "all done" {
		t.Errorf("result: got %q want %q", got.Result, "all done")
	}
	if !got.FinishedAt.Equal(finish) {
		t.Errorf("finished_at: got %v want %v", got.FinishedAt, finish)
	}
}

// TestRecorder_FailedTask verifies the failed path separately since
// it carries an error message instead of a result.
func TestRecorder_FailedTask(t *testing.T) {
	rec, dir := newTestRecorder(t)
	ctx := context.Background()

	rec.OnTaskPending(ctx, "tsk_2", "x", "failing thing", time.Now())
	rec.OnTaskRunning(ctx, "tsk_2")
	rec.OnTaskFailed(ctx, "tsk_2", "boom: connection refused", time.Now())

	got, err := openQueryForTest(t, dir).GetTask(ctx, "tsk_2")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "failed" {
		t.Errorf("status: got %q want failed", got.Status)
	}
	if got.Error == "" {
		t.Error("error message should be non-empty")
	}
	if got.Result != "" {
		t.Errorf("result should be empty on failure, got %q", got.Result)
	}
}

// TestRecorder_AssistantMessageRoundOrdering fires two assistant turns
// for the same task at different rounds and verifies ListMessages returns
// them in (round, id) order. Role is recorded distinctly per message —
// "cto" for round 1, "researcher" for round 2 — exercising delegation-
// chain correlation.
func TestRecorder_AssistantMessageRoundOrdering(t *testing.T) {
	rec, dir := newTestRecorder(t)
	ctx := context.Background()
	rec.OnTaskPending(ctx, "tsk_3", "x", "multi-round", time.Now())

	rec.OnAssistantMessage(ctx, "tsk_3", "cto", 1, "first thought")
	rec.OnAssistantMessage(ctx, "tsk_3", "researcher", 2, "second thought")
	rec.OnAssistantMessage(ctx, "tsk_3", "researcher", 2, "another in same round")

	msgs, err := openQueryForTest(t, dir).ListMessages(ctx, "tsk_3")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	wantContent := []string{"first thought", "second thought", "another in same round"}
	wantRole := []string{"cto", "researcher", "researcher"}
	for i, m := range msgs {
		if m.Content != wantContent[i] {
			t.Errorf("msg[%d] content: got %q want %q", i, m.Content, wantContent[i])
		}
		if m.Role != wantRole[i] {
			t.Errorf("msg[%d] role: got %q want %q", i, m.Role, wantRole[i])
		}
	}
}

// TestRecorder_ToolCallFields fires one tool call with an error and one
// without, verifies both rows are inserted with the expected fields + that
// role is preserved per call.
func TestRecorder_ToolCallFields(t *testing.T) {
	rec, dir := newTestRecorder(t)
	ctx := context.Background()
	rec.OnTaskPending(ctx, "tsk_4", "x", "tools", time.Now())

	rec.OnToolCall(ctx, "tsk_4", "cto", 1, "bash",
		`{"cmd":"echo hi"}`, "hi\n", 12*time.Millisecond, nil)
	rec.OnToolCall(ctx, "tsk_4", "researcher", 1, "file_read",
		`{"path":"/nope"}`, "", 3*time.Millisecond,
		errFoo("no such file"))

	calls, err := openQueryForTest(t, dir).ListToolCalls(ctx, "tsk_4")
	if err != nil {
		t.Fatalf("ListToolCalls: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	if calls[0].Tool != "bash" {
		t.Errorf("call[0].tool: got %q want bash", calls[0].Tool)
	}
	if calls[0].Role != "cto" {
		t.Errorf("call[0].role: got %q want cto", calls[0].Role)
	}
	if calls[0].Error != "" {
		t.Errorf("call[0].error should be empty, got %q", calls[0].Error)
	}
	if calls[0].DurationMs != 12 {
		t.Errorf("call[0].duration_ms: got %d want 12", calls[0].DurationMs)
	}

	if calls[1].Role != "researcher" {
		t.Errorf("call[1].role: got %q want researcher", calls[1].Role)
	}
	if calls[1].Error == "" {
		t.Error("call[1].error should be non-empty")
	}
}

// TestRecorder_SearchAcrossSources verifies the search path covers
// task.task, message.content, and tool_calls.result.
func TestRecorder_SearchAcrossSources(t *testing.T) {
	rec, dir := newTestRecorder(t)
	ctx := context.Background()

	rec.OnTaskPending(ctx, "tsk_5", "alpha", "research oauth patterns", time.Now())
	rec.OnTaskPending(ctx, "tsk_6", "beta", "different work", time.Now())

	rec.OnAssistantMessage(ctx, "tsk_6", "cto", 1, "thinking about oauth flows")
	rec.OnToolCall(ctx, "tsk_6", "cto", 2, "bash", `{"cmd":"cat"}`, "oauth_token=abc", time.Millisecond, nil)

	hits, err := openQueryForTest(t, dir).Search(ctx, regexp.MustCompile(`oauth`), "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	kinds := map[string]int{}
	for _, h := range hits {
		kinds[h.Kind]++
	}
	if kinds["task"] == 0 {
		t.Error("expected at least one task hit for 'oauth'")
	}
	if kinds["message"] == 0 {
		t.Error("expected at least one message hit for 'oauth'")
	}
	if kinds["tool_call"] == 0 {
		t.Error("expected at least one tool_call hit for 'oauth'")
	}
}

// TestRecorder_SearchOrgFilter verifies the org filter narrows results.
func TestRecorder_SearchOrgFilter(t *testing.T) {
	rec, dir := newTestRecorder(t)
	ctx := context.Background()

	rec.OnTaskPending(ctx, "tsk_7", "alpha", "needle", time.Now())
	rec.OnTaskPending(ctx, "tsk_8", "beta", "needle", time.Now())

	hits, err := openQueryForTest(t, dir).Search(ctx, regexp.MustCompile(`needle`), "alpha", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least 1 hit, got 0")
	}
	for _, h := range hits {
		if h.TaskID != "tsk_7" {
			t.Errorf("org filter leaked: hit for %q", h.TaskID)
		}
	}
}

// TestRecorder_SecretScrubbing verifies API keys are scrubbed from message
// content + tool args before insert. Mirrors the audit package's contract.
func TestRecorder_SecretScrubbing(t *testing.T) {
	rec, dir := newTestRecorder(t)
	ctx := context.Background()
	rec.OnTaskPending(ctx, "tsk_9", "x", "scrub test", time.Now())

	// Anthropic key shape (sk-ant-...)
	dirty := "key=sk-ant-api03-1234567890abcdefABCDEF1234567890abcdefABCDEF-XXXXXXXX"
	rec.OnAssistantMessage(ctx, "tsk_9", "cto", 1, dirty)
	rec.OnToolCall(ctx, "tsk_9", "cto", 1, "bash", `{"cmd":"`+dirty+`"}`, dirty, time.Millisecond, nil)

	msgs, _ := openQueryForTest(t, dir).ListMessages(ctx, "tsk_9")
	if len(msgs) != 1 {
		t.Fatalf("msgs: got %d want 1", len(msgs))
	}
	if contains(msgs[0].Content, "sk-ant-api03") {
		t.Errorf("secret leaked into message: %q", msgs[0].Content)
	}

	calls, _ := openQueryForTest(t, dir).ListToolCalls(ctx, "tsk_9")
	if len(calls) != 1 {
		t.Fatalf("calls: got %d want 1", len(calls))
	}
	if contains(calls[0].Args, "sk-ant-api03") {
		t.Errorf("secret leaked into args: %q", calls[0].Args)
	}
	if contains(calls[0].Result, "sk-ant-api03") {
		t.Errorf("secret leaked into result: %q", calls[0].Result)
	}
}

// TestRecorder_CloseIsIdempotent verifies Close can be called multiple
// times without panicking. main.go defers it + the test cleanup also
// calls it.
func TestRecorder_CloseIsIdempotent(t *testing.T) {
	rec, _ := newTestRecorder(t)
	if err := rec.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// errFoo is a tiny error type for the tool-call test.
type errFoo string

func (e errFoo) Error() string { return string(e) }

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
