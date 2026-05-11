# Brainstorming 2: Reworking the Toolset Based on Pi-Subagents

**Date**: 2026-05-10
**Source**: Study of [nicobailon/pi-subagents](https://github.com/nicobailon/pi-subagents)

---

## Problem Statement

Our current CTO/Employee split works but has gaps. The CTO gets basic file ops + delegation tools, employees get specialized tool packages. But we're missing key patterns that pi-subagents nails:

1. **No structured handoff pipeline** — scout → plan → implement → review doesn't exist as a first-class workflow
2. **No watchdog/guardrails** — agents can run forever, fail silently, or return planning instead of implementation
3. **No oracle/consistency check** — nothing prevents the CTO from making conflicting decisions across a long session
4. **No bidirectional communication** — employees can't ask the CTO for clarification mid-task
5. **Tool overlap is crude** — `shell` and `code` packages are nearly identical, `browser` is just `bash` with instructions

---

## Proposed Rework

### Phase 1: New Agent Roles (Add, Don't Replace)

Current roles: sarah, jake, marcus, elena, alex, ryan, generalist
Proposed additions based on pi-subagents patterns:

#### 1. `scout` — Fast Codebase Recon

```yaml
# config/roles/scout/config.yaml
name: scout
description: "Fast codebase recon. Maps relevant files, entry points, data flow, and produces context for other agents."
imports: [shell]  # read, grep, glob only — no write
model: gemini-3-flash-preview
max_rounds: 10
temperature: 0.2
output: context.md
```

**System prompt shape**: "Map the area before diving deeper. Output `context.md` with: Files Retrieved, Key Code, Architecture, Start Here. Use grep/find/read to scout. Do not edit files."

**Why**: Currently the CTO has to use `delegate_to` with marcus just to understand code. A dedicated scout with low thinking cost and structured output gives better handoff to planner/worker.

#### 2. `planner` — Implementation Plans

```yaml
# config/roles/planner/config.yaml
name: planner
description: "Creates concrete implementation plans from context. Reads and plans — never edits code."
imports: [shell]  # read, grep, glob — no write except plan output
model: deepseek/deepseek-v4-flash
max_rounds: 12
temperature: 0.2
output: plan.md
```

**System prompt shape**: "Turn requirements and code context into a concrete implementation plan. Name exact files. Small, ordered, actionable tasks. Call out risks. Output `plan.md`."

**Why**: Currently marcus both plans AND implements. Splitting these produces better plans (the planner isn't tempted to start coding) and lets the CTO review before committing to implementation.

#### 3. `reviewer` — Code Review + Small Fixes

```yaml
# config/roles/reviewer/config.yaml
name: reviewer
description: "Reviews code diffs, plans, or codebase health. Can make small corrective edits but not broad rewrites."
imports: [code]
model: gemini-3-flash-preview
max_rounds: 12
temperature: 0.3
```

**System prompt shape**: "Review the diff/plan/code. Verify: implementation matches intent, tests pass, no regressions, minimal change. If everything looks good, say so. Structured output: Correct, Fixed, Blocker, Note."

**Why**: We have no review step. Adding a reviewer after implementation catches bugs the worker missed. Different model (Gemini flash for speed vs DeepSeek for implementation).

#### 4. `oracle` — Decision Consistency

```yaml
# config/roles/oracle/config.yaml
name: oracle
description: "Challenges assumptions, catches drift, recommends the safest next move. Read-only."
imports: [shell]  # read-only access
model: deepseek/deepseek-v4-flash
max_rounds: 10
temperature: 0.2
```

**System prompt shape**: "You are the oracle. Before acting, reconstruct inherited decisions. Identify drift. Surface contradictions. Recommend narrow corrections over broad pivots. Output: Inherited Decisions, Diagnosis, Drift Check, Recommendation, Risks."

**Why**: Long sessions accumulate hidden decisions. The oracle gets a fork of the conversation and spots things the CTO missed due to context rot. Cheap insurance for complex tasks.

#### 5. `context-builder` — Requirements to Context

```yaml
# config/roles/context-builder/config.yaml
name: context-builder
description: "Analyzes requirements against the codebase and produces structured handoff context."
imports: [shell, research]  # code reading + web research
model: gemini-3-flash-preview
max_rounds: 15
temperature: 0.3
output: context.md
```

**System prompt shape**: "Read the request. Search the codebase for relevant files, patterns, dependencies. Conduct web research if needed. Output `context.md` (files, patterns, constraints) and `meta-prompt.md` (goal, evidence, success criteria, validation)."

**Why**: Stronger than scout for complex tasks. Gathers BOTH code context and external docs/API references before planning. The handoff material is complete enough that the next agent doesn't rediscover the same ground.

---

### Phase 2: Rework Tool Packages

Current tool packages are too coarse. Refined split:

#### `shell.yaml` → Split into `inspect.yaml` + `ops.yaml`

```yaml
# inspect.yaml — Read-only inspection tools
tools:
  - bash          # but only read-only commands enforced via prompt
  - file_read
  - file_grep
  - file_glob

# ops.yaml — Full shell + file write
tools:
  - bash
  - file_read
  - file_write
  - file_edit
  - file_grep
  - file_glob
```

**Why**: Scout, planner, and oracle should NEVER write files (except their designated output). Currently giving them `shell` gives them `file_write` too. The split enforces read-only at the tool level.

#### `code.yaml` stays as-is

Already well-defined for marcus/reviewer.

#### `browser.yaml` → Expand with SoM context

No change to tools, but add structured output contract to jake's prompt:
- After every browser action, produce a structured `browser_state.md`
- Other agents can read this for context without running browser themselves

#### Add `intercom.yaml` — Bidirectional Communication

```yaml
# intercom.yaml — Ask the CTO questions mid-task
tools:
  - ask_supervisor   # New tool: send question to CTO, wait for reply
  - yield_artifact   # Existing: hand off work products
```

**Why**: Currently employees can't ask for clarification. They either guess or fail. `ask_supervisor` lets the worker pause and ask the CTO "which approach should I use?" — like pi-subagents' `contact_supervisor`.

---

### Phase 3: Structured Pipelines

Instead of ad-hoc delegation, add first-class pipeline support to the CTO's toolset:

#### `delegate_chain` Tool

```json
{
  "name": "delegate_chain",
  "description": "Execute a pipeline of agents sequentially, piping output between steps.",
  "parameters": {
    "task": "The original task from the user",
    "steps": [
      {"agent": "scout", "output": "context.md"},
      {"agent": "planner", "reads": ["context.md"], "output": "plan.md"},
      {"agent": "marcus", "reads": ["plan.md"]},
      {"agent": "reviewer", "reads": ["plan.md"]}
    ],
    "context": "fork"  // or "fresh"
  }
}
```

**How it works**:
1. Each step runs in a sub-agent loop
2. `{previous}` template variable pipes output between steps
3. `{task}` provides the original user request
4. `reads` specifies files from chain directory to inject into prompt
5. `output` specifies what file the agent should write
6. Chain directory (`/tmp/chain-<id>/`) holds all artifacts

**Why**: Currently the CTO manually orchestrates multi-step work with multiple `delegate_to` calls, remembering context in its own conversation. A chain offloads this to a structured pipeline that's more reliable and observable.

#### `delegate_parallel` Tool

```json
{
  "name": "delegate_parallel",
  "description": "Run multiple agents concurrently on different aspects of the same task.",
  "parameters": {
    "task": "The task",
    "agents": [
      {"agent": "reviewer", "task": "Review for correctness"},
      {"agent": "reviewer", "task": "Review for test coverage"},
      {"agent": "reviewer", "task": "Review for unnecessary complexity"}
    ],
    "concurrency": 3
  }
}
```

**Why**: Parallel review is a killer pattern from pi-subagents. Three reviewers with different angles catch more issues than one pass. Currently we can do this with `delegate_async` but it's not structured.

---

### Phase 4: Guardrails and Watchdogs

#### 1. Completion Mutation Guard

**Problem**: Worker agents sometimes return a plan instead of actually implementing it.
**Solution**: After a `marcus` or `worker` delegation completes, check:
- Did the agent call any mutating tools (file_write, file_edit, bash with write commands)?
- If not, and the task was framed as implementation, mark as failed.

```go
// In agent_loop.go, after sub-agent completes
func evaluateCompletionGuard(result *AgentResult, task string) {
    if isImplementationTask(task) && !result.HasMutatingToolCalls() {
        result.ExitCode = 1
        result.Error = "Agent completed without making edits for an implementation task"
    }
}
```

**Patterns that signal "should mutate"**: "implement", "fix", "refactor", "add", "update", "change", "create", "build", "write"
**Patterns that signal "read-only is OK"**: "review", "plan", "analyze", "research", "scout", "check", "inspect", "oracle"

#### 2. Activity Watchdog

**Problem**: Agents can get stuck in loops or hang on long operations.
**Solution**: Per-sub-agent activity tracking with configurable thresholds.

```yaml
# In org or kernel config
watchdog:
  needs_attention_after_ms: 60000    # 1 min idle → flag
  active_long_running_after_ms: 240000  # 4 min active → flag
  failed_tool_attempts_before_attention: 3
```

Implementation: Track `lastActivityAt` per sub-agent. Emit control events:
- `needs_attention`: No tool calls/messages for N seconds
- `active_long_running`: Still active but past time threshold
- `tool_failures`: N consecutive mutating tool failures

The CTO receives these as structured notifications and can decide to interrupt, nudge, or let it continue.

#### 3. Context Fork Filtering

**Problem**: When forking context for sub-agents, parent-only artifacts pollute the child's context.
**Solution**: Strip from forked context:
- Prior `delegate_to` / `delegate_async` tool calls and results
- Orchestration instruction messages
- Session metadata / slash commands
- Keep: user messages, regular tool calls/results, assistant text

```go
func filterForkContext(messages []Message) []Message {
    var filtered []Message
    for _, msg := range messages {
        if isOrchestrationArtifact(msg) {
            continue  // skip parent-only artifacts
        }
        filtered = append(filtered, msg)
    }
    return filtered
}
```

---

### Phase 5: Intercom / ask_supervisor

#### New Tool: `ask_supervisor`

```json
{
  "name": "ask_supervisor",
  "description": "Ask the CTO a question and wait for a reply. Use when blocked or needing a decision.",
  "parameters": {
    "reason": "need_decision | progress_update",
    "message": "The question or update"
  }
}
```

**Implementation**:
1. Employee calls `ask_supervisor`
2. Sub-agent loop pauses (doesn't terminate)
3. Question is sent back to CTO via channel
4. CTO sees it in the SSE stream as a structured event
5. User/CTO responds via a new message
6. Response is injected into sub-agent's context
7. Sub-agent loop resumes

**Why**: This is the single biggest missing feature vs pi-subagents. Currently employees can only succeed or fail. No mid-task clarification. This would let:
- Worker ask "should I use approach A or B?"
- Scout report "I found something unexpected, should I dig deeper?"
- Oracle flag "this conflicts with your earlier decision about X — which wins?"

---

### Phase 6: Agent Definition Improvements

#### Frontmatter Enhancements

Add to `config.yaml`:

```yaml
# Current
name: marcus
imports: [code]
max_rounds: 25

# Proposed additions
default_context: fork       # fork = inherit parent conversation, fresh = blank
output: plan.md             # expected output artifact
default_reads: [context.md] # auto-read these files before starting
system_prompt_mode: replace # replace (default) or append
progress_tracking: true     # maintain progress.md in chain dir
max_subagent_depth: 0       # prevent sub-delegation (workers can't spawn workers)
```

#### Agent-as-Markdown Option

Consider supporting `.md` files with YAML frontmatter alongside `config.yaml` + `prompt.md`:

```markdown
---
name: scout
description: Fast codebase recon
tools: read, grep, glob, bash
thinking: low
output: context.md
---

You are a scouting subagent. Move fast but do not guess...
```

This is more portable and self-contained (one file vs two). Could support both formats.

---

## Migration Path

### Immediate (No Breaking Changes)
1. Add `scout`, `planner`, `reviewer` roles as new config/roles/ directories
2. Add `inspect.yaml` tool package (subset of shell)
3. Update CTO prompt to mention new agents

### Medium Term
4. Add `delegate_chain` and `delegate_parallel` tools to CTO
5. Add `ask_supervisor` tool for employees
6. Add completion mutation guard to agent loop
7. Add activity watchdog with configurable thresholds

### Longer Term
8. Add `oracle` and `context-builder` roles
9. Context fork filtering for sub-agents
10. Agent-as-markdown format support
11. Chain directory with shared artifacts between steps

---

## Recommended Pipeline for Implementation Work

```
User asks for a change
  → CTO: delegate_chain({
      steps: [
        {agent: "scout", output: "context.md"},
        {agent: "planner", reads: ["context.md"], output: "plan.md"},
        {agent: "marcus", reads: ["plan.md", "context.md"]},
        {agent: "reviewer", reads: ["plan.md"]}
      ]
    })
  → CTO reviews reviewer feedback
  → CTO: delegate_to("marcus", "Apply reviewer fixes")
  → Done
```

For quick tasks, the CTO can still delegate directly:
```
CTO: delegate_to("marcus", "Fix the typo in main.go")
```

For uncertain tasks:
```
CTO: delegate_to("oracle", "Review my plan before we commit to this approach")
```

---

## Open Questions

1. **Model assignment**: Should scout/planner/reviewer use cheaper models (Gemini flash) vs marcus on DeepSeek? Currently roles have model overrides. This works but needs model registry awareness.
2. **Chain state**: Where does the chain directory live? `/tmp/chain-<id>/` like pi-subagents? Or in the project sandbox?
3. **ask_supervisor UX**: How does the user see and respond to supervisor questions in the TUI? Inline in SSE stream? Separate panel?
4. **Parallel sub-agents**: Currently `delegate_async` runs sub-agents in goroutines. For `delegate_parallel`, do we need a concurrency limiter? Pi-subagents uses MAX_CONCURRENCY=4.
5. **Backward compatibility**: Can we add these without breaking existing org configs and pux.yaml setups?

---

## Part B: Insights from Pi-Lens

**Source**: [apmantza/pi-lens](https://github.com/apmantza/pi-lens) — Real-time code feedback extension for Pi

Pi-lens is a **completely different beast** from pi-subagents. It's not about delegation or orchestration — it's a **code quality watchdog** that runs inline during agent tool calls. 719 files, production-grade, 35+ language support.

### What Pi-Lens Does

On every `write` and `edit` tool call, pi-lens runs a pipeline:

1. **Secrets scan** — blocking; aborts the write if credentials detected
2. **Auto-format** — deferred to end of turn, batched
3. **Auto-fix** — safe fixes from Biome, Ruff, ESLint, stylelint, sqlfluff, RuboCop
4. **LSP file sync** — opens/updates file in active language servers
5. **Dispatch lint** — parallel runner groups: LSP diagnostics, tree-sitter rules, ast-grep rules, fact rules, linters, Semgrep
6. **Cascade diagnostics** — impact cascade showing which other files were affected

### Key Patterns Worth Borrowing

#### 1. Read-Before-Edit Guard

**Problem**: Agents sometimes edit files they've never read. This causes blind edits that break things.
**Pi-lens solution**:
- Track every `read` call with file path + line ranges
- Block any `write`/`edit` to a file not previously read (zero-read block)
- Block if file changed on disk since last read (stale-read block)
- Block if edit target lines fall outside the ranges previously read (out-of-range block)
- Coverage accumulates across multiple reads (read 1-100, then 101-200 = full coverage)

**Our adaptation**: Add read tracking to the agent loop. Track which files and line ranges have been read. Before executing `file_edit` or `file_write`, validate coverage. This prevents the model from hallucinating edits to code it hasn't seen.

```go
// In agent_loop.go
type ReadCoverage struct {
    FileRanges map[string][]Range // file -> list of read ranges
    FileHashes map[string]string  // file -> content hash at read time
}

func (rc *ReadCoverage) ValidateEdit(filePath string, editRange Range) error {
    ranges, ok := rc.FileRanges[filePath]
    if !ok {
        return fmt.Errorf("blocked: file %s has not been read", filePath)
    }
    // Check if editRange is covered by any read range
    // Check if file hash matches (not stale)
    ...
}
```

#### 2. Secrets Scanning on Write

**Problem**: Agents can accidentally write API keys, tokens, passwords to files.
**Pi-lens solution**: Regex-based secrets scanner that runs BEFORE the write completes. Blocking — aborts the write if credentials detected. Filters false positives (test files, env var names, HTTP headers).

**Our adaptation**: Add a pre-write hook to `file_write` and `file_edit` tools:

```go
// In tool execution, before writing
func scanForSecrets(content string, filePath string) error {
    patterns := []struct{
        name    string
        pattern *regexp.Regexp
    }{
        {"AWS key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
        {"GitHub token", regexp.MustCompile(`gh[ps]_[A-Za-z0-9_]{36}`)},
        {"private key", regexp.MustCompile(`-----BEGIN (RSA |EC )?PRIVATE KEY-----`)},
        {"generic secret", regexp.MustCompile(`(?i)(password|secret|token|api_key|apikey)\s*[:=]\s*['"][^'"]{8,}['"]`)},
    }
    // Skip test files, .env.example, etc.
    if isTestFile(filePath) || isExampleFile(filePath) {
        return nil
    }
    for _, p := range patterns {
        if p.pattern.MatchString(content) {
            return fmt.Errorf("blocked: potential %s detected in %s", p.name, filePath)
        }
    }
    return nil
}
```

#### 3. Auto-Format After Edits

**Problem**: Agent writes code with inconsistent formatting. Next read shows ugly diffs.
**Pi-lens solution**: Deferred formatting — queue files during the turn, format once at turn end. Supports 26 formatters with config-gated detection (only runs if project has relevant config).

**Our adaptation**: After `file_write`/`file_edit` completes, optionally run the project's formatter:
- Go: `gofmt` / `goimports`
- Python: `ruff format` / `black`
- TypeScript: `prettier` / `biome`
- Rust: `rustfmt`

Could be a simple post-edit hook in the sandbox:
```bash
# After file_write completes
case "$FILE_EXTENSION" in
    go) gofmt -w "$FILE_PATH" ;;
    py) ruff format "$FILE_PATH" 2>/dev/null ;;
    ts|tsx|js|jsx) npx prettier --write "$FILE_PATH" 2>/dev/null ;;
esac
```

#### 4. Turn-Based Lifecycle

**Pi-lens lifecycle**:
- `session_start`: Detect project, install tools, warm caches
- `tool_result` (write/edit): Run pipeline on the changed file
- `turn_end`: Deferred formatting, test runs, dependency analysis, cascade diagnostics
- `agent_end`: Summary notification

**Our adaptation**: Our agent loop already has round-based execution. Add lifecycle hooks:
- **Session start**: Detect project language, pre-warm sandbox tools
- **Post-tool hook** (after file_edit/file_write): Run format + lint
- **Post-delegation hook** (after delegate_to returns): Run review if configured
- **Session end**: Summary of changes, test status

#### 5. Content-Hash Deduplication

**Problem**: Running the same lint/format pipeline on unchanged content wastes time.
**Pi-lens solution**: Hash the file content before running the pipeline. If the hash matches the last run, skip.

**Our adaptation**: Simple optimization — hash file content before running post-edit hooks:
```go
type EditCache struct {
    hashes map[string]string // file -> last processed hash
}

func (ec *EditCache) ShouldProcess(filePath, content string) bool {
    hash := md5.Sum([]byte(content))
    hashStr := hex.EncodeToString(hash[:])
    if ec.hashes[filePath] == hashStr {
        return false // already processed this exact content
    }
    ec.hashes[filePath] = hashStr
    return true
}
```

#### 6. Review Graph / Impact Cascade

**Problem**: When an agent edits file A, it may break imports in file B, C, D — but nobody checks.
**Pi-lens solution**: Builds a dependency graph during session. At turn end, renders which files were affected and how diagnostics propagated.

**Our adaptation**: For our code-editing workflow (marcus), after implementation completes, the reviewer agent could:
1. Check which files were modified
2. For each modified export, find all importers
3. Verify importers still compile/pass
4. Report cascade impacts

This is a lighter-weight version — we don't need the full LSP integration.

#### 7. Agent Behavior Warnings

**Problem**: Agents sometimes thrash — edit the same file 5+ times in a loop without converging.
**Pi-les solution**: Track edit frequency per file. If the same file is edited >N times in a turn, show a behavior warning.

**Our adaptation**: Track edit count per file in the agent loop. After N edits to the same file:
```go
const maxEditsPerFile = 5

func (al *AgentLoop) checkEditThrashing(filePath string) {
    al.editCounts[filePath]++
    if al.editCounts[filePath] >= maxEditsPerFile {
        // Inject a warning into the agent's next context
        al.injectWarning(fmt.Sprintf(
            "You have edited %s %d times. Consider stopping to reassess.",
            filePath, al.editCounts[filePath]))
    }
}
```

---

### What NOT to Borrow from Pi-Lens

Pi-lens is a Pi extension running in the same process as the agent. It intercepts tool calls at the framework level. We run a **separate Go backend** with Docker sandboxes. Some pi-lens patterns don't map:

1. **LSP integration** — Too heavy for sandbox environments. Our agents edit files in Docker containers, not on the host. LSP requires persistent processes.
2. **37 language servers** — Overkill for our use case. Our agents primarily write Go, TypeScript, Python.
3. **ast-grep/tree-sitter as tools** — Cool but not our priority. Our agents use grep/read for code analysis.
4. **Widget system** — We have our own TUI. Pi-lens renders into Pi's TUI.
5. **Cascade diagnostics** — Requires a full dependency graph. Overkill for most tasks.

### Summary: What to Actually Build

From pi-lens, the highest-value, lowest-effort additions to our orchestrator:

| Priority | Feature | Effort | Impact |
|----------|---------|--------|--------|
| 1 | **Read-before-edit guard** | Medium | Prevents blind edits, huge quality win |
| 2 | **Secrets scanning** | Low | Prevents credential leaks |
| 3 | **Edit thrashing detection** | Low | Catches infinite loops early |
| 4 | **Auto-format after edit** | Medium | Cleaner output, fewer review cycles |
| 5 | **Content-hash dedup** | Low | Performance optimization |
| 6 | **Post-delegation review hook** | Medium | Automatic quality gate |

Items 1-3 can be built entirely in Go as middleware in the agent loop. Items 4-6 require sandbox integration.

---

## Combined Roadmap

### Phase 1: New Roles + Read Guard (Week 1-2)
- Add `scout`, `planner`, `reviewer` roles
- Add `inspect.yaml` tool package
- Add read-before-edit tracking and validation
- Add secrets scanning on write

### Phase 2: Chain Pipeline + Intercom (Week 3-4)
- Add `delegate_chain` tool with template variables
- Add `delegate_parallel` tool with concurrency control
- Add `ask_supervisor` tool for mid-task clarification
- Add completion mutation guard

### Phase 3: Watchdog + Quality Gates (Week 5-6)
- Add activity watchdog with configurable thresholds
- Add edit thrashing detection
- Add auto-format post-edit hooks in sandbox
- Add oracle and context-builder roles

### Phase 4: Polish (Week 7-8)
- Context fork filtering
- Agent-as-markdown format support
- Content-hash dedup
- Production readiness checks for code output

---

## Part C: TUI Improvements from Pi-Subagents

**Key finding**: Pi-subagents' TUI is NOT a standalone TUI — it's a Pi extension that plugs into the base Pi TUI via `ctx.ui.setWidget()` and `ctx.ui.custom()`. It only adds sub-agent-specific rendering on top of the same `@earendil-works/pi-tui` framework we use (`@mariozechner/pi-tui` fork). Our TUI base is fine. What we need are specific rendering components for delegation results.

### What Pi-Subagents' TUI Does (render.ts — 1249 lines)

**`renderSubagentResult()`** — Main delegation result renderer:
- Status glyphs: spinner/ok/failed/detached/warning per agent
- Agent name + `[fork]` context badge
- Live progress: tool count, tokens, duration
- Last 3 tool calls with args preview
- Last 5 lines of output
- Artifacts paths, session files, model fallback chain
- Compact vs expanded (Ctrl+O) views

**`renderWidget()`** — Persistent async job status bar:
- Animated spinner (80ms) for running background agents
- Running/queued/finished grouping with tree-style `├─/└─` connectors
- Line budget fitting (adapts to terminal height)
- Chain vs parallel mode rendering

**`ChainClarifyComponent`** (chain-clarify.ts — 1333 lines) — Pre-flight editor:
- Full-screen boxed overlay with `╭─╮│╰─╯` borders
- Three modes: single, parallel, chain
- Per-step editing: task template, output path, reads, model, thinking level, skills
- Inline text editor with cursor, word navigation, scrolling
- Model selector with fuzzy search + thinking level selector
- Skill selector with checkboxes
- Output propagation: changing step N's output auto-updates step N+1's reads
- Background toggle (`[b]`)

**Control notices** — Watchdog alerts:
- Idle (60s), long-running (240s), failed tools (3 attempts)
- Debounced foreground delivery (1s)

### Our Current Gap

Our `ToolExecutionComponent` (tool-execution.ts) renders delegation as:
```
● delegate_to: marcus: fix the bug in auth.go
  [output text]
```

No sub-agent tree. No progress tracking. No chain visualization. No parallel display. No async job widget.

### What to Add to Our TUI

#### 1. Sub-Agent Result Tree in `ToolExecutionComponent`

When `delegate_to` or `delegate_async` returns, detect if the result contains sub-agent details and render a tree:

```
✓ chain · scout → planner → worker · 3 steps · 47 tools · 12.4k tok · 2m14s
  ✓ Step 1: scout · ok · 12 tools · 34s
    ⎿  Found 8 files related to authentication...
  ✓ Step 2: planner · ok · 5 tools · 28s
    ⎿  Plan written to plan.md
  ✓ Step 3: worker · ok · 30 tools · 1m12s
    ⎿  Fixed auth bug, added test, all passing
```

Implementation: Extend `ToolExecutionComponent.updateResult()` to detect delegation results with structured `details` containing agent status/progress. Add a `renderSubagentTree()` method.

#### 2. Async Job Widget

A persistent widget showing `delegate_async` background tasks:

```
● Async agents · background
  ├─ ⠋ chain (research → write) · running · 23 tools · 45s
  │    ⎿  Running: web_search("auth patterns")
  └─ ✓ single (worker) · done · 14 tools · 1m02s
       ⎿  Output: /tmp/artifacts/output.md
```

Implementation: Add to the TUI footer/header area or a collapsible sidebar. Track async jobs in state, update via SSE events.

#### 3. Chain Visualization for Pipeline Results

When using `delegate_chain` (proposed in Phase 2), show the pipeline flow:

```
scout → planner → worker → reviewer
done    done      running   pending
```

Implementation: Part of the sub-agent tree rendering above, with horizontal flow display for chain mode.

#### 4. Pre-Flight Confirmation (Optional, Later)

Before running a chain/pipeline, show a confirmation overlay:

```
╭─ Chain: scout → planner → worker ───────────────────────────╮
│ Original Task: Fix the auth bug in login.go                  │
│                                                              │
│ ▶ Step 1: scout                                              │
│     task: Survey the codebase for auth-related files         │
│     model: default                                           │
│                                                              │
│   Step 2: planner                                            │
│     task: Create implementation plan based on {previous}     │
│     model: default                                           │
│                                                              │
│   Step 3: worker                                             │
│     task: Implement the plan from {previous}                 │
│     model: default                                           │
│                                                              │
╰─ [Enter] Run • [Esc] Cancel • ↑↓ Navigate ─────────────────╯
```

This is the `ChainClarifyComponent` pattern — adapt to our Go backend's configuration.

### What NOT to Add

- **Model/thinking/skill selectors** — We don't expose per-step model selection to users
- **Reads/writes path editing** — Our chain steps don't have explicit file dependencies
- **Fork context badges** — We don't have the fork/fresh context distinction yet

### TUI Implementation Priority

1. **High**: Sub-agent result tree (extends existing `ToolExecutionComponent`)
2. **High**: Async job widget (new component, tracks `delegate_async` state)
3. **Medium**: Chain flow visualization (when delegate_chain lands)
4. **Low**: Pre-flight confirmation overlay (nice-to-have, requires UX design)
