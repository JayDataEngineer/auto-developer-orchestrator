package orchestration

import (
	"context"
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// DelegateRunner creates and runs sub-agents for delegate_to/delegate_async.
type DelegateRunner interface {
	RunDelegate(ctx context.Context, task, instructions string, toolNames []string, maxRounds int, temperature float32) (map[string]any, error)
	RunDelegateAsync(ctx context.Context, taskID, task, instructions string, toolNames []string) (map[string]any, error)
	CollectAsyncResults(ctx context.Context) (map[string]any, error)
}

// DelegateToTool implements core.Tool for synchronous sub-agent delegation.
type DelegateToTool struct {
	runner DelegateRunner
}

func NewDelegateToTool(r DelegateRunner) *DelegateToTool {
	return &DelegateToTool{runner: r}
}

func (t *DelegateToTool) Name() string        { return "delegate_to" }
func (t *DelegateToTool) Description() string { return "Create a sub-agent with a restricted tool set to complete a focused task" }

func (t *DelegateToTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {"type": "string", "description": "Description of the task for the sub-agent"},
			"instructions": {"type": "string", "description": "Custom instructions telling the sub-agent how to work"},
			"tools": {"type": "array", "items": {"type": "string"}, "description": "Tool names the sub-agent can use"},
			"max_rounds": {"type": "integer", "description": "Maximum tool rounds (default 15)"},
			"temperature": {"type": "number", "description": "Temperature for generation (default 0.4)"}
		},
		"required": ["task", "instructions", "tools"]
	}`)
}

func (t *DelegateToTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	task, _ := args["task"].(string)
	instructions, _ := args["instructions"].(string)

	if task == "" {
		task, _ = args["step"].(string)
	}
	if task == "" {
		return nil, core.NewToolError("delegate_to", "missing required parameter 'task'")
	}
	if instructions == "" {
		return nil, core.NewToolError("delegate_to", "missing required parameter 'instructions'")
	}

	var toolNames []string
	if raw, ok := args["tools"].([]any); ok {
		for _, t := range raw {
			if name, ok := t.(string); ok {
				toolNames = append(toolNames, name)
			}
		}
	}
	if len(toolNames) == 0 {
		return nil, core.NewToolError("delegate_to", "missing required parameter 'tools'")
	}

	maxRounds := 15
	if v, ok := args["max_rounds"].(float64); ok && v > 0 {
		maxRounds = int(v)
	}
	temperature := float32(0.4)
	if v, ok := args["temperature"].(float64); ok {
		temperature = float32(v)
	}

	return t.runner.RunDelegate(ctx, task, instructions, toolNames, maxRounds, temperature)
}

// DelegateAsyncTool implements core.Tool for async sub-agent delegation.
type DelegateAsyncTool struct {
	runner DelegateRunner
}

func NewDelegateAsyncTool(r DelegateRunner) *DelegateAsyncTool {
	return &DelegateAsyncTool{runner: r}
}

func (t *DelegateAsyncTool) Name() string        { return "delegate_async" }
func (t *DelegateAsyncTool) Description() string { return "Launch a sub-agent in the background and return immediately" }

func (t *DelegateAsyncTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task_id": {"type": "string", "description": "Unique ID for this async task"},
			"task": {"type": "string", "description": "Description of the task"},
			"instructions": {"type": "string", "description": "Custom instructions for the sub-agent"},
			"tools": {"type": "array", "items": {"type": "string"}, "description": "Tool names the sub-agent can use"}
		},
		"required": ["task_id", "task", "instructions", "tools"]
	}`)
}

func (t *DelegateAsyncTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	taskID, _ := args["task_id"].(string)
	task, _ := args["task"].(string)
	instructions, _ := args["instructions"].(string)

	if taskID == "" || task == "" || instructions == "" {
		return nil, core.NewToolError("delegate_async", "missing required parameters")
	}

	var toolNames []string
	if raw, ok := args["tools"].([]any); ok {
		for _, t := range raw {
			if name, ok := t.(string); ok {
				toolNames = append(toolNames, name)
			}
		}
	}

	return t.runner.RunDelegateAsync(ctx, taskID, task, instructions, toolNames)
}

// CollectResultsTool waits for all pending async delegates to complete.
type CollectResultsTool struct {
	runner DelegateRunner
}

func NewCollectResultsTool(r DelegateRunner) *CollectResultsTool {
	return &CollectResultsTool{runner: r}
}

func (t *CollectResultsTool) Name() string        { return "collect_results" }
func (t *CollectResultsTool) Description() string { return "Wait for all pending async delegate tasks to complete and return their results" }

func (t *CollectResultsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *CollectResultsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	return t.runner.CollectAsyncResults(ctx)
}

// PlanTool implements core.Tool for creating execution plans.
// Accepts either:
//   1. String steps: ["task A", "task B", "task C"] — sequential dependencies
//   2. Step objects: [{"id":"init","task":"Setup",},{"id":"build","task":"Build","depends_on":["init"]}]
// The second form produces a dependency graph for parallel execution.
type PlanTool struct{}

