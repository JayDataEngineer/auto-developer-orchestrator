package mcpserver

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestTaskStore_Lifecycle(t *testing.T) {
	s := NewTaskStore(nil)
	task := s.Insert("demo", "say hi")
	if task.ID == "" {
		t.Fatal("Insert: empty ID")
	}
	if task.Status != StatusPending {
		t.Errorf("status = %q, want %q", task.Status, StatusPending)
	}
	if task.Org != "demo" || task.Task != "say hi" {
		t.Errorf("org/task = %q/%q", task.Org, task.Task)
	}

	// Get returns the same fields.
	got, ok := s.Get(task.ID)
	if !ok {
		t.Fatalf("Get(%q): not found", task.ID)
	}
	if got.Org != "demo" {
		t.Errorf("Get.Org = %q", got.Org)
	}

	// State transitions.
	s.SetRunning(task.ID)
	got, _ = s.Get(task.ID)
	if got.Status != StatusRunning {
		t.Errorf("status after SetRunning = %q", got.Status)
	}

	s.UpdateProgress(task.ID, 3, []string{"thinking", "still thinking"})
	got, _ = s.Get(task.ID)
	if got.Round != 3 {
		t.Errorf("Round = %d, want 3", got.Round)
	}
	if len(got.Tail) != 2 {
		t.Errorf("Tail len = %d, want 2", len(got.Tail))
	}

	s.SetComplete(task.ID, "done!")
	got, _ = s.Get(task.ID)
	if got.Status != StatusComplete {
		t.Errorf("status = %q, want %q", got.Status, StatusComplete)
	}
	if got.Result != "done!" {
		t.Errorf("Result = %q", got.Result)
	}
	if got.FinishedAt.IsZero() {
		t.Error("FinishedAt is zero after SetComplete")
	}
}

func TestTaskStore_Failed(t *testing.T) {
	s := NewTaskStore(nil)
	task := s.Insert("demo", "fail")
	s.SetFailed(task.ID, "upstream error")

	got, ok := s.Get(task.ID)
	if !ok {
		t.Fatal("Get: missing after SetFailed")
	}
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want %q", got.Status, StatusFailed)
	}
	if got.Error != "upstream error" {
		t.Errorf("Error = %q", got.Error)
	}
}

func TestTaskStore_GetMissingReturnsFalse(t *testing.T) {
	s := NewTaskStore(nil)
	if _, ok := s.Get("tsk_nope"); ok {
		t.Error("Get on missing ID returned true")
	}
}

func TestTaskStore_GetReturnsCopy(t *testing.T) {
	// Mutating the returned Task must NOT affect the store. Populate Tail
	// first so the test exercises the slice-copy path too.
	s := NewTaskStore(nil)
	task := s.Insert("demo", "test")
	s.UpdateProgress(task.ID, 1, []string{"first", "second"})
	s.SetComplete(task.ID, "result")

	got, _ := s.Get(task.ID)
	got.Result = "tampered"
	got.Tail[0] = "tampered"
	_ = got // mutation target — proves Get returns a copy

	again, _ := s.Get(task.ID)
	if again.Result != "result" {
		t.Errorf("Get did not return a copy: Result = %q", again.Result)
	}
	if again.Tail[0] != "first" {
		t.Errorf("Get did not deep-copy Tail: Tail[0] = %q", again.Tail[0])
	}
}

func TestTaskStore_ShutdownCancelsInFlight(t *testing.T) {
	s := NewTaskStore(nil)
	t1 := s.Insert("a", "x")
	t2 := s.Insert("b", "y")
	s.SetRunning(t1.ID)
	s.SetRunning(t2.ID)

	var cancelled []string
	var mu sync.Mutex
	s.SetCancel(t1.ID, func() {
		mu.Lock(); cancelled = append(cancelled, t1.ID); mu.Unlock()
	})
	s.SetCancel(t2.ID, func() {
		mu.Lock(); cancelled = append(cancelled, t2.ID); mu.Unlock()
	})

	// Mark t1 complete first — SetComplete calls cancel itself (clearing
	// the field), so Shutdown must NOT cancel t1 a second time.
	s.SetComplete(t1.ID, "done")

	// Snapshot the cancelled set after SetComplete to compare against.
	var afterComplete []string
	mu.Lock()
	afterComplete = append(afterComplete, cancelled...)
	mu.Unlock()

	s.Shutdown()

	// After Shutdown, the cancelled slice should have grown by exactly 1
	// (t2). t1 was cancelled once by SetComplete; Shutdown does not
	// re-cancel it.
	mu.Lock()
	defer mu.Unlock()
	if len(cancelled) != len(afterComplete)+1 {
		t.Errorf("cancelled = %v, want %v + [t2.id]", cancelled, afterComplete)
	}
	// The new entry should be t2.ID.
	if cancelled[len(cancelled)-1] != t2.ID {
		t.Errorf("last cancelled = %s, want %s", cancelled[len(cancelled)-1], t2.ID)
	}

	// t2 should now be marked failed.
	got, _ := s.Get(t2.ID)
	if got.Status != StatusFailed {
		t.Errorf("t2 status = %q, want %q", got.Status, StatusFailed)
	}
	if got.Error != "server shutdown" {
		t.Errorf("t2 error = %q, want 'server shutdown'", got.Error)
	}
}

