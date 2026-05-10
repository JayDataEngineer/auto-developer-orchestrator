package orchestration

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// DelegateRunner creates and runs sub-agents for delegate_to/delegate_async.
type DelegateRunner interface {
	RunDelegate(ctx context.Context, task, instructions string, toolNames []string, maxRounds int, temperature float32, modelID string) (map[string]any, error)
	RunDelegateAsync(ctx context.Context, taskID, task, instructions string, toolNames []string) (map[string]any, error)
	CollectAsyncResults(ctx context.Context) (map[string]any, error)
	RunDivisionDelegate(ctx context.Context, task, divisionPath, modelID string) (map[string]any, error)
}

// MCPResolver resolves an MCP server prefix to a list of tool names.
// nil means no MCP servers configured.
type MCPResolver func(prefix string) []string

// ModelResolver resolves a model ID to a core.LLMProvider.
// Returns nil if the model can't be resolved (caller falls back to default).
type ModelResolver func(modelID string) core.LLMProvider

// ProviderFactory creates a fresh LLMProvider for each sub-agent invocation.
// Each call returns an isolated provider with its own session/slot on llama-server.
type ProviderFactory func() core.LLMProvider

// resolveRole checks if instructions matches a role name from config/roles/.
// If it does, returns the role's prompt, tools, and defaults.
// If not, returns the raw instructions as-is (custom delegation).
// mcpResolver is called to expand mcp_servers entries into concrete tool names.
// roleMap is checked first (org-specific roles), then kernel defaults.
// The 6th return value is the division path (non-empty = division head).
func resolveRole(instructions string, toolNames []string, maxRounds int, temperature float32, mcpResolver MCPResolver, roleMap map[string]*common.AgentRole) (string, []string, int, float32, string, string) {
	// Try org-specific roles first, then kernel defaults
	var role *common.AgentRole
	if roleMap != nil {
		role = roleMap[instructions]
	}
	if role == nil {
		role = common.GetAgentRole(instructions)
	}
	if role != nil {
		prompt := role.Prompt
		tools := toolNames
		if len(tools) == 0 {
			tools = append([]string{}, role.Tools...)
			// Expand MCPServers → tool names
			if mcpResolver != nil {
				for _, server := range role.MCPServers {
					serverTools := mcpResolver(server)
					tools = append(tools, serverTools...)
				}
			}
		}
		rounds := maxRounds
		if rounds == 15 && role.MaxRounds != 15 {
			rounds = role.MaxRounds
		}
		temp := temperature
		if temp == 0.4 && role.Temperature != 0.4 {
			temp = role.Temperature
		}
		return prompt, tools, rounds, temp, role.Model, role.Division
	}
	return instructions, toolNames, maxRounds, temperature, "", ""
}

// DelegateToTool implements core.Tool for synchronous sub-agent delegation.
type DelegateToTool struct {
	runner           DelegateRunner
	mcpResolver      MCPResolver
	roleMap          map[string]*common.AgentRole
	validAgentNames  []string
}

func NewDelegateToTool(r DelegateRunner, mcpResolver MCPResolver, roleMap map[string]*common.AgentRole, validAgentNames []string) *DelegateToTool {
	return &DelegateToTool{runner: r, mcpResolver: mcpResolver, roleMap: roleMap, validAgentNames: validAgentNames}
}

func (t *DelegateToTool) Name() string { return "delegate_to" }
func (t *DelegateToTool) Description() string {
	return "Delegate a task to an employee. Use a role name as instructions, or write custom instructions."
}

