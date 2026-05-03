package graph

import (
	"fmt"
	"testing"
)

func TestTaskGraphBasic(t *testing.T) {
	g := NewTaskGraph()

	// Add nodes
	g.AddNode(&TaskNode{ID: "a", Name: "Task A", Priority: 1})
	g.AddNode(&TaskNode{ID: "b", Name: "Task B", Dependencies: []string{"a"}, Priority: 2})
	g.AddNode(&TaskNode{ID: "c", Name: "Task C", Dependencies: []string{"a"}, Priority: 3})
	g.AddNode(&TaskNode{ID: "d", Name: "Task D", Dependencies: []string{"b", "c"}, Priority: 1})

	// Validate
	if err := g.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	// Topological sort
	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("topological sort failed: %v", err)
	}

	// "a" must come before "b", "c", and "d"
	// "b" and "c" must come before "d"
	aIdx := indexOf(sorted, "a")
	bIdx := indexOf(sorted, "b")
	cIdx := indexOf(sorted, "c")
	dIdx := indexOf(sorted, "d")

	if aIdx >= bIdx {
		t.Errorf("a should come before b, got a=%d b=%d sorted=%v", aIdx, bIdx, sorted)
	}
	if aIdx >= cIdx {
		t.Errorf("a should come before c, got a=%d c=%d sorted=%v", aIdx, cIdx, sorted)
	}
	if bIdx >= dIdx {
		t.Errorf("b should come before d, got b=%d d=%d sorted=%v", bIdx, dIdx, sorted)
	}
	if cIdx >= dIdx {
		t.Errorf("c should come before d, got c=%d d=%d sorted=%v", cIdx, dIdx, sorted)
	}
}

func TestCycleDetection(t *testing.T) {
	g := NewTaskGraph()
	g.AddNode(&TaskNode{ID: "a", Name: "Task A", Dependencies: []string{"b"}})
	g.AddNode(&TaskNode{ID: "b", Name: "Task B", Dependencies: []string{"a"}})

	err := g.Validate()
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
}

func TestReadyToExecute(t *testing.T) {
	g := NewTaskGraph()
	g.AddNode(&TaskNode{ID: "a", Name: "Task A", Priority: 1})
	g.AddNode(&TaskNode{ID: "b", Name: "Task B", Dependencies: []string{"a"}, Priority: 2})
	g.AddNode(&TaskNode{ID: "c", Name: "Task C", Dependencies: []string{"a"}, Priority: 3})

	// Initially, only "a" is ready
	ready := g.ReadyToExecute()
	if len(ready) != 1 || ready[0] != "a" {
		t.Errorf("expected only 'a' ready, got %v", ready)
	}

	// Complete "a"
	g.SetResult("a", "done", nil)

	// Now "b" and "c" should be ready, with "c" first (higher priority)
	ready = g.ReadyToExecute()
	if len(ready) != 2 {
		t.Errorf("expected 2 ready tasks, got %d: %v", len(ready), ready)
	}
	if ready[0] != "c" { // higher priority
		t.Errorf("expected 'c' first (higher priority), got %v", ready)
	}

	// Complete "c"
	g.SetResult("c", "done", nil)

	ready = g.ReadyToExecute()
	if len(ready) != 1 || ready[0] != "b" {
		t.Errorf("expected only 'b' ready, got %v", ready)
	}

	// Complete "b"
	g.SetResult("b", "done", nil)

	if !g.AllComplete() {
		t.Error("graph should be all complete")
	}
}

func TestEmptyGraph(t *testing.T) {
	g := NewTaskGraph()
	if err := g.Validate(); err != nil {
		t.Errorf("empty graph should be valid: %v", err)
	}
	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Errorf("topological sort on empty graph should succeed: %v", err)
	}
	if len(sorted) != 0 {
		t.Errorf("expected empty sort, got %v", sorted)
	}
}

func TestSingleNode(t *testing.T) {
	g := NewTaskGraph()
	g.AddNode(&TaskNode{ID: "solo", Name: "Solo Task"})

	if err := g.Validate(); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	ready := g.ReadyToExecute()
	if len(ready) != 1 || ready[0] != "solo" {
		t.Errorf("solo node should be ready: %v", ready)
	}

	g.SetResult("solo", "all done", nil)
	if !g.AllComplete() {
		t.Error("single-node graph should be complete")
	}
}

func TestMissingDependency(t *testing.T) {
	g := NewTaskGraph()
	g.AddNode(&TaskNode{ID: "a", Name: "Task A", Dependencies: []string{"nonexistent"}})

	err := g.Validate()
	if err == nil {
		t.Fatal("expected error for missing dependency, got nil")
	}
}

func TestFromPlan(t *testing.T) {
	stepIDs := []string{"step0", "step1", "step2"}
	steps := []string{"Plan", "Implement", "Test"}

	g, err := FromPlan(stepIDs, steps)
	if err != nil {
		t.Fatalf("FromPlan failed: %v", err)
	}

	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("sort failed: %v", err)
	}

	// Should be in order: step0, step1, step2
	if sorted[0] != "step0" || sorted[1] != "step1" || sorted[2] != "step2" {
		t.Errorf("expected sequential order, got %v", sorted)
	}
}

func TestFromPlanWithDeps(t *testing.T) {
	steps := []map[string]any{
		{"id": "init", "task": "Initialize"},
		{"id": "build_a", "task": "Build A", "depends_on": []any{"init"}},
		{"id": "build_b", "task": "Build B", "depends_on": []any{"init"}},
		{"id": "test", "task": "Test", "depends_on": []any{"build_a", "build_b"}},
	}

	g, err := FromPlanWithDeps(steps)
	if err != nil {
		t.Fatalf("FromPlanWithDeps failed: %v", err)
	}

	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("sort failed: %v", err)
	}

	// init must come first
	if sorted[0] != "init" {
		t.Errorf("init should be first, got %v", sorted)
	}

	// build_a and build_b must come before test
	initIdx := indexOf(sorted, "init")
	buildAIdx := indexOf(sorted, "build_a")
	buildBIdx := indexOf(sorted, "build_b")
	testIdx := indexOf(sorted, "test")

	if initIdx > buildAIdx || initIdx > buildBIdx {
		t.Errorf("init should come before build_a/build_b: %v", sorted)
	}
	if buildAIdx > testIdx || buildBIdx > testIdx {
		t.Errorf("build_a/build_b should come before test: %v", sorted)
	}
}

func TestStats(t *testing.T) {
	g := NewTaskGraph()
	g.AddNode(&TaskNode{ID: "a", Name: "A"})
	g.AddNode(&TaskNode{ID: "b", Name: "B", Dependencies: []string{"a"}})
	g.AddNode(&TaskNode{ID: "c", Name: "C", Dependencies: []string{"a"}})

	stats := g.Stats()
	if stats["total"] != 3 || stats["pending"] != 3 {
		t.Errorf("expected 3 pending, got %v", stats)
	}

	g.SetResult("a", "ok", nil)
	g.SetResult("b", "ok", nil)
	g.SetResult("c", "fail", fmt.Errorf("boom"))

	stats = g.Stats()
	if stats["completed"] != 2 || stats["failed"] != 1 {
		t.Errorf("expected 2 completed + 1 failed, got %v", stats)
	}
}

func indexOf(slice []string, item string) int {
	for i, s := range slice {
		if s == item {
			return i
		}
	}
	return -1
}
