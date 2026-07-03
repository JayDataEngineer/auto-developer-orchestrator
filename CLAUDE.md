# Pux — Agent & Developer Guide

## What this is

Pux is **Deepagents (Python/LangGraph) driving a Docker sandbox.** The agent
layer (orchestration, subagent delegation, sessions/threads, skills, the Agent
Protocol server + client) is [deepagents](https://docs.langchain.com/oss/python/deepagents),
living in `harness/`. The harness ALSO owns the Docker sandbox lifecycle +
the specialist tool implementations (browser/desktop/vision/python/skills) —
all native Python as of Phase 8a–8g. The Go binary (`backend/mcpserver`) is
now **fully vestigial** — it carries no model-visible tools and owns no
container (the harness boots its own via `pux sandbox start` or lazily on
first tool use). Phase 8i deletes it; until then it stays compiled but unused.

Three layers:

- **`harness/`** (Python, uv) — the agent layer. `pux_harness/graph.py` builds
  per-org deepagents graphs (CTO + specialist subagents) with a
  `PuxSandboxBackend` (native fs/shell tools) + native specialist
  `pux_sandbox_*` tools. `pux_harness/container.py` owns the Docker sandbox
  create/start/stop/remove + declarative policy enforcement. Served over the
  LangChain Agent Protocol REST API (`server.py`, FastAPI on `:9988`). Driven
  by `cli.py` (the `pux` client) or the in-process runner (`main.py`).
- **`backend/mcpserver`** (Go binary) — **vestigial (Phase 8i deletes it).**
  Historically booted the Docker sandbox + exposed bash/file/python/browser/
  desktop/vision tools over MCP at `http://127.0.0.1:9987`. As of 8a–8g every
  one of those responsibilities lives in the harness; the binary is kept only
  as a reference impl + until 8i removes it.
- **`bin/pux`** (bash launcher) — sources `.env`, routes `serve` / `direct` /
  `sandbox` / client subcommands into the harness.

The pi-mono TS harness (`bin/pux.mjs`, `.pi/extensions/*`, `pi-*` npm deps,
`package.json`) is **deleted** (Phase 4). Its org-overlay + delegation jobs are
now `harness/pux_harness/orgs.py` + deepagents' native `task(subagent_type=…)`
delegation. The pre-pi-mono HEAD is tagged `v0.2.0-pre-pi-mono`; the
pre-deepagents HEAD is the commit before Phase 4. Branch: `pi-pivot`.

## Quick start

```bash
# One-time: build sandbox image
cd sandbox && docker build -t pux-sandbox:latest . && cd ..

# Sync the Python harness
cd harness && uv sync && cd ..

# Boot the sandbox container (harness-owned; replaces the old `task start`)
pux sandbox start                # or: `pux sandbox status` to reuse a running one
                                 # (omitted → the harness boots lazily on first use)

# Start the Agent Protocol server (blocks; the canonical executor)
pux serve                        # FastAPI on http://127.0.0.1:9988

# Drive it (client — requires `pux serve` running)
pux agents                                    # list the 10 orgs
pux dispatch --org general "describe this"   # one-shot run -> answer + thread_id
pux resume                                    # list recent threads
pux show <thread_id>                          # last message + status
pux run <thread_id> "follow up"               # background run on a thread
pux wait <run_id>                             # block for a background run

# Sandbox lifecycle (harness-owned, Phase 8g):
pux sandbox start                             # boot (with $PUX_ORG policy if set)
pux sandbox status                            # is it up? (reuses if running)
pux sandbox stop                              # save-persisted + stop + remove

# No server? In-process runner for dev/verify:
pux direct --org general                       # runs the graph directly, no HTTP
pux direct --org general --check-contract      # validate all 10 orgs (no tokens)
```

For direct Go-side work (the sandbox bridge):

```bash
task build                      # Build the binary at backend/mcpserver
task run                        # Foreground dev mode (signals propagate via exec)
task start                      # Build + start in BACKGROUND (daemonize, returns immediately)
task stop                       # SIGTERM the backgrounded server
task status                     # Show running state (PID, addr, sandbox, uptime)
task smoke                      # Build + start + smoke test + stop (exercises supervisor)
task test                       # Go unit tests
task hooks                      # Install pre-commit (gitleaks)
```

The Go server takes `--addr` (default `127.0.0.1:9987`) and `--project`
(defaults to `$PWD`). Env vars `PUX_MCP_ADDR` / `PUX_PROJECT_PATH` mirror
them. The Agent Protocol server reads `PUX_API_HOST` / `PUX_API_PORT`
(default `127.0.0.1:9988`), `PUX_API_DB` (default
`<project>/.pux/agent-protocol.sqlite`), `PUX_MODEL` (provider/model, e.g.
`mimo-v2.5`, `glm-5.2`).

**Why 9987 (not 9876):** the sandbox's `sb_server.py` (browser-mode HTTP API)
listens on 9876 *inside* the container. Any org sandbox that boots with
`--network=host` leaks that listener to the host's 9876. Defaulting
pux-mcpserver to 9987 avoids the collision out-of-the-box. **Why 9988 (the
Agent Protocol server):** adjacent to the MCP bridge, and the conventional
Agent Protocol port 8000 is taken on this host.

## Architecture

```
┌─────────────────────────────────────────────────┐
│ pux (bash launcher → harness/cli.py)            │  Agent Protocol client
└──────────────┬──────────────────────────────────┘
               │ Agent Protocol REST (httpx)
┌──────────────▼──────────────────────────────────┐
│ pux serve  (FastAPI, :9988)                     │  Agent Protocol server
│  harness/pux_harness/server.py                  │  (per-org graph cache,
│  deepagents org graphs + AsyncSqliteSaver       │   SQLite threads/history)
└──────────────┬──────────────────────────────────┘
               │ deepagents graph + PuxSandboxBackend
┌──────────────▼──────────────────────────────────┐
│ harness  (Python, deepagents)                   │  the whole agent + sandbox
│  - graph.py / orgs.py / native_tools.py         │   layer: 13 specialist
│  - container.py  SandboxContainer.ensure()      │   tools NATIVE (no MCP hop
│  - docker_exec.py  DockerExecClient.exec()      │   for fs/shell OR
│  - context_offload.py + policy.py               │   specialists)
└──────────────┬──────────────────────────────────┘
               │ Docker SDK (create / exec / stop)
┌──────────────▼──────────────────────────────────┐
│ pux-sandbox container                           │
│   Chrome + Xvfb + xdotool + tesseract +         │
│   supervisord + /workspace bind-mount +         │
│   backbone scripts (chmod 0444)                 │
└─────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────┐
│ backend/mcpserver  (Go, :9987)  — VESTIGIAL     │  carries NO model-visible
│  No tools (all 13 specialists ported native).   │  tools since 8a–8f; the
│  Deleted in Phase 8i (after 8g proven).         │  container lifecycle moved
└─────────────────────────────────────────────────┘  to container.py (8g).
```

**The Go binary is vestigial.** Phase 8a–8f ported all 13 specialist tools
into `native_tools.py` (the graph no longer calls `shared_client()`); Phase 8g
moved the container lifecycle into `container.py`. Nothing model-visible flows
through Go. It stays in the tree **only** until 8i deletes it — kept last so
the repo is never half-deleted mid-pivot. To run Pux you do **not** start it.

## Harness layout (Python, deepagents)

```
harness/
├── pyproject.toml              # deepagents + langgraph + fastapi + uvicorn + httpx
├── pux_harness/
│   ├── graph.py                # build_graph(org) -> compiled deepagents graph
│   │                           # one shared MCP client + PuxSandboxBackend per process
│   ├── server.py               # Agent Protocol server (FastAPI, :9988)
│   ├── cli.py                  # `pux` client (httpx → server)
│   ├── main.py                 # in-process runner (`pux direct`)
│   ├── bridge.py               # PuxMCPClient: direct JSON-RPC to Go MCP :9987
│   ├── sandbox.py              # PuxSandboxBackend(BaseSandbox) -> native fs tools
│   ├── model.py                # provider/model factory (PUX_MODEL)
│   ├── orgs.py                 # system-prompt builder + subagent loader + contract glue
│   └── contract.py             # declarative org-contract enforcer (7 rules)
└── tests/
    ├── test_org_contract.py    # 24 tests — the all-orgs-green gate
    └── test_server.py          # 22 tests — Agent Protocol routing (stub graph)
```

**The deepagents seam (source-verified):** `create_deep_agent(model,
system_prompt, tools, subagents, backend, checkpointer)` (deepagents
`graph.py:270`). The `backend` flows into the main `FilesystemMiddleware`
**and** every subagent's — one backend serves the whole tree, so native
fs/shell tools (`ls/read_file/write_file/edit_file/glob/grep/execute`) are
available to every subagent regardless of its `tools:` whitelist.
`PuxSandboxBackend` subclasses `BaseSandbox` (Shape A — 4 abstract methods:
`execute`/`id`/`upload_files`/`download_files`); it inherits `ls/read/write/
edit/grep/glob` free, all routed through our `execute()` → MCP `bash`.

**Adding an org:** drop `orgs/<name>/AGENTS.md` (CTO body) with an
`agents: [slug…]` frontmatter listing its specialists. Optionally add
`orgs/<name>/policy.yaml`. Run `--check-contract` to validate (the contract
also runs as a pytest gate). No harness-level per-org code — org-bundled
`*.py`/`Dockerfile`/`docker-compose.yml`/`bootstrap.sh` is the org's sandbox
payload, reached via the sandbox, never imported by the harness.

**Adding a subagent:** write `.pi/agents/<slug>.md` with frontmatter
(`name`, `description`, `tools`, `model`, `thinking`, `output`, …) + body.
Reference it from an org's `agents:` frontmatter. Delegation is deepagents'
native `task(subagent_type="<slug>", task="…")`.

**Adding a skill:** write `.pi/skills/<name>/SKILL.md` with `name` +
`description` frontmatter. Reached by the agent via the `list_skills` /
`load_skill` MCP tools.

## Go backend layout

```
backend/
├── cmd/mcpserver/main.go      # Entry point + signal-gated sandbox boot
├── cmd/mcpserver/supervisor.go # run/start/stop/status subcommands
└── internal/
    ├── adapters/              # BashExecutor, FileOps (sandbox bridge)
    ├── audit/                 # JSONL audit log (opt-in via PUX_AUDIT_LOG)
    ├── core/                  # core.Tool, ToolError
    ├── mcpserver/             # MCP server + tool registry
    │   ├── server.go          # JSON-RPC dispatch + tool registry
    │   ├── transport.go       # HTTP handler
    │   ├── session.go         # Session ID generator
    │   ├── sandbox_python.go  # python tool (sandbox-aware)
    │   ├── skills_tool.go     # list_skills / load_skill
    │   ├── vision_tool.go     # describe_image (local ONNX vision)
    │   ├── browser_tool.go    # browser_* tools wrapping in-sandbox sb_server.py
    │   ├── desktop_tool.go    # desktop_* tools wrapping xdotool + desktop_observe.py
    │   └── shell.go           # shared shQ shell-escape helper
    ├── retry/                 # Provider retry (kept; may be unused post-pivot)
    ├── sandbox/               # Docker sandbox lifecycle
    ├── sensitive/             # Secret scrubbing
    ├── skills/                # list_skills / load_skill package
    └── tools/
        ├── bash/              # Bash tool (Validator-based deny list)
        ├── file/              # File tools (read/write/edit/grep/glob)
        └── truncate/          # Output truncation
```

The pre-pivot agent/history/tui/org packages are gone. They live on the
`v0.2.0-pre-pi-mono` tag if needed:
`git show v0.2.0-pre-pi-mono:backend/internal/agent/loop.go`.

## Lifecycle (`run` / `start` / `stop` / `status`)

The Go binary ships with four subcommands. `mcpserver` with no subcommand (or
with `--flags` only) defaults to `run`.

| Subcommand | When to use |
|-----------|--------------|
| `run` | **Dev/foreground.** Boots in the terminal, sandbox + HTTP listener live until SIGINT/SIGTERM. The `run` task uses `exec` so signals from a parent shell propagate directly. |
| `start` | **Background/CI.** Daemonizes via `setsid` + `cmd.Process.Release()`, writes a PID file at `<project>/.pux/mcpserver.pid`, returns immediately. Refuses if a server is already running unless `--force` stops it first. Logs default to discarded — pass `--log <path>` or set `PUX_MCP_LOG` to capture. |
| `stop` | Reads the PID file, SIGTERMs the server, polls up to `--wait` (default 10s), SIGKILLs if still alive. Removes the PID file. |
| `status` | Reports PID + addr + project + sandbox + container_id + uptime. `--live` adds an HTTP ping to verify the server actually responds. |

**Stale-PID recovery:** if `start` finds a PID file but the process is dead
(crash, SIGKILL, OOM), it cleans up and proceeds.

**Single-tenant per project:** the PID file lives at `<project>/.pux/mcpserver.pid`.
Override via `PUX_PID_FILE` for unusual layouts.

Set `PUX_AUDIT_LOG=/path/to/audit.jsonl` to append every tool call (args +
result + duration, secret-scrubbed) to a forensic log. Opt-in; default off.

## Connecting to the (vestigial) Go bridge

The harness no longer depends on the Go server for any model-visible work.
`bridge.py` (the direct JSON-RPC client) still exists but is reached **only**
by `--check-contract` rule 4 (live-bridge tool probe) and `--check`; the
agent graph itself stopped calling it in Phase 8a–8f. The container is now
created + exec'd by `container.py` / `docker_exec.py` directly over the Docker
SDK — no JSON-RPC hop. Until 8i deletes it, the Go binary still *can* be
attached from another MCP client (Claude Desktop, etc.):

```json
{
  "mcpServers": {
    "pux-sandbox": { "url": "http://127.0.0.1:9987" }
  }
}
```

The server speaks MCP protocol version `2025-03-26`. Sessions are tracked
via the `Mcp-Session-Id` header (generated on `initialize`).

## Tool surface

fs/shell is **deepagents-native** (via `PuxSandboxBackend.execute()` → **direct
`docker exec`** into the container — Phase 8a cut the MCP→Go middleman); all
13 specialists are **native Python StructuredTools** too (Phase 8b–8f). Phase
8g moved the container **lifecycle** (create/start/stop) into `container.py`,
so the Go bridge now owns nothing the harness uses — it carries NO
model-visible tools AND no longer owns the container:

| Tool | Source | Backed by |
|------|--------|----------|
| `ls` / `read_file` / `write_file` / `edit_file` / `glob` / `grep` / `execute` | native (`BaseSandbox`) | `PuxSandboxBackend.execute()` → `DockerExecClient` (docker exec) |
| `python` | **native** (`pux_sandbox_python`, 8b) | `python3 -c` via docker exec |
| `list_skills` / `load_skill` | **native** (8c) | host FS walk/read at `<project>/.pi/skills/` |
| `describe_image` | **native** (`pux_sandbox_describe_image`, 8d) | `/usr/local/bin/describe_image.py` via docker exec (local ONNX) |
| `browser_navigate` / `_click` / `_type` / `_screenshot` / `_evaluate` | **native** (8e) | `curl` to in-sandbox `sb_server.py` via docker exec |
| `desktop_screenshot` / `_click` / `_type` / `_key` | **native** (8f) | `xdotool` + `/usr/local/bin/desktop_observe.py` via docker exec |

All paths the tools report are **inside the sandbox container**. Your project
is bind-mounted at `/sandbox/workspace/`. `create_deep_agent` injects
`FilesystemMiddleware(backend)` into every subagent, so native fs tools are
always available regardless of a subagent's `tools:` whitelist.

`/sandbox/` also contains read-only backbone scripts (`scripts.py`, etc.)
that ship with the sandbox image — the agent can invoke them but can't edit
them (`chmod 0444`).

## Context offload (Phase 7)

Proactive tool-output offload keeps large results out of the working context
*before* they accumulate. `harness/pux_harness/context_offload.py`:

- **`ContextOffloadMiddleware(AgentMiddleware)`** — `wrap_tool_call`/
  `awrap_tool_call` measure every result; a text `ToolMessage` over `threshold`
  (default 8000 chars ≈ 2K tokens) is stashed to `ctx_store.py` (host-side
  `<project>/.pux/ctx/`) and replaced with a short stub + a `ctx:<id>` handle.
  This is the *proactive* complement to deepagents' reactive
  `SummarizationMiddleware` (which only evicts after overflow).
- **`ctx_recall` / `ctx_search`** — agent tools to pull a stashed result back
  by handle or grep across all of them. **Exempt** from offload: their job is
  to inject content, so re-stashing would trap the agent (proven + fixed in the
  Phase 7 E2E).

Runs on the **main agent only** — deepagents' `SubAgentMiddleware` doesn't
forward a raw SubAgent spec's `middleware` key into the compiled specialist
(verified), so attaching it there is a silent no-op; subagent offload is a
`CompiledSubAgent`-pre-compilation follow-up, not a shim. The store is a
harness-owned cache of results that already came back to it — no new
host-write capability, no Docker, `.pux/` is gitignored.

## Agent Protocol server (`pux serve`)

`harness/pux_harness/server.py` serves the deepagents org graphs over a subset
of the published Agent Protocol spec. **Org → agent_id.** One
`AsyncSqliteSaver` (persistent threads/history) is shared across all org
graphs; per-org compiled graphs are cached lazily. Runs are ephemeral
executions tracked in-memory; the durable thread state (messages + checkpoints)
lives in SQLite. A small `pux_threads` index table maps thread_id → org so a
thread remembers which org's graph owns it across restarts.

| Endpoint | Behavior |
|----------|----------|
| `GET /ok` | health + org list |
| `POST /agents/search` | list orgs as agents (+ specialists) |
| `GET /agents/{agent_id}` | org descriptor |
| `POST /threads` | create a thread for an agent |
| `POST /threads/search` | list/search threads (optional `agent_id` filter) |
| `GET /threads/{thread_id}` | thread state (last message + status) |
| `DELETE /threads/{thread_id}` | delete (checkpointer + index) |
| `GET /threads/{thread_id}/history` | revision history (langgraph checkpoints) |
| `POST /threads/{thread_id}/runs` | background run → `run_id` |
| `GET /threads/{thread_id}/runs` | list a thread's runs |
| `POST /runs/wait` | ephemeral blocking run (create+run+return; thread is kept) |
| `GET /runs/{run_id}/wait` | block for a background run's final output |
| `POST /runs/{run_id}/cancel` | cancel a background run |

**Implementation choice:** thin FastAPI implementing the published spec, NOT
`langgraph-api` (the Platform runtime). Rationale: minimalist (we own ~330 LOC
vs adopting an opinionated runtime), and the REST contract is identical either
way — swapping the server impl behind these endpoints is invisible to clients,
so the choice is reversible. SSE streaming (run lifecycle + tool + nested-
subagent events) is deferred to Phase 9.

### Org mode (`--org <name>`)

In-process: `pux direct --org <name>` builds the graph with
`orgs/<name>/AGENTS.md` appended to the base system prompt. Over the server:
`dispatch --org <name>` (ephemeral) or `POST /threads {agent_id}` then
`POST /threads/{id}/runs`. The main agent becomes that org's CTO and
delegates to its declared specialists via the `task` tool.

### Threads & history

The server persists threads + checkpoint history in SQLite
(`<project>/.pux/agent-protocol.sqlite`). Every run writes a resumable
thread; revisions are langgraph checkpoints. `pux resume/show/history/run/wait`
are the client surface over `/threads/*` + `/runs/*`.

## Adding a new tool

**The canonical path is now Python** (`native_tools.py`) — Phase 8a–8f ported
all 13 specialists there and the graph stopped calling the Go bridge. Add a
new specialist as a `StructuredTool` built in `native_tools.py` (it gets a
`shared_exec()` client for docker-exec); append its name to `SPECIALIST_TOOLS`
+ `PORTED_SPECIALISTS` so the contract + parity check pick it up. Reference it
from an agent's `tools:` frontmatter. No rebuild, no Go.

**The Go path below is the legacy that 8i will delete** — kept only so the
vestigial binary's `tools/list` still mirrors the live surface for `--check`:

**Specialist tool on the Go bridge** (browser_*, desktop_*, vision, future
mobile_*, device_*): append a spec entry to the family's slice in its file
(`browserSpecs` in `browser_tool.go`, `desktopSpecs` in `desktop_tool.go`);
the family's `RegisterXXXTools(srv, exec, cfg)` helper picks it up. Or a
standalone Go type implementing `core.Tool` registered in
`cmd/mcpserver/main.go`. Rebuild (`task build`) → shows in `tools/list`. Then
reference it in agent `tools:` frontmatter and overlays. **Mirror it in
`native_tools.py` too** — that is the surface the agent actually sees.

**Standalone tool** (one-off like `describe_image`, `python`, `list_skills`):

1. Write a Go type implementing `core.Tool` (`Name()`, `Description()`, `Schema()`, `Execute()`).
   See `mcpserver/sandbox_python.go` for a 100-LOC example.
2. In `cmd/mcpserver/main.go`, register it: `srv.RegisterTool(myTool)`.
3. If it needs sandbox access, take an `adapters.BashExecutor` or `adapters.FileOps`
   in the constructor — same pattern as `bash.New(exec)`.
4. Rebuild (`task build`) — the tool shows up in `tools/list` automatically.

**Family of related tools** (browser_*, desktop_*, future mobile_*, device_*):

1. Append a spec entry to the family's slice in its file
   (`browserSpecs` in `browser_tool.go`, `desktopSpecs` in `desktop_tool.go`).
2. The family's `RegisterXXXTools(srv, exec, cfg)` helper picks up the new
   spec automatically. No main.go change.
3. Add a test in the family's `_test.go` file using the spec-lookup helper.

## Vision (local ONNX, opt-in)

`describe_image` runs local vision inference inside the sandbox via
Qwen3.5-2B-ONNX-OPT fp16. No external MCP dependency — model weights are
operator-supplied, downloaded once via host-side script.

**Bootstrap:**
```bash
scripts/bootstrap-vision.sh                 # downloads to $PWD/.pux/models/
scripts/bootstrap-vision.sh --project DIR   # explicit project root
scripts/bootstrap-vision.sh --check         # exit 0 if ready, 1 if not
```

**Contract:** when the model is absent, `describe_image` returns
`{success:false, reason:"unavailable"}` — NOT a Go error, NOT an
`isError:true` envelope. The driving agent falls back to text-only
reasoning without breaking its loop.

## Browser (in-sandbox sb_server.py)

Five MCP tools wrap the sandbox's `sb_server.py` (persistent SeleniumBase
HTTP API on `127.0.0.1:9876` inside the container).

