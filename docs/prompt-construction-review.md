# Prompt Construction — Current State + Rework Options

Status: ground-truth map of how the pre-fold harness assembled agent system
prompts, plus rework directions (written 2026-07 as input for a cross-framework
research pass). **Superseded at the 2026-08 fold**: the harness's part-registry
assembly (`prompt_parts.assemble_prompt`) is retired, and the headline defect
below is what the fold's simplified design resolved. Surviving truth is
re-verified against `src/`; historical claims are marked.

---

## TL;DR

- **Headline defect (RESOLVED by the fold).** One root file (`AGENTS.md`) did
  two unrelated jobs: developer/project guide AND runtime agent base prompt,
  injected **whole** into every org CTO's prompt — dev-only sections (branch
  strategy, testing rules, deleted-feature history) leaked into live agent
  context. Post-fold, root `AGENTS.md` is the dev guide only (never injected);
  the runtime base is the org's own chain: `profiles/<org>/AGENTS.md` overlay
  (+ `extends:` ancestors) + the `profiles/_shared/` addenda.
- Assembly is now a **fixed, small sequence**, not a part registry:
  `build_system_prompt(org)` (`src/profiles/loaders.py`) joins the extends-chain
  overlay with the addenda (`load_shared_prompt_body` — `harness_addendum.md`,
  `ask_user_suffix.md`, `dynamic_dispatch_suffix.md`); subagents get their
  extends-merged `.md` body as `system_prompt` (`src/profiles/subagents.py`,
  `org_subagent_specs`).
- The rework directions below are preserved as the record of how we got here;
  D1 (the split) landed in the fold's shape, D3's "prompt as data" landed as
  files-not-code, and the research conclusions (§5, §6) remain valid guidance.

---

## 1. How it worked pre-fold (historical)

### 1.1 The two assemblies

Every agent got a system prompt via one of two disjoint registries in
the pre-fold harness's `agent/prompt_parts.py` — `SUPERVISOR_PROMPT_PARTS` (the
org CTO) and `SUBAGENT_PROMPT_PARTS` (delegated specialists) — joined by
`assemble_prompt(parts, ctx, scope)`, which kept only scope-matching,
non-`None` parts **in registry order** separated by `"\n\n"` (the "no-gap"
property). The registries were disjoint by construction, so a subagent could
never inherit supervisor content.

**Retired with the harness at the 2026-08 fold.** The surviving equivalents:

- Supervisor prompt = `build_system_prompt(org)` (`src/profiles/loaders.py`):
  `_chain_overlay(org)` (extends-chain AGENTS.md concatenation, root→child,
  `\n\n`-joined) + the caller's `addendum` (the `_shared` prompt blocks read via
  `load_shared_prompt_body`, `src/profiles/loaders.py`).
- Subagent prompt = the agent's extends-merged `.md` body — `_load_agent_spec`
  (`src/profiles/loaders.py`) resolves `<slug>.md` (org-local then
  `profiles/_shared/agents/`), desugars `capabilities:` and resolves agent-level
  `extends:` via `_merge_extends`; `org_subagent_specs` (`src/profiles/
  subagents.py`) projects each spec onto a native deepagents `SubAgent` dict
  with `system_prompt` = the merged body.

### 1.2 Pre-fold supervisor composition (historical)

Four parts: `agents_md_core` (root AGENTS.md + chain overlay + the harness
addendum, folded into part 1 via a byte-identity seam), `org_system_prompt_suffix`
(profile.yaml), `ask_user_suffix` (conditional on ask_user + turn-based
transport), `dynamic_dispatch_suffix` (conditional on `CodeInterpreterMiddleware`
mounted). The addendum fold was a "byte-identity hack" — the fold's assembly
renders it moot: each `_shared` file is now dormant prose, never injected
(§1.1).

### 1.3 Inheritance — two levels, both survive in `src/profiles/loaders.py`

**Org-level `extends:`** (`org.yaml`):
- **Roster**: parent `agents:` ∪ own (`org_agent_slugs`; `inherit_roster: false`
  opts out — `_org_inherit_roster`).
- **AGENTS.md overlay**: parent + own concatenated **own-last** via
  `_chain_overlay`. Blind concatenation — a child can append, not override.
- **profile.yaml**: deep-merged root→child.
- **policy.yaml**: NEVER inherited (each org owns its egress).
- Cycle-safe: the extends-chain resolver raises on cycle/unresolvable parent.