func NewPlanTool() *PlanTool { return &PlanTool{} }

func (t *PlanTool) Name() string        { return "create_plan" }
func (t *PlanTool) Description() string { return "Create a multi-step execution plan. Steps can optionally declare dependencies to enable parallel execution." }

func (t *PlanTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"steps": {
				"type": "array",
				"items": {
					"oneOf": [
						{"type": "string", "description": "A step description. Steps without depends_on are sequential."},
						{
							"type": "object",
							"properties": {
								"id": {"type": "string", "description": "Unique identifier for this step"},
								"task": {"type": "string", "description": "What this step should accomplish"},
								"depends_on": {"type": "array", "items": {"type": "string"}, "description": "Step IDs this step depends on"}
							},
							"required": ["id", "task"]
						}
					]
				},
				"description": "Ordered list of steps to execute"
			}
		},
		"required": ["steps"]
	}`)
}

func (t *PlanTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	rawSteps, ok := args["steps"].([]any)
	if !ok || len(rawSteps) == 0 {
		return nil, core.NewToolError("create_plan", "missing required parameter 'steps'")
	}

	// Detect format: string steps vs step objects
	hasObjects := false
	hasStrings := false
	for _, s := range rawSteps {
		if _, isObj := s.(map[string]any); isObj {
			hasObjects = true
		} else if _, isStr := s.(string); isStr {
			hasStrings = true
		}
	}

	if hasObjects && hasStrings {
		return nil, core.NewToolError("create_plan", "mixed step formats: all steps must be strings or all must be objects")
	}

	if hasStrings {
		// Simple sequential steps
		var steps []string
		for _, s := range rawSteps {
			if step, ok := s.(string); ok {
				steps = append(steps, step)
			}
		}
		return map[string]any{
			"steps":    steps,
			"created":  true,
			"parallel": false,
			"count":    len(steps),
		}, nil
	}

	// Object steps with optional dependencies
	type stepObj struct {
		ID        string   `json:"id"`
		Task      string   `json:"task"`
		DependsOn []string `json:"depends_on,omitempty"`
	}

	var planSteps []map[string]any
	var stepObjs []stepObj
	for _, s := range rawSteps {
		obj, ok := s.(map[string]any)
		if !ok {
			continue
		}
		id, _ := obj["id"].(string)
		task, _ := obj["task"].(string)
		if id == "" || task == "" {
			continue
		}

		var deps []string
		if depRaw, ok := obj["depends_on"].([]any); ok {
			for _, d := range depRaw {
				if depID, ok := d.(string); ok {
					deps = append(deps, depID)
				}
			}
		}

		stepObjs = append(stepObjs, stepObj{ID: id, Task: task, DependsOn: deps})
		planSteps = append(planSteps, map[string]any{
			"id":         id,
			"task":       task,
			"depends_on": deps,
		})
	}

	if len(planSteps) == 0 {
		return nil, core.NewToolError("create_plan", "no valid steps provided")
	}

	// Determine if any steps have dependencies (enables parallel execution)
	hasParallel := false
	for _, so := range stepObjs {
		if len(so.DependsOn) > 0 {
			hasParallel = true
			break
		}
	}

	// Find root steps (no dependencies) — these can start immediately
	var roots []string
	for _, so := range stepObjs {
		if len(so.DependsOn) == 0 {
			roots = append(roots, so.ID)
		}
	}

	// Find parallel groups — steps that share the same dependencies
	depGroups := make(map[string][]string)
	for _, so := range stepObjs {
		key := ""
		for _, d := range so.DependsOn {
			key += d + ","
		}
		depGroups[key] = append(depGroups[key], so.ID)
	}

	var parallelGroups [][]string
	for _, group := range depGroups {
		if len(group) > 1 {
			parallelGroups = append(parallelGroups, group)
		}
	}

	return map[string]any{
		"steps":           planSteps,
		"created":         true,
		"parallel":        hasParallel,
		"count":           len(planSteps),
		"root_steps":      roots,
		"parallel_groups": parallelGroups,
	}, nil
}

// SynthesizeTool signals the orchestrator is done and provides the final answer.
type SynthesizeTool struct{}

func NewSynthesizeTool() *SynthesizeTool { return &SynthesizeTool{} }

func (t *SynthesizeTool) Name() string        { return "synthesize" }
func (t *SynthesizeTool) Description() string { return "Signal that the task is complete and provide the final answer" }

func (t *SynthesizeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"conclusion": {"type": "string", "description": "The final conclusion/answer"}
		},
		"required": ["conclusion"]
	}`)
}

func (t *SynthesizeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	conclusion, _ := args["conclusion"].(string)
	return map[string]any{"conclusion": conclusion, "done": true}, nil
}
