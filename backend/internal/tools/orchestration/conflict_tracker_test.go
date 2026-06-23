package orchestration

import (
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestConflictTrackerNoConflict(t *testing.T) {
	bus := make(chan core.AgentEvent, 4)
	tr := NewConflictTracker(bus)
	if c := tr.Record("alice", "/workspace/a.txt"); len(c) != 0 {
		t.Errorf("single writer, got conflicts %v", c)
	}
	select {
	case <-bus:
		t.Error("no event should fire for single writer")
	default:
	}
}

func TestConflictTrackerDetectsOverlap(t *testing.T) {
	bus := make(chan core.AgentEvent, 4)
	tr := NewConflictTracker(bus)
	tr.Record("alice", "/workspace/a.txt")
	conflicts := tr.Record("bob", "/workspace/a.txt")
	if len(conflicts) != 1 || conflicts[0] != "alice" {
		t.Errorf("got %v, want [alice]", conflicts)
	}
	evt := <-bus
	if evt.Type != core.EventTypeResourceConflict {
		t.Errorf("event type = %q, want resource_conflict", evt.Type)
	}
	data, ok := evt.Data.(core.ResourceConflictData)
	if !ok {
		t.Fatalf("payload type %T", evt.Data)
	}
	if data.Path != "/workspace/a.txt" {
		t.Errorf("path = %q", data.Path)
	}
	if data.AgentA != "bob" || data.AgentB != "alice" {
		t.Errorf("AgentA=%q AgentB=%q", data.AgentA, data.AgentB)
	}
}

func TestConflictTrackerIdempotentPerAgent(t *testing.T) {
	tr := NewConflictTracker(nil)
	tr.Record("alice", "/x.txt")
	if c := tr.Record("alice", "/x.txt"); len(c) != 0 {
		t.Errorf("same agent re-record got conflicts %v", c)
	}
}

func TestConflictTrackerClearDropsEntries(t *testing.T) {
	tr := NewConflictTracker(nil)
	tr.Record("alice", "/x.txt")
	tr.Clear("alice")
	if c := tr.Record("bob", "/x.txt"); len(c) != 0 {
		t.Errorf("after Clear, got conflicts %v", c)
	}
}

func TestConflictTrackerNormalizesPaths(t *testing.T) {
	tr := NewConflictTracker(nil)
	tr.Record("alice", "/workspace/./foo/../a.txt")
	if c := tr.Record("bob", "/workspace/a.txt"); len(c) != 1 {
		t.Errorf("normalized paths should conflict; got %v", c)
	}
}

func TestConflictTrackerEmptyArgsIgnored(t *testing.T) {
	tr := NewConflictTracker(nil)
	if c := tr.Record("", "/x"); len(c) != 0 {
		t.Errorf("empty agentID should be ignored; got %v", c)
	}
	if c := tr.Record("alice", ""); len(c) != 0 {
		t.Errorf("empty path should be ignored; got %v", c)
	}
}
