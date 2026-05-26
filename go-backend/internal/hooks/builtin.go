package hooks

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/auto-developer-orchestrator/backend/internal/checkpoint"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/session"
)

func init() {
	// file_checkpoint — auto-backup files before writes/edits/destructive bash
	RegisterHook("file_checkpoint", func(dep HookDeps) (core.LoopHook, error) {
		if dep.ProjectDir == "" {
			return nil, fmt.Errorf("file_checkpoint: projectDir required")
		}
		home := dep.HomeDir
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		sessionID := dep.SessionID
		if sessionID == "" {
			sessionID = "unknown"
		}
		cpDir := filepath.Join(home, ".pi", "agent", "checkpoints", sessionID)
		mgr := checkpoint.NewManager(sessionID, dep.ProjectDir, cpDir)
		return NewFileCheckpointHook(mgr), nil
	})

	// git_checkpoint — git commit after file-modifying tool calls
	RegisterHook("git_checkpoint", func(dep HookDeps) (core.LoopHook, error) {
		if dep.GitExecutor == nil {
			return nil, fmt.Errorf("git_checkpoint: gitExecutor required")
		}
		return NewGitCheckpointHook(dep.GitExecutor), nil
	})

	// raise_browser — raise Chrome window on agent start (VNC visibility)
	RegisterHook("raise_browser", func(dep HookDeps) (core.LoopHook, error) {
		return &raiseBrowserHook{raiseFn: dep.RaiseBrowserFunc}, nil
	})

	// journal_checkpoint — session-tree checkpointing after each tool call
	RegisterHook("journal_checkpoint", func(dep HookDeps) (core.LoopHook, error) {
		if dep.JournalTree == nil {
			return nil, fmt.Errorf("journal_checkpoint: journalTree required")
		}
		tree, ok := dep.JournalTree.(*session.SessionTree)
		if !ok {
			return nil, fmt.Errorf("journal_checkpoint: journalTree must be *session.SessionTree")
		}
		return NewJournalCheckpointHook(tree), nil
	})
}

// raiseBrowserHook is a LoopHook that raises the browser window on agent start.
// Extracted from the hardcoded logic in parallel_runner.go.
type raiseBrowserHook struct {
	raiseFn func() error // nil = no-op
}

func (h *raiseBrowserHook) Name() string { return "raise_browser" }

func (h *raiseBrowserHook) OnAgentStart(_ context.Context, _ *core.LoopState) error {
	if h.raiseFn != nil {
		if err := h.raiseFn(); err != nil {
			log.Printf("raise_browser: %v", err)
		}
	}
	return nil
}

func (h *raiseBrowserHook) OnBeforeTurn(_ context.Context, _ *core.LoopState) ([]string, error) {
	return nil, nil
}

func (h *raiseBrowserHook) OnBeforeModel(_ context.Context, _ *core.LoopState, msgs []core.Message) ([]core.Message, error) {
	return msgs, nil
}

func (h *raiseBrowserHook) OnAfterModel(_ context.Context, _ *core.LoopState, _ *core.GenerateResponse) error {
	return nil
}

func (h *raiseBrowserHook) OnAfterToolCall(_ context.Context, _ *core.LoopState, _ string, _ map[string]any, _ string, _ error) error {
	return nil
}

func (h *raiseBrowserHook) OnAgentEnd(_ context.Context, _ *core.LoopState) error { return nil }
