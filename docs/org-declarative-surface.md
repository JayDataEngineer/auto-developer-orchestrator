# The Org Declarative Surface

> **Closed vocabulary.** Every declarative field an org can use is listed
> here. No new fields without adding them to this doc AND a rule that
> validates them. An org is a thin data layer — code lives in the workspace
> (`src/`), not in the org tree.

An org is a directory under `profiles/` (top-level or `profiles/specialists/`)
with a fixed set of files. Every file is data; the workspace interprets it. If
you need a new behavior, add it to the workspace — not as a one-off field on
one org.

## File inventory

```
profiles/<name>/
├── AGENTS.md            ← the CTO behavior overlay (prose-only, no frontmatter)
├── org.yaml             ← the roster + extends + roster_deny + capabilities
├── profile.yaml         ← system_prompt_suffix, rubric, middleware, overrides
├── policy.yaml          ← egress ACL, credentials, jobs
├── agents/              ← one <slug>.md per specialist
│   └── <slug>.md        ← frontmatter (name/description/tools/skills/...) + body
└── skills/              ← progressive-disclosure skills (loaded on demand)
    └── <name>/
        ├── SKILL.md     ← frontmatter (name/description) + the index body
        └── references/  ← the detailed how-to (zero fixed prompt cost)
```

(`models.yaml` was org-local model-tier overrides pre-fold — **retired**: model
config is dcode's own `_get_default_model_spec()` (`src/run.py`) reading the
operator's deepagents config.)

## AGENTS.md — the CTO behavior overlay

Prose-only (YAML frontmatter is a permanent contract failure —
`no-legacy-org-roster`). A specialist that `extends: general` layers its
AGENTS.md AFTER the base — the chain is concatenated root→child with `\n\n`
(`_chain_overlay` in `src/profiles/loaders.py`).
**Behavior, not how-to.** How-to goes in skills (progressive disclosure via
`read_file` — zero fixed prompt cost). Budget: 4,000 tokens
(`profiles/_shared/budgets.yaml → default`; the budget *gate* itself retired
with the contract lane — see §contract).

## org.yaml

```yaml
extends: general          # single-hop parent (inherits roster + AGENTS.md + profile)
agents:                   # the roster — slugs this org delegates to
  - researcher
  - browser
inherit_roster: false     # (optional) refuse the parent's roster union (consumed
                          #   by _org_inherit_roster in src/profiles/loaders.py)
roster_deny:              # slugs this org FORBIDS
  - general-purpose
capabilities:             # the org's MCP server refs (CU-4 canonical site)
  - {kind: mcp, ref: github}
```

`roster_deny` remains part of the declared vocabulary (the coder org ships it),
but the offline checker that enforced it retired with the contract lane —
post-fold it is data with no runtime consumer; `inherit_roster` is the live
switch.

## profile.yaml — the declarative knobs

```yaml
# Middleware — gate-as-data, the surviving shape. Post-fold the ONLY known
# middleware ref is [rubric] (src/middlewares/rubric.py); an unknown ref raises
# "unknown middleware ref". The pre-fold interpreter/prompt_capture/etc.
# registry is retired.
middleware:
  supervisor:
    add: [rubric]

# Rubric — the quality gate (RubricMiddleware, survives)
rubric:
  enabled: true
  max_iterations: 3
  default: |                 # the supervisor's own rubric (grades the CTO loop)
    Grade whether...

# System prompt suffix (appended to EVERY prompt in this org)
system_prompt_suffix: |
  When drafting tweets, always save as a draft...

# General-purpose subagent — deepagents' auto-add control
# (DORMANT DATA: declared vocabulary, no runtime consumer in src/)
general_purpose_subagent:
  enabled: false             # neuter the auto-add; paired with roster_deny

# Tool description overrides (keyed by full pux_sandbox_* name)
tool_description_overrides:
  pux_sandbox_python:
    description: "..."
```

**Retired/dormant keys (pre-fold vocabulary, no runtime consumer post-fold):**
`models:` (`base_model`/`worker_model` — the tier system died with the model
factory), `middleware.supervisor.add: [interpreter]` (interpreter lane died;
the ref now raises), `model_retry`, `tool_retry`, `web_router`, `ask_user` (the
HITL turn-ending instruction is file-sourced instead: `profiles/_shared/
ask_user_suffix.md`), `extra_prompt_parts`. They parse and pass through, but
nothing reads them — delete them from a profile when you touch it.

## agents/<slug>.md — frontmatter vocabulary

```yaml
---
name: "my-agent"
description: "What this agent does (shown in the task tool's description)"

# Capabilities (unified sugar — desugars to the kind-specific keys via
# src/compiler/capabilities.py)
capabilities:
  - {kind: tool, ref: python}              # a REGISTRY tool (pux_sandbox_*)
  - {kind: mcp, ref: sandbox_browser}      # an MCP server the org declared
  - {kind: skill, ref: profiles/.../skills}  # a skill directory

# Or the legacy kind-specific keys (still valid, capabilities: desugars to these)
tools:
  - python
skills:
  - profiles/specialists/invest/skills

# Model override for this agent
model: glm-5.2-air

# Middleware on this agent (validated against the known set: [rubric])
middleware: [rubric]

# The agent's own rubric (overrides the supervisor default)
rubric: |
  Grade whether...

# Extends another agent (base body + delta body joined with \n\n)
extends: browser

# System prompt suffix (appended after the body)
system_prompt_suffix: |
  ...

# Delta vocabulary for extends:
tools_add: [...]           # add to base tool list
tools_remove: [...]        # remove from base tool list
skills_add: [...]          # add to base skill list
description_append: "..."  # append to base description
tool_description_overrides:
  ...                      # per-key merge, delta wins
---

The body IS the system_prompt — the specialist's behavior prose.
Budget: 1,500 tokens (budgets.yaml → subagent_default).
```

Merging is `_merge_extends` in `src/profiles/loaders.py`; a declared key must
never silently vanish from the merged spec (unconsumed keys carry over, delta
wins). Per-agent `mcp:` refs must name a server the org actually loaded —
otherwise `org_subagent_specs` (`src/profiles/subagents.py`) raises.

## policy.yaml — the security boundary (surviving sections)

```yaml
egress:                    # the ACL — which hosts/ports the org can reach
  allow:
    - host: api.openai.com
      port: 443

credentials:
  required:
    - ALPACA_API_KEY
  optional:
    - FRED_API_KEY
```

**Retired with the container sandbox (2026-08 fold):** the `sandbox:` block
(docker build/tier), `host_setup:` (host-side hooks before container start),
and in-container `jobs:`. The workspace backend is deepagents'
`LocalShellBackend` (`src/sandbox/local.py`) — host fs, no container, so
egress/tier machinery has nothing to stage. Cookie-bridge helper scripts still
live in `profiles/_shared/sandbox/` (e.g. `extract_browser_cookies.py`) but are
invoked by the operator, not by a hook runner.

## _shared/ — cross-org data

```
profiles/_shared/
├── budgets.yaml              ← token budgets (default/subagent_default/overrides)
├── harness_addendum.md       ← dormant prose (documented reader, not wired)
├── ask_user_suffix.md        ← the HITL turn-ending suffix
├── dynamic_dispatch_suffix.md ← the eval-tool dispatch strategy
├── general_purpose.md        ← GP subagent text (front-matter → description/prompt)
├── agents/                   ← cross-org agents (browser, explorer, researcher, web-search)
├── skills/                   ← cross-org skills (source-citation)
└── sandbox/                  ← cross-org backbone scripts
```

All three `*_suffix.md`/`addendum` prompt blocks are **dormant prose**:
`load_shared_prompt_body` (`src/profiles/loaders.py`) is their single documented
reader, but no runtime call passes them into `build_system_prompt` — the
supervisor prompt is the extends-chain overlay alone (`src/run.py` passes no
addendum).

## budgets.yaml — the token-budget data

```yaml
default: 4000              # supervisor prompt budget (tokens, 4 chars/token)
subagent_default: 1500     # subagent prompt budget
overrides: {}              # per-org waivers (each with waiver_reason = tracked debt)
```

**Dormant data post-fold.** The enforcement command (`pux prompt stats`, the
`--check-contract` budget rule) retired with the contract lane; nothing in
`src/` reads `budgets.yaml`. It remains the documented budget convention.

## The contract

Post-fold, "the contract" is enforced in three places, all offline (no model,
no container):

1. **Fail-loud loaders** — `src/profiles/loaders.py` (unknown agent frontmatter
   keys raise in `_load_agent_spec`), `src/middlewares/rubric.py` (unknown
   `middleware:` ref raises), `src/profiles/subagents.py` (an `mcp:` ref naming
   no loaded server raises).
2. **The compiler surface** — `src/compiler/capabilities.py` desugars
   `capabilities:`; `pux sync --check` / `pux check` (src/compiler/cli.py)
   drift-checks the emitted union `.deepagents/` + `.mcp.json` against the
   checked-in surface (exit 1 on drift).
3. **Tripwires** — `tests/guards/tripwire_checks.py`: `kit-import-isolation`
   (the pure compiler surface never imports the heavy runtime) and the
   no-harness-refs gate (the workspace has no harness).

The pre-fold rule list (in the retired `contract.py`) survives in spirit:
no frontmatter in AGENTS.md; no hard-coded org-name branches; middleware names
validated; extends-chains acyclic + resolvable; policy top-level keys checked;
no committed artifacts (`.pyc`, `.env*`, session files gitignored). Retired
rules: `interpreter-opt-in` (the lane died) and `prompt-budget` (dormant, see
above).

## The skill escape hatch

When an agent or CTO prompt is too large, move how-to details to a skill
reference. Skills cost zero fixed prompt tokens — the `list_skills` specialist
tool + deepagents `SkillsMiddleware` inject only the name + description at
startup; the body is loaded on demand via `read_file`. Pattern:

1. **AGENTS.md / agent body** = behavior (what to do, when to delegate, stop conditions)
2. **SKILL.md** = the index (table mapping tasks → references)
3. **references/** = the how-to (commands, SQL, directory specs, procedures)

This is the pattern DRE uses: a 120-line AGENTS.md backed by 17 skill
references (~3,000 lines of how-to at zero prompt cost).
