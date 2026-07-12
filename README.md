# Pux

**Deepagents (Python/LangGraph) driving a Docker sandbox.** Pux is an agent
orchestrator: a [deepagents](https://docs.langchain.com/oss/python/deepagents)
agent layer served over the [LangChain Agent
Protocol](https://langchain-ai.github.io/agent-protocol/), backed by a Docker
sandbox that exposes bash / file / browser / desktop / vision tools.

This repo is the **orchestrator app**: it owns `orgs/`, the `sandbox/` Docker
image, the integration `tests/`, and the optional `site/` web UI. The agent
layer is a separate library — **`pux-harness/`** — pinned as a git submodule
([github.com/JayDataEngineer/pux-harness](https://github.com/JayDataEngineer/pux-harness)),
consumed by uv as a path dependency (`[tool.uv.sources] pux-harness = { path = "pux-harness" }`).

- **`pux-harness/`** (Python, uv; submodule) — the deepagents Pux harness
  library. Builds per-org deepagents graphs (a CTO + specialist subagents),
  serves them over the Agent Protocol REST API, and ships the `pux` console
  script. Native fs/shell tools (`ls` / `read_file` / `write_file` /
  `edit_file` / `glob` / `grep` / `execute`) run through a `PuxSandboxBackend`;
  the 40 specialist tools (`browser_*`, `desktop_*`, `describe_image`,
  `python`, skills) are native Python too. `container.py` owns
  the Docker sandbox lifecycle + declarative policy enforcement.
  The harness boots its own container directly over the Docker SDK — there is
  no Go server.
- **`pux` console script** (`pux_harness.cli:main`) — the native CLI
  dispatches `pux direct` / `pux sandbox` / `pux <client-cmd>` into the harness.
  (The Agent Protocol server is **Aegra** in prod — `scripts/start_pux_aegra.sh` —
  or `langgraph dev` / `aegra dev` for local smoke; it is no longer a `pux`
  subcommand.)

Single-tenant, localhost-only, no auth. One pux process = one project = one
sandbox.

## Quick start

```bash
# 1. Clone (with submodules) + sync the orchestrator venv
git clone --recursive <this-repo> pux && cd pux
uv sync                    # resolves pux-harness from the ./pux-harness/ submodule

# 2. Start host-side infra (SurrealDB + media-mcp) — one command
make infra                 # or: make infra-core (SurrealDB only, lighter)
                           # GPU: MEDIA_DEVICE=cuda TORCH_VARIANT=cu124 make infra

# 3. Build the sandbox image (one-time)
make sandbox               # or: cd sandbox && docker build -t pux-sandbox:latest .

# 4. Boot the sandbox container (harness-owned; or it self-boots on first tool use)
pux sandbox start                  # with $PUX_ORG policy if set

# 5. Start the Agent Protocol server
#    prod (this repo's deployment): scripts/start_pux_aegra.sh   (Aegra on :9988)
#    local keyless dev:             cd pux-harness && uv run langgraph dev

# 6. Drive it (client — requires the server running)
pux agents                         # list the 14 orgs
pux dispatch --org general "describe this project"   # one-shot run → thread_id
pux resume                         # list recent threads (+ task snippets, offline-capable)
pux show <thread_id>               # prints last msg + the exact resume command

# No server? In-process runner for dev:
pux direct --org general --task "describe this project"   # runs the graph directly, no HTTP
```

### What `make infra` starts

| Service | Port | Used by |
|---------|------|---------|
| **SurrealDB** | `localhost:8000` | deep-research-engine (ns: research, db: main), game-studio, social-media-pipeline. The shared knowledge graph — persists across runs, query it to resume research. |
| **media-mcp** | `localhost:8101` | deep-research-engine (Parakeet ASR + Pyannote diarization + Florence-2 vision). Built from the `infra/media-mcp` submodule. |
| **ollama** | `localhost:11434` | Optional (`make infra-embeddings`). Embedding model for SurrealDB vector search. |

The sandbox reaches these via `host.docker.internal` (the docker gateway). Orgs
that need host-side services declare the URLs in their `policy.yaml`
`sandbox.env` block — no manual configuration needed. Ray cluster (LLM, TTS,
3D, music) is NOT managed here — only game-studio needs it; bring your own GPU
box or set `OPENROUTER_API_KEY` for LLM fallback.

The Agent Protocol server is **Aegra** in prod (OSS langgraph-api drop-in —
`scripts/start_pux_aegra.sh`, binds the Tailscale IP on :9988) or `langgraph dev`
/ `aegra dev` for local smoke; the `pux` client defaults to
`http://127.0.0.1:9988` (override with `PUX_API_URL`). There is no Go server —
the harness drives the Docker sandbox directly over the SDK.

## Subcommands

| Subcommand | What it does |
|------------|-------------|
| _(server)_ | The Agent Protocol server is **Aegra** (prod: `scripts/start_pux_aegra.sh`) or `langgraph dev` / `aegra dev` (local). Not a `pux` subcommand. |
| `pux acp [--org <name>]` | ACP stdio server — exposes one org to ACP editors (Zed / VS Code / Neovim); the editor IS the TUI. |
| `pux mcp` | FastMCP server (SSE on :9987) wrapping the Agent Protocol — exposes orgs as MCP tools to any MCP client (Hermes, Claude Desktop, Zed). Requires the Agent Protocol server running (Aegra / `langgraph dev`). |
| `pux direct --org <name> --task "..."` | In-process runner — no server. The verify/dev path. |
| `pux sandbox <start\|stop\|status\|ensure\|pause\|unpause\|dump-persist>` | Docker sandbox lifecycle (harness-owned, 8g). Replaces the old `task start/stop/status`. `pause`/`unpause` use the cgroup freezer — processes freeze in place (memory resident), no teardown, no re-boot. `dump-persist` streams the named Docker volume to a host tarball (the bits the workspace bind-mount does NOT cover). |
| `pux agents` | List orgs as Agent Protocol agents (+ their specialists). |
| `pux dispatch --org <name> "task"` | Ephemeral blocking run; prints the answer + a resumable `thread_id`. |
| `pux resume [--org <name>]` | List recent threads (with task snippets + offline fallback to the sqlite store when the server is down). The first half of "pick up where I left off". |
| `pux show <thread_id>` | Print a thread's last message + status, AND the exact `pux direct --thread <id> --task "…"` command to resume it. Works offline. |
| `pux history <thread_id>` | Print a thread's revision history (langgraph checkpoints). |
| `pux run <thread_id> "task"` | Background run on an existing thread → `run_id`. |
| `pux wait <run_id>` | Block for a background run's output. |
| `pux direct --thread <thread_id> --task "…"` | **Resume a thread in-process** — the checkpointer restores the full conversation, the agent sees every prior turn. No server needed. |
| `pux bundle <thread_id>` | **Optional** tarball export — transcript + artifacts + memos in one file. Works offline. `--all` ignores mtime, `--since ISO8601` filters, `--no-files` is transcript-only. |

## Tool surface

fs/shell is **deepagents-native** (via `PuxSandboxBackend.execute()` → docker
exec inside the container); all 40 specialists are **`pux_sandbox_*`** native
Python tools too. The container lifecycle moved into
`container.py`; the Go bridge was deleted — every model-visible path is
Python:

| Tool | Backed by |
|------|----------|
| `ls` / `read_file` / `write_file` / `edit_file` / `glob` / `grep` / `execute` | native — `PuxSandboxBackend.execute()` → docker exec (8a) |
| `python` | native — docker exec `python3 -c` (8b) |
| `list_skills` | native — host FS `orgs/_shared/skills/` + each `orgs/<name>/skills/` (8c). Discovery aid; bodies peeked via native `read_file`. |
| `describe_image` | native — **driving-model PRIMARY** (mimo-v2.5 multimodal) → in-sandbox ONNX fallback (8d) |
| `multimodal` | native — image **or** audio **or** video + a PROMPT → multimodal model (18.B). Returns the model's reasoning or an HONEST error; **no silent fallback** (the value is the prompt-conditioned judgment — e.g. "is this audio intelligible?" — that a generic describer can't give). |
| `multimodal_mega` | native — resilient sibling of `multimodal`: model first, then a per-type WATERFALL on failure (image→ONNX, audio→honest-unavailable, video→ffmpeg keyframes→per-frame image waterfall) (18.B). Use when you want SOMETHING back even if the model is down. |
| `browser_*` (autopilot surface) | native — `curl` to in-sandbox `sb_server.py` via docker exec (8e). Navigate/click/type/screenshot/evaluate PLUS the action set: `search`/`scroll`/`go_back`/`wait`/`find_text`/`extract`/`extract_images`/`save_screenshot`/`download`/`upload`/`tabs`/`new_tab`/`switch_tab`/`close_tab`/`dropdown_options`/`select_dropdown`/`save_session`/`restore_session`. Each tool's docstring carries the autopilot knowledge; the shared `browser` agent is a lean loop over them. |
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
├── AGENTS.md       # CTO system prompt body (prose only — no frontmatter)
├── org.yaml        # specialist roster: `agents: [slug, …]`
├── policy.yaml     # optional: egress ACLs, creds, sandbox image/tier, cookies
└── profile.yaml    # optional: per-org overrides (prompt suffix, tool overrides, models map, rubric verify-gate)
```

`pux --org <name>` (in-process) / `dispatch --org <name>` (server) appends the
body to the base system prompt — the main agent becomes that org's CTO and
delegates to its declared specialists via the `task` tool.

**`dev-bot` is the Claude-Code-equivalent coding org.** Its `AGENTS.md` is a
10-pattern coding state machine (PLAN → EXECUTE → RECOVER → ESCALATE, risk-tiered
autonomy, think-aloud-then-act, verify-or-die), and its `profile.yaml` opts into
the **`RubricMiddleware` verify-gate**: after the agent implements, a grader
sub-agent runs the test suite + reads the diff + greps for regressions and gates
the deliverable on a ship rubric (`satisfied` / `needs_revision` / revise, up to
`max_iterations`). Override the rubric per-run with `--rubric`:

```bash
pux direct --org dev-bot --rubric "- must include a docstring" "add foo(x) to src/bar.py + a test"
```

Specialist subagents are ONE file each — `orgs/<name>/agents/<slug>.md` (YAML
frontmatter: `name`, `description`, optional `tools`/`skills`/`model`; body =
the system-prompt prose). Cross-org agents live under `orgs/_shared/agents/`
(an org specializes one by dropping a same-named `<slug>.md` in its own
`agents/` dir). The org contract enforces that every `org.yaml` slug resolves
to a `.md` with the required frontmatter keys + a non-empty body (the
`no-legacy-agent-py` tripwire permanently forbids the old `.pi/agents/*.py`
form), every `tools:` entry is a real tool (native fs or a `pux_sandbox_*`
specialist), and any `policy.yaml` is schema-valid:

```bash
uv run pux check-contract   # exit 0 = green
```

## Threads & history

The Agent Protocol server persists threads + checkpoint history in SQLite
(`<project>/.pux/agent-protocol.sqlite`). Every run (ephemeral or background)
writes a resumable thread; revisions are langgraph checkpoints:

```bash
pux dispatch --org general "..."   # → thread_id
pux resume                         # list threads (with task snippets; offline-capable)
pux show <thread_id>               # last message + status + the resume command
pux history <thread_id>            # revisions
pux run <thread_id> "follow up"    # continue on the same thread (server mode)
pux direct --thread <thread_id> --task "follow up"   # continue in-process (no server)
```

## Picking up where you left off (session preservation)

A run is **not single-use**. Every thread's full conversation is checkpointed
to disk, the sandbox can be frozen in place (no teardown), and the workspace
bind-mount means files appear on the host the moment the agent writes them.
Resume is the default; export is the optional extra.

### The short version

```bash
pux resume                                    # list threads (+ task snippets)
pux show dre-deadbeef                         # prints last msg + the resume cmd
pux direct --thread dre-deadbeef --task "now write the substack version"
# (equivalently: pux run dre-deadbeef "now write the substack version"  → run_id)

# Freeze the sandbox between sessions instead of stop/start:
pux sandbox pause                             # cgroup freezer — processes frozen, memory resident
pux sandbox unpause                           # thaw — every process resumes in place
```

`pux direct --thread <id>` and `pux run <id> "…"` both route through the
langgraph checkpointer. The agent on resume **sees every prior turn** — the
research, the brief, the citations — as if the process never stopped. This
holds whether the original run was `pux direct` or `pux dispatch`.

### What persists, and where

Every run writes to **four layers**, each with a different persistence story.
`pux sandbox status` surfaces the sandbox ID + persist volume + thread store
location so you can verify the state is safe before stopping.

| Layer | Where it lives | Survives `sandbox stop`? | How to retrieve |
|-------|----------------|--------------------------|-----------------|
| Conversation + checkpoints | SQLite at `<project>/.pux/agent-protocol.sqlite` (or Postgres under Aegra prod) | yes | `pux resume`, `pux show <id>`, `pux direct --thread <id>` |
| Workspace files (`artifacts/`, `memos/`, `.pux/sessions/`, `wild-runs/`) | **bind-mount** — `<project>/<dir>/` on host the moment the agent writes | yes (host files) | `ls <project>/artifacts/` |
| Per-thread provenance | `<project>/.pux/sessions/<thread_id>.meta.json` (written by `pux direct`) | yes | `cat <project>/.pux/sessions/<thread_id>.meta.json` |
| Chrome profile + apt list + `/root` dotfiles | **named Docker volume** `sandbox-<id>-persist` | yes — `destroy()` starts a stopped container to snapshot before removing; `pause`/`unpause` keeps the container alive without teardown | `pux sandbox status` (shows volume size); `pux sandbox dump-persist` for a tarball |

**`PUX_SANDBOX_ID` (defaults to `mcp-default`) is the key for the named
persist volume.** Do NOT change it between runs — doing so orphans the old
volume (Chrome profile, installs, dotfiles) and starts a fresh one. The
`pux sandbox status` output names the volume explicitly so you can verify.

### `pause` vs `stop` — when to use which

| Action | Container | Processes | Memory | Use when |
|--------|-----------|-----------|--------|----------|
| `pause` | stays running (frozen) | frozen in place | resident | you'll resume within hours; want zero re-boot cost |
| `stop`  | removed | killed | gone (volume survives) | you're done for the day; frees RAM |
| `unpause` | still running | resumed mid-instruction | resident | continuing a paused session |

`pause` is the right answer to "I want to come back to this exact session
after lunch." `stop` is the right answer to "I'm done with the sandbox
entirely." **The thread store survives both** — `pux direct --thread <id>`
works regardless of container state (it boots a fresh container if needed,
then restores the conversation from the checkpointer).

### Exporting a run (optional)

`pux bundle` packages a thread into one tarball — useful for archival or
handing the work to someone who doesn't have Pux installed. Works offline
(falls back to the on-disk thread store + meta.json when the server is down):

```bash
pux bundle dre-deadbeef                       # → ./dre-deadbeef.tgz
pux bundle dre-deadbeef --all                 # ignore mtime filter
pux bundle dre-deadbeef --since 2026-07-12T00:00:00Z
pux bundle dre-deadbeef --no-files            # transcript only

# The named-volume bits (Chrome cookies, apt list, dotfiles):
pux sandbox dump-persist                      # → ./sandbox-<id>-persist-<ts>.tgz
```

The bundle tarball contains `MANIFEST.json` (thread_id, agent_id, file
inventory with sizes + mtimes, transcript source `server` or `disk`),
`transcript.json` (full thread state + revision history), and every
workspace file the agent wrote during the run.

Workspace files are gitignored (runtime state, not source). The bundle
output is gitignored too — it's a tarball, push it elsewhere. Artifact
files SHOULD carry a `pux:agent=… saved=… task=… stage=…` HTML-comment
provenance header (see `orgs/specialists/deep-research-engine/AGENTS.md`
"Provenance") so a future reader can trace a file back to its producing run.

## Tests

```bash
# from the repo root — `tests/` is the orchestrator integration suite (org
# contract, delegation, export, stack/graph/acp against the real orgs/ tree +
# the container-side sb_server.py JS). The pux-harness library's own
# org-agnostic suite lives in the submodule at pux-harness/tests/.
uv run pytest -q
```

The server tests use FastAPI's `TestClient` with a stub graph (no tokens, no
Docker) to lock the REST envelope + thread/run CRUD; the real LLM-driven run
is proven end-to-end in the verify log (`pux direct --org general --task "..."`).

## Architecture

```
┌──────────────────────────────────────────┐
│ pux (console_scripts → cli.py)          │  native CLI
└──────────────┬───────────────────────────┘
               │ Agent Protocol REST (httpx)
┌──────────────▼───────────────────────────┐
│ aegra serve / langgraph dev (:9988)      │  Agent Protocol server
│  deepagents org graphs; langgraph-api    │  owns checkpointer + store
└──────────────┬───────────────────────────┘
               │ deepagents graph + PuxSandboxBackend
┌──────────────▼───────────────────────────┐
│ harness (Python, deepagents)             │  40 specialists NATIVE; no MCP hop
│  container.py (lifecycle + policy) +     │  for fs/shell OR specialists
│  docker_exec.py (docker exec)            │
└──────────────┬───────────────────────────┘
               │ Docker SDK (create / exec / stop)
┌──────────────▼───────────────────────────┐
│ pux-sandbox container                    │
│  Chrome + Xvfb + xdotool + tesseract +   │
│  supervisord + /workspace bind-mount     │
└──────────────────────────────────────────┘
```

There is no Go server on this branch. The Go MCP tree + JSON-RPC bridge were
deleted — every model-visible path (fs, shell, the 40 specialists,
and the container lifecycle) lives in the Python harness and drives the sandbox
directly over the Docker SDK.

## Web UI (`site/`)

`site/` is an OPTIONAL, standalone React/Vite/CopilotKit frontend — a browser
workbench for the harness (chat sidebar + editor / terminal / sandbox / VNC
panels). It is NOT a member of any workspace: the repo root is a uv workspace,
and `site/` carries its own `package.json` + `package-lock.json` + `tsconfig.json`
(`rm -rf site/` leaves the rest of the repo untouched). It talks to a running
harness two ways — the chat sidebar hits the AG-UI endpoint at
`http://127.0.0.1:9988/agui/<org>` (proxied through a small Node BFF in
`site/server/`), and thread/run/agent CRUD goes to the Agent Protocol REST API
at `:9988` directly. Run it from `site/`:

```bash
scripts/start_pux_aegra.sh &            # Agent Protocol server on :9988 (Aegra)
cd site && npm install && npm run dev   # vite (5176) + Node BFF (3001)
```

Open http://127.0.0.1:5176. See [`site/README.md`](site/README.md) for details.

## Branch layout

- **`pi-pivot`** — current. Deepagents pivot: Phases 0–18 shipped (harness +
  native sandbox, declarative contract, TS harness deleted, Agent Protocol
  server + client, all 10 orgs ported to RUN on deepagents, the policy engine
  ported Go→Python + its enforcement wired into `container.py`, proactive
  context-offload, the entire Go sandbox re-hosted in Python — fs/shell + all
  40 specialists via direct `docker exec`, container lifecycle + policy
  enforcement harness-owned — and the Go MCP server + JSON-RPC bridge deleted).
  TUI as Agent Protocol consumer + SSE remains.
- **`master`** — pre-pivot MVP. Slim Go MCP server with in-process agent loop.
- **`v0.2.0-pre-pi-mono`** — tag of master HEAD before the pivot. Safety net.

## License

See [LICENSE](LICENSE).
