package hooks

import (
	"context"
	"log"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/checkpoint"
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// FileCheckpointHook intercepts file-modifying tool calls and creates
// backups before execution. Implements LoopHook + ToolCallWrapper.
//
// Intercepts: file_write, file_edit (and aliases), destructive bash commands.
// Storage: ~/.pi/agent/checkpoints/{sessionID}/ — survives rm -rf on project.
type FileCheckpointHook struct {
	manager *checkpoint.Manager
	logger  *log.Logger
}

// NewFileCheckpointHook creates a checkpoint hook backed by the given manager.
func NewFileCheckpointHook(m *checkpoint.Manager) *FileCheckpointHook {
	return &FileCheckpointHook{
		manager: m,
		logger:  log.Default(),
	}
}

// Manager returns the underlying checkpoint manager (for handler access).
func (h *FileCheckpointHook) Manager() *checkpoint.Manager {
	return h.manager
}

func (h *FileCheckpointHook) Name() string { return "file_checkpoint" }

func (h *FileCheckpointHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	snap, err := h.manager.CreateSnapshot(ctx, "agent-start", state.Round)
	if err != nil {
		h.logger.Printf("checkpoint: initial snapshot failed: %v", err)
		return nil // non-fatal
	}
	h.logger.Printf("checkpoint: initial snapshot %s (%d files)", snap.ID, snap.FileCount)
	return nil
}

func (h *FileCheckpointHook) OnBeforeTurn(_ context.Context, _ *core.LoopState) ([]string, error) {
	return nil, nil
}

func (h *FileCheckpointHook) OnBeforeModel(_ context.Context, _ *core.LoopState, msgs []core.Message) ([]core.Message, error) {
	return msgs, nil
}

func (h *FileCheckpointHook) OnAfterModel(_ context.Context, _ *core.LoopState, _ *core.GenerateResponse) error {
	return nil
}

func (h *FileCheckpointHook) OnAfterToolCall(_ context.Context, state *core.LoopState, toolName string, _ map[string]any, _ string, _ error) error {
	// Create a snapshot after file-modifying tool calls so each change is restorable
	switch toolName {
	case "file_write", "write_file", "file_edit", "edit_file", "bash":
		snap, err := h.manager.CreateSnapshot(nil, "after-"+toolName, state.Round)
		if err != nil {
			h.logger.Printf("checkpoint: post-tool snapshot failed: %v", err)
		} else if snap.FileCount > 0 {
			h.logger.Printf("checkpoint: post-%s snapshot %s (%d files)", toolName, snap.ID, snap.FileCount)
		}
	}
	return nil
}

func (h *FileCheckpointHook) OnAgentEnd(_ context.Context, state *core.LoopState) error {
	// Final snapshot with all tracked files
	if snap, err := h.manager.CreateSnapshot(nil, "agent-end", state.Round); err != nil {
		h.logger.Printf("checkpoint: final snapshot failed: %v", err)
	} else {
		h.logger.Printf("checkpoint: final snapshot %s (%d files)", snap.ID, snap.FileCount)
	}
	if err := h.manager.Close(); err != nil {
		h.logger.Printf("checkpoint: close failed: %v", err)
	}
	return nil
}

// WrapToolCall intercepts file-modifying tools and creates backups before execution.
func (h *FileCheckpointHook) WrapToolCall(
	ctx context.Context,
	toolName string,
	args map[string]any,
	next func(context.Context, string, map[string]any) (any, error),
) (any, error) {
	switch toolName {
	case "file_write", "write_file", "file_edit", "edit_file":
		if path, ok := args["file_path"].(string); ok && path != "" {
			resolved := h.resolveFilePath(path)
			if v, err := h.manager.TrackBeforeWrite(ctx, resolved); err != nil {
				h.logger.Printf("checkpoint: track %s failed: %v", path, err)
			} else if v > 0 {
				h.logger.Printf("checkpoint: backed up %s (v%d)", path, v)
			}
		}

	case "bash":
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			if checkpoint.IsDestructiveCommand(cmd) {
				if n, err := h.manager.TrackBeforeBash(ctx, cmd); err != nil {
					h.logger.Printf("checkpoint: pre-bash backup failed: %v", err)
				} else if n > 0 {
					h.logger.Printf("checkpoint: pre-bash backup (%d files)", n)
				}
			}
		}
	}

	return next(ctx, toolName, args)
}

// resolveFilePath remaps /sandbox/workspace/ paths to the actual project directory.
// The file tools do this internally (SimpleSandboxOps.absPath), but the hook
// sees the raw path from tool args before that remapping happens.
func (h *FileCheckpointHook) resolveFilePath(p string) string {
	projectDir := h.manager.ProjectDir()
	if projectDir == "" {
		return p
	}
	if strings.HasPrefix(p, "/sandbox/workspace/") {
		return projectDir + p[len("/sandbox/workspace"):]
	}
	if p == "/sandbox/workspace" {
		return projectDir
	}
	return p
}
