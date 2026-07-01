// history.go is the optional history-pane wrapper. It probes
// $PUX_HISTORY_DIR/history.sqlite via os.Stat — never OpenQuery — so the
// TUI can degrade gracefully when history is not enabled. OpenQuery would
// create the file (the schema-apply step), which would be a side effect
// the TUI shouldn't have.
//
// The wrapper is nil-safe at every method: if the probe failed at boot,
// `hist` is nil and every method returns the zero value or empty slice.
// Callers can call freely without nil checks.

package tui

import (
	"context"
	"os"
	"path/filepath"

	"github.com/auto-developer-orchestrator/backend/internal/history"
)

// HistoryPane wraps a *history.Query for the TUI's read-only needs.
// The zero value (nil Query) is a no-op pane — every method returns
// its zero value.
type HistoryPane struct {
	q *history.Query
}

// Available reports whether history is wired in. Returns false for the
// zero value — callers use this to decide whether to show the [H]istory
// hint in the top bar.
func (h *HistoryPane) Available() bool {
	return h != nil && h.q != nil
}

// Close releases the underlying handle if one exists. Safe to call on
// the zero value.
func (h *HistoryPane) Close() error {
	if h == nil || h.q == nil {
		return nil
	}
	return h.q.Close()
}

// ListTasks returns the most-recent tasks (optionally filtered by org).
// Returns nil if history is unavailable. limit <= 0 → server default (50).
func (h *HistoryPane) ListTasks(ctx context.Context, org string, limit int) ([]history.TaskRow, error) {
	if !h.Available() {
		return nil, nil
	}
	return h.q.ListTasks(ctx, org, limit)
}

// GetTask returns one task by ID. Returns the zero TaskRow + nil error
// when history is unavailable — callers should check Available() first
// to disambiguate "no history" from "task not found".
func (h *HistoryPane) GetTask(ctx context.Context, taskID string) (history.TaskRow, error) {
	if !h.Available() {
		return history.TaskRow{}, nil
	}
	return h.q.GetTask(ctx, taskID)
}

// ListMessages returns the assistant turns for one task. nil/empty when
// history is unavailable.
func (h *HistoryPane) ListMessages(ctx context.Context, taskID string) ([]history.MessageRow, error) {
	if !h.Available() {
		return nil, nil
	}
	return h.q.ListMessages(ctx, taskID)
}

// ListToolCalls returns the in-loop tool dispatches for one task. nil/empty
// when history is unavailable.
func (h *HistoryPane) ListToolCalls(ctx context.Context, taskID string) ([]history.ToolCallRow, error) {
	if !h.Available() {
		return nil, nil
	}
	return h.q.ListToolCalls(ctx, taskID)
}

// MaybeLoadHistoryPane probes $PUX_HISTORY_DIR/history.sqlite. Returns
// a usable HistoryPane if the file exists, OR a zero-value (no-op) pane
// if either:
//   - PUX_HISTORY_DIR is unset
//   - the directory exists but history.sqlite does not (avoids the side
//     effect of OpenQuery creating it)
//   - OpenQuery fails for any other reason
//
// The pane is optional — the TUI's chat mode works without it. Operators
// who want the pane set PUX_HISTORY_DIR before launching the TUI.
func MaybeLoadHistoryPane() *HistoryPane {
	dir := os.Getenv("PUX_HISTORY_DIR")
	if dir == "" {
		return &HistoryPane{}
	}
	// Probe for the file BEFORE opening. OpenQuery would create it.
	if _, err := os.Stat(filepath.Join(dir, "history.sqlite")); err != nil {
		return &HistoryPane{}
	}
	q, err := history.OpenQuery(dir)
	if err != nil {
		// Don't surface an error — the TUI is still usable without the pane.
		// Operators who expected the pane will see the missing [H]istory hint.
		return &HistoryPane{}
	}
	return &HistoryPane{q: q}
}
