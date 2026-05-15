# Pux Contract Specification

> Source of truth. Everything in this system conforms to this document or it's a bug.

## Philosophy

Pux is a contract system. The kernel enforces communication policy. Interfaces consume the contract. Extensions implement the contract. Purity comes from the contract, not the code.

The kernel does not know or care who consumes its events. It emits. Interfaces render. The contract is the only thing that connects them.

## Roles

```
┌─────────────┐         ┌─────────────┐         ┌──────────────┐
│  Interfaces  │◄────────│   Kernel    │◄────────│  Capabilities │
│  TUI, CLI,  │  SSE    │  (Go:3847)  │  Tools   │  MCP, Ext,   │
│  Web, API   │  Events │             │  & LLM   │  Browser,    │
│             │────────►│             │◄────────│  Desktop     │
│             │  Prompt │             │  Execute │              │
└─────────────┘         └─────────────┘         └──────────────┘
```

- **Kernel**: Emits SSE events. Executes tools. Manages sandbox lifecycle. Knows nothing about rendering.
- **Interfaces**: Consume SSE events. Render to their medium. Send prompts back. Know nothing about tool execution.
- **Capabilities**: Tools, MCP servers, extensions. Implement the Tool interface. Know nothing about interfaces.

## Contract 1: Prompt Request

**Direction**: Interface → Kernel
**Transport**: `POST /api/pux/prompt`

```json
{
  "message": "string (required)",
  "project": "string (required) — project path or name",
  "org": "string (optional) — org path for role overlay",
  "agentId": "string (optional) — session continuity",
  "model": "string (optional) — override model",
  "thinkingLevel": "string (optional) — low|medium|high",
  "autoBranch": "boolean (optional)"
}
```

**Response**: SSE stream of events (Contract 2).

## Contract 2: SSE Event Stream

**Direction**: Kernel → Interface
**Transport**: Server-Sent Events (`text/event-stream`)

Every event has:
- `event:` field = event type
- `data:` field = JSON payload
- Optional `id:` field = event ID for replay

### 2.1 Agent Lifecycle

| Event | Payload | When |
|-------|---------|------|
| `agent_spawned` | `{agentId: string}` | Session created |
| `agent_start` | `{}` | Agent loop begins |
| `agent_end` | `{input: int, output: int, cache: int, model: string, contextWindow?: int}` | Agent loop completes |

### 2.2 Content Streaming

| Event | Payload | When |
|-------|---------|------|
| `text_delta` | `{text: string, agentName?: string}` | Response text chunk |
| `thinking_delta` | `{text: string, agentName?: string}` | Reasoning text chunk |

Text and thinking deltas accumulate. Interface buffers until a non-delta event arrives.

### 2.3 Tool Execution

| Event | Payload | When |
|-------|---------|------|
| `tool_execution_start` | `{toolName: string, toolId: string, args: any, agentName?: string}` | Tool call begins |
| `tool_execution_end` | `{toolName: string, toolId: string, result: any, error?: string, agentName?: string}` | Tool call completes |
| `tool_update` | `{toolName: string, toolId: string, text: string, agentName?: string}` | Long-running tool progress |

Tool events are correlated by `toolId`. Start + end are guaranteed pairs.

### 2.4 Sub-Agent Delegation

| Event | Payload | When |
|-------|---------|------|
| `subagent_start` | `{agentName: string, task: string, toolName?: string}` | Delegation begins |
| `subagent_end` | `{agentName: string, status: string, task: string, error?: string}` | Delegation completes |

Sub-agents emit their own `text_delta`, `thinking_delta`, and `tool_execution_*` events with `agentName` set.

### 2.5 Context Management

| Event | Payload | When |
|-------|---------|------|
| `compaction_start` | `{}` | Context compression begins |
| `compaction_end` | `{compactedMessages: int, keptMessages: int, contextTokens: int, contextSize: int, contextUtil: float, compactionType: string}` | Context compression done |

### 2.6 User Interaction

| Event | Payload | When |
|-------|---------|------|
| `user_question` | `{questionId: string, question: string, options?: string[], allowFreeText?: boolean, default?: string}` | Agent asks user |
| `approval_request` | `{requestId: string, title?: string, description?: string, toolName?: string, args?: any}` | Agent needs approval |

Interface MUST handle these by presenting UI and sending the response back.
Response: `POST /api/pux/respond` with `{requestId: string, response: string}`.

### 2.7 Artifacts & Plans

| Event | Payload | When |
|-------|---------|------|
| `artifact_created` | `{type: string, content: any}` | Tool produces output artifact |
| `artifact_updated` | `{type: string, content: any}` | Artifact modified |
| `plan_created` | `{planId: string, name: string, content: string, filePath: string}` | Plan document created |
| `plan_updated` | `{planId: string, content: string}` | Plan document modified |

### 2.8 System

| Event | Payload | When |
|-------|---------|------|
| `error` | `{error: string}` | Unrecoverable error, stream ends |
| `hook_request` | `{hookId: string, hookPoint: string, toolName: string, args: any, result?: any}` | External hook paused |

### 2.9 Stream Control

- `[DONE]` — Stream terminator. Not an event, a sentinel.
- `keepalive` — Every 15 seconds. Interfaces ignore.

### 2.10 Step Lifecycle

| Event | Payload | When |
|-------|---------|------|
| `step_start` | `{round: int}` | Agent loop iteration begins |
| `step_end` | `{round: int, decision: string}` | Agent loop iteration completes |