| Tool | Endpoint | Field contract |
|------|----------|---------------|
| `browser_navigate` | `/navigate` | `{url}` |
| `browser_click` | `/click` | `{index}` or `{selector}` (mutually exclusive) |
| `browser_type` | `/type` | `{text}` + (`{index}` or `{selector}`) |
| `browser_screenshot` | `/read` | `{}` (no args) — returns page + SoM labels + screenshot path |
| `browser_evaluate` | `/evaluate` | `{code}` — JavaScript expression, use `return` for explicit values |

**Set-of-Marks labels:** `/navigate` and `/read` return an `element_map`
with integer `index`es. Pass that integer to `browser_click(index=N)` or
`browser_type(index=N, text=...)`.

## Desktop (Xvfb DISPLAY=:99, xdotool + OCR)

Four MCP tools wrap the sandbox's X11 desktop.

| Tool | Field contract |
|------|---------------|
| `desktop_screenshot` | `{}` — returns `{image_b64, elements[], windows[], resolution, ocr_available}` |
| `desktop_click` | `{x, y, button?}` — button default 1 (left); 2=middle, 3=right |
| `desktop_type` | `{text, clear?}` — clear default true (Ctrl+A + Delete first) |
| `desktop_key` | `{keys}` — xdotool key combo like `Return`, `ctrl+c`, `alt+Tab` |

