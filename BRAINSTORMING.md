# Brainstorming — Patterns Worth Stealing

Sources:
- [cloudwego/eino](https://github.com/cloudwego/eino) (ByteDance/CloudWeGo)
- [google/adk-python](https://github.com/google/adk-python) (Google Agent Development Kit)
- [langchain-ai/deepagents](https://github.com/langchain-ai/deepagents) (LangChain Deep Agents)

Date: 2026-05-10

---

## What We're Building

A Claude Code CLI replacement. Local-first, model-agnostic, tool-heavy agent that runs in a terminal.
Our kernel/low-library approach is the right foundation. This doc captures ideas worth porting, not frameworks worth adopting.

---

## 1. Branch as a First-Class Concept

**What Eino does:** `GraphBranch` is a typed routing decision. After a node runs, a branch function inspects the output and picks which node runs next. The graph is compiled with all possible branches declared upfront.

```go
// Eino: branch after model node — route to tools or END
branch := compose.NewStreamGraphBranch(
    func(ctx, stream) (string, error) {
        if hasToolCalls(stream) { return "tools", nil }
        return compose.END, nil
    },
    map[string]bool{"tools": true, compose.END: true},
)
graph.AddBranch("model", branch)
```

**What we do now:** The LLM is the branch. Pux sees results, decides to delegate/respond/ask. The "routing" is implicit in the agent loop — `if len(toolCalls) > 0 { execute } else { respond }`.

**Why it matters for us:**
- Our routing IS the LLM's decision. We can't enumerate all branches at compile time because the LLM makes novel choices.
- BUT: we could formalize the *types* of routing decisions the system makes, even if the LLM picks the specific target:
  - `delegate_to(agent, task)` — branch to sub-agent
  - `respond(text)` — branch to END
  - `ask_user(question)` — branch to human-in-the-loop
  - `spawn_parallel(agents[], tasks[])` — fan-out branch
- Making these explicit means: visualization, debugging, replay, and most importantly — **the TUI can show what's happening**.

**Idea:** A `RoutingDecision` type in the agent loop that the TUI subscribes to. Not a compiled graph, but a runtime event stream that says "I chose to delegate to Sarah because X." This is halfway between Eino's static branches and our current implicit routing.

---

## 2. Checkpoint / Interrupt / Resume

**What Eino does:** Every node can be an interrupt point. State is gob-serialized and stored in a `CheckPointStore`. The system can pause mid-execution, persist everything, and resume later. This powers human-in-the-loop: the agent pauses, asks a question, waits for human input, then continues from exactly where it left off.

```go
// Eino: interrupt and resume pattern
r, err := graph.Compile(ctx,
    compose.WithInterruptBeforeNodes(map[string]bool{"tools": true}),
    compose.WithCheckPointStore(store),
)
// Later: resume from checkpoint
r.Resume(ctx, checkpointID, humanInput)
```

**What we do now:** Session persistence via JSONL — we save the full message history and can "continue" a session. But we can't pause mid-tool-execution and resume. If the user closes the terminal, the agent loop dies.

**Why it matters for a Claude Code replacement:**
- Long-running tasks (deep research, multi-file refactors) — user should be able to step away and come back
- Plan approval — agent proposes a plan, pauses, user approves, agent continues
- Cost control — pause before expensive operations (cloud API calls, long tool chains)
- Multi-terminal — start a task on desktop, check on it from laptop via Tailscale

**Idea:** Not full graph checkpointing (too complex for our flat loop). Instead:
- **Snapshot the agent loop state** after each tool call completes: messages[], pendingToolCalls[], currentRound, sandboxID
- Store snapshots in the session JSONL alongside messages
- TUI sends a "pause" signal → loop finishes current tool → saves snapshot → waits
- Resume loads snapshot, reconstructs context, continues loop
- This gives us 80% of the value with 20% of the complexity

---

## 3. Tool Middleware Chain

**What Eino does:** Tools are wrapped in a middleware chain. Each middleware can intercept before/after tool execution. They use this for: callbacks/observability, tool result collection, interrupt-on-tool-call.

```go
// Eino: middleware wraps tool execution
middleware := func(next InvokableToolEndpoint) InvokableToolEndpoint {
    return func(ctx, input) (*ToolOutput, error) {
        log.Printf("calling tool %s", input.Name)
        result, err := next(ctx, input)  // actual tool execution
        log.Printf("tool %s returned", input.Name)
        return result, err
    }
}
```

**What we do now:** `VisionAwareExecutor` wraps the tool registry and auto-describes images in tool results. It's a single hardcoded wrapper. Other cross-cutting concerns (logging, metrics, dedup) are scattered across `agent_loop.go` and `http_session.go`.

**Why it matters:**
- Clean separation of tool execution from cross-cutting concerns
- Composable: timeout + retry + dedup all stack naturally
- Per-tool configuration: some tools get retries, some get both
- Third-party extensibility: add custom middleware without touching core

**Idea:** Two hook types (LangChain pattern — see Decisions):

```go
// Node-style: runs at a point, returns state update or nil
type BeforeModelHook func(state *AgentState) map[string]any
type AfterModelHook func(state *AgentState) map[string]any

// Wrap-style: wraps a call, controls execution
type WrapToolCall func(req ToolRequest, handler func(ToolRequest) ToolResponse) ToolResponse

// Timeout as a wrap hook
func withTimeout(d time.Duration) WrapToolCall {
    return func(req ToolRequest, handler func(ToolRequest) ToolResponse) ToolResponse {
        ctx, cancel := context.WithTimeout(req.Context, d)
        defer cancel()
        return handler(req.WithContext(ctx))
    }
}
```

Vision stays native — it's a first-class capability, not a wrap hook. The dedup logic in `agent_loop.go` becomes a wrap hook. Clean, composable, testable.

---

## 4. The Pregel Execution Model (for cycles)

**What Eino does:** Pregel (think: Apache Spark's execution model) runs nodes in supersteps. Each superstep: all runnable nodes execute, their outputs flow to successor nodes via channels, next superstep begins. This naturally handles cycles — the ChatModel → Tools → ChatModel loop runs until the branch routes to END.

**What we do now:** A flat `for` loop in `agent_loop.go`. Call model, check for tool calls, execute tools, append results, loop. Simple, fast, debuggable.

**Why it matters (and why we should NOT adopt it):**
- Pregel adds a compilation step, a channel-based execution engine, and reflective type checking. ~15K lines for what our loop does in ~400.
- Our flat loop is faster (no channel overhead), simpler to debug (just read the loop), and more flexible (LLM makes routing decisions that a compiled graph can't express).
- The one advantage of Pregel — natural parallelism — we already have via `delegate_async` + `collect_results`.

**Idea worth stealing:** Not the execution model, but the **mental model of supersteps as units of observability**. Each loop iteration is a "step" with a clear begin/end. We should emit step-level events:
- `StepStart{round, modelInput}`
- `StepModelComplete{modelOutput, toolCalls}`
- `StepToolComplete{toolName, toolResult}`
- `StepEnd{round, decision: delegate|respond|ask}`

This makes the TUI, Langfuse tracing, and debugging all simpler. Right now the SSE stream is a flat sequence of events — wrapping them in step boundaries would be a big UX win.

---

## 5. History Rewriting Across Agents

**What Eino does:** When agent B receives context from agent A, the `HistoryRewriter` transforms A's messages into a user-perspective summary: `"For context: [AgentA] said: X. [AgentA] called tool: Y with args: Z."`. This prevents agent B from getting confused by seeing another agent's raw tool calls.

**What we do now:** The CTO prompt includes the delegation context — Pux says "I asked Sarah to research X, here's what she found." The sub-agent gets its own isolated prompt + task description. The CTO summarizes results for the user.

**Why it matters:**
- Our approach works because Pux is the central router. Every handoff goes through Pux.
- But with recursive delegation (CTO → Division Head → Worker), context gets lossy. The Worker never sees the original user message, only the Division Head's paraphrase.
- Eino's approach of rewriting history instead of paraphrasing is more faithful to the original context.

**Idea:** When delegating, include a **compressed but faithful** version of the conversation history, not just the task description. Something like:
- Original user message (always)
- CTO's reasoning for delegating (why this agent)
- Key findings from previous agents (if any)
- The specific task

This is what good managers do IRL — they don't just say "research X", they say "the client asked about X because of Y, we already know Z, focus on W."

---

## 6. Visualization and Introspection

**What Eino does:** `GraphInfo` captures the full graph structure at compile time — nodes, edges, branches, field mappings. This powers visualization tools and debugging.

**What we do now:** The TUI shows a flat stream of events. Sub-agent activity is visible but not structured. There's no way to see "the plan" or "what's happening next."

**Why it matters for a Claude Code replacement:**
- Users need to understand what the agent is doing and why
- Debugging failed delegations requires seeing the routing decisions
- Plan visualization would help with the plan-approve-execute workflow

**Idea:** Terminal-within-terminal (Claude CLI pattern — see Decisions):
1. **Summary view** (main screen): compact list of active sub-agents, each showing last 2-3 actions. Status indicators (running/done/failed). Always visible.
2. **Expanded view** (terminal-within-terminal): navigate down to a specific sub-agent to watch its full output stream. Pull up with keybinding, dismiss back to summary.

```
── Summary View (main screen) ──
Marcus (Dev)      ● running   Editing login.go:42... bash(go test)
Sarah (Research)  ✓ done      Found 3 relevant docs
Alex (IT Ops)     ○ waiting   Queued: run tests

── Expanded View (drill into Marcus) ──
[12:01] Reading login.go...
[12:02] bash(grep auth login.go)
       → Found 3 matches at lines 42, 67, 89
[12:03] Editing login.go:42...
       → Replaced auth logic
```

This is the single biggest UX improvement we could make. It turns the agent from "magic black box" to "transparent colleague."

---

---

---

# Google ADK Python — Patterns

Source: `google/adk-python` (cloned to `/tmp/adk-python/`)

## 7. transfer_to_agent as an LLM-Callable Tool with Enum Constraints

**What ADK does:** Agent transfers aren't a special API — they're a *tool* the LLM calls. `transfer_to_agent(agent_name)` is injected into the agent's tool list. The `TransferToAgentTool` adds a JSON Schema `enum` constraint to the `agent_name` parameter, listing only valid agent names. This prevents the LLM from hallucinating a name that doesn't exist.

```python
# ADK: TransferToAgentTool injects enum constraint
class TransferToAgentTool(FunctionTool):
    def __init__(self, agent_names: list[str]):
        super().__init__(func=transfer_to_agent)
        self._agent_names = agent_names

    def _get_declaration(self):
        function_decl = super()._get_declaration()
        # Add enum constraint — LLM can ONLY pick valid names
        agent_name_schema.enum = self._agent_names
        return function_decl
```

The transfer instructions are dynamically generated at runtime based on the agent tree — the LLM sees a list of available agents with their names and descriptions, plus rules about when to transfer.

**What we do now:** `delegate_to(agent_name, task)` is a tool on the CTO. The agent name is a free-form string — the model must spell it correctly. We rely on the CTO prompt listing available employees.

**Why it matters:**
- Free-form agent names is a real failure mode. Models typo names, invent names, or confuse similar names.
- Enum constraints are zero-cost — they just constrain the model's output space. No extra inference, no retry.
- The dynamic instruction generation (listing agents + descriptions + transfer rules) is essentially what our CTO prompt template does, but ADK regenerates it per-request based on the current agent tree.

**Idea:** Add enum constraints to `delegate_to` and `delegate_async`. The tool definition already knows the available agents — just expose them as an enum on the `agent_name` parameter. This is a 10-line change to our tool schema generation.

---

## 8. Six Callback Hooks (Per-Agent Lifecycle)

**What ADK does:** Every `LlmAgent` has 6 callback hooks:
- `before_model_callback` — inspect/modify the LLM request before sending
- `after_model_callback` — inspect/modify the LLM response after receiving
- `on_model_error_callback` — handle model errors (retry, fallback, log)
- `before_tool_callback` — inspect/modify tool call before execution
- `after_tool_callback` — inspect/modify tool result after execution
- `on_tool_error_callback` — handle tool errors (retry, fallback, log)

These are per-agent, per-invocation. A parent agent can set different hooks on different children.

**What we do now:** We have `VisionAwareExecutor` (wraps tool execution) and scattered error handling in `agent_loop.go`. No formal hook system.

**Why it matters:**
- This is the **middleware chain from pattern #3** but at the agent lifecycle level instead of the tool level.
- The `before_model_callback` is where you'd inject dynamic context (like our CTO prompt template variables).
- The `after_tool_callback` is where vision description happens (our `VisionAwareExecutor`).
- The `on_*_error_callback` is where retry logic should live (currently scattered).

**Idea:** Not 6 separate callbacks — that's over-engineering for our flat loop. Instead, 3 hooks on the agent loop:
1. `OnBeforeModel(messages) → messages` — modify the request
2. `OnToolResult(tool, result) → result` — post-process tool output
3. `OnError(phase, err) → decision` — retry, fallback, or abort

These map directly to the 3 places in our loop where cross-cutting concerns currently live. The `ToolMiddleware` from pattern #3 handles tool-level stuff. These handle loop-level stuff.

---

## 9. Agent Tree with Structured Transfer Rules

**What ADK does:** Agents form a tree. `BaseAgent` has `parent_agent` and `sub_agents`. Transfer rules are structured:
- Parent → sub-agent: always allowed
- Sub-agent → parent: allowed unless `disallow_transfer_to_parent=True`
- Sub-agent → peer: allowed when parent is also an LlmAgent AND `disallow_transfer_to_peers=False`

The `_get_transfer_targets()` function computes the valid set:
```python
def _get_transfer_targets(agent):
    result = list(agent.sub_agents)
    if not agent.disallow_transfer_to_parent:
        result.append(agent.parent_agent)
    if not agent.disallow_transfer_to_peers:
        result.extend(agent.parent_agent.sub_agents)  # peers
    return result
```

**What we do now:** Flat CTO → Employee. No peer-to-peer transfer. The CTO is the only router. With recursive delegation (CTO → Division Head → Worker), the division head can't transfer to another division's workers.

**Why it matters:**
- Peer transfer is useful when agents discover they're not the right one. If Sarah (Research) gets a question that needs code, she could transfer directly to Marcus (Dev) instead of going back through the CTO.
- Our current architecture forces all routing through the CTO, which adds a round-trip and can lose context.
- The structured rules (allow/deny per direction) are better than our implicit "CTO handles everything."

**Idea:** Not full peer transfer (that creates cycles and confusion). But we could support:
- **Return to CTO with recommendation:** Sub-agent says "I can't do X, but Marcus can." CTO then delegates to Marcus. This is one extra round-trip but keeps routing centralized.
- **Hot-passthrough:** If a sub-agent recognizes it should transfer, it returns a special `TransferTo(agent)` action. The CTO loop intercepts this and re-delegates without another model call. Zero extra inference cost.

---

## 10. output_key for Cross-Agent State Coordination

**What ADK does:** Agents can have an `output_key` — a string that tells the system "store this agent's final text in session state under this key." Other agents can read from session state. This enables coordination without message passing.

```python
# Agent A writes to state
agent_a = LlmAgent(name="researcher", output_key="research_results")
# Agent B reads from state via template variables
agent_b = LlmAgent(name="writer", instruction="Use this research: {research_results}")
```

**What we do now:** Sub-agents return results via `yield_artifact`. The CTO collects artifacts and summarizes for the user or next agent.

**Why it matters:**
- `output_key` is a simple version of what we already do with artifacts, but formalized.
- The interesting bit is the template variable syntax — `{research_results}` in the instruction string gets interpolated from session state. This is more declarative than our approach.

**Idea:** Our `yield_artifact` system already does this. But we could add template variable interpolation to employee prompts: `{{.Artifacts.research_results}}` in a role's `prompt.md` would pull from the artifact store. This would make sequential pipeline agents more declarative — the prompt says what it needs, not the CTO.

---

# LangChain DeepAgents — Patterns

Source: `langchain-ai/deepagents` (cloned to `/tmp/deepagents/`)

## 11. Ordered Middleware Stack (The Big One)

**What DeepAgents does:** `create_deep_agent()` assembles a 10+ middleware stack in a specific order:

```
TodoListMiddleware → SkillsMiddleware → FilesystemMiddleware →
SubAgentMiddleware → AsyncSubAgentMiddleware → SummarizationMiddleware →
PatchToolCallsMiddleware → [user middleware] → [profile middleware] →
ToolExclusionMiddleware → PromptCachingMiddleware → MemoryMiddleware →
HumanInTheLoopMiddleware
```

Each middleware wraps the agent. The ORDER matters: TodoList runs first (so skills and filesystem see the todo state), Summarization runs before PatchToolCalls (so patches apply to already-compressed context), etc.

The `HarnessProfile` system adds model-specific middleware. Different models get different middleware stacks — a weaker model gets more guardrails, a stronger model gets more autonomy.

**What we do now:** Our middleware is `VisionAwareExecutor` (single wrapper) + scattered logic. No ordered stack, no profile-specific behavior.

**Why it matters:**
- This is the **most important pattern from DeepAgents** because it solves the "where does this logic go?" problem.
- Every agent framework eventually accumulates cross-cutting concerns: todo management, skill lookups, file permissions, sub-agent delegation, context compression, tool patching, caching, memory, human-in-the-loop.
- Without a middleware stack, these concerns scatter across the codebase (exactly what happened to us).
- The profile-specific middleware is genius — different models need different guardrails, and this makes it declarative.

**Idea:** Combined with pattern #3 (Tool Middleware), we'd have two middleware stacks:
1. **Loop-level middleware** (DeepAgents style): wraps the entire agent loop. TodoList, Summarization, Memory, HumanInTheLoop live here.
2. **Tool-level middleware** (Eino style): wraps individual tool execution. Vision, Timeout, Retry, Dedup live here.

The profile system maps to our model config: `config/models.json` already has per-model settings. We could add a `middleware` field that specifies which loop-level middleware to activate for each model.

---

## 12. BASE_AGENT_PROMPT — Explicit Agent Behavior Rules

**What DeepAgents does:** They ship a `BASE_AGENT_PROMPT` — a carefully engineered system prompt with explicit behavioral rules:

```
- NEVER add unnecessary preamble like "I'll help you with that" or "Let me analyze this."
- Start with the answer or action.
- Don't say "I'll now do X" — just do it.
- When calling tools, don't explain why you're calling them. Just call them.
- Be concise in tool results summaries.
```

This is prompt engineering as a first-class concern — they spent real time on reducing "AI helper noise."

**What we do now:** `config/prompt.md` has the CTO system prompt, and each role has its own `prompt.md`. The prompt quality varies.

**Why it matters:**
- The "AI helper noise" problem is real and frustrating. Every model adds "I'll help you with that!" and "Let me analyze this step by step..." which wastes tokens and time.
- At 118 tok/s, every wasted token is visible latency.
- For a Claude Code replacement, the user wants terse, actionable output. Not pleasantries.

**Idea:** Add explicit anti-preamble rules to our CTO prompt template. Not as an afterthought — as a dedicated section. Something like:

```markdown
## Communication Style
- NO preamble. No "I'll help you with that." No "Let me analyze this."
- Start with the answer or the action.
- When delegating, say who and what. Not why you chose them.
- When reporting results, give the answer. Not the journey.
- Tool calls need no explanation. Just call them.
```

This is essentially free to implement (just prompt editing) and has outsized impact on the user experience.

---

## 13. SubAgent Types — Declarative, Compiled, Async

**What DeepAgents does:** Three types of sub-agents:
- **Declarative SubAgent**: config-only (name, description, tools, prompt). Created at runtime from YAML.
- **Compiled SubAgent**: pre-built agent object. For complex agents that need code, not just config.
- **Async SubAgent**: runs in parallel, results collected later. Has its own middleware stack.

The general-purpose sub-agent is auto-added if no sub-agents are specified — the system always has at least one.

**What we do now:** Employees are config-only (YAML + markdown). The `OrchestratorFactory` creates sub-orchestrators from config. We have `delegate_async` for parallel execution.

**Why it matters:**
- Our approach already covers declarative and async. We don't have the "compiled" concept because we don't need it — our config is expressive enough.
- The auto-add of a general-purpose sub-agent is interesting. Currently if all employees are busy or no employee matches, the CTO is stuck. A fallback "generalist" that can do basic work would prevent deadlocks.

**Idea:** Add a fallback employee — a general-purpose agent with basic tools (bash, file ops). Always available when no specialist matches. This prevents the CTO from getting stuck when it can't find the right person for a task.

---

## 14. HarnessProfile — Model-Specific Behavior Profiles

**What DeepAgents does:** A `HarnessProfile` customizes the agent for different models:
- Adds/removes middleware based on model capabilities
- Adjusts prompt templates for model-specific quirks
- Overrides tool descriptions for models that need more/less guidance
- Sets model-specific parameters (temperature, max_tokens)

```python
class HarnessProfile:
    middleware: list[Middleware]  # added to base stack
    prompt_overrides: dict[str, str]  # per-section prompt changes
    tool_description_overrides: dict[str, str]  # per-tool description
```

**What we do now:** `config/models.json` has per-model settings (context length, temperature, etc.). But the prompt and tool descriptions are the same regardless of model.

**Why it matters:**
- We support multiple models (Qwen, Gemma, DeepSeek, Gemini). Each has different strengths and weaknesses.
- DeepSeek needs more guidance on tool calling (it hallucinates tool names). Gemma needs shorter prompts. Qwen handles complex instructions well.
- Right now we use the same prompt for all models and hope for the best.

**Idea:** Add model-specific prompt overrides to `config/models.json`. Not a full profile system — just:
```json
{
  "deepseek-v4-flash": {
    "prompt_suffix": "IMPORTANT: Only use tool names from the provided list. Do not invent tool names.",
    "tool_description_overrides": {
      "delegate_to": "Delegate a task to a team member. Available members: {{.Agents}}"
    }
  }
}
```

This is a small config change that could significantly improve tool-calling accuracy on weaker models.

---

## 15. TodoListMiddleware — Structured Task Management

**What DeepAgents does:** The `TodoListMiddleware` gives the agent a structured todo list. The agent can create, update, and complete items. The todo state is visible in the context window, so the model can track progress on complex multi-step tasks.

**What we do now:** The `plan` tool creates a plan, but there's no structured task tracking during execution. The model just... does things and hopes it remembers what it was doing.

**Why it matters:**
- Complex tasks (multi-file refactors, deep research) lose the plot. The model starts strong and then forgets what it was doing.
- A visible todo list keeps the model on track — it can see what's done, what's next, and what's blocked.
- This is essentially the "plan approval" pattern but internal to the agent — no human needed.

**Idea:** Not a separate middleware. Add a `todo_list` tool that maintains a structured list in the session state. The CTO creates todos during planning, marks them complete during execution. The todo list is injected into every model call as context:

```json
{"todos": [
  {"id": 1, "task": "Fix auth bug in login.go", "status": "done"},
  {"id": 2, "task": "Run tests", "status": "in_progress"},
  {"id": 3, "task": "Update docs", "status": "pending"}
]}
```

This is cheap to implement and directly addresses the "losing the plot" problem.

---

## Decisions

### Checkpoint Scope → Code Changes Only

Snapshot when code changes happen (file edits, new files, deletions). Not per tool call, not per round, not per delegation. The expensive state to recover is file modifications, not bash output or search results. A checkpoint = the diff since last snapshot + current loop position (round, pending tools). This is narrow, useful, and doesn't explode storage.

### Middleware Ordering → Node-style + Wrap-style Hooks (LangChain Pattern)

Two hook types, not one:

**Node-style hooks** — run at specific execution points, return state updates or `nil`:
- `before_agent` — once per invocation
- `before_model` — before each model call (message limit checks, context injection)
- `after_model` — after each model response (response logging, token counting)
- `after_agent` — once per invocation (cleanup, reporting)

**Wrap-style hooks** — wrap around a call, control execution:
- `wrap_model_call(request, handler) → response` — retry, fallback, caching
- `wrap_tool_call(request, handler) → response` — timeout, validation, post-processing

Ordering is declaration order in the wiring. Node hooks are sequential and lightweight. Wrap hooks compose naturally (outer wraps inner).

**Vision is NOT middleware.** Vision is native to the system with a fallback chain (MCP → native llama.cpp → cloud). It lives in the core tool execution path, not in a middleware wrapper. We work with visuals constantly — it's a first-class capability, not a cross-cutting concern.

### Trace Tree → Terminal Within Terminal (Claude CLI Pattern)

Not a rendered tree in the main view. Two layers:

1. **Summary view** (main screen): compact list of active sub-agents, each showing last 2-3 actions. Status indicators (running/done/failed). This is always visible and lightweight.

2. **Expanded view** (terminal-within-terminal): navigate down to a specific sub-agent to watch its full output stream. Pull up with a keybinding, dismiss back to summary. Background commands also accessible below the text input.

This matches how Claude Code CLI works. Users get situational awareness without clutter, and can drill in when they care.

### Dynamic Graph → No. Plans Are for Humans, Routing Is on the Fly.

The CTO does not emit graph structures. Plans are `.md` documents for human consumption and model context. Routing decisions are made by the LLM at runtime — that's the whole point of our approach. Graph formalism is unnecessary indirection for what the model already does naturally.

### HarnessProfile → Deferred. Stub Only.

Not implementing model-specific profiles now. Can plant a stub (`profile.yaml`) in agent config folders for future use. The current `models.json` per-model settings (temperature, context length) are sufficient for now.

### Todo List → Every Model Call, ~250 Tokens Max, Rendered on TUI.

Injected into every model call as system context, not on demand. Always visible = better tracking, and 250 tokens is negligible at our context sizes. Rendered in the TUI as a compact checklist (done/in-progress/pending). The todo list is a tool the model calls to update, but the state is always injected.

Format:
```json
{"todos": [
  {"task": "Fix auth bug in login.go", "status": "done"},
  {"task": "Run tests", "status": "in_progress"},
  {"task": "Update docs", "status": "pending"}
]}
```

### Peer Transfer → Return-with-Recommendation. Delegators Are the Routers.

Sub-agents don't transfer directly to peers. If Sarah can't handle something, she returns to the CTO with a recommendation ("Marcus should handle this because X"). The CTO re-delegates. This keeps routing centralized and debuggable. No hot-passthrough, no peer-to-peer. The delegators (CTO, Division Heads) own intercommunication.

---

## Implementation Priorities

| Priority | Pattern | Source | Effort | Impact |
|----------|---------|--------|--------|--------|
| **1** | Anti-Preamble Prompt Rules | DeepAgents | Tiny | Huge UX win for CLI |
| **2** | Node + Wrap Hooks | LangChain | Medium | Kernel extension point |
| **3** | Enum Constraints on agent_name | ADK | Tiny | Prevents delegation failures |
| **4** | Step-Level Events | Eino | Small | TUI, Langfuse, debugging |
| **5** | Todo List Tool + TUI Render | DeepAgents | Small | Complex task tracking |
| **6** | Terminal-within-Terminal Trace | Claude CLI | Medium | Sub-agent visibility |
| **7** | Checkpoint on Code Changes | Eino | Medium | Long-running tasks |
| **8** | History Rewriting for Delegation | Eino | Small | Better delegation context |
| **9** | Fallback Generalist Employee | DeepAgents | Small | Prevents delegation deadlocks |
| **10** | HarnessProfile Stub | DeepAgents | Tiny | Future model-specific config |

Deferred: Hot-passthrough transfer, full peer transfer, compiled sub-agents, dynamic graph plans.

---

## Things We Should NOT Take

### From Eino
- **The generic type system.** 15K lines of `reflect` gymnastics to achieve what concrete code does in 400 lines.
- **Compiled graphs.** Our LLM makes routing decisions at runtime. Pre-compiled graphs can't express that.
- **The Pregel channel execution engine.** Unnecessary indirection for a single-machine, single-loop agent.
- **Cloud hooks everywhere.** `callbacks.OnStart`/`OnEnd` on every operation adds latency.
- **Gob serialization for state.** Our JSONL approach is human-readable and greppable. Gob is neither.

### From Google ADK
- **Pydantic BaseModel for agents.** Python-specific. Our Go structs are fine.
- **`disallow_transfer_to_parent/peers` as booleans on the agent.** Too granular. Our "CTO is the router" is simpler and works.
- **`output_key` stringly-typed state coordination.** Our artifact system is more explicit and type-safe.
- **`model_post_init` for setting parent refs.** Magic side effects in constructors. Our explicit `NewOrchestrator()` is cleaner.

### From DeepAgents
- **10+ middleware by default.** That's a lot of abstraction for what should be a tight agent loop. We should add middleware only when needed.
- **PatchToolCallsMiddleware.** This patches model mistakes at the middleware level. Better to fix the prompt or use enum constraints (pattern #3).
- **PromptCachingMiddleware.** Model-specific optimization. We handle this at the HTTP engine level already.
- **The general-purpose sub-agent auto-add.** Good idea but should be configurable, not automatic. Some use cases don't need it.
