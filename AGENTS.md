# Auto-developer orchestrator — developer guide

> **This is the developer guide for this repo (also the instruction file
> dcode/Claude Code combine into context). It is NOT a runtime prompt** — the
> main agent's workspace instructions live at `.deepagents/AGENTS.md`.

## Architecture

The repo **IS a plain [dcode](https://docs.langchain.com/oss/deepagents/code/overview)
workspace — zero custom agent machinery.** `dcode` discovers everything:

- `.deepagents/agents/<name>/AGENTS.md` — 30 specialists (frontmatter
  `name` / `description` / optional `model: provider:model` + body prose).
- `.deepagents/skills/<name>/` — 11 skills; each carries its own
  `references/` + `scripts/` (operational scripts live inside the skill).
- `.deepagents/AGENTS.md` — workspace instructions for the main agent.
- `.mcp.json` — MCP servers (authored; env placeholders `${VAR}` resolve at
  activation; project-level trust applies on first run).
- This root `AGENTS.md`.

There is no launcher, no compiler, no tool registry, no middleware wiring —
`dcode` is the harness. The entry point is `dcode` itself.

## Philosophy

1. **The dcode surface is the source of truth, authored in place.** Edit the
   files; there is no build step and nothing can drift.
2. **Thin over upstream.** Never re-implement what deepagents/dcode provides
   (see `docs/thin-architecture.md`).
3. **Verify or die.** Run a tool, watch its output, then reason. "Should
   work" is banned.
4. **No fallbacks.** Surface errors verbatim — don't paper over them.

## Conventions

- No co-authored-by Claude in git commits; commit style is `chore:`-prefixed.
- No emojis unless the user explicitly requests them.
- Skill scripts run from the workspace root; runtime state goes under
  `data/` (git-ignored). Never commit sessions or credentials.
- Models resolve via dcode's `~/.deepagents/config.toml`; per-subagent
  override with frontmatter `model: provider:model` (must name a configured
  provider).
- Host services (SurrealDB, media-mcp) come up with `make infra`; remote
  services (Ray, Forge, ComfyUI, CompreFace) are bring-your-own — the skills
  name the env vars they read.

## Adding to the workspace

- **A subagent** → `.deepagents/agents/<name>/AGENTS.md` (frontmatter
  `name`, `description`; optional `model:`). dcode picks it up next session.
- **A skill** → `.deepagents/skills/<name>/SKILL.md` (frontmatter `name`,
  `description`, optional `allowed-tools`). Supporting scripts and reference
  docs go inside the skill dir.
- **An MCP server** → an entry in `.mcp.json` (+ the env var documented in
  `.env.example`).

## Branch strategy

- **`master`** = the dcode workspace (current).
- Older branches hold pre-fold history. Frozen.

## Memory

Auto-memory lives at `~/.claude/projects/.../memory/`. Read `MEMORY.md` first
when picking up strategic context.
