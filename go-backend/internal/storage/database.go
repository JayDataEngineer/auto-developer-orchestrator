package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // Postgres driver
	_ "github.com/mattn/go-sqlite3"     // SQLite driver
)

// Database represents the application database.
// Supports both SQLite (default) and Postgres (cluster).
type Database struct {
	db          *sql.DB
	dialect     Dialect
	projectsDir string
	Conversations *ConversationStore // new 3-table conversation persistence

	// lastMsgID tracks the most recent assistant message ID for tool result association.
	// Updated by SaveAssistantMessage and FinalizeStreamingMessage, read by SaveToolResult.
	// Safe for sequential access patterns (single agent loop per session).
	lastMsgMu  sync.Mutex
	lastMsgID  int64
}

// NewDatabase creates a new database connection.
// Detects SQLite vs Postgres from the dataSource URL scheme:
//   - "postgres://..." or "postgresql://..." → cluster Postgres via pgx
//   - anything else → local SQLite
func NewDatabase(dataSource string) (*Database, error) {
	driverName, dialect := DetectDriver(dataSource)

	// Append SQLite connection params if needed
	openDSN := dataSource
	if dialect == DialectSQLite {
		openDSN = dataSource + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000"
	}

	db, err := sql.Open(driverName, openDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open database (%s): %w", driverName, err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Select DDL for the detected dialect
	ddl := sqliteDDL
	if dialect == DialectPostgres {
		ddl = postgresDDL
	}

	// Create tables
	if _, err = db.Exec(ddl); err != nil {
		return nil, fmt.Errorf("failed to create tables (%s): %w", driverName, err)
	}

	// Initialize new conversation tables (must happen before migrations that reference them)
	convStore := NewConversationStore(db, dialect)
	if err := convStore.MigrateNewTables(); err != nil {
		return nil, fmt.Errorf("failed to create conversation tables: %w", err)
	}

	// Run schema migrations for existing databases
	if err := runMigrations(db, dialect); err != nil {
		return nil, fmt.Errorf("failed to run migrations (%s): %w", driverName, err)
	}

	// Insert default config (dialect-safe)
	upsertConfig := Rebind(dialect, `INSERT INTO system_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`)
	_, _ = db.Exec(upsertConfig, "projectsDir", "/app/projects")

	return &Database{
		db:            db,
		dialect:       dialect,
		projectsDir:   "/app/projects",
		Conversations: convStore,
	}, nil
}

// runMigrations applies incremental schema changes to existing databases.
// Each migration is idempotent — it checks whether the change is needed before applying.
func runMigrations(db *sql.DB, dialect Dialect) error {
	type migration struct {
		name   string
		sql    string
		verify func(*sql.DB) error // optional SQLite verification (checks column exists after ALTER)
	}

	columnExists := func(table, column string) func(*sql.DB) error {
		return func(d *sql.DB) error {
			var count int
			d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count)
			if count < 1 {
				return fmt.Errorf("column %q not found in %q", column, table)
			}
			return nil
		}
	}

	migrations := []migration{
		{
			name: "add tool_call_id and tool_name columns",
			sql:  Rebind(dialect, `ALTER TABLE conversation_messages ADD COLUMN tool_call_id TEXT NOT NULL DEFAULT ''; ALTER TABLE conversation_messages ADD COLUMN tool_name TEXT NOT NULL DEFAULT ''`),
			verify: func(d *sql.DB) error {
				var count int
				d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('conversation_messages') WHERE name IN ('tool_call_id', 'tool_name')`).Scan(&count)
				if count < 2 {
					return fmt.Errorf("tool_call_id and tool_name columns not found in conversation_messages")
				}
				return nil
			},
		},
		{
			name:   "add status column to conversation_titles",
			sql:    Rebind(dialect, `ALTER TABLE conversation_titles ADD COLUMN status TEXT NOT NULL DEFAULT ''`),
			verify: columnExists("conversation_titles", "status"),
		},
		{
			name:   "add tool_calls column to messages table",
			sql:    Rebind(dialect, `ALTER TABLE messages ADD COLUMN tool_calls TEXT NOT NULL DEFAULT '[]'`),
			verify: columnExists("messages", "tool_calls"),
		},
	}

	for _, m := range migrations {
		// Check if migration already applied
		var count int
		row := db.QueryRow(Rebind(dialect,
			`SELECT COUNT(*) FROM system_config WHERE key = ?`), "migration:"+m.name)
		if err := row.Scan(&count); err == nil && count > 0 {
			continue // already applied
		}

		// Apply
		if _, err := db.Exec(m.sql); err != nil {
			if dialect == DialectSQLite {
				if m.verify != nil {
					if verr := m.verify(db); verr != nil {
						return fmt.Errorf("migration %q SQL error: %w; verify: %w", m.name, err, verr)
					}
				}
			} else {
				return fmt.Errorf("migration %q failed: %w", m.name, err)
			}
		}

		// Mark as applied
		db.Exec(Rebind(dialect,
			`INSERT INTO system_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`),
			"migration:"+m.name, "applied")
	}

	return nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// DB returns the underlying *sql.DB for use by other stores (e.g. EventStore).
func (d *Database) DB() *sql.DB {
	return d.db
}

// GetProjectsDir returns the configured projects directory.
func (d *Database) GetProjectsDir() string {
	return d.projectsDir
}

// GetCustomProjects returns all custom projects
func (d *Database) GetCustomProjects(ctx context.Context) ([]CustomProject, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT name, path FROM custom_projects ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []CustomProject{}
	for rows.Next() {
		var p CustomProject
		if err := rows.Scan(&p.Name, &p.Path); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}

	return projects, rows.Err()
}

// CustomProject represents a custom project
type CustomProject struct {
	Name string
	Path string
}

// AddCustomProject adds a custom project
func (d *Database) AddCustomProject(ctx context.Context, name, path string) error {
	_, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, "INSERT INTO custom_projects (name, path) VALUES (?, ?)"),
		name, path)
	return err
}

// EnsureCustomProject adds a custom project if it doesn't already exist
func (d *Database) EnsureCustomProject(ctx context.Context, name, path string) error {
	_, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, "INSERT INTO custom_projects (name, path) VALUES (?, ?) ON CONFLICT(name) DO NOTHING"),
		name, path)
	return err
}

// DeleteCustomProject removes a custom project by name.
func (d *Database) DeleteCustomProject(ctx context.Context, name string) error {
	_, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, "DELETE FROM custom_projects WHERE name = ?"),
		name)
	return err
}

// GetProjectDir returns the directory for a project
func (d *Database) GetProjectDir(ctx context.Context, projectName string) (string, error) {
	var path string
	err := d.db.QueryRowContext(ctx,
		Rebind(d.dialect, "SELECT path FROM custom_projects WHERE name = ?"), projectName).Scan(&path)
	if err == nil {
		return path, nil
	}
	return d.projectsDir + "/" + projectName, nil
}

// GetAutomationMode returns the automation mode for a project
func (d *Database) GetAutomationMode(ctx context.Context, projectName string) (bool, error) {
	var isAutoMode bool
	err := d.db.QueryRowContext(ctx,
		Rebind(d.dialect, "SELECT is_auto_mode FROM automation_modes WHERE project_name = ?"), projectName).Scan(&isAutoMode)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return isAutoMode, nil
}

// SetAutomationMode sets the automation mode for a project
func (d *Database) SetAutomationMode(ctx context.Context, projectName string, isAutoMode bool) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO automation_modes (project_name, is_auto_mode)
		VALUES (?, ?)
		ON CONFLICT(project_name) DO UPDATE SET is_auto_mode = ?, updated_at = CURRENT_TIMESTAMP`),
		projectName, isAutoMode, isAutoMode)
	return err
}

// GetCurrentTaskIndex returns the current task index for a project
func (d *Database) GetCurrentTaskIndex(ctx context.Context, projectName string) (int, error) {
	var index int
	err := d.db.QueryRowContext(ctx,
		Rebind(d.dialect, "SELECT current_index FROM task_indices WHERE project_name = ?"), projectName).Scan(&index)
	if err != nil {
		if err == sql.ErrNoRows {
			return -1, nil
		}
		return -1, err
	}
	return index, nil
}

// SetCurrentTaskIndex sets the current task index for a project
func (d *Database) SetCurrentTaskIndex(ctx context.Context, projectName string, index int) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO task_indices (project_name, current_index)
		VALUES (?, ?)
		ON CONFLICT(project_name) DO UPDATE SET current_index = ?, updated_at = CURRENT_TIMESTAMP`),
		projectName, index, index)
	return err
}

// StoredMessage represents a persisted conversation message.
type StoredMessage struct {
	ID         int64  `json:"id"`
	Project    string `json:"project"`
	AgentID    string `json:"agentId"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	Text       string `json:"text"`
	Thinking   string `json:"thinking"`
	ToolCalls  string `json:"toolCalls"`
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	CreatedAt  string `json:"createdAt"`
}

