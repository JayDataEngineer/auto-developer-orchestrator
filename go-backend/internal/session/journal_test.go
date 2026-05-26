package session

import (
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

	// Restore from checkpoint
	restored, err := cm.RestoreCheckpoint(checkpointID)
	if err != nil {
		t.Fatalf("failed to restore checkpoint: %v", err)
	}
	if restored.Round != 3 {
		t.Errorf("restored round should be 3, got %d", restored.Round)
	}
	if restored.Label != "pre-bash" {
		t.Errorf("expected label 'pre-bash', got %q", restored.Label)
	}
	if len(restored.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(restored.Messages))
	}
	if len(restored.ToolResults) != 1 {
		t.Errorf("expected 1 tool result, got %d", len(restored.ToolResults))
	}
}
