# Prompt Construction — Current State + Rework Options

Status: ground-truth map of how pux assembles agent system prompts today, plus
rework directions. Every claim cites `file:line` so it's verifiable. Written as
input for a cross-framework research pass.

---

## TL;DR

- One root file (`AGENTS.md`) does **two unrelated jobs**: developer/project
  guide AND runtime agent base prompt. It is injected **whole** into every org
  CTO's prompt, so dev-only sections (branch strategy, testing rules, deleted-
  feature history) leak into live agent context. **This is the headline defect.**
- Assembly itself is decent: an ordered, conditional, no-gap part registry
  (`prompt_parts.assemble_prompt`), with a *structurally* disjoint supervisor vs
  subagent split. The rework should **extend** this, not replace it.
- The clean fix has two stages: (1) **split** the dev guide from the runtime
  base; (2) **promote "audience" to a first-class section dimension** and make
  the part registry **data-driven** (a manifest, not a hardcoded tuple).

---

## 1. How it works today

### 1.1 The two assemblies

Every agent gets a system prompt via one of two disjoint registries in
`pux-harness/pux_harness/agent/prompt_parts.py`:

- **SUPERVISOR** (the org CTO / operator driver) — `SUPERVISOR_PROMPT_PARTS`
  (`prompt_parts.py:203`).
- **SUBAGENT** (a delegated specialist) — `SUBAGENT_PROMPT_PARTS`
  (`prompt_parts.py:231`).

Both are joined by `assemble_prompt(parts, ctx, scope)` (`prompt_parts.py:87`),
which keeps only the parts whose scope matches AND whose `build()` returns
non-`None`, **in registry order**, separated by `"\n\n"`. Skipped parts leave no
trace (the "no-gap" property).

The registries are **disjoint by construction** — no part is scoped to both — so
a subagent can never inherit supervisor content (`prompt_parts.py:16-21`). This
is the one structural boundary, and it's enforced well.

### 1.2 Supervisor (CTO) prompt — exact composition

Built in `stack.build_stack` (`stack.py:859-870`), which calls:

```
assemble_prompt(SUPERVISOR_PROMPT_PARTS,
  PromptCtx(
    agents_md_base      = build_system_prompt(org),
    system_prompt_suffix= profile.system_prompt_suffix,   # from profile.yaml
    ask_user_active     = <precomputed flag>,
    interpreter_mounted = _interpreter_mounted(supervisor_middleware),
  ), PromptScope.SUPERVISOR)
```

The 4 supervisor parts, in final output order:

| # | Part (`prompt_parts.py`) | Source | Condition |
|---|---|---|---|
| 1 | `agents_md_core` (`:207`) | `build_system_prompt(org)` **+ `_ADDENDUM`** (`:110`) | always-on |
| 2 | `org_system_prompt_suffix` (`:210`) | `profile.yaml` `system_prompt_suffix` | present |
| 3 | `ask_user_suffix` (`:214`) | `ASK_USER_PROMPT_SUFFIX` (`hitl.py:32`) | ask_user active **and** turn-based transport |
| 4 | `dynamic_dispatch_suffix` (`:219`) | `_DYNAMIC_DISPATCH_SUFFIX` (`:169`) | `CodeInterpreterMiddleware` actually mounted (strength-`pro` base, or explicit `add: [interpreter]`) |

`build_system_prompt(org)` (`orgs.py:171-179`) is:

```python
overlay = _chain_overlay(org, _orgs_dir().parent)   # kit/loaders.py:301
return f"{load_root_prompt()}\n\n{overlay}"          # root AGENTS.md + chain overlay
```

where `load_root_prompt()` (`orgs.py:151-161`) is:

```python
_split_frontmatter(_read("AGENTS.md"))[1]   # WHOLE body, frontmatter stripped, NO section filter
```

So **part 1 expands to**: `root_AGENTS.md_body + "\n\n" + chain_overlay + <ADDENDUM>`.
`_ADDENDUM` carries a baked leading `"\n"` and is folded into part 1 (not a
separate part) specifically to preserve a single-newline seam the `"\n\n"`
joiner can't reproduce (`prompt_parts.py:105-109`). **That fold is a byte-
identity hack, not clean design.**

