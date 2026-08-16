# Pux — developer guide

> **This is a developer guide for the pux repo (also the Claude Code instruction
> file). It is NOT a runtime prompt.** The agent base prompt lives at
> `profiles/_shared/` and flows to every org CTO via `extends:` (orgs layer
> their specialization on top — prompts are additive). Nothing in the runtime
> reads this file.

## Architecture

The repo **IS a dcode workspace** — no harness library, no dual track. The org
tree (`profiles/`) projects onto dcode's native surface via `src/`:

- **`src/compiler/`** — the projection: `uv run pux sync` emits the union
  `.deepagents/` (agents + skills) + `.mcp.json` at the project root; `pux
  check` drift-compares the checked-in surface; `pux compile --org X --out D`
  stages one org's layout.
- **`src/run.py`** — the launch: `build_org_agent` = dcode's
  `create_deep_agent`, `launch` = dcode's `run_textual_app` (the same TUI a
  bare `dcode` run shows). Model default = dcode's own
  `_get_default_model_spec()`; per-agent override via frontmatter `model:`.
- **`src/profiles/`** — loaders (`discover_orgs`, `org_agent_slugs`,
  `build_system_prompt`, `_load_agent_spec`), subagents (roster → native
  `SubAgent` dicts).
- **`src/tools/`** — the 11-tool registry (`registry.py` + `resolve.py`).
- **`src/sandbox/`** — deepagents' `LocalShellBackend` (same backend dcode's
  CLI uses — no container, no gateway).
- **`src/middlewares/`** — `rubric.py`: a rostered agent's `middleware:
  [rubric]` + `rubric:` frontmatter prose map onto deepagents'
  `RubricMiddleware`.
- **`src/protocol/`** — `.mcp.json` projection; **`src/plugins/`** — the
  `pux-orgs` plugin marketplace.

The server lane (when a server is needed) is deepagents' own **ACP package + a
JSON adapter file** — never custom code.

## Philosophy

1. **One dcode workspace, many orgs.** Each org is a thin layer on top —
   an `AGENTS.md` overlay, an `org.yaml` roster, `agents/*.md` specs, skills.
   The compiler projects them onto dcode's native surface.
2. **Policy over code.** Every org-specific behaviour is declared in
   `policy.yaml` or the org's `AGENTS.md`. `src/` never hard-codes org logic.
3. **Verify or die.** Run a tool, watch its output, then reason about the
   result. "Should work" is banned.
4. **No fallbacks.** If something breaks, surface the error — don't paper
   over it with a fallback path. (The tool-layer fallback nets in `src/tools/`
   are the deliberate exception — they route a failed primary model to the
   ONNX tier and report the reason honestly.)

## Conventions

- No co-authored-by Claude in git commits.
- Use astral uv for any Python environments.
- Prefer 'prove' (integration-style) over 'assert' (unit-only) when feasible.
- "Verify or die" — no claiming a thing works without running it.
- No fallbacks, no deprecation aliases, no backwards-compat shims.
- No emojis unless the user explicitly requests them.
- Keep responses concise — fewer than 4 lines unless the user asks for detail.

## Policy engine (dormant arm)

`policy.yaml` + `sandbox/` + `_shared/budgets.yaml` are the opt-in sandbox
arm's data — the `PUX_SANDBOX=openshell` adapter (its langchain adapter pins
deepagents<0.6) is deliberately absent from this workspace, so nothing reads
them today. They document the org's intended shell policy
(`allowed_commands` / `forbidden_commands`, `env`, sandbox image/tier,
budgets) for when the arm returns.

## Per-org composition (the native surface)

The org spec is 100% dcode format — no custom config format:

- `org.yaml` — `extends:` chain, `agents:` roster (set-union with the
  parent), `inherit_roster:`, `capabilities:` (MCP refs resolved against
  `_shared/tool_servers.yaml`; every declared ref must load or the launch
  raises — the org's surface can never silently shrink).
- `AGENTS.md` — the org's CTO overlay; parent + own concatenated own-last
  (root→child). The chain overlay IS the system prompt.
- `agents/<slug>.md` — one file per rostered subagent: YAML frontmatter
  (`name`, `description`, `tools:` refs, `model:`, `mcp:` refs,
  `middleware: [rubric]` + `rubric:` prose → deepagents' own
  `RubricMiddleware` — a grader sub-agent runs after the CTO finishes and
  grades the deliverable against that prose, then returns
  `satisfied` / `needs_revision` / `max_iterations_reached`) + body =
  system-prompt prose.
- `skills/` — dcode skills; bodies peeked with `read_file`.

Every declared ref resolves or the build raises. Model defaults are dcode's
own (`_get_default_model_spec()`); per-agent override via frontmatter
`model:`.

(The legacy `profile.yaml` format — `system_prompt_suffix`, `ask_user`,
`models:`, org-level `rubric:`/`middleware:` — was retired at the 2026-08-16
fold: it had no reader in the dcode track. Its still-live prose was folded
into the org overlays. `profile.local.yaml` is likewise inert legacy.)

## Testing harness rules

- "Should work" is banned. Verify with a real run
  (`uv run python src/run.py --org <name> --dry-run` for the plan; `dcode` in
  the repo for the TUI), or a test that exercises the actual code path.
- Adding a Python tool → add it to the registry (`src/tools/registry.py`) and
  add a test exercising the real code path where feasible.
- Adding an org / subagent → `uv run pux sync` must still emit cleanly and
  `uv run pux check` must exit 0 (the checked-in `.deepagents/` is
  sync-tested).
- The guards in `tests/guards/` enforce the workspace invariants: no
  `pux_harness` refs in `src/`, no re-implementations of dcode's surface.

## Branch strategy

- **`master`** = the dcode workspace (current).
- Older branches (`pi-pivot`, `v0.1.0-fullstack-legacy`, `v0.2.0-pre-pi-mono`)
  hold the pre-fold history. Frozen.

## What's NOT here (deferred or dropped)

**Dropped** (dcode does it natively, or wasn't pulling weight):

- ~~The pux-harness submodule / dual-track harness~~ — the repo IS the
  workspace; `src/` is the projection layer, dcode is the runtime.
- ~~Custom Agent Protocol server overlay (runtime/, site/, aegra)~~ — the
  server lane is deepagents' own ACP package + a JSON adapter file.
- ~~Docker sandbox + `PuxSandboxBackend` + the 40 `pux_sandbox_*`
  specialists~~ — the sandbox is deepagents' `LocalShellBackend`; the tool
  surface is the 11-tool `src/tools/` registry.
- ~~Second graph builder (`kit/compile.py`) + legacy `pux` subcommands~~ —
  the only graph builder is `create_deep_agent`; `pux` = sync/check/compile.
- ~~`orgs/`~~ — renamed `profiles/` (folder-only; the concept vocabulary —
  `--org`, `org.yaml`, `org_agent_slugs`, `discover_orgs`, `PUX_ORG_PATHS`,
  `pux-orgs` — is unchanged).

**Deferred** (might land later):

- Multi-org orchestration
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Diligence evals, safeguard router

## Memory

Auto-memory lives at `~/.claude/projects/.../memory/`. The memory directory
tracks the strategic context — fold rationale, legacy lessons learned,
decisions deferred. Read `MEMORY.md` first when picking up context.
