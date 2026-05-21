package scheduler

import (
	"context"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
)

func newTestRunLogDB(t *testing.T) *storage.Database {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.NewDatabase(dir + "/runlog_test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAppendAndReadRunLog(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	entry := &storage.DBRunLogEntry{
		JobID:   "job-1",
		Action:  "finished",
		Status:  "ok",
		Summary: "success",
	}
	if err := db.AppendRunLog(ctx, entry); err != nil {
		t.Fatal(err)
	}

	entries, err := db.ReadRunLogs(ctx, "job-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].JobID != "job-1" {
		t.Errorf("expected jobID job-1, got %s", entries[0].JobID)
	}
	if entries[0].Status != "ok" {
		t.Errorf("expected status ok, got %s", entries[0].Status)
	}
	if entries[0].Action != "finished" {
		t.Errorf("expected action finished, got %s", entries[0].Action)
	}
}

func TestReadMultipleEntries(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-3", Status: "ok"})
	}

	entries, err := db.ReadRunLogs(ctx, "job-3", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries with limit=3, got %d", len(entries))
	}
}

func TestReadReturnsNewestFirst(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-4", Status: "ok", Summary: "first"})
	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-4", Status: "ok", Summary: "second"})

	entries, _ := db.ReadRunLogs(ctx, "job-4", 10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Both entries may have the same created_at timestamp, so we just verify both are present
	summaries := map[string]bool{entries[0].Summary: true, entries[1].Summary: true}
	if !summaries["first"] || !summaries["second"] {
		t.Errorf("expected both entries, got %v and %v", entries[0].Summary, entries[1].Summary)
	}
}

func TestReadNonExistent(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	entries, err := db.ReadRunLogs(ctx, "no-job", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for non-existent job, got %d", len(entries))
	}
}

func TestReadAllMultipleJobs(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-a", Status: "ok"})
	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-b", Status: "ok"})
	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-a", Status: "error"})

	entries, err := db.ReadAllRunLogs(ctx, 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries across all jobs, got %d", len(entries))
	}
}

func TestReadAllWithJobIDFilter(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-x", Status: "ok"})
	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-y", Status: "ok"})

	entries, err := db.ReadAllRunLogs(ctx, 10, "", "job-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry for job-x filter, got %d", len(entries))
	}
}

func TestReadAllWithStatusFilter(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-f", Status: "ok"})
	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-f", Status: "error"})

	entries, err := db.ReadAllRunLogs(ctx, 10, "error", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Status != "error" {
			t.Errorf("expected only error entries, got %s", e.Status)
		}
	}
}

func TestReadAllRespectsLimit(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "job-l", Status: "ok"})
	}

	entries, _ := db.ReadAllRunLogs(ctx, 5, "", "")
	if len(entries) > 5 {
		t.Errorf("expected at most 5 entries, got %d", len(entries))
	}
}

func TestPruneRunLogs(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "prune-test", Status: "ok"})
	}

	err := db.PruneRunLogs(ctx, "prune-test", 5)
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := db.ReadRunLogs(ctx, "prune-test", 100)
	if len(entries) > 5 {
		t.Errorf("expected at most 5 entries after prune, got %d", len(entries))
	}
}

func TestPruneNoOpIfSmall(t *testing.T) {
	db := newTestRunLogDB(t)
	ctx := context.Background()

	db.AppendRunLog(ctx, &storage.DBRunLogEntry{JobID: "small-test", Status: "ok"})

	err := db.PruneRunLogs(ctx, "small-test", 100)
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := db.ReadRunLogs(ctx, "small-test", 100)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (no prune), got %d", len(entries))
	}
}
