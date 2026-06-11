package scheduler

import (
	"context"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"go.uber.org/zap"
)

// RunLogEntry is a single entry in a job's run log.
type RunLogEntry struct {
	Ts             int64  `json:"ts"`
	JobID          string `json:"jobId"`
	Action         string `json:"action"`
	Status         string `json:"status,omitempty"`
	Error          string `json:"error,omitempty"`
	Summary        string `json:"summary,omitempty"`
	Delivered      bool   `json:"delivered,omitempty"`
	DeliveryStatus string `json:"deliveryStatus,omitempty"`
	DeliveryError  string `json:"deliveryError,omitempty"`
	SessionID      string `json:"sessionId,omitempty"`
	RunAtMs        int64  `json:"runAtMs,omitempty"`
	DurationMs     int64  `json:"durationMs,omitempty"`
	NextRunAtMs    int64  `json:"nextRunAtMs,omitempty"`
	Model          string `json:"model,omitempty"`
	Provider       string `json:"provider,omitempty"`
	InputTokens    int    `json:"inputTokens,omitempty"`
	OutputTokens   int    `json:"outputTokens,omitempty"`
	CacheTokens    int    `json:"cacheTokens,omitempty"`
	JobName        string `json:"jobName,omitempty"` // Added when reading "all" scope
}

// appendRunLog writes a run log entry to the database.
func (s *Scheduler) appendRunLog(entry RunLogEntry) {
	ctx := context.Background()
	entry.Action = "finished"
	if entry.Ts == 0 {
		entry.Ts = time.Now().UnixMilli()
	}
	if entry.RunAtMs == 0 {
		entry.RunAtMs = time.Now().UnixMilli()
	}

	dbEntry := &storage.DBRunLogEntry{
		JobID:          entry.JobID,
		Action:         entry.Action,
		Status:         entry.Status,
		Error:          entry.Error,
		Summary:        entry.Summary,
		Delivered:      entry.Delivered,
		DeliveryStatus: entry.DeliveryStatus,
		DeliveryError:  entry.DeliveryError,
		SessionID:      entry.SessionID,
		RunAtMs:        entry.RunAtMs,
		DurationMs:     entry.DurationMs,
		NextRunAtMs:    entry.NextRunAtMs,
		Model:          entry.Model,
		Provider:       entry.Provider,
		InputTokens:    entry.InputTokens,
		OutputTokens:   entry.OutputTokens,
		CacheTokens:    entry.CacheTokens,
	}

	if err := s.db.AppendRunLog(ctx, dbEntry); err != nil {
		s.logger.Error("failed to append run log", zap.String("jobId", entry.JobID), zap.Error(err))
		return
	}

	// Prune old entries
	if err := s.db.PruneRunLogs(ctx, entry.JobID, runLogKeepN); err != nil {
		s.logger.Error("failed to prune run logs", zap.String("jobId", entry.JobID), zap.Error(err))
	}
}

// dbRunLogsToEntries converts database run log entries to RunLogEntry structs.
func dbRunLogsToEntries(dbEntries []*storage.DBRunLogEntry) []RunLogEntry {
	if len(dbEntries) == 0 {
		return nil
	}
	entries := make([]RunLogEntry, len(dbEntries))
	for i, d := range dbEntries {
		entries[i] = RunLogEntry{
			Ts:             0, // will be set from CreatedAt below
			JobID:          d.JobID,
			Action:         d.Action,
			Status:         d.Status,
			Error:          d.Error,
			Summary:        d.Summary,
			Delivered:      d.Delivered,
			DeliveryStatus: d.DeliveryStatus,
			DeliveryError:  d.DeliveryError,
			SessionID:      d.SessionID,
			RunAtMs:        d.RunAtMs,
			DurationMs:     d.DurationMs,
			NextRunAtMs:    d.NextRunAtMs,
			Model:          d.Model,
			Provider:       d.Provider,
			InputTokens:    d.InputTokens,
			OutputTokens:   d.OutputTokens,
			CacheTokens:    d.CacheTokens,
		}
		if d.CreatedAt != nil {
			entries[i].Ts = d.CreatedAt.UnixMilli()
		}
	}
	return entries
}
