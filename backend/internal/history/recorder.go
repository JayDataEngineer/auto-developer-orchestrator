// recorder.go adapts the Store to the three core/observer.go interfaces.
// One Recorder value satisfies TaskObserver + ChatObserver + ToolObserver
// — main.go assigns the same pointer to all three slots in the wiring.

package history

import (
	"context"
	"time"
)

// Recorder implements core.TaskObserver + core.ChatObserver +
// core.ToolObserver. Construct via New (which opens the underlying Store).
type Recorder struct {
	store *Store
}

// New opens the sqlite database at dir/ history.sqlite and returns a
// Recorder ready to be plugged into NewTaskStore + dispatchRuntime.
// Returns an error if the database can't be opened or the schema fails.
//
// Close should be called at shutdown (main.go defers it).
func New(dir string) (*Recorder, error) {
	s, err := Open(dir, nil)
	if err != nil {
		return nil, err
	}
	return &Recorder{store: s}, nil
}

// Close releases the underlying sqlite handle. Idempotent.
func (r *Recorder) Close() error {
	return r.store.Close()
}

// ── TaskObserver ──────────────────────────────────────────────────────

func (r *Recorder) OnTaskPending(ctx context.Context, taskID, org, task string, startedAt time.Time) {
	r.store.RecordTaskPending(ctx, taskID, org, task, startedAt)
}

func (r *Recorder) OnTaskRunning(ctx context.Context, taskID string) {
	r.store.RecordTaskRunning(ctx, taskID)
}

func (r *Recorder) OnTaskComplete(ctx context.Context, taskID, result string, finishedAt time.Time) {
	r.store.RecordTaskComplete(ctx, taskID, result, finishedAt)
}

func (r *Recorder) OnTaskFailed(ctx context.Context, taskID, errorMsg string, finishedAt time.Time) {
	r.store.RecordTaskFailed(ctx, taskID, errorMsg, finishedAt)
}

// ── ChatObserver ──────────────────────────────────────────────────────

func (r *Recorder) OnAssistantMessage(ctx context.Context, taskID, role string, round int, content string) {
	r.store.RecordAssistantMessage(ctx, taskID, role, round, content)
}

// ── ToolObserver ──────────────────────────────────────────────────────

// OnToolCall forwards to RecordToolCall. The error is converted to a
// string body (or empty if nil) — the recorder never blocks on errors
// from the observer fire site.
func (r *Recorder) OnToolCall(ctx context.Context, taskID, role string, round int, tool, args, result string, duration time.Duration, err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	r.store.RecordToolCall(ctx, taskID, role, round, tool, args, result, errMsg, duration)
}