**Pixel coordinates are the contract.** OCR text positions drift across
runs, so click by `(x, y)` from the latest `desktop_screenshot`.

## Sandbox policy (declarative, opt-in per org)

`orgs/<name>/policy.yaml` is the per-org enforcement contract. Presence
opts that org into five independent layers; absence = today's behavior
(full egress, root-owning writes, no required creds, default image +
tier). All five sections optional — declare only what you need.

```yaml
# orgs/<name>/policy.yaml
workspace:
  mounts:
    - host: ${GAME_ROOT}            # ${VAR} resolved from operator env
      container: /workspace/game
      mode: rw                      # rw (default) | ro
  run_as_host_user: true            # match container UID:GID to operator

egress:
  allow:                            # deny-by-default when non-empty
    - host: github.com              # DNS resolved at boot, all IPs allowed
      port: 443
    - host: 100.86.69.57            # literal IP also works
      ports: [18800, 18080, 18265]

credentials:
  required: [ALPACA_API_KEY]        # refuse create if absent in env
  optional: [FRED_API_KEY]          # inject if present, silent skip

sandbox:
  image: my-org-sandbox:latest      # override pux-sandbox:latest (specialist
                                    # deps: manim, kokoro, etc). Tag must
                                    # exist locally. See video-production for
                                    # a shipped example.
  tier: isolated                    # override caller-supplied tier.
                                    # isolated = bridge network + egress ACLs
                                    # bridged  = host net (skips ACLs)

browser:
  cookies_env: TWITTER_COOKIES_B64  # base64-encoded cookie JSON, injected
                                    # as env var. seed-cookies.sh (priority 80)
                                    # decodes + POSTs to sb_server.py /cookies.
                                    # Cookies NEVER touch disk in container.
```

