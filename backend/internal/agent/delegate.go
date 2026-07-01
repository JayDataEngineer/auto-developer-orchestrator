package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// RoleLookup resolves a role name to its loop config. Implemented by the
// dispatch layer (main.go wiring) — kept as an interface here so the
// delegate tool can be tested without spinning up the whole org system.
type RoleLookup interface {
	// Role returns the named role's loop config, or nil/false if unknown.
	// The returned config is fully populated (prompt, tools, etc.) — the
	// caller just constructs a Loop and Runs it.
	Role(name string) (RoleConfig, bool)
}

// RoleConfig mirrors the fields needed to construct a child Loop. The
// dispatch layer translates org.Role → RoleConfig when wiring.
type RoleConfig struct {
	Name       string
	Prompt     string
	Tools      []core.OpenAITool
	MaxRounds  int
	MaxTokens  int
	Thinking   bool
}

// DelegateTool implements the `delegate_to` MCP tool. From the CTO's view
// it looks synchronous: call it with (role, task), get back the employee's
// result string. Internally it constructs a child Loop and Runs it to
// completion on the same executor + provider.
//
// Recursion safety: the child loop's tool whitelist MUST NOT include
// `delegate_to` (enforced by the wiring layer). Hard depth cap is also
// enforced here via context-local recursion counter — but the primary
// mechanism is the whitelist.
//
// Observer propagation: taskID, role, chat, and tool flow from the parent
// CTO loop into every child loop the delegate tool spawns. The child's
// recorded events are stamped with the role name (not "cto") so the
// history sidecar can correlate which agent did what in a delegation chain.
type DelegateTool struct {
	lookup   RoleLookup
	provider core.LLMProvider
	executor core.ToolExecutor
	taskID   string
	chat     core.ChatObserver
	tool     core.ToolObserver
}

// NewDelegateTool wires the tool with its dependencies. lookup, prov, and
// exec are required — the tool cannot delegate without them. taskID, chat,
// and tool propagate into every child loop so observer events from
// delegated sub-tasks correlate to the parent dispatch task. Nil observers
// + empty taskID are valid (children simply don't fire events).
func NewDelegateTool(
	lookup RoleLookup,
	prov core.LLMProvider,
	exec core.ToolExecutor,
	taskID string,
	chat core.ChatObserver,
	tool core.ToolObserver,
) *DelegateTool {
	return &DelegateTool{
		lookup:   lookup,
		provider: prov,
		executor: exec,
		taskID:   taskID,
		chat:     chat,
		tool:     tool,
	}
}

func (t *DelegateTool) Name() string { return "delegate_to" }

func (t *DelegateTool) Description() string {
	return "Delegate a sub-task to a specialist role (employee). The role " +
		"runs its own agent loop with its own system prompt + tool whitelist. " +
		"Returns the role's final response text. Use this to break a large " +
		"task into specialized sub-tasks — the CTO coordinates, employees execute."
}

func (t *DelegateTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"role": {
				"type": "string",
				"description": "The role name to delegate to (must be declared in this org's [[roles]] list)"
			},
			"task": {
				"type": "string",
				"description": "The sub-task description. Be specific — the role sees only this, not the parent conversation."
			}
		},
		"required": ["role", "task"],
		"additionalProperties": false
	}`)
}

// Execute runs the child loop and returns its result. Errors from the child
// loop (ErrMaxRounds, provider failures) become tool errors — surfaced to
// the parent CTO as part of the tool result body so the CTO can react.
func (t *DelegateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	roleName, _ := args["role"].(string)
	task, _ := args["task"].(string)
	if roleName == "" {
		return nil, core.NewToolError("delegate_to", "missing required parameter 'role'")
	}
	if task == "" {
		return nil, core.NewToolError("delegate_to", "missing required parameter 'task'")
	}

	role, ok := t.lookup.Role(roleName)
	if !ok {
		return nil, core.NewToolError("delegate_to",
			fmt.Sprintf("unknown role %q — check list_orgs for available roles", roleName))
	}

	loop, err := NewLoop(LoopConfig{
		Provider:     t.provider,
		Executor:     t.executor,
		SystemPrompt: role.Prompt,
		Tools:        role.Tools,
		MaxRounds:    role.MaxRounds,
		MaxTokens:    role.MaxTokens,
		Thinking:     role.Thinking,
		TaskID:       t.taskID,
		Role:         roleName,
		ChatObserver: t.chat,
		ToolObserver: t.tool,
	})
	if err != nil {
		return nil, fmt.Errorf("delegate_to: build child loop: %w", err)
	}

	result, err := loop.Run(ctx, task)
	if err != nil {
		// Surface as text result, not Go error — the parent CTO should see
		// WHY the delegation failed and decide whether to retry or escalate.
		// Returning an error here would kill the parent loop.
		return map[string]any{
			"role":   roleName,
			"ok":     false,
			"error":  err.Error(),
			"result": "",
		}, nil
	}
	return map[string]any{
		"role":   roleName,
		"ok":     true,
		"result": result,
	}, nil
}

// FilterTools returns the subset of `tools` whose Name matches an entry in
// `whitelist`. Used by the dispatch layer to enforce role-level tool
// isolation. If whitelist is empty, all tools are returned (open policy —
// the role has the same tools as the CTO).
func FilterTools(tools []core.OpenAITool, whitelist []string) []core.OpenAITool {
	if len(whitelist) == 0 {
		return tools
	}
	allowed := make(map[string]struct{}, len(whitelist))
	for _, n := range whitelist {
		allowed[n] = struct{}{}
	}
	out := make([]core.OpenAITool, 0, len(tools))
	for _, t := range tools {
		if _, ok := allowed[t.Function.Name]; ok {
			out = append(out, t)
		}
	}
	return out
}

// AssertNoDelegateInWhitelist is a sanity check the dispatch layer calls
// when wiring roles. Recursion via delegate_to is structurally prevented
// (child loop would need delegate_to in its tool list to recurse), but
// this guard makes misconfiguration loud rather than allowing infinite
// recursion at runtime. Returns an error naming the offending role.
func AssertNoDelegateInWhitelist(roleName string, whitelist []string) error {
	if slices.Contains(whitelist, "delegate_to") {
		return errors.New("role " + roleName + ": delegate_to must not appear in its own tool whitelist (would allow infinite recursion)")
	}
	return nil
}
