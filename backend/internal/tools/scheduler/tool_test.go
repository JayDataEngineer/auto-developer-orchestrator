package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ── Mock Backend ─────────────────────────────────────────────────────────

type mockBackend struct {
	jobs     []*JobInfo
	runs     []RunInfo
	lastID   string
	lastJob  *JobInfo
	deleted  []string
	triggered []string
	createErr error
	updateErr error
	deleteErr error
	triggerErr error
}

func (m *mockBackend) ListJobsInfo() []*JobInfo {
	return m.jobs
}

func (m *mockBackend) FindJobByNameOrID(nameOrID string) *JobInfo {
	for _, j := range m.jobs {
		if j.Name == nameOrID || j.ID == nameOrID {
			return j
		}
	}
	return nil
}

func (m *mockBackend) CreateJobParams(name, project, message, scheduleType, cronExpr, atTime, description, model string, everySeconds int64, enabled bool) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	id := "job-test-001"
	m.lastID = id
	m.lastJob = &JobInfo{
		ID: id, Name: name, Project: project, Message: message,
		Schedule: scheduleType, CronExpr: cronExpr, AtTime: atTime,
		Description: description, Model: model, EverySeconds: everySeconds,
		Enabled: enabled, Status: "idle",
	}
	m.jobs = append(m.jobs, m.lastJob)
	return id, nil
}

func (m *mockBackend) UpdateJobParams(id, name, message, project, model, description, scheduleType, cronExpr, atTime string, everySeconds int64, enabled *bool) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for _, j := range m.jobs {
		if j.ID == id {
			if name != "" {
				j.Name = name
			}
			if message != "" {
				j.Message = message
			}
			if scheduleType != "" {
				j.Schedule = scheduleType
			}
			if cronExpr != "" {
				j.CronExpr = cronExpr
			}
			if enabled != nil {
				j.Enabled = *enabled
			}
			break
		}
	}
	return nil
}

func (m *mockBackend) DeleteJob(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deleted = append(m.deleted, id)
	for i, j := range m.jobs {
		if j.ID == id {
			m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			break
		}
	}
	return nil
}

func (m *mockBackend) TriggerJob(id string) error {
	if m.triggerErr != nil {
		return m.triggerErr
	}
	m.triggered = append(m.triggered, id)
	return nil
}

func (m *mockBackend) ListRunsInfo(jobID string, limit int) []RunInfo {
	return m.runs
}

func newTool(m *mockBackend) *SchedulerTool {
	return NewSchedulerTool(m, "/home/user/projects/myapp")
}

func resultMap(result any) map[string]any {
	m, ok := result.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func resultText(result any) string {
	m := resultMap(result)
	if m == nil {
		return ""
	}
	content, ok := m["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		return ""
	}
	text, _ := content[0]["text"].(string)
	return text
}

// ── Tests ────────────────────────────────────────────────────────────────

func TestTool_Basics(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	if tool.Name() != "scheduler" {
		t.Errorf("expected name 'scheduler', got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
	schema := tool.Schema()
	if !strings.Contains(string(schema), `"action"`) {
		t.Error("schema should contain 'action' property")
	}
	if !strings.Contains(string(schema), `"create"`) {
		t.Error("schema should contain 'create' action")
	}
}

func TestTool_List_Empty(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "No scheduled jobs") {
		t.Errorf("expected empty message, got %q", text)
	}
}

func TestTool_List_WithJobs(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{ID: "j1", Name: "Daily Report", Project: "myapp", Status: "idle", Enabled: true, Schedule: "cron", CronExpr: "0 9 * * *"},
			{ID: "j2", Name: "Health Check", Project: "myapp", Status: "running", Enabled: true, Schedule: "every", EverySeconds: 300},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Daily Report") {
		t.Errorf("expected 'Daily Report' in output, got %q", text)
	}
	if !strings.Contains(text, "Health Check") {
		t.Errorf("expected 'Health Check' in output, got %q", text)
	}
}

func TestTool_Detail_Found(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{ID: "j1", Name: "Daily Report", Project: "myapp", Message: "Summarize", Status: "idle", Enabled: true, Schedule: "cron", CronExpr: "0 9 * * *"},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "detail",
		"name":   "Daily Report",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Daily Report") {
		t.Errorf("expected job name, got %q", text)
	}
	if !strings.Contains(text, "myapp") {
		t.Errorf("expected project, got %q", text)
	}
}

func TestTool_Detail_NotFound(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "detail",
		"name":   "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing job")
	}
	te, ok := err.(*core.ToolError)
	if !ok {
		t.Fatalf("expected ToolError, got %T: %v", err, err)
	}
	if !strings.Contains(te.Message, "not found") {
		t.Errorf("expected 'not found' in error, got %q", te.Message)
	}
}

