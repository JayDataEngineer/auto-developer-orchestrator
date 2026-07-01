-- history/schema.sql — sqlite schema for the history sidecar.
--
-- Three tables: tasks (dispatch lifecycle), messages (assistant turns),
-- tool_calls (in-loop tool dispatches). All written by the recorder;
-- read by cmd/pux-history CLI. None of this is exposed via MCP.
--
-- Idempotent — all statements are CREATE ... IF NOT EXISTS so re-opening
-- an existing database is a no-op.

CREATE TABLE IF NOT EXISTS tasks (
    id          TEXT PRIMARY KEY,
    org         TEXT NOT NULL,
    task        TEXT NOT NULL,
    status      TEXT NOT NULL,           -- pending / running / complete / failed
    result      TEXT,
    error       TEXT,
    started_at  INTEGER NOT NULL,        -- unix milli
    finished_at INTEGER
);

CREATE TABLE IF NOT EXISTS messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     TEXT NOT NULL,
    round       INTEGER NOT NULL,
    role        TEXT NOT NULL,           -- agent role: "cto" or delegated role name
    content     TEXT,
    ts          INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE TABLE IF NOT EXISTS tool_calls (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      TEXT NOT NULL,
    round        INTEGER NOT NULL,
    role         TEXT NOT NULL DEFAULT 'cto',  -- agent role: "cto" or delegated role name
    tool         TEXT NOT NULL,
    args         TEXT,                   -- raw JSON arg string, scrubbed
    result       TEXT,                   -- scrubbed
    error        TEXT,                   -- scrubbed
    duration_ms  INTEGER,
    ts           INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task_id, round);
CREATE INDEX IF NOT EXISTS idx_tool_calls_task ON tool_calls(task_id, round);
CREATE INDEX IF NOT EXISTS idx_tasks_org_started ON tasks(org, started_at DESC);