**Final supervisor prompt order:**
```
ROOT AGENTS.md  →  [extends-chain overlay]  →  Harness addendum  →
profile suffix  →  [ask_user suffix]  →  [dynamic-dispatch suffix]
```

### 1.3 Subagent prompt — exact composition

Built in `load_subagents` (`orgs.py:597-726`). For each slug in the org roster:

1. `_load_agent_spec(slug, org)` (`kit/loaders.py:424-497`) resolves
   `<slug>.md` (org-local then `orgs/_shared/agents/`), desugars an optional
   `capabilities:` block, and resolves agent-level `extends:` via
   `_merge_extends` (`kit/loaders.py:330-421`). Returns `{**frontmatter,
   "system_prompt": body}`.
2. The subagent prompt is assembled (`orgs.py:692-705`):

| # | Part (`prompt_parts.py`) | Source |
|---|---|---|
| 1 | `agent_body` (`:232`) | the agent's (extends-merged) body |
| 2 | `org_system_prompt_suffix` (`:237`) | `profile.yaml` `system_prompt_suffix` (org-wide) |
| 3 | `agent_system_prompt_suffix` (`:242`) | the agent's OWN frontmatter `system_prompt_suffix` |

**A subagent NEVER sees**: root `AGENTS.md`, the harness addendum, the
orchestrator pattern, or the dynamic-dispatch notice. Only its specialization +
the two suffixes.

Precedence (most-specific wins, `orgs.py:641-649`):
```
.md body (or extends-merged body)
  → org-wide system_prompt_suffix
  → per-agent system_prompt_suffix
```

### 1.4 Inheritance — two levels, both string-concat for prose

**Org-level `extends:`** (`org.yaml`; `kit/loaders.py:105-194`):
- **Roster**: parent `agents:` ∪ own (`org_agent_slugs`, `:265`).
- **AGENTS.md overlay**: parent + own concatenated **own-last** via
  `_chain_overlay` (`:301-313`), joined `"\n\n"`. **Blind concatenation — a
  child cannot override a section of the parent's overlay, only append.**
- **profile.yaml**: deep-merged root→child (`profile._resolved_profile_yaml`).
- **policy.yaml**: NEVER inherited (each org owns its egress).
- Cycle-safe: `org_extends_chain` raises on cycle/unresolvable parent
  (`:148-183`); runtime uses the cycle-safe fallback `_resolved_org_chain`.

**Agent-level `extends:`** (agent frontmatter; `kit/loaders.py:330-421`):
- `_merge_extends`: `name`/`description`/`model` delta-wins; `tools` additive
  (`tools_add`/`tools_remove`) or full-replace; `system_prompt` = base body +
  delta body joined `"\n\n"`.

### 1.5 The override vocabulary (`HarnessProfileConfig` fields)

The SAME fields work **org-wide** (`profile.yaml`) and **per-agent**
(frontmatter) (`orgs.py:462-487`):

- `system_prompt_suffix` — **append**. The ONLY safe prompt lever.
- `tool_description_overrides`, `excluded_tools` — tool-surface shaping.
- `general_purpose_subagent` — own deepagents' auto-added generic worker
  (org-wide only; `orgs.py:490-594`).
- `base_system_prompt` — **BANNED**. It was a global-REPLACE that nuked the whole
  assembly. Permanent contract failure in `profile.validate_profile`
  (`profile.py:836-847`) + a runtime guard in `stack.build_stack`
  (`stack.py:849-854`). Per-agent variant also banned (`orgs.py:686-691`).

Net: **an org can append to the prompt, or shape tools — it cannot refine,
reorder, or remove sections of the base.**

### 1.6 Parallel injection channels (NOT part of `assemble_prompt`)

The assembled `system_prompt` is only **part** of what the model sees. Three
more channels inject at bind/startup time via `create_deep_agent`
(`graph.py:130-145`):

- **Memory** — `MemoryMiddleware` (deepagents) loads `/memories/AGENTS.md` and
  injects it into the system prompt at startup (`graph.py:119-128`).
- **Skills** — `SkillsMiddleware` injects each skill's name + description
  (metadata only; bodies peeked via `read_file`).