func TestTool_Create_Cron(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":      "create",
		"name":        "Morning Report",
		"message":     "Generate daily report",
		"project":     "myapp",
		"scheduleType": "cron",
		"cronExpr":    "0 9 * * *",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Created job 'Morning Report'") {
		t.Errorf("expected creation message, got %q", text)
	}
	if m.lastJob.Schedule != "cron" {
		t.Errorf("expected schedule 'cron', got %q", m.lastJob.Schedule)
	}
	if m.lastJob.CronExpr != "0 9 * * *" {
		t.Errorf("expected cronExpr, got %q", m.lastJob.CronExpr)
	}
}

func TestTool_Create_Every(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":      "create",
		"name":        "Health Check",
		"message":     "Run tests",
		"project":     "myapp",
		"scheduleType": "every",
		"everySeconds": float64(300),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Created job") {
		t.Errorf("expected creation message, got %q", text)
	}
	if m.lastJob.EverySeconds != 300 {
		t.Errorf("expected everySeconds=300, got %d", m.lastJob.EverySeconds)
	}
}

func TestTool_Create_Manual(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":  "create",
		"name":    "One-off Task",
		"message": "Do the thing",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Created job") {
		t.Errorf("expected creation message, got %q", text)
	}
	if m.lastJob.Schedule != "manual" {
		t.Errorf("expected schedule 'manual', got %q", m.lastJob.Schedule)
	}
	// Project defaults to project dir name
	if m.lastJob.Project != "myapp" {
		t.Errorf("expected default project 'myapp', got %q", m.lastJob.Project)
	}
}

func TestTool_Create_MissingName(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":  "create",
		"message": "Do stuff",
	})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestTool_Create_MissingMessage(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"name":   "test",
	})
	if err == nil {
		t.Fatal("expected error for missing message")
	}
}

func TestTool_Create_BackendError(t *testing.T) {
	m := &mockBackend{createErr: core.NewToolError("scheduler", "invalid cron")}
	tool := newTool(m)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":   "create",
		"name":     "test",
		"message":  "msg",
		"cronExpr": "bad",
	})
	if err == nil {
		t.Fatal("expected error from backend")
	}
	if !strings.Contains(err.Error(), "create failed") {
		t.Errorf("expected 'create failed', got %v", err)
	}
}

func TestTool_Edit(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{ID: "j1", Name: "Daily Report", Project: "myapp", Message: "old", Status: "idle", Enabled: true, Schedule: "cron", CronExpr: "0 9 * * *"},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action":  "edit",
		"name":    "Daily Report",
		"message": "new prompt",
		"cronExpr": "0 10 * * *",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Updated job") {
		t.Errorf("expected update message, got %q", text)
	}
	if m.jobs[0].Message != "new prompt" {
		t.Errorf("expected updated message, got %q", m.jobs[0].Message)
	}
}

func TestTool_Edit_Enable(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{ID: "j1", Name: "Job1", Status: "disabled", Enabled: false, Schedule: "manual"},
		},
	}
	tool := newTool(m)

	disabled := false
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":  "edit",
		"name":    "Job1",
		"enabled": disabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTool_Edit_NotFound(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	_, err := tool.Execute(context.Background(), map[string]any{
		"action":  "edit",
		"name":    "ghost",
		"message": "new",
	})
	if err == nil {
		t.Fatal("expected error for missing job")
	}
}

func TestTool_Delete(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{ID: "j1", Name: "Daily Report", Schedule: "manual"},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "delete",
		"name":   "Daily Report",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Deleted job") {
		t.Errorf("expected delete message, got %q", text)
	}
	if len(m.jobs) != 0 {
		t.Errorf("expected 0 jobs after delete, got %d", len(m.jobs))
	}
	if len(m.deleted) != 1 || m.deleted[0] != "j1" {
		t.Errorf("expected j1 in deleted list, got %v", m.deleted)
	}
}

func TestTool_Delete_ByID(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{ID: "j1", Name: "Daily Report", Schedule: "manual"},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "delete",
		"name":   "j1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Deleted job") {
		t.Errorf("expected delete message, got %q", text)
	}
}

func TestTool_Trigger(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{ID: "j1", Name: "Daily Report", Schedule: "manual"},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "trigger",
		"name":   "Daily Report",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Triggered job") {
		t.Errorf("expected trigger message, got %q", text)
	}
	if len(m.triggered) != 1 || m.triggered[0] != "j1" {
		t.Errorf("expected j1 triggered, got %v", m.triggered)
	}
}

func TestTool_Runs_Empty(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{"action": "runs"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "No run history") {
		t.Errorf("expected empty runs message, got %q", text)
	}
}

