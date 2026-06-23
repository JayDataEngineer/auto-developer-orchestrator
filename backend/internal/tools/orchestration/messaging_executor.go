package orchestration

import (
	"context"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// messagingExecutor wraps a parent executor and intercepts the three
// messaging tool calls (send_message / wait_for_message / list_peers),
// dispatching them to per-agent tool implementations that share the
// runner-owned MessageBus. Other tools fall through to the parent.
//
// Sub-agents are constructed per-delegation; rather than mutating the
// parent tool registry for each one, we wrap the executor so each
// sub-agent gets its own (bus, selfID) binding without leaking tools
// into shared registries.
type messagingExecutor struct {
	parent core.ToolExecutor
	tools  map[string]core.Tool
}

// newMessagingExecutor wraps parent with messaging tool dispatch for the
// given (bus, selfID) pair. Returns parent unchanged if bus is nil.
func newMessagingExecutor(parent core.ToolExecutor, bus *MessageBus, selfID string) core.ToolExecutor {
	if bus == nil {
		return parent
	}
	tools := map[string]core.Tool{}
	for _, t := range MessagingTools(bus, selfID) {
		tools[t.Name()] = t
	}
	return &messagingExecutor{parent: parent, tools: tools}
}

// Execute routes messaging tool calls locally; everything else hits parent.
func (m *messagingExecutor) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	if t, ok := m.tools[name]; ok {
		return t.Execute(ctx, args)
	}
	return m.parent.Execute(ctx, name, args)
}

// ToolTimeoutHint delegates to parent so wrapped timeout metadata still
// reaches the agent loop. Messaging tools don't have a meaningful timeout
// hint, but the parent's hint applies to the rest of the toolset.
func (m *messagingExecutor) ToolTimeoutHint(toolName string) time.Duration {
	if h, ok := m.parent.(interface{ ToolTimeoutHint(string) time.Duration }); ok {
		return h.ToolTimeoutHint(toolName)
	}
	return 0
}

// messagingToolSpecs returns the OpenAI tool specs for the three messaging
// tools. Used by prepareDelegation to advertise the tools to the sub-agent's
// model — the implementations are dispatched by messagingExecutor at runtime.
func messagingToolSpecs() []core.OpenAITool {
	out := make([]core.OpenAITool, 0, 3)
	for _, t := range MessagingTools(nil, "template") {
		out = append(out, core.OpenAITool{
			Type: "function",
			Function: core.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return out
}
