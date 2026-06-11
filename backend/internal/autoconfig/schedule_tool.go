package autoconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	schedulertool "github.com/auto-developer-orchestrator/backend/internal/tools/scheduler"
)

// ScheduleTool lets the AI manage scheduled jobs through the ArtifactStore contract.
// Implements core.Tool — registered as a CTO tool.
type ScheduleTool struct {
	store *ScheduleStore
}

func NewScheduleTool(store *ScheduleStore) *ScheduleTool {
	return &ScheduleTool{store: store}
}

func (t *ScheduleTool) Name() string { return "manage_schedule" }

func (t *ScheduleTool) Description() string {
	return `Manage scheduled agent runs. Create, edit, trigger, and monitor cron jobs and interval tasks.

Operations:
- list: Show all schedules
- show: Display a schedule's full configuration
- create: Create a new schedule
- update: Update an existing schedule
- delete: Remove a schedule
- trigger: Manually trigger a schedule now
- runs: Show run history for a schedule`
}

func (t *ScheduleTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["list", "show", "create", "update", "delete", "trigger", "runs"],
				"description": "Operation to perform"
			},
			"name": {
				"type": "string",
				"description": "Schedule name or ID (required for show/update/delete/trigger/runs)"
			},
			"message": {
				"type": "string",
				"description": "Prompt message for the job (required for create)"
			},
			"project": {
				"type": "string",
				"description": "Project name (defaults to current project)"
			},
			"scheduleType": {
				"type": "string",
				"enum": ["cron", "every", "at", "manual"],
				"description": "Schedule type"
			},
			"cronExpr": {
				"type": "string",
				"description": "Cron expression e.g. '0 9 * * *' for daily at 9am"
			},
			"everySeconds": {
				"type": "number",
				"description": "Interval in seconds (for 'every' schedule)"
			},
			"atTime": {
				"type": "string",
				"description": "One-shot time in RFC3339 format (for 'at' schedule)"
			},
			"description": {
				"type": "string",
				"description": "Job description"
			},
			"model": {
				"type": "string",
				"description": "Model override for this job"
			},
			"enabled": {
				"type": "boolean",
				"description": "Whether the job is enabled"
			}
		},
		"required": ["operation"]
	}`)
}

func (t *ScheduleTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	op, _ := args["operation"].(string)
	if op == "" {
		return nil, core.NewToolError("manage_schedule", "missing required parameter 'operation'")
	}

	switch op {
	case "list":
		return t.list(ctx)
	case "show":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_schedule", "show requires 'name'")
		}
		return t.show(ctx, name)
	case "create":
		return t.create(ctx, args)
	case "update":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_schedule", "update requires 'name'")
		}
		return t.update(ctx, name, args)
	case "delete":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_schedule", "delete requires 'name'")
		}
		return t.delete(ctx, name)
	case "trigger":
		name, _ := args["name"].(string)
		if name == "" {
			return nil, core.NewToolError("manage_schedule", "trigger requires 'name'")
		}
		return t.trigger(ctx, name)
	case "runs":
		name, _ := args["name"].(string)
		return t.runs(ctx, name)
	default:
		return nil, core.NewToolError("manage_schedule", fmt.Sprintf("unknown operation: %s", op))
	}
}

func (t *ScheduleTool) list(ctx context.Context) (any, error) {
	jobs := t.store.Backend().ListJobsInfo()
	rows := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, jobToMap(j))
	}
	return map[string]any{
		"operation": "list",
		"jobs":      rows,
		"count":     len(rows),
		"widget": core.WidgetResult{
			Type:  "list",
			Title: fmt.Sprintf("%d scheduled job%s", len(rows), pluralS(len(rows))),
			Icon:  "Calendar",
			Columns: []core.WidgetColumn{
				{Key: "name", Label: "Name", Type: core.WidgetColText},
				{Key: "scheduleType", Label: "Schedule", Type: core.WidgetColBadge},
				{Key: "enabled", Label: "On", Type: core.WidgetColBoolean},
				{Key: "status", Label: "Status", Type: core.WidgetColStatus, ColorMap: map[string]string{
					"running": "text-blue-400", "error": "text-red-400", "idle": "text-muted-foreground",
				}},
				{Key: "lastRunAt", Label: "Last Run", Type: core.WidgetColDate},
			},
			Rows:  rows,
			Empty: "No scheduled jobs",
			Actions: []core.WidgetAction{
				{Label: "Run", Icon: "Play", Method: "POST", URL: "/api/scheduler/{id}/trigger"},
				{Label: "Toggle", Icon: "Pause", Method: "PUT", URL: "/api/scheduler/{id}"},
				{Label: "Delete", Icon: "Trash2", Method: "DELETE", URL: "/api/scheduler/{id}",
					Confirm: "Delete this schedule?", Variant: "destructive"},
			},
		},
	}, nil
}

func (t *ScheduleTool) show(ctx context.Context, name string) (any, error) {
	job := t.store.Backend().FindJobByNameOrID(name)
	if job == nil {
		return nil, fmt.Errorf("schedule %q not found", name)
	}
	item := jobToMap(job)
	return map[string]any{
		"operation": "show",
		"job":       item,
		"widget": core.WidgetResult{
			Type:  "detail",
			Title: job.Name,
			Icon:  "Calendar",
			Columns: []core.WidgetColumn{
				{Key: "scheduleType", Label: "Schedule Type", Type: core.WidgetColBadge},
				{Key: "cronExpr", Label: "Cron", Type: core.WidgetColMono},
				{Key: "enabled", Label: "Enabled", Type: core.WidgetColBoolean},
				{Key: "status", Label: "Status", Type: core.WidgetColStatus, ColorMap: map[string]string{
					"running": "text-blue-400", "error": "text-red-400", "idle": "text-muted-foreground",
				}},
				{Key: "lastRunAt", Label: "Last Run", Type: core.WidgetColDate},
				{Key: "nextRunAt", Label: "Next Run", Type: core.WidgetColDate},
				{Key: "lastRunStatus", Label: "Last Status", Type: core.WidgetColBadge},
				{Key: "durationMs", Label: "Duration", Type: core.WidgetColMono},
				{Key: "project", Label: "Project", Type: core.WidgetColText},
			},
			Item: item,
			Actions: []core.WidgetAction{
				{Label: "Run Now", Icon: "Play", Method: "POST", URL: fmt.Sprintf("/api/scheduler/%s/trigger", job.ID)},
				{Label: "Delete", Icon: "Trash2", Method: "DELETE", URL: fmt.Sprintf("/api/scheduler/%s", job.ID),
					Confirm: fmt.Sprintf("Delete schedule %q?", job.Name), Variant: "destructive"},
			},
		},
	}, nil
}

func (t *ScheduleTool) create(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("manage_schedule", "create requires 'name'")
	}
	message, _ := args["message"].(string)
	if message == "" {
		return nil, core.NewToolError("manage_schedule", "create requires 'message'")
	}

	_, err := t.store.Put(ctx, name, args)
	if err != nil {
		return nil, err
	}
	job := t.store.Backend().FindJobByNameOrID(name)
	item := jobToMap(job)
	return map[string]any{
		"operation": "create",
		"job":       item,
		"message":   fmt.Sprintf("Schedule %q created.", name),
		"widget": core.WidgetResult{
			Type:    "confirm",
			Title:   "Schedule Created",
			Icon:    "CheckCircle",
			Message: fmt.Sprintf("Schedule %q created.", name),
			Item:    item,
		},
	}, nil
}

func (t *ScheduleTool) update(ctx context.Context, name string, args map[string]any) (any, error) {
	_, err := t.store.Put(ctx, name, args)
	if err != nil {
		return nil, err
	}
	job := t.store.Backend().FindJobByNameOrID(name)
	item := jobToMap(job)
	return map[string]any{
		"operation": "update",
		"job":       item,
		"message":   fmt.Sprintf("Schedule %q updated.", name),
		"widget": core.WidgetResult{
			Type:    "confirm",
			Title:   "Schedule Updated",
			Icon:    "CheckCircle",
			Message: fmt.Sprintf("Schedule %q updated.", name),
			Item:    item,
		},
	}, nil
}

func (t *ScheduleTool) delete(ctx context.Context, name string) (any, error) {
	if err := t.store.Delete(ctx, name); err != nil {
		return nil, err
	}
	return map[string]any{
		"operation": "delete",
		"jobName":   name,
		"message":   fmt.Sprintf("Schedule %q deleted.", name),
		"widget": core.WidgetResult{
			Type:    "confirm",
			Title:   "Deleted",
			Icon:    "Trash2",
			Message: fmt.Sprintf("Schedule %q deleted.", name),
		},
	}, nil
}

func (t *ScheduleTool) trigger(ctx context.Context, name string) (any, error) {
	if err := t.store.Backend().TriggerJob(name); err != nil {
		return nil, core.NewToolError("manage_schedule", fmt.Sprintf("trigger failed: %s", err.Error()))
	}
	return map[string]any{
		"operation": "trigger",
		"jobName":   name,
		"message":   fmt.Sprintf("Triggered schedule %q.", name),
		"widget": core.WidgetResult{
			Type:    "confirm",
			Title:   "Triggered",
			Icon:    "Play",
			Message: fmt.Sprintf("Triggered schedule %q.", name),
		},
	}, nil
}

func (t *ScheduleTool) runs(ctx context.Context, name string) (any, error) {
	backend := t.store.Backend()
	job := backend.FindJobByNameOrID(name)
	var jobID string
	if job != nil {
		jobID = job.ID
	}
	runsList := backend.ListRunsInfo(jobID, 10)

	rows := make([]map[string]any, 0, len(runsList))
	for _, r := range runsList {
		summary := r.Summary
		if summary == "" && r.Error != "" {
			summary = "ERROR: " + r.Error
		}
		rows = append(rows, map[string]any{
			"timestamp": time.UnixMilli(r.Ts).Format(time.RFC3339),
			"status":    r.Status,
			"summary":   truncate(summary, 120),
			"error":     r.Error,
			"jobName":   r.JobName,
		})
	}

	return map[string]any{
		"operation": "runs",
		"jobName":   name,
		"runs":      rows,
		"count":     len(rows),
		"widget": core.WidgetResult{
			Type:  "list",
			Title: fmt.Sprintf("Runs for %s", name),
			Icon:  "Clock",
			Columns: []core.WidgetColumn{
				{Key: "status", Label: "Status", Type: core.WidgetColStatus, ColorMap: map[string]string{
					"success": "text-green-400", "error": "text-red-400",
				}},
				{Key: "timestamp", Label: "Time", Type: core.WidgetColDate},
				{Key: "summary", Label: "Summary", Type: core.WidgetColText},
			},
			Rows:  rows,
			Empty: "No runs yet",
		},
	}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func jobToMap(j *schedulertool.JobInfo) map[string]any {
	if j == nil {
		return nil
	}
	return map[string]any{
		"id": j.ID, "name": j.Name, "description": j.Description,
		"project": j.Project, "message": j.Message, "model": j.Model,
		"scheduleType": j.Schedule, "cronExpr": j.CronExpr,
		"everySeconds": j.EverySeconds, "atTime": j.AtTime,
		"enabled": j.Enabled, "status": j.Status,
		"lastRunAt": formatTime(j.LastRunAt), "lastRunStatus": j.LastRunStatus,
		"lastError": j.LastError, "nextRunAt": formatTime(j.NextRunAt),
		"consecutiveErrors": j.ConsecutiveErrors,
		"inputTokens": j.InputTokens, "outputTokens": j.OutputTokens,
		"durationMs": j.DurationMs,
	}
}

var _ core.Tool = (*ScheduleTool)(nil)