// Dialect returns the active database dialect.
func (d *Database) Dialect() Dialect { return d.dialect }

// SaveUserMessage persists a user message to the new messages table.
func (d *Database) SaveUserMessage(ctx context.Context, project, agentID, content string) (int64, error) {
	return d.Conversations.SaveUserMessage(ctx, project, agentID, content)
}

// SaveToolResult persists a tool result. Inserts a row into tool_executions
// linked to the most recent assistant message.
func (d *Database) SaveToolResult(ctx context.Context, project, agentID, toolCallID, toolName, content string) (int64, error) {
	d.lastMsgMu.Lock()
	msgID := d.lastMsgID
	d.lastMsgMu.Unlock()

	if msgID == 0 {
		d.db.QueryRowContext(ctx,
			Rebind(d.dialect, `SELECT id FROM messages WHERE project = ? AND agent_id = ? AND role = 'assistant' ORDER BY created_at DESC LIMIT 1`),
			project, agentID).Scan(&msgID)
	}

	if msgID > 0 {
		return d.Conversations.SaveToolExecution(ctx, msgID, toolCallID, toolName, "{}", content, "")
	}

	// Fallback: insert into legacy table
	res, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `INSERT INTO conversation_messages (project, agent_id, role, content, tool_call_id, tool_name) VALUES (?, ?, 'tool', ?, ?, ?)`),
		project, agentID, content, toolCallID, toolName)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveAssistantMessage persists an assistant response to the new tables.
