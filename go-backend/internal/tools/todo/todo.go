package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Item is a single todo item.
type Item struct {
	Content string `json:"content"`
	Status  string `json:"status"` // pending, in_progress, completed
}

// Store holds the todo list state.
type Store struct {
	mu    sync.Mutex
	Items []Item
}

func NewStore() *Store {
	return &Store{Items: make([]Item, 0)}
}

// Replace replaces the entire todo list. Returns the new list.
func (s *Store) Replace(items []Item) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Items = items
	return s.Items
}

// Tool implements core.Tool for managing the todo list.
// Uses full-state replacement: each call sends the complete list.
type Tool struct {
	store *Store
}

func NewTool(store *Store) *Tool {
	return &Tool{store: store}
}

func (t *Tool) Name() string { return "todo" }
func (t *Tool) Description() string {
	return `Use this tool to create and manage a structured task list for your current work session. This helps you track progress, organize complex tasks, and demonstrate thoroughness to the user.

Only use this tool if you think it will be helpful in staying organized. If the user's request is trivial and takes less than 3 steps, it is better to NOT use this tool and just do the task directly.

## When to Use

- Complex multi-step tasks (3+ distinct steps)
- Non-trivial tasks requiring careful planning
- User provides multiple tasks at once
- The plan may need future revisions based on results

## How to Use

1. When you start working on a task — mark it as in_progress BEFORE beginning work.
2. After completing a task — mark it as completed and add any new follow-up tasks discovered.
3. You can make several updates to the todo list at once. For example, when you complete a task, you can mark the next task as in_progress.
4. Each call REPLACES the entire list. Always include ALL items, not just the ones that changed.

## When NOT to Use

- Single straightforward tasks
- Tasks that take less than 3 trivial steps
- Purely conversational or informational requests

## Task States

- pending: Task not yet started
- in_progress: Currently working on it
- completed: Task finished successfully

## Rules

- Mark tasks in_progress BEFORE starting them.
- Mark tasks completed IMMEDIATELY after finishing (don't batch completions).
- ONLY mark completed when FULLY accomplished. If blocked, keep as in_progress.
- Always have at least one task in_progress unless all are completed.
- Remove tasks that are no longer relevant.`
}

func (t *Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"todos": {
				"type": "array",
				"description": "The complete todo list. Each call replaces the entire list.",
				"items": {
					"type": "object",
					"properties": {
						"content": {"type": "string", "description": "Description of the task"},
						"status": {"type": "string", "description": "Task status", "enum": ["pending", "in_progress", "completed"]}
					},
					"required": ["content", "status"]
				}
			}
		},
		"required": ["todos"]
	}`)
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawTodos, ok := args["todos"].([]any)
	if !ok {
		return nil, core.NewToolError("todo", "missing required parameter 'todos' (must be an array)")
	}

	items := make([]Item, 0, len(rawTodos))
	for i, raw := range rawTodos {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, core.NewToolError("todo", fmt.Sprintf("todos[%d] must be an object", i))
		}
		content, _ := m["content"].(string)
		status, _ := m["status"].(string)
		if content == "" {
			return nil, core.NewToolError("todo", fmt.Sprintf("todos[%d] missing required 'content'", i))
		}
		switch status {
		case "pending", "in_progress", "completed":
			// valid
		case "":
			return nil, core.NewToolError("todo", fmt.Sprintf("todos[%d] missing required 'status'", i))
		default:
			return nil, core.NewToolError("todo", fmt.Sprintf("todos[%d] invalid status %q (use pending, in_progress, or completed)", i, status))
		}
		items = append(items, Item{Content: content, Status: status})
	}

	current := t.store.Replace(items)

	// Build widget rows
	rows := make([]map[string]any, 0, len(current))
	for _, item := range current {
		rows = append(rows, map[string]any{"content": item.Content, "status": item.Status})
	}

	return map[string]any{
		"todos": current,
		"widget": core.WidgetResult{
			Type:  "list",
			Title: fmt.Sprintf("%d task%s", len(rows), pluralS(len(rows))),
			Icon:  "ListTodo",
			Columns: []core.WidgetColumn{
				{Key: "content", Label: "Task", Type: core.WidgetColText},
				{Key: "status", Label: "Status", Type: core.WidgetColStatus, ColorMap: map[string]string{
					"pending":    "text-muted-foreground",
					"in_progress": "text-blue-400",
					"completed":  "text-green-400",
				}},
			},
			Rows:  rows,
			Empty: "No tasks",
		},
	}, nil
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
