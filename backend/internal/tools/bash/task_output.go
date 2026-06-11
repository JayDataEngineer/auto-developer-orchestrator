package bash

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// TaskOutputTool retrieves output from background tasks.
// This is the "wait until task is done (Esc to continue)" tool the AI uses.
type TaskOutputTool struct {
	taskMgr *core.TaskManager
}

// NewTaskOutputTool creates a new task_output tool.
func NewTaskOutputTool(taskMgr *core.TaskManager) *TaskOutputTool {
	return &TaskOutputTool{taskMgr: taskMgr}
}

func (t *TaskOutputTool) Name() string { return "task_output" }

func (t *TaskOutputTool) Description() string {
	return "Get the output of a background task. Use block=true to wait for completion, or block=false to check current status. Use this to retrieve results from commands run with run_in_background=true."
}

func (t *TaskOutputTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task_id": {
				"type": "string",
				"description": "The background task ID (returned by bash with run_in_background=true)"
			},
			"block": {
				"type": "boolean",
				"default": false,
				"description": "Wait for the task to complete before returning. If false, returns current status immediately."
			},
			"timeout": {
				"type": "integer",
				"default": 300,
				"description": "Maximum seconds to wait if blocking (default: 300s / 5 minutes)"
			}
		},
		"required": ["task_id"]
	}`)
}

func (t *TaskOutputTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return nil, core.NewToolError("task_output", "missing required parameter 'task_id'")
	}

	block, _ := args["block"].(bool)
	timeoutSec := 300
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		timeoutSec = int(v)
	}

	// Non-blocking: return current status
	if !block {
		status, err := t.taskMgr.Status(taskID)
		if err != nil {
			return nil, err
		}
		output, _ := t.taskMgr.GetOutput(taskID)
		return map[string]any{
			"task_id":    status.ID,
			"status":     string(status.Status),
			"output":     output,
			"exit_code":  status.ExitCode,
			"duration":   status.Duration,
			"error":      status.Error,
		}, nil
	}

	// Blocking: wait for completion or context cancellation
	result, err := t.taskMgr.Wait(taskID, time.Duration(timeoutSec)*time.Second)
	if err != nil {
		// Context cancelled (user pressed Esc) or timeout
		status, _ := t.taskMgr.Status(taskID)
		if status != nil {
			output, _ := t.taskMgr.GetOutput(taskID)
			return map[string]any{
				"task_id":      taskID,
				"status":       string(status.Status),
				"output":       output,
				"message":      fmt.Sprintf("Wait cancelled — task %s is still running", taskID),
				"wait_expired": true,
			}, nil
		}
		return nil, err
	}

	output, _ := t.taskMgr.GetOutput(taskID)
	return map[string]any{
		"task_id":   result.ID,
		"status":    string(result.Status),
		"output":    output,
		"exit_code": result.ExitCode,
		"duration":  result.Duration,
		"error":     result.Error,
	}, nil
}
