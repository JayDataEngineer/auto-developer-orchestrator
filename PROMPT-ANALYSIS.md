# Prompt System Analysis

A comparison of four prompt architectures: Pux (ours), Claude Code, OpenCode, and Pi-Mono.

---

## 1. Architecture & Composition Model

| Dimension | Pux (Ours) | Claude Code | OpenCode | Pi-Mono |
|---|---|---|---|---|
| Composition | Go `text/template` — declarative, config-driven | Programmatic `getSystemPrompt()` assembles `string[]` | Hardcoded Go string constants (`baseAnthropicCoderPrompt`) | `buildSystemPrompt(options)` — programmatic TS |
| Sub-agents | **Built-in to prompt**: CTO delegates to role-based employees | Opt-in **coordinator mode** as separate prompt path | None (single-agent) | None (single-agent) |
| Dynamic sections | Template vars: `{{.Agents}}`, `{{.Tools}}`, `{{.Skills}}` | `systemPromptSections()` registry — memoized, cacheable | Appends env info + context files | Appends context files, skills, date |

## 2. Prompt Caching

Claude Code is the only system with prompt caching:

- **`SYSTEM_PROMPT_DYNAMIC_BOUNDARY`** marker splits cacheable (static identity, tone, style) from non-cacheable (MCP, memory, env info)
- Section registry with **memoized `systemPromptSection()`** + `DANGEROUS_uncachedSystemPromptSection()` for volatile content
- Cache cleared on `/clear` and `/compact`

**Pux**: No caching — template re-rendered every turn.
**OpenCode**: No caching.
**Pi-Mono**: No caching.

Claude Code's approach saves significant tokens per turn (the static prefix survives prompt cache key, only dynamic sections bust it).

## 3. Tool Prompt Strategy

| System | Approach | Granularity |
|---|---|---|
| Claude Code | **Per-tool `prompt.ts`** — BashTool: 369 lines, AgentTool: 287 lines. Rich guidance incl. git workflows, prose. | Very high |
| Pux | Tools listed in `capability.yaml` by name + natural-language `SKILL.md`. `{{.Tools}}` in template. | Medium |
| Pi-Mono | One-liner "snippets" injected into system prompt. | Low |
| OpenCode | Tool guidance inline in main prompt (`# Tool usage policy`). | Low |

## 4. Prompt Organization

**Pux — Most config-driven:**
```
config/
  prompt.md              ← Go template (93 lines)
  workers/*.yaml          ← 7 role definitions (persona, capabilities, model, temp)
  capabilities/*/         ← 6 capability packages
    capability.yaml       ← tool list, MCP server refs, sandbox tier
    SKILL.md              ← natural language instructions
```

**Claude Code — Most programmatic:**
```
constants/
  prompts.ts              ← getSystemPrompt() builder (914 lines)
  system.ts               ← identity constants
  systemPromptSections.ts ← memoized section registry
tools/*/prompt.ts         ← per-tool descriptions
utils/systemPrompt.ts     ← buildEffectiveSystemPrompt() resolver
```

**OpenCode — Hardcoded constants:**
```
internal/llm/prompt/
  coder.go                ← baseAnthropicCoderPrompt + baseOpenAICoderPrompt
  prompt.go               ← GetAgentPrompt() dispatcher
  task.go, title.go, summarizer.go ← smaller sub-prompts
```

**Pi-Mono — File-based extender:**
```
packages/coding-agent/src/core/
  system-prompt.ts        ← buildSystemPrompt() (168 lines)
  skills.ts               ← loads SKILL.md from directories
  prompt-templates.ts     ← loads .md prompt templates
  resource-loader.ts      ← discovers AGENTS.md, CLAUDE.md, .pi/*
```

## 5. Identity & Tone

| System | Opening Identity | Tone |
|---|---|---|
| Pux | "You are Pux — the CTO. You dispatch employees to do work." | Terse, directive, no preamble |
| Claude Code | "You are Claude Code, Anthropic's official CLI for Claude." | Professional, careful, comprehensive |
| OpenCode | "You are OpenCode, an interactive CLI tool that helps users with SE tasks." | Extremely concise, example-driven |
| Pi-Mono | "You are an expert coding assistant operating inside pi." | Neutral, matter-of-fact |

