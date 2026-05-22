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
	RunDelegate(ctx context.Context, task, instructions string, toolNames []string, maxRounds int, temperature float32, modelID string, sandboxTier string) (map[string]any, error)
	RunDelegateTracked(ctx context.Context, task, instructions, agentName string, toolNames []string, maxRounds int, temperature float32, modelID string, sandboxTier string, delegatesTo []string) (map[string]any, error)
	RunDelegateAsync(ctx context.Context, taskID, task, instructions, agentName string, toolNames []string, maxRounds int, temperature float32, modelID string) (map[string]any, error)
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

// RoleProvider returns the current role map (may include JIT workers).
type RoleProvider func() map[string]*common.AgentRole

// NameProvider returns the current list of valid agent names.
type NameProvider func() []string

// resolveRole checks if instructions matches a role name from config/workers/.
// If it does, returns the role's prompt, tools, and defaults.
// If not, returns the raw instructions as-is (custom delegation).
// mcpResolver is called to expand mcp_servers entries into concrete tool names.
// roleMap is checked first (org-specific roles), then kernel defaults.
// The 6th return value is the division path (non-empty = division head).
// The 7th return value is the sandbox tier ("" = isolated/default).
func resolveRole(instructions string, toolNames []string, maxRounds int, temperature float32, mcpResolver MCPResolver, roleMap map[string]*common.AgentRole) (string, []string, int, float32, string, string, string, []string) {
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
		return prompt, tools, rounds, temp, role.Model, role.Division, role.SandboxTier, role.DelegatesTo
	}
	return instructions, toolNames, maxRounds, temperature, "", "", "", nil
}

// DelegateToTool implements core.Tool for synchronous sub-agent delegation.
// Returns an agent_ref and file changes for continuation/review.
type DelegateToTool struct {
	runner        DelegateRunner
	mcpResolver   MCPResolver
	roleProvider  RoleProvider
	nameProvider  NameProvider
}

func NewDelegateToTool(r DelegateRunner, mcpResolver MCPResolver, roleMap map[string]*common.AgentRole, validAgentNames []string) *DelegateToTool {
	// Wrap static args in provider functions for backwards compat
	staticRoles := roleMap
	staticNames := validAgentNames
	return &DelegateToTool{
		runner:       r,
		mcpResolver:  mcpResolver,
		roleProvider: func() map[string]*common.AgentRole { return staticRoles },
		nameProvider: func() []string { return staticNames },
	}
}

// NewDelegateToToolDynamic creates a DelegateToTool with dynamic providers.
// The providers are called on each Execute/Schema call, enabling JIT workers.
func NewDelegateToToolDynamic(r DelegateRunner, mcpResolver MCPResolver, roleProvider RoleProvider, nameProvider NameProvider) *DelegateToTool {
	return &DelegateToTool{runner: r, mcpResolver: mcpResolver, roleProvider: roleProvider, nameProvider: nameProvider}
}

func (t *DelegateToTool) Name() string { return "delegate_to" }
func (t *DelegateToTool) Description() string {
	return "Delegate a task to a specialized agent. The agent runs autonomously — you CANNOT communicate with it during execution. Your 'task' field is the agent's ONLY context beyond its role training.\n\nYour task brief MUST include:\n1. GOAL — exactly what to accomplish (not vague direction)\n2. CONTEXT — all file paths, URLs, names, values, error messages the agent needs\n3. STEPS — numbered list for multi-step tasks\n4. EXPECTED OUTPUT — file path, format, or specific data to return\n5. VERIFICATION — build command, test command, or how to confirm success\n\nBAD:  \"Fix the login bug\"\nGOOD: \"In /sandbox/workspace/go-backend/internal/handlers/auth.go, the Login handler returns 500 when email is empty (line 47). Add validation to return 400 with {\"error\": \"email required\"}. Run `go test ./internal/handlers/...` to verify.\"\n\nON FAILURE: Re-delegate immediately with the FULL error output pasted into the CONTEXT section. Do NOT paraphrase the error — paste it verbatim. Add what went wrong and any fixes you've identified. Maximum 2 retries. Returns result and file changes. Use delegate_revert to undo if wrong."
}

