package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newTestDB(t *testing.T) *Database {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := NewDatabase(dbPath)
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ── NewDatabase ───────────────────────────────────────────────

func TestNewDatabase(t *testing.T) {
	db := newTestDB(t)
	if db == nil {
		t.Fatal("expected non-nil Database")
	}
	if db.GetProjectsDir() != "/app/projects" {
		t.Errorf("expected default projectsDir, got %q", db.GetProjectsDir())
	}
}

// ── CustomProjects ────────────────────────────────────────────

func TestGetCustomProjectsEmpty(t *testing.T) {
	db := newTestDB(t)
	projects, err := db.GetCustomProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

func TestAddAndGetCustomProjects(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.AddCustomProject(ctx, "proj-a", "/tmp/a"); err != nil {
		t.Fatal(err)
	}
	if err := db.AddCustomProject(ctx, "proj-b", "/tmp/b"); err != nil {
		t.Fatal(err)
	}

	projects, err := db.GetCustomProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}
	names := map[string]string{}
	for _, p := range projects {
		names[p.Name] = p.Path
	}
	if names["proj-a"] != "/tmp/a" || names["proj-b"] != "/tmp/b" {
		t.Errorf("unexpected projects: %v", names)
	}
}

func TestAddCustomProjectDuplicate(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.AddCustomProject(ctx, "dup", "/tmp/x"); err != nil {
		t.Fatal(err)
	}
	if err := db.AddCustomProject(ctx, "dup", "/tmp/y"); err == nil {
		t.Error("expected error for duplicate project name")
	}
}

func TestEnsureCustomProject(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.EnsureCustomProject(ctx, "proj", "/tmp/p1"); err != nil {
		t.Fatal(err)
	}
	// Second call should not error (INSERT OR IGNORE)
	if err := db.EnsureCustomProject(ctx, "proj", "/tmp/p2"); err != nil {
		t.Fatal(err)
	}

	projects, _ := db.GetCustomProjects(ctx)
	if len(projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Path != "/tmp/p1" {
		t.Errorf("expected original path, got %q", projects[0].Path)
	}
}

// ── GetProjectDir ─────────────────────────────────────────────

func TestGetProjectDirCustom(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	db.AddCustomProject(ctx, "my-proj", "/data/my-proj")

	dir, err := db.GetProjectDir(ctx, "my-proj")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/data/my-proj" {
		t.Errorf("expected /data/my-proj, got %q", dir)
	}
}

func TestGetProjectDirDefault(t *testing.T) {
	db := newTestDB(t)

	dir, err := db.GetProjectDir(context.Background(), "unknown")
	if err != nil {
		t.Fatal(err)
	}
	expected := "/app/projects/unknown"
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}
}

// ── AutomationMode ────────────────────────────────────────────

func TestGetAutomationModeDefault(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	mode, err := db.GetAutomationMode(ctx, "proj")
	if err != nil {
		t.Fatal(err)
	}
	if mode {
		t.Error("expected default mode to be false")
	}
}

func TestSetAndGetAutomationMode(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.SetAutomationMode(ctx, "proj", true); err != nil {
		t.Fatal(err)
	}
	mode, err := db.GetAutomationMode(ctx, "proj")
	if err != nil {
		t.Fatal(err)
	}
	if !mode {
		t.Error("expected mode true")
	}

	// Update to false
	if err := db.SetAutomationMode(ctx, "proj", false); err != nil {
		t.Fatal(err)
	}
	mode, _ = db.GetAutomationMode(ctx, "proj")
	if mode {
		t.Error("expected mode false after update")
	}
}

// ── TaskIndex ─────────────────────────────────────────────────

func TestGetCurrentTaskIndexDefault(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	idx, err := db.GetCurrentTaskIndex(ctx, "proj")
	if err != nil {
		t.Fatal(err)
	}
	if idx != -1 {
		t.Errorf("expected -1, got %d", idx)
	}
}

func TestSetAndGetTaskIndex(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.SetCurrentTaskIndex(ctx, "proj", 5); err != nil {
		t.Fatal(err)
	}
	idx, err := db.GetCurrentTaskIndex(ctx, "proj")
	if err != nil {
		t.Fatal(err)
	}
	if idx != 5 {
		t.Errorf("expected 5, got %d", idx)
	}

	// Update
	db.SetCurrentTaskIndex(ctx, "proj", 10)
	idx, _ = db.GetCurrentTaskIndex(ctx, "proj")
	if idx != 10 {
		t.Errorf("expected 10 after update, got %d", idx)
	}
}