## 6. Delegation / Multi-Agent

- **Pux**: **Prompt-native delegation.** CTO persona exists *to* delegate. `delegate_to`/`delegate_async`/`collect_results`/`yield_artifact`. Employees defined in YAML with distinct tool sets. Uniquely role-granular.
- **Claude Code**: Coordinator mode is a separate prompt path. Worker tools limited to a subset. Coordinator synthesizes → writes spec → delegates. Not default.
- **OpenCode**: No delegation. Single agent.
- **Pi-Mono**: No delegation. Single agent. (mom — Slack bot — is a separate binary, not sub-agent).

## 7. Skills System

| System | Mechanism |
|---|---|
| Pux | `{{.Skills}}` injected from `skills.go` — available-skills XML block. Uses `skill` tool to load. |
| Claude Code | SkillTool with budget (`SKILL_BUDGET_CONTEXT_PERCENT = 0.01`), discoverable skills, auto-surfaced each turn |
| Pi-Mono | Filesystem: SKILL.md discovered in directories, formatted as `<available_skills>` XML |
| OpenCode | None |

## 8. Output Style & Tone Calibration

- **Claude Code**: Has `OutputStyleConfig` — separate prompt files that modify tone (Explanatory, Learning, custom). Per-model override section.
- **Pux**: No output style system. Tone hardcoded in template.
- **OpenCode**: Tone hardcoded with extreme terseness (4-line max, example-driven).
- **Pi-Mono**: Tone minimal ("Be concise") with no style system.

## 9. Per-Model Behavioral Tuning

- **Claude Code**: Ant-model-override section, per-model knowledge cutoff dates, model-specific beta headers.
- **Pux**: Stub `config/profiles.yaml` (marked "NOT YET ACTIVE"). Per-worker model overrides exist in YAML but no behavioral tweaks.
- **OpenCode**: Provider branching (`baseOpenAICoderPrompt` vs `baseAnthropicCoderPrompt`).
- **Pi-Mono**: Minimal — thinking level, model selection.

## 10. Project Context Ingress

| System | Mechanism |
|---|---|
| Claude Code | `loadMemoryPrompt()`, CLAUDE.md, session memory from `/compact` |
| OpenCode | Configurable `contextPaths` (CLAUDE.md, OpenCode.md, .cursor/rules/) |
| Pi-Mono | AGENTS.md, CLAUDE.md, `contextFiles` from `ResourceLoader` |
| Pux | `{{.ProjectContext}}` from `pux.yaml`, `SystemPromptAddon` in `.pi/settings.json` |

---

## Strengths & Weaknesses

### Pux's Strengths

1. **Config-driven design.** Adding employee = YAML file + capability dir. No code changes. Most extensible.
2. **Role-based delegation.** Unique among the four. CTO/employee split is the prompt's core identity.
3. **Capability packaging.** Clean separation of tool definitions (`capability.yaml`) from instructions (`SKILL.md`). MCP server support as first-class.
4. **Template + YAML = low barrier.** Configure without writing Go.

### Pux's Weaknesses vs Claude Code

1. **No prompt caching.** Claude Code's `SYSTEM_PROMPT_DYNAMIC_BOUNDARY` + section registry saves tokens per turn. Pux re-renders entire prompt.
2. **No per-model behavioral profiles.** `profiles.yaml` is a stub. Claude Code has model-specific adjustments.
3. **No output style system.** Can't offer "explanatory" or "learning" mode via config.
4. **Less sophisticated tool descriptions.** Claude Code's 369-line BashTool prompt embeds git workflows, PR instructions, sandbox guidance.
5. **Section-level granularity.** Claude Code independently caches/regenerates sections. Pux is all-or-nothing.

### Pux's Weaknesses vs OpenCode