func (t *DelegateToTool) Schema() json.RawMessage {
	enumJSON := "[]"
	if t.nameProvider != nil {
		names := t.nameProvider()
		if len(names) > 0 {
			encoded, _ := json.Marshal(names)
			enumJSON = string(encoded)
		}
	}
	schema := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"task": {"type": "string", "description": "COMPLETE task brief. The agent has NO other context besides its role training. Include: (1) GOAL — specific outcome, (2) CONTEXT — all paths, URLs, names, errors, current state, (3) STEPS — numbered for multi-step tasks, (4) EXPECTED OUTPUT — file path and format for results, (5) VERIFICATION — build/test commands or how to confirm success. A task brief under 3 sentences is almost certainly too vague."},
			"role": {"type": "string", "description": "Agent role to assign. Each role has specialized tools and training.", "enum": %s},
			"tools": {"type": "array", "items": {"type": "string"}, "description": "Tool names the sub-agent can use (optional if using a role name)"},
			"max_rounds": {"type": "integer", "description": "Maximum tool rounds (default: from role or 15)"},
			"temperature": {"type": "number", "description": "Temperature for generation (default: from role or 0.4)"}
		},
		"required": ["task", "role"]
	}`, enumJSON)
	return json.RawMessage(schema)
}

func (t *DelegateToTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	task, _ := args["task"].(string)
	role, _ := args["role"].(string)
	if role == "" {
		role, _ = args["instructions"].(string) // backwards compat
	}

	if task == "" {
		task, _ = args["step"].(string)
	}
	if task == "" {
		return nil, core.NewToolError("delegate_to", "missing required parameter 'task'")
	}
	if role == "" {
		return nil, core.NewToolError("delegate_to", "missing required parameter 'role'")
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
	roleMap := t.roleProvider()
	resolvedInstructions, resolvedTools, resolvedRounds, resolvedTemp, resolvedModel, division, sandboxTier, delegatesTo := resolveRole(role, toolNames, maxRounds, temperature, t.mcpResolver, roleMap)

	// Division head: delegate to a full sub-orchestrator
	if division != "" {
		return t.runner.RunDivisionDelegate(ctx, task, division, resolvedModel)
	}

	if len(resolvedTools) == 0 {
		return nil, core.NewToolError("delegate_to", "no tools specified and role '"+role+"' has no default tools")
	}

	// Use tracked delegation — returns agent_ref + file changes
	// Pass role name as agentName for correct SSE event labeling
	agentName := role
	return t.runner.RunDelegateTracked(ctx, task, resolvedInstructions, agentName, resolvedTools, resolvedRounds, resolvedTemp, resolvedModel, sandboxTier, delegatesTo)
}

// DelegateContinueTool sends feedback to an existing sub-agent for continuation.
// The subagent keeps its session (memory of what it already tried) and receives
// targeted feedback instead of starting from scratch.
type DelegateContinueTool struct {
	runner DelegateRunner
}

func NewDelegateContinueTool(r DelegateRunner) *DelegateContinueTool {
	return &DelegateContinueTool{runner: r}
}

func (t *DelegateContinueTool) Name() string { return "delegate_continue" }
func (t *DelegateContinueTool) Description() string {
	return "Send feedback to a still-running delegated agent. Only works if the agent is still active. If the agent has already completed, use delegate_to to start a new delegation."
}

func (t *DelegateContinueTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"agent_ref": {"type": "string", "description": "The agent reference returned by delegate_to"},
			"feedback": {"type": "string", "description": "Targeted feedback for the agent. Be specific about what to change or fix."}
		},
		"required": ["agent_ref", "feedback"]
	}`)
}

func (t *DelegateContinueTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	agentRef, _ := args["agent_ref"].(string)
	feedback, _ := args["feedback"].(string)

	if agentRef == "" {
		return nil, core.NewToolError("delegate_continue", "missing required parameter 'agent_ref'")
	}
	if feedback == "" {
		return nil, core.NewToolError("delegate_continue", "missing required parameter 'feedback'")
	}

	tracker, ok := t.runner.(interface {
		RunDelegateContinue(ctx context.Context, agentRef, feedback string) (map[string]any, error)
	})
	if !ok {
		return nil, core.NewToolError("delegate_continue", "runner does not support continuation")
	}

	return tracker.RunDelegateContinue(ctx, agentRef, feedback)
}

// DelegateRevertTool reverts file changes made by a sub-agent and releases its session.
type DelegateRevertTool struct {
	runner DelegateRunner
}

func NewDelegateRevertTool(r DelegateRunner) *DelegateRevertTool {
	return &DelegateRevertTool{runner: r}
}

func (t *DelegateRevertTool) Name() string { return "delegate_revert" }
func (t *DelegateRevertTool) Description() string {
	return "Revert all file changes made by a completed sub-agent. Use when delegated work is wrong and needs to be undone."
}

