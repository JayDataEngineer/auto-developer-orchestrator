package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/auto-developer-orchestrator/backend/internal/util"
	"go.uber.org/zap"
)

func newTestDB(t *testing.T) *storage.Database {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.NewDatabase(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestScheduler(t *testing.T) (*Scheduler, *storage.Database) {
	t.Helper()
	db := newTestDB(t)
	logger := zap.NewNop()
	s := NewScheduler(db, func(ctx context.Context, project, agentID, message, model, org string, autoBranch, autoMerge, sandboxOnly bool) (string, error) {
		return "test output", nil
	}, logger)
	return s, db
}

// --- Job validation ---

func TestValidateJobMissingName(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 60}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for missing name")
	}
}

func TestValidateJobMissingProject(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Message: "m", Schedule: ScheduleEvery, EverySeconds: 60}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for missing project")
	}
}

func TestValidateJobMissingMessage(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Schedule: ScheduleEvery, EverySeconds: 60}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for missing message")
	}
}

func TestValidateJobCronMissingExpr(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: ScheduleCron}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for missing cron expression")
	}
}

func TestValidateJobCronInvalidExpr(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: ScheduleCron, CronExpr: "not-valid"}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for invalid cron expression")
	}
}

func TestValidateJobCronValid(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: ScheduleCron, CronExpr: "0 * * * * *"}
	if err := s.validateJob(job); err != nil {
		t.Errorf("valid cron job should pass: %v", err)
	}
}

func TestValidateJobEveryZeroSeconds(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 0}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for zero everySeconds")
	}
}

func TestValidateJobEveryValid(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 60}
	if err := s.validateJob(job); err != nil {
		t.Errorf("valid every job should pass: %v", err)
	}
}

func TestValidateJobAtMissingTime(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: ScheduleAt}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for missing atTime")
	}
}

func TestValidateJobAtInvalidTime(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: ScheduleAt, AtTime: "not-rfc3339"}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for invalid atTime")
	}
}

func TestValidateJobAtValid(t *testing.T) {
	s, _ := newTestScheduler(t)
	future := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: ScheduleAt, AtTime: future}
	if err := s.validateJob(job); err != nil {
		t.Errorf("valid at job should pass: %v", err)
	}
}

func TestValidateJobInvalidScheduleType(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "n", Project: "p", Message: "m", Schedule: "bogus"}
	if err := s.validateJob(job); err == nil {
		t.Error("expected error for invalid schedule type")
	}
}

// --- Job CRUD ---

func TestCreateJobEvery(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "test", Project: "p", Message: "hello", Schedule: ScheduleEvery, EverySeconds: 300, Enabled: true}
	if err := s.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if job.ID == "" {
		t.Error("expected job ID to be set")
	}
	if job.Status != StatusIdle {
		t.Errorf("expected status idle, got %s", job.Status)
	}
	if job.NextRunAt.IsZero() {
		t.Error("expected NextRunAt to be set")
	}
}

func TestCreateJobDisabled(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "test", Project: "p", Message: "hello", Schedule: ScheduleEvery, EverySeconds: 300, Enabled: false}
	if err := s.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusDisabled {
		t.Errorf("expected status disabled, got %s", job.Status)
	}
}

func TestGetJobNotFound(t *testing.T) {
	s, _ := newTestScheduler(t)
	_, err := s.GetJob("nonexistent")
	if err == nil {
		t.Error("expected error for missing job")
	}
}

func TestGetJobFound(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{ID: "job-1", Name: "test", Project: "p", Message: "hello", Schedule: ScheduleEvery, EverySeconds: 300, Enabled: true}
	s.CreateJob(job)

	got, err := s.GetJob("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "test" {
		t.Errorf("expected name test, got %s", got.Name)
	}
}

func TestListJobsEmpty(t *testing.T) {
	s, _ := newTestScheduler(t)
	jobs := s.ListJobs()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}
}

