# Pux Prompt System

How prompts are assembled at every level, from the shared base down to
per-subagent overrides. Each layer stacks on top of the previous one.

## Assembly order

```
root AGENTS.md (project-level, if present)
  → orgs/general/AGENTS.md (base org prompt — Pux operating principles)
    → orgs/specialists/<org>/AGENTS.md (specialist CTO overlay)
      → profile.yaml system_prompt_suffix (org-wide, every agent)
        → ask_user suffix (if HITL enabled)
          → dynamic-tools suffix (if dynamic tools enabled)
```

This chain is assembled declaratively in `prompt_parts.py` — an ordered
registry of parts, not inline string concatenation. Each part is a named
element with a build function; `build_stack` feeds a `PromptContext`
through the registry to produce the final system prompt.


## Layer 1: Root AGENTS.md

**File:** `AGENTS.md` (project root)

Project-level instructions loaded by `load_root_prompt`. Present in every
org's prompt. Use for repo-wide conventions (coding standards, commit
message rules, "verify or die" discipline).

Rarely the right place for org-specific behavior — use layers 2-4 instead.


## Layer 2: Base org prompt

**File:** `orgs/general/AGENTS.md`

The foundation every org starts from. Contains:
- Pux identity ("you are driving Pux")
- Orchestrator/CTO pattern (the supervisor delegates, doesn't do everything)
- Delegation rules (task tool, subagent isolation, context discipline)
- Operating principles

A specialist org inherits this automatically via `extends: general` in
its `org.yaml`. The base is **additive** — the specialist overlay
appends after it, never replaces it.


## Layer 3: Specialist overlay (the CTO personality)

**File:** `orgs/specialists/<org>/AGENTS.md`

The org's CTO overlay. Appended after the base prompt. This is where the
org's identity lives: mission, operating modes, domain rules, voice,
quality standards, stopping criteria.

Example (`coder/AGENTS.md`): the Claude-Code-equivalent coding org —
defines risk-tiered autonomy, think-aloud-then-act, the ship gate,
error recovery.

**To change what the CTO does:** edit this file.


## Layer 4: Profile suffix (org-wide standing rules)

**File:** `orgs/<org>/profile.yaml`

```yaml
system_prompt_suffix: |
  Always write tests before implementation.
  Never skip verification.
  Commit after each logical unit of work.
```

Appended to **every agent in the org** — the CTO AND every subagent.
This is the non-invasive way to add standing rules without touching
AGENTS.md. The suffix comes AFTER the full AGENTS.md chain.

Other profile.yaml knobs that shape behavior without editing prompts:

| Field | Effect |
|-------|--------|
| `system_prompt_suffix` | Text appended to every agent's prompt |
| `excluded_tools` | Drops named tools from the surface |
| `tool_description_overrides` | Rewrites a tool's description |
| `middleware` | Adds/removes middleware (routing, audit, rubric, etc.) |
| `models` | Per-role model overrides (base, grader, worker) |
| `ask_user` | Enables human-in-the-loop interrupts |
| `rubric` | Quality gate (grader runs after CTO finishes) |
| `general_purpose_subagent.enabled` | Neuter the auto-added GP slot |

**To add a rule that applies to all agents:** edit `profile.yaml`.


## Layer 5: Subagent definitions

**File:** `orgs/<org>/agents/<slug>.md` (org-local)
**File:** `orgs/_shared/agents/<slug>.md` (cross-org shared)

```markdown
---
name: "coder-explorer"
description: "Recon specialist for codebase exploration"
tools: [read_file, glob, grep]
model: mimo-v2.5
---
You are a recon specialist. Read code, map structure, report back.
Never modify files. Your job is reconnaissance, not implementation.
```

**Frontmatter** controls identity (`name`, `description`), tool surface
(`tools`), model (`model`), and optional middleware.

**Body** is the subagent's system prompt — its personality and rules.

Cross-org shared agents live in `orgs/_shared/agents/`. An org overrides
a shared agent by dropping a same-named `<slug>.md` in its own `agents/`
directory. The org-local version wins.

**To change a subagent's behavior:** edit its `.md` file.


## Layer 6: Skills (contextual prompt modules)

**File:** `orgs/<org>/skills/<skill>/SKILL.md`

Skills are on-demand prompt modules the agent discovers and loads when
relevant to a task. Unlike layers 1-5, skills are NOT always in the
prompt — they're loaded contextually based on what the agent is doing.

A skill directory contains:
- `SKILL.md` — the skill prompt + metadata
- Optional reference files, scripts, templates

Example: `game-studio/skills/game-studio-workflows/` carries character
pipeline, scene construction, and asset workflow skills.


## Layer 7: tool_surface (supervisor scoping)

**File:** `orgs/<org>/policy.yaml`

```yaml
tool_surface:
  groups: [code, browser]
```

Scopes the **supervisor's** specialist tool surface by capability group
(`code` / `skills` / `media` / `browser` / `desktop`) or bare specialist
slugs. Anything not listed is dropped from the supervisor's own tool list.
Subagents still resolve their own `tools:` allowlist against the FULL
surface — this only affects what the CTO sees directly.

Absent/empty → supervisor carries every specialist (the default).


## Roster: who the org delegates to

**File:** `orgs/<org>/org.yaml`

```yaml
extends: general          # inherit parent's roster
agents:                   # the org's specialist subagents
  - coder-explorer
  - code-worker
  - web-agent
capabilities:             # CU-4: foreign MCP tool-server declarations
  - {kind: mcp, ref: web_research}
```

`extends:` inherits a parent's roster (and its agents). An org can add
or remove specialists from the inherited roster. The `capabilities:`
block declares foreign MCP tool servers (catalog refs into
`orgs/_shared/tool_servers.yaml`).


## Quick reference: what to edit

| Goal | Edit |
|------|------|
| Change the CTO's personality/behavior | `orgs/specialists/<org>/AGENTS.md` |
| Add a rule for ALL agents in an org | `orgs/<org>/profile.yaml` → `system_prompt_suffix` |
| Change a specific subagent's behavior | `orgs/<org>/agents/<slug>.md` (body) |
| Change a subagent's tools | `orgs/<org>/agents/<slug>.md` (frontmatter `tools:`) |
| Change which specialists an org delegates to | `orgs/<org>/org.yaml` (roster) |
| Scope the supervisor's own tool surface | `orgs/<org>/policy.yaml` → `tool_surface.groups` |
| Override a shared agent for one org | Create `orgs/<org>/agents/<slug>.md` |
| Add a contextual skill | `orgs/<org>/skills/<skill>/SKILL.md` |
| Add a foreign MCP tool server | `orgs/_shared/tool_servers.yaml` + `orgs/<org>/org.yaml` `capabilities:` |
| Create a new org | `orgs/specialists/<new>/` with AGENTS.md + org.yaml + profile.yaml |
| Repo-wide conventions | `AGENTS.md` (project root) |


## Inheritance chain example

For org `coder` (`extends: general`):

```
1. AGENTS.md (root)                           — repo conventions
2. orgs/general/AGENTS.md                     — Pux base (orchestrator pattern)
3. orgs/specialists/coder/AGENTS.md           — CTO overlay (coding discipline)
4. orgs/specialists/coder/profile.yaml        — suffix (verify-or-die, voice)
   → system_prompt_suffix                     — appended to CTO + all subagents
5. Per-subagent: orgs/specialists/coder/
   agents/coder-explorer.md                   — recon specialist prompt
6. Skills: loaded contextually at runtime
```

The final supervisor prompt is layers 1-4 concatenated. Each subagent
gets layers 1-2 (base) + its own `.md` body + the org-wide suffix from
layer 4.
