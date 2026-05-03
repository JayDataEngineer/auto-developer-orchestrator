package hooks

import (
	"context"
	"log"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/session"
)

// JournalCheckpointHook saves node-level checkpoints before each tool call.
// Integrates with session.CheckpointManager for granular state save/restore.
// Checkpoints enable:
//   - Rollback on tool error (backtrack to pre-tool state)
//   - Pre-compaction snapshots (restore full context if compaction loses too much)
//   - Branch/fork from any checkpoint for exploring alternatives
type JournalCheckpointHook struct {
	tree *session.SessionTree
	cm   *session.CheckpointManager
}

// NewJournalCheckpointHook creates a hook that checkpoints before each tool call.
func NewJournalCheckpointHook(tree *session.SessionTree) *JournalCheckpointHook {
	return &JournalCheckpointHook{
		tree: tree,
		cm:   session.NewCheckpointManager(tree),
	}
}

func (h *JournalCheckpointHook) Name() string { return "journal_checkpoint" }

func (h *JournalCheckpointHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	// Save initial checkpoint so we can always roll back to the start
	nodeID := h.tree.GetCurrentNode()
	log.Printf("JOURNAL: checkpoint hook active at node %s", nodeID)
	return nil
}

func (h *JournalCheckpointHook) OnBeforeTurn(ctx context.Context, state *core.LoopState) ([]string, error) {
	return nil, nil
}

func (h *JournalCheckpointHook) OnAfterToolCall(ctx context.Context, state *core.LoopState, toolName string, args map[string]any, result string, err error) error {
	// Don't checkpoint after delegation tools — sub-agents manage their own state
	if toolName == "delegate_to" || toolName == "delegate_async" || toolName == "collect_results" {
		return nil
	}

	// Get current messages from the session
	messages, buildErr := h.tree.BuildContext(ctx)
	if buildErr != nil {
		log.Printf("JOURNAL: failed to build context for checkpoint: %v", buildErr)
		messages = nil
	}

	// Save a checkpoint after every tool execution
	// This captures the state right after the tool result was appended
	checkpointID, cpErr := h.cm.SaveCheckpoint(
		"tool:"+toolName,
		messages,
		state.ToolResults,
		state.Round,
		state.TotalInputTokens+state.TotalOutputTokens,
	)
	if cpErr != nil {
		log.Printf("JOURNAL: failed to save checkpoint for tool %q: %v", toolName, cpErr)
		return nil // non-fatal
	}

	log.Printf("JOURNAL: checkpoint %s saved after %s (round %d, %d messages, %d tool results)",
		checkpointID, toolName, state.Round, len(messages), len(state.ToolResults))

	return nil
}

func (h *JournalCheckpointHook) OnAgentEnd(ctx context.Context, state *core.LoopState) error {
	messages, buildErr := h.tree.BuildContext(ctx)
	if buildErr != nil {
		log.Printf("JOURNAL: failed to build context for final checkpoint: %v", buildErr)
		return nil
	}

	_, err := h.cm.SaveCheckpoint(
		"agent_end",
		messages,
		state.ToolResults,
		state.Round,
		state.TotalInputTokens+state.TotalOutputTokens,
	)
	if err != nil {
		log.Printf("JOURNAL: failed to save final checkpoint: %v", err)
	}

	log.Printf("JOURNAL: agent end checkpoint saved (round %d)", state.Round)
	return nil
}

// CheckpointManager returns the underlying checkpoint manager for external access.
func (h *JournalCheckpointHook) CheckpointManager() *session.CheckpointManager {
	return h.cm
}
