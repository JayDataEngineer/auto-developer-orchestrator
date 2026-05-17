package storage

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // Postgres driver
	_ "github.com/mattn/go-sqlite3"     // SQLite driver
)

// Database represents the application database.
// Supports both SQLite (default) and Postgres (cluster).
type Database struct {
	db          *sql.DB
	dialect     Dialect
	projectsDir string
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

	// Run schema migrations for existing databases
	if err := runMigrations(db, dialect); err != nil {
		return nil, fmt.Errorf("failed to run migrations (%s): %w", driverName, err)
	}

	// Insert default config (dialect-safe)
	upsertConfig := Rebind(dialect, `INSERT INTO system_config (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`)
	_, _ = db.Exec(upsertConfig, "projectsDir", "/app/projects")

	return &Database{
		db:          db,
		dialect:     dialect,
		projectsDir: "/app/projects",
	}, nil
}

// runMigrations applies incremental schema changes to existing databases.
// Each migration is idempotent — it checks whether the change is needed before applying.
func runMigrations(db *sql.DB, dialect Dialect) error {
	type migration struct {
		name string
		sql  string
	}

	migrations := []migration{
		{
			name: "add tool_call_id and tool_name columns",
			sql:  Rebind(dialect, `ALTER TABLE conversation_messages ADD COLUMN tool_call_id TEXT NOT NULL DEFAULT ''; ALTER TABLE conversation_messages ADD COLUMN tool_name TEXT NOT NULL DEFAULT ''`),
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
			// SQLite ALTER TABLE ADD COLUMN fails if column already exists.
			// That's fine — mark it as done anyway.
			if dialect == DialectSQLite {
				// Verify columns actually exist before marking done
				var colCount int
				db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('conversation_messages') WHERE name IN ('tool_call_id', 'tool_name')`).Scan(&colCount)
				if colCount < 2 {
					return fmt.Errorf("migration %q failed: %w", m.name, err)
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

// GetSystemConfig returns a system configuration value
func (d *Database) GetSystemConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := d.db.QueryRowContext(ctx,
		Rebind(d.dialect, "SELECT value FROM system_config WHERE key = ?"), key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetSystemConfig sets a system configuration value
func (d *Database) SetSystemConfig(ctx context.Context, key, value string) error {
	_, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO system_config (key, value)
		VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?`),
		key, value, value)
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

// SaveUserMessage persists a user message.
func (d *Database) SaveUserMessage(ctx context.Context, project, agentID, content string) (int64, error) {
	res, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `INSERT INTO conversation_messages (project, agent_id, role, content) VALUES (?, ?, 'user', ?)`),
		project, agentID, content)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveToolResult persists a tool result message.
// Stored as role='tool' with dedicated tool_call_id and tool_name columns.
func (d *Database) SaveToolResult(ctx context.Context, project, agentID, toolCallID, toolName, content string) (int64, error) {
	res, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `INSERT INTO conversation_messages (project, agent_id, role, content, tool_call_id, tool_name) VALUES (?, ?, 'tool', ?, ?, ?)`),
		project, agentID, content, toolCallID, toolName)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveAssistantMessage persists an assistant response.
func (d *Database) SaveAssistantMessage(ctx context.Context, project, agentID, text, thinking, toolCallsJSON string) (int64, error) {
	res, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `INSERT INTO conversation_messages (project, agent_id, role, text, thinking, tool_calls) VALUES (?, ?, 'assistant', ?, ?, ?)`),
		project, agentID, text, thinking, toolCallsJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveStreamingMessage creates or updates an in-progress assistant message.
// Uses '[streaming]' as a sentinel in tool_calls to identify the placeholder row.
func (d *Database) SaveStreamingMessage(ctx context.Context, project, agentID, text, thinking string) error {
	var id int64
	err := d.db.QueryRowContext(ctx,
		Rebind(d.dialect, `SELECT id FROM conversation_messages
			WHERE project = ? AND agent_id = ? AND role = 'assistant' AND tool_calls = '[streaming]'
			ORDER BY created_at DESC LIMIT 1`),
		project, agentID).Scan(&id)

	if err != nil {
		// No streaming row exists — insert one
		_, err = d.db.ExecContext(ctx,
			Rebind(d.dialect, `INSERT INTO conversation_messages (project, agent_id, role, text, thinking, tool_calls)
				VALUES (?, ?, 'assistant', ?, ?, '[streaming]')`),
			project, agentID, text, thinking)
		return err
	}
	// Update existing streaming row
	_, err = d.db.ExecContext(ctx,
		Rebind(d.dialect, `UPDATE conversation_messages SET text = ?, thinking = ? WHERE id = ?`),
		text, thinking, id)
	return err
}

