package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// StoredEvent is a persisted agent event from the append-only log.
type StoredEvent struct {
	ID          int64           `json:"id"`
	SessionID   string          `json:"sessionId"`
	EventType   string          `json:"eventType"`
	EventData   json.RawMessage `json:"eventData"`
	SubAgentID  string          `json:"subAgentId"`
	SequenceNum int64           `json:"sequenceNum"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// EventStore persists agent events for session recovery and replay.
type EventStore struct {
	db      *sql.DB
	dialect Dialect
}

// NewEventStore creates a new event store backed by the given database.
func NewEventStore(db *sql.DB, dialect Dialect) *EventStore {
	return &EventStore{db: db, dialect: dialect}
}

// Append persists an event to the append-only log.
func (s *EventStore) Append(ctx context.Context, sessionID, eventType, subAgentID string, eventData []byte) (int64, error) {
	result, err := s.db.ExecContext(ctx, Rebind(s.dialect, `
		INSERT INTO agent_events (session_id, event_type, event_data, sub_agent_id, sequence_num)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(sequence_num), 0) + 1 FROM agent_events WHERE session_id = ?))
	`), sessionID, eventType, string(eventData), subAgentID, sessionID)
	if err != nil {
		return 0, fmt.Errorf("append event: %w", err)
	}
	id, _ := result.LastInsertId()
	return id, nil
}

// Replay reads events for a session in sequence order.
func (s *EventStore) Replay(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]StoredEvent, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, Rebind(s.dialect, `
		SELECT id, session_id, event_type, event_data, sub_agent_id, sequence_num, created_at
		FROM agent_events
		WHERE session_id = ? AND sequence_num > ?
		ORDER BY sequence_num
		LIMIT ?
	`), sessionID, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("replay events: %w", err)
	}
	defer rows.Close()

	var events []StoredEvent
	for rows.Next() {
		var e StoredEvent
		if err := rows.Scan(&e.ID, &e.SessionID, &e.EventType, &e.EventData, &e.SubAgentID, &e.SequenceNum, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// LatestSequence returns the highest sequence number for a session (0 if none).
func (s *EventStore) LatestSequence(ctx context.Context, sessionID string) (int64, error) {
	var seq int64
	row := s.db.QueryRowContext(ctx, Rebind(s.dialect, `
		SELECT COALESCE(MAX(sequence_num), 0) FROM agent_events WHERE session_id = ?
	`), sessionID)
	if err := row.Scan(&seq); err != nil {
		return 0, fmt.Errorf("latest sequence: %w", err)
	}
	return seq, nil
}