- **Tool schemas** — every armed tool's docstring becomes part of the model's
  tool surface.

### 1.7 Proven scale (coder, measured)

Assembled coder supervisor prompt = **28,357 chars**. Confirmed to contain
dev-only sections: `Branch strategy`, `Testing harness rules`, `What's NOT
here`, `Per-org harness profile`, `console_scripts entry point`,
`v0.2.0-pre-pi-mono`, `pi-pivot`. (See audit transcript.)

---

## 2. Problems (honest list)

1. **Dev-doc / runtime-prompt conflation (headline).** Root `AGENTS.md` is a
   project/dev guide stapled to a runtime base. Injected whole → ~18KB of
   branch-strategy / testing-rules / deleted-feature prose sits in every org
   CTO's context. Noise that can mislead a live agent + token tax every turn.
2. **No audience dimension.** "Who is this section for?" is not expressible, so
   there is no way to mark dev content out-of-band for runtime. The only filter
   is whole-file (`_split_frontmatter(...)[1]`).
3. **Whole-file, no sectioning.** Can't slice a file by section at injection
   time. Forces the dev/runtime split to be file-granular.
4. **Suffix-only override.** Because replace was (rightly) banned, orgs can only
   *append*. They cannot cleanly refine, remove, or reorder base sections.
5. **`extends:` overlay is blind concatenation.** A child org can't override a
   section of its parent's CTO prose — only append more.
6. **Byte-identity seam hacks.** `_ADDENDUM`'s baked leading `"\n"` and the
   folded `agents_md_core` exist to reproduce a pre-refactor seam. Brittle;
   they block clean per-section joiners.
7. **Tool-doc duplication.** Root `AGENTS.md` prose-describes browser/desktop/
   python tools that ALSO have docstrings. Two sources of truth → drift.
8. **No introspection.** "What exactly is in org X's prompt?" requires running
   Python against the loaders. No CLI, no provenance.
9. **Flag coupling.** Conditional-part flags (`ask_user_active`,
   `interpreter_mounted`) are precomputed in `stack.py` and threaded through
   `PromptCtx`, coupling prompt assembly to the stack factory.
10. **Two `build_system_prompt` / two `load_root_prompt`** (harness shim in
    `agent/orgs.py` vs backend-agnostic core in `kit/loaders.py`). The runtime
    uses the shim; the kit is the reusable core. Drift risk between them.

### What the current design gets RIGHT (preserve in any rework)
- Ordered, conditional, no-gap part registry (`assemble_prompt`).
- Structurally disjoint supervisor/subagent registries.
- Cycle-safe, inheritance-aware `extends:` at both org and agent level.
- Safe-by-default: suffix-only, replace banned.
- Byte-identity discipline (shows care, even if it produces hacks).

---

## 3. Rework directions

These are not mutually exclusive; they layer. Roughly ordered minimal → maximal.

### D1 — Split the dev guide from the runtime base *(prerequisite, low risk)*
- New lean `orgs/_shared/base.md` = runtime-only base (tool-surface summary,
  verify-or-die, no-fallbacks, orchestrator pattern, org-mode framing).
- `load_root_prompt()` / `build_system_prompt()` repoint to it.
- Root `AGENTS.md` becomes pure dev guide (branch strategy, testing rules,
  architecture, profile reference, dropped/deferred). Never injected.
- **Fixes:** problems 1, 2, 3 at file granularity. Unblocks everything else.
- **Cost:** every org prompt changes (smaller, cleaner); contract + prompt-
  assembly tests need repointing; operator (no-`--org`) mode picks up the lean
  base too.

### D2 — Audience as a first-class section dimension
- Each section declares an `audience`: `operator | cto | subagent | dev`.
- `assemble_prompt` filters by the active audience; `dev` never reaches runtime.
- Generalizes the existing scope filter (which is really a 2-value audience).
- **Fixes:** problems 1, 2 structurally (not just at file granularity).
- **Pairs with:** D3 (sections need to come from somewhere structured).

### D3 — Data-driven section manifest (replace the hardcoded tuple)
- A `prompt:` manifest (per default, overridable per org) lists ordered sections:
  `{id, source: <file|inline>, audience, condition, layer}`.
