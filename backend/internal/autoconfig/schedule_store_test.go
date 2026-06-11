package autoconfig

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	schedulertool "github.com/auto-developer-orchestrator/backend/internal/tools/scheduler"
)

// mockSchedulerBackend implements schedulertool.Backend in-memory for tests.
type mockSchedulerBackend struct {
	mu   sync.Mutex
	jobs map[string]*schedulertool.JobInfo
	idSeq int
}

func newMockSchedulerBackend() *mockSchedulerBackend {
	return &mockSchedulerBackend{
		jobs: make(map[string]*schedulertool.JobInfo),
	}
}

func (m *mockSchedulerBackend) ListJobsInfo() []*schedulertool.JobInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*schedulertool.JobInfo, 0, len(m.jobs))
	for _, j := range m.jobs {
		result = append(result, j)
	}
	return result
}

func (m *mockSchedulerBackend) FindJobByNameOrID(nameOrID string) *schedulertool.JobInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.Name == nameOrID || j.ID == nameOrID {
			return j
		}
	}
	return nil
}

func (m *mockSchedulerBackend) CreateJobParams(name, project, message, scheduleType, cronExpr, atTime, description, model string, everySeconds int64, enabled bool) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idSeq++
	id := fmt.Sprintf("job-%d", m.idSeq)
	m.jobs[name] = &schedulertool.JobInfo{
		ID: id, Name: name, Project: project, Message: message,
		Schedule: scheduleType, CronExpr: cronExpr, AtTime: atTime,
		Description: description, Model: model, EverySeconds: everySeconds,
		Enabled: enabled, Status: "idle",
	}
	return id, nil
}

func (m *mockSchedulerBackend) UpdateJobParams(id, name, message, project, model, description, scheduleType, cronExpr, atTime string, everySeconds int64, enabled *bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == id {
			if name != "" {
				j.Name = name
			}
			if message != "" {
				j.Message = message
			}
			if project != "" {
				j.Project = project
			}
			if model != "" {
				j.Model = model
			}
			if description != "" {
				j.Description = description
			}
			if scheduleType != "" {
				j.Schedule = scheduleType
			}
			if cronExpr != "" {
				j.CronExpr = cronExpr
			}
			if atTime != "" {
				j.AtTime = atTime
			}
			if everySeconds > 0 {
				j.EverySeconds = everySeconds
			}
			if enabled != nil {
				j.Enabled = *enabled
			}
			return nil
		}
	}
	return fmt.Errorf("job %q not found", id)
}

func (m *mockSchedulerBackend) DeleteJob(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, j := range m.jobs {
		if j.ID == id {
			delete(m.jobs, name)
			return nil
		}
	}
	return fmt.Errorf("job %q not found", id)
}

func (m *mockSchedulerBackend) TriggerJob(idOrName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == idOrName || j.Name == idOrName {
			j.LastRunAt = time.Now()
			j.LastRunStatus = "running"
			return nil
		}
	}
	return fmt.Errorf("job %q not found", idOrName)
}

func (m *mockSchedulerBackend) ListRunsInfo(jobID string, limit int) []schedulertool.RunInfo {
	return nil
}

// ── Tests ──────────────────────────────────────────────────────────

func TestScheduleStoreList(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)
	ctx := context.Background()

	// Empty
	result, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	m := result.(map[string]any)
	if m["count"] != 0 {
		t.Errorf("count = %d, want 0", m["count"])
	}

	// Create two jobs via backend directly
	backend.CreateJobParams("morning-report", "myapp", "Write morning report", "cron", "0 9 * * *", "", "", "", 0, true)
	backend.CreateJobParams("nightly-backup", "myapp", "Run nightly backup", "every", "", "", "", "", 86400, true)

	result, err = s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	m = result.(map[string]any)
	items := m["items"].([]string)
	if len(items) != 2 {
		t.Errorf("count = %d, want 2", len(items))
	}
}

func TestScheduleStoreGet(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)
	ctx := context.Background()

	id, err := backend.CreateJobParams("daily-digest", "myapp", "Send daily digest", "cron", "0 8 * * *", "", "Daily email digest", "gemma", 0, true)
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.Get(ctx, "daily-digest")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	m := result.(map[string]any)
	if m["name"] != "daily-digest" {
		t.Errorf("name = %q, want daily-digest", m["name"])
	}
	if m["id"] != id {
		t.Errorf("id = %q, want %q", m["id"], id)
	}
	if m["scheduleType"] != "cron" {
		t.Errorf("scheduleType = %q, want cron", m["scheduleType"])
	}
	if m["message"] != "Send daily digest" {
		t.Errorf("message = %q, want Send daily digest", m["message"])
	}
}

