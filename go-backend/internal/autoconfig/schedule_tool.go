package autoconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// ScheduleTool lets the AI manage scheduled jobs through the ArtifactStore contract.
// Implements core.Tool — registered as a CTO tool.
// Replaces the old schedulertool.SchedulerTool with a contract-adherent version.
type ScheduleTool struct {
	store *ScheduleStore
}

// NewScheduleTool creates a schedule management tool.
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
	result, err := t.store.List(ctx)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (t *ScheduleTool) show(ctx context.Context, name string) (any, error) {
	result, err := t.store.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return result, nil
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

	result, err := t.store.Put(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (t *ScheduleTool) update(ctx context.Context, name string, args map[string]any) (any, error) {
	result, err := t.store.Put(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (t *ScheduleTool) delete(ctx context.Context, name string) (any, error) {
	if err := t.store.Delete(ctx, name); err != nil {
		return nil, err
	}
	return TextResult(fmt.Sprintf("Schedule %q deleted.", name)), nil
}

func (t *ScheduleTool) trigger(ctx context.Context, name string) (any, error) {
	if err := t.store.Backend().TriggerJob(name); err != nil {
		return nil, core.NewToolError("manage_schedule", fmt.Sprintf("trigger failed: %s", err.Error()))
	}
	return TextResult(fmt.Sprintf("Triggered schedule %q.", name)), nil
}

func (t *ScheduleTool) runs(ctx context.Context, name string) (any, error) {
	// TriggerJob is on the Backend, not the store. We use name not ID.
	backend := t.store.Backend()
	job := backend.FindJobByNameOrID(name)
	var jobID string
	if job != nil {
		jobID = job.ID
	}
	runsList := backend.ListRunsInfo(jobID, 10)

	if len(runsList) == 0 {
		return map[string]any{
			"runs":  []any{},
			"count": 0,
		}, nil
	}

	type runEntry struct {
		Timestamp string `json:"timestamp"`
		Status    string `json:"status"`
		Summary   string `json:"summary,omitempty"`
		Error     string `json:"error,omitempty"`
		JobName   string `json:"jobName,omitempty"`
	}

	entries := make([]runEntry, 0, len(runsList))
	for _, r := range runsList {
		summary := r.Summary
		if summary == "" && r.Error != "" {
			summary = "ERROR: " + r.Error
		}
		entries = append(entries, runEntry{
			Timestamp: time.UnixMilli(r.Ts).Format(time.RFC3339),
			Status:    r.Status,
			Summary:   truncate(summary, 120),
			Error:     r.Error,
			JobName:   r.JobName,
		})
	}

	return map[string]any{
		"runs":  entries,
		"count": len(entries),
	}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// Ensure compile-time check that ScheduleTool implements core.Tool.
var _ core.Tool = (*ScheduleTool)(nil)