**Pipeline** — Phase 6 ported the *resolver* (`harness/pux_harness/policy.py`,
a 1:1 port of the Go package: `load/validate_env/env_vars/resolve_mounts/
egress_rules/resolve_tier` — 35 parity tests mirror the 22 Go tests); Phase 8g
ported the *enforcer* too, into `container.py::create()`. The harness now
reads `PUX_ORG` and applies policy at container create; the Go enforcer
(`backend/internal/policy/` + `sandbox/policy_hook.go`) is dead code until 8i
deletes it. The no-model dry-run `pux direct --org <X> --check-policy` resolves
mounts/creds/egress/tier without touching Docker.

1. `--org X` → harness sets `PUX_ORG=X` in env
2. `container.SandboxContainer._resolve_policy()` reads `PUX_ORG`, calls
   `policy.load(X, projectRoot)`
3. `policy.validate_env` checks required creds present → fail loud BEFORE Docker
4. `policy.resolve_mounts` expands `${VAR}` placeholders → fail loud if unset
5. Required + optional creds + `cookies_env` value + `SEED_COOKIES_ENV` pointer
   injected as `environment=[KEY=VALUE]` on the create call
6. `run_as_host_user` → `create(user="UID:GID")`
7. `egress.allow` non-empty → stages `<project>/.pux/egress.conf` (0600) +
   `cap_add=["NET_ADMIN"]`
