package llama

import (
	"context"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// GitCheckpointHook creates git stashes before risky tool calls.
// This gives the agent rollback capability — if a file edit or bash command
// breaks something, the previous state can be restored.
//
// Pattern from Pi's git-checkpoint extension:
// https://github.com/mariozechner/pi-mono/blob/main/examples/extensions/git-checkpoint.ts
type GitCheckpointHook struct {
	manager   *sandbox.Manager
	sandboxID string
	logger    *zap.Logger
}

// NewGitCheckpointHook creates a new git checkpoint hook.
func NewGitCheckpointHook(mgr *sandbox.Manager, sandboxID string, logger *zap.Logger) *GitCheckpointHook {
	return &GitCheckpointHook{
		manager:   mgr,
		sandboxID: sandboxID,
		logger:    logger,
	}
}

func (h *GitCheckpointHook) Name() string { return "git_checkpoint" }

func (h *GitCheckpointHook) BeforeToolCall(ctx context.Context, toolName string, args map[string]interface{}) (bool, map[string]interface{}, error) {
	if !h.shouldCheckpoint(toolName, args) {
		return true, nil, nil
	}

	// Best-effort git stash — don't fail the tool call if git isn't initialized
	cmd := "cd /sandbox/workspace 2>/dev/null && git rev-parse --git-dir 2>/dev/null"
	if _, err := h.manager.ExecInSandbox(ctx, h.sandboxID, []string{"bash", "-c", cmd}); err != nil {
		return true, nil, nil // Not a git repo — skip checkpoint
	}

	// Stage all changes and create a named stash
	stashCmd := "cd /sandbox/workspace && git add -A && git stash push -m 'auto-checkpoint' 2>/dev/null || true"
	if _, err := h.manager.ExecInSandbox(ctx, h.sandboxID, []string{"bash", "-c", stashCmd}); err != nil {
		h.logger.Debug("Git checkpoint stash failed (non-fatal)", zap.Error(err))
		return true, nil, nil
	}

	h.logger.Debug("Git checkpoint created",
		zap.String("tool", toolName),
		zap.String("sandbox", h.sandboxID),
	)
	return true, nil, nil
}

func (h *GitCheckpointHook) AfterToolCall(ctx context.Context, toolName string, args map[string]interface{}, result interface{}, err error) (interface{}, error) {
	// No post-action needed for checkpoint hook
	return nil, nil
}

// shouldCheckpoint returns true for tools that modify files.
func (h *GitCheckpointHook) shouldCheckpoint(toolName string, args map[string]interface{}) bool {
	switch toolName {
	case "file_write", "file_edit", "undo_edit":
		return true
	case "bash":
		return isRiskyBash(args)
	default:
		return false
	}
}

// isRiskyBash checks if a bash command looks destructive.
func isRiskyBash(args map[string]interface{}) bool {
	cmd, _ := args["command"].(string)
	if cmd == "" {
		return false
	}
	lower := strings.ToLower(cmd)
	risky := []string{"rm ", "rm -", "rmdir", "mv ", "cp ", "sed -i", "truncate",
		"dd ", "chmod", "chown", "> ", ">> ", "git push", "git reset",
		"apt install", "apt-get install", "pip install", "npm install",
		"curl ", "wget "}
	for _, r := range risky {
		if strings.Contains(lower, r) {
			return true
		}
	}
	return false
}