1. **OpenCode's prompt is tighter.** Extreme conciseness enforcement (4-line max, example-driven).
2. **Context ingestion.** Configurable paths, LSP integration instructions, directory listing.

### Pux's Weaknesses vs Pi-Mono

1. **Prompt template system.** Pi-mono's `.pi/prompts/*.md` with arg substitution (`$1`, `$@`) is powerful.
2. **Simplicity.** Pi-mono's `buildSystemPrompt()` is 168 lines. Pux's composition chain spans 7+ files.

---

## Proposed Direction: Streamlined Claude Code Pattern

### Core Idea: Section Pipeline + Cache Boundary

```
[Stable sections] → BOUNDARY → [Inherited sections] → [Volatile sections]
 cached globally      |       cached per-session       never cached
```

### Section Plan

| Section | Stability | Content |
|---|---|---|
| `identity` | Stable | "You are Pux, the CTO." |
| `system` | Stable | Tool display rules, system reminders |
| `delegation` | Stable | delegate_to/delegate_async mechanics |
| `communication` | Stable | Terse, no preamble, no "I'll help" |
| `actions` | Stable | Reversibility, blast radius, risky ops |
| `planning` | Stable | Plan protocol |
| `artifacts` | Stable | yield_artifact handoff |
| `paths` | Stable | Sandbox paths |
| `employees` | Inherited | Worker roster (per-project) |
| `mcp_instructions` | Inherited | MCP server guidance |
| `environment` | Volatile | CWD, git status, date, platform |
| `skills` | Volatile | Available skills block |
| `project_context` | Volatile | pux.yaml manifesto + context |
| `sandbox_id` | Volatile | Sandbox identifier |

### Assembly

```go
func (b *Builder) Build(ctx *Context) string {
    var parts []string
    for _, s := range b.sections {
        content := b.cachedOrCompute(s, ctx)
        parts = append(parts, content)
        if s.Stability == Stable && isLastStable(s) {
            parts = append(parts, BOUNDARY)
        }
    }
    return strings.Join(parts, "\n\n")
}
```

### What Changes vs Current

| Current | After |
|---|---|
| `config/prompt.md` single template | `config/prompt_sections/*.md` — one file per section |
| `common.go` prompt builder | `PromptBuilder` with cache management |
| `BuildOrchestratorPrompt()` | `builder.Build(ctx)` |
| Tool descriptions in Go registry | Each tool registers `Prompt() string` method |

### What's Dropped from Claude Code

- No `USER_TYPE === 'ant'` branches (internal-only, irrelevant)
- No coordinator mode (ours is built-in, native)
- No per-model knowledge cutoffs (models change too fast)
- No growthbook/feature flags in prompt assembly
- No circular dependency hacks
- No 369-line BashTool prompt (git workflows → delegate to code_ops employee)

### Subagent Isolation

Subagents are **untouched** by this change. The boundary/caching layer is entirely in the CTO prompt pipeline:

```
CTO prompt pipeline                 Subagent prompt pipeline
──────────────────                  ─────────────────────
section pipeline + cache            BuildWorkerPrompt() — unchanged
config/prompt_sections/*.md         config/workers/*.yaml
                                    config/capabilities/*/SKILL.md
```

Editing a subagent = still `workers/<name>.yaml` + `capabilities/<name>/SKILL.md`. The caching layer is invisible from the config writer's perspective.

---

## Critical Bug: Prompt Bleed Between CTO and Subagents

### The Problem

Currently the CTO sees `role.Description` (the `persona` field) in `FormatAgentList()`. The subagent also gets the same `persona` field injected by `BuildWorkerPrompt()`. They share the same string.

```
role.Description (persona)  →  shown to CTO via FormatAgentList()
                            →  injected into subagent via BuildWorkerPrompt()
                            ← SAME STRING
```

This works fine while personas are short one-liners. But the moment someone writes a full workflow into the persona — step-by-step instructions, tool usage patterns, decision trees — that entire workflow bleeds into the CTO's context. The CTO doesn't need to know *how* an employee works, just *what* they can do.