8. `sandbox.image` overrides the image used at container create
9. `sandbox.tier` overrides the caller-supplied tier (re-evaluates gVisor)
10. Supervisor runs `apply-egress-policy.sh` at boot priority 15:
    `iptables -P OUTPUT DROP` + allowlist + always allow loopback/DNS/established
11. Supervisor runs `seed-cookies.sh` at boot priority 80 (after sb_server.py
    at priority 70): decodes base64 env, POSTs to `http://127.0.0.1:9876/cookies`,
    validates `count` matches.

**Skipped for TierBridged** — host networking makes iptables-in-container
meaningless; operator explicitly chose host net for that sandbox. The
egress firewall staging step is skipped when the *resolved* tier (after
policy override) is TierBridged.

**Browser cookie contract** — CDP's bulk `Network.setCookies` silently
rejects the ENTIRE batch when any cookie carries an `expires` field
(bisected + proven against the real twitter payload 2026-07-02).
`sb_server.py::_dicts_to_cookie_params` strips `expires` — cookies
become session-scoped, which is correct for the seed-at-boot use case.

**Verify gates** (baked into `task test`):

- `go test -race ./internal/policy/...` — 28 tests (placeholder expansion,
  missing-env errors, optional vs required, hostname resolution, port
  range, IPv4 + IPv6 literals, DNS failure, sandbox.image+tier override,
  ResolveTier semantics, cookies_env injection pointer, shipped policies
  parse cleanly)
