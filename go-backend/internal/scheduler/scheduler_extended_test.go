package scheduler

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
)

func newExtScheduler(t *testing.T) *Scheduler {
	t.Helper()
	dir := t.TempDir()
	logger := zap.NewNop()
	s := NewScheduler(dir, nil, logger)
	return s
}

// ── FindJobByWebhookToken ─────────────────────────────────────

func TestFindJobByWebhookToken(t *testing.T) {
	s := newExtScheduler(t)

	s.CreateJob(&Job{
		Name:         "webhook-job",
		Project:      "proj",
		Message:      "test",
		Schedule:     ScheduleEvery,
		EverySeconds: 3600,
		WebhookToken: "secret-token",
	})

	found, err := s.FindJobByWebhookToken("secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if found.Name != "webhook-job" {
		t.Errorf("expected webhook-job, got %q", found.Name)
	}

	_, err = s.FindJobByWebhookToken("wrong-token")
	if err == nil {
		t.Error("expected error for wrong token")
	}
}

// ── ListExecutions ────────────────────────────────────────────

func TestListExecutionsFiltered(t *testing.T) {
	s := newExtScheduler(t)
	s.CreateJob(&Job{ID: "j1", Name: "exec-job", Project: "proj", Message: "test", Schedule: ScheduleEvery, EverySeconds: 60, Enabled: true})

	s.mu.Lock()
	s.executions = append(s.executions,
		&JobExecution{JobID: "j1", Status: "success"},
		&JobExecution{JobID: "j1", Status: "failed"},
		&JobExecution{JobID: "other", Status: "success"},
	)
	s.mu.Unlock()

	all := s.ListExecutions("", 10)
	if len(all) != 3 {
		t.Errorf("expected 3, got %d", len(all))
	}

	filtered := s.ListExecutions("j1", 10)
	if len(filtered) != 2 {
		t.Errorf("expected 2 for job filter, got %d", len(filtered))
	}

	limited := s.ListExecutions("", 1)
	if len(limited) != 1 {
		t.Errorf("expected 1 with limit, got %d", len(limited))
	}
}

// ── CanStart ──────────────────────────────────────────────────

func TestCanStartDeps(t *testing.T) {
	s := newExtScheduler(t)
	s.CreateJob(&Job{ID: "down", Name: "downstream", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 60, BlockedBy: []string{"up"}})

	// Upstream doesn't exist yet
	can, _ := s.CanStart("down")
	if can {
		t.Error("should not start when upstream doesn't exist")
	}

	// Create upstream with failure
	s.CreateJob(&Job{ID: "up", Name: "upstream", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 60, LastRunStatus: "failed"})
	can, _ = s.CanStart("down")
	if can {
		t.Error("should not start when upstream failed")
	}

	// Update upstream to success
	s.mu.Lock()
	s.jobs["up"].LastRunStatus = "success"
	s.mu.Unlock()
	can, _ = s.CanStart("down")
	if !can {
		t.Error("should start when all deps succeeded")
	}

	// Nonexistent job
	_, err := s.CanStart("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

// ── SetDependencies ───────────────────────────────────────────

func TestSetDependenciesSuccess(t *testing.T) {
	s := newExtScheduler(t)
	s.CreateJob(&Job{ID: "a", Name: "job-a", Project: "p", Message: "a", Schedule: ScheduleEvery, EverySeconds: 60})
	s.CreateJob(&Job{ID: "b", Name: "job-b", Project: "p", Message: "b", Schedule: ScheduleEvery, EverySeconds: 60})

	err := s.SetDependencies("a", []string{"b"}, nil)
	if err != nil {
		t.Fatalf("SetDependencies: %v", err)
	}

	job, _ := s.GetJob("a")
	if len(job.Blocks) != 1 || job.Blocks[0] != "b" {
		t.Errorf("expected blocks=[b], got %v", job.Blocks)
	}
}

func TestSetDependenciesCycleDetection(t *testing.T) {
	s := newExtScheduler(t)
	s.CreateJob(&Job{ID: "a", Name: "a", Project: "p", Message: "a", Schedule: ScheduleEvery, EverySeconds: 60, BlockedBy: []string{"b"}})
	s.CreateJob(&Job{ID: "b", Name: "b", Project: "p", Message: "b", Schedule: ScheduleEvery, EverySeconds: 60})

	err := s.SetDependencies("b", nil, []string{"a"})
	if err == nil {
		t.Error("expected error for cycle")
	}
}

func TestSetDependenciesNotFoundJob(t *testing.T) {
	s := newExtScheduler(t)
	err := s.SetDependencies("nonexistent", nil, nil)
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

// ── ListRuns / ListAllRuns ────────────────────────────────────

func TestListRunsNoLogMgr(t *testing.T) {
	s := newExtScheduler(t)
	runs, err := s.ListRuns("job-1", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if runs != nil {
		t.Error("expected nil with no log manager")
	}
}

func TestListAllRunsNoLogMgr(t *testing.T) {
	s := newExtScheduler(t)
	runs, err := s.ListAllRuns(10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if runs != nil {
		t.Error("expected nil with no log manager")
	}
}

// ── Start/Stop ────────────────────────────────────────────────

func TestSchedulerStartStop(t *testing.T) {
	s := newExtScheduler(t)
	s.Start(context.Background())
	time.Sleep(50 * time.Millisecond)
	s.Stop()
}

// ── SetIsolatedExecutor / SetSessionInjector ──────────────────

func TestSetIsolatedExecutorMethod(t *testing.T) {
	s := newExtScheduler(t)
	// SetIsolatedExecutor takes (*IsolatedExecutor, *RunLogManager, string)
	s.SetIsolatedExecutor(nil, nil, "/tmp/test")
	if s.projectRoot != "/tmp/test" {
		t.Errorf("expected projectRoot=/tmp/test, got %q", s.projectRoot)
	}
}

