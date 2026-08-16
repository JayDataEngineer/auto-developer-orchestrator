# Auto-developer orchestrator — a dcode workspace

**This repo IS a [dcode](https://docs.langchain.com/oss/deepagents/code/overview)
(Deep Agents Code) workspace — nothing more.** There is no harness library, no
launcher, no compiler, no custom agent code: `dcode` discovers the whole
surface at startup and provides the runtime (graph, TUI, tools, model
resolution). Everything dcode reads is **authored in place**:

- **`.deepagents/agents/<name>/AGENTS.md`** — the 30 specialists (frontmatter
  `name` / `description` / optional `model: provider:model`).
- **`.deepagents/skills/<name>/`** — the 11 skills. Each skill owns its
  supporting files: `references/` docs and `scripts/` (the operational
  scripts — research pipeline, investing, game studio, telegram/twitter
  sessions, media clients).
- **`.deepagents/AGENTS.md`** — workspace instructions, appended to the main
  agent's system prompt every session.
- **`.mcp.json`** — the project's MCP servers (dcode gates them behind
  workspace trust on first run).
- **root `AGENTS.md`** — this repo's developer guide (combined by dcode).

Run it:

```bash
dcode                       # the TUI, the full 30-agent roster, all skills
dcode -n "task..."          # non-interactive
```

## Host-side infrastructure

```bash
make infra                  # SurrealDB (:8000) + media-mcp (:8101)
make infra-core             # SurrealDB only
```

| Service | Port | Used by |
|---------|------|---------|
| **SurrealDB** | `localhost:8000` | deep-research, game-studio, social-media (the shared knowledge graph) |
| **media-mcp** | `localhost:8101` | deep-research (ASR + diarization + vision) — built from the `infra/media-mcp` submodule |

Remote services (Ray cluster, Forge, ComfyUI, CompreFace) are bring-your-own;
the skills name the env vars they read. `.env.example` documents the keys the
MCP servers and scripts consume.

## Where things live

| Path | What |
|------|------|
| `.deepagents/` | the authored dcode surface (agents, skills, workspace instructions) |
| `.mcp.json` | MCP servers (env placeholders like `${PUX_MCP_WEB_RESEARCH_URL}` resolve from the environment) |
| `scripts/` | one-off host utilities (e.g. `preprocess_pipeline.py`, referenced by the deep-research skill) |
| `infra/` | MCP-server infrastructure (media-mcp, nitter) + compose files |
| `docs/` | engineering history (thin-architecture mandate, retired prod list) |

## Conventions

- The dcode surface is **authored, not generated** — edit `.deepagents/`
  and `.mcp.json` directly; there is no sync step and nothing to drift.
- Every skill script runs from the workspace root and resolves its own
  state under `data/` (sessions, credentials, caches — git-ignored).
- Model defaults come from dcode's own `~/.deepagents/config.toml`; a
  subagent overrides via frontmatter `model:`.

## History

This repo previously hosted a pux harness: a profiles tree compiled onto the
dcode surface (`src/compiler/`), a per-org launcher (`src/run.py`), a tool
registry, rubric middleware, and a dual-track agent server. All of it was
deleted on 2026-08-16 — dcode already does everything it did. What survived
is exactly the parts dcode reads: the agents, the skills (now carrying their
own scripts), the MCP config, and the content. See `docs/thin-architecture.md`
for the governing philosophy.
