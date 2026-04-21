package storage

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Database represents the application database
type Database struct {
	db          *sql.DB
	projectsDir string
}

// NewDatabase creates a new database connection
func NewDatabase(dataSource string) (*Database, error) {
	db, err := sql.Open("sqlite3", dataSource+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Create tables
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS custom_projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			path TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS automation_modes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_name TEXT UNIQUE NOT NULL,
			is_auto_mode BOOLEAN DEFAULT FALSE,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS task_indices (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_name TEXT UNIQUE NOT NULL,
			current_index INTEGER DEFAULT -1,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS system_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);

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

		CREATE INDEX IF NOT EXISTS idx_conv_msgs_project_agent
			ON conversation_messages(project, agent_id, created_at);

		CREATE TABLE IF NOT EXISTS conversation_titles (
			project TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (project, agent_id)
		);

		CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			type TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_artifacts_agent
			ON artifacts(agent_id);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Insert default config
	_, _ = db.Exec(`INSERT OR IGNORE INTO system_config (key, value) VALUES ('projectsDir', '/app/projects')`)

	return &Database{
		db:          db,
		projectsDir: "/app/projects",
	}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
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
		"INSERT INTO custom_projects (name, path) VALUES (?, ?)",
		name, path)
	return err
}

// EnsureCustomProject adds a custom project if it doesn't already exist
func (d *Database) EnsureCustomProject(ctx context.Context, name, path string) error {
	_, err := d.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO custom_projects (name, path) VALUES (?, ?)",
		name, path)
	return err
}

// GetProjectDir returns the directory for a project
func (d *Database) GetProjectDir(ctx context.Context, projectName string) (string, error) {
	// Check custom projects first
	var path string
	err := d.db.QueryRowContext(ctx,
		"SELECT path FROM custom_projects WHERE name = ?", projectName).Scan(&path)
	if err == nil {
		return path, nil
	}

	// Return default path
	return d.projectsDir + "/" + projectName, nil
}

// GetAutomationMode returns the automation mode for a project
func (d *Database) GetAutomationMode(ctx context.Context, projectName string) (bool, error) {
	var isAutoMode bool
	err := d.db.QueryRowContext(ctx,
		"SELECT is_auto_mode FROM automation_modes WHERE project_name = ?", projectName).Scan(&isAutoMode)
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
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO automation_modes (project_name, is_auto_mode) 
		VALUES (?, ?)
		ON CONFLICT(project_name) DO UPDATE SET is_auto_mode = ?, updated_at = CURRENT_TIMESTAMP`,
		projectName, isAutoMode, isAutoMode)
	return err
}

// GetCurrentTaskIndex returns the current task index for a project
func (d *Database) GetCurrentTaskIndex(ctx context.Context, projectName string) (int, error) {
	var index int
	err := d.db.QueryRowContext(ctx,
		"SELECT current_index FROM task_indices WHERE project_name = ?", projectName).Scan(&index)
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
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO task_indices (project_name, current_index) 
		VALUES (?, ?)
		ON CONFLICT(project_name) DO UPDATE SET current_index = ?, updated_at = CURRENT_TIMESTAMP`,
		projectName, index, index)
	return err
}

// GetSystemConfig returns a system configuration value
func (d *Database) GetSystemConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := d.db.QueryRowContext(ctx,
		"SELECT value FROM system_config WHERE key = ?", key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetSystemConfig sets a system configuration value
func (d *Database) SetSystemConfig(ctx context.Context, key, value string) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO system_config (key, value)
		VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = ?`,
		key, value, value)
	return err
}

// StoredMessage represents a persisted conversation message.
type StoredMessage struct {
	ID        int64  `json:"id"`
	Project   string `json:"project"`
	AgentID   string `json:"agentId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Text      string `json:"text"`
	Thinking  string `json:"thinking"`
	ToolCalls string `json:"toolCalls"`
	CreatedAt string `json:"createdAt"`
}

// SaveUserMessage persists a user message.
func (d *Database) SaveUserMessage(ctx context.Context, project, agentID, content string) (int64, error) {
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO conversation_messages (project, agent_id, role, content) VALUES (?, ?, 'user', ?)`,
		project, agentID, content)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SaveAssistantMessage persists an assistant response.
func (d *Database) SaveAssistantMessage(ctx context.Context, project, agentID, text, thinking, toolCallsJSON string) (int64, error) {
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO conversation_messages (project, agent_id, role, text, thinking, tool_calls) VALUES (?, ?, 'assistant', ?, ?, ?)`,
		project, agentID, text, thinking, toolCallsJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetConversationHistory returns messages for a project+agent, ordered by creation time.
func (d *Database) GetConversationHistory(ctx context.Context, project, agentID string, limit int) ([]StoredMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, project, agent_id, role, content, text, thinking, tool_calls, created_at
		 FROM conversation_messages
		 WHERE project = ? AND agent_id = ?
		 ORDER BY created_at ASC
		 LIMIT ?`,
		project, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []StoredMessage
	for rows.Next() {
		var m StoredMessage
		if err := rows.Scan(&m.ID, &m.Project, &m.AgentID, &m.Role, &m.Content, &m.Text, &m.Thinking, &m.ToolCalls, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// ClearConversationHistory deletes all messages for a project+agent.
func (d *Database) ClearConversationHistory(ctx context.Context, project, agentID string) error {
	_, err := d.db.ExecContext(ctx,
		`DELETE FROM conversation_messages WHERE project = ? AND agent_id = ?`,
		project, agentID)
	if err != nil {
		return err
	}
	// Also remove any custom title
	_, _ = d.db.ExecContext(ctx,
		`DELETE FROM conversation_titles WHERE project = ? AND agent_id = ?`,
		project, agentID)
	return nil
}

// SetConversationTitle sets a custom title for a conversation.
func (d *Database) SetConversationTitle(ctx context.Context, project, agentID, title string) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO conversation_titles (project, agent_id, title)
		VALUES (?, ?, ?)
		ON CONFLICT(project, agent_id) DO UPDATE SET title = excluded.title`,
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
	_, err := d.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO artifacts (id, agent_id, type, title, content, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
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
