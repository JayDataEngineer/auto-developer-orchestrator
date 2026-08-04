# The Org Declarative Surface

> **Closed vocabulary.** Every declarative field an org can use is listed
> here. No new fields without adding them to this doc AND a contract rule
> that validates them. An org is a thin data layer — code lives in the
> harness, not in the org tree.

An org is a directory under `orgs/` (top-level or `orgs/specialists/`) with
a fixed set of files. Every file is data; the harness interprets it. If you
need a new behavior, add it to the harness — not as a one-off field on one
org.

## File inventory

```
orgs/<name>/
├── AGENTS.md            ← the CTO behavior overlay (prose-only, no frontmatter)
├── org.yaml             ← the roster + extends + roster_deny
├── profile.yaml         ← middleware, models, rubric, suffixes, retry config
├── policy.yaml          ← egress ACL, sandbox, browser, host_setup hooks
├── models.yaml          ← (optional) org-local model tier overrides
├── agents/              ← one <slug>.md per specialist
│   └── <slug>.md        ← frontmatter (name/description/tools/skills/...) + body
└── skills/              ← progressive-disclosure skills (loaded on demand)
    └── <name>/
        ├── SKILL.md     ← frontmatter (name/description) + the index body
        └── references/  ← the detailed how-to (zero fixed prompt cost)
```

## AGENTS.md — the CTO behavior overlay

Prose-only (YAML frontmatter is a permanent contract failure —
`no-legacy-org-roster`). A specialist that `extends: general` layers its
AGENTS.md AFTER the base — the chain is concatenated root→child with `\n\n`.
**Behavior, not how-to.** How-to goes in skills (progressive disclosure via
`read_file` — zero fixed prompt cost). Budget: 4,000 tokens
(`orgs/_shared/budgets.yaml → default`).

## org.yaml

```yaml
extends: general          # single-hop parent (inherits roster + AGENTS.md + profile)
agents:                   # the roster — slugs this org delegates to
  - researcher
  - browser
roster_deny:              # slugs this org FORBIDS (enforced by contract)
  - general-purpose
```

## profile.yaml — the declarative knobs

```yaml
# Middleware — the gate-as-data model. The resolver merges:
#   DEFAULT_SUPERVISOR ∪ gate-driven ∪ add − remove (add wins)
middleware:
  supervisor:
    add: [interpreter]       # opt-in middleware (e.g. CodeInterpreterMiddleware)
    remove: [prompt_capture]  # opt-out of a default middleware

# Models — per-scope model tiers
models:
  base_model: glm-5.2        # the CTO model (routing + reasoning)
  worker_model: glm-5.2-air  # the specialist model (execution)

# Rubric — the quality gate (RubricMiddleware)
rubric:
  enabled: true
  max_iterations: 3
  default: |                 # the supervisor's own rubric (grades the CTO loop)
    Grade whether...

# The CodeInterpreter dispatch suffix (org opt-in only — no strength auto-gate)
# Already covered by middleware.supervisor.add: [interpreter] above.

# System prompt suffix (appended to EVERY prompt in this org)
system_prompt_suffix: |
  When drafting tweets, always save as a draft...

# Model retry — retry on provider failures
model_retry:
  enabled: true
  max_attempts: 3

# Tool retry — retry on tool execution failures
tool_retry:
  max_attempts: 2

# Web router — route web research calls
web_router:
  enabled: true

# Ask user — turn-ending HITL instruction
ask_user: true

# General-purpose subagent — deepagents' auto-add control
general_purpose_subagent:
  enabled: false             # neuter the auto-add; paired with roster_deny

# Tool description overrides (keyed by full pux_sandbox_* name)
tool_description_overrides:
  pux_sandbox_python:
    description: "..."

# Extra prompt parts — always-on file-injected sections
extra_prompt_parts:
  supervisor:
    - name: custom_context
      file: prompts/custom_context.md

# Excluded tools (removed from the tool surface)
excluded_tools:
  - pux_sandbox_desktop_*

# Excluded middleware (removed from the middleware stack)
excluded_middleware:
  - browser_vision
```

## agents/<slug>.md — frontmatter vocabulary

