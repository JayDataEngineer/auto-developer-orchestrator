package session

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// JournalCheckpoint is a snapshot of a session tree node's state.
// Captures enough data to restore the session to exactly this point.
type JournalCheckpoint struct {
	NodeID       string           `json:"node_id"`
	Timestamp    time.Time        `json:"timestamp"`
	Label        string           `json:"label,omitempty"`
	Messages     []core.Message   `json:"messages"`
	ToolResults  []core.ToolResult `json:"tool_results"`
	Round        int              `json:"round"`
	TotalTokens  int              `json:"total_tokens"`
	CurrentNode  string           `json:"current_node"`
	ParentCheckpoint string       `json:"parent_checkpoint,omitempty"`
}

// CheckpointManager adds node-level journal checkpoint save/restore to SessionTree.
type CheckpointManager struct {
	tree        *SessionTree
	checkpoints map[string]*JournalCheckpoint // nodeID → checkpoint
	history     []string                       // ordered checkpoint IDs
	mu          sync.RWMutex
}

// NewCheckpointManager creates a checkpoint manager for a session tree.
func NewCheckpointManager(tree *SessionTree) *CheckpointManager {
	return &CheckpointManager{
		tree:        tree,
		checkpoints: make(map[string]*JournalCheckpoint),
		history:     nil,
	}
}

// SaveCheckpoint captures the current state at the given node.
// Creates a journal checkpoint that can later be restored.
func (cm *CheckpointManager) SaveCheckpoint(label string, messages []core.Message, toolResults []core.ToolResult, round int, totalTokens int) (string, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	nodeID := cm.tree.GetCurrentNode()
	cp := &JournalCheckpoint{
		NodeID:      nodeID,
		Timestamp:   time.Now(),
		Label:       label,
		Messages:    deepCopyMessages(messages),
		ToolResults: deepCopyToolResults(toolResults),
		Round:       round,
		TotalTokens: totalTokens,
		CurrentNode: nodeID,
	}

	cm.checkpoints[nodeID] = cp
	cm.history = append(cm.history, nodeID)

	// Write checkpoint as a checkpoint entry in the session tree
	cpData, err := json.Marshal(cp)
	if err != nil {
		return "", fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	cm.tree.mu.Lock()
	checkpointID := newID("chk")
	entry := core.SessionEntry{
		ID:        checkpointID,
		ParentID:  cm.tree.current,
		Timestamp: cp.Timestamp,
		Type:      core.EntryTypeCompaction, // reuse compaction entry type for checkpoint
		Label:     label,
		Data:      cpData,
	}

	if err := cm.tree.writeEntry(entry); err != nil {
		cm.tree.mu.Unlock()
		return "", fmt.Errorf("failed to write checkpoint entry: %w", err)
	}

	cm.tree.entries[checkpointID] = entry
	checkNode := &core.TreeNode{Entry: entry, Parent: cm.tree.nodes[cm.tree.current]}
	cm.tree.nodes[checkpointID] = checkNode
	if parent, ok := cm.tree.nodes[cm.tree.current]; ok {
		parent.Children = append(parent.Children, checkNode)
	}
	cm.tree.mu.Unlock()

	return checkpointID, nil
}

// RestoreCheckpoint navigates to a checkpoint and restores the session state.
// Returns the restored messages, tool results, and metadata.
func (cm *CheckpointManager) RestoreCheckpoint(nodeID string) (*JournalCheckpoint, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	cp, ok := cm.checkpoints[nodeID]
	if !ok {
		// Try to find by walking the tree
		if entry, ok := cm.tree.entries[nodeID]; ok {
			if err := json.Unmarshal(entry.Data, &cp); err != nil {
				return nil, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
			}
		}
	}
	if cp == nil {
		return nil, fmt.Errorf("checkpoint %s not found", nodeID)
	}

	// Navigate to the checkpoint node
	if err := cm.tree.Navigate(nodeID); err != nil {
		return nil, fmt.Errorf("failed to navigate to checkpoint: %w", err)
	}

	return cp, nil
}

// deepCopyMessages creates a deep copy of message slice.
func deepCopyMessages(msgs []core.Message) []core.Message {
	if msgs == nil {
		return nil
	}
	out := make([]core.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if m.ToolCalls != nil {
			out[i].ToolCalls = make([]core.ToolCallResponse, len(m.ToolCalls))
			copy(out[i].ToolCalls, m.ToolCalls)
		}
	}
	return out
}

// deepCopyToolResults creates a deep copy of tool result slice.
func deepCopyToolResults(results []core.ToolResult) []core.ToolResult {
	if results == nil {
		return nil
	}
	out := make([]core.ToolResult, len(results))
	copy(out, results)
	return out
}
