package orchestration

import (
	"context"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// writeToolNames is the set of tool call names that mutate files. Names not
// in this set skip the conflict-tracker fast path. Kept conservative — only
// tools where the `path` / `file_path` arg is a real filesystem path.
var writeToolNames = map[string]bool{
	"file_write": true,
	"file_edit":  true,
	"write":      true,
	"edit":       true,
}

// writeObservingExecutor wraps a parent executor and records file-modifying
// tool calls against the ConflictTracker. The actual write still falls
// through to the parent — this layer is observational, not blocking. The
// CTO gets the resource_conflict SSE event and can decide to re-plan.
type writeObservingExecutor struct {
	parent  core.ToolExecutor
	tracker *ConflictTracker
	agentID string
}

// newWriteObservingExecutor wraps parent so file_write / file_edit calls
// are recorded for turf-war detection. No-op if tracker is nil.
func newWriteObservingExecutor(parent core.ToolExecutor, tracker *ConflictTracker, agentID string) core.ToolExecutor {
	if tracker == nil {
		return parent
	}
	return &writeObservingExecutor{parent: parent, tracker: tracker, agentID: agentID}
}

// Execute records the write before delegating to parent. Non-write tools
// pass straight through.
func (w *writeObservingExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	if writeToolNames[name] {
		if path := extractPath(args); path != "" {
			w.tracker.Record(w.agentID, path)
		}
	}
	return w.parent.Execute(ctx, name, args)
}

// ToolTimeoutHint delegates to parent.
func (w *writeObservingExecutor) ToolTimeoutHint(toolName string) time.Duration {
	if h, ok := w.parent.(interface{ ToolTimeoutHint(string) time.Duration }); ok {
		return h.ToolTimeoutHint(toolName)
	}
	return 0
}

// extractPath pulls the path argument from a tool call. Tolerates the
// various names different tool families use. Returns "" if no path found.
func extractPath(args map[string]any) string {
	if args == nil {
		return ""
	}
	for _, k := range []string{"path", "file_path", "filename", "file"} {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	// Last-ditch: scan for any string arg that looks like a path.
	for _, v := range args {
		if s, ok := v.(string); ok && looksLikePath(s) {
			return s
		}
	}
	return ""
}

// looksLikePath is a cheap heuristic: contains a slash OR has a known file
// extension. Used only as a fallback when no recognized path key is present.
func looksLikePath(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	if i := strings.LastIndex(s, "."); i > 0 {
		ext := strings.ToLower(s[i:])
		switch ext {
		case ".go", ".py", ".ts", ".tsx", ".js", ".jsx", ".md", ".txt", ".yaml", ".yml", ".json", ".toml":
			return true
		}
	}
	return false
}