// Inserts a messages row and thinking row (if non-empty). Tool call declarations
// are stored as JSON in the tool_calls column; execution results go into
// tool_executions via SaveToolResult.
func (d *Database) SaveAssistantMessage(ctx context.Context, project, agentID, text, thinking, toolCallsJSON string) (int64, error) {
	res, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `INSERT INTO messages (project, agent_id, role, content, tool_calls) VALUES (?, ?, 'assistant', ?, ?)`),
		project, agentID, text, toolCallsJSON)
	if err != nil {
		return 0, err
	}
	msgID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	d.lastMsgMu.Lock()
	d.lastMsgID = msgID
	d.lastMsgMu.Unlock()

	if thinking != "" {
		_, err = d.db.ExecContext(ctx,
			Rebind(d.dialect, `INSERT INTO thinking (message_id, content) VALUES (?, ?)`),
			msgID, thinking)
		if err != nil {
			return msgID, err
		}
	}

	return msgID, nil
}

// SaveStreamingMessage creates or updates an in-progress assistant message using the ConversationStore.
func (d *Database) SaveStreamingMessage(ctx context.Context, project, agentID, text, thinking string) error {
	sid := d.Conversations.StreamingID()
	if sid == 0 {
		_, err := d.Conversations.CreateStreamingRow(ctx, project, agentID)
		if err != nil {
			return err
		}
	}
	if err := d.Conversations.UpdateStreamingText(ctx, text); err != nil {
		return err
	}
	return d.Conversations.SaveStreamingThinking(ctx, thinking)
}