- `assemble_prompt` becomes filter-by-audience/condition + join. The
  `SUPERVISOR_PROMPT_PARTS`/`SUBAGENT_PROMPT_PARTS` tuples become data.
- An org can add/remove/reorder sections **without code**.
- **Fixes:** problems 8 (introspection), 9 (flag coupling moves into condition
  expressions), and enables D7/D8.

### D4 — Named-section inheritance (replace blind overlay concat)
- A prompt is a set of **named sections**, not a blob. `extends:` merges at
  section granularity (replace / append / prepend), like CSS specificity or
  class methods.
- **Fixes:** problems 4 (suffix-only), 5 (blind overlay concat).
- **Cost:** needs a section model + merge-semantics spec.

### D5 — Templating with runtime variables
- Base prompt as a template (`{{org}}`, `{{transport}}`, `{{tools_available}}`,
  `{{model_strength}}`); harness injects facts at render.
- Replaces the conditional-parts + flag-coupling with in-template conditionals.
- **Fixes:** problem 9 a different way. **Tradeoff:** couples template to a
  context dict; harder to audit than data sections; can grow tangled. Common in
  frameworks — worth seeing how they keep it sane.

### D6 — Tool-doc single-sourcing
- Stop prose-describing tools in the base; let docstrings be the sole source.
- Base carries only cross-cutting policy.
- **Fixes:** problem 7. Cleanup regardless of which other directions land.

### D7 — Prompt as a contract-checked artifact
- Contract rules: no `audience: dev` reaches runtime; prompt under a token
  budget; required sections present; no seam artifacts; no tool-doc duplication.
- **Fixes:** problem 1 *permanently* (makes the regression structurally
  impossible, not just currently absent).

### D8 — First-class prompt introspection CLI
- `pux prompt show --org X [--scope supervisor|subagent:slug]` renders the exact
  assembled prompt **with provenance** (which file/section each chunk came from).
- Cheap to build on D3's manifest. Turns problem 8 from a Python exercise into a
  command.

### Recommended path
1. **D1 now** — split dev guide from runtime base. Low risk, fixes the headline
   defect, unblocks the rest.
2. **D2 + D3** — audience-tagged, data-driven section manifest. The proper
   rework; generalizes the good `assemble_prompt` idea and makes D7/D8 trivial.
3. **D6** — tool-doc single-sourcing, as cleanup.
4. **D4** only if overlay-section refinement becomes a real need (today nothing
   exercises it).
5. **D5** is an alternative to D2/D3's condition model, not obviously better —
   let the research inform this.

---

## 4. Questions to take to the field research

How do other agent frameworks compose multi-agent / multi-tenant system prompts?
Specifically:

1. **Section composition model** — registry of parts? template? manifest/data?
   How do LangGraph + deepagents, OpenAI Agents SDK, AutoGen, CrewAI, DSPy,
   Anthropic's Claude Agent SDK, PydanticAI each assemble a system prompt?
2. **Audience / scope** — do any tag sections by recipient (supervisor vs
   subagent vs developer)? How do they keep developer/maintainer docs out of
   runtime prompts?
3. **Inheritance / override** — how do they let a sub-agent or sub-config
   inherit and *refine* (not just append) a parent prompt? Section-granular
   override? mixin/trait models?
4. **Dev-docs vs runtime** — where do frameworks put contributor/maintainer
   guidance so it never reaches the model? (CLAUDE.md / AGENTS.md conventions,
   separate doc trees, etc.)
5. **Templating vs data** — do mature frameworks settle on templating (Jinja et
   al.) or structured section data? What are the failure modes of each?
6. **Introspection** — do any ship a "show me the exact prompt" tool with
   provenance? (Claude Code, Cursor, etc.)
7. **Token budgeting** — anyone enforcing/previewing prompt size at config time?

The goal of the research: pick the **section-composition model** (Q1) and the
**inheritance/override semantics** (Q3) that best fit pux's "one harness, many
orgs, each a thin overlay" shape, then decide template-vs-data (Q5).

---

## 5. Field research — verified reconciliation (07-10)

A web-agent research pass was run against §4's questions. Findings below are
**verified** (web-checked), not taken on the research doc's say-so.