**Agent-level `extends:`** (agent frontmatter): `_merge_extends` — `name`/
`description`/`model` delta-wins; `tools` additive (`tools_add`/`tools_remove`)
or full-replace; `skills` additive (`skills_add`); `tool_description_overrides`
per-key, delta wins; `system_prompt` = base body + delta body joined `"\n\n"`.

### 1.4 The override vocabulary (surviving shape)

- `system_prompt_suffix` — **append**. The safe prompt lever.
- `tool_description_overrides`, `excluded_tools` — tool-surface shaping.
- `general_purpose_subagent` — control over deepagents' auto-added generic
  worker (data; the launch emits the neutered slot).
- **Replace-style keys remain banned by design** — an org can append or shape
  tools, never nuke the base assembly.

### 1.5 Parallel injection channels (unchanged post-fold)

The assembled `system_prompt` is only **part** of what the model sees, injected
at graph-compile time by deepagents itself:
- **Memory** — deepagents' memory middleware (`memory_auto_save`).
- **Skills** — `SkillsMiddleware` injects each skill's name + description
  (metadata only; bodies peeked via `read_file`).
- **Tool schemas** — every armed tool's docstring becomes part of the model's
  tool surface.

### 1.6 Proven scale (coder, measured pre-fold)

Assembled coder supervisor prompt = **28,357 chars**, confirmed to contain
dev-only sections (`Branch strategy`, `Testing harness rules`, `What's NOT
here`, …). That measurement is precisely the defect the fold removed.

---

## 2. Problems (honest list — post-fold disposition)

1. **Dev-doc / runtime-prompt conflation (headline).** → **RESOLVED** — root
   `AGENTS.md` is dev-only; runtime base is per-org chain + addenda.
2. **No audience dimension.** → Fold's shape: the org chain IS the audience —
   dev content has no injection path.
3. **Whole-file, no sectioning.** → Same; per-file granularity is now the
   design, not a workaround.
4. **Suffix-only override.** → Retained as a deliberate invariant.
5. **`extends:` overlay blind concatenation.** → Retained (still true in
   `_chain_overlay`); section-granular inheritance never became a need.
6. **Byte-identity seam hacks.** → Gone with the part registry.
7. **Tool-doc duplication.** → Partially addressed: browser tools moved to MCP
   (single docstring source); REGISTRY tool docs come from the `ToolSpec`s.
8. **No introspection.** → Partially: `uv run python src/run.py --org X
   --dry-run` prints org/model/MCP/subagent tools+middleware; no full prompt
   renderer exists.
9. **Flag coupling** (conditional parts precomputed in the stack factory). →
   Gone: conditionals reduced to "file present or not".
10. **Two `build_system_prompt` / two `load_root_prompt`** (harness shim vs kit
    core). → Gone with the fold; there is one loader.

### What the design got RIGHT (carried into the fold)
- Ordered, conditional, no-gap assembly — the fold kept the ordering property as
  a fixed sequence.
- Structurally disjoint supervisor/subagent prompts — preserved by design
  (subagents never see the org overlay).
- Cycle-safe, inheritance-aware `extends:` at both org and agent level — kept in
  `src/profiles/loaders.py`.
- Safe-by-default: suffix-only, replace banned — kept.

---

## 3. Rework directions — post-fold disposition

These were not mutually exclusive; they layered. Disposition:

