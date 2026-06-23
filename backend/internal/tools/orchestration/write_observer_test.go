package orchestration

import (
	"context"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestWriteObservingExecutorRecords(t *testing.T) {
	bus := make(chan core.AgentEvent, 4)
	tr := NewConflictTracker(bus)
	parent := &stubExecutor{}
	exec := newWriteObservingExecutor(parent, tr, "alice")

	_, _ = exec.Execute(context.Background(), "file_write", map[string]any{
		"path": "/workspace/a.txt",
	})

	// Now bob writes the same file — should fire conflict.
	conflicts := tr.Record("bob", "/workspace/a.txt")
	if len(conflicts) != 1 || conflicts[0] != "alice" {
		t.Errorf("expected [alice] in conflicts, got %v", conflicts)
	}
}

func TestWriteObservingExecutorIgnoresNonWriteTools(t *testing.T) {
	bus := make(chan core.AgentEvent, 4)
	tr := NewConflictTracker(bus)
	parent := &stubExecutor{}
	exec := newWriteObservingExecutor(parent, tr, "alice")

	_, _ = exec.Execute(context.Background(), "bash", map[string]any{
		"cmd": "cat /workspace/a.txt",
	})

	// alice's bash call shouldn't record a write — bob writing a.txt is clean.
	if c := tr.Record("bob", "/workspace/a.txt"); len(c) != 0 {
		t.Errorf("bash was recorded as a write; conflicts=%v", c)
	}
}

func TestWriteObservingExecutorNilTrackerReturnsParent(t *testing.T) {
	parent := &stubExecutor{}
	exec := newWriteObservingExecutor(parent, nil, "alice")
	if exec != parent {
		t.Error("nil tracker should return parent unchanged")
	}
}

func TestExtractPath(t *testing.T) {
	cases := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"path": "/x"}, "/x"},
		{map[string]any{"file_path": "/y"}, "/y"},
		{map[string]any{"filename": "/z"}, "/z"},
		{map[string]any{"file": "/w"}, "/w"},
		{map[string]any{"other": "/q"}, "/q"}, // fallback heuristic
		{map[string]any{"cmd": "ls"}, ""},     // nothing looks like a path
		{nil, ""},
	}
	for _, c := range cases {
		got := extractPath(c.args)
		if got != c.want {
			t.Errorf("extractPath(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}