// ── Conversation Messages ─────────────────────────────────────

func TestSaveAndGetUserMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	id, err := db.SaveUserMessage(ctx, "proj", "agent1", "Hello world")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	msgs, err := db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", msgs[0].Content)
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", msgs[0].Role)
	}
	if msgs[0].Project != "proj" {
		t.Errorf("expected project 'proj', got %q", msgs[0].Project)
	}
}

func TestSaveAssistantMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	id, err := db.SaveAssistantMessage(ctx, "proj", "agent1", "Hi there", "thinking...", `[{"tool":"bash"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	msgs, _ := db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", msgs[0].Role)
	}
	if msgs[0].Text != "Hi there" {
		t.Errorf("expected text 'Hi there', got %q", msgs[0].Text)
	}
	if msgs[0].Thinking != "thinking..." {
		t.Errorf("expected thinking, got %q", msgs[0].Thinking)
	}
	if msgs[0].ToolCalls != `[{"tool":"bash"}]` {
		t.Errorf("expected tool calls, got %q", msgs[0].ToolCalls)
	}
}

func TestSaveStreamingMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// First save creates the row
	if err := db.SaveStreamingMessage(ctx, "proj", "agent1", "Hello", "hmm"); err != nil {
		t.Fatal(err)
	}

	msgs, _ := db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Text != "Hello" {
		t.Errorf("expected 'Hello', got %q", msgs[0].Text)
	}
	if msgs[0].ToolCalls != "[streaming]" {
		t.Errorf("expected '[streaming]', got %q", msgs[0].ToolCalls)
	}

	// Second save updates the existing row
	if err := db.SaveStreamingMessage(ctx, "proj", "agent1", "Hello world", "hmm ok"); err != nil {
		t.Fatal(err)
	}

	msgs, _ = db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after update, got %d", len(msgs))
	}
	if msgs[0].Text != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", msgs[0].Text)
	}

	// Finalize replaces [streaming] with real tool calls
	if err := db.FinalizeStreamingMessage(ctx, "proj", "agent1", "Hello world done", "hmm ok done", `[{"tool":"bash"}]`); err != nil {
		t.Fatal(err)
	}

	msgs, _ = db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after finalize, got %d", len(msgs))
	}
	if msgs[0].Text != "Hello world done" {
		t.Errorf("expected 'Hello world done', got %q", msgs[0].Text)
	}
	if msgs[0].ToolCalls != `[{"tool":"bash"}]` {
		t.Errorf("expected real tool calls, got %q", msgs[0].ToolCalls)
	}
}

func TestFinalizeStreamingMessageNoRow(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	err := db.FinalizeStreamingMessage(ctx, "proj", "agent1", "text", "thinking", "[]")
	if err == nil {
		t.Error("expected error when no streaming row exists")
	}
}

func TestGetConversationHistoryLimit(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		db.SaveUserMessage(ctx, "proj", "a", "msg")
	}

	msgs, err := db.GetConversationHistory(ctx, "proj", "a", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages with limit, got %d", len(msgs))
	}
}

func TestGetConversationHistoryDefaultLimit(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		db.SaveUserMessage(ctx, "proj", "a", "msg")
	}

	// limit=0 should default to 200
	msgs, err := db.GetConversationHistory(ctx, "proj", "a", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Errorf("expected 5 messages, got %d", len(msgs))
	}
}

func TestGetConversationHistoryEmpty(t *testing.T) {
	db := newTestDB(t)
	msgs, err := db.GetConversationHistory(context.Background(), "proj", "a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestGetConversationHistoryMultipleAgents(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	db.SaveUserMessage(ctx, "proj", "agent-a", "msg for a")
	db.SaveUserMessage(ctx, "proj", "agent-b", "msg for b")

	msgsA, _ := db.GetConversationHistory(ctx, "proj", "agent-a", 100)
	msgsB, _ := db.GetConversationHistory(ctx, "proj", "agent-b", 100)

	if len(msgsA) != 1 || msgsA[0].Content != "msg for a" {
		t.Error("agent-a should have 1 message")
	}
	if len(msgsB) != 1 || msgsB[0].Content != "msg for b" {
		t.Error("agent-b should have 1 message")
	}
}

// ── ClearConversationHistory ──────────────────────────────────

func TestClearConversationHistory(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	db.SaveUserMessage(ctx, "proj", "agent1", "Hello")
	db.SaveAssistantMessage(ctx, "proj", "agent1", "Hi", "", "[]")

	msgs, _ := db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 before clear, got %d", len(msgs))
	}

	if err := db.ClearConversationHistory(ctx, "proj", "agent1"); err != nil {
		t.Fatal(err)
	}

	msgs, _ = db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if len(msgs) != 0 {
		t.Errorf("expected 0 after clear, got %d", len(msgs))
	}
}

// ── ConversationTitle ─────────────────────────────────────────

func TestSetConversationTitle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	db.SaveUserMessage(ctx, "proj", "agent1", "Hello")

	if err := db.SetConversationTitle(ctx, "proj", "agent1", "My Chat"); err != nil {
		t.Fatal(err)
	}

	// Verify title appears in summaries
	summaries, err := db.GetConversationSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Title != "My Chat" {
		t.Errorf("expected title 'My Chat', got %q", summaries[0].Title)
	}
}

// ── ConversationSummaries ─────────────────────────────────────

func TestGetConversationSummariesEmpty(t *testing.T) {
	db := newTestDB(t)
	summaries, err := db.GetConversationSummaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries, got %d", len(summaries))
	}
}

func TestGetConversationSummaries(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	db.SaveUserMessage(ctx, "proj1", "a1", "Hello")
	db.SaveAssistantMessage(ctx, "proj1", "a1", "Hi", "", "[]")
	db.SaveUserMessage(ctx, "proj2", "a2", "World")

	summaries, err := db.GetConversationSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	projects := map[string]bool{}
	for _, s := range summaries {
		projects[s.Project] = true
		if s.MessageCount == 0 {
			t.Errorf("expected non-zero message count for %s", s.Project)
		}
	}
	if !projects["proj1"] || !projects["proj2"] {
		t.Errorf("expected proj1 and proj2, got %v", projects)
	}
}

func TestConversationSummaryLastMessageTruncation(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	longMsg := make([]byte, 200)
	for i := range longMsg {
		longMsg[i] = 'x'
	}
	db.SaveUserMessage(ctx, "proj", "a", string(longMsg))

	summaries, _ := db.GetConversationSummaries(ctx)
	if len(summaries) != 1 {
		t.Fatal("expected 1 summary")
	}
	if len(summaries[0].LastMessage) > 80 {
		t.Errorf("last message should be truncated to 80, got %d", len(summaries[0].LastMessage))
	}
}

// ── Artifacts ─────────────────────────────────────────────────

func TestSaveAndGetArtifacts(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	a1 := &DBArtifact{ID: "art-1", AgentID: "agent-a", Type: "plan", Title: "Plan A", Content: "Do stuff"}
	a2 := &DBArtifact{ID: "art-2", AgentID: "agent-a", Type: "todo", Title: "Todo A", Content: "Items"}

	if err := db.SaveArtifact(ctx, a1); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveArtifact(ctx, a2); err != nil {
		t.Fatal(err)
	}

	arts, err := db.GetArtifactsByAgent(ctx, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(arts))
	}
	ids := map[string]bool{}
	for _, a := range arts {
		ids[a.ID] = true
		if a.AgentID != "agent-a" {
			t.Errorf("expected agent-a, got %q", a.AgentID)
		}
	}
	if !ids["art-1"] || !ids["art-2"] {
		t.Errorf("expected both artifacts, got %v", ids)
	}
}

func TestGetArtifactsByAgentEmpty(t *testing.T) {
	db := newTestDB(t)
	arts, err := db.GetArtifactsByAgent(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(arts))
	}
}

func TestSaveArtifactUpsert(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	a := &DBArtifact{ID: "art-1", AgentID: "a", Type: "plan", Title: "V1", Content: "original"}
	db.SaveArtifact(ctx, a)

	// Upsert
	a.Content = "updated"
	db.SaveArtifact(ctx, a)

	arts, _ := db.GetArtifactsByAgent(ctx, "a")
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact after upsert, got %d", len(arts))
	}
	if arts[0].Content != "updated" {
		t.Errorf("expected updated content, got %q", arts[0].Content)
	}
}

// ── Close ─────────────────────────────────────────────────────

func TestClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "close-test.db")
	db, err := NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestSaveToolResult(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	id, err := db.SaveToolResult(ctx, "proj", "agent1", "call_123", "bash", "total 42\nfile1.txt")
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}

	msgs, _ := db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "tool" {
		t.Errorf("expected role 'tool', got %q", msgs[0].Role)
	}
	if msgs[0].ToolCallID != "call_123" {
		t.Errorf("expected toolCallId 'call_123', got %q", msgs[0].ToolCallID)
	}
	if msgs[0].ToolName != "bash" {
		t.Errorf("expected toolName 'bash', got %q", msgs[0].ToolName)
	}
	if msgs[0].Content != "total 42\nfile1.txt" {
		t.Errorf("expected content, got %q", msgs[0].Content)
	}
}

func TestFullConversationRoundTrip(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Simulate a full conversation: user → assistant (with tool call) → tool result → assistant
	db.SaveUserMessage(ctx, "proj", "agent1", "List files in /tmp")

	toolCallsJSON := `[{"id":"call_abc","name":"bash","args":{"cmd":"ls /tmp"}}]`
	db.SaveAssistantMessage(ctx, "proj", "agent1", "", "", toolCallsJSON)

	db.SaveToolResult(ctx, "proj", "agent1", "call_abc", "bash", "file1.txt\nfile2.txt")

	db.SaveAssistantMessage(ctx, "proj", "agent1", "I found 2 files in /tmp: file1.txt and file2.txt", "", "[]")

	msgs, err := db.GetConversationHistory(ctx, "proj", "agent1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}

	// Verify user message
	if msgs[0].Role != "user" {
		t.Errorf("msg[0]: expected role 'user', got %q", msgs[0].Role)
	}
	if msgs[0].Content != "List files in /tmp" {
		t.Errorf("msg[0]: expected content, got %q", msgs[0].Content)
	}

	// Verify assistant message with tool calls
	if msgs[1].Role != "assistant" {
		t.Errorf("msg[1]: expected role 'assistant', got %q", msgs[1].Role)
	}
	if msgs[1].ToolCalls != toolCallsJSON {
		t.Errorf("msg[1]: expected tool_calls, got %q", msgs[1].ToolCalls)
	}

	// Verify tool result
	if msgs[2].Role != "tool" {
		t.Errorf("msg[2]: expected role 'tool', got %q", msgs[2].Role)
	}
	if msgs[2].ToolCallID != "call_abc" {
		t.Errorf("msg[2]: expected toolCallId 'call_abc', got %q", msgs[2].ToolCallID)
	}
	if msgs[2].ToolName != "bash" {
		t.Errorf("msg[2]: expected toolName 'bash', got %q", msgs[2].ToolName)
	}
	if msgs[2].Content != "file1.txt\nfile2.txt" {
		t.Errorf("msg[2]: expected content, got %q", msgs[2].Content)
	}

	// Verify final assistant response
	if msgs[3].Role != "assistant" {
		t.Errorf("msg[3]: expected role 'assistant', got %q", msgs[3].Role)
	}
	if msgs[3].Text != "I found 2 files in /tmp: file1.txt and file2.txt" {
		t.Errorf("msg[3]: expected text, got %q", msgs[3].Text)
	}
}

func TestMigrationAddsToolColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration-test.db")
	ctx := context.Background()

	// Create a DB with the OLD schema (no tool_call_id/tool_name)
	oldDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	_, err = oldDB.Exec(`
		CREATE TABLE IF NOT EXISTS system_config (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE IF NOT EXISTS conversation_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project TEXT NOT NULL,
			agent_id TEXT NOT NULL DEFAULT 'default',
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL DEFAULT '',
			thinking TEXT NOT NULL DEFAULT '',
			tool_calls TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		oldDB.Close()
		t.Fatal(err)
	}
	// Insert some data with the old schema
	_, _ = oldDB.Exec(`INSERT INTO conversation_messages (project, agent_id, role, content) VALUES ('test', 'a1', 'user', 'hello')`)
	oldDB.Close()

	// Open with NewDatabase — this should trigger migration
	db, err := NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Verify new columns exist by inserting a tool result
	_, err = db.SaveToolResult(ctx, "test", "a1", "call_1", "bash", "ok")
	if err != nil {
		t.Fatalf("SaveToolResult failed after migration: %v", err)
	}

	// Verify round-trip
	msgs, _ := db.GetConversationHistory(ctx, "test", "a1", 100)
	// Should have: original user message + new tool result
	var found bool
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == "call_1" && m.ToolName == "bash" {
			found = true
		}
	}
	if !found {
		t.Error("tool result not found after migration")
	}

	// Verify migration is marked as applied (idempotent)
	var count int
	db.DB().QueryRow(`SELECT COUNT(*) FROM system_config WHERE key = 'migration:add tool_call_id and tool_name columns'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected migration to be marked as applied, got count=%d", count)
	}
}
