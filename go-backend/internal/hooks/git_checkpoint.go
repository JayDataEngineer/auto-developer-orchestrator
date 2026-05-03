package hooks

import (
	"context"
	"log"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// GitExecutor abstracts git operations for checkpointing.
type GitExecutor interface {
	Commit(ctx context.Context, message string) error
}

// GitCheckpointHook creates a git checkpoint before every tool call.
type GitCheckpointHook struct {
	logger *log.Logger
	git    GitExecutor
}

// NewGitCheckpointHook creates a git checkpoint hook.
func NewGitCheckpointHook(g GitExecutor) *GitCheckpointHook {
	return &GitCheckpointHook{
		logger: log.Default(),
		git:    g,
	}
}

func (h *GitCheckpointHook) Name() string { return "git_checkpoint" }

func (h *GitCheckpointHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	if h.git == nil {
		return nil
	}
	return h.git.Commit(ctx, "checkpoint: agent session started")
}

func (h *GitCheckpointHook) OnBeforeTurn(ctx context.Context, state *core.LoopState) ([]string, error) {
	return nil, nil
}

func (h *GitCheckpointHook) OnAfterToolCall(ctx context.Context, state *core.LoopState, toolName string, args map[string]any, result string, err error) error {
	if h.git == nil {
		return nil
	}
	// Create checkpoint after file-modifying tools
	switch toolName {
	case "file_write", "file_edit", "bash":
		msg := "checkpoint: after " + toolName
		if task, ok := args["command"].(string); ok {
			msg += " " + task
		}
		return h.git.Commit(ctx, msg)
	}
	return nil
}

func (h *GitCheckpointHook) OnAgentEnd(ctx context.Context, state *core.LoopState) error {
	if h.git == nil {
		return nil
	}
	return h.git.Commit(ctx, "checkpoint: agent session ended")
}
