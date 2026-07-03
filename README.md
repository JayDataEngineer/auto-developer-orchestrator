# Pux

**Deepagents (Python/LangGraph) driving a Docker sandbox.** Pux is an agent
orchestrator: a [deepagents](https://docs.langchain.com/oss/python/deepagents)
agent layer served over the [LangChain Agent
Protocol](https://langchain-ai.github.io/agent-protocol/), backed by a Docker
sandbox that exposes bash / file / browser / desktop / vision tools.

Three pieces, one each:

- **`harness/`** (Python, uv) — the agent layer. Builds per-org deepagents
  graphs (a CTO + specialist subagents), serves them over the Agent Protocol
  REST API, and ships a thin `pux` client. Native fs/shell tools (`ls` /
  `read_file` / `write_file` / `edit_file` / `glob` / `grep` / `execute`) run
  through a `PuxSandboxBackend`; specialist tools (`browser_*`, `desktop_*`,
  `describe_image`, `python`, skills) come from the Go MCP bridge.
- **`backend/mcpserver`** (Go binary) — boots the Docker sandbox, exposes the
  tools over MCP at `http://127.0.0.1:9987`. Today this is the sandbox bridge
  the harness calls into (Phase 8 of the pivot re-hosts it in Python and
  deletes this binary).
- **`bin/pux`** (bash launcher) — routes `pux serve` / `pux direct` /
  `pux <client-cmd>` into the harness.

Single-tenant, localhost-only, no auth. One pux process = one project = one
sandbox.

## Quick start

```bash
# 1. Clone + sync the Python harness
git clone <this-repo> pux && cd pux
cd harness && uv sync && cd ..

# 2. Build the sandbox image (one-time)
cd sandbox && docker build -t pux-sandbox:latest . && cd ..

# 3. Start the Go MCP sandbox server (verifies Docker + image)
task start                         # or: task run  (foreground dev)

# 4. Start the Agent Protocol server
pux serve                          # FastAPI on http://127.0.0.1:9988

# 5. Drive it (client — requires the server running)
pux agents                         # list the 10 orgs
pux dispatch --org general "describe this project"   # one-shot run
pux resume                         # list recent threads

# No server? In-process runner for dev:
pux direct --org general           # runs the graph directly, no HTTP
```

The MCP sandbox server listens at `http://127.0.0.1:9987`; the Agent Protocol
server at `http://127.0.0.1:9988`. The `pux` client defaults to the latter
(override with `PUX_API_URL`).

## Subcommands

| Subcommand | What it does |
|------------|-------------|
| `pux serve` | Start the Agent Protocol server (uvicorn on :9988). |
| `pux direct --org <name> [--task "..."]` | In-process runner — no server. The verify/dev path. |
| `pux agents` | List orgs as Agent Protocol agents (+ their specialists). |
| `pux dispatch --org <name> "task"` | Ephemeral blocking run; prints the answer + a resumable `thread_id`. |
| `pux resume [--org <name>]` | List recent threads. |
| `pux show <thread_id>` | Print a thread's last message + status. |
| `pux history <thread_id>` | Print a thread's revision history (langgraph checkpoints). |
| `pux run <thread_id> "task"` | Background run on an existing thread → `run_id`. |
| `pux wait <run_id>` | Block for a background run's output. |

## Tool surface

fs/shell is **deepagents-native** (via `PuxSandboxBackend.execute()` → docker
exec inside the container); all 13 specialists are **`pux_sandbox_*`** native
Python tools too (Phase 8b–8f). The Go bridge now owns only container lifecycle:

| Tool | Backed by |
|------|----------|
| `ls` / `read_file` / `write_file` / `edit_file` / `glob` / `grep` / `execute` | native — `PuxSandboxBackend.execute()` → docker exec (8a) |
| `python` | native — docker exec `python3 -c` (8b) |
| `list_skills` / `load_skill` | native — host FS `.pi/skills/` (8c) |
| `describe_image` | native — `/usr/local/bin/describe_image.py` via docker exec (8d) |
| `browser_navigate` / `_click` / `_type` / `_screenshot` / `_evaluate` | native — `curl` to in-sandbox `sb_server.py` via docker exec (8e) |
| `desktop_screenshot` / `_click` / `_type` / `_key` | native — `xdotool` + `desktop_observe.py` via docker exec (8f) |

All paths the tools report are **inside the sandbox container**; the project is
bind-mounted at `/sandbox/workspace/`. `create_deep_agent` injects
`FilesystemMiddleware(backend)` into every subagent, so native fs tools are
always available regardless of a subagent's `tools:` whitelist.

## Org system

Orgs are markdown-driven and **declaratively contracted**. Drop a directory
under `orgs/<name>/`:

```
orgs/<name>/
├── AGENTS.md       # CTO system prompt body + `agents: [slug…]` frontmatter
└── policy.yaml     # optional: egress ACLs, creds, sandbox image/tier, cookies
```

`pux --org <name>` (in-process) / `dispatch --org <name>` (server) appends the
body to the base system prompt — the main agent becomes that org's CTO and
delegates to its declared specialists via the `task` tool.

Specialist subagents live under `.pi/agents/*.md` with rich frontmatter
(`tools`, `model`, `thinking`, `output`, …). The org contract enforces that
every `agents:` slug resolves, every `tools:` entry is a real tool (native or
live bridge), and any `policy.yaml` is schema-valid:

```bash
cd harness && uv run python -m pux_harness.main --check-contract   # exit 0 = green
```

## Threads & history

The Agent Protocol server persists threads + checkpoint history in SQLite
(`<project>/.pux/agent-protocol.sqlite`). Every run (ephemeral or background)
writes a resumable thread; revisions are langgraph checkpoints:

```bash
pux dispatch --org general "..."   # → thread_id
pux show <thread_id>               # last message + status
pux history <thread_id>            # revisions
pux run <thread_id> "follow up"    # continue on the same thread
```

## Tests

```bash
cd harness && uv run pytest -q          # 102 tests: org contract + server routing + policy + context offload
```

The server tests use FastAPI's `TestClient` with a stub graph (no tokens, no
Docker) to lock the REST envelope + thread/run CRUD; the real LLM-driven run
is proven end-to-end in the Phase 4 verify log (`dispatch --org general` →
9 Go files).

## Architecture

```
┌──────────────────────────────────────────┐
│ pux (bash launcher → harness/cli.py)     │  client
└──────────────┬───────────────────────────┘
               │ Agent Protocol REST (httpx)
┌──────────────▼───────────────────────────┐
│ pux serve  (FastAPI, :9988)              │  Agent Protocol server
│  deepagents org graphs + AsyncSqliteSaver│  (per-org graph cache, threads)
└──────────────┬───────────────────────────┘
               │ MCP JSON-RPC (harness/bridge.py)
┌──────────────▼───────────────────────────┐
│ pux-mcpserver (Go, :9987)                │  sandbox bridge (deleted in Phase 8)
│  tool registry + lifecycle supervisor    │
└──────────────┬───────────────────────────┘
               │ docker exec
┌──────────────▼───────────────────────────┐
│ pux-sandbox container                    │
│  Chrome + Xvfb + xdotool + tesseract +   │
│  supervisord + /workspace bind-mount     │
└──────────────────────────────────────────┘
```

## Branch layout

- **`pi-pivot`** — current. Deepagents pivot in progress: Phases 0–7 shipped
  (harness + bridge, native sandbox, declarative contract, TS harness deleted,
  Agent Protocol server + client, all 10 orgs ported to RUN on deepagents, the
  policy resolution engine ported Go→Python, and proactive context-offload —
  `ContextOffloadMiddleware` stashes >8K-char tool results behind a `ctx:<id>`
  handle + `ctx_recall`/`ctx_search` retrieval tools). Phases 8–9 roadmap
  (delete Go MCP, TUI).
- **`master`** — pre-pivot MVP. Slim Go MCP server with in-process agent loop.
- **`v0.2.0-pre-pi-mono`** — tag of master HEAD before the pivot. Safety net.

## License

See [LICENSE](LICENSE).