```yaml
---
name: "my-agent"
description: "What this agent does (shown in the task tool's description)"

# Capabilities (unified sugar — desugars to the kind-specific keys)
capabilities:
  - {kind: tool, ref: browser_navigate}    # a pux_sandbox_* tool
  - {kind: skill, ref: orgs/.../skills}    # a skill directory

# Or the legacy kind-specific keys (still valid, capabilities: desugars to these)
tools:
  - browser_navigate
skills:
  - orgs/specialists/invest/skills

# Model override for this agent
model: glm-5.2-air

# Middleware on this agent (validated against the registry)
middleware: [rubric]

# The agent's own rubric (overrides the supervisor default via _RubricOverride)
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

## policy.yaml — the security boundary

```yaml
egress:                    # the ACL — which hosts/ports the sandbox can reach
  allow:
    - host: api.openai.com
      port: 443

sandbox:                   # Docker sandbox config
  build:
    dockerfile: Dockerfile
    context: .
  tier: isolated           # network mode: isolated (bridge) | host

browser:                   # browser session config
  cookie_bridges:          # host-side cookie extraction hooks

host_setup:                # host-side hooks run BEFORE the container starts
  - name: extract_twitter_cookies
    helper_script: scripts/extract_cookies.sh
    exports:
      TWITTER_COOKIES_B64: {source: stdout}

jobs:                      # prep jobs run inside the sandbox
  - name: warmup
    script: scripts/warmup.sh

tool_surface:              # declared tools (the pux_sandbox_* surface)
  ...
```

## _shared/ — cross-org data

```
orgs/_shared/
├── budgets.yaml              ← token budgets (default/subagent_default/overrides)
├── harness_addendum.md       ← the deepagents delegation addendum (every CTO)
├── ask_user_suffix.md        ← the HITL turn-ending suffix
├── dynamic_dispatch_suffix.md ← the eval-tool dispatch strategy
├── general_purpose.md        ← GP subagent text (front-matter → description/prompt)
├── agents/                   ← cross-org agents (browser, explorer, researcher, web-search)
├── skills/                   ← cross-org skills (source-citation)
└── sandbox/                  ← cross-org backbone scripts
```

## budgets.yaml — the token-budget gate

```yaml
default: 4000              # supervisor prompt budget (tokens, 4 chars/token)
subagent_default: 1500     # subagent prompt budget
overrides: {}              # per-org waivers (each with waiver_reason = tracked debt)
```

`--check-contract` fails when any prompt exceeds its budget. Run
`pux prompt stats --org X` (supervisor) or `pux prompt stats --org X --agent Y`
(subagent) for the per-part breakdown.

## The contract

`pux check-contract` enforces the closed vocabulary offline (no Docker, no
model). The rules (in `pux_harness/agent/contract.py`):

- **No frontmatter in AGENTS.md** — the roster lives in org.yaml.
- **No hard-coded org-name branches** — AST-walks the harness for `if name == "coder":` style branches.
- **Middleware names validated** — every `middleware.add/remove` name must exist in the registry.
- **Interpreter is opt-in only** — no auto-gate; `middleware.supervisor.add: [interpreter]` is the sole path.
- **prompt-budget** — supervisor and subagent prompts within their budgets.
- **roster-deny-enforced** — no denied slug in the effective roster.
- **roster-deny-disables-general-purpose** — GP denial paired with `general_purpose_subagent: {enabled: false}`.
- **Agent extends chain** — acyclic, resolvable, valid delta vocabulary.
- **Policy sections** — only known top-level keys in policy.yaml.
- **No committed artifacts** — `artifacts/`, `.pyc`, `.env*`, session files are gitignored.

## The skill escape hatch

When an agent or CTO prompt is too large, move how-to details to a skill
reference. Skills cost zero fixed prompt tokens — the `SkillsMiddleware`
injects only the name + description at startup; the body is loaded on demand
via `read_file`. Pattern:

1. **AGENTS.md / agent body** = behavior (what to do, when to delegate, stop conditions)
2. **SKILL.md** = the index (table mapping tasks → references)
3. **references/** = the how-to (commands, SQL, directory specs, procedures)

This is the pattern DRE uses: a 120-line AGENTS.md backed by 17 skill
references (~3,000 lines of how-to at zero prompt cost).
