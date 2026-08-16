# Pux — developer guide

> **This is a developer guide for the pux repo (also the Claude Code instruction
> file). It is NOT a runtime prompt.** The agent base prompt lives at
> `profiles/general/AGENTS.md` and flows to every org CTO via `extends:` (orgs layer
> their specialization on top — prompts are additive). Nothing in the runtime
> reads this file.

## Architecture

Two-layer model: **harness/** (lifecycle, policy, scheduling, org roster) +
**console_scripts entry point** (`pux` CLI from `pux_harness.cli:main`).

The harness owns the full sandbox lifecycle. There is no Go MCP server,
no `bridge.py`, no `smoke_mcp.py` — the entire Go sandbox was re-hosted
in Python and deleted. The per-org `bootstrap.sh` +
`docker-compose.yml` shadow lifecycle was also deleted outright;
`policy.yaml` `host_setup` + `sandbox.build` cover host hooks and image
builds (permanent `no-legacy-sandbox-artifacts` tripwire).

### Two-tier Python separation

Backbone scripts under `/sandbox/*.py` are immutable (chmod 0444).
Agent-authored scratch lives under `/sandbox/workspace/scripts/`. Don't
try to edit the backbone.

## Philosophy

1. **One harness, many orgs.** The harness is the single owner of the
   sandbox lifecycle. Each org is a thin layer on top — a `policy.yaml`,
   an `AGENTS.md`, a roster.
2. **Policy over code.** Every org-specific behaviour is declared in
   `policy.yaml` or the org's `AGENTS.md`. The harness never hard-codes
   org logic.
3. **Verify or die.** Run a tool, watch its output, then reason about the
   result. "Should work" is banned.
4. **No fallbacks.** If something breaks, surface the error — don't paper
   over it with a fallback path.

## Conventions

- No co-authored-by Claude in git commits.
- Use astral uv for any Python environments (sandbox scripts, smoke test runner, the harness).
- Prefer 'prove' (integration-style) over 'assert' (unit-only) when feasible.
- "Verify or die" — no claiming a thing works without running it.
- No fallbacks, no deprecation aliases, no backwards-compat shims.
- No emojis unless the user explicitly requests them.
- Keep responses concise — fewer than 4 lines unless the user asks for detail.

## Policy engine

`policy.yaml` declares everything the harness needs per org:

- `allowed_commands` / `forbidden_commands` — shell command guardrails
- `env` — required environment variables
- `image` / `build` — sandbox image selection or build spec
- `host_setup` — host-side hooks run before `create()` (cookie extraction, etc.)
- `tools` — specialist tool whitelist
- `jobs` — explicit job declarations (warn-and-continue runner)

Jobs are triggered via CLI (`pux jobs run --org X`) or server endpoint
(`POST /jobs/{org}/run`). Status is queryable via `pux jobs status --org X`
or `GET /jobs/{org}/status`.

## Per-org harness profile

An OPTIONAL `profiles/<name>/profile.yaml` applies small org-wide overrides to the
deepagents stack the harness compiles for that org — the CTO system prompt AND
every specialist subagent (so the shared `browser` agent inherits it too). It is
NOT a policy file (no egress/sandbox effect); it shapes the agent graph itself:

- `system_prompt_suffix` — appended to the assembled CTO prompt + each
  subagent's prompt.
- `tool_description_overrides` — rewrite a specialist tool's description, keyed
  by its full `pux_sandbox_*` name (nudges how the model calls it in this org).
- `excluded_tools` — drop specialist tools (full `pux_sandbox_*` names) from
  this org's stack entirely.
- `base_system_prompt` — REPLACE the assembled CTO prompt (rarely needed; the
  suffix is the usual lever).

An OPTIONAL `profile.local.yaml` in the same directory is deep-merged on top
(local wins). It is **not** tracked by git (the `.gitignore` catch-all covers
it) — use it for operator-specific overrides that shouldn't land in the shared
repo (account-specific context, personal model preferences, etc.). Absent is
the common case; byte-identical to the original single-file reader.

Two more blocks ride on the same file:

- `models:` — a map overriding the **model-role spec**. The harness resolves
  four roles — `base_model` (CTO driver), `worker_model` (subagents),
  `multimodal_model` (describe_image, decoupled from base), `grader_model` (the
  rubric gate) — all defaulting to `mimo-v2.5` in the shipped
  `pux-harness/pux_harness/agent/models.yaml`. Override per-org here, e.g.
  `models: {grader_model: glm-5.2}`, or per-agent via frontmatter `model:`.
  Resolution: frontmatter `model:` > this `models:` map > `PUX_<ROLE>_MODEL`
  env (legacy `PUX_MODEL` for base) > shipped default. One file to edit when
  cloning — no hardcoded model ids anywhere in the harness.
- `rubric:` — opt the org into deepagents' beta **`RubricMiddleware`
  verify-gate**. The grader (a separate sub-agent on `grader_model`) runs after
  the CTO finishes, grades the deliverable against a ship-gate rubric using
  sandbox evidence tools (`pux_grader_execute` / `pux_grader_read_file` /
  `pux_grader_grep` — run tests, read the diff, grep for regressions; NEVER the
  agent's summary alone), and returns `satisfied` / `needs_revision` /
  `max_iterations_reached`. On `needs_revision` the feedback is fed back and the
  CTO revises, up to `max_iterations`. Per-org opt-in + `enabled: true`; orgs
  without the block are byte-identical to today. The default rubric
  (`rubric.default`) is injected at invoke time by `server._execute` /
  `main._run`; an operator `--rubric` override wins. `profiles/specialists/dev-bot/profile.yaml`
  is the shipped sample (dev-bot is the Claude-Code-equivalent coding org).
- `middleware:` — add/remove named deepagents middleware per scope
  (`supervisor` / `subagent`). A base set (context + routing + session_guide,
  + `rubric` / `tool_retry` when gated) mounts for every org; `add` / `remove`
  override it (same-named add wins). One strength-gated entry: `interpreter` —
  the orchestrator's dynamic-dispatch surface — auto-mounts iff the resolved
  base model is **strong**, else off (a flash / unknown base gets neither the
  tool NOR the dispatch guidance — no token waste on a path it can't drive);
  `add: [interpreter]` / `remove: [interpreter]` override either way. Strength
  is a per-id catalog attribute, NOT a profile field:
  `pux-harness/pux_harness/agent/models.yaml` flags each id `strength: pro`
  (the strong orchestrators glm-5.2 / glm-5.1) or `strength: flash` (every
  other id; unknown → flash = fail-safe).

The loader (`pux-harness/pux_harness/agent/profile.py`) uses deepagents'
`HarnessProfileConfig` SCHEMA but applies the fields at the `build_graph(org)`
call site rather than the global model-keyed `_HARNESS_PROFILES` registry —
that registry has no per-org namespace, so two orgs sharing a model would
collide and the long-lived server path would leak across orgs. Most orgs ship
no profile; absence is a no-op (byte-identical stack). `profiles/specialists/twitter-agent/
profile.yaml` is the shipped sample.

## Testing harness rules

- "Should work" is banned. Verify with a real `pux direct` / `pux dispatch`
  run (ground-truth answer), or a test that exercises the actual code path.
- Adding a Python specialist tool → add it to `SPECIALISTS` (drives the
  contract resolver + the bound surface) and add a test exercising the real
  code path where feasible.
- Adding an Agent Protocol endpoint → add a routing test in
  `harness/tests/test_server.py` (stub graph, no tokens).
- Adding an org / subagent → it must pass `--check-contract` + the
  `test_org_contract.py` gate.

## Branch strategy

- **`pi-pivot`** = current branch. The deepagents pivot.
- **`master`** = pre-pivot MVP. Slim Go MCP server. Frozen.
- **`v0.2.0-pre-pi-mono`** = tag of master HEAD before the pi-mono pivot. Safety net.
- **`dev`** + **`v0.1.0-fullstack-legacy`** = the older fullstack predecessor. Frozen.

## What's NOT here (deferred or dropped)

**Dropped** (deepagents does it natively, or wasn't pulling weight):

- ~~pi-mono TS harness~~ — replaced by the Python harness + Agent Protocol server
- ~~In-process Go agent loop / Go dispatch surface / Go history recorder / Bubble Tea TUI~~ — replaced by deepagents + the Agent Protocol server
- ~~Go MCP server + bridge.py + smoke_mcp.py~~ — re-hosted in Python then deleted
- ~~Per-org bootstrap.sh + docker-compose.yml shadow lifecycle~~ — harness owns the full lifecycle now
- ~~TOML org config~~ — replaced by per-org `AGENTS.md` markdown

**Deferred** (might land later):

- SSE streaming for Agent Protocol runs
- Multi-org orchestration
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Diligence evals, safeguard router

## Memory

Auto-memory lives at `~/.claude/projects/.../memory/`. The memory directory
tracks the strategic context — pivot rationale, fullstack lessons learned,
decisions deferred. Read `MEMORY.md` first when picking up context.