func (t *DelegateToTool) Schema() json.RawMessage {
	enumJSON := "[]"
	if len(t.validAgentNames) > 0 {
		names, _ := json.Marshal(t.validAgentNames)
		enumJSON = string(names)
	}
	schema := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"task": {"type": "string", "description": "Description of the task for the sub-agent"},
			"instructions": {"type": "string", "description": "Employee role name or custom instructions", "enum": %s},
			"tools": {"type": "array", "items": {"type": "string"}, "description": "Tool names the sub-agent can use (optional if using a role name)"},
			"max_rounds": {"type": "integer", "description": "Maximum tool rounds (default: from role or 15)"},
			"temperature": {"type": "number", "description": "Temperature for generation (default: from role or 0.4)"}
		},
		"required": ["task", "instructions"]
	}`, enumJSON)
	return json.RawMessage(schema)
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

	maxRounds := 15
	if v, ok := args["max_rounds"].(float64); ok && v > 0 {
		maxRounds = int(v)
	}
	temperature := float32(0.4)
	if v, ok := args["temperature"].(float64); ok {
		temperature = float32(v)
	}

	// Resolve role name → prompt + defaults
	resolvedInstructions, resolvedTools, resolvedRounds, resolvedTemp, resolvedModel, division := resolveRole(instructions, toolNames, maxRounds, temperature, t.mcpResolver, t.roleMap)

	// Division head: delegate to a full sub-orchestrator
	if division != "" {
		return t.runner.RunDivisionDelegate(ctx, task, division, resolvedModel)
	}

	if len(resolvedTools) == 0 {
		return nil, core.NewToolError("delegate_to", "no tools specified and role '"+instructions+"' has no default tools")
	}

	return t.runner.RunDelegate(ctx, task, resolvedInstructions, resolvedTools, resolvedRounds, resolvedTemp, resolvedModel)
}

// DelegateAsyncTool implements core.Tool for async sub-agent delegation.
type DelegateAsyncTool struct {
	runner           DelegateRunner
	mcpResolver      MCPResolver
	roleMap          map[string]*common.AgentRole
	validAgentNames  []string
}

func NewDelegateAsyncTool(r DelegateRunner, mcpResolver MCPResolver, roleMap map[string]*common.AgentRole, validAgentNames []string) *DelegateAsyncTool {
	return &DelegateAsyncTool{runner: r, mcpResolver: mcpResolver, roleMap: roleMap, validAgentNames: validAgentNames}
}

func (t *DelegateAsyncTool) Name() string { return "delegate_async" }
func (t *DelegateAsyncTool) Description() string {
	return "Launch an employee in the background. Use a role name as instructions."
}

func (t *DelegateAsyncTool) Schema() json.RawMessage {
	enumJSON := "[]"
	if len(t.validAgentNames) > 0 {
		names, _ := json.Marshal(t.validAgentNames)
		enumJSON = string(names)
	}
	schema := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"task_id": {"type": "string", "description": "Unique ID for this async task"},
			"task": {"type": "string", "description": "Description of the task"},
			"instructions": {"type": "string", "description": "Employee role name or custom instructions", "enum": %s},
			"tools": {"type": "array", "items": {"type": "string"}, "description": "Tool names (optional if using a role name)"}
		},
		"required": ["task_id", "task", "instructions"]
	}`, enumJSON)
	return json.RawMessage(schema)
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

	// Resolve role name → prompt + defaults
	resolvedInstructions, resolvedTools, _, _, _, _ := resolveRole(instructions, toolNames, 15, 0.4, t.mcpResolver, t.roleMap)

	if len(resolvedTools) == 0 {
		return nil, core.NewToolError("delegate_async", "no tools specified and role '"+instructions+"' has no default tools")
	}

	return t.runner.RunDelegateAsync(ctx, taskID, task, resolvedInstructions, resolvedTools)
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

// PlanTool creates execution plans.
type PlanTool struct{}

func NewPlanTool() *PlanTool { return &PlanTool{} }

func (t *PlanTool) Name() string        { return "create_plan" }
func (t *PlanTool) Description() string { return "Create a multi-step execution plan with optional parallel dependencies" }

func (t *PlanTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"steps": {
				"type": "array",
				"items": {
					"oneOf": [
						{"type": "string"},
						{
							"type": "object",
							"properties": {
								"id": {"type": "string"},
								"task": {"type": "string"},
								"depends_on": {"type": "array", "items": {"type": "string"}}
							},
							"required": ["id", "task"]
						}
					]
				}
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
		return nil, core.NewToolError("create_plan", "mixed step formats")
	}

	if hasStrings {
		var steps []string
		for _, s := range rawSteps {
			if step, ok := s.(string); ok {
				steps = append(steps, step)
			}
		}
		return map[string]any{"steps": steps, "created": true, "count": len(steps)}, nil
	}

	var planSteps []map[string]any
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
		planSteps = append(planSteps, map[string]any{"id": id, "task": task, "depends_on": deps})
	}

	return map[string]any{"steps": planSteps, "created": true, "count": len(planSteps)}, nil
}

// SynthesizeTool signals completion.
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
