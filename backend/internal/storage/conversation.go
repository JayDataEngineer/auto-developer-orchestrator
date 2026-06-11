package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// ConversationStore provides clean persistence for conversations using
// separate tables for messages, thinking, and tool executions.
//
// The legacy conversation_messages table is kept for backward compatibility.
// GetConversationHistory falls back to the legacy table when the new tables
// are empty.
type ConversationStore struct {
	db      *sql.DB
	dialect Dialect

	// streamingID tracks the row ID of the current in-progress assistant message.
	// Replaces the old [streaming] sentinel hack.
	mu          sync.Mutex
	streamingID int64 // 0 = no active streaming row
}

// NewConversationStore creates a store backed by the given database.
func NewConversationStore(db *sql.DB, dialect Dialect) *ConversationStore {
	return &ConversationStore{db: db, dialect: dialect}
}

// MigrateNewTables creates the new conversation tables if they don't exist.
func (s *ConversationStore) MigrateNewTables() error {
	var ddl string
	if s.dialect == DialectPostgres {
		ddl = newTablesPG
	} else {
		ddl = newTablesSQLite
	}
	_, err := s.db.Exec(ddl)
	return err
}

const newTablesPG = `
CREATE TABLE IF NOT EXISTS messages (
    id SERIAL PRIMARY KEY,
    project TEXT NOT NULL,
    agent_id TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content TEXT NOT NULL DEFAULT '',
    tool_calls TEXT NOT NULL DEFAULT '[]',
    model TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_project_agent
    ON messages(project, agent_id, created_at);

CREATE TABLE IF NOT EXISTS thinking (
    id SERIAL PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_thinking_message
    ON thinking(message_id);

CREATE TABLE IF NOT EXISTS tool_executions (
    id SERIAL PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    args TEXT NOT NULL DEFAULT '{}',
    result TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tool_execs_message
    ON tool_executions(message_id);
`

const newTablesSQLite = `
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project TEXT NOT NULL,
    agent_id TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content TEXT NOT NULL DEFAULT '',
    tool_calls TEXT NOT NULL DEFAULT '[]',
    model TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_project_agent
    ON messages(project, agent_id, created_at);

CREATE TABLE IF NOT EXISTS thinking (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_thinking_message
    ON thinking(message_id);

CREATE TABLE IF NOT EXISTS tool_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tool_call_id TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    args TEXT NOT NULL DEFAULT '{}',
    result TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tool_execs_message
    ON tool_executions(message_id);
`

// --- Write operations (new tables) ---

// SaveUserMessage persists a user message.
func (s *ConversationStore) SaveUserMessage(ctx context.Context, project, agentID, content string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		Rebind(s.dialect, `INSERT INTO messages (project, agent_id, role, content) VALUES (?, ?, 'user', ?)`),
		project, agentID, content)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CreateStreamingRow creates an in-progress assistant message row and returns its ID.
// Call UpdateStreamingText to append content, then FinalizeStreaming to complete it.
func (s *ConversationStore) CreateStreamingRow(ctx context.Context, project, agentID string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		Rebind(s.dialect, `INSERT INTO messages (project, agent_id, role, content) VALUES (?, ?, 'assistant', '')`),
		project, agentID)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.streamingID = id
	s.mu.Unlock()
	return id, nil
}

// UpdateStreamingText updates the text content of the current streaming row.
func (s *ConversationStore) UpdateStreamingText(ctx context.Context, text string) error {
	s.mu.Lock()
	id := s.streamingID
	s.mu.Unlock()
	if id == 0 {
		return fmt.Errorf("no active streaming row")
	}
	_, err := s.db.ExecContext(ctx,
		Rebind(s.dialect, `UPDATE messages SET content = ? WHERE id = ?`),
		text, id)
	return err
}

// SaveStreamingThinking upserts the thinking content for the current streaming row.
func (s *ConversationStore) SaveStreamingThinking(ctx context.Context, thinking string) error {
	s.mu.Lock()
	id := s.streamingID
	s.mu.Unlock()
	if id == 0 {
		return fmt.Errorf("no active streaming row")
	}

	// Upsert: insert thinking row if none exists, otherwise update
	var existingID int64
	err := s.db.QueryRowContext(ctx,
		Rebind(s.dialect, `SELECT id FROM thinking WHERE message_id = ?`),
		id).Scan(&existingID)
	if err == sql.ErrNoRows {
		_, err = s.db.ExecContext(ctx,
			Rebind(s.dialect, `INSERT INTO thinking (message_id, content) VALUES (?, ?)`),
			id, thinking)
		return err
	}
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		Rebind(s.dialect, `UPDATE thinking SET content = ? WHERE id = ?`),
		thinking, existingID)
	return err
}

// FinalizeStreaming completes the current streaming row with final content and resets.
// Returns the message ID of the finalized row.
func (s *ConversationStore) FinalizeStreaming(ctx context.Context, text, thinking string) (int64, error) {
	s.mu.Lock()
	id := s.streamingID
	s.streamingID = 0
	s.mu.Unlock()
	if id == 0 {
		return 0, fmt.Errorf("no active streaming row")
	}

	// Update message text
	if _, err := s.db.ExecContext(ctx,
		Rebind(s.dialect, `UPDATE messages SET content = ? WHERE id = ?`),
		text, id); err != nil {
		return 0, err
	}

	// Upsert thinking
	if thinking != "" {
		var existingID int64
		err := s.db.QueryRowContext(ctx,
			Rebind(s.dialect, `SELECT id FROM thinking WHERE message_id = ?`),
			id).Scan(&existingID)
		if err == sql.ErrNoRows {
			_, err = s.db.ExecContext(ctx,
				Rebind(s.dialect, `INSERT INTO thinking (message_id, content) VALUES (?, ?)`),
				id, thinking)
		} else if err == nil {
			_, err = s.db.ExecContext(ctx,
				Rebind(s.dialect, `UPDATE thinking SET content = ? WHERE id = ?`),
				thinking, existingID)
		}
		if err != nil {
			return 0, err
		}
	}
	return id, nil
}

// SaveToolExecution persists a tool call and its result for a given message.
func (s *ConversationStore) SaveToolExecution(ctx context.Context, messageID int64, toolCallID, toolName, argsJSON, resultJSON, errMsg string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		Rebind(s.dialect, `INSERT INTO tool_executions (message_id, tool_call_id, tool_name, args, result, error) VALUES (?, ?, ?, ?, ?, ?)`),
		messageID, toolCallID, toolName, argsJSON, resultJSON, errMsg)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// StreamingID returns the current streaming row ID (0 if none).
func (s *ConversationStore) StreamingID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streamingID
}

// Clear deletes all messages, thinking, and tool_executions for a project+agent.
func (s *ConversationStore) Clear(ctx context.Context, project, agentID string) error {
	// Delete tool executions first (FK dependency)
	_, err := s.db.ExecContext(ctx,
		Rebind(s.dialect, `DELETE FROM tool_executions WHERE message_id IN (
			SELECT id FROM messages WHERE project = ? AND agent_id = ?)`),
		project, agentID)
	if err != nil {
		return err
	}
	// Delete thinking (FK dependency)
	_, err = s.db.ExecContext(ctx,
		Rebind(s.dialect, `DELETE FROM thinking WHERE message_id IN (
			SELECT id FROM messages WHERE project = ? AND agent_id = ?)`),
		project, agentID)
	if err != nil {
		return err
	}
	// Delete messages
	_, err = s.db.ExecContext(ctx,
		Rebind(s.dialect, `DELETE FROM messages WHERE project = ? AND agent_id = ?`),
		project, agentID)
	return err
}
