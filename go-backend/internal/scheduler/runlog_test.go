package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newTestRunLogManager(t *testing.T) *RunLogManager {
	t.Helper()
	dir := t.TempDir()
	mgr, err := NewRunLogManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func TestNewRunLogManagerCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "subdir", "runs")
	mgr, err := NewRunLogManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
	_ = mgr
}

func TestAppendAndRead(t *testing.T) {
	mgr := newTestRunLogManager(t)

	entry := RunLogEntry{
		JobID:   "job-1",
		Status:  "ok",
		Summary: "success",
	}
	if err := mgr.Append(entry); err != nil {
		t.Fatal(err)
	}

	entries, err := mgr.Read("job-1", 10)
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

func TestAppendSetsTimestamps(t *testing.T) {
	mgr := newTestRunLogManager(t)

	entry := RunLogEntry{JobID: "job-2", Status: "ok"}
	mgr.Append(entry)

	entries, _ := mgr.Read("job-2", 10)
	if entries[0].Ts == 0 {
		t.Error("expected Ts to be set")
	}
	if entries[0].RunAtMs == 0 {
		t.Error("expected RunAtMs to be set")
	}
}

func TestReadMultipleEntries(t *testing.T) {
	mgr := newTestRunLogManager(t)

	for i := 0; i < 5; i++ {
		mgr.Append(RunLogEntry{JobID: "job-3", Status: "ok"})
	}

	entries, err := mgr.Read("job-3", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries with limit=3, got %d", len(entries))
	}
}

func TestReadReturnsNewestFirst(t *testing.T) {
	mgr := newTestRunLogManager(t)

	mgr.Append(RunLogEntry{JobID: "job-4", Status: "ok", Summary: "first"})
	mgr.Append(RunLogEntry{JobID: "job-4", Status: "ok", Summary: "second"})

	entries, _ := mgr.Read("job-4", 10)
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}
	if entries[0].Summary != "second" {
		t.Errorf("expected newest first, got %s", entries[0].Summary)
	}
}

func TestReadNonExistent(t *testing.T) {
	mgr := newTestRunLogManager(t)

	entries, err := mgr.Read("no-job", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for non-existent job, got %d", len(entries))
	}
}

func TestReadAllMultipleJobs(t *testing.T) {
	mgr := newTestRunLogManager(t)

	mgr.Append(RunLogEntry{JobID: "job-a", Status: "ok"})
	mgr.Append(RunLogEntry{JobID: "job-b", Status: "ok"})
	mgr.Append(RunLogEntry{JobID: "job-a", Status: "error"})

	entries, err := mgr.ReadAll(10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("expected 3 entries across all jobs, got %d", len(entries))
	}
}

func TestReadAllWithJobIDFilter(t *testing.T) {
	mgr := newTestRunLogManager(t)

	mgr.Append(RunLogEntry{JobID: "job-x", Status: "ok"})
	mgr.Append(RunLogEntry{JobID: "job-y", Status: "ok"})

	entries, err := mgr.ReadAll(10, "", "job-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry for job-x filter, got %d", len(entries))
	}
}

func TestReadAllWithStatusFilter(t *testing.T) {
	mgr := newTestRunLogManager(t)

	mgr.Append(RunLogEntry{JobID: "job-f", Status: "ok"})
	mgr.Append(RunLogEntry{JobID: "job-f", Status: "error"})

	entries, err := mgr.ReadAll(10, "error", "")
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
	mgr := newTestRunLogManager(t)

	for i := 0; i < 10; i++ {
		mgr.Append(RunLogEntry{JobID: "job-l", Status: "ok"})
	}

	entries, _ := mgr.ReadAll(5, "", "")
	if len(entries) > 5 {
		t.Errorf("expected at most 5 entries, got %d", len(entries))
	}
}

func TestJobPathSanitizes(t *testing.T) {
	mgr := newTestRunLogManager(t)
	path := mgr.jobPath("job/with/slashes")
	if filepath.Base(path) != "jobwithslashes.jsonl" {
		t.Errorf("expected sanitized filename, got %s", filepath.Base(path))
	}
}

func TestAppendCreatesJSONL(t *testing.T) {
	mgr := newTestRunLogManager(t)

	mgr.Append(RunLogEntry{JobID: "jsonl-test", Status: "ok"})

	path := mgr.jobPath("jsonl-test")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var entry RunLogEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Errorf("file should contain valid JSON lines: %v", err)
	}
}

func TestPruneKeepsLines(t *testing.T) {
	mgr := newTestRunLogManager(t)

	// Write enough entries to exceed the 2MB limit is impractical in tests,
	// so test the prune function directly with small limits
	for i := 0; i < 10; i++ {
		mgr.Append(RunLogEntry{JobID: "prune-test", Status: "ok", Summary: "entry"})
	}

	path := mgr.jobPath("prune-test")
	err := mgr.prune(path, 100, 5) // small maxBytes to trigger prune
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := mgr.Read("prune-test", 100)
	if len(entries) > 5 {
		t.Errorf("expected at most 5 entries after prune, got %d", len(entries))
	}
}

func TestPruneNoOpIfSmall(t *testing.T) {
	mgr := newTestRunLogManager(t)
	mgr.Append(RunLogEntry{JobID: "small-test", Status: "ok"})

	path := mgr.jobPath("small-test")
	err := mgr.prune(path, 10_000_000, 100) // large limit, should not prune
	if err != nil {
		t.Fatal(err)
	}

	entries, _ := mgr.Read("small-test", 100)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (no prune), got %d", len(entries))
	}
}