func (t *DelegateRevertTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"agent_ref": {"type": "string", "description": "The agent reference returned by delegate_to"}
		},
		"required": ["agent_ref"]
	}`)
}

func (t *DelegateRevertTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	agentRef, _ := args["agent_ref"].(string)
	if agentRef == "" {
		return nil, core.NewToolError("delegate_revert", "missing required parameter 'agent_ref'")
	}

	tracker, ok := t.runner.(interface {
		RevertAgent(ctx context.Context, agentRef string) (map[string]any, error)
	})
	if !ok {
		return nil, core.NewToolError("delegate_revert", "runner does not support revert")
	}

	return tracker.RevertAgent(ctx, agentRef)
}

// DelegateAsyncTool implements core.Tool for async sub-agent delegation.
type DelegateAsyncTool struct {
	runner        DelegateRunner
	mcpResolver   MCPResolver
	roleProvider  RoleProvider
	nameProvider  NameProvider
}

func NewDelegateAsyncTool(r DelegateRunner, mcpResolver MCPResolver, roleMap map[string]*common.AgentRole, validAgentNames []string) *DelegateAsyncTool {
	staticRoles := roleMap
	staticNames := validAgentNames
	return &DelegateAsyncTool{
		runner:       r,
		mcpResolver:  mcpResolver,
		roleProvider: func() map[string]*common.AgentRole { return staticRoles },
		nameProvider: func() []string { return staticNames },
	}
}

// NewDelegateAsyncToolDynamic creates a DelegateAsyncTool with dynamic providers.
func NewDelegateAsyncToolDynamic(r DelegateRunner, mcpResolver MCPResolver, roleProvider RoleProvider, nameProvider NameProvider) *DelegateAsyncTool {
	return &DelegateAsyncTool{runner: r, mcpResolver: mcpResolver, roleProvider: roleProvider, nameProvider: nameProvider}
}

func (t *DelegateAsyncTool) Name() string { return "delegate_async" }
func (t *DelegateAsyncTool) Description() string {
	return "Launch an agent in the background for parallel work. The agent runs autonomously — same task brief rules as delegate_to. Write a COMPLETE task brief with goal, context, steps, expected output, and verification. Use collect_results to wait for completion."
}

func (t *DelegateAsyncTool) Schema() json.RawMessage {
	enumJSON := "[]"
	if t.nameProvider != nil {
		names := t.nameProvider()
		if len(names) > 0 {
			encoded, _ := json.Marshal(names)
			enumJSON = string(encoded)
		}
	}
	schema := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"task_id": {"type": "string", "description": "Unique ID for this async task"},
			"task": {"type": "string", "description": "COMPLETE task brief. The agent has NO other context besides its role training. Include: (1) GOAL — specific outcome, (2) CONTEXT — all paths, URLs, names, errors, current state, (3) STEPS — numbered for multi-step tasks, (4) EXPECTED OUTPUT — file path and format for results, (5) VERIFICATION — build/test commands or how to confirm success. A task brief under 3 sentences is almost certainly too vague."},
			"role": {"type": "string", "description": "Agent role to assign. Each role has specialized tools and training.", "enum": %s},
			"tools": {"type": "array", "items": {"type": "string"}, "description": "Tool names (optional if using a role name)"}
		},
		"required": ["task_id", "task", "role"]
	}`, enumJSON)
	return json.RawMessage(schema)
}

func (t *DelegateAsyncTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	taskID, _ := args["task_id"].(string)
	task, _ := args["task"].(string)
	role, _ := args["role"].(string)
	if role == "" {
		role, _ = args["instructions"].(string) // backwards compat
	}

	if taskID == "" || task == "" || role == "" {
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

	maxRounds := 15
	if v, ok := args["max_rounds"].(float64); ok && v > 0 {
		maxRounds = int(v)
	}
	temperature := float32(0.4)
	if v, ok := args["temperature"].(float64); ok {
		temperature = float32(v)
	}

	// Resolve role name → prompt + defaults
	roleMap := t.roleProvider()
	resolvedInstructions, resolvedTools, resolvedRounds, resolvedTemp, resolvedModel, _, _, _ := resolveRole(role, toolNames, maxRounds, temperature, t.mcpResolver, roleMap)

	if len(resolvedTools) == 0 {
		return nil, core.NewToolError("delegate_async", "no tools specified and role '"+role+"' has no default tools")
	}

	return t.runner.RunDelegateAsync(ctx, taskID, task, resolvedInstructions, role, resolvedTools, resolvedRounds, resolvedTemp, resolvedModel)
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
