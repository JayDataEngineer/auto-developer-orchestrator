package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/history"
)

// TestHistoryPane_NotAvailableByDefault verifies the zero value reports
// unavailable + all methods return zero values. This is the nil-safety
// contract — callers don't need to nil-check before invoking methods.
func TestHistoryPane_NotAvailableByDefault(t *testing.T) {
	var h *HistoryPane // nil pointer
	if h.Available() {
		t.Error("nil pane should not be Available()")
	}
	tasks, err := h.ListTasks(context.Background(), "", 10)
	if err != nil {
		t.Errorf("nil ListTasks err: %v", err)
	}
	if tasks != nil {
		t.Errorf("nil ListTasks: got %v want nil", tasks)
	}
	if err := h.Close(); err != nil {
		t.Errorf("nil Close err: %v", err)
	}
}

// TestHistoryPane_ZeroValueNoop verifies a non-nil but zero-value pane
// (the MaybeLoadHistoryPane failure case) also satisfies the nil-safety
// contract.
func TestHistoryPane_ZeroValueNoop(t *testing.T) {
	h := &HistoryPane{} // q == nil
	if h.Available() {
		t.Error("zero-value pane should not be Available()")
	}
	tasks, err := h.ListTasks(context.Background(), "", 10)
	if err != nil || tasks != nil {
		t.Errorf("zero-value ListTasks: err=%v tasks=%v", err, tasks)
	}
	msgs, err := h.ListMessages(context.Background(), "tsk_x")
	if err != nil || msgs != nil {
		t.Errorf("zero-value ListMessages: err=%v msgs=%v", err, msgs)
	}
	calls, err := h.ListToolCalls(context.Background(), "tsk_x")
	if err != nil || calls != nil {
		t.Errorf("zero-value ListToolCalls: err=%v calls=%v", err, calls)
	}
}

// TestMaybeLoadHistoryPane_NoEnvVar verifies the loader returns a no-op
// pane when PUX_HISTORY_DIR is unset.
func TestMaybeLoadHistoryPane_NoEnvVar(t *testing.T) {
	os.Unsetenv("PUX_HISTORY_DIR")
	h := MaybeLoadHistoryPane()
	if h.Available() {
		t.Error("pane should not be available without PUX_HISTORY_DIR")
	}
}

// TestMaybeLoadHistoryPane_NoFile verifies the loader returns a no-op pane
// when the directory exists but history.sqlite doesn't. This is the side-
// effect-avoidance contract — OpenQuery would create the file.
func TestMaybeLoadHistoryPane_NoFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PUX_HISTORY_DIR", dir)
	// Confirm the file doesn't exist before the call.
	if _, err := os.Stat(filepath.Join(dir, "history.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("precondition: history.sqlite should not exist, got %v", err)
	}
	h := MaybeLoadHistoryPane()
	if h.Available() {
		t.Error("pane should not be available when history.sqlite is missing")
	}
	// Verify the file STILL doesn't exist after the call.
	if _, err := os.Stat(filepath.Join(dir, "history.sqlite")); !os.IsNotExist(err) {
		t.Errorf("MaybeLoadHistoryPane created history.sqlite as a side effect: %v", err)
	}
}

// TestMaybeLoadHistoryPane_LoadsWithRealFile verifies the loader wires up
// correctly when the database file exists. Uses the history package's own
// writer to create the file first.
func TestMaybeLoadHistoryPane_LoadsWithRealFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PUX_HISTORY_DIR", dir)

	// Open the writer to create the file + apply schema.
	rec, err := history.New(dir)
	if err != nil {
		t.Fatalf("history.New: %v", err)
	}
	ctx := context.Background()
	rec.OnTaskPending(ctx, "tsk_pane1", "_demo", "test task", time.Now())
	if err := rec.Close(); err != nil {
		t.Fatalf("rec.Close: %v", err)
	}

	h := MaybeLoadHistoryPane()
	if !h.Available() {
		t.Fatal("pane should be available after history file exists")
	}
	defer h.Close()

	tasks, err := h.ListTasks(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count: got %d want 1", len(tasks))
	}
	if tasks[0].ID != "tsk_pane1" {
		t.Errorf("task ID: got %q want tsk_pane1", tasks[0].ID)
	}
}
