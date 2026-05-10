package hooks

import (
	"context"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/todo"
)

// TodoHook injects the current todo list into the agent context before each model call.
// This keeps the model aware of what's done, what's next, and what's blocked.
type TodoHook struct {
	store *todo.Store
}

func NewTodoHook(store *todo.Store) *TodoHook {
	return &TodoHook{store: store}
}

func (h *TodoHook) Name() string { return "todo" }

func (h *TodoHook) OnAgentStart(_ context.Context, _ *core.LoopState) error { return nil }

// OnBeforeTurn injects the todo list as a system message before each model call.
func (h *TodoHook) OnBeforeTurn(_ context.Context, _ *core.LoopState) ([]string, error) {
	todos := h.store.Format()
	if todos == "" {
		return nil, nil
	}
	return []string{"Current todo list:\n" + todos}, nil
}

func (h *TodoHook) OnBeforeModel(_ context.Context, _ *core.LoopState, msgs []core.Message) ([]core.Message, error) { return msgs, nil }
func (h *TodoHook) OnAfterModel(_ context.Context, _ *core.LoopState, _ *core.GenerateResponse) error { return nil }
func (h *TodoHook) OnAfterToolCall(_ context.Context, _ *core.LoopState, _ string, _ map[string]any, _ string, _ error) error {
	return nil
}
func (h *TodoHook) OnAgentEnd(_ context.Context, _ *core.LoopState) error { return nil }