func TestTaskStore_ConcurrentAccess(t *testing.T) {
	// Run under -race; the test passes if the race detector is silent.
	s := NewTaskStore(nil)
	const N = 50
	var wg sync.WaitGroup
	wg.Add(N * 3)
	for range N {
		go func() {
			defer wg.Done()
			task := s.Insert("a", "x")
			s.SetRunning(task.ID)
		}()
		go func() {
			defer wg.Done()
			s.All()
		}()
		go func() {
			defer wg.Done()
			tasks := s.All()
			for _, t := range tasks {
				s.Get(t.ID)
			}
		}()
	}
	wg.Wait()
}

func TestNewTaskID_UniqueAndPrefixed(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		id := newTaskID()
		if len(id) < 4 || id[:4] != "tsk_" {
			t.Errorf("id = %q, want tsk_ prefix", id)
		}
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate id generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestErrTaskNotFound_Sentinel(t *testing.T) {
	// Sanity-check the typed sentinel exists and errors.Is works.
	if !errors.Is(ErrTaskNotFound, ErrTaskNotFound) {
		t.Fatal("errors.Is(ErrTaskNotFound, ErrTaskNotFound) = false")
	}
}

// ── MCP tools (Dispatch / Status / ListOrgs) ─────────────────────────

// fakeDispatcher records every Dispatch call and returns a canned task_id.
type fakeDispatcher struct {
	mu       sync.Mutex
	calls    []dispatchCall
	taskID   string // returned on next Dispatch
	execErr  error
}

type dispatchCall struct {
	org  string
	task string
}

func (d *fakeDispatcher) Dispatch(org, task string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, dispatchCall{org: org, task: task})
	if d.execErr != nil {
		return "", d.execErr
	}
	if d.taskID == "" {
		return "tsk_test", nil
	}
	return d.taskID, nil
}

func TestDispatchTool_HappyPath(t *testing.T) {
	store := NewTaskStore(nil)
	disp := &fakeDispatcher{}
	tool := NewDispatchTool(store, disp)

	res, err := tool.Execute(context.Background(), map[string]any{
		"org_name":         "demo",
		"task_description": "say hi",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("res type = %T", res)
	}
	if m["task_id"] != "tsk_test" {
		t.Errorf("task_id = %v", m["task_id"])
	}
	if m["status"] != string(StatusPending) {
		t.Errorf("status = %v", m["status"])
	}
	if len(disp.calls) != 1 || disp.calls[0].org != "demo" || disp.calls[0].task != "say hi" {
		t.Errorf("disp.calls = %+v", disp.calls)
	}
}

func TestDispatchTool_Validation(t *testing.T) {
	tool := NewDispatchTool(NewTaskStore(nil), &fakeDispatcher{})
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("missing org_name: expected error")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"org_name": "x"}); err == nil {
		t.Error("missing task_description: expected error")
	}
}

func TestTaskStatusTool_Paths(t *testing.T) {
	store := NewTaskStore(nil)
	task := store.Insert("demo", "test")
	store.SetRunning(task.ID)
	store.UpdateProgress(task.ID, 2, []string{"hello", "world"})

	tool := NewTaskStatusTool(store)

	// Happy path.
	res, err := tool.Execute(context.Background(), map[string]any{"task_id": task.ID})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := res.(map[string]any)
	if m["status"] != string(StatusRunning) {
		t.Errorf("status = %v", m["status"])
	}
	if m["round"].(int) != 2 {
		t.Errorf("round = %v", m["round"])
	}
	tail, _ := m["transcript_tail"].([]string)
	if len(tail) != 2 || tail[0] != "hello" {
		t.Errorf("tail = %v", tail)
	}

	// Missing task_id.
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("missing task_id: expected error")
	}

	// Unknown task_id.
	if _, err := tool.Execute(context.Background(), map[string]any{"task_id": "tsk_ghost"}); err == nil {
		t.Error("unknown task_id: expected error")
	}
}

// fakeLister is a OrgLister that returns a canned slice.
type fakeLister struct {
	orgs []OrgSummary
	err  error
}

func (l *fakeLister) List() ([]OrgSummary, error) {
	if l.err != nil {
		return nil, l.err
	}
	return l.orgs, nil
}

func TestListOrgsTool(t *testing.T) {
	tool := NewListOrgsTool(&fakeLister{orgs: []OrgSummary{
		{Name: "demo", Description: "demo org", Roles: []string{"researcher"}},
		{Name: "invest", Description: "investing org", Roles: []string{"analyst", "writer"}},
	}})

	res, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m := res.(map[string]any)
	orgs, _ := m["orgs"].([]OrgSummary)
	if len(orgs) != 2 {
		t.Fatalf("orgs len = %d, want 2", len(orgs))
	}
	if m["count"].(int) != 2 {
		t.Errorf("count = %v", m["count"])
	}
}
