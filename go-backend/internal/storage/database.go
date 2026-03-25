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
	db, err := sql.Open("sqlite3", dataSource)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

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

		CREATE TABLE IF NOT EXISTS jules_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_name TEXT NOT NULL,
			task_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			status TEXT DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS system_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
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

// GetProjectsDir returns the projects directory
func (d *Database) GetProjectsDir() string {
	// TODO: Load from database
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

// StoreJulesSession stores a Jules session
func (d *Database) StoreJulesSession(ctx context.Context, projectName, taskID, sessionID string) error {
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO jules_sessions (project_name, task_id, session_id) VALUES (?, ?, ?)",
		projectName, taskID, sessionID)
	return err
}

// GetJulesSession retrieves a Jules session
func (d *Database) GetJulesSession(ctx context.Context, sessionID string) (*JulesSession, error) {
	var session JulesSession
	err := d.db.QueryRowContext(ctx, `
		SELECT project_name, task_id, session_id, status 
		FROM jules_sessions WHERE session_id = ?`, sessionID).Scan(
		&session.ProjectName, &session.TaskID, &session.SessionID, &session.Status)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// JulesSession represents a Jules session
type JulesSession struct {
	ProjectName string
	TaskID      string
	SessionID   string
	Status      string
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