// FinalizeStreamingMessage completes a streaming placeholder with final content.
// Returns error if no streaming row exists (caller should fall back to SaveAssistantMessage).
func (d *Database) FinalizeStreamingMessage(ctx context.Context, project, agentID, text, thinking, toolCallsJSON string) error {
	msgID, err := d.Conversations.FinalizeStreaming(ctx, text, thinking)
	if err != nil {
		return err
	}

	// Store message ID for subsequent SaveToolResult calls
	d.lastMsgMu.Lock()
	d.lastMsgID = msgID
	d.lastMsgMu.Unlock()

	// Update tool_calls on the message
	if toolCallsJSON != "" && toolCallsJSON != "[]" {
		_, err = d.db.ExecContext(ctx,
			Rebind(d.dialect, `UPDATE messages SET tool_calls = ? WHERE id = ?`),
			toolCallsJSON, msgID)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetConversationHistory returns messages for a project+agent, ordered by creation time.
// Reads from the new tables (messages, thinking, tool_executions) with fallback to legacy.
func (d *Database) GetConversationHistory(ctx context.Context, project, agentID string, limit int) ([]StoredMessage, error) {
	if limit <= 0 {
		limit = 200
	}

	// Try new tables first
	msgRows, err := d.db.QueryContext(ctx,
		Rebind(d.dialect, `SELECT m.id, m.project, m.agent_id, m.role, m.content, m.tool_calls,
			COALESCE(t.content, '') AS thinking,
			m.created_at
			FROM messages m
			LEFT JOIN thinking t ON t.message_id = m.id
			WHERE m.project = ? AND m.agent_id = ?
			ORDER BY m.created_at ASC, m.id ASC
			LIMIT ?`),
		project, agentID, limit)
	if err != nil {
		return d.getConversationHistoryLegacy(ctx, project, agentID, limit)
	}
	defer msgRows.Close()

	// Load tool executions for all returned messages in one query
	// Use a map of message_id → []StoredMessage for tool results
	type toolExec struct {
		MessageID  int64
		ToolCallID string
		ToolName   string
		Result     string
		CreatedAt  string
	}
	var msgs []StoredMessage
	var msgIDs []int64
	msgIDSet := map[int64]bool{}
	for msgRows.Next() {
		var id int64
		var proj, agID, role, content, toolCalls, thinking, createdAt string
		if err := msgRows.Scan(&id, &proj, &agID, &role, &content, &toolCalls, &thinking, &createdAt); err != nil {
			return nil, err
		}

		if role == "user" {
			msgs = append(msgs, StoredMessage{
				ID: id, Project: proj, AgentID: agID,
				Role: "user", Content: content, CreatedAt: createdAt,
			})
		} else if role == "assistant" {
			// Skip streaming placeholder rows (empty content, no tool_calls)
			if content == "" && thinking == "" && toolCalls == "[]" {
				continue
			}
			msgs = append(msgs, StoredMessage{
				ID: id, Project: proj, AgentID: agID,
				Role: "assistant", Text: content, Thinking: thinking,
				ToolCalls: toolCalls, CreatedAt: createdAt,
			})
			msgIDs = append(msgIDs, id)
			msgIDSet[id] = true
		}
	}

	if len(msgs) == 0 {
		return d.getConversationHistoryLegacy(ctx, project, agentID, limit)
	}

	// Load tool executions for the assistant messages
	if len(msgIDs) > 0 {
		// Build IN clause
		query := Rebind(d.dialect, `SELECT message_id, tool_call_id, tool_name, result, created_at
			FROM tool_executions WHERE message_id IN (`)
		args := make([]interface{}, len(msgIDs))
		for i, id := range msgIDs {
			if i > 0 {
				query += ","
			}
			query += "?"
			args[i] = id
		}
		query += `) ORDER BY id ASC`

		teRows, err := d.db.QueryContext(ctx, query, args...)
		if err == nil {
			var toolExecs []toolExec
			for teRows.Next() {
				var te toolExec
				if err := teRows.Scan(&te.MessageID, &te.ToolCallID, &te.ToolName, &te.Result, &te.CreatedAt); err == nil {
					toolExecs = append(toolExecs, te)
				}
			}
			teRows.Close()

			// Interleave tool results after their parent assistant messages
			var merged []StoredMessage
			for _, m := range msgs {
				merged = append(merged, m)
				if m.Role == "assistant" {
					for _, te := range toolExecs {
						if te.MessageID == m.ID {
							merged = append(merged, StoredMessage{
								Role: "tool", Content: te.Result,
								ToolCallID: te.ToolCallID, ToolName: te.ToolName,
								Project: m.Project, AgentID: m.AgentID,
								CreatedAt: te.CreatedAt,
							})
						}
					}
				}
			}
			msgs = merged
		}
	}

	return msgs, nil
}

// getConversationHistoryLegacy reads from the old conversation_messages table as fallback.
func (d *Database) getConversationHistoryLegacy(ctx context.Context, project, agentID string, limit int) ([]StoredMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.db.QueryContext(ctx,
		Rebind(d.dialect, `SELECT id, project, agent_id, role, content, text, thinking, tool_calls, tool_call_id, tool_name, created_at
		 FROM conversation_messages
		 WHERE project = ? AND agent_id = ?
		 ORDER BY created_at ASC
		 LIMIT ?`),
		project, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []StoredMessage
	for rows.Next() {
		var m StoredMessage
		if err := rows.Scan(&m.ID, &m.Project, &m.AgentID, &m.Role, &m.Content, &m.Text, &m.Thinking, &m.ToolCalls, &m.ToolCallID, &m.ToolName, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// ClearConversationHistory deletes all messages, thinking, tool_executions, titles, and artifacts for a project+agent.
func (d *Database) ClearConversationHistory(ctx context.Context, project, agentID string) error {
	// Clear new tables
	if err := d.Conversations.Clear(ctx, project, agentID); err != nil {
		return err
	}
	// Clear artifacts for this agent
	_, _ = d.db.ExecContext(ctx,
		`DELETE FROM artifacts WHERE agent_id = ?`,
		agentID)
	// Clear legacy table and titles
	_, _ = d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM conversation_messages WHERE project = ? AND agent_id = ?`),
		project, agentID)
	_, _ = d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM conversation_titles WHERE project = ? AND agent_id = ?`),
		project, agentID)
	return nil
}

// ClearProjectConversations deletes all conversation data for every session in a project.
func (d *Database) ClearProjectConversations(ctx context.Context, project string) error {
	// Clear new tables
	if _, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM tool_executions WHERE message_id IN (
			SELECT id FROM messages WHERE project = ?)`),
		project); err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM thinking WHERE message_id IN (
			SELECT id FROM messages WHERE project = ?)`),
		project); err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM messages WHERE project = ?`),
		project); err != nil {
		return err
	}
	// Also clear legacy and titles
	_, _ = d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM conversation_messages WHERE project = ?`),
		project)
	_, _ = d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM conversation_titles WHERE project = ?`),
		project)
	return nil
}

// CompactSession removes old tool-execution and thinking rows for a project+agent,
// keeping the most recent ones. Returns the number of items removed.
func (d *Database) CompactSession(ctx context.Context, project, agentID string) (int, error) {
	// Count total related items (messages + tool_executions + thinking)
	var total int
	d.db.QueryRowContext(ctx,
		Rebind(d.dialect, `SELECT COUNT(*) FROM messages WHERE project = ? AND agent_id = ?`),
		project, agentID).Scan(&total)

	keep := 20
	if total <= keep {
		return 0, nil
	}

	// Keep the most recent `keep` messages; delete older ones and their children
	removed := 0

	result, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		DELETE FROM tool_executions WHERE message_id IN (
			SELECT id FROM messages
			WHERE project = ? AND agent_id = ?
			  AND id NOT IN (
			    SELECT id FROM messages
			    WHERE project = ? AND agent_id = ?
			    ORDER BY created_at DESC
			    LIMIT ?
			  )
		)`),
		project, agentID, project, agentID, keep)
	if err == nil {
		n, _ := result.RowsAffected()
		removed += int(n)
	}

	result, err = d.db.ExecContext(ctx, Rebind(d.dialect, `
		DELETE FROM thinking WHERE message_id IN (
			SELECT id FROM messages
			WHERE project = ? AND agent_id = ?
			  AND id NOT IN (
			    SELECT id FROM messages
			    WHERE project = ? AND agent_id = ?
			    ORDER BY created_at DESC
			    LIMIT ?
			  )
		)`),
		project, agentID, project, agentID, keep)
	if err == nil {
		n, _ := result.RowsAffected()
		removed += int(n)
	}

	result, err = d.db.ExecContext(ctx, Rebind(d.dialect, `
		DELETE FROM messages
		WHERE project = ? AND agent_id = ?
		  AND id NOT IN (
		    SELECT id FROM messages
		    WHERE project = ? AND agent_id = ?
		    ORDER BY created_at DESC
		    LIMIT ?
		  )`),
		project, agentID, project, agentID, keep)
	if err == nil {
		n, _ := result.RowsAffected()
		removed += int(n)
	}

	return removed, nil
}