// FinalizeStreamingMessage completes a streaming placeholder with final content.
// If no streaming row exists, returns an error (caller should fall back to SaveAssistantMessage).
func (d *Database) FinalizeStreamingMessage(ctx context.Context, project, agentID, text, thinking, toolCallsJSON string) error {
	result, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `UPDATE conversation_messages SET text = ?, thinking = ?, tool_calls = ?
			WHERE project = ? AND agent_id = ? AND role = 'assistant' AND tool_calls = '[streaming]'`),
		text, thinking, toolCallsJSON, project, agentID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no streaming message found")
	}
	return nil
}

// GetConversationHistory returns messages for a project+agent, ordered by creation time.
func (d *Database) GetConversationHistory(ctx context.Context, project, agentID string, limit int) ([]StoredMessage, error) {
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

// ClearConversationHistory deletes all messages for a project+agent.
func (d *Database) ClearConversationHistory(ctx context.Context, project, agentID string) error {
	_, err := d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM conversation_messages WHERE project = ? AND agent_id = ?`),
		project, agentID)
	if err != nil {
		return err
	}
	_, _ = d.db.ExecContext(ctx,
		Rebind(d.dialect, `DELETE FROM conversation_titles WHERE project = ? AND agent_id = ?`),
		project, agentID)
	return nil
}

// CompactSession removes old tool-result messages for a project+agent, keeping the most recent ones.
// Returns the number of messages compacted.
func (d *Database) CompactSession(ctx context.Context, project, agentID string) (int, error) {
	// Get total message count
	var total int
	err := d.db.QueryRowContext(ctx,
		Rebind(d.dialect, `SELECT COUNT(*) FROM conversation_messages WHERE project = ? AND agent_id = ?`),
		project, agentID).Scan(&total)
	if err != nil {
		return 0, err
	}

	// Keep the most recent 20 messages, delete older ones
	keep := 20
	if total <= keep {
		return 0, nil
	}

	result, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		DELETE FROM conversation_messages
		WHERE project = ? AND agent_id = ?
		  AND id NOT IN (
		    SELECT id FROM conversation_messages
		    WHERE project = ? AND agent_id = ?
		    ORDER BY created_at DESC
		    LIMIT ?
		  )`),
		project, agentID, project, agentID, keep)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return int(affected), nil
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

// GetConversationSummaries returns the latest conversation for each project+agent pair.
func (d *Database) GetConversationSummaries(ctx context.Context) ([]ConversationSummary, error) {
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
			COALESCE(ct.title, '') AS title
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
		if err := rows.Scan(&s.Project, &s.AgentID, &s.LastMessage, &s.LastAt, &s.MessageCount, &s.Title); err != nil {
			return nil, err
		}
		// Truncate last message for display
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

// ── Context Transcripts (pre-compaction snapshots) ──────────────────

// Transcript is a saved snapshot of conversation messages before compaction.
type Transcript struct {
	ID            int64  `json:"id"`
	SessionID     string `json:"sessionId"`
	AgentID       string `json:"agentId"`
	MessagesJSON  string `json:"messagesJson"`
	TokenCount    int    `json:"tokenCount"`
	TriggerReason string `json:"triggerReason"`
	CreatedAt     string `json:"createdAt"`
}

// SaveTranscript persists a pre-compaction message snapshot.
func (d *Database) SaveTranscript(ctx context.Context, sessionID, agentID, messagesJSON, triggerReason string, tokenCount int) (int64, error) {
	res, err := d.db.ExecContext(ctx, Rebind(d.dialect, `
		INSERT INTO context_transcripts (session_id, agent_id, messages_json, token_count, trigger_reason)
		VALUES (?, ?, ?, ?, ?)`),
		sessionID, agentID, messagesJSON, tokenCount, triggerReason)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetLatestTranscript returns the most recent transcript for a session.
func (d *Database) GetLatestTranscript(ctx context.Context, sessionID string) (*Transcript, error) {
	var t Transcript
	err := d.db.QueryRowContext(ctx, Rebind(d.dialect, `
		SELECT id, session_id, agent_id, messages_json, token_count, trigger_reason, created_at
		FROM context_transcripts
		WHERE session_id = ?
		ORDER BY created_at DESC LIMIT 1`),
		sessionID).Scan(&t.ID, &t.SessionID, &t.AgentID, &t.MessagesJSON, &t.TokenCount, &t.TriggerReason, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
