package core

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionEntryTypeConstants(t *testing.T) {
	if EntryTypeSession != "session" {
		t.Errorf("EntryTypeSession = %q, want %q", EntryTypeSession, "session")
	}
	if EntryTypeUserMessage != "user_message" {
		t.Errorf("EntryTypeUserMessage = %q, want %q", EntryTypeUserMessage, "user_message")
	}
	if EntryTypeAssistantMessage != "assistant_message" {
		t.Errorf("EntryTypeAssistantMessage = %q, want %q", EntryTypeAssistantMessage, "assistant_message")
	}
	if EntryTypeToolResult != "tool_result" {
		t.Errorf("EntryTypeToolResult = %q, want %q", EntryTypeToolResult, "tool_result")
	}
	if EntryTypeCompaction != "compaction" {
		t.Errorf("EntryTypeCompaction = %q, want %q", EntryTypeCompaction, "compaction")
	}
	if EntryTypeBranchSummary != "branch_summary" {
		t.Errorf("EntryTypeBranchSummary = %q, want %q", EntryTypeBranchSummary, "branch_summary")
	}
	if EntryTypeSystemMessage != "system_message" {
		t.Errorf("EntryTypeSystemMessage = %q, want %q", EntryTypeSystemMessage, "system_message")
	}
}

func TestSessionEntry(t *testing.T) {
	now := time.Now()
	entry := SessionEntry{
		ID:        "entry_1",
		ParentID:  "parent_1",
		Timestamp: now,
		Type:      EntryTypeUserMessage,
		Label:     "greeting",
		Data:      json.RawMessage(`{"content":"hello"}`),
	}
	if entry.ID != "entry_1" {
		t.Errorf("ID = %q, want %q", entry.ID, "entry_1")
	}
	if entry.ParentID != "parent_1" {
		t.Errorf("ParentID = %q, want %q", entry.ParentID, "parent_1")
	}
	if !entry.Timestamp.Equal(now) {
		t.Errorf("Timestamp mismatch")
	}
	if entry.Type != EntryTypeUserMessage {
		t.Errorf("Type = %q, want %q", entry.Type, EntryTypeUserMessage)
	}
	if entry.Label != "greeting" {
		t.Errorf("Label = %q, want %q", entry.Label, "greeting")
	}
}

func TestSessionEntry_JSONRoundTrip(t *testing.T) {
	now := time.Now()
	entry := SessionEntry{
		ID:        "entry_1",
		ParentID:  "parent_1",
		Timestamp: now,
		Type:      EntryTypeAssistantMessage,
		Label:     "response",
		Data:      json.RawMessage(`{"content":"I can help!"}`),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var decoded SessionEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.ID != entry.ID {
		t.Errorf("ID mismatch: %q vs %q", decoded.ID, entry.ID)
	}
	if decoded.Type != entry.Type {
		t.Errorf("Type mismatch: %q vs %q", decoded.Type, entry.Type)
	}
}

func TestTreeNode_Leaf(t *testing.T) {
	node := &TreeNode{
		Entry: SessionEntry{
			ID:   "root",
			Type: EntryTypeSession,
		},
		Children: nil,
		Parent:   nil,
	}
	if node.Entry.ID != "root" {
		t.Errorf("expected root, got %q", node.Entry.ID)
	}
	if node.Children != nil {
		t.Errorf("expected nil children for leaf")
	}
	if node.Parent != nil {
		t.Errorf("expected nil parent for root")
	}
}

func TestTreeNode_WithChild(t *testing.T) {
	child := &TreeNode{
		Entry: SessionEntry{ID: "child", Type: EntryTypeUserMessage},
	}
	parent := &TreeNode{
		Entry:    SessionEntry{ID: "parent", Type: EntryTypeSession},
		Children: []*TreeNode{child},
	}
	child.Parent = parent

	if len(parent.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(parent.Children))
	}
	if parent.Children[0].Entry.ID != "child" {
		t.Errorf("expected child ID 'child', got %q", parent.Children[0].Entry.ID)
	}
	if child.Parent.Entry.ID != "parent" {
		t.Errorf("expected parent ID 'parent', got %q", child.Parent.Entry.ID)
	}
}

func TestSessionHeader(t *testing.T) {
	now := time.Now()
	h := SessionHeader{
		Type:          "session",
		Version:       1,
		ID:            "sess_1",
		Timestamp:     now,
		CWD:           "/home/project",
		ParentSession: "parent_sess",
	}
	if h.Type != "session" {
		t.Errorf("Type = %q, want %q", h.Type, "session")
	}
	if h.Version != 1 {
		t.Errorf("Version = %d, want %d", h.Version, 1)
	}
	if h.ID != "sess_1" {
		t.Errorf("ID = %q, want %q", h.ID, "sess_1")
	}
	if h.CWD != "/home/project" {
		t.Errorf("CWD = %q, want %q", h.CWD, "/home/project")
	}
	if h.ParentSession != "parent_sess" {
		t.Errorf("ParentSession = %q, want %q", h.ParentSession, "parent_sess")
	}
}

func TestSessionHeader_WithoutParent(t *testing.T) {
	h := SessionHeader{
		Type:    "session",
		Version: 1,
		ID:      "sess_1",
		CWD:     "/home",
	}
	if h.ParentSession != "" {
		t.Errorf("expected empty ParentSession, got %q", h.ParentSession)
	}
}

func TestEntryTypeValidStrings(t *testing.T) {
	// Verify that entry type constants produce valid JSON for known types
	types := []SessionEntryType{
		EntryTypeSession,
		EntryTypeUserMessage,
		EntryTypeAssistantMessage,
		EntryTypeToolResult,
		EntryTypeCompaction,
		EntryTypeBranchSummary,
		EntryTypeSystemMessage,
	}
	for _, et := range types {
		entry := SessionEntry{ID: "e", Type: et, Timestamp: time.Now()}
		data, err := json.Marshal(entry)
		if err != nil {
			t.Errorf("failed to marshal entry with type %q: %v", et, err)
			continue
		}
		var decoded SessionEntry
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("failed to unmarshal entry with type %q: %v", et, err)
			continue
		}
		if decoded.Type != et {
			t.Errorf("type round-trip: expected %q, got %q", et, decoded.Type)
		}
	}
}
