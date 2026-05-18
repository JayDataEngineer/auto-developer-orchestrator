package hooks

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/perms"
)

// PermissionHook intercepts tool calls and checks permission levels.
// Implements LoopHook + ToolCallWrapper.
//
// Permission levels:
//   - auto:   pass through without asking
//   - confirm: emit a decision_request via DecisionRegistry, block for user response
//   - deny:    return an error immediately
//
// Users can grant "always allow for this session" — cached in a session map.
type PermissionHook struct {
	config     *perms.ToolPermissionConfig
	registry   *core.DecisionRegistry
	subscriber chan core.AgentEvent
	timeout    time.Duration

	mu      sync.Mutex
	session map[string]bool // tool name → allowed-for-session

	logger *log.Logger
}

// NewPermissionHook creates a permission hook using the unified decision registry.
// Pass nil for subscriber to disable SSE events (permissions still checked).
func NewPermissionHook(cfg *perms.ToolPermissionConfig, registry *core.DecisionRegistry, subscriber chan core.AgentEvent) *PermissionHook {
	if registry == nil {
		registry = core.GlobalDecisions
	}
	return &PermissionHook{
		config:     cfg,
		registry:   registry,
		subscriber: subscriber,
		timeout:    5 * time.Minute,
		session:    make(map[string]bool),
		logger:     log.Default(),
	}
}

func (h *PermissionHook) Name() string { return "permission" }

func (h *PermissionHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	return nil
}

func (h *PermissionHook) OnBeforeTurn(ctx context.Context, state *core.LoopState) ([]string, error) {
	return nil, nil
}

func (h *PermissionHook) OnBeforeModel(_ context.Context, _ *core.LoopState, msgs []core.Message) ([]core.Message, error) {
	return msgs, nil
}

func (h *PermissionHook) OnAfterModel(_ context.Context, _ *core.LoopState, _ *core.GenerateResponse) error {
	return nil
}

func (h *PermissionHook) OnAfterToolCall(_ context.Context, _ *core.LoopState, _ string, _ map[string]any, _ string, _ error) error {
	return nil
}

func (h *PermissionHook) OnAgentEnd(ctx context.Context, state *core.LoopState) error {
	return nil
}

// WrapToolCall checks permission before executing. Implements ToolCallWrapper.
func (h *PermissionHook) WrapToolCall(ctx context.Context, toolName string, args map[string]any, next func(context.Context, string, map[string]any) (any, error)) (any, error) {
	// Look up permission level, defaulting to auto.
	level := h.getPermissionLevel(toolName)
	switch level {
	case perms.PermAutoApprove:
		return next(ctx, toolName, args)
	case perms.PermDeny:
		return nil, fmt.Errorf("tool %q execution denied by policy", toolName)
	case perms.PermRequireApproval:
		// Check session cache
		if h.isSessionAllowed(toolName) {
			return next(ctx, toolName, args)
		}

		// Build description from args
		desc := formatToolArgs(toolName, args)

		reqID := fmt.Sprintf("perm-%s-%d", toolName, time.Now().UnixNano())
		req := core.DecisionRequest{
			ID:         reqID,
			SourceTool: toolName,
			Title:      fmt.Sprintf("Allow %q?", toolName),
			Description: desc,
			Hint:       core.HintApproval,
			Metadata: map[string]any{
				"toolName": toolName,
				"toolArgs": args,
			},
		}

		// Use DecisionRegistry for HITL (works with existing SSE chain)
		resp, err := h.registry.WaitForDecision(ctx, req, h.subscriber, h.timeout)
		if err != nil {
			return nil, fmt.Errorf("tool %q permission check failed: %w", toolName, err)
		}

		switch resp.Action {
		case "approve":
			return next(ctx, toolName, args)
		case "allow_session":
			h.setSessionAllowed(toolName)
			return next(ctx, toolName, args)
		case "reject":
			return nil, fmt.Errorf("tool %q rejected by user", toolName)
		default:
			return nil, fmt.Errorf("tool %q permission %q not recognized", toolName, resp.Action)
		}

	default:
		return next(ctx, toolName, args)
	}
}

func (h *PermissionHook) getPermissionLevel(toolName string) perms.PermissionLevel {
	if h.config == nil {
		return perms.PermAutoApprove
	}
	all := h.config.AllPermissions()
	if p, ok := all[toolName]; ok {
		return p.Level
	}
	return perms.PermAutoApprove
}

func (h *PermissionHook) isSessionAllowed(toolName string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.session[toolName]
}

func (h *PermissionHook) setSessionAllowed(toolName string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.session[toolName] = true
	h.logger.Printf("PERMISSION: tool %q allowed for session", toolName)
}

// formatToolArgs creates a human-readable description of tool arguments.
func formatToolArgs(toolName string, args map[string]any) string {
	switch toolName {
	case "bash":
		if cmd, ok := args["command"].(string); ok {
			return cmd
		}
		if cmd, ok := args["cmd"].(string); ok {
			return cmd
		}
	case "file_write", "write_file":
		if path, ok := args["file_path"].(string); ok {
			return fmt.Sprintf("Write to: %s", path)
		}
	case "file_edit", "edit_file":
		path, _ := args["file_path"].(string)
		old, _ := args["old_string"].(string)
		preview := old
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		return fmt.Sprintf("Edit: %s\nReplace: %q", path, preview)
	case "file_read", "read_file":
		if path, ok := args["file_path"].(string); ok {
			return fmt.Sprintf("Read: %s", path)
		}
	case "delegate_to":
		if role, ok := args["role"].(string); ok {
			return fmt.Sprintf("Delegate to %s", role)
		}
		if task, ok := args["task"].(string); ok {
			preview := task
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			return preview
		}
	case "delegate_async":
		if task, ok := args["task"].(string); ok {
			preview := task
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			return preview
		}
	}
	// Fallback: show args as JSON-like text
	if len(args) == 0 {
		return "(no arguments)"
	}
	text := ""
	for k, v := range args {
		if len(text) > 200 {
			text += "..."
			break
		}
		text += fmt.Sprintf("%s: %v\n", k, v)
	}
	return text
}