### Verified ✓ — real precedent that backs our directions
- **PydanticAI** — `@agent.system_prompt` decorators appended per-call ⇒ our
  conditional-parts idea is an established pattern, just expressed as code.
- **CrewAI** — role/goal/backstory merged into one prompt ⇒ structured-attribute
  composition (a flavor of D3).
- **DSPy** — declarative signatures compiled by adapters ⇒ the "prompt as data"
  extreme of D3.
- **LangGraph** — per-node system prompts + middleware-onion interception ⇒ D3
  + the injection seams we already have.
- **Jinja2 `{% extends %}` + `{% block %}` + `{{ super() }}`** ⇒ real, mature
  named-section inheritance with append/prepend/replace. Validates D4's model.
- **SKILL.md manifest pattern** — Anthropic pioneered it; **OpenAI adopted it
  (Skills = folder + SKILL.md, zipped)** (Simon Willison; OpenAI Cookbook). This
  *also* validates our existing `SkillsMiddleware` + `orgs/_shared/skills/`
  layout — we're already on the converging standard.
- **griffe** — real docstring→schema parser. Backs D6 single-sourcing.
- **Tracing** — LangSmith / Langfuse / Pydantic Logfire all log the exact
  compiled prompt per turn ⇒ D8 introspection has proven demand.
- **O(K²) context-replay cost** — directionally correct; the runaway-billing
  anecdote is a real public incident (anecdotal, not pux data).

### Fabricated / oversold ✗ — do NOT cite these anywhere permanent
- **"Wasaphi"** — **not a real framework.** The `prerequisite_met` section-isolation
  pattern is real but comes from a *single Towards AI blog post*
  ("Modular System Prompts"), which the research doc mis-attributed to a
  nonexistent framework. The pattern is sound; the source is one author's blog.
- **"Gola"** — a real but niche YAML-configured assistant (gola.chat), not the
  "enterprise structured-manifest framework" the doc implies. Demote.
- Both names were cut from our citations. Net: the section-manifest pattern is
  well-supported by *PydanticAI + LangGraph + DSPy + CrewAI*; it does **not**
  need the fake source.

### Invented numbers — treat as defaults, not findings
- **"2,000-token hard cap"** — the web agent's number, not measured. **Infeasible
  for us:** the orchestrator-pattern + tool-surface prose alone exceed 2K
  tokens. A real budget (if we set one, Phase 4) is ~4–6K tokens *including tool
  schemas*; the actual win is removing the ~18KB / ~5K-token dev-doc leak, which
  is pure waste regardless of any cap.
- **CLAUDE.md "80–120 line limit"** and **"34–47% compression"** — unverified
  defaults from the research pass. Don't enshrine.

### Convergence = the signal that matters
Both analyses (mine, §3; the research pass, its §5) independently propose the
**same four-phase sequence**:

| Phase | This doc | Research pass |
|---|---|---|
| 1 | D1 split + D6 tool-doc single-source | "Separate onboarding + single-source docstrings" |
| 2 | D3 manifest + D2 audience tags | "Declarative YAML manifest + audience filter" |
| 3 | D4 named-section inheritance | "Jinja2-style block inheritance" |
| 4 | D8 CLI + D7 budget | "Introspection CLI + token gate + retry budget" |

Two independent passes converging on one sequence, against verified industry
precedent, is the strongest evidence available that this is the right shape.
**The one correction:** drop the 2K-token cap; the real lever is the dev-doc
leak, which Phase 1 already removes.

---

## 6. Upstream-primitive check — `PipelinePromptTemplate` is deleted (07-10)

A proposal came in: drop the bespoke `assemble_prompt` and use LangChain's
`PipelinePromptTemplate`, moving the composition burden upstream ([[rely-on-upstream]]).
The instinct is correct; **the specific primitive is gone.** Verified against our
own venv:

- **Pinned `langchain_core 1.4.8`.** `from langchain_core.prompts.pipeline import
  PipelinePromptTemplate` → `ModuleNotFoundError`. The `prompts.pipeline`
  **submodule does not exist**; **zero files** in the installed package reference
  the symbol. It was removed in the 1.x cleanup. The proposal didn't check the
  version — adopting it would require a legacy LangChain pin, violating
  [[no-legacy-left-behind]].
