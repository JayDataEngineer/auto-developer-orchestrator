package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/base"
)

// JobInfo is a read-only view of a scheduled job for tool consumption.
type JobInfo struct {
	ID               string
	Name             string
	Description      string
	Project          string
	Message          string
	Model            string
	Schedule         string
	CronExpr         string
	EverySeconds     int64
	AtTime           string
	Enabled          bool
	Status           string
	LastRunAt        time.Time
	LastRunStatus    string
	LastError        string
	NextRunAt        time.Time
	ConsecutiveErrors int
	InputTokens      int
	OutputTokens     int
	DurationMs       int64
}

// RunInfo is a read-only view of a run log entry for tool consumption.
type RunInfo struct {
	Ts      int64
	Status  string
	Summary string
	Error   string
	JobName string
}

// Backend is the interface the scheduler tool needs.
// Implemented by the scheduler package's adapter (backend.go).
type Backend interface {
	ListJobsInfo() []*JobInfo
	FindJobByNameOrID(nameOrID string) *JobInfo
	CreateJobParams(name, project, message, scheduleType, cronExpr, atTime, description, model string, everySeconds int64, enabled bool) (string, error)
	UpdateJobParams(id, name, message, project, model, description, scheduleType, cronExpr, atTime string, everySeconds int64, enabled *bool) error
	DeleteJob(id string) error
	TriggerJob(id string) error
	ListRunsInfo(jobID string, limit int) []RunInfo
}

// SchedulerTool lets the LLM manage scheduled jobs.
type SchedulerTool struct {
	backend     Backend
	projectDir  string
	projectName string
}

// NewSchedulerTool creates a scheduler tool from a Backend implementation.
func NewSchedulerTool(b Backend, projectDir string) *SchedulerTool {
	name := ""
	parts := strings.Split(strings.TrimRight(projectDir, "/"), "/")
	if len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	return &SchedulerTool{backend: b, projectDir: projectDir, projectName: name}
}

// AllTools returns every scheduler tool, wired with the given backend + projectDir.
// Pass a nil b to get nil back (caller skips scheduler wiring when no backend).
func AllTools(b Backend, projectDir string) []core.Tool {
	if b == nil {
		return nil
	}
	return []core.Tool{NewSchedulerTool(b, projectDir)}
}

// NewSchedulerToolFromAny accepts a Backend via any to break import cycles.
func NewSchedulerToolFromAny(b any, projectDir string) *SchedulerTool {
	if b == nil {
		return nil
	}
	return NewSchedulerTool(b.(Backend), projectDir)
}

func (t *SchedulerTool) Name() string        { return "scheduler" }
func (t *SchedulerTool) Description() string { return "Manage scheduled jobs: list, create, edit, delete, trigger, view details and run history. Jobs run prompts on cron schedules, intervals, or one-shot times." }