- **D1 — Split the dev guide from the runtime base.** → **LANDED** (in the
  fold's shape): root `AGENTS.md` is dev-only; the runtime base is the org's
  own chain — `profiles/<org>/AGENTS.md` + `extends:` ancestors. The `_shared`
  addenda (`harness_addendum.md`, `ask_user_suffix.md`,
  `dynamic_dispatch_suffix.md`) exist as editable markdown behind
  `load_shared_prompt_body` but are **dormant** — no runtime call wires them
  into the prompt.
- **D2 — Audience as a first-class dimension.** → Effectively landed: audience
  is expressed by *location* (dev-only content simply never reaches the prompt).
- **D3 — Data-driven section manifest.** → Landed as files-not-code: the
  addenda are editable markdown read by `load_shared_prompt_body`; the assembly
  sequence is fixed in `src/profiles/loaders.py`.
- **D4 — Named-section inheritance.** → NOT built; still no section-granular
  overlay (blind concat remains). Deferred unless a real need appears.
- **D5 — Templating with runtime variables.** → NOT built.
- **D6 — Tool-doc single-sourcing.** → Partially landed (browser→MCP; REGISTRY
  docstrings are the tool docs).
- **D7 — Prompt as a contract-checked artifact.** → Landed in reduced form:
  `tests/guards/tripwire_checks.py` + the compiler's fail-loud loaders.
- **D8 — Prompt introspection CLI.** → NOT built (only `--dry-run`).

---

## 4. Questions to take to the field research — CLOSED

The research pass (§5) answered these; the fold's simpler design no longer needs
them as open questions. Kept in the record for future reference: section
composition model, audience/scope tagging, inheritance/override semantics,
dev-docs-vs-runtime conventions, templating-vs-data, introspection, token
budgeting.

---

## 5. Field research — verified reconciliation (07-10, still valid)

A web-agent research pass answered §4's questions. Findings are **verified**
(web-checked), not taken on the research doc's say-so.

### Verified ✓ — real precedent
- **PydanticAI** — `@agent.system_prompt` decorators appended per-call ⇒
  conditional-parts-as-code is an established pattern.
- **CrewAI** — role/goal/backstory merged into one prompt ⇒ structured-attribute
  composition.
- **DSPy** — declarative signatures compiled by adapters ⇒ "prompt as data".
- **LangGraph** — per-node system prompts + middleware-onion interception.
- **Jinja2 `{% extends %}` + `{% block %}` + `{{ super() }}`** ⇒ real named-section
  inheritance with append/prepend/replace.
- **SKILL.md manifest pattern** — Anthropic pioneered it; **OpenAI adopted it**
  (Skills = folder + SKILL.md). This validated the `SkillsMiddleware` +
  `profiles/_shared/skills/` layout — still the converging standard.
- **griffe** — real docstring→schema parser.
- **Tracing** — LangSmith / Langfuse / Pydantic Logfire log the compiled prompt
  per turn ⇒ introspection has proven demand.

### Fabricated / oversold ✗ — do NOT cite these anywhere permanent
- **"Wasaphi"** — **not a real framework.** The `prerequisite_met` section-isolation
  pattern is real but comes from a single Towards AI blog post ("Modular System
  Prompts"), mis-attributed in the research doc. The pattern is sound; the
  source is one author's blog.
- **"Gola"** — a real but niche YAML-configured assistant (gola.chat), not the
  "enterprise structured-manifest framework" the doc implied. Demote.
- Net: the section-manifest pattern is well-supported by *PydanticAI + LangGraph
  + DSPy + CrewAI*; it does not need the fake source.

### Invented numbers — treat as defaults, not findings
- **"2,000-token hard cap"** — the web agent's number, not measured. Infeasible:
  the orchestrator-pattern + tool-surface prose alone exceed 2K tokens. A real
  budget (if set) is ~4–6K tokens *including tool schemas*; the actual win was
  removing the ~18KB / ~5K-token dev-doc leak, which the fold removed.
- **CLAUDE.md "80–120 line limit"** and **"34–47% compression"** — unverified
  defaults. Don't enshrine.

---

## 6. Upstream-primitive check — `PipelinePromptTemplate` is deleted (07-10)

A proposal came to drop the bespoke `assemble_prompt` for LangChain's
`PipelinePromptTemplate`. Verified against the then-pinned `langchain_core 1.4.8`:

- `from langchain_core.prompts.pipeline import PipelinePromptTemplate` →
  `ModuleNotFoundError`; the `prompts.pipeline` submodule does not exist. It was
  removed in the 1.x cleanup.
- deepagents' boundary takes `str | SystemMessage | None` — any templating engine
  collapses to a string, so "native tracing / validation" upside doesn't survive
  the boundary.
- Even if it existed, wrong shape: a flat token-replacement renderer. The real
  problems were content architecture, not string rendering.
- **The LCEL pipe doesn't fill the gap either:** `p1 | p2` is a `RunnableSequence`
  (dataflow chaining), not text-section concatenation.

**Bottom line (still valid):** LangChain ships **no section-assembly primitive by
design** — the modern stack expects the system prompt to arrive finished. Section
composition is the application's job. The fold's fixed-sequence assembly in
`src/profiles/loaders.py` + `src/profiles/subagents.py` is exactly that: a small,
ours-to-own join, leaning upstream only where it genuinely fits (middleware, tool
schemas, deepagents' own prompt threading).