- **deepagents boundary takes `str | SystemMessage | None`** (`create_deep_agent`
  signature, `graph.py:132` passes `system_prompt=plan.supervisor_prompt`, a
  string). Any templating engine collapses to a string here, so the claimed
  "native LangSmith tracing / strict validation" upside doesn't survive the
  boundary — the model call sees a pre-rendered string no matter what built it.
- **Even if it existed, wrong shape.** It's a flat token-replacement renderer;
  the proposal itself admits Phase 2 needs empty-string hacks for conditionals
  and Phase 3 has no block overrides. Our problems are **content architecture**
  (audience partitioning, dev-doc leak, section inheritance), not **string
  rendering**. A renderer doesn't touch any of them.

### What survives in 1.4.8 + where upstream genuinely helps
`langchain_core.prompts` exports `PromptTemplate`, `ChatPromptTemplate`,
`MessagesPlaceholder`, and ships **Jinja2 as a first-class formatter**
(`jinja2_formatter`, `validate_jinja2`, `template_format="jinja2"`).

The real upstream moves, mapped to our phases — none of which is
`PipelinePromptTemplate`:

| Need | Upstream-native home | Status |
|---|---|---|
| Conditional / state-aware / audience-scoped injection (D2) | **LangGraph middleware onion** | already our pattern (WebRouter, PrepareWarmup, Skills, Memory) |
| Tool docs single-sourced (D6) | **tool-schema pipeline** (docstrings → bound tools) | upstream already; just stop re-describing in prose |
| Prompt tracing / introspection (D8) | **Langfuse / LangSmith** | already wired ([[langfuse-observability]]) |
| Block-level inheritance (D4, *if* we do it) | **Jinja2** (`{% extends %}`/`{% block %}`/`{{ super() }}`) | language is upstream; langchain exposes variable-substitution flavor via `template_format="jinja2"`, but multi-file `extends` needs a Jinja2 `Environment(loader=...)` we'd wire ourselves — still upstream, ~small |

### Refined verdict
- **Keep `assemble_prompt`.** It's ~15 lines: ordered, conditional, no-gap join.
  That is the irreducible pux-specific core (audience manifest + ordering). No
  upstream primitive partitions content by audience for a multi-org harness —
  that's legitimately ours.
- **Phase 1 unchanged** (split `base.md` + D6 docstrings).
- **Phase 2** = make `assemble_prompt` data-driven (the manifest IS the part
  list). Still ours, still tiny.
- **Phase 3** = the *only* phase that needs real template power, and the right
  upstream engine for it is **Jinja2** (alive, first-class), not the dead
  pipeline primitive. Decide when we get there.
- **Phase 4** = Langfuse already gives us most of D8.

The "lean upstream" instinct redirected correctly — just not at
`PipelinePromptTemplate`.

### 6.1 The LCEL pipe doesn't fill the gap either (verified)
A follow-up proposed the modern replacement LangChain *does* recommend — the
`|` pipe / LCEL (`prompt | model | parser`). Verified it's the **wrong axis**:
`p1 | p2` is a `RunnableSequence` that feeds p1's **output as p2's input**
(dataflow). Proven by invoking `p1|p2` with `{x:"hello"}` — it nested p1's
rendered message object as the `{x}` value *inside* p2, producing
`SUFFIX: messages=[HumanMessage(content='BASE: hello'...)]`. That is runnable
**chaining**, not text-section **concatenation**.

The closest `ChatPromptTemplate` fit for our need is one system message with
`{base}\n\n{suffix}\n\n{ask_user}` vars — i.e. `str.format` with extra steps,
and it reintroduces the empty-string conditional friction (`ask_user=""` →
dangling `\n\n`) that `assemble_prompt` already solves via the no-gap property.

**Bottom line:** LangChain ships **no section-assembly primitive by design** —
the modern stack expects the system prompt to arrive finished (`str |
SystemMessage`). Section composition is the application's job. "Lean upstream"
correctly means: keep `assemble_prompt` (15 lines, our domain logic), and lean
on LangChain where it genuinely fits (middleware, tool schemas, Langfuse,
Jinja2-if-D4) — *not* on a templating primitive for the section join.