func (t *SchedulerTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["list", "detail", "create", "edit", "delete", "trigger", "runs"],
				"description": "Action to perform"
			},
			"name": {
				"type": "string",
				"description": "Job name or ID (for detail/edit/delete/trigger/runs)"
			},
			"project": {
				"type": "string",
				"description": "Project name for the job (defaults to current project)"
			},
			"message": {
				"type": "string",
				"description": "Prompt message for the job (for create/edit)"
			},
			"scheduleType": {
				"type": "string",
				"enum": ["cron", "every", "at", "manual"],
				"description": "Schedule type (for create/edit)"
			},
			"cronExpr": {
				"type": "string",
				"description": "Cron expression e.g. '0 9 * * *' for daily at 9am (for cron schedule)"
			},
			"everySeconds": {
				"type": "number",
				"description": "Interval in seconds (for every schedule)"
			},
			"atTime": {
				"type": "string",
				"description": "One-shot time in RFC3339 format (for at schedule)"
			},
			"description": {
				"type": "string",
				"description": "Job description"
			},
			"model": {
				"type": "string",
				"description": "Model override for the job"
			},
			"enabled": {
				"type": "boolean",
				"description": "Whether the job is enabled"
			}
		},
		"required": ["action"]
	}`)
}

func (t *SchedulerTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	action, _ := args["action"].(string)
	if action == "" {
		return nil, core.NewToolError("scheduler", "missing required parameter 'action'")
	}

	switch action {
	case "list":
		return t.actionList()
	case "detail":
		return t.actionDetail(args)
	case "create":
		return t.actionCreate(args)
	case "edit":
		return t.actionEdit(args)
	case "delete":
		return t.actionDelete(args)
	case "trigger":
		return t.actionTrigger(args)
	case "runs":
		return t.actionRuns(args)
	default:
		return nil, core.NewToolError("scheduler", fmt.Sprintf("unknown action '%s'. Use: list, detail, create, edit, delete, trigger, runs", action))
	}
}

func (t *SchedulerTool) actionList() (any, error) {
	jobs := t.backend.ListJobsInfo()
	if len(jobs) == 0 {
		return textResult("No scheduled jobs found."), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Scheduled Jobs (%d):", len(jobs)))
	lines = append(lines, "")
	for _, j := range jobs {
		status := j.Status
		if !j.Enabled {
			status = "disabled"
		}
		sched := formatJobSchedule(j)
		last := "never"
		if !j.LastRunAt.IsZero() {
			last = j.LastRunAt.Format("Jan 02 15:04")
		}
		next := "none"
		if !j.NextRunAt.IsZero() {
			next = j.NextRunAt.Format("Jan 02 15:04")
		}
		lines = append(lines, fmt.Sprintf("  %-20s  %-8s  %-16s  last: %s  next: %s", j.Name, status, sched, last, next))
	}

	return textResult(strings.Join(lines, "\n")), nil
}

func (t *SchedulerTool) actionDetail(args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("scheduler", "missing 'name' parameter for detail action")
	}
	job := t.backend.FindJobByNameOrID(name)
	if job == nil {
		return nil, core.NewToolError("scheduler", fmt.Sprintf("job '%s' not found", name))
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Job: %s (%s)", job.Name, job.ID))
	if job.Description != "" {
		lines = append(lines, fmt.Sprintf("  Description: %s", job.Description))
	}
	lines = append(lines, fmt.Sprintf("  Project: %s", job.Project))
	lines = append(lines, fmt.Sprintf("  Schedule: %s", formatJobSchedule(job)))
	lines = append(lines, fmt.Sprintf("  Enabled: %v  Status: %s", job.Enabled, job.Status))
	if job.Model != "" {
		lines = append(lines, fmt.Sprintf("  Model: %s", job.Model))
	}
	lines = append(lines, fmt.Sprintf("  Message: %s", job.Message))
	if !job.LastRunAt.IsZero() {
		lines = append(lines, fmt.Sprintf("  Last Run: %s (%s)", job.LastRunAt.Format(time.RFC3339), job.LastRunStatus))
	}
	if job.LastError != "" {
		lines = append(lines, fmt.Sprintf("  Last Error: %s", job.LastError))
	}
	if !job.NextRunAt.IsZero() {
		lines = append(lines, fmt.Sprintf("  Next Run: %s", job.NextRunAt.Format(time.RFC3339)))
	}
	if job.ConsecutiveErrors > 0 {
		lines = append(lines, fmt.Sprintf("  Consecutive Errors: %d", job.ConsecutiveErrors))
	}
	if job.InputTokens > 0 || job.OutputTokens > 0 {
		lines = append(lines, fmt.Sprintf("  Tokens: %d in / %d out", job.InputTokens, job.OutputTokens))
	}
	if job.DurationMs > 0 {
		lines = append(lines, fmt.Sprintf("  Duration: %s", formatDuration(job.DurationMs)))
	}

	return textResult(strings.Join(lines, "\n")), nil
}

func (t *SchedulerTool) actionCreate(args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	message, _ := args["message"].(string)
	project, _ := args["project"].(string)

	if name == "" {
		return nil, core.NewToolError("scheduler", "missing 'name' for create")
	}
	if message == "" {
		return nil, core.NewToolError("scheduler", "missing 'message' for create")
	}
	if project == "" {
		project = t.projectName
	}

	// Determine schedule type
	scheduleType := "manual"
	if v, ok := args["scheduleType"].(string); ok && v != "" {
		scheduleType = v
	} else {
		if v, ok := args["cronExpr"].(string); ok && v != "" {
			scheduleType = "cron"
		} else if v, ok := args["everySeconds"].(float64); ok && v > 0 {
			scheduleType = "every"
		} else if v, ok := args["atTime"].(string); ok && v != "" {
			scheduleType = "at"
		}
	}

	var everySeconds int64
	if v, ok := args["everySeconds"].(float64); ok {
		everySeconds = int64(v)
	}

	enabled := true
	if v, ok := args["enabled"].(bool); ok {
		enabled = v
	}

	id, err := t.backend.CreateJobParams(
		name, project, message, scheduleType,
		base.StringArgDefault(args, "cronExpr", ""),
		base.StringArgDefault(args, "atTime", ""),
		base.StringArgDefault(args, "description", ""),
		base.StringArgDefault(args, "model", ""),
		everySeconds,
		enabled,
	)
	if err != nil {
		return nil, core.NewToolError("scheduler", fmt.Sprintf("create failed: %s", err.Error()))
	}

	return textResult(fmt.Sprintf("Created job '%s' (%s)\nSchedule: %s\nProject: %s\nMessage: %s",
		name, id, scheduleType, project, message)), nil
}

func (t *SchedulerTool) actionEdit(args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("scheduler", "missing 'name' for edit")
	}
	job := t.backend.FindJobByNameOrID(name)
	if job == nil {
		return nil, core.NewToolError("scheduler", fmt.Sprintf("job '%s' not found", name))
	}

	var enabled *bool
	if v, ok := args["enabled"].(bool); ok {
		enabled = &v
	}

	scheduleType := ""
	cronExpr := base.StringArgDefault(args, "cronExpr", "")
	everySeconds := int64(0)
	atTime := base.StringArgDefault(args, "atTime", "")
	if cronExpr != "" {
		scheduleType = "cron"
	} else if v, ok := args["everySeconds"].(float64); ok && v > 0 {
		scheduleType = "every"
		everySeconds = int64(v)
	} else if atTime != "" {
		scheduleType = "at"
	}

	err := t.backend.UpdateJobParams(
		job.ID,
		base.StringArgDefault(args, "name", ""),
		base.StringArgDefault(args, "message", ""),
		base.StringArgDefault(args, "project", ""),
		base.StringArgDefault(args, "model", ""),
		base.StringArgDefault(args, "description", ""),
		scheduleType,
		cronExpr,
		atTime,
		everySeconds,
		enabled,
	)
	if err != nil {
		return nil, core.NewToolError("scheduler", fmt.Sprintf("edit failed: %s", err.Error()))
	}

	return textResult(fmt.Sprintf("Updated job '%s'", name)), nil
}

func (t *SchedulerTool) actionDelete(args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("scheduler", "missing 'name' for delete")
	}
	job := t.backend.FindJobByNameOrID(name)
	if job == nil {
		return nil, core.NewToolError("scheduler", fmt.Sprintf("job '%s' not found", name))
	}

	if err := t.backend.DeleteJob(job.ID); err != nil {
		return nil, core.NewToolError("scheduler", fmt.Sprintf("delete failed: %s", err.Error()))
	}

	return textResult(fmt.Sprintf("Deleted job '%s' (%s)", job.Name, job.ID)), nil
}

func (t *SchedulerTool) actionTrigger(args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("scheduler", "missing 'name' for trigger")
	}
	job := t.backend.FindJobByNameOrID(name)
	if job == nil {
		return nil, core.NewToolError("scheduler", fmt.Sprintf("job '%s' not found", name))
	}

	if err := t.backend.TriggerJob(job.ID); err != nil {
		return nil, core.NewToolError("scheduler", fmt.Sprintf("trigger failed: %s", err.Error()))
	}

	return textResult(fmt.Sprintf("Triggered job '%s' (%s)", job.Name, job.ID)), nil
}

func (t *SchedulerTool) actionRuns(args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	var jobID string
	if name != "" {
		job := t.backend.FindJobByNameOrID(name)
		if job != nil {
			jobID = job.ID
		}
	}

	runs := t.backend.ListRunsInfo(jobID, 10)
	if len(runs) == 0 {
		label := "all jobs"
		if name != "" {
			label = name
		}
		return textResult(fmt.Sprintf("No run history for %s.", label)), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Recent Runs (%d):", len(runs)))
	for _, r := range runs {
		ts := time.UnixMilli(r.Ts).Format("Jan 02 15:04:05")
		output := r.Summary
		if output == "" && r.Error != "" {
			output = "ERROR: " + r.Error
		}
		lines = append(lines, fmt.Sprintf("  %-20s  %-8s  %s", ts, r.Status, truncate(output, 80)))
	}

	return textResult(strings.Join(lines, "\n")), nil
}

// ── Helpers ──────────────────────────────────────────────────────────────

func textResult(text string) map[string]any {
	return map[string]any{"content": []map[string]any{
		{"type": "text", "text": text},
	}}
}

func formatJobSchedule(j *JobInfo) string {
	switch j.Schedule {
	case "cron":
		return "cron: " + j.CronExpr
	case "every":
		return "every: " + formatDuration(j.EverySeconds*1000)
	case "at":
		return "at: " + j.AtTime
	case "manual":
		return "manual"
	default:
		return j.Schedule
	}
}

func formatDuration(ms int64) string {
	s := ms / 1000
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%dm", s/60)
	}
	if s < 86400 {
		return fmt.Sprintf("%dh", s/3600)
	}
	return fmt.Sprintf("%dd", s/86400)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "..."
}