func TestTool_Runs_WithData(t *testing.T) {
	m := &mockBackend{
		runs: []RunInfo{
			{Ts: time.Now().UnixMilli(), Status: "ok", Summary: "Completed successfully"},
			{Ts: time.Now().Add(-1 * time.Hour).UnixMilli(), Status: "error", Error: "connection timeout"},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{"action": "runs"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Recent Runs") {
		t.Errorf("expected runs header, got %q", text)
	}
	if !strings.Contains(text, "ok") {
		t.Errorf("expected 'ok' status, got %q", text)
	}
}

func TestTool_Runs_FilterByJob(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{ID: "j1", Name: "Daily Report", Schedule: "manual"},
		},
		runs: []RunInfo{
			{Ts: time.Now().UnixMilli(), Status: "ok", Summary: "done"},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "runs",
		"name":   "Daily Report",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Recent Runs") {
		t.Errorf("expected runs header, got %q", text)
	}
}

func TestTool_UnknownAction(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	_, err := tool.Execute(context.Background(), map[string]any{"action": "foobar"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("expected 'unknown action', got %v", err)
	}
}

func TestTool_MissingAction(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)

	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing action")
	}
}

func TestNewSchedulerToolFromAny(t *testing.T) {
	m := &mockBackend{}

	// nil returns nil
	if tool := NewSchedulerToolFromAny(nil, "/tmp"); tool != nil {
		t.Error("expected nil for nil input")
	}

	// Valid backend
	tool := NewSchedulerToolFromAny(m, "/home/user/projects/testproj")
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}
	if tool.projectName != "testproj" {
		t.Errorf("expected projectName 'testproj', got %q", tool.projectName)
	}
}

func TestTool_Schema_HasAllActions(t *testing.T) {
	m := &mockBackend{}
	tool := newTool(m)
	schema := string(tool.Schema())

	actions := []string{"list", "detail", "create", "edit", "delete", "trigger", "runs"}
	for _, a := range actions {
		if !strings.Contains(schema, a) {
			t.Errorf("schema missing action %q", a)
		}
	}
}

func TestTool_Detail_WithMetrics(t *testing.T) {
	now := time.Now()
	m := &mockBackend{
		jobs: []*JobInfo{
			{
				ID: "j1", Name: "Daily Report", Project: "myapp", Message: "Summarize",
				Status: "idle", Enabled: true, Schedule: "cron", CronExpr: "0 9 * * *",
				LastRunAt: now.Add(-24 * time.Hour), LastRunStatus: "success",
				NextRunAt: now.Add(1 * time.Hour), InputTokens: 1200, OutputTokens: 3400,
				DurationMs: 45000, ConsecutiveErrors: 0,
			},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "detail",
		"name":   "Daily Report",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "Tokens:") {
		t.Errorf("expected token info, got %q", text)
	}
	if !strings.Contains(text, "Duration:") {
		t.Errorf("expected duration info, got %q", text)
	}
	if !strings.Contains(text, "Next Run:") {
		t.Errorf("expected next run info, got %q", text)
	}
}

func TestTool_Detail_WithErrors(t *testing.T) {
	m := &mockBackend{
		jobs: []*JobInfo{
			{
				ID: "j1", Name: "Failing Job", Project: "myapp", Message: "Do thing",
				Status: "error", Enabled: true, Schedule: "cron", CronExpr: "0 9 * * *",
				LastError: "connection refused", ConsecutiveErrors: 3,
			},
		},
	}
	tool := newTool(m)

	result, err := tool.Execute(context.Background(), map[string]any{
		"action": "detail",
		"name":   "Failing Job",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	if !strings.Contains(text, "connection refused") {
		t.Errorf("expected error text, got %q", text)
	}
	if !strings.Contains(text, "Consecutive Errors:") {
		t.Errorf("expected consecutive errors, got %q", text)
	}
}

// ── Formatting helpers ───────────────────────────────────────────────────

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "0s"},
		{5000, "5s"},
		{30000, "30s"},
		{60000, "1m"},
		{300000, "5m"},
		{3600000, "1h"},
		{7200000, "2h"},
		{86400000, "1d"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.ms)
		if got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.ms, got, tt.want)
		}
	}
}

func TestFormatJobSchedule(t *testing.T) {
	tests := []struct {
		job  *JobInfo
		want string
	}{
		{&JobInfo{Schedule: "cron", CronExpr: "0 9 * * *"}, "cron: 0 9 * * *"},
		{&JobInfo{Schedule: "every", EverySeconds: 300}, "every: 5m"},
		{&JobInfo{Schedule: "at", AtTime: "2026-06-01T09:00:00Z"}, "at: 2026-06-01T09:00:00Z"},
		{&JobInfo{Schedule: "manual"}, "manual"},
	}
	for _, tt := range tests {
		got := formatJobSchedule(tt.job)
		if got != tt.want {
			t.Errorf("formatJobSchedule(%+v) = %q, want %q", tt.job, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q, want 'hello'", got)
	}
	if got := truncate("hello world!", 8); got != "hello w..." {
		t.Errorf("truncate long = %q, want 'hello w...'", got)
	}
}