// SetConversationTitle sets a custom title for a conversation.
func (d *Database) SetConversationTitle(ctx context.Context, project, agentID, title string) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO conversation_titles (project, agent_id, title)
		VALUES (?, ?, ?)
		ON CONFLICT(project, agent_id) DO UPDATE SET title = excluded.title`),
		project, agentID, title)
	return err
}

// SetConversationStatus updates the status of a conversation (processing, unread, read).
func (d *Database) SetConversationStatus(ctx context.Context, project, agentID, status string) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO conversation_titles (project, agent_id, title, status)
		VALUES (?, ?, '', ?)
		ON CONFLICT(project, agent_id) DO UPDATE SET status = excluded.status`),
		project, agentID, status)
	return err
}

// ConversationSummary is a summary of a single conversation session.
type ConversationSummary struct {
	Project      string `json:"project"`
	AgentID      string `json:"agentId"`
	LastMessage  string `json:"lastMessage"`
	LastAt       string `json:"lastAt"`
	MessageCount int    `json:"messageCount"`
	Title        string `json:"title"`
	Status       string `json:"status,omitempty"` // "running" if agent is active
}

// GetConversationSummaries returns the latest conversation for each project+agent pair
// using the new messages table (with fallback to legacy).
func (d *Database) GetConversationSummaries(ctx context.Context) ([]ConversationSummary, error) {
	// Try new tables first
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			m.project,
			m.agent_id,
			COALESCE(
				(SELECT content FROM messages sub
				 WHERE sub.project = m.project AND sub.agent_id = m.agent_id AND sub.role = 'user'
				 ORDER BY sub.created_at DESC LIMIT 1),
				''
			) AS last_message,
			MAX(m.created_at) AS last_at,
			COUNT(*) AS message_count,
			COALESCE(
				NULLIF(ct.title, ''),
				(SELECT content FROM messages first_um
				 WHERE first_um.project = m.project AND first_um.agent_id = m.agent_id AND first_um.role = 'user'
				 ORDER BY first_um.created_at ASC LIMIT 1),
				''
			) AS title,
			COALESCE(ct.status, '') AS status
		FROM messages m
		LEFT JOIN conversation_titles ct ON ct.project = m.project AND ct.agent_id = m.agent_id
		GROUP BY m.project, m.agent_id
		ORDER BY last_at DESC
	`)
	if err != nil {
		// Fallback to legacy table
		return d.getConversationSummariesLegacy(ctx)
	}
	defer rows.Close()

	var summaries []ConversationSummary
	for rows.Next() {
		var s ConversationSummary
		if err := rows.Scan(&s.Project, &s.AgentID, &s.LastMessage, &s.LastAt, &s.MessageCount, &s.Title, &s.Status); err != nil {
			return nil, err
		}
		if len(s.LastMessage) > 80 {
			s.LastMessage = s.LastMessage[:80]
		}
		summaries = append(summaries, s)
	}
	if len(summaries) > 0 {
		return summaries, nil
	}

	return d.getConversationSummariesLegacy(ctx)
}

// getConversationSummariesLegacy queries the old conversation_messages table.
func (d *Database) getConversationSummariesLegacy(ctx context.Context) ([]ConversationSummary, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT
			cm.project,
			cm.agent_id,
			COALESCE(
				(SELECT content FROM conversation_messages sub
				 WHERE sub.project = cm.project AND sub.agent_id = cm.agent_id AND sub.role = 'user'
				 ORDER BY sub.created_at DESC LIMIT 1),
				''
			) AS last_message,
			MAX(cm.created_at) AS last_at,
			COUNT(*) AS message_count,
			COALESCE(
				NULLIF(ct.title, ''),
				(SELECT content FROM conversation_messages first_um
				 WHERE first_um.project = cm.project AND first_um.agent_id = cm.agent_id AND first_um.role = 'user'
				 ORDER BY first_um.created_at ASC LIMIT 1),
				''
			) AS title,
			COALESCE(ct.status, '') AS status
		FROM conversation_messages cm
		LEFT JOIN conversation_titles ct ON ct.project = cm.project AND ct.agent_id = cm.agent_id
		GROUP BY cm.project, cm.agent_id
		ORDER BY last_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []ConversationSummary
	for rows.Next() {
		var s ConversationSummary
		if err := rows.Scan(&s.Project, &s.AgentID, &s.LastMessage, &s.LastAt, &s.MessageCount, &s.Title, &s.Status); err != nil {
			return nil, err
		}
		if len(s.LastMessage) > 80 {
			s.LastMessage = s.LastMessage[:80]
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

// DBArtifact represents a persisted agent artifact.
type DBArtifact struct {
	ID        string `json:"id"`
	AgentID   string `json:"agentId"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updatedAt"`
}

