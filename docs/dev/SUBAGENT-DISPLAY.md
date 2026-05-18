# Sub-Agent Display — Nested Tool Cards in the Web UI

How sub-agent delegation renders in the frontend: a collapsible card showing
the agent name, task, status, and a nested list of tool calls performed by
the sub-agent.

## Architecture

```
Backend (Go)                          Frontend (React)
─────────────                         ───────────────
AgentLoop                             PuxChatAdapter
  ├─ SubscriberKey in ctx               ├─ SSE event stream
  ├─ delegate_to tool                     ├─ subagent_start → track active agent
  │   └─ ParallelRunner.RunDelegateTracked   ├─ tool_execution_start (agentName) → store
  │       ├─ emit subagent_start             ├─ tool_execution_end (agentName) → store
  │       ├─ run sub-agent loop              ├─ subagent_end → clear active agent
  │       ├─ forward tool events             └─ tool_execution_start (no agentName) → tools map
  │       └─ emit subagent_end
  └─ SSE stream to client
                                      DelegateToolUI (delegate_to card)
                                        ├─ header: agent name, task, status, duration
                                        └─ collapsible: SubAgentToolRow list
```

## Event Flow

### 1. CTO calls delegate_to

The CTO agent loop emits standard tool events:

```
tool_execution_start  { toolName: "delegate_to", args: { task, instructions } }
```

### 2. Sub-agent starts

`ParallelRunner.RunDelegateTracked()` emits `subagent_start` to the parent
subscriber channel:

```
subagent_start  { agentName: "shell_ops", task: "list files", toolName: "delegate_to" }
```

### 3. Sub-agent runs tools

The sub-agent's tool events are forwarded to the parent subscriber with
`agentName` set:

```
tool_execution_start  { toolName: "bash", agentName: "shell_ops", args: { command: "ls" } }
tool_execution_end    { toolName: "bash", agentName: "shell_ops", result: "..." }
```

### 4. Sub-agent completes

```
subagent_end  { agentName: "shell_ops", status: "completed" }
```

### 5. delegate_to returns

```
tool_execution_end  { toolName: "delegate_to", result: { result, status, agent_ref } }
```

## Frontend Routing

`PuxChatAdapter` (in `shared/src/pux-chat-adapter.ts`) routes SSE events:

| Event | Has agentName? | Destination |
|-------|---------------|-------------|
| `tool_execution_start` | Yes | Zustand store → `addAgentToolCall()` |
| `tool_execution_end` | Yes | Zustand store → `updateAgentToolCall()` |
| `tool_execution_start` | No | `tools` map (flat display) |
| `tool_execution_end` | No | `tools` map (flat display) |
| `subagent_start` | — | Track `activeSubAgentName` |
| `subagent_end` | — | Clear `activeSubAgentName` |

The `activeSubAgentName` tracker ensures that tool events between
`subagent_start` and `subagent_end` are routed to the store even if
the event doesn't carry `agentName`.

## UI Components

### DelegateToolUI (`src/web/src/components/assistant-ui/delegate-tool-ui.tsx`)

Registered via `makeAssistantToolUI({ toolName: "delegate_to" })`.

Renders a collapsible card:
- **Header**: Agent icon, agent name, task preview, status (working/done/failed),
  tool count, elapsed time, expand/collapse chevron
- **Body** (collapsible): List of `SubAgentToolRow` entries from Zustand store

### SubAgentToolRow

Each row shows:
- Colored status dot (blue=pulse while running, green=done, red=error)
- Human-readable tool label (via `TOOL_LABELS` map)
- Argument preview (URL, command, file path, etc.)
- Duration in ms or seconds

### Auto-expand behavior

Uses `useCollapsibleRoot(isRunning)`:
- Auto-expands while the sub-agent is running
- Collapses when complete (user can re-open)

## The Subscriber Type Bug (2026-05-18)

### Symptom

Sub-agent events (`subagent_start`, `subagent_end`, forwarded tool events)
never appeared in the SSE stream. The sub-agent ran successfully (result
was returned) but all intermediate events were silently dropped.

### Root Cause

Go type mismatch in the subscriber context value:

```go
// loop.go:150 — stores chan<- AgentEvent (send-only)
ctx = context.WithValue(ctx, SubscriberKey{}, subscriber)

// parallel_runner.go — asserted chan AgentEvent (bidirectional)
// WRONG: chan<- T and chan T are different Go types!
ch, _ := ctx.Value(SubscriberKey{}).(chan core.AgentEvent)
```

The type assertion failed silently (returned nil). `SendEvent()` uses
`recover()` to swallow nil-channel panics, so no crash — just invisible events.

Debug log confirmed: `subscriber=false type=chan core.AgentEvent`.

### Fix

`subscriberFromCtx()` now tries both type assertions:

```go
func subscriberFromCtx(ctx context.Context) chan<- core.AgentEvent {
    if ch, ok := ctx.Value(core.SubscriberKey{}).(chan<- core.AgentEvent); ok {
        return ch
    }
    if ch, ok := ctx.Value(core.SubscriberKey{}).(chan core.AgentEvent); ok {
        return ch  // for tests that store bidirectional channels
    }
    return nil
}
```

Also updated `WaitForDecision()` signature to accept `chan<- AgentEvent`.

### Regression Test

`TestSubscriberKeyContextRoundTrip` in `event_test.go` verifies the full
store → extract → send → receive cycle.

## Key Files

| File | Purpose |
|------|---------|
| `go-backend/internal/core/loop.go:150` | Stores subscriber in context |
| `go-backend/internal/tools/orchestration/parallel_runner.go:560-717` | Emits subagent_start/end, forwards tool events |
| `go-backend/internal/tools/orchestration/parallel_runner.go:243-254` | subscriberFromCtx with dual type assertion |
| `go-backend/internal/core/decision.go:94` | WaitForDecision subscriber param |
| `shared/src/pux-chat-adapter.ts` | SSE event routing (sub-agent vs flat tools) |
| `src/web/src/components/assistant-ui/delegate-tool-ui.tsx` | Collapsible delegate card UI |
| `src/web/src/lib/pux-store.ts` | Re-exports Zustand store types |
