package storage

import (
	"fmt"
	"strings"
)

// Dialect represents the SQL dialect of the connected database.
type Dialect int

const (
	DialectSQLite Dialect = iota
	DialectPostgres
)

// DetectDriver returns the sql.Driver name and dialect for a given data source.
// "postgres://" or "postgresql://" → pgx driver, Postgres dialect.
// Everything else → sqlite3 driver, SQLite dialect.
func DetectDriver(dataSource string) (driverName string, dialect Dialect) {
	if strings.HasPrefix(dataSource, "postgres://") || strings.HasPrefix(dataSource, "postgresql://") {
		return "pgx", DialectPostgres
	}
	return "sqlite3", DialectSQLite
}

// Rebind converts ? placeholders to $1, $2, ... for Postgres.
// SQLite queries pass through unchanged.
func Rebind(dialect Dialect, query string) string {
	if dialect == DialectSQLite {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			b.WriteString(fmt.Sprintf("$%d", n))
			n++
		} else {
			b.WriteByte(query[i])
		}
	}
	return b.String()
}

// PostgreSQL DDL — standard SQL with SERIAL, TIMESTAMPTZ, ON CONFLICT.
const postgresDDL = `
CREATE TABLE IF NOT EXISTS custom_projects (
	id SERIAL PRIMARY KEY,
	name TEXT UNIQUE NOT NULL,
	path TEXT NOT NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS automation_modes (
	id SERIAL PRIMARY KEY,
	project_name TEXT UNIQUE NOT NULL,
	is_auto_mode BOOLEAN DEFAULT FALSE,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS task_indices (
	id SERIAL PRIMARY KEY,
	project_name TEXT UNIQUE NOT NULL,
	current_index INTEGER DEFAULT -1,
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS system_config (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS conversation_messages (
	id SERIAL PRIMARY KEY,
	project TEXT NOT NULL,
	agent_id TEXT NOT NULL DEFAULT 'default',
	role TEXT NOT NULL,
	content TEXT NOT NULL DEFAULT '',
	text TEXT NOT NULL DEFAULT '',
	thinking TEXT NOT NULL DEFAULT '',
	tool_calls TEXT NOT NULL DEFAULT '[]',
	tool_call_id TEXT NOT NULL DEFAULT '',
	tool_name TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_conv_msgs_project_agent
	ON conversation_messages(project, agent_id, created_at);

CREATE TABLE IF NOT EXISTS conversation_titles (
	project TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (project, agent_id)
);

CREATE TABLE IF NOT EXISTS artifacts (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL,
	type TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_artifacts_agent
	ON artifacts(agent_id);

CREATE TABLE IF NOT EXISTS agent_events (
	id SERIAL PRIMARY KEY,
	session_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	event_data TEXT NOT NULL DEFAULT '{}',
	sub_agent_id TEXT NOT NULL DEFAULT '',
	sequence_num INTEGER NOT NULL,
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_events_session_seq
	ON agent_events(session_id, sequence_num);

CREATE TABLE IF NOT EXISTS context_transcripts (
	id SERIAL PRIMARY KEY,
	session_id TEXT NOT NULL,
	agent_id TEXT NOT NULL DEFAULT '',
	messages_json TEXT NOT NULL,
	token_count INTEGER DEFAULT 0,
	trigger_reason TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_transcripts_session
	ON context_transcripts(session_id, created_at DESC);
`

// SQLite DDL — uses AUTOINCREMENT and WAL journal mode params in connection string.
const sqliteDDL = `
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
	tool_call_id TEXT NOT NULL DEFAULT '',
	tool_name TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_conv_msgs_project_agent
	ON conversation_messages(project, agent_id, created_at);

CREATE TABLE IF NOT EXISTS conversation_titles (
	project TEXT NOT NULL,
	agent_id TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS agent_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	event_type TEXT NOT NULL,
	event_data TEXT NOT NULL DEFAULT '{}',
	sub_agent_id TEXT NOT NULL DEFAULT '',
	sequence_num INTEGER NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_events_session_seq
	ON agent_events(session_id, sequence_num);

CREATE TABLE IF NOT EXISTS context_transcripts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	agent_id TEXT NOT NULL DEFAULT '',
	messages_json TEXT NOT NULL,
	token_count INTEGER DEFAULT 0,
	trigger_reason TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_transcripts_session
	ON context_transcripts(session_id, created_at DESC);
`
