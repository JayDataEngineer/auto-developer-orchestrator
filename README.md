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
  carrying the operational scripts, plus three tool plugins —
  orchestrator-tools (python exec + describe_image), browser (the 42-tool
  stealth `sandbox_browser` MCP), and opensandbox (the dcode sandbox
  provider).
- **`.deepagents/AGENTS.md`** — workspace instructions, appended to the main
  agent's system prompt every session.
- **`profiles/<name>/`** — the MCP lanes: each profile's `.mcp.json` rides
  into a session via dcode's native `--mcp-config` seam (coding's declares
  ZERO) — see Profiles.
- **`.mcp.json`** — EMPTY by design: MCP is opt-in per lane (context is too
  expensive to spend by default). Servers live in `profiles/<name>/.mcp.json`;
  a bare `dcode` loads none.
- **root `AGENTS.md`** — this repo's developer guide (combined by dcode).

Run it:

```bash
dcode                       # the TUI, the full 30-agent roster, all skills
dcode -n "task..."          # non-interactive
```

## Profiles (MCP lanes on a native session)

dcode's project context — roster, skills, AGENTS.md, `.mcp.json` — comes
from the launch folder via **git-root discovery**; any session in this repo
sees the full union surface by construction. **Profiles scope the MCP lane**:
`profiles/run.py` opens a native dcode session in a folder with the profile's
`.mcp.json` attached through dcode's own `--mcp-config` seam, so only that
lane's servers load (coding declares ZERO — git/github via `execute` + `gh`).

```bash
make coding                 # union roster · ZERO MCP
make research               # union roster · web_research, surreal, equibles, nitter
make invest                 # union roster · equibles, web_research, surreal
make game                   # union roster · godot-mcp-runtime, ray_inference, surreal
make media                  # union roster · ray_inference, surreal
make social                 # union roster · nitter, surreal (browser via the async subagent)
make profiles-check         # dry-run every profile (roster + skills + MCP lane)
```

`profiles/run.py` is the whole launcher — dcode's own seams and nothing else:
`config._load_dotenv` anchored to the repo (workspace credentials from any
launch folder), a single `chdir` into the session folder (the server's
project context IS the process CWD — git-root discovery picks up roster,
skills, AGENTS.md there), the native `mcp_config_path` seam for the lane's
servers, then `run_textual_app` / `run_non_interactive` with `server_kwargs`
— the app launches the graph server itself and the TUI talks to a
server-backed `RemoteAgent` (the approval-mode Store lives on that server).
Zero monkey patches, zero ProjectContext overrides.

`dwork` (any folder) is the same launcher on the coding lane:
`profiles/run.py coding -M local-qwen:qwen3.8-27b --cwd <folder>` — the
folder IS the workspace, the zero-MCP lane and the local model ride along.
The headless E2E seam is `-n "task..."` (dcode's `run_non_interactive`, the
same server machinery).

MCP trust is dcode's native flow: servers with a saved approval in
`~/.deepagents/config.toml` load; new ones prompt once (interactive) or are
skipped (headless) — a committed `.mcp.json` can never self-approve. Extra
flags reach the launcher directly:
`profiles/run.py coding -M provider:model -m "task..." -n "headless task"`.

## Subagent tool isolation (browser-specialist)

