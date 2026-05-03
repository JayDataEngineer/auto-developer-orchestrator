package session

import (
	"os"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

func TestCheckpointManager(t *testing.T) {
	// Create temp session file
	tmpFile := t.TempDir() + "/test-session.jsonl"

	tree, err := New(tmpFile, "/tmp")
	if err != nil {
		t.Fatalf("failed to create session tree: %v", err)
	}
	defer tree.Close()

	// Append some messages
	tree.AppendMessage(core.Message{Role: "user", Content: "Hello"})
	tree.AppendMessage(core.Message{Role: "assistant", Content: "Hi there!"})

	// Create checkpoint manager
	cm := NewCheckpointManager(tree)

	// Save a checkpoint
	messages := []core.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}
	toolResults := []core.ToolResult{
		{ToolCallID: "tc1", ToolName: "bash", Content: "ls output"},
	}

	checkpointID, err := cm.SaveCheckpoint("pre-bash", messages, toolResults, 3, 1500)
	if err != nil {
		t.Fatalf("failed to save checkpoint: %v", err)
	}
	if checkpointID == "" {
		t.Fatal("checkpoint ID should not be empty")
	}

	// List checkpoints
	checkpoints := cm.ListCheckpoints()
	if len(checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(checkpoints))
	}

	// Get latest
	latest := cm.LatestCheckpoint()
	if latest == nil {
		t.Fatal("latest checkpoint should not be nil")
	}
	if latest.Round != 3 {
		t.Errorf("expected round 3, got %d", latest.Round)
	}
	if latest.Label != "pre-bash" {
		t.Errorf("expected label 'pre-bash', got %q", latest.Label)
	}
	if len(latest.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(latest.Messages))
	}
	if len(latest.ToolResults) != 1 {
		t.Errorf("expected 1 tool result, got %d", len(latest.ToolResults))
	}

	// Append more messages AFTER checkpoint
	tree.AppendMessage(core.Message{Role: "user", Content: "Do more"})

	// Restore from checkpoint
	restored, err := cm.RestoreCheckpoint(latest.NodeID)
	if err != nil {
		t.Fatalf("failed to restore checkpoint: %v", err)
	}
	if restored.Round != 3 {
		t.Errorf("restored round should be 3, got %d", restored.Round)
	}

	// Save another checkpoint
	cp2ID, err := cm.SaveCheckpoint("pre-edit", messages, toolResults, 5, 2000)
	if err != nil {
		t.Fatalf("failed to save second checkpoint: %v", err)
	}

	checkpoints = cm.ListCheckpoints()
	if len(checkpoints) != 2 {
		t.Errorf("expected 2 checkpoints, got %d", len(checkpoints))
	}

	// Rollback to latest
	rollback, err := cm.RollbackToCheckpoint()
	if err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	if rollback.Round != 5 {
		t.Errorf("rollback should restore round 5, got %d", rollback.Round)
	}

	_ = cp2ID
	_ = os.RemoveAll // ensure os import used
}
