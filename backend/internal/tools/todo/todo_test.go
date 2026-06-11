package todo

import (
	"context"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/core/testutil"
)

func TestTool_Name(t *testing.T) {
	if NewTool(NewStore()).Name() != "todo" {
		t.Errorf("Name() = %q, want %q", NewTool(NewStore()).Name(), "todo")
	}
}

func TestTool_Schema(t *testing.T) {
	testutil.AssertValidSchema(t, NewTool(NewStore()))
}

func TestTool_Execute_FullList(t *testing.T) {
	tool := NewTool(NewStore())
	result, err := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"content": "Task one", "status": "pending"},
			map[string]any{"content": "Task two", "status": "in_progress"},
			map[string]any{"content": "Task three", "status": "completed"},
		},
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)

	todos := m["todos"].([]Item)
	if len(todos) != 3 {
		t.Fatalf("expected 3 todos, got %d", len(todos))
	}
	if todos[0].Content != "Task one" || todos[0].Status != "pending" {
		t.Errorf("first item = %+v, want {Task one, pending}", todos[0])
	}
	if todos[1].Content != "Task two" || todos[1].Status != "in_progress" {
		t.Errorf("second item = %+v, want {Task two, in_progress}", todos[1])
	}
	if todos[2].Content != "Task three" || todos[2].Status != "completed" {
		t.Errorf("third item = %+v, want {Task three, completed}", todos[2])
	}

	// Widget should be present
	widget, ok := m["widget"]
	if !ok {
		t.Fatal("expected widget in result")
	}
	w := widget.(core.WidgetResult)
	if w.Type != "list" {
		t.Errorf("widget type = %q, want %q", w.Type, "list")
	}
	if len(w.Rows) != 3 {
		t.Errorf("widget rows = %d, want 3", len(w.Rows))
	}
}

func TestTool_Execute_ReplacesEntireList(t *testing.T) {
	s := NewStore()
	tool := NewTool(s)

	// First call: 3 items
	_, err := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"content": "A", "status": "pending"},
			map[string]any{"content": "B", "status": "pending"},
			map[string]any{"content": "C", "status": "pending"},
		},
	})
	testutil.AssertNoError(t, err)
	if len(s.Items) != 3 {
		t.Fatalf("expected 3 items after first call, got %d", len(s.Items))
	}

	// Second call: replace with 2 items (A completed, B removed)
	_, err = tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"content": "A", "status": "completed"},
			map[string]any{"content": "C", "status": "in_progress"},
		},
	})
	testutil.AssertNoError(t, err)
	if len(s.Items) != 2 {
		t.Fatalf("expected 2 items after second call, got %d", len(s.Items))
	}
	if s.Items[0].Status != "completed" {
		t.Errorf("first item status = %q, want completed", s.Items[0].Status)
	}
	if s.Items[1].Status != "in_progress" {
		t.Errorf("second item status = %q, want in_progress", s.Items[1].Status)
	}
}

func TestTool_Execute_EmptyList(t *testing.T) {
	tool := NewTool(NewStore())
	result, err := tool.Execute(context.Background(), map[string]any{
		"todos": []any{},
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	todos := m["todos"].([]Item)
	if len(todos) != 0 {
		t.Fatalf("expected 0 todos, got %d", len(todos))
	}
}

func TestTool_Execute_MissingTodos(t *testing.T) {
	tool := NewTool(NewStore())
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing todos")
	}
}

func TestTool_Execute_MissingContent(t *testing.T) {
	tool := NewTool(NewStore())
	_, err := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"status": "pending"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing content")
	}
}

func TestTool_Execute_InvalidStatus(t *testing.T) {
	tool := NewTool(NewStore())
	_, err := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"content": "task", "status": "done"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid status 'done'")
	}
}

func TestTool_Execute_MissingStatus(t *testing.T) {
	tool := NewTool(NewStore())
	_, err := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"content": "task"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing status")
	}
}

func TestTool_Execute_WidgetStructure(t *testing.T) {
	tool := NewTool(NewStore())
	result, err := tool.Execute(context.Background(), map[string]any{
		"todos": []any{
			map[string]any{"content": "Build feature", "status": "in_progress"},
		},
	})
	testutil.AssertNoError(t, err)
	m := result.(map[string]any)
	w := m["widget"].(core.WidgetResult)

	if w.Title != "1 task" {
		t.Errorf("title = %q, want %q", w.Title, "1 task")
	}
	if w.Icon != "ListTodo" {
		t.Errorf("icon = %q, want %q", w.Icon, "ListTodo")
	}
	if len(w.Columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(w.Columns))
	}
	if w.Columns[0].Key != "content" {
		t.Errorf("first column key = %q, want content", w.Columns[0].Key)
	}
	if w.Columns[1].Key != "status" {
		t.Errorf("second column key = %q, want status", w.Columns[1].Key)
	}
}
