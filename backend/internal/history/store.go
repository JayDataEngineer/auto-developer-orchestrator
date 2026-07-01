// Package history is a fully-deletable sidecar that durably records
// dispatch-surface activity: task lifecycle, agent-loop assistant messages,
// and in-loop tool dispatches. Storage is a single sqlite database file.
//
// The seam lives in internal/core/observer.go (TaskObserver, ChatObserver,
// ToolObserver — all nil-safe). This package implements all three. The
// recorder is constructed at server boot when PUX_HISTORY_DIR is set, and
// wired into the task store + every agent.Loop the dispatch surface spawns.
//
// Deletion proof: rm this package + cmd/pux-history/ + drop the 8 wiring
// lines in main.go + the 2 fields in dispatch.go + the TaskStore observer
// plumbing. The server still builds with zero history overhead.
//
// This package is host-side only — no MCP tool exposes the read path.
// Operators query the database via the separate cmd/pux-history binary
// (list / show / search subcommands).
package history

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
)

//go:embed schema.sql
var schemaSQL string

// dbFileName is the sqlite filename inside the configured dir.
const dbFileName = "history.sqlite"

// ErrClosed is returned (or surfaced via the typed check) when a Store
// method is called after Close.
var ErrClosed = errors.New("history: store closed")

// Store wraps a *sql.DB bound to the history schema. Safe for concurrent
// use (sqlite enforces its own serialization via a single writer; modernc
// uses a sync.Mutex internally for the connection pool).
//
// All write paths are fire-and-forget from the caller's view: the recorder
// methods return no error. Write failures are logged to the supplied logger
// hook (if non-nil) — recording is best-effort and must never break the
// dispatch surface.
type Store struct {
	mu      sync.Mutex
	db      *sql.DB
	closed  bool
	logErr  func(string, ...any)
}

// Open opens (or creates) the sqlite database at <dir>/history.sqlite,
// applies the schema, and returns a ready-to-use Store. The dir is created
// if it doesn't exist.
func Open(dir string, logErr func(string, ...any)) (*Store, error) {
	if dir == "" {
		return nil, errors.New("history: dir is required")
	}
	dsn := filepath.Join(dir, dbFileName)
	// SQLite busy_timeout: wait up to 5s on contention before erroring.
	// journal_mode=WAL: better concurrent read/write throughput.
	dsnWithParams := dsn + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsnWithParams)
	if err != nil {
		return nil, fmt.Errorf("history: open sqlite: %w", err)
	}
	// Single writer connection — modernc serializes anyway, and a small
	// pool keeps read queries responsive while a write is in flight.
	db.SetMaxOpenConns(4)

	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("history: apply schema: %w", err)
	}

	return &Store{db: db, logErr: logErr}, nil
}

// Close releases the database handle. Idempotent.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

// ── Task lifecycle writes ─────────────────────────────────────────────

// RecordTaskPending inserts the initial task row in pending state.
func (s *Store) RecordTaskPending(ctx context.Context, taskID, org, task string, startedAt time.Time) {
	if err := s.exec(ctx,
		"INSERT INTO tasks (id, org, task, status, started_at) VALUES (?, ?, ?, ?, ?)",
		taskID, org, task, "pending", startedAt.UnixMilli(),
	); err != nil {
		s.fail("task pending", err)
	}
}

// RecordTaskRunning flips a task's status to running. Idempotent against
// repeated calls (the dispatch goroutine fires this exactly once).
func (s *Store) RecordTaskRunning(ctx context.Context, taskID string) {
	if err := s.exec(ctx, "UPDATE tasks SET status = ? WHERE id = ?", "running", taskID); err != nil {
		s.fail("task running", err)
	}
}

// RecordTaskComplete flips a task to complete with the final result text.
func (s *Store) RecordTaskComplete(ctx context.Context, taskID, result string, finishedAt time.Time) {
	if err := s.exec(ctx,
		"UPDATE tasks SET status = ?, result = ?, finished_at = ? WHERE id = ?",
		"complete", sensitive.ScrubText(result), finishedAt.UnixMilli(), taskID,
	); err != nil {
		s.fail("task complete", err)
	}
}

// RecordTaskFailed flips a task to failed with the error message.
func (s *Store) RecordTaskFailed(ctx context.Context, taskID, errorMsg string, finishedAt time.Time) {
	if err := s.exec(ctx,
		"UPDATE tasks SET status = ?, error = ?, finished_at = ? WHERE id = ?",
		"failed", sensitive.ScrubText(errorMsg), finishedAt.UnixMilli(), taskID,
	); err != nil {
		s.fail("task failed", err)
	}
}

// ── Transcript writes ─────────────────────────────────────────────────

// RecordAssistantMessage appends one assistant turn to the messages table.
// role identifies which agent produced the message ("cto" or a delegated
// role name). content is scrubbed before insert.
func (s *Store) RecordAssistantMessage(ctx context.Context, taskID, role string, round int, content string) {
	if role == "" {
		role = "cto"
	}
	if err := s.exec(ctx,
		"INSERT INTO messages (task_id, round, role, content, ts) VALUES (?, ?, ?, ?, ?)",
		taskID, round, role, sensitive.ScrubText(content), time.Now().UnixMilli(),
	); err != nil {
		s.fail("assistant message", err)
	}
}

// ── Tool-call writes ──────────────────────────────────────────────────

// RecordToolCall appends one tool dispatch to the tool_calls table.
// role identifies which agent dispatched the tool ("cto" or a delegated
// role name). argsRaw, result, and errorMsg are scrubbed before insert.
// duration is the wall-clock time the tool took.
func (s *Store) RecordToolCall(ctx context.Context, taskID, role string, round int, tool, argsRaw, result, errorMsg string, duration time.Duration) {
	if role == "" {
		role = "cto"
	}
	var errStr any
	if errorMsg != "" {
		errStr = sensitive.ScrubText(errorMsg)
	}
	if err := s.exec(ctx,
		"INSERT INTO tool_calls (task_id, round, role, tool, args, result, error, duration_ms, ts) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		taskID, round, role, tool,
		sensitive.ScrubText(argsRaw),
		sensitive.ScrubText(result),
		errStr,
		duration.Milliseconds(),
		time.Now().UnixMilli(),
	); err != nil {
		s.fail("tool call", err)
	}
}

// ── internals ─────────────────────────────────────────────────────────

// exec acquires the mutex, checks for closed, and runs the statement.
// The mutex is held across Exec so a concurrent Close can't race against
// an in-flight write.
func (s *Store) exec(ctx context.Context, query string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// fail routes a write error through the configured logger hook (if any).
// Recording is best-effort — failures must not propagate up into the
// dispatch surface and break tool dispatch.
func (s *Store) fail(what string, err error) {
	if s.logErr == nil {
		return
	}
	s.logErr("history: write failed: %s: %v", what, err)
}
