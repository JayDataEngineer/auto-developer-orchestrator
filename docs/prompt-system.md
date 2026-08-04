# Pux Prompt System

How prompts are really assembled — the ground-truth model, verified against
`prompt_parts.py`, `orgs.py`, `stack.py`, `kit/loaders.py`, `profile.py`, and
`graph.py`. Every path and part name below is real and current.

If you read one section, read **[The two prompts](#the-two-prompts)**.

---

## The two prompts

There are **two disjoint system prompts** in this system. An experimenter's
first question is always *"which one am I editing?"* — they have different
parts, different rules, and different levers. The registries are disjoint by
construction: no part is scoped to both, so supervisor content can never leak
into a subagent and vice versa.

| | **Supervisor** (the org CTO) | **Subagent** (a delegated specialist) |
|---|---|---|
| Sees root `AGENTS.md` | ❌ never (dev guide only — not injected) | ❌ never |
| Sees the base org prompt (`orgs/general/AGENTS.md`) | ✅ via `extends:` chain | ❌ never |
| Sees the harness addendum | ✅ always (folded into part 1) | ❌ never |
| Sees the dynamic-dispatch notice | ✅ when interpreter mounted | ❌ never |
| Sees its own `.md` body | n/a (no body) | ✅ always |
| Sees the org-wide `system_prompt_suffix` | ✅ | ✅ |
| Sees a per-agent `system_prompt_suffix` | n/a | ✅ if its frontmatter carries one |
| Memory channel (`/memories/AGENTS.md`) | ✅ at startup | only if memory middleware is mounted on subagents |
| Skills metadata | ✅ name + description only | ✅ name + description only (if mounted) |

**The most common experimenter mistake** is editing root `AGENTS.md` expecting
the CTO to pick up the change. It won't — root `AGENTS.md` is a pure dev guide
(Claude Code instruction file); nothing in the runtime reads it. Edit
`orgs/general/AGENTS.md` to change what every CTO sees, or
`orgs/specialists/<org>/AGENTS.md` for one org. Subagents get their `.md` body +
suffixes, nothing else from the base chain.

---

## What lands in the prompt (and what doesn't)

The system prompt is **not just files**. It is assembled from two sources:

- **(A) `assemble_prompt`** — a registry of conditional parts in
  `prompt_parts.py`, joined with `"\n\n"` in order. Parts whose `build()`
  returns `None` are skipped with no trace (the *no-gap* property). **Every
  prose block this registry emits is editable markdown under `orgs/_shared/`**
  (the three `.md` files named in the table below); Python holds only
  safety-net fallback constants used when a file is absent.
- **(B) Middleware injection** at bind/startup — memory, skills metadata, tool
  docstrings, and the `description:` routing signal.

If you account for every file and the model still behaves unexpectedly, the
cause is in (B), not (A).

---

## Part A — the assembled prompt

### Supervisor (CTO) — 4 parts, in this exact order

| # | Part name (`prompt_parts.py`) | Source | Condition |
|---|---|---|---|
| 1 | `agents_md_core` | extends-chain overlay + **harness addendum** | always-on |
| 2 | `org_system_prompt_suffix` | `profile.yaml → system_prompt_suffix` | present |
| 3 | `ask_user_suffix` | `orgs/_shared/ask_user_suffix.md` (falls back to `hitl.ASK_USER_PROMPT_SUFFIX`) | `ask_user` active AND turn-based transport |
| 4 | `dynamic_dispatch_suffix` | `orgs/_shared/dynamic_dispatch_suffix.md` (falls back to `_DYNAMIC_DISPATCH_SUFFIX`) | `CodeInterpreterMiddleware` actually mounted (strength-`pro` base, or explicit `add: [interpreter]`) |

**Part 1 expands to** (in this order, joined `"\n\n"`):
```
orgs/general/AGENTS.md          (Pux base — orchestrator pattern)
  → orgs/specialists/<org>/AGENTS.md (CTO overlay)
  → orgs/_shared/harness_addendum.md (deepagents delegation rules — wins on conflict)
```
The addendum is folded into part 1 (not a separate part) to preserve a
byte-identical seam. It is **editable markdown** at `orgs/_shared/harness_addendum.md`
— change the delegation instructions, tool-surface map, or workspace path
there and every org's CTO picks it up with zero code edits. The embedded
`prompt_parts._ADDENDUM` constant is the safety-net fallback (used only by
minimal fixtures / packed archives that omit `_shared/`).

Parts 3 and 4 are likewise **editable markdown** —
`orgs/_shared/ask_user_suffix.md` (the turn-ending HITL instruction) and
`orgs/_shared/dynamic_dispatch_suffix.md` (the eval-tool dispatch strategy).
Both fall back to their embedded Python constants (`hitl.ASK_USER_PROMPT_SUFFIX`,
`prompt_parts._DYNAMIC_DISPATCH_SUFFIX`) when the file is absent. **100% of
prompt prose is now editable from the `orgs/` tree** — no Python edit required
to change any block that reaches the model.

### Subagent — 3 parts, in this exact order

| # | Part name (`prompt_parts.py`) | Source | Condition |
|---|---|---|---|
| 1 | `agent_body` | the agent's (extends-merged) `.md` body. Deepagents prepends its own short `DEFAULT_SUBAGENT_PROMPT` ("you have standard tools; the caller sees only your final message; ensure it contains the complete answer") at bind time — this is outside pux's assembly but the model does see it. | always-on |
| 2 | `org_system_prompt_suffix` | same `profile.yaml` field as the supervisor | present |
| 3 | `agent_system_prompt_suffix` | the agent's **own frontmatter** `system_prompt_suffix` | present in frontmatter |

A subagent **never** sees: root `AGENTS.md`, the base org prompt, the harness
addendum, the orchestrator pattern, or the dynamic-dispatch notice. Only its
specialization + the two suffixes.

Precedence (most-specific wins): `.md` body → org-wide suffix → per-agent suffix.

---

## Part B — parallel injection channels (NOT in `assemble_prompt`)

Four more channels add content to what the model sees, at bind/startup time.
These are the answer to *"I accounted for every file but the model still
behaves unexpectedly."*

| Channel | What it adds | Who sees it | Source |
|---|---|---|---|
| **Memory** | The body of `/memories/AGENTS.md`, injected at startup | supervisor (always); subagents only if memory middleware is mounted on them | `pux_harness.memory.MEMORY_SOURCES` |
| **Skills metadata** | Each skill's `name` + `description` **only**. The `SKILL.md` body is fetched on demand via `read_file` when the agent decides to load it. | supervisor (via `SkillsMiddleware`); subagents if mounted | `graph.py:142` |
| **Tool docstrings** | Every armed tool's schema/docstring becomes part of the tool surface | every agent | deepagents bind |
| **`description:` routing** | Each subagent's `description:` frontmatter is joined into `"- {name}: {description}"` and injected **verbatim** into the `task` tool's own description — that is what the CTO reads to pick which subagent to delegate to. **Verbose descriptions tax every CTO turn.** | supervisor, every turn | `deepagents/middleware/subagents.py` |

### Implications

- **`description:` is a routing signal, not a capability list.** It must answer
  *"when do I pick THIS agent?"* — nothing more. Capability prose belongs in the
  `.md` body, which is in context only when the agent runs.
- **Don't re-describe tools in prompt prose.** The model already sees every
  tool's docstring. Restating mechanics in `AGENTS.md` or an agent body is
  duplicate source-of-truth (and drifts).
- **SKILL.md bodies are NOT auto-injected.** Only the name + description land
  in the prompt. The body is loaded on demand via `read_file`. A 500-line
  `SKILL.md` costs zero prompt tokens until the agent explicitly reads it.

---

## Inheritance — three files, three different rules

`extends: general` (in `org.yaml`) is primarily a **roster** inheritance. The
prose-inheritance rules differ per file:

| File | Inheritance rule |
|---|---|
| `AGENTS.md` overlay | **Blind concatenation.** Parent body + own body, own-last, joined `"\n\n"`. A child cannot override a section of the parent's prose — it can only append. |
| `profile.yaml` | **Deep-merged** root → child. Child wins on conflicts. `system_prompt_suffix` / `models` / `middleware` deltas accumulate. |
| `policy.yaml` | **Never inherited.** Each org owns its own egress + `tool_surface`. |
| Agent `.md` files | **Per-agent `extends:`** supported via `_merge_extends` (`kit/loaders.py`): `name`/`description`/`model` delta-wins; `tools` additive (`tools_add`/`tools_remove`) or full-replace; body = base body + delta body joined `"\n\n"`. |

---

## Constraints (things you cannot do — by design)

| Want | Answer |
|---|---|
| Replace the whole base prompt | ❌ `base_system_prompt` is a **permanent contract failure** in `profile.validate_profile` + a runtime guard in `stack.build_stack`. It was a global-REPLACE that nuked the assembly. Don't try to revive it. |
| Remove or refine a base section from a specialist overlay | ❌ Overlay is blind concat. You can only append. (Named-section inheritance is rework direction D4 in `prompt-construction-review.md` — not landed.) |
| Reorder parts of the prompt | ❌ Order is fixed in the registry tuple (`SUPERVISOR_PROMPT_PARTS` / `SUBAGENT_PROMPT_PARTS`). |
| Suppress a base rule for one org | ⚠️ Only by appending a counter-rule in your overlay or suffix. The base text still reaches the model. |
| Make a subagent see the base prompt | ❌ Registries are disjoint by construction. Put the rule in the agent's `.md` body or the org-wide suffix. |

---

## The safe levers, ordered by how local the effect is

1. **Per-agent `system_prompt_suffix`** (agent frontmatter) — touches ONE subagent.
2. **Agent `.md` body edit** — touches ONE subagent.
3. **`profile.yaml → system_prompt_suffix`** — touches every agent in ONE org (CTO + subagents).
4. **`profile.yaml → extra_prompt_parts`** — appends a named, file-sourced section to the supervisor and/or subagent prompt for ONE org (always-on). Use when `system_prompt_suffix` isn't enough (you need multiple sections, scope targeting, or file-based content).
5. **`orgs/specialists/<org>/AGENTS.md`** — touches the CTO of ONE org.
6. **`orgs/general/AGENTS.md`** — touches every CTO in every org that `extends: general`.
7. **`orgs/_shared/harness_addendum.md`** — touches every CTO in every org (the deepagents delegation/tool-surface prose). Wins on conflict with org overlays.
8. **`orgs/_shared/ask_user_suffix.md`** — touches every CTO in every org (the turn-ending HITL instruction). Emitted only when `ask_user` is active over a turn-based transport; edit the file to change what it says when it fires.
9. **`orgs/_shared/dynamic_dispatch_suffix.md`** — touches every CTO in every org (the eval-tool dispatch strategy). Emitted only when `CodeInterpreterMiddleware` is mounted; edit the file to change what it says when it fires.
10. **Root `AGENTS.md`** — touches NOTHING at runtime. It is a pure dev guide
    (Claude Code instruction file). Do not edit it expecting prompt changes;
    edit `orgs/general/AGENTS.md` instead.

---

## `profile.yaml` knob table (complete)

`profile.yaml` is the non-invasive way to shape behavior without editing
prompts. The first three fields ALSO work in an agent's own frontmatter
(per-agent override).

| Field | Effect | Per-agent? |
|---|---|---|
| `system_prompt_suffix` | Text appended to every agent's prompt (supervisor part 2, subagent part 2/3) | ✅ |
| `tool_description_overrides` | Rewrites a tool's description in the surface | ✅ |
| `excluded_tools` | Drops named tools from the surface | ✅ |
| `middleware` | `{supervisor: {add: [], remove: []}, subagent: {...}}` — add/remove registered middleware by name | ❌ org-wide |
| `excluded_middleware` | Unscoped supervisor remove (shorthand for `middleware.supervisor.remove`) | ❌ |
| `model_retry` | `ModelRetryMiddleware` config (default ON); `{enabled: false}` to disable, `{max_retries: N}` to tune | ❌ |
| `models` | Per-role model overrides (`base`, `grader`, `worker`) | ❌ |
| `ask_user` | Enables HITL interrupt (opt-in; transport-aware — dropped in MCP/autonomous mode) | ❌ |
| `rubric` | Post-CTO grader gate (`RubricGate` — its own top-level block, **not** middleware). `{enabled, max_iterations, default}` | ❌ |
| `general_purpose_subagent.enabled` | `false` = neuter deepagents' auto-added generic worker (emits a dead slot, not a live catch-all) | ❌ |
| `extra_prompt_parts` | List of `{name, file, scope}` — appends always-on file-sourced sections to the prompt. `file` is relative to the org's own dir. `scope` is `[supervisor]`, `[subagent]`, or both. Always-on (conditional logic still requires Python). **Delta-wins**: child's list replaces parent's. | ❌ |

---

## Agent frontmatter — complete field set

Every field is optional except `name` and `description`.

| Field | Effect |
|---|---|
| `name` | Identity (required). |
| `description` | **Routing signal** — injected into the supervisor's `task` tool description every turn. Answer "when do I pick THIS agent?", nothing more (see Part B). |
| `tools` | Tool allowlist (explicit `tools:` is a **full-replace**; resolved against the full specialist surface; native fs/shell tools are injected regardless). |
| `tools_add` / `tools_remove` | Additive tools delta (only when no explicit `tools:`; used with `extends:`). Set semantics on the tool suffix after the last `/`. |
| `model` | Per-agent model override (delta-wins). |
| `extends` | Parent agent slug — inherits body (append), tools (additive or replace), name/description/model (delta-wins) via `_merge_extends`. A `pux:`-namespaced slug resolves against library base agents only (pull a shipped agent without vendoring). |
| `description_append` | Concatenates onto the effective description (add context without restating the parent's). Only meaningful with `extends:`. |
| `capabilities` | `[{kind: tool \| skill \| mcp, ref: ...}]` — desugared into this agent's `tools`/`skills`/`mcp` before the `extends:` merge (CU-3). |
| `system_prompt_suffix` | Per-agent suffix appended after the org-wide suffix (subagent part 3). |
| `tool_description_overrides`, `excluded_tools` | Same shape as `profile.yaml`, applied per-agent (per-key merge for overrides; delta-wins). |
| `middleware` | `[name, ...]` — mounts registered middleware on THIS subagent only (resolved on top of the org's subagent baseline). |
| `skills` / `skills_add` | Skills sources for this agent (project-relative dirs). Explicit `skills:` is full-replace; `skills_add` is additive (dedup, order preserved). |

The **body** (below the frontmatter) is the subagent's system prompt prose.
Deepagents prepends its own short `DEFAULT_SUBAGENT_PROMPT` at bind time
(outside pux's assembly) — see the subagent table in Part A.

---

## How to SEE the actual prompt (introspection)

### `pux prompt show` — the introspection CLI (D8)

```bash
# Supervisor (CTO) prompt — with part-by-part provenance:
pux prompt show --org coder

# Just the assembled text (no labels):
pux prompt show --org coder --raw

# A specific subagent's prompt:
pux prompt show --org coder --scope subagent:coder-explorer

# Preview what the ask_user suffix WOULD emit over a turn-based transport:
pux prompt show --org coder --with-ask-user

# Preview what the dynamic_dispatch suffix WOULD emit when interpreter-mounted:
pux prompt show --org coder --with-interpreter

# Both at once (all 4 parts ACTIVE, 0 conditional):
pux prompt show --org coder --with-ask-user --with-interpreter
```

Docker-free: walks the SAME part registries (`SUPERVISOR_PROMPT_PARTS` /
`SUBAGENT_PROMPT_PARTS`) that `assemble_prompt` uses at runtime, but statically.
Each part is labeled with its **source** (which file it came from), **status**
(ACTIVE or CONDITIONAL), and **char count**. Conditional parts (ask_user,
dynamic_dispatch) are marked with their trigger so you know what WOULD emit them
at runtime. The `--with-ask-user` / `--with-interpreter` flags **simulate** the
runtime-on state so you can preview the exact content without running over a
real transport.

### `pux org chain` — inheritance introspection

```bash
# See the extends-chain, per-file merge rules, and effective supervisor base:
pux org chain --org coder
```

Prints: (1) the extends-chain (root→child with arrows), (2) which files each org
in the chain contributes (AGENTS.md / profile.yaml / policy.yaml / org.yaml /
agents/), (3) the per-file merge-rule table (FIXED — concatenation for
AGENTS.md, deep-merge for profile.yaml, never-inherited for policy.yaml,
extends-merge for agents/*.md), and (4) the effective supervisor base
composition (which AGENTS.md files + the addendum file form `agents_md_core`).

Example output (supervisor):
```
=== SUPERVISOR (CTO) prompt — org 'coder' ===

── part 1/4: agents_md_core ──────────────────────
   source: orgs/general/AGENTS.md + orgs/specialists/<org>/AGENTS.md (extends-chain overlay) + orgs/_shared/harness_addendum.md
   status: ACTIVE — always-on
   chars: 17,021
   [content...]

── part 2/4: org_system_prompt_suffix ────────────
   source: profile.yaml → system_prompt_suffix
   status: ACTIVE — present (non-empty suffix configured)
   chars: 700
   [content...]

── part 3/4: ask_user_suffix ─────────────────────
   status: CONDITIONAL — ask_user active AND turn-based transport
   (not emitted at static time — depends on runtime state)

── part 4/4: dynamic_dispatch_suffix ─────────────
   status: CONDITIONAL — CodeInterpreterMiddleware mounted
   (not emitted at static time — depends on runtime state)

=== TOTAL: 17,721 chars (2 active, 2 conditional) ===
```

**What the static view CAN'T resolve:** the conditional flags (`ask_user_active`,
`interpreter_mounted`) depend on runtime transport + middleware resolution. The
full assembly (with those parts emitted) requires the stack factory
(`build_stack`) which needs Docker + resolved specialists. For that, enable
Langfuse tracing (`LANGFUSE_*` env vars — already wired) and read the compiled
per-turn prompt from the trace.

### Python one-liner (scripting)

For scripts, call the same renderer directly:

```python
from pathlib import Path
from pux_harness.agent.prompt_show import show_supervisor, show_subagent

root = Path(".")  # repo root
print(show_supervisor("coder", root))                        # with provenance
print(show_supervisor("coder", root, raw=True))              # just text
print(show_subagent("coder", "coder-explorer", root))        # subagent
```

---

## Quick reference — what to edit (operator table)

Every row maps a goal to the right file. Paths are exact.

| Goal | Edit |
|---|---|
| **See the exact assembled prompt** | `pux prompt show --org <org>` (supervisor) or `--scope subagent:<slug>` (subagent) |
| **Preview conditional parts** | `pux prompt show --org <org> --with-ask-user` / `--with-interpreter` |
| **See the inheritance chain + merge rules** | `pux org chain --org <org>` |
| Change the CTO's personality/behavior for ONE org | `orgs/specialists/<org>/AGENTS.md` |
| Add a rule for EVERY agent in ONE org (CTO + subagents) | `orgs/specialists/<org>/profile.yaml → system_prompt_suffix` |
| Add a named, file-sourced section (supervisor and/or subagent) | `orgs/specialists/<org>/profile.yaml → extra_prompt_parts` (list of `{name, file, scope}`) |
| Add a rule for ONE subagent only | `orgs/specialists/<org>/agents/<slug>.md` frontmatter → `system_prompt_suffix` (preferred) or body |
| Change a subagent's prose | `orgs/specialists/<org>/agents/<slug>.md` (body) |
| Change a subagent's tools | `orgs/specialists/<org>/agents/<slug>.md` (frontmatter `tools:`) |
| Specialize a shared agent via inheritance (not copy-paste) | `orgs/specialists/<org>/agents/<slug>.md` frontmatter → `extends: <parent-slug>` |
| Mount middleware on ONE subagent | `orgs/specialists/<org>/agents/<slug>.md` frontmatter → `middleware: [name, ...]` |
| Change which specialists an org delegates to | `orgs/specialists/<org>/org.yaml` (`agents:` roster; `extends:` inherits parent's) |
| Refuse the inherited base roster (own roster only) | `orgs/specialists/<org>/org.yaml` → `inherit_roster: false` + explicit `agents: [...]` (see `coder/org.yaml`) |
| Add a foreign MCP tool server | `orgs/_shared/tool_servers.yaml` + `orgs/specialists/<org>/org.yaml → capabilities:` |
| Scope the supervisor's own tool surface | `orgs/specialists/<org>/policy.yaml → tool_surface.groups` (supervisor only; subagents resolve their own `tools:` against the FULL surface) |
| Override a shared agent for ONE org | Create `orgs/specialists/<org>/agents/<slug>.md` (org-local wins over `_shared`) |
| Add a contextual skill | `orgs/specialists/<org>/skills/<skill>/SKILL.md` (or `orgs/_shared/skills/<skill>/`) |
| Add/remove middleware for an org | `orgs/specialists/<org>/profile.yaml → middleware` |
| Tune model retry behavior | `orgs/specialists/<org>/profile.yaml → model_retry` |
| Add a post-CTO quality gate | `orgs/specialists/<org>/profile.yaml → rubric` |
| Repo-wide conventions (dev guide only — NOT a runtime prompt) | `AGENTS.md` (project root) — does not reach the model; edit `orgs/general/AGENTS.md` for runtime |
| Change what EVERY CTO sees (orchestrator pattern) | `orgs/general/AGENTS.md` |
| Change what EVERY CTO sees (delegation rules / tool-surface map) | `orgs/_shared/harness_addendum.md` |
| Change what EVERY CTO sees (ask-user turn-ending instruction) | `orgs/_shared/ask_user_suffix.md` |
| Change what EVERY CTO sees (eval-tool dispatch strategy) | `orgs/_shared/dynamic_dispatch_suffix.md` |
| Create a new org | `orgs/specialists/<new>/` with `AGENTS.md` + `org.yaml` + `profile.yaml` (+ optional `policy.yaml`) |

---

## Verified inheritance example — org `coder`

`coder` declares `extends: general` and `inherit_roster: false` (refuses the
inherited base roster; ships its own three specialists). Here is exactly what
each audience receives:

```
SUPERVISOR (coder CTO):
  part 1   orgs/general/AGENTS.md          (Pux base — orchestrator pattern)
         + orgs/specialists/coder/AGENTS.md (coder CTO overlay)
         + orgs/_shared/harness_addendum.md (deepagents delegation rules — wins on conflict)
  part 2   orgs/specialists/coder/profile.yaml → system_prompt_suffix
  part 3  [ask_user suffix]                (only if ask_user enabled + turn-based transport)
           source: orgs/_shared/ask_user_suffix.md
  part 4  [dynamic-dispatch suffix]        (only if CodeInterpreterMiddleware mounted)
           source: orgs/_shared/dynamic_dispatch_suffix.md

  + side channels:
      - /memories/AGENTS.md at startup
      - skills metadata (name + description only)
      - every armed tool's docstring
      - each subagent's description: → injected into the task tool, every turn

SUBAGENT (e.g. coder-explorer):
  part 1   orgs/specialists/coder/agents/coder-explorer.md (body)
         [deepagents prepends its own short DEFAULT_SUBAGENT_PROMPT at bind]
  part 2   orgs/specialists/coder/profile.yaml → system_prompt_suffix
  part 3  [agent frontmatter system_prompt_suffix]   (only if the .md carries it)

  + side channels:
      - skills metadata (if mounted)
      - every armed tool's docstring
```

The key contrast: the supervisor prompt is layers-deep (root + base + overlay +
addendum + suffixes); the subagent prompt is its own body + suffixes. They
share **only** the org-wide `system_prompt_suffix`.

---

## Cross-references

- **`docs/prompt-construction-review.md`** — the ground-truth map this doc
  summarizes, with `file:line` citations for every claim and the full rework
  direction list (D1-D8). Read this when the table above doesn't answer your
  question.
- **`docs/prompt-text-review.md`** — file-by-file prose audit of every prompt
  in the system (redundancy, verbosity, docstring duplication). Read this when
  editing a specific org's wording.
- **`pux-harness/pux_harness/agent/prompt_parts.py`** — the registries
  themselves (`SUPERVISOR_PROMPT_PARTS`, `SUBAGENT_PROMPT_PARTS`), the fallback
  constants (`_ADDENDUM`, `_DYNAMIC_DISPATCH_SUFFIX`), and `build_extra_parts`
  (the `extra_prompt_parts` builder). The three file-lifters
  (`load_harness_addendum`, `load_ask_user_suffix`, `load_dynamic_dispatch_suffix`)
  are the runtime readers for the three `orgs/_shared/*.md` prompt blocks.
- **`pux-harness/pux_harness/kit/loaders.py`** — the canonical loaders
  (`build_system_prompt`, `_chain_overlay`, `_merge_extends`, `_load_agent_spec`).