- `task smoke` — boots real Docker, confirms no-policy path unchanged
- E2E (proven 2026-07-02): twitter-agent with browser cookies — seed-cookies.sh
  runs at boot, 14 cookies persist, navigate to x.com → logged-in home feed;
  egress firewall drops example.com (3.9s timeout), allows api.x.com (404 from
  server, not firewall). game-studio bridge networking — 3 allow rules applied
  correctly, host-and-container return identical results per service state.

## MCP transport contract (Go bridge)

| Method | Behavior |
|--------|----------|
| `initialize` | Returns protocolVersion + serverInfo. Generates Mcp-Session-Id. |
| `notifications/initialized` | Fire-and-forget notification, no response. |
| `tools/list` | Returns array of `{name, description, inputSchema}`. |
| `tools/call` | Executes the named tool. Errors return `isError: true` in the result envelope (not as JSON-RPC error). |
| `ping` | Returns empty result. |
| (batch) | Rejected with `-32600 Invalid request`. |
| (unknown method) | `-32601 Method not found`. |
| (parse error) | `-32700 Parse error`. |

CORS is wide-open (`*`) since this is localhost-only. Preflight `OPTIONS`
returns 204. `GET` and `DELETE` are reserved (405 / 204 respectively).

## Verification

**Go contract** (60+ tests in `mcpserver/`):

- **Protocol envelope** (6 tests): initialize, session ID generation,
  notifications/initialized, ping, unknown method, parse errors
- **Tool dispatch** (6 tests): empty/non-empty tools/list, unknown tool,
  tool-error vs Go-error split, string vs map return shapes, duplicate
  registration panics
- **Transport** (4 tests): end-to-end HTTP (initialize → list → call),
  batch rejection, CORS preflight, session mismatch
- **Browser tools** (12 tests): navigate/click/type/screenshot/evaluate
  arg-marshal + endpoint routing, timeout, malformed response, exec
  failure, label-vs-selector dispatch
- **Desktop tools** (17 tests): screenshot parses desktop_observe.py
  JSON + handles malformed/timeout/exec-failure, click coord parsing
  + button validation, type shell-escaping + clear flag, key combo building

Plus a real end-to-end smoke test (`task smoke`) that boots against a live
Docker container and exercises every tool.

**Harness contract** (`harness/tests/`, run with `uv run pytest -q`):

- **Org contract** (24 tests): all 10 orgs green; tool-resolution against
  the live bridge surface; each violation class fires.
- **Agent Protocol server** (22 tests): pure helpers + HTTP routing with
  a stub graph (no tokens, no Docker) — locks the REST envelope + thread/run
  CRUD. The real LLM-driven run is proven end-to-end in the Phase 4 verify
  log (`dispatch --org general` → 9 Go files via the researcher subagent).

**Verify gates before committing:**

- Harness: `cd harness && uv run pytest -q` + `uv run python -m
  pux_harness.main --check-contract` (exit 0)
