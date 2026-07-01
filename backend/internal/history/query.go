// query.go is the read API for the history database. Used by the
// cmd/pux-history CLI binary. Lives here (next to the writer) so the
// schema is owned by one package.

package history

import (
	"context"
	"database/sql"
	"regexp"
	"time"
)

// TaskRow is a flat view of one dispatch task. Returned by ListTasks +
// GetTask. Times are zero-valued when the corresponding timestamp column
// is NULL (e.g. FinishedAt is zero for a still-running task).
type TaskRow struct {
	ID         string
	Org        string
	Task       string
	Status     string
	Result     string
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

// MessageRow is one assistant turn.
type MessageRow struct {
	ID      int64
	TaskID  string
	Round   int
	Role    string
	Content string
	Ts      time.Time
}

// ToolCallRow is one in-loop tool dispatch.
type ToolCallRow struct {
	ID         int64
	TaskID     string
	Round      int
	Role       string // agent role: "cto" or delegated role name
	Tool       string
	Args       string
	Result     string
	Error      string
	DurationMs int64
	Ts         time.Time
}

// Query is the read surface. Construct via OpenQuery (separate from the
// Recorder constructor so the CLI binary doesn't accidentally pull in the
// write paths — and vice versa).
type Query struct {
	db *sql.DB
}

// OpenQuery opens the database read-only at the same path the server
// writes to. Used by the CLI. Caller must Close.
func OpenQuery(dir string) (*Query, error) {
	s, err := Open(dir, nil)
	if err != nil {
		return nil, err
	}
	return &Query{db: s.db}, nil
}

// Close releases the underlying handle.
func (q *Query) Close() error {
	return q.db.Close()
}

// ListTasks returns the most-recently-started tasks, optionally filtered
// by org. limit caps the result count; 0 → default 50.
func (q *Query) ListTasks(ctx context.Context, org string, limit int) ([]TaskRow, error) {
	if limit <= 0 {
		limit = 50
	}
	var (
		rows *sql.Rows
		err  error
	)
	if org == "" {
		rows, err = q.db.QueryContext(ctx,
			"SELECT id, org, task, status, COALESCE(result,''), COALESCE(error,''), started_at, COALESCE(finished_at,0) "+
				"FROM tasks ORDER BY started_at DESC LIMIT ?", limit)
	} else {
		rows, err = q.db.QueryContext(ctx,
			"SELECT id, org, task, status, COALESCE(result,''), COALESCE(error,''), started_at, COALESCE(finished_at,0) "+
				"FROM tasks WHERE org = ? ORDER BY started_at DESC LIMIT ?", org, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

// GetTask returns one task by ID. Returns sql.ErrNoRows if not found.
func (q *Query) GetTask(ctx context.Context, taskID string) (TaskRow, error) {
	var (
		t          TaskRow
		result     sql.NullString
		errStr     sql.NullString
		startedMs  int64
		finishedMs sql.NullInt64
	)
	err := q.db.QueryRowContext(ctx,
		"SELECT id, org, task, status, result, error, started_at, finished_at "+
			"FROM tasks WHERE id = ?", taskID).
		Scan(&t.ID, &t.Org, &t.Task, &t.Status, &result, &errStr, &startedMs, &finishedMs)
	if err != nil {
		return TaskRow{}, err
	}
	t.Result = result.String
	t.Error = errStr.String
	t.StartedAt = time.UnixMilli(startedMs)
	if finishedMs.Valid {
		t.FinishedAt = time.UnixMilli(finishedMs.Int64)
	}
	return t, nil
}

// ListMessages returns the assistant turns for one task, in chronological
// order (round ascending, then id ascending).
func (q *Query) ListMessages(ctx context.Context, taskID string) ([]MessageRow, error) {
	rows, err := q.db.QueryContext(ctx,
		"SELECT id, task_id, round, role, COALESCE(content,''), ts "+
			"FROM messages WHERE task_id = ? ORDER BY round ASC, id ASC", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MessageRow, 0)
	for rows.Next() {
		var m MessageRow
		var ts int64
		if err := rows.Scan(&m.ID, &m.TaskID, &m.Round, &m.Role, &m.Content, &ts); err != nil {
			return nil, err
		}
		m.Ts = time.UnixMilli(ts)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListToolCalls returns the in-loop tool dispatches for one task, in
// chronological order.
func (q *Query) ListToolCalls(ctx context.Context, taskID string) ([]ToolCallRow, error) {
	rows, err := q.db.QueryContext(ctx,
		"SELECT id, task_id, round, role, tool, COALESCE(args,''), COALESCE(result,''), COALESCE(error,''), COALESCE(duration_ms,0), ts "+
			"FROM tool_calls WHERE task_id = ? ORDER BY round ASC, id ASC", taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ToolCallRow, 0)
	for rows.Next() {
		var t ToolCallRow
		var ts int64
		if err := rows.Scan(&t.ID, &t.TaskID, &t.Round, &t.Role, &t.Tool, &t.Args, &t.Result, &t.Error, &t.DurationMs, &ts); err != nil {
			return nil, err
		}
		t.Ts = time.UnixMilli(ts)
		out = append(out, t)
	}
	return out, rows.Err()
}

// SearchHits is the result of a regex search across the database. Each
// hit names its source (task / message / tool_call) plus the row ID +
// a short snippet around the first match.
type SearchHit struct {
	Kind     string // "task" | "message" | "tool_call"
	TaskID   string
	RowID    int64
	Round    int  // messages + tool_calls only; 0 otherwise
	Snippet  string
}

// Search applies a compiled regex across the task descriptions + results,
// message contents, and tool-call args/results/error bodies. Returns every
// hit, capped at limit (0 → 200). Errors during a single source are
// non-fatal — the search returns the hits that did parse.
func (q *Query) Search(ctx context.Context, re *regexp.Regexp, org string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 200
	}
	hits := make([]SearchHit, 0, limit)
	remaining := func() int { return limit - len(hits) }

	// tasks.task + tasks.result
	if remaining() > 0 {
		args := []any{}
		qry := "SELECT id, org, task, COALESCE(result,'') FROM tasks"
		if org != "" {
			qry += " WHERE org = ?"
			args = append(args, org)
		}
		rows, err := q.db.QueryContext(ctx, qry, args...)
		if err == nil {
			hits = append(hits, scanTasksForSearch(rows, re)...)
		}
	}

	// messages.content
	if remaining() > 0 {
		args := []any{}
		qry := "SELECT id, task_id, round, COALESCE(content,'') FROM messages"
		if org != "" {
			qry = "SELECT m.id, m.task_id, m.round, COALESCE(m.content,'') FROM messages m " +
				"JOIN tasks t ON t.id = m.task_id WHERE t.org = ?"
			args = append(args, org)
		}
		rows, err := q.db.QueryContext(ctx, qry, args...)
		if err == nil {
			hits = append(hits, scanMessageRowsForSearch(rows, re)...)
		}
	}

	// tool_calls.args/result/error
	if remaining() > 0 {
		args := []any{}
		qry := "SELECT id, task_id, round, COALESCE(args,''), COALESCE(result,''), COALESCE(error,'') FROM tool_calls"
		if org != "" {
			qry = "SELECT tc.id, tc.task_id, tc.round, COALESCE(tc.args,''), COALESCE(tc.result,''), COALESCE(tc.error,'') FROM tool_calls tc " +
				"JOIN tasks t ON t.id = tc.task_id WHERE t.org = ?"
			args = append(args, org)
		}
		rows, err := q.db.QueryContext(ctx, qry, args...)
		if err == nil {
			hits = append(hits, scanToolCallsForSearch(rows, re)...)
		}
	}

	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// ── scan helpers ──────────────────────────────────────────────────────

func scanTasks(rows *sql.Rows) ([]TaskRow, error) {
	out := make([]TaskRow, 0)
	for rows.Next() {
		var t TaskRow
		var startedMs int64
		var finishedMs sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Org, &t.Task, &t.Status, &t.Result, &t.Error, &startedMs, &finishedMs); err != nil {
			return nil, err
		}
		t.StartedAt = time.UnixMilli(startedMs)
		if finishedMs.Valid {
			t.FinishedAt = time.UnixMilli(finishedMs.Int64)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanTasksForSearch(rows *sql.Rows, re *regexp.Regexp) []SearchHit {
	defer rows.Close()
	out := make([]SearchHit, 0)
	for rows.Next() {
		var id, org, task, result string
		if err := rows.Scan(&id, &org, &task, &result); err != nil {
			return out
		}
		for _, hay := range []string{task, result} {
			if loc := re.FindStringIndex(hay); loc != nil {
				out = append(out, SearchHit{
					Kind:    "task",
					TaskID:  id,
					Snippet: snippet(hay, loc[0]),
				})
				break
			}
		}
	}
	return out
}

func scanMessageRowsForSearch(rows *sql.Rows, re *regexp.Regexp) []SearchHit {
	defer rows.Close()
	out := make([]SearchHit, 0)
	for rows.Next() {
		var id int64
		var taskID string
		var round int
		var body string
		if err := rows.Scan(&id, &taskID, &round, &body); err != nil {
			return out
		}
		if loc := re.FindStringIndex(body); loc != nil {
			out = append(out, SearchHit{
				Kind:    "message",
				TaskID:  taskID,
				RowID:   id,
				Round:   round,
				Snippet: snippet(body, loc[0]),
			})
		}
	}
	return out
}

func scanToolCallsForSearch(rows *sql.Rows, re *regexp.Regexp) []SearchHit {
	defer rows.Close()
	out := make([]SearchHit, 0)
	for rows.Next() {
		var id int64
		var taskID string
		var round int
		var args, result, errStr string
		if err := rows.Scan(&id, &taskID, &round, &args, &result, &errStr); err != nil {
			return out
		}
		for _, hay := range []string{args, result, errStr} {
			if loc := re.FindStringIndex(hay); loc != nil {
				out = append(out, SearchHit{
					Kind:    "tool_call",
					TaskID:  taskID,
					RowID:   id,
					Round:   round,
					Snippet: snippet(hay, loc[0]),
				})
				break
			}
		}
	}
	return out
}

// snippet returns ~80 chars around the match offset.
func snippet(s string, at int) string {
	const radius = 60
	start := at - radius
	if start < 0 {
		start = 0
	}
	end := at + radius
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}
