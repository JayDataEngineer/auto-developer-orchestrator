package todo

import (
	"context"
	"errors"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

func TestStore_Add(t *testing.T) {
	s := NewStore()
	if err := s.Add("task one"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(s.Items))
	}
	if s.Items[0].Task != "task one" {
		t.Errorf("expected 'task one', got %q", s.Items[0].Task)
	}
	if s.Items[0].Status != "pending" {
		t.Errorf("expected status 'pending', got %q", s.Items[0].Status)
	}
}

func TestStore_Add_Empty(t *testing.T) {
	s := NewStore()
	if err := s.Add(""); err == nil {
		t.Fatal("expected error for empty task")
	}
}

func TestStore_Add_Deduplicate(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("my task"))
	testutil.AssertNoError(t, s.Add("My Task")) // case-insensitive duplicate
	if len(s.Items) != 1 {
		t.Fatalf("expected 1 item after dedup, got %d", len(s.Items))
	}
}

func TestStore_Update(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("task one"))
	testutil.AssertNoError(t, s.Update("task one", "done"))
	if s.Items[0].Status != "done" {
		t.Errorf("expected status 'done', got %q", s.Items[0].Status)
	}
}

func TestStore_Update_PrefixMatch(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("implement login page"))
	testutil.AssertNoError(t, s.Update("implement", "in_progress"))
	if s.Items[0].Status != "in_progress" {
		t.Errorf("expected status 'in_progress', got %q", s.Items[0].Status)
	}
}

func TestStore_Update_NotFound(t *testing.T) {
	s := NewStore()
	if err := s.Update("nonexistent", "done"); err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestStore_Delete(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("task one"))
	testutil.AssertNoError(t, s.Add("task two"))
	testutil.AssertNoError(t, s.Delete("task one"))
	if len(s.Items) != 1 {
		t.Fatalf("expected 1 item after delete, got %d", len(s.Items))
	}
	if s.Items[0].Task != "task two" {
		t.Errorf("expected 'task two', got %q", s.Items[0].Task)
	}
}

func TestStore_Delete_NotFound(t *testing.T) {
	s := NewStore()
	if err := s.Delete("nothing"); err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestStore_Clear(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("a"))
	testutil.AssertNoError(t, s.Add("b"))
	s.Clear()
	if len(s.Items) != 0 {
		t.Fatalf("expected 0 items after clear, got %d", len(s.Items))
	}
}

func TestStore_Format_Empty(t *testing.T) {
	s := NewStore()
	if f := s.Format(); f != "" {
		t.Errorf("expected empty string, got %q", f)
	}
}

func TestStore_Format_NonEmpty(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("test task"))
	f := s.Format()
	if f == "" {
		t.Fatal("expected non-empty format")
	}
}

func TestTool_Name(t *testing.T) {
	if NewTool(NewStore()).Name() != "todo" {
		t.Errorf("Name() = %q, want %q", NewTool(NewStore()).Name(), "todo")
	}
}

func TestTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewTool(NewStore()))
}

func TestTool_Execute_Add(t *testing.T) {
	tool := NewTool(NewStore())
	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "add",
		"task":   "new task",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	testutil.AssertBoolField(t, m, "success", true)
}

func TestTool_Execute_Update(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("my task"))
	tool := NewTool(s)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "update",
		"task":   "my task",
		"status": "done",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	testutil.AssertStringField(t, m, "status", "done")
}

func TestTool_Execute_Delete(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("my task"))
	tool := NewTool(s)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "delete",
		"task":   "my task",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	testutil.AssertBoolField(t, m, "success", true)
}

func TestTool_Execute_Clear(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("my task"))
	tool := NewTool(s)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "clear",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	testutil.AssertBoolField(t, m, "success", true)
	if len(s.Items) != 0 {
		t.Errorf("expected 0 items after clear, got %d", len(s.Items))
	}
}

func TestTool_Execute_List(t *testing.T) {
	s := NewStore()
	testutil.AssertNoError(t, s.Add("task a"))
	testutil.AssertNoError(t, s.Add("task b"))
	tool := NewTool(s)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "list",
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	todos := m["todos"].([]Item)
	if len(todos) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(todos))
	}
}

func TestTool_Execute_UnknownAction(t *testing.T) {
	tool := NewTool(NewStore())
	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "invalid",
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	var toolErr *core.ToolError
	if !ok(t, err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T", err)
	}
}

func TestTool_Execute_MissingParams(t *testing.T) {
	tool := NewTool(NewStore())

	tests := []struct {
		name string
		args map[string]any
	}{
		{name: "add without task", args: map[string]any{"action": "add"}},
		{name: "update without task", args: map[string]any{"action": "update"}},
		{name: "update without status", args: map[string]any{"action": "update", "task": "x"}},
		{name: "delete without task", args: map[string]any{"action": "delete"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(context.Background(), tc.args)
			if err == nil {
				t.Fatal("expected error for missing params")
			}
		})
	}
}

func ok[T any](t *testing.T, err error, _ *T) bool {
	t.Helper()
	var ptr T
	return errors.As(err, &ptr)
}