- Go side: `task test` + `task smoke` (real Docker) — only when touching the
  bridge
- Boot check: `pux serve` + `pux agents` (server boots, lists 10 orgs) +
  `pux dispatch --org general "<forcing task>"` (real run returns ground truth)

## Pivot roadmap (pi-pivot branch)

Phases 0–5 shipped (2026-07-03): harness + bridge, native sandbox,
declarative contract, TS harness deleted, Agent Protocol server + client,
and all 10 orgs ported to RUN on deepagents (Phase 5).

| Phase | What | Status |
|-------|------|--------|
| 5 | Port remaining 7 orgs to RUN on deepagents (delegation-forcing tasks) | **SHIPPED 2026-07-03** — all 10 orgs run E2E. Each `pux direct --org <name>` forcing task in `main.py:DEFAULT_TASKS` makes the CTO delegate via `task(subagent_type=<specialist>)` and drive a native fs/shell tool (`execute`/`read_file`/`glob`) against the org's own bundled content; every run returned the correct ground-truth answer (invest=17 .py via invest-researcher, game-studio=6 skills via docs-writer, dre=7 .py via dre-auditor, smp=3 angles via smp-writer, twitter=1 skill via twitter-drafter, telegram=4 msgs via telegram-drafter, video=3 entries via video-scriptwriter). New structural test `test_every_org_has_a_forcing_task`; pytest 47/47. |
| 6 | Policy engine Go→Python (egress/creds/image+tier/browser) | **SHIPPED 2026-07-03** — `harness/pux_harness/policy.py` is a faithful 1:1 port of `backend/internal/policy` (pure logic: load/validate_env/env_vars/resolve_mounts/egress_rules/resolve_tier). `tests/test_policy.py` mirrors the 22 Go tests (35 cases incl. IPv6, container-resolved `host.docker.internal` passthrough, DNS-refresh comments, ports fanout). Contract rule 5 now runs the real engine as a deep-schema check (`load` + `resolve_mounts` — offline; `egress_rules` deliberately NOT called, it resolves DNS). Consumer: `pux direct --org <name> --check-policy` — a no-model dry-run that resolves mounts, checks creds (names only — never values), resolves egress DNS, reports tier/image; exits 1 on missing required creds (the same gate container-create enforces). pytest 84/84. Enforcement wiring (binds/env/caps/egress.conf staging at `ContainerCreate`) landed in **Phase 8g** (`container.py::create()`) once the harness owned container creation — see the Phase 8 row. |
| 7 | context-mode integration (ctx MCP + wrap_tool_call offload) | **SHIPPED 2026-07-03** — **native harness offload, NOT an external ctx-MCP bridge** (context-mode is a stdio Claude-Code bun plugin, unreachable over HTTP from the harness; meta-mcp `list_servers` confirmed). `harness/pux_harness/context_offload.py` `ContextOffloadMiddleware(AgentMiddleware)` measures each `wrap_tool_call`/`awrap_tool_call` result; a ToolMessage > `threshold` (default 8000 chars ≈ 2K tokens) gets stashed to `ctx_store.py` (host-side `<project>/.pux/ctx/<id>.txt+.json`; hex-only ids reject path-escape) and replaced with a preview + `ctx:<id>` handle. `ctx_recall` / `ctx_search` StructuredTools pull stashed bytes back on demand — only the slice the agent asks for re-enters context. This is the *proactive* complement to deepagents' own reactive `SummarizationMiddleware`. **Two findings the E2E surfaced + fixed:** (1) `ctx_recall`/`ctx_search` are exempt from offload (`_RETRIEVAL_TOOLS`) — their job is to inject content, so re-stashing trapped the agent in a recall→offload loop (proven: pre-fix ctx_recall(12K)→re-offloaded; post-fix returns full text); (2) **main-agent-only** — deepagents' `SubAgentMiddleware` does not forward a raw SubAgent spec's `middleware` key into the compiled specialist (verified: a researcher's `read_file` of an 11.9K-char file returned the full text yet nothing stashed), so attaching it to specialists is a silent no-op. Forcing it would be a shim; left documented as a `CompiledSubAgent`-pre-compilation follow-up. **Proven E2E** (`pux direct --org general` glm-5.2): `execute seq 1 3000`→12087B offloaded→`ctx_recall` retrieves full text→correct answer; pytest 102/102; `--check-contract` exit 0. |
| 8 | Re-host sandbox in Python (`execute()`→docker exec; 13 specialist tools); wire policy enforcement here; delete Go MCP | **8a–8g SHIPPED 2026-07-03** — **8a** `docker_exec.py` `DockerExecClient` (container discovered by `openshell.project-path` label; `exec()` via Docker SDK `exec_run(tty=False)`; `PuxSandboxBackend.execute()`/`id` retargeted from MCP `bash` to direct docker exec). **8b/8c** `native_tools.py` ports `python` (docker exec `python3 -c`), `list_skills`/`load_skill` (host FS `.pi/skills/`) — fixes a latent Go bug (skills package read `<root>/skills/` → `count:0`; Python reads the real path → `count:1`). **8d** ports `describe_image` (exit-code dispatch: 0/1/2=unavailable/3=deps + timeout); `docker_exec.exec(timeout=N)` enforces per-call deadlines via thread `.result(timeout=)` → `ExecTimeout`. **8e** ports the 5 `browser_*` tools (`_sb_post` builds the exact `curl … sb_server` cmd Go's `postJSON` did). **8f** ports the 4 `desktop_*` tools (`_exec_desktop` runs `DISPLAY=:99 xdotool …` / `desktop_observe.py` via docker exec; pixel-coord contract). `_result` uses `sort_keys=True` → byte-equivalent to the Go bridge's `json.MarshalIndent(v,"","  ")` (Go sorts map keys — verified live list_skills emits `count` before `skills`). `PORTED_SPECIALISTS == SPECIALIST_TOOLS` (all 13 native) → `build_graph` no longer calls `shared_client()`. **8g** `container.py` `SandboxContainer` owns the container lifecycle (create/start/stop/remove) — a faithful port of `manager.go::CreateSandbox`+`DestroySandbox` (CLI-mode slice; desktop/VNC/port-allocator paths dropped — native tools curl sb_server/xdotool directly). `ensure()` is the single-tenant gate (discover-by-label → reuse, else create); the exec client now **self-boots** so `task start` is no longer required. Policy **enforcement** (the part deferred from Phase 6 — binds/env/caps/egress.conf staging at create) runs harness-side now, porting `policy_hook.go::applyOrgPolicy` step-for-step. **Proven E2E** (2026-07-03): stopped the Go binary + removed its container → `pux sandbox start` booted a fresh one → inspect byte-matches the Go-managed config (image/labels/5 binds/extra_hosts/2GB·2cpu·512pids/shared-infra/runc); `pux direct --org general` → CTO→researcher→native `execute find … -name '*.go'` → 9 correct files. Policy enforcement proven at the Docker level: `PUX_ORG=game-studio pux sandbox start` → container created with `CapAdd:[NET_ADMIN]` + `.pux/egress.conf` staged (0600) with the 4 resolved rules incl. container-resolved `host.docker.internal`. New `tests/test_container.py` (28 cases — runtime decision table, cache-name determinism + live-match, env defaults, URL rejection). `pux sandbox {start,stop,status}` CLI surface replaces `task start/stop/status`. pytest 130/130, contract exit 0. **8i remains**: delete the Go MCP tree (LAST — after 8a–8g proven, which they are). |
| 9 | TUI/clients as Agent Protocol consumers (+ SSE streaming) | roadmap |

## Branch strategy

- **`pi-pivot`** = current branch. The deepagents pivot. PRs here keep both
  surfaces clean: the Python harness is the agent layer; the Go binary is the
  (temporary) sandbox bridge.
- **`master`** = pre-pivot MVP. Slim Go MCP server with the in-process agent
  loop + history recorder + Bubble Tea TUI + dispatch surface. Frozen from
  the pi-pivot perspective.
- **`v0.2.0-pre-pi-mono`** = tag of master HEAD before the pi-mono pivot. Safety
  net. `git show v0.2.0-pre-pi-mono:backend/internal/agent/loop.go` works.
- **`dev`** + **`v0.1.0-fullstack-legacy`** = the older fullstack predecessor
  (TUI, web UI, CLI, multi-agent). Frozen.

## Testing harness rules

- "Should work" is banned. Verify with `task smoke` (real Docker), a real
  `pux dispatch` run (ground-truth answer), or a test that exercises the
  actual code path.
- Adding a Go tool → add a test in `mcpserver/server_test.go` covering its
  return shape (string vs map vs error).
- Adding an Agent Protocol endpoint → add a routing test in
  `harness/tests/test_server.py` (stub graph, no tokens).
- Adding an org / subagent → it must pass `--check-contract` + the
  `test_org_contract.py` gate.
- Changing the JSON-RPC envelope → update both `server.go` and the
  `server_test.go` table.

## What's NOT here (deferred or dropped)

Dropped (deepagents does it natively, or wasn't pulling weight):

- ~~pi-mono TS harness (`bin/pux.mjs`, `.pi/extensions/*`, `pi-*` npm deps)~~ —
  replaced by the Python harness + Agent Protocol server (Phase 4)
- ~~In-process Go agent loop / Go dispatch surface / Go history recorder /
  Bubble Tea TUI~~ — replaced by deepagents + the Agent Protocol server
- ~~TOML org config~~ — replaced by per-org `AGENTS.md` markdown

Deferred (might land later if a concrete need emerges):

- SSE streaming for Agent Protocol runs (Phase 9)
- Multi-org orchestration (invest, twitter-agent, etc.) — current
  `.pi/agents/*.md` + `orgs/<name>/AGENTS.md` covers most cases
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Diligence evals, safeguard router
- Runtime MCP-server fallback URLs

## Conventions

- No co-authored-by Claude in git commits.
- Use astral uv for any Python environments (sandbox scripts, smoke test runner, the harness).
- Prefer 'prove' (integration-style) over 'assert' (unit-only) when feasible.
- "Verify or die" — no claiming a thing works without running it.
- IaC + self-bootstrap — every new service ships as docker-compose +
  bootstrap.sh, not manual `docker run`.
- No fallbacks, no deprecation aliases, no backwards-compat shims.

## Memory

Auto-memory lives at `~/.claude/projects/.../memory/`. The memory directory
tracks the strategic context — pivot rationale, fullstack lessons learned,
decisions deferred. Read `MEMORY.md` first when picking up context.