This is the key difference vs Claude Code: Claude Code's tool prompts are bloated (369-line BashTool), but they're bloated in the *worker's* context, not the coordinator's. Our system accidentally shares both contexts.

### The Fix: Three-Level Prompt Separation

| Level | Field | Who sees it | Format | Example |
|---|---|---|---|---|
| **Hint** | `hint` | CTO only | 1 line | "Write, test, and modify code" |
| **Persona** | `persona` | Subagent only | Short paragraph | "You are a Senior Developer — precise, thorough..." |
| **Instructions** | SKILL.md | Subagent only | Full detail | Workflows, tool patterns, decision trees |

`FormatAgentList()` renders **only** `hint`. Never `persona`, never SKILL.md content.
`BuildWorkerPrompt()` gets `persona + SKILL.md`. Never sees `hint`.

### Before (current)

```yaml
# config/workers/code_ops.yaml
persona: "Senior Developer — write, modify, and test code."
capabilities:
  - code
```

CTO sees: `"Senior Developer — write, modify, and test code."` — OK for now, but no room to grow.

### After (proposed)

```yaml
# config/workers/code_ops.yaml
hint: "Write, test, and modify code"
persona: |
  You are a Senior Developer. You write precise, tested code.
  Always read before editing. Verify after changing.
  Commit only when asked. Follow existing conventions.
capabilities:
  - code
```

CTO sees: `"Write, test, and modify code"` (from `hint`).
Subagent gets: the full `persona` paragraph + `config/capabilities/code/SKILL.md` (36 lines of tool workflows).

### Why This Matters

The three-level split makes it **structurally impossible** for subagent detail to bloat the CTO prompt. No convention to follow — the code enforces it. The `hint` field is explicitly for CTO-facing summary. The `persona` and SKILL.md are explicitly subagent-only.

This means:
- SKILL.md files can grow to arbitrary length without affecting CTO token usage
- Persona can include role-specific tone and behavioral guidance the CTO never sees
- The CTO's employee roster section stays compact regardless of subagent complexity
- Adding a complex workflow to an employee is zero-cost to the orchestrator

### Implementation

```go
// formatRolesList — CTO sees ONLY hint
func formatRolesList(roles map[string]*AgentRole) string {
    ...
    for _, name := range names {
        role := roles[name]
        hint := role.Hint
        if hint == "" {
            hint = role.Description // backwards compat
        }
        fmt.Fprintf(&b, "### %s\n%s\nCapabilities: %s\n\n", role.Name, hint, capability)
    }
    return b.String()
}

// BuildWorkerPrompt — subagent gets persona + SKILL.md (never hint)
func BuildWorkerPrompt(role *AgentRole, skillContent string) string {
    var b strings.Builder
    b.WriteString(role.Description) // persona, NOT hint
    if skillContent != "" {
        b.WriteString("\n\n")
        b.WriteString(skillContent) // SKILL.md
    }
    return b.String()
}
```

Backwards compatible: if `hint` is missing, falls back to `persona` (current behavior).

---

## Claude Code's Bloat Problem (And Why We Avoid It)

Claude Code's system prompt is massive — 5-8K tokens per turn. The static prefix is ~2K tokens, tool descriptions push it further. Their `SYSTEM_PROMPT_DYNAMIC_BOUNDARY` caches the static portion but doesn't shrink it.

The irony: their worker tool prompts (BashTool: 369 lines, AgentTool: 287 lines) mostly externalize things like git commit/PR workflows that *should be delegated to sub-agents*. Our architecture already does this — `code_ops` gets those instructions via SKILL.md, not jammed into a tool description the CTO stares at every turn.

Our design is **intrinsically more compact** because:
1. Role-specific guidance lives on the employee (SKILL.md), not in the orchestrator's prompt
2. The three-level split (hint/persona/instructions) prevents any bleed
3. Tool descriptions stay minimal in the CTO context — they're the employee's concern

Target: CTO static prefix under 500 tokens. Full subagent prompts can be 2-5K tokens each — that's fine, they run in separate contexts.