func TestListJobsMultiple(t *testing.T) {
	s, _ := newTestScheduler(t)
	s.CreateJob(&Job{ID: "job-a", Name: "a", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 60, Enabled: true})
	s.CreateJob(&Job{ID: "job-b", Name: "b", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 120, Enabled: true})
	jobs := s.ListJobs()
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestUpdateJobNotFound(t *testing.T) {
	s, _ := newTestScheduler(t)
	err := s.UpdateJob("nonexistent", &Job{Name: "x"})
	if err == nil {
		t.Error("expected error for missing job")
	}
}

func TestUpdateJobName(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{ID: "job-u", Name: "old", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 60, Enabled: true}
	s.CreateJob(job)

	err := s.UpdateJob("job-u", &Job{Name: "new", Enabled: true, Schedule: ScheduleEvery, EverySeconds: 60})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetJob("job-u")
	if got.Name != "new" {
		t.Errorf("expected name new, got %s", got.Name)
	}
}

func TestDeleteJobNotFound(t *testing.T) {
	s, _ := newTestScheduler(t)
	err := s.DeleteJob("nonexistent")
	if err == nil {
		t.Error("expected error for missing job")
	}
}

func TestDeleteJob(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{ID: "job-d", Name: "test", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 60, Enabled: true}
	s.CreateJob(job)

	if err := s.DeleteJob("job-d"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetJob("job-d"); err == nil {
		t.Error("job should be deleted")
	}
}

// --- Cron scheduling ---

func TestCreateCronJob(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Name: "cron-test", Project: "p", Message: "tick", Schedule: ScheduleCron, CronExpr: "0 */5 * * * *", Enabled: true}
	if err := s.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if job.cronEntryID == 0 {
		t.Error("expected cron entry ID to be set")
	}
	if job.NextRunAt.IsZero() {
		t.Error("expected NextRunAt for cron job")
	}
}

// --- Persistence via database ---

func TestSaveAndLoad(t *testing.T) {
	db := newTestDB(t)
	logger := zap.NewNop()

	s1 := NewScheduler(db, nil, logger)
	s1.CreateJob(&Job{Name: "persist-test", Project: "p", Message: "m", Schedule: ScheduleEvery, EverySeconds: 300, Enabled: true})

	s2 := NewScheduler(db, nil, logger)
	if err := s2.load(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs := s2.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job after load, got %d", len(jobs))
	}
	if jobs[0].Name != "persist-test" {
		t.Errorf("expected name persist-test, got %s", jobs[0].Name)
	}
}

// --- Execution tracking ---

func TestListExecutionsEmpty(t *testing.T) {
	s, _ := newTestScheduler(t)
	execs := s.ListExecutions("", 10)
	if len(execs) != 0 {
		t.Errorf("expected 0 executions, got %d", len(execs))
	}
}

func TestTriggerJobNotFound(t *testing.T) {
	s, _ := newTestScheduler(t)
	if err := s.TriggerJob("nonexistent"); err == nil {
		t.Error("expected error for missing job")
	}
}

func TestTriggerJobExecutes(t *testing.T) {
	done := make(chan struct{}, 1)
	var called atomic.Int32
	db := newTestDB(t)
	logger := zap.NewNop()

	s := NewScheduler(db, func(ctx context.Context, project, agentID, message, model, org string, autoBranch, autoMerge, sandboxOnly bool) (string, error) {
		called.Add(1)
		select {
		case done <- struct{}{}:
		default:
		}
		return "done", nil
	}, logger)

	job := &Job{Name: "trigger-test", Project: "p", Message: "run me", Schedule: ScheduleEvery, EverySeconds: 300, Enabled: true}
	s.CreateJob(job)

	if err := s.TriggerJob(job.ID); err != nil {
		t.Fatal(err)
	}

	// Wait for async execution with timeout
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for prompt sender to be called")
	}

	if called.Load() != 1 {
		t.Errorf("expected prompt sender called once, got %d", called.Load())
	}
}

// --- Error backoff ---

func TestErrorBackoffZero(t *testing.T) {
	s, _ := newTestScheduler(t)
	if d := s.errorBackoff(0); d != 0 {
		t.Errorf("expected 0 for 0 errors, got %v", d)
	}
}

func TestErrorBackoffEscalates(t *testing.T) {
	s, _ := newTestScheduler(t)
	prev := time.Duration(0)
	for i := 1; i <= len(DefaultBackoffSchedule); i++ {
		d := s.errorBackoff(i)
		if d <= prev {
			t.Errorf("backoff should escalate: i=%d, d=%v, prev=%v", i, d, prev)
		}
		prev = d
	}
}

func TestErrorBackoffCapsAtMax(t *testing.T) {
	s, _ := newTestScheduler(t)
	maxBackoff := DefaultBackoffSchedule[len(DefaultBackoffSchedule)-1]
	d := s.errorBackoff(100)
	if d != maxBackoff {
		t.Errorf("expected max backoff %v, got %v", maxBackoff, d)
	}
}

// --- ComputeNextRun ---

func TestComputeNextRunEvery(t *testing.T) {
	s, _ := newTestScheduler(t)
	job := &Job{Schedule: ScheduleEvery, EverySeconds: 300}
	s.computeNextRun(job)
	if job.NextRunAt.IsZero() {
		t.Error("expected NextRunAt to be set for every schedule")
	}
}

func TestComputeNextRunAt(t *testing.T) {
	s, _ := newTestScheduler(t)
	future := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	job := &Job{Schedule: ScheduleAt, AtTime: future}
	s.computeNextRun(job)
	if job.NextRunAt.IsZero() {
		t.Error("expected NextRunAt to be set for at schedule")
	}
}

// --- TruncateStr ---

func TestTruncateStr(t *testing.T) {
	if s := util.Truncate("hello", 10); s != "hello" {
		t.Errorf("expected hello, got %s", s)
	}
	if s := util.Truncate("hello world", 5); s != "hello" {
		t.Errorf("expected hello, got %s", s)
	}
}
