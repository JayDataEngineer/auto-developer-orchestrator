package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Item is a single todo item.
type Item struct {
	Task   string `json:"task"`
	Status string `json:"status"` // pending, in_progress, done, blocked
}

// Store holds the todo list state.
type Store struct {
	mu    sync.Mutex
	Items []Item
}

func NewStore() *Store {
	return &Store{Items: make([]Item, 0)}
}

// Format returns the todo list as a compact JSON string for prompt injection (~250 tokens max).
func (s *Store) Format() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Items) == 0 {
		return ""
	}
	data, _ := json.Marshal(map[string]any{"todos": s.Items})
	return string(data)
}

// Add adds a todo item.
func (s *Store) Add(task string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task == "" {
		return fmt.Errorf("task cannot be empty")
	}
	// Deduplicate
	for _, item := range s.Items {
		if strings.EqualFold(item.Task, task) {
			return nil
		}
	}
	s.Items = append(s.Items, Item{Task: task, Status: "pending"})
	return nil
}

// Update changes the status of a todo item by task name (prefix match).
func (s *Store) Update(task string, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Items {
		if strings.EqualFold(s.Items[i].Task, task) || strings.HasPrefix(strings.ToLower(s.Items[i].Task), strings.ToLower(task)) {
			s.Items[i].Status = status
			return nil
		}
	}
	return fmt.Errorf("todo not found: %s", task)
}

// Delete removes a todo item.
func (s *Store) Delete(task string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Items {
		if strings.EqualFold(s.Items[i].Task, task) || strings.HasPrefix(strings.ToLower(s.Items[i].Task), strings.ToLower(task)) {
			s.Items = append(s.Items[:i], s.Items[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("todo not found: %s", task)
}

// Clear removes all todos.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Items = nil
}

// Tool implements core.Tool for managing the todo list.
type Tool struct {
	store *Store
}

func NewTool(store *Store) *Tool {
	return &Tool{store: store}
}

func (t *Tool) Name() string        { return "todo" }
func (t *Tool) Description() string { return "Manage the task todo list. Use add, update, delete, or clear." }

func (t *Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"description": "Action: add, update, delete, clear, list",
				"enum": ["add", "update", "delete", "clear", "list"]
			},
			"task": {"type": "string", "description": "Task description (for add, update, delete)"},
			"status": {
				"type": "string",
				"description": "New status (for update only): pending, in_progress, done, blocked",
				"enum": ["pending", "in_progress", "done", "blocked"]
			}
		},
		"required": ["action"]
	}`)
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	task, _ := args["task"].(string)
	status, _ := args["status"].(string)

	switch action {
	case "add":
		if task == "" {
			return nil, core.NewToolError("todo", "missing required parameter 'task' for add")
		}
		if err := t.store.Add(task); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "action": "add", "task": task}, nil

	case "update":
		if task == "" {
			return nil, core.NewToolError("todo", "missing required parameter 'task' for update")
		}
		if status == "" {
			return nil, core.NewToolError("todo", "missing required parameter 'status' for update")
		}
		if err := t.store.Update(task, status); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "action": "update", "task": task, "status": status}, nil

	case "delete":
		if task == "" {
			return nil, core.NewToolError("todo", "missing required parameter 'task' for delete")
		}
		if err := t.store.Delete(task); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "action": "delete", "task": task}, nil

	case "clear":
		t.store.Clear()
		return map[string]any{"success": true, "action": "clear"}, nil

	case "list":
		return map[string]any{"todos": t.store.Items}, nil

	default:
		return nil, core.NewToolError("todo", "unknown action: "+action)
	}
}
