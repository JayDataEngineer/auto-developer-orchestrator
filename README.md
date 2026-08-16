# Auto-developer orchestrator — a dcode workspace

**This repo IS a [dcode](https://docs.langchain.com/oss/deepagents/code/overview)
(Deep Agents Code) workspace — nothing more.** There is no harness library, no
launcher, no compiler, no custom agent code: `dcode` discovers the whole
surface at startup and provides the runtime (graph, TUI, tools, model
resolution). Everything dcode reads is **authored in place**:

- **`.deepagents/agents/<name>/AGENTS.md`** — the 30 specialists (frontmatter
  `name` / `description` / optional `model: provider:model`).
- **`.deepagents/skills/<name>/`** — 5 workspace skills (browser
  interaction conventions + vendored flows), each owning its `references/`
  and `scripts/`.
- **`plugins/`** — the in-repo `orchestrator` marketplace: 6 skill
  families (deep-research, investment-analysis, twitter-automation,
  telegram-automation, game-studio-workflows, social-media-publishing)
  carrying the operational scripts, plus the orchestrator-tools MCP server
  (python exec + describe_image).
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
| **nitter-mcp** | `localhost:41730` | twitter reads (opt-in: `make infra-nitter`; needs accounts in `infra/nitter/.env`) — built from `infra/nitter/` |

Remote services (Ray cluster, Forge, ComfyUI, CompreFace) are bring-your-own;
the skills name the env vars they read. `.env.example` documents the keys the
MCP servers and scripts consume.

## Sandbox (upstream OpenSandbox platform)

The sandbox is the upstream
[OpenSandbox](https://github.com/opensandbox-group/OpenSandbox) platform — no
handrolled container. Its server (Docker runtime) runs on `localhost:8080`;
dcode reaches it through the `opensandbox` MCP server (19 tools:
`sandbox_create`, `sandbox_connect`, `command_run`, `file_*`,
`sandbox_healthcheck`, ...). Built-in environments cover command, filesystem
and code interpreter, with browser/desktop examples upstream (chrome + VNC,
playwright, desktop, vscode).

```bash
uv tool install opensandbox-cli       # the osb CLI
uv tool install opensandbox-mcp --with "mcp<2"   # MCP server — upstream 0.1.1
                                          # imports mcp.server.fastmcp, which
                                          # mcp 2.x moved out; pin 1.x
uv tool install opensandbox-server    # the sandbox server
make sandbox-config      # once: write ~/.sandbox.toml (docker runtime example)
make sandbox             # start the server (insecure mode, no API key)
make sandbox-status      # health probe
dcode                    # opensandbox tools arrive via .mcp.json
osb sandbox create --image python:3.12   # the upstream CLI (same server)
make sandbox-stop        # stop the server
```

## Where things live

| Path | What |
|------|------|
| `.deepagents/` | the authored dcode surface (agents, skills, workspace instructions) |
| `.mcp.json` | MCP servers (env placeholders like `${MCP_WEB_RESEARCH_URL}` resolve from the environment) |
| `plugins/` | the in-repo dcode marketplace (see below) — install with `dcode plugin install <name>@orchestrator` |
| `infra/` | MCP-server infrastructure (media-mcp, nitter) + compose files |
| `docs/` | engineering history (thin-architecture mandate, retired prod list) |

## Plugins (in-repo marketplace)

The old pux action tools and the operational skill families ship as dcode
plugins under `plugins/`, registered as the `orchestrator` marketplace
(`plugins/.claude-plugin/marketplace.json`):

```bash
dcode plugin marketplace add ./plugins    # one-time: register the marketplace
dcode plugin install <name>@orchestrator  # e.g. deep-research@orchestrator
dcode plugin list                         # shows enabled state
```

- **Plugin skills are namespaced** — invoke as
  `/skill:deep-research@orchestrator:deep-research`. They don't show in
  `dcode skills list`; they load in-session.
- **orchestrator-tools** restores the pux action tools as an MCP stdio
  server: `python` (host-side exec, JSON envelope) and `describe_image`
  (vision via an OpenAI-compatible endpoint — `VISION_API_URL` /
  `VISION_MODEL` / `VISION_API_KEY`, see `.env.example`).
- Install copies a plugin to dcode's managed cache; the originals stay
  in-repo as the source of truth. `/reload` re-reads after edits.

## Conventions

- The dcode surface is **authored, not generated** — edit `.deepagents/`
  and `.mcp.json` directly; there is no sync step and nothing to drift.
- Every skill script runs from the workspace root and resolves its own
  state under `data/` (sessions, credentials, caches — git-ignored).
- Model defaults come from dcode's own `~/.deepagents/config.toml`; a
  subagent overrides via frontmatter `model:`. Custom `class_path`
  providers (config.toml) resolve for the **main** agent only — subagent
  `model:` frontmatter goes through langchain's provider inference, so it
  must name a langchain-known provider or be omitted (inherit the runtime
  model). The four media/game-studio specialists omit it.

## History

This repo previously hosted a pux harness: a profiles tree compiled onto the
dcode surface (`src/compiler/`), a per-org launcher (`src/run.py`), a tool
registry, rubric middleware, and a dual-track agent server. All of it was
deleted on 2026-08-16 — dcode already does everything it did. What survived
is exactly the parts dcode reads: the agents, the skills (now carrying their
own scripts), the MCP config, and the content. See `docs/thin-architecture.md`
for the governing philosophy.