dcode subagents inherit the main agent's full toolset by design — there is no
per-subagent tool allowlist yet. For the one specialist whose toolset must
NEVER enter a session's context window (the 42-tool stealth browser), the
isolation is spatial instead: the browser runs as a **remote async subagent**
on [Aegra](https://aegra.dev) (the self-hosted LangGraph Platform alternative)
and dcode reaches it through its native seam:

```toml
# ~/.deepagents/config.toml — dcode's own async-subagent registry
[async_subagents.browser-specialist]
description = "42-tool stealth browser (SeleniumBase Pure CDP) — …"
url = "http://127.0.0.1:2026"
graph_id = "browser_specialist"
```

The main agent sees only the five `*_async_task` middleware tools; none of the
42 browser tool schemas load into any dcode session (coding profile: 0 MCP
tools — the lane runs on the 14 built-ins; git/github via `execute` + `gh`).
Delegating is native model behavior:
`start_async_task` (gated by dcode's HITL approval) → `check_async_task`
until `success` → the specialist's final message arrives as the task result.

Isolation holds in **both directions, at three boundaries**: (1) context —
dcode never loads the browser schemas, the specialist never gets the main
agent's tools; (2) tools — the graph's `create_deep_agent` drops every
built-in file/shell/subagent tool via a `HarnessProfile` (`excluded_tools` +
`general_purpose_subagent=disabled`), so its toolset is exactly the 42
browser tools (proved with a `bind_tools` spy); (3) host — mc_browser and
its Chromium run inside an **OpenSandbox container** (the platform we
already run on `:8080`): no credentials, no host mounts, no host reach. A
live hostile prove had the agent fire its `run` escape hatch (arbitrary
Python) and `file://` navigation at the host `.env` path — nothing to find.

```bash
make aegra-sandbox-image    # once: build the workload image (mc_browser + Chromium)
make aegra                  # start the deployment (Aegra :2026 + its Postgres :5433)
make aegra-status           # health of Aegra + the browser sandbox
make aegra-stop             # stop Aegra (the browser sandbox is left running by design)
make aegra-sandbox-kill     # teardown the browser sandbox (Chrome state dies with it)
```

- `deployments/browser-specialist/` — the graph (deepagents-core
  `create_deep_agent` + the browser MCP via langchain-mcp-adapters, exposed as
  a langgraph-sdk **runtime factory** so the MCP session lives on the server's
  own event loop), its `aegra.json`, the workload image `sandbox/Dockerfile`,
  and `patches/`.
- The browser sandbox is created on the first browser task (lease 23h,
  renewed every 12h and on every Aegra restart), so tabs/cookies/sessions
  survive restarts; all per-call MCP sessions land on the one long-lived
  mc_browser HTTP process inside it.
- `patches/aegra-thread-values.patch` — Agent Protocol conformance fix
  (Aegra's `GET /threads/{id}` omitted `values`, which deepagents'
  `check_async_task` reads as the completed run's result); applied to the
  deployment venv by `make aegra-patch` (also a dependency of `make aegra`)
  and prepared as an upstream PR.
- `make aegra` needs `deployments/browser-specialist/.env` (postgres port,
  auth mode, `ANTHROPIC_AUTH_TOKEN` — same key the dcode model uses).

**When does a specialist go remote?** Only when its toolset is too heavy (or
too untrusted) to sit in the session's context window *and* its work doesn't
need millisecond local feedback. The execution spectrum every subagent lands
on, by design:

| Tier | Mode | Who | Why |
|------|------|-----|-----|
| heavy/untrusted tools | **remote AP + OpenSandbox** (Aegra async subagent, browser in a container) | browser-specialist | 42 tool schemas never enter a session; Chrome/captcha stack runs in a sandbox with no credentials and no host reach |
| repo work | **standard dcode subagent** (in-process) | coder, reviewer, explorer, all `.deepagents/agents/*` | millisecond file I/O, local git state, direct rg/AST loops — an RPC boundary would add a file-sync tax to every edit |
| code execution | **sandboxed** (`dcode --sandbox opensandbox`) | every agent's execute/read/write — subagents included (proved: they share the session's sandboxed backend) | arbitrary commands run inside the OpenSandbox container, not on the host |

Editing is direct; only *execution* is sandboxed; only *heavy tools* are
remote — and when they are, their execution is sandboxed too (tier one
stacks both). A new specialist joins the remote tier only when its tool
schemas or attack surface justify paying the delegation round-trip.

**The full placement rules live in
[`docs/isolation-patterns.md`](docs/isolation-patterns.md)** — the pattern
catalog: three sandbox tiers (S1 session / S2 on-demand via MCP / S3
workload container), the MCP segmentation ladder (L0 workspace → L1 profile
→ L2 server trim → L2.5 model-keyed subagent exclusion → L3 per-subagent
`tools:` frontmatter → L4 spatial), the measured audit behind them (MCP
inheritance is uniform: game = 50 schemas in all 11 subagents,
docs-writer included), and the per-profile target state. The zero-MCP
rows of that target state are **shipped** via the L2.5 bridge
(`plugins/tool-scoping` + `model: openai:glm-5-turbo` frontmatter +
`make scoping-check` as the fail-open tripwire); the rest wait on the
upstream L3 PR — per-subagent `tools:`/`skills:` frontmatter for dcode,
prepared on `feat/subagent-tools-skills-frontmatter` in the deepagents
checkout (E2E-proven, 12.6k tests green, not pushed).


## Host-side infrastructure

```bash
make infra                  # SurrealDB (:8000) + media-mcp (:8101)
make infra-core             # SurrealDB only
make infra-nitter           # nitter-mcp (:41730, read-only Twitter)
make infra-equibles         # Equibles financial terminal (:43181, self-hosted from vendor/mcp/equibles-mcp)
make qwen                   # local Qwen3.8-27B server (:8388, llama.cpp on the 4090 — see qwen/)
make aegra-sandbox-image    # browser-specialist workload image (once)
make aegra                  # browser-specialist deployment (Aegra :2026)
```

| Service | Port | Used by |
|---------|------|---------|
| **SurrealDB** | `localhost:8000` | deep-research, game-studio, social-media (the shared knowledge graph) |
| **media-mcp** | `localhost:8101` | deep-research (ASR + diarization + vision) — built from the `infra/media-mcp` submodule |
| **nitter-mcp** | `localhost:41730` | twitter reads (opt-in: `make infra-nitter`; needs accounts in `infra/nitter/.env`) — built from `infra/nitter/` |
| **Equibles** | `localhost:43181` | invest/research financial data (SEC, 13F, insiders, congress, FRED, prices) — self-hosted upstream stack (`make infra-equibles`, from `~/Documents/programs/vendor/mcp/equibles-mcp`); the whole deployment — URLs, ports, the meta-mcp catalog of sibling servers — lives in that folder |
| **Aegra** | `localhost:2026` | the isolated browser-specialist subagent (own Postgres on 5433; browser itself in an OpenSandbox container) — see below |
| **Local Qwen** | `localhost:8388` | any session as `-M local-qwen:qwen3.8-27b` — llama.cpp on the 4090, isolated handle in `qwen/` (needs `make qwen`) |

Remote services (Ray cluster, Forge, ComfyUI, CompreFace) are bring-your-own;
the skills name the env vars they read. `.env.example` documents the keys the
MCP servers and scripts consume. The github MCP is the official
`github-mcp-server` binary (`~/.local/bin`, from GitHub releases) reading
`GITHUB_TOKEN`; `make scoping-check` fails loudly if any server's URL
placeholder is unset or a declared server serves zero tools.

## Sandbox (upstream OpenSandbox platform)

The sandbox is the upstream
[OpenSandbox](https://github.com/opensandbox-group/OpenSandbox) platform — no
handrolled container. Its server (Docker runtime) runs on `localhost:8080`;
dcode reaches it through the `opensandbox` MCP server (19 tools:
`sandbox_create`, `sandbox_connect`, `command_run`, `file_*`,
`sandbox_healthcheck`, ...). Built-in environments cover command, filesystem
and code interpreter, plus browser/desktop examples upstream (chrome + VNC,
playwright, desktop, vscode).

One-time install (uv tools). The `mcp<2` pin is required: upstream
`opensandbox-mcp` 0.1.1 imports `mcp.server.fastmcp`, which mcp 2.x moved out.

```bash
uv tool install opensandbox-cli
uv tool install opensandbox-mcp --with "mcp<2"
uv tool install opensandbox-server
```

**dcode's own `--sandbox` seam can drive it too** — the `opensandbox` plugin
ships a provider (entry point `deepagents_code.sandbox_providers`) so
dcode's execute/read/write/glob/grep run inside an OpenSandbox container:

```bash
uv pip install --python "$(uv tool dir)/deepagents-code/bin/python" ./plugins/opensandbox
dcode --sandbox opensandbox
```

Daily use — `make sandbox` writes `~/.sandbox.toml` on first run
(re-run `make sandbox-config` to regenerate); the server runs in insecure
mode (no API key), which is fine for this single-user box:

```bash
make sandbox                # start the server
make sandbox-status         # health probe
make sandbox-stop           # stop the server
dcode                       # opensandbox tools arrive via a lane's .mcp.json (root loads none)
osb sandbox create --image python:3.12   # the upstream CLI, same server
```

## Where things live

| Path | What |
|------|------|
| `.deepagents/` | the authored dcode surface (agents, skills, workspace instructions) |
| `profiles/` | scoped dcode project roots + the native-API launcher (`run.py`) |
| `.mcp.json` | EMPTY — MCP is opt-in per lane; servers live in `profiles/<name>/.mcp.json` (env placeholders like `${MCP_WEB_RESEARCH_URL}` resolve from the environment) |
| `plugins/` | the in-repo dcode marketplace (see below) — install with `dcode plugin install <name>@orchestrator` |
| `infra/` | MCP-server infrastructure (media-mcp, nitter) + compose files |
| `docs/` | engineering history (thin-architecture mandate, retired prod list, browser-specialist isolation record) |

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
- **browser** restores the pux-era `mc_browser` as a FastMCP stdio server
  (`sandbox_browser`, 42 tools: navigate/read/click/type_text/search/
  screenshot/cookies/solve_captcha/...) driving a local Chrome/Chromium via
  SeleniumBase Pure CDP Mode — no chromedriver, stealth flags, one Chrome per
  server process. Env: `MC_BROWSER_CHROME` (binary override),
  `MC_BROWSER_HEADLESS=1` for display-less hosts. Self-contained
  (`uv run --with fastmcp --with seleniumbase`).
- **opensandbox** ships the dcode sandbox provider (see the Sandbox section)
  plus the usage skill.
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
  model). Three agents use that seam deliberately:
  `model: openai:glm-5-turbo` opts into the **no-MCP tier**
  (`game-studio-docs-writer`, `task-planner`, `web-agent`) — a harness
  profile registered by `plugins/tool-scoping` strips every MCP tool from
  exactly those subagents (see docs/isolation-patterns.md, L2.5). The
  `openai:` prefix resolves against the same z.ai gateway as the main
  model via `OPENAI_BASE_URL`/`OPENAI_API_KEY` in `.env` (the plugin also
  pins the gateway's chat-completions-only behavior for that exact model
  key, overriding the built-in Responses-API default); after a dcode
  upgrade, reinstall both plugins:
  `uv pip install --python "$(uv tool dir)/deepagents-code/bin/python" ./plugins/opensandbox ./plugins/tool-scoping`
  and run `make scoping-check` (structural tripwire) or
  `make scoping-e2e` (behavioral — real turns + a live MCP round trip,
  spends tokens).

## History

This repo previously hosted a pux harness: a profiles tree compiled onto the
dcode surface (`src/compiler/`), a per-org launcher (`src/run.py`), a tool
registry, rubric middleware, and a dual-track agent server. All of it was
deleted on 2026-08-16 — dcode already does everything it did. What survived
is exactly the parts dcode reads: the agents, the skills (now carrying their
own scripts), the MCP config, and the content. See `docs/thin-architecture.md`
for the governing philosophy.
