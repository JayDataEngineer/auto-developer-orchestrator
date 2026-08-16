# Pux Prompt System

How prompts are really assembled — the ground-truth model, verified against
`src/profiles/loaders.py`, `src/profiles/subagents.py`, `src/run.py`, and
`src/middlewares/rubric.py`. Every path and part name below is real and
current as of the 2026-08 fold.

If you read one section, read **[The two prompts](#the-two-prompts)**.

---

## The two prompts

There are **two disjoint system prompts** in this system. An experimenter's
first question is always *"which one am I editing?"* — they have different
sources, different rules, and different levers. Supervisor content can never
leak into a subagent and vice versa.

| | **Supervisor** (the org CTO) | **Subagent** (a delegated specialist) |
|---|---|---|
| Sees root `AGENTS.md` | ❌ never (dev guide only — not injected) | ❌ never |
| Sees the base org prompt (`profiles/general/AGENTS.md`) | ✅ via `extends:` chain | ❌ never |
| Sees the `_shared` addenda (`harness_addendum.md`, …) | ❌ not injected (dormant files) | ❌ never |
| Sees `profile.yaml → system_prompt_suffix` | ❌ dormant (no consumer) | ❌ dormant |
| Sees its own `.md` body | n/a (no body) | ✅ always |
| Sees a per-agent `system_prompt_suffix` | n/a | ❌ dormant (no consumer) |
| Memory channel | ✅ deepagents' own memory (`memory_auto_save`) | only if memory middleware is mounted on subagents |
| Skills metadata | ✅ name + description only | ✅ name + description only (if mounted) |

**The most common experimenter mistake** is editing root `AGENTS.md` expecting
the CTO to pick up the change. It won't — root `AGENTS.md` is a pure dev guide
(Claude Code instruction file); nothing in the runtime reads it. Edit
`profiles/general/AGENTS.md` to change what every CTO sees, or
`profiles/specialists/<org>/AGENTS.md` for one org. Subagents get their `.md`
body, nothing else from the base chain.

---

## What lands in the prompt (and what doesn't)

The system prompt is **not just files**. It comes from two sources:

- **(A) The compiler/loaders** — `build_system_prompt(org)`
  (`src/profiles/loaders.py`) for the supervisor, `org_subagent_specs(org)`
  (`src/profiles/subagents.py`) for subagents. This is a **fixed, small
  sequence** — there is no part registry anymore.
- **(B) deepagents injection** at graph-compile/bind time — memory, skills
  metadata, tool docstrings, and the `description:` routing signal.

If you account for every file and the model still behaves unexpectedly, the
cause is in (B), not (A).

---

## Part A — the assembled prompt

### Supervisor (CTO) — one part

| # | Source | Condition |
|---|---|---|
| 1 | extends-chain overlay (`_chain_overlay`) — `profiles/general/AGENTS.md` → `profiles/specialists/<org>/AGENTS.md`, root→child, `\n\n`-joined | always-on |

`build_system_prompt(org, project_root)` (`src/profiles/loaders.py:276`)
returns exactly that chain overlay; `src/run.py` passes no addendum.

**Historical (pre-fold).** This used to be a 4-part registry (`prompt_parts.py`):
`agents_md_core` (overlay + the harness addendum, byte-identity-folded),
`org_system_prompt_suffix`, `ask_user_suffix`, `dynamic_dispatch_suffix` —
with Python fallback constants. All retired with the harness at the 2026-08
fold. The `profiles/_shared/*.md` addenda (`harness_addendum.md`,
`ask_user_suffix.md`, `dynamic_dispatch_suffix.md`) still exist as editable
markdown, and `load_shared_prompt_body` (`src/profiles/loaders.py:283`) is the
documented single reader, but **nothing wires them into the runtime prompt
today** — they are dormant prose, not live prompt content.

### Subagent — one part

| # | Source | Condition |
|---|---|---|
| 1 | the agent's extends-merged `.md` body — `_load_agent_spec` (`src/profiles/loaders.py`) resolves `<slug>.md` (org-local then `profiles/_shared/agents/`), desugars `capabilities:`, resolves agent-level `extends:` via `_merge_extends`; `org_subagent_specs` (`src/profiles/subagents.py`) projects it onto a native deepagents `SubAgent` with `system_prompt` = the merged body. Deepagents prepends its own short `DEFAULT_SUBAGENT_PROMPT` at bind time (outside this assembly but the model does see it). | always-on |

A subagent **never** sees: root `AGENTS.md`, the base org prompt, the addenda,
or the orchestrator pattern. Only its specialization.

---

## Part B — parallel injection channels (deepagents' own)

These are the answer to *"I accounted for every file but the model still
behaves unexpectedly."*

| Channel | What it adds | Who sees it | Source |
|---|---|---|---|
| **Memory** | deepagents' own memory, saved/loaded via `memory_auto_save` | supervisor; subagents only if memory middleware is mounted on them | deepagents |
| **Skills metadata** | Each skill's `name` + `description` **only**. The `SKILL.md` body is fetched on demand via `read_file` when the agent decides to load it. | supervisor (via `SkillsMiddleware`); subagents if mounted | deepagents |
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
prose-inheritance rules differ per file (all in `src/profiles/loaders.py`):

| File | Inheritance rule |
|---|---|
| `AGENTS.md` overlay | **Blind concatenation.** Parent body + own body, own-last, joined `"\n\n"` (`_chain_overlay`). A child cannot override a section of the parent's prose — it can only append. |
| `profile.yaml` | **Deep-merged** root → child. Child wins on conflicts. |
| `policy.yaml` | **Never inherited.** Each org owns its own egress. |
| Agent `.md` files | **Per-agent `extends:`** via `_merge_extends`: `name`/`description`/`model` delta-wins; `tools` additive (`tools_add`/`tools_remove`) or full-replace; `skills` additive (`skills_add`); `tool_description_overrides` per-key, delta wins; body = base body + delta body joined `"\n\n"`. |

Both extends chains are cycle-safe — unresolvable parents raise.

---

## Constraints (things you cannot do — by design)

| Want | Answer |
|---|---|
| Replace the whole base prompt | ❌ There is no replace lever at all — the pre-fold `base_system_prompt` global-REPLACE (a permanent contract failure then) doesn't even exist as a key anymore. Append-only is the design. |
| Remove or refine a base section from a specialist overlay | ❌ Overlay is blind concat. You can only append. |
| Reorder parts of the prompt | ❌ Order is fixed in the assembly code (no registry to reorder). |
| Suppress a base rule for one org | ⚠️ Only by appending a counter-rule in your overlay. The base text still reaches the model. |
| Make a subagent see the base prompt | ❌ Supervisor and subagent prompts are disjoint by construction. Put the rule in the agent's `.md` body. |

---

## The safe levers, ordered by how local the effect is

1. **Agent `.md` body edit** — touches ONE subagent (this is the system prompt).
2. **Agent frontmatter** (`tools:`, `middleware: [rubric]`, `model:`) — shapes ONE subagent's surface.
3. **`profiles/specialists/<org>/AGENTS.md`** — touches the CTO of ONE org.
4. **`profiles/general/AGENTS.md`** — touches every CTO in every org that `extends: general`.
5. **Root `AGENTS.md`** — touches NOTHING at runtime. It is a pure dev guide
   (Claude Code instruction file). Do not edit it expecting prompt changes;
   edit `profiles/general/AGENTS.md` instead.

**Dormant levers (declared vocabulary, no runtime consumer post-fold):**
`profile.yaml → system_prompt_suffix`, per-agent `system_prompt_suffix`,
`profile.yaml → extra_prompt_parts`, and the `profiles/_shared/*.md` addenda.
They parse and pass through the loaders but are not appended to any prompt;
delete them from a profile when you touch it, or wire them back in via
`load_shared_prompt_body` + the `addendum=` parameter of `build_system_prompt`.

---

## `profile.yaml` knob table (post-fold status)

| Field | Effect | Runtime status |
|---|---|---|
| `system_prompt_suffix` | Text appended to prompts | **dormant** (no consumer) |
| `tool_description_overrides` | Tool-description rewrite, per-key merge | merged into specs (`_merge_extends`); application is dormant |
| `excluded_tools` | Drop named tools from the surface | **dormant** |
| `middleware` | `{supervisor: {add/remove}}` — only `rubric` is a known ref; unknown refs raise (`src/middlewares/rubric.py`) | consumed at subagent level |
| `excluded_middleware` | Unscoped supervisor remove | **dormant** |
| `model_retry` | Retry config | **dormant** |
| `models` | Per-role model overrides | **retired** (dcode's `_get_default_model_spec()` rules) |
| `ask_user` | HITL interrupt flag | **dormant** (the suffix it triggered is not injected) |
| `rubric` | Post-CTO grader gate | consumed for agents that carry `middleware: [rubric]` + `rubric:` prose (subagents); org-level block is dormant |
| `general_purpose_subagent.enabled` | Neuter deepagents' auto-added generic worker | data; the launch's slot emission honors the shape |
| `extra_prompt_parts` | Always-on file-sourced sections | **dormant** |

---

## Agent frontmatter — complete field set

Every field is optional except `name` and `description`.

| Field | Effect | Runtime status |
|---|---|---|
| `name` | Identity (required). | consumed |
| `description` | **Routing signal** — injected into the supervisor's `task` tool description every turn (see Part B). | consumed |
| `tools` | Tool allowlist (explicit `tools:` is a **full-replace**; resolved against the registry surface). | consumed |
| `tools_add` / `tools_remove` | Additive tools delta (only when no explicit `tools:`; used with `extends:`). Set semantics on the tool suffix after the last `/`. | consumed |
| `model` | Per-agent model override (delta-wins). | consumed |
| `extends` | Parent agent slug — inherits body (append), tools (additive or replace), name/description/model (delta-wins) via `_merge_extends`. A `pux:`-namespaced slug resolves against library base agents only. | consumed |
| `description_append` | Concatenates onto the effective description. Only meaningful with `extends:`. | consumed |
| `capabilities` | `[{kind: tool \| skill \| mcp, ref: ...}]` — desugared into this agent's `tools`/`skills`/`mcp` before the `extends:` merge (`src/compiler/capabilities.py`). | consumed |
| `middleware` | `[rubric]` — mounts `RubricMiddleware` on THIS subagent (unknown refs raise). | consumed |
| `rubric` | The agent's own grading prose for `RubricMiddleware`. | consumed (with `middleware: [rubric]`) |
| `skills` / `skills_add` | Skills sources for this agent. Explicit `skills:` is full-replace; `skills_add` is additive (dedup, order preserved). | consumed |
| `system_prompt_suffix` | Per-agent suffix | **dormant** |
| `tool_description_overrides`, `excluded_tools` | Same shape as `profile.yaml` | **dormant** |

The **body** (below the frontmatter) is the subagent's system prompt prose.
Deepagents prepends its own short `DEFAULT_SUBAGENT_PROMPT` at bind time.

---

## How to SEE the actual prompt (introspection)

The pre-fold introspection CLIs (`pux prompt show`, `pux org chain`) are
**retired** with the part registry — the CLI is now exactly `sync`, `check`,
`compile` (`src/compiler/cli.py`).

Surviving introspection:

```bash
uv run python src/run.py --org coder --dry-run
# prints org, model default, MCP servers, and per-subagent tools + middleware
# (not the full prompt text — there is no prompt renderer post-fold)
```

For the supervisor prompt text: `build_system_prompt("coder", project_root=…)`
is a pure function (stdlib + yaml, no runtime deps) — callable from a script
against `src/profiles/loaders.py`. For subagent prompts: the merged `.md`
bodies themselves (`profiles/specialists/coder/agents/*.md`).

---

## Quick reference — what to edit (operator table)

Every row maps a goal to the right file. Paths are exact.

| Goal | Edit |
|---|---|
| **See the launch plan** | `uv run python src/run.py --org <org> --dry-run` |
| Change the CTO's personality/behavior for ONE org | `profiles/specialists/<org>/AGENTS.md` |
| Change what EVERY CTO sees (orchestrator pattern) | `profiles/general/AGENTS.md` |
| Add a rule for ONE subagent only | `profiles/specialists/<org>/agents/<slug>.md` (body, or frontmatter `tools:`/`middleware: [rubric]`) |
| Change a subagent's tools | `profiles/specialists/<org>/agents/<slug>.md` (frontmatter `tools:`) |
| Specialize a shared agent via inheritance (not copy-paste) | `profiles/specialists/<org>/agents/<slug>.md` frontmatter → `extends: <parent-slug>` |
| Change which specialists an org delegates to | `profiles/specialists/<org>/org.yaml` (`agents:` roster; `extends:` inherits parent's) |
| Refuse the inherited base roster (own roster only) | `profiles/specialists/<org>/org.yaml` → `inherit_roster: false` + explicit `agents: [...]` |
| Add a foreign MCP tool server | `profiles/_shared/tool_servers.yaml` + `profiles/specialists/<org>/org.yaml → capabilities:` |
| Override a shared agent for ONE org | Create `profiles/specialists/<org>/agents/<slug>.md` (org-local wins over `_shared`) |
| Add a contextual skill | `profiles/specialists/<org>/skills/<skill>/SKILL.md` (or `profiles/_shared/skills/<skill>/`) |
| Add a post-CTO quality gate on a subagent | `profiles/specialists/<org>/agents/<slug>.md` frontmatter → `middleware: [rubric]` + `rubric:` prose |
| Repo-wide conventions (dev guide only — NOT a runtime prompt) | `AGENTS.md` (project root) — does not reach the model; edit `profiles/general/AGENTS.md` for runtime |
| Create a new org | `profiles/specialists/<new>/` with `AGENTS.md` + `org.yaml` (+ optional `profile.yaml`, `policy.yaml`) |

---

## Verified inheritance example — org `coder`

`coder` declares `extends: general` and `inherit_roster: false` (refuses the
inherited base roster; ships its own three specialists). Here is exactly what
each audience receives:

```
SUPERVISOR (coder CTO):
  profiles/general/AGENTS.md          (Pux base — orchestrator pattern)
    + profiles/specialists/coder/AGENTS.md (coder CTO overlay)
  [built by build_system_prompt — src/profiles/loaders.py]

  + side channels (deepagents):
      - memory (deepagents' own, memory_auto_save)
      - skills metadata (name + description only)
      - every armed tool's docstring
      - each subagent's description: → injected into the task tool, every turn

SUBAGENT (e.g. coder-explorer):
  profiles/specialists/coder/agents/coder-explorer.md (extends-merged body)
         [deepagents prepends its own short DEFAULT_SUBAGENT_PROMPT at bind]
  + frontmatter effects: tools, model, middleware: [rubric]

  + side channels:
      - skills metadata (if mounted)
      - every armed tool's docstring
```

The key contrast: the supervisor prompt is the layered org chain; the subagent
prompt is its own merged body. They share nothing — no suffix, no addendum —
at runtime.

---

## Cross-references

- **`docs/prompt-construction-review.md`** — the full history: the pre-fold
  part-registry ground-truth map, the rework directions (D1–D8) with their
  post-fold dispositions, and the verified field research.
- **`docs/prompt-text-review.md`** — file-by-file prose audit of every prompt
  in the system (redundancy, verbosity, docstring duplication). Read this when
  editing a specific org's wording.
- **`src/profiles/loaders.py`** — the canonical loaders (`build_system_prompt`,
  `_chain_overlay`, `_merge_extends`, `_load_agent_spec`,
  `load_shared_prompt_body`).
- **`src/profiles/subagents.py`** — roster → native `SubAgent` projection
  (`org_subagent_specs`).
- **`src/run.py`** — the launch (`build_org_agent`, `_load_mcp`, dcode's own
  `_get_default_model_spec`, `run_textual_app`).