Decision values: `"respond"`, `"delegate"`, `"ask"`, `"error"`.

## Contract 3: Tool Interface

**Direction**: Kernel ↔ Capabilities

Every tool implements:

```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage        // JSON Schema for parameters
    Execute(ctx context.Context, args map[string]any) (any, error)
}
```

### 3.1 Tool Sources

All tools enter the kernel through ONE of these paths:

1. **Compiled tools** — Go code in `go-backend/internal/tools/`. Fast, for core operations (bash, file ops, delegation).
2. **MCP tools** — External MCP servers. Wrapped by `MCPTool` adapter. For research, vision, etc.
3. **Extension tools** — TypeScript MCP servers. Discovered, started, wrapped by `MCPTool`. For user-defined capabilities.
4. **Future: Ray tools** — Ray Serve deployments. Will be wrapped by same `MCPTool` adapter. For GPU workloads.

All sources produce the same `Tool` interface. The kernel does not distinguish.

### 3.2 Tool Execution Contract

1. LLM emits tool call (name + args)
2. Kernel resolves tool by name (with alias support)
3. Kernel emits `tool_execution_start`
4. Tool executes (may emit `tool_update` for long operations)
5. Kernel emits `tool_execution_end` (result or error)
6. Result fed back to LLM

Large results (>4096 bytes) are automatically spilled to disk by the context manager. The agent gets a preview + `load_spilled()` reference.

### 3.3 Delegation Tools

`delegate_to` and `delegate_async` are special CTO tools that create sub-agent loops. They:
- Accept `agentName` + `task` arguments
- Create a new agent loop with the named employee's tools
- Emit `subagent_start` / `subagent_end` events
- Sub-agent events flow through the same SSE stream with `agentName` set

### 3.4 No Bypass

Tools MUST NOT:
- Hold direct references to the SSE subscriber channel
- Access interface state
- Import rendering logic
- Depend on a specific interface being connected

Tools that need to emit events (e.g., `user_question`, `plan_created`) retrieve the subscriber from the execution context (`core.SubscriberKey`), injected by the agent loop. The tool is a pure `Tool` interface implementation — no special constructor parameters for event streams.

### 3.5 Organization Overlay Boundary

Org roles are strictly additive:
- Org can ADD new roles
- Org CANNOT replace kernel roles (jake, ryan, sarah, marcus, elena, alex)
- Name collision with a kernel role is silently ignored — the kernel role wins

## Contract 4: Client State

**Direction**: Interface internal

`ChatState` (`shared/src/pux-chat-adapter.ts`) is the canonical client-side event consumer. Every interface MUST use it or an equivalent that:

1. Subscribes to the SSE stream
2. Accumulates `text_delta` and `thinking_delta` into messages
3. Tracks tool execution state via `toolId` correlation
4. Maintains message history with role, text, thinking, tools
5. Signals streaming state (active/idle)

```typescript
interface ChatMessage {
  role: "user" | "assistant" | "error"
  text: string
  thinking?: string
  tools: ChatToolCall[]
  errorMessage?: string
}

interface ChatToolCall {
  id: string
  name: string
  args?: any
  status: "running" | "done" | "error"
  result?: any
}
```

Interfaces that bypass ChatState are out of contract and must be fixed.

## Contract 5: Extensions

Extensions are TypeScript MCP servers. They:

1. Drop into `extensions/` (project) or `~/.pux/extensions/` (global)
2. Declare tools via `extension.yaml` manifest
3. Start as Bun subprocesses
4. Expose MCP protocol on a port (announced via `PUX_EXT_PORT:<port>` stdout)
5. Tools discovered via MCP `tools/list`
6. Wrapped as `Tool` interface by the kernel

Extensions MUST NOT:
- Access kernel internals
- Directly emit SSE events
- Modify other extensions' state

Extensions CAN:
- Call external APIs (including Ray services)
- Maintain their own state
- Define custom tool schemas
- Render in the TUI via `renderCall`/`renderResult` hooks

## Contract 6: Organizations

Organizations are config overlays. They:

1. Detected by `pux.yaml` in project directory
2. Add org-specific roles to kernel defaults (MERGE, not replace)
3. Prepend manifesto to CTO system prompt
4. Register scheduled prompts with the scheduler
5. Provide org-specific tool packages

Org roles overlay kernel roles by name. Kernel staff (jake, ryan, sarah, etc.) are always available.

## Compliance Rules

1. **Kernel emits only contract events.** No ad-hoc event types. New events require updating this document.
2. **Interfaces consume only contract events.** Ignore unknown events gracefully. Never depend on kernel internals.
3. **Tools implement only the Tool interface.** No shortcuts to the SSE stream.
4. **Extensions speak only MCP.** No Go FFI, no direct kernel access.
5. **ChatState is the canonical client.** Any interface that accumulates its own state is a contract violation.

## Future Integrations

Everything new enters through the same contracts:

- **Ray Serve (GPU)** → MCP server wrapping Ray deployments → `MCPTool` → Tool interface
- **Computer Use (Go)** → Compiled tool in `internal/tools/` → Tool interface → contract events
- **Browser Use (Go)** → Compiled tool in `internal/tools/` → Tool interface → contract events
- **New interfaces (mobile, API)** → Consume SSE events via ChatState → Send prompts via Contract 1
- **New LLM backends** → Kernel abstracts via engine interface → Same event stream → No interface changes
