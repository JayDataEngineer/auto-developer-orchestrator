package core

import (
	"context"
	"time"
)

// LoopHook is a lifecycle hook for the agent loop.
// Hooks are injected at agent composition time and run in order.
// Unlike tools, hooks know about the loop's state and can inject messages,
// trigger compaction, detect cycles, etc.
type LoopHook interface {
	// Name returns a human-readable name for logging.
	Name() string

	// OnAgentStart is called when an agent run begins (before the first turn).
	OnAgentStart(ctx context.Context, state *LoopState) error

	// OnBeforeTurn is called before each LLM generation turn.
	// Returns a list of user messages to inject (e.g., goal nudges, cycle warnings).
	OnBeforeTurn(ctx context.Context, state *LoopState) ([]string, error)

	// OnAfterToolCall is called after a tool is executed.
	// Call args: toolName, toolArgs, result content, error.
	OnAfterToolCall(ctx context.Context, state *LoopState, toolName string, args map[string]any, result string, err error) error

	// OnAgentEnd is called when an agent run completes.
	OnAgentEnd(ctx context.Context, state *LoopState) error
}

// LoopState provides hooks access to the agent loop's mutable state.
type LoopState struct {
	SessionID       string
	ProjectDir      string
	SandboxID       string
	Round           int
	ContentLength   int
	ToolResults     []ToolResult
	FailCounts      map[string]int
	ConsecutiveFails int
	TotalInputTokens  int
	TotalOutputTokens int
	TurnInputTokens   int
	TurnOutputTokens  int
	TurnModel         string
	StartedAt       time.Time
}

// NoopHook is a hook that does nothing (useful as a default).
type NoopHook struct{}

func (h *NoopHook) Name() string { return "noop" }
func (h *NoopHook) OnAgentStart(_ context.Context, _ *LoopState) error { return nil }
func (h *NoopHook) OnBeforeTurn(_ context.Context, _ *LoopState) ([]string, error) { return nil, nil }
func (h *NoopHook) OnAfterToolCall(_ context.Context, _ *LoopState, _ string, _ map[string]any, _ string, _ error) error { return nil }
func (h *NoopHook) OnAgentEnd(_ context.Context, _ *LoopState) error { return nil }