// SaveArtifact persists an artifact, inserting or replacing by ID.
func (d *Database) SaveArtifact(ctx context.Context, a *DBArtifact) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO artifacts (id, agent_id, type, title, content, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET agent_id = excluded.agent_id, type = excluded.type,
			title = excluded.title, content = excluded.content, updated_at = CURRENT_TIMESTAMP`),
		a.ID, a.AgentID, a.Type, a.Title, a.Content)
	return err
}

// GetArtifactsByAgent returns all artifacts for a given agent.
func (d *Database) GetArtifactsByAgent(ctx context.Context, agentID string) ([]*DBArtifact, error) {
	rows, err := d.db.QueryContext(ctx,
		"SELECT id, agent_id, type, title, content, updated_at FROM artifacts WHERE agent_id = ?",
		agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifacts []*DBArtifact
	for rows.Next() {
		var a DBArtifact
		if err := rows.Scan(&a.ID, &a.AgentID, &a.Type, &a.Title, &a.Content, &a.UpdatedAt); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, &a)
	}
	return artifacts, rows.Err()
}

// ── Scheduled Jobs ─────────────────────────────────────────────────────

// ScheduledJob is the database representation of a scheduler.Job.
type ScheduledJob struct {
	ID                   string
	Name                 string
	Description          string
	Project              string
	AgentID              string
	Message              string
	Model                string
	Org                  string
	ScheduleType         string
	CronExpr             string
	Timezone             string
	EverySeconds         int64
	AtTime               string
	AutoBranch           bool
	AutoMerge            bool
	Enabled              bool
	DeliveryMode         string
	DeliveryWebhookURL   string
	DeliveryBestEffort   bool
	FailureAlertAfter    int
	FailureAlertWebhookURL string
	Status               string
	LastRunAt            *time.Time
	LastRunStatus        string
	LastError            string
	NextRunAt            *time.Time
	ConsecutiveErrors    int
	InputTokens          int
	OutputTokens         int
	DurationMs           int64
	Blocks               string // JSON array
	BlockedBy            string // JSON array
	LastOutput           string // last successful output (for context chaining)
	ContextFrom          string // JSON array of job IDs whose output is injected as context
	SandboxOnly          bool
	WebhookToken         string
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
}

const scheduledJobCols = `id, name, description, project, agent_id, message, model, org,
	schedule_type, cron_expr, timezone, every_seconds, at_time,
	auto_branch, auto_merge, enabled,
	delivery_mode, delivery_webhook_url, delivery_best_effort,
	failure_alert_after, failure_alert_webhook_url,
	status, last_run_at, last_run_status, last_error, next_run_at,
	consecutive_errors, input_tokens, output_tokens, duration_ms,
	blocks, blocked_by, sandbox_only, webhook_token, created_at, updated_at`

func scanScheduledJob(row interface{ Scan(...interface{}) error }, j *ScheduledJob) error {
	return row.Scan(
		&j.ID, &j.Name, &j.Description, &j.Project, &j.AgentID, &j.Message, &j.Model, &j.Org,
		&j.ScheduleType, &j.CronExpr, &j.Timezone, &j.EverySeconds, &j.AtTime,
		&j.AutoBranch, &j.AutoMerge, &j.Enabled,
		&j.DeliveryMode, &j.DeliveryWebhookURL, &j.DeliveryBestEffort,
		&j.FailureAlertAfter, &j.FailureAlertWebhookURL,
		&j.Status, &j.LastRunAt, &j.LastRunStatus, &j.LastError, &j.NextRunAt,
		&j.ConsecutiveErrors, &j.InputTokens, &j.OutputTokens, &j.DurationMs,
		&j.Blocks, &j.BlockedBy, &j.SandboxOnly, &j.WebhookToken, &j.CreatedAt, &j.UpdatedAt,
	)
}

// SaveScheduledJob upserts a scheduled job.
func (d *Database) SaveScheduledJob(ctx context.Context, j *ScheduledJob) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO scheduled_jobs (`+scheduledJobCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?,  ?, ?, ?, ?, ?,  ?, ?, ?,  ?, ?, ?,  ?, ?,  ?, ?, ?, ?, ?,  ?, ?, ?, ?,  ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, description=excluded.description, project=excluded.project,
			agent_id=excluded.agent_id, message=excluded.message, model=excluded.model, org=excluded.org,
			schedule_type=excluded.schedule_type, cron_expr=excluded.cron_expr, timezone=excluded.timezone,
			every_seconds=excluded.every_seconds, at_time=excluded.at_time,
			auto_branch=excluded.auto_branch, auto_merge=excluded.auto_merge, enabled=excluded.enabled,
			delivery_mode=excluded.delivery_mode, delivery_webhook_url=excluded.delivery_webhook_url,
			delivery_best_effort=excluded.delivery_best_effort,
			failure_alert_after=excluded.failure_alert_after, failure_alert_webhook_url=excluded.failure_alert_webhook_url,
			status=excluded.status, last_run_at=excluded.last_run_at, last_run_status=excluded.last_run_status,
			last_error=excluded.last_error, next_run_at=excluded.next_run_at,
			consecutive_errors=excluded.consecutive_errors, input_tokens=excluded.input_tokens,
			output_tokens=excluded.output_tokens, duration_ms=excluded.duration_ms,
			blocks=excluded.blocks, blocked_by=excluded.blocked_by, sandbox_only=excluded.sandbox_only,
			webhook_token=excluded.webhook_token, updated_at=CURRENT_TIMESTAMP`),
		j.ID, j.Name, j.Description, j.Project, j.AgentID, j.Message, j.Model, j.Org,
		j.ScheduleType, j.CronExpr, j.Timezone, j.EverySeconds, j.AtTime,
		j.AutoBranch, j.AutoMerge, j.Enabled,
		j.DeliveryMode, j.DeliveryWebhookURL, j.DeliveryBestEffort,
		j.FailureAlertAfter, j.FailureAlertWebhookURL,
		j.Status, j.LastRunAt, j.LastRunStatus, j.LastError, j.NextRunAt,
		j.ConsecutiveErrors, j.InputTokens, j.OutputTokens, j.DurationMs,
		j.Blocks, j.BlockedBy, j.SandboxOnly, j.WebhookToken, j.CreatedAt, j.UpdatedAt,
	)
	return err
}

// GetScheduledJob returns a single job by ID.
func (d *Database) GetScheduledJob(ctx context.Context, id string) (*ScheduledJob, error) {
	var j ScheduledJob
	err := scanScheduledJob(d.db.QueryRowContext(ctx,
		Rebind(d.dialect, `SELECT `+scheduledJobCols+` FROM scheduled_jobs WHERE id = ?`), id), &j)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// ListScheduledJobs returns all scheduled jobs.
func (d *Database) ListScheduledJobs(ctx context.Context) ([]*ScheduledJob, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT `+scheduledJobCols+` FROM scheduled_jobs ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*ScheduledJob
	for rows.Next() {
		var j ScheduledJob
		if err := scanScheduledJob(rows, &j); err != nil {
			return nil, err
		}
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}

// DeleteScheduledJob removes a job by ID.
func (d *Database) DeleteScheduledJob(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `DELETE FROM scheduled_jobs WHERE id = ?`), id)
	return err
}

// FindJobByWebhookToken finds a job by its webhook token.
func (d *Database) FindJobByWebhookToken(ctx context.Context, token string) (*ScheduledJob, error) {
	var j ScheduledJob
	err := scanScheduledJob(d.db.QueryRowContext(ctx,
		Rebind(d.dialect, `SELECT `+scheduledJobCols+` FROM scheduled_jobs WHERE webhook_token = ?`), token), &j)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// FindJobByProjectAndName finds a job by project and name.
func (d *Database) FindJobByProjectAndName(ctx context.Context, project, name string) (*ScheduledJob, error) {
	var j ScheduledJob
	err := scanScheduledJob(d.db.QueryRowContext(ctx,
		Rebind(d.dialect, `SELECT `+scheduledJobCols+` FROM scheduled_jobs WHERE project = ? AND name = ?`), project, name), &j)
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// ── Scheduler Run Logs ─────────────────────────────────────────────────

// DBRunLogEntry is the database representation of a scheduler run log entry.
type DBRunLogEntry struct {
	ID             int64
	JobID          string
	Action         string
	Status         string
	Error          string
	Summary        string
	Delivered      bool
	DeliveryStatus string
	DeliveryError  string
	SessionID      string
	RunAtMs        int64
	DurationMs     int64
	NextRunAtMs    int64
	Model          string
	Provider       string
	InputTokens    int
	OutputTokens   int
	CacheTokens    int
	CreatedAt      *time.Time
}

const runLogCols = `id, job_id, action, status, error, summary, delivered, delivery_status, delivery_error,
	session_id, run_at_ms, duration_ms, next_run_at_ms, model, provider,
	input_tokens, output_tokens, cache_tokens, created_at`

func scanRunLog(row interface{ Scan(...interface{}) error }, e *DBRunLogEntry) error {
	return row.Scan(
		&e.ID, &e.JobID, &e.Action, &e.Status, &e.Error, &e.Summary,
		&e.Delivered, &e.DeliveryStatus, &e.DeliveryError,
		&e.SessionID, &e.RunAtMs, &e.DurationMs, &e.NextRunAtMs,
		&e.Model, &e.Provider, &e.InputTokens, &e.OutputTokens, &e.CacheTokens, &e.CreatedAt,
	)
}

// AppendRunLog inserts a run log entry.
func (d *Database) AppendRunLog(ctx context.Context, e *DBRunLogEntry) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO scheduler_run_logs (job_id, action, status, error, summary, delivered, delivery_status, delivery_error,
			session_id, run_at_ms, duration_ms, next_run_at_ms, model, provider,
			input_tokens, output_tokens, cache_tokens)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?,  ?, ?, ?, ?,  ?, ?,  ?, ?, ?)`),
		e.JobID, e.Action, e.Status, e.Error, e.Summary, e.Delivered, e.DeliveryStatus, e.DeliveryError,
		e.SessionID, e.RunAtMs, e.DurationMs, e.NextRunAtMs, e.Model, e.Provider,
		e.InputTokens, e.OutputTokens, e.CacheTokens,
	)
	return err
}