func TestScheduleStoreGetByID(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)
	ctx := context.Background()

	id, _ := backend.CreateJobParams("weekly-summary", "myapp", "Weekly summary", "at", "", "2026-01-01T00:00:00Z", "", "", 0, true)

	result, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get by ID: %v", err)
	}
	m := result.(map[string]any)
	if m["name"] != "weekly-summary" {
		t.Errorf("name = %q, want weekly-summary", m["name"])
	}
}

func TestScheduleStorePutCreate(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)
	ctx := context.Background()

	spec := map[string]any{
		"message":      "Send hourly report",
		"project":      "monitor",
		"scheduleType": "every",
		"everySeconds": float64(3600),
		"description":  "Hourly status report",
	}

	result, err := s.Put(ctx, "hourly-report", spec)
	if err != nil {
		t.Fatalf("Put create: %v", err)
	}
	msg := result.(map[string]any)["message"].(string)
	if msg == "" {
		t.Error("expected non-empty message from Put")
	}

	// Verify it exists
	job := backend.FindJobByNameOrID("hourly-report")
	if job == nil {
		t.Fatal("expected job to exist after Put")
	}
	if job.Message != "Send hourly report" {
		t.Errorf("message = %q, want Send hourly report", job.Message)
	}
}

func TestScheduleStorePutUpdate(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)
	ctx := context.Background()

	backend.CreateJobParams("daily-report", "myapp", "Original message", "cron", "0 9 * * *", "", "", "", 0, true)

	spec := map[string]any{
		"message": "Updated message",
		"enabled": false,
	}

	result, err := s.Put(ctx, "daily-report", spec)
	if err != nil {
		t.Fatalf("Put update: %v", err)
	}
	_ = result

	job := backend.FindJobByNameOrID("daily-report")
	if job == nil {
		t.Fatal("expected job to exist")
	}
	if job.Message != "Updated message" {
		t.Errorf("message = %q, want Updated message", job.Message)
	}
	if job.Enabled != false {
		t.Errorf("enabled = %v, want false", job.Enabled)
	}
}

func TestScheduleStoreDelete(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)
	ctx := context.Background()

	backend.CreateJobParams("temp-job", "myapp", "Temp", "manual", "", "", "", "", 0, true)

	if err := s.Delete(ctx, "temp-job"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if job := backend.FindJobByNameOrID("temp-job"); job != nil {
		t.Error("expected job to be deleted")
	}
}

func TestScheduleStoreDeleteNotFound(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)
	ctx := context.Background()

	if err := s.Delete(ctx, "nonexistent"); err == nil {
		t.Error("expected error for nonexistent schedule")
	}
}

func TestScheduleStoreGetNotFound(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)
	ctx := context.Background()

	_, err := s.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent schedule")
	}
}

func TestScheduleStoreBackend(t *testing.T) {
	backend := newMockSchedulerBackend()
	s := NewScheduleStore(backend)

	if s.Backend() != backend {
		t.Error("Backend() should return the wrapped backend")
	}
}

