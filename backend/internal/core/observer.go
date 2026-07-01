package core

import (
	"context"
	"time"
)

// TaskObserver watches dispatch-task lifecycle transitions. The task store
// fires these inside its mutex critical section. Optional — store tolerates
// nil. The order is: OnTaskPending (on Insert) → OnTaskRunning (when the
// goroutine starts work) → OnTaskComplete | OnTaskFailed (terminal).
type TaskObserver interface {
	OnTaskPending(ctx context.Context, taskID, org, task string, startedAt time.Time)
	OnTaskRunning(ctx context.Context, taskID string)
	OnTaskComplete(ctx context.Context, taskID, result string, finishedAt time.Time)
	OnTaskFailed(ctx context.Context, taskID, errorMsg string, finishedAt time.Time)
}

// ChatObserver watches agent-loop assistant messages. Fired by agent.Loop
// after each provider response is appended to the conversation. Optional —
// Loop tolerates nil. Round is the 1-based Plan/Act/Observe cycle index.
// Role identifies which agent produced the message: "cto" for the dispatch
// CTO loop, or the role name for a delegated employee loop.
type ChatObserver interface {
	OnAssistantMessage(ctx context.Context, taskID, role string, round int, content string)
}

// ToolObserver watches agent-loop tool dispatches. Fired by agent.Loop's
// dispatchTools after each tool returns. Optional — Loop tolerates nil.
//
// This is distinct from the audit hook (which fires at MCP-server level
// for external tool calls). This catches only in-loop calls driven by the
// agent's own tool_use blocks. Args + result are pre-scrubbed string forms
// (JSON for structured args, stringified result). Duration covers the
// executor call; err is the tool error (non-nil = tool failed).
//
// Role identifies which agent dispatched the tool: "cto" or a role name.
type ToolObserver interface {
	OnToolCall(ctx context.Context, taskID, role string, round int, tool, args, result string, duration time.Duration, err error)
}
