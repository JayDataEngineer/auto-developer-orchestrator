// Package audit writes an append-only log of every tool call processed by
// the MCP server. Opt-in via PUX_AUDIT_LOG=/path/to/audit.jsonl.
//
// Each line is one JSON object with: timestamp, session_id, tool, args
// (secret-scrubbed + size-capped), result (same), error, duration_ms.
//
// The audit log answers "what did the model just do to my code?" — it is
// not a conversation log (the client owns that) and not a server debug
// log (those go through zap).
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
)

// MaxFieldValue caps any single args/result/error field. Bash output can
// easily be tens of MB; the audit log is for forensic reconstruction, not
// full-fidelity replay.
const MaxFieldValue = 4096

// Entry is one audit log record. JSON-tagged for direct JSONL encoding.
type Entry struct {
	Timestamp  time.Time `json:"ts"`
	SessionID  string    `json:"session_id,omitempty"`
	Tool       string    `json:"tool"`
	Args       any       `json:"args"`
	Result     any       `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
}

// Logger appends audit entries to a file. Safe for concurrent callers.
// A nil *Logger is valid — all methods are no-ops. This lets callers skip
// the opt-in check: pass nil when audit is disabled.
type Logger struct {
	mu  sync.Mutex
	f   *os.File
	enc *json.Encoder
}

// Open creates a Logger that appends to path. Returns nil (no-op logger)
// when path is empty — this is the "disabled" mode.
func Open(path string) (*Logger, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &Logger{f: f, enc: json.NewEncoder(f)}, nil
}

// Close releases the underlying file handle. No-op on a disabled logger.
func (l *Logger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	return l.f.Close()
}

// Log appends one entry. Args, Result, and Error are scrubbed (secrets
// redacted via sensitive.ScrubText) and size-capped before serialization.
// Safe for concurrent callers; calls are serialized under a mutex.
func (l *Logger) Log(e Entry) {
	if l == nil || l.f == nil {
		return
	}
	e.Args = scrubAndCap(e.Args)
	e.Result = scrubAndCap(e.Result)
	if e.Error != "" {
		e.Error = scrubAndCap(e.Error).(string)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(e)
}

// scrubAndCap stringifies, scrubs, and truncates a value. The cap is
// applied AFTER scrubbing so a truncated secret still can't leak — though
// ScrubText already redacts the whole match.
func scrubAndCap(v any) any {
	var s string
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		s = x
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return "<unloggable>"
		}
		s = string(b)
	}
	s = sensitive.ScrubText(s)
	if len(s) > MaxFieldValue {
		s = s[:MaxFieldValue] + "...[truncated]"
	}
	return s
}