func TestScheduleListViaTool(t *testing.T) {
	backend := newMockSchedulerBackend()
	store := NewScheduleStore(backend)
	tool := NewScheduleTool(store)
	ctx := context.Background()

	// Empty
	result, err := tool.Execute(ctx, map[string]any{"operation": "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	m := result.(map[string]any)
	if m["count"] != 0 {
		t.Errorf("count = %d, want 0", m["count"])
	}

	// Add one
	backend.CreateJobParams("test-job", "p", "msg", "manual", "", "", "", "", 0, true)
	result, err = tool.Execute(ctx, map[string]any{"operation": "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	m = result.(map[string]any)
	if m["count"] != 1 {
		t.Errorf("count = %d, want 1", m["count"])
	}
}

func TestScheduleShowViaTool(t *testing.T) {
	backend := newMockSchedulerBackend()
	store := NewScheduleStore(backend)
	tool := NewScheduleTool(store)
	ctx := context.Background()

	backend.CreateJobParams("show-job", "myapp", "Show me", "cron", "0 9 * * *", "", "", "", 0, true)

	result, err := tool.Execute(ctx, map[string]any{"operation": "show", "name": "show-job"})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	m := result.(map[string]any)
	if m["operation"] != "show" {
		t.Errorf("operation = %q, want show", m["operation"])
	}
	job := m["job"].(map[string]any)
	if job["name"] != "show-job" {
		t.Errorf("name = %q, want show-job", job["name"])
	}
}

func TestScheduleCreateViaTool(t *testing.T) {
	backend := newMockSchedulerBackend()
	store := NewScheduleStore(backend)
	tool := NewScheduleTool(store)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"operation":    "create",
		"name":         "my-schedule",
		"message":      "Do the thing",
		"scheduleType": "cron",
		"cronExpr":     "0 9 * * *",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = result

	job := backend.FindJobByNameOrID("my-schedule")
	if job == nil {
		t.Fatal("expected job after create")
	}
}

func TestScheduleDeleteViaTool(t *testing.T) {
	backend := newMockSchedulerBackend()
	store := NewScheduleStore(backend)
	tool := NewScheduleTool(store)
	ctx := context.Background()

	backend.CreateJobParams("delete-me", "p", "msg", "manual", "", "", "", "", 0, true)

	result, err := tool.Execute(ctx, map[string]any{"operation": "delete", "name": "delete-me"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	_ = result

	if job := backend.FindJobByNameOrID("delete-me"); job != nil {
		t.Error("expected job to be deleted")
	}
}

func TestScheduleTriggerViaTool(t *testing.T) {
	backend := newMockSchedulerBackend()
	store := NewScheduleStore(backend)
	tool := NewScheduleTool(store)
	ctx := context.Background()

	backend.CreateJobParams("trigger-me", "p", "msg", "manual", "", "", "", "", 0, true)

	result, err := tool.Execute(ctx, map[string]any{"operation": "trigger", "name": "trigger-me"})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	_ = result

	job := backend.FindJobByNameOrID("trigger-me")
	if job == nil {
		t.Fatal("expected job to exist")
	}
	if job.LastRunStatus != "running" {
		t.Errorf("LastRunStatus = %q, want running", job.LastRunStatus)
	}
}

func TestScheduleRunsViaTool(t *testing.T) {
	backend := newMockSchedulerBackend()
	store := NewScheduleStore(backend)
	tool := NewScheduleTool(store)
	ctx := context.Background()

	backend.CreateJobParams("runs-job", "p", "msg", "manual", "", "", "", "", 0, true)

	// Runs with name
	result, err := tool.Execute(ctx, map[string]any{"operation": "runs", "name": "runs-job"})
	if err != nil {
		t.Fatalf("runs with name: %v", err)
	}
	m := result.(map[string]any)
	if count, ok := m["count"].(int); !ok || count != 0 {
		t.Errorf("count = %v (type %T), want 0", m["count"], m["count"])
	}

	// Runs without name (all jobs)
	result, err = tool.Execute(ctx, map[string]any{"operation": "runs"})
	if err != nil {
		t.Fatalf("runs all: %v", err)
	}
	m = result.(map[string]any)
	if count, ok := m["count"].(int); !ok || count != 0 {
		t.Errorf("count = %v, want 0", m["count"])
	}
}

func TestScheduleToolMissingOperation(t *testing.T) {
	store := NewScheduleStore(newMockSchedulerBackend())
	tool := NewScheduleTool(store)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Error("expected error for missing operation")
	}
}

func TestScheduleToolMissingName(t *testing.T) {
	store := NewScheduleStore(newMockSchedulerBackend())
	tool := NewScheduleTool(store)
	ctx := context.Background()

	tests := []string{"show", "update", "delete", "trigger"}
	for _, op := range tests {
		_, err := tool.Execute(ctx, map[string]any{"operation": op})
		if err == nil {
			t.Errorf("expected error for %s without name", op)
		}
	}
}

func TestScheduleToolCreateRequiresMessage(t *testing.T) {
	store := NewScheduleStore(newMockSchedulerBackend())
	tool := NewScheduleTool(store)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{
		"operation": "create",
		"name":      "no-msg",
	})
	if err == nil {
		t.Error("expected error for create without message")
	}
}