// ReadRunLogs returns the most recent run log entries for a job.
func (d *Database) ReadRunLogs(ctx context.Context, jobID string, limit int) ([]*DBRunLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.QueryContext(ctx,
		Rebind(d.dialect, `SELECT `+runLogCols+` FROM scheduler_run_logs WHERE job_id = ? ORDER BY created_at DESC LIMIT ?`),
		jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*DBRunLogEntry
	for rows.Next() {
		var e DBRunLogEntry
		if err := scanRunLog(rows, &e); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// ReadAllRunLogs returns run log entries across all jobs with optional filters.
func (d *Database) ReadAllRunLogs(ctx context.Context, limit int, statusFilter, jobIDFilter string) ([]*DBRunLogEntry, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT ` + runLogCols + ` FROM scheduler_run_logs WHERE 1=1`
	var args []interface{}
	if statusFilter != "" && statusFilter != "all" {
		query += ` AND status = ?`
		args = append(args, statusFilter)
	}
	if jobIDFilter != "" {
		query += ` AND job_id = ?`
		args = append(args, jobIDFilter)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, Rebind(d.dialect, query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*DBRunLogEntry
	for rows.Next() {
		var e DBRunLogEntry
		if err := scanRunLog(rows, &e); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// PruneRunLogs keeps only the last keepN entries for a job.
func (d *Database) PruneRunLogs(ctx context.Context, jobID string, keepN int) error {
	if keepN <= 0 {
		keepN = 2000
	}
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		DELETE FROM scheduler_run_logs
		WHERE job_id = ? AND id NOT IN (
			SELECT id FROM scheduler_run_logs WHERE job_id = ? ORDER BY created_at DESC LIMIT ?
		)`), jobID, jobID, keepN)
	return err
}
