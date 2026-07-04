# Pux — Agent & Developer Guide

## What this is

Pux is **Deepagents (Python/LangGraph) driving a Docker sandbox.** The agent
layer (orchestration, subagent delegation, sessions/threads, skills, the Agent
Protocol server + client) is [deepagents](https://docs.langchain.com/oss/python/deepagents),
living in `harness/`. The harness ALSO owns the Docker sandbox lifecycle +
every specialist tool implementation (browser/desktop/vision/python/skills) —
all native Python as of Phase 8a–8g. There is **no Go server**: the Go MCP
tree (`backend/`) + its bridge client (`bridge.py`) were deleted in Phase 8i.
The harness boots its own container via `pux sandbox start` (or lazily on
first tool use) and drives it directly over the Docker SDK.

Two layers:

- **`harness/`** (Python, uv) — the agent layer. `pux_harness/graph.py` builds
  per-org deepagents graphs (CTO + specialist subagents) with a
  `PuxSandboxBackend` (native fs/shell tools) + native specialist
  `pux_sandbox_*` tools. `pux_harness/container.py` owns the Docker sandbox
  create/start/stop/remove + declarative policy enforcement. Served over the
  LangChain Agent Protocol REST API (`server.py`, FastAPI on `:9988`). Driven
  by `cli.py` (the `pux` client) or the in-process runner (`main.py`).
- **`bin/pux`** (bash launcher) — sources `.env`, routes `serve` / `direct` /
  `acp` / `sandbox` / client subcommands into the harness.

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

# Boot the sandbox container (harness-owned; self-boots on first tool use too)
pux sandbox start                # or: `pux sandbox status` to reuse a running one
                                 # (omitted → the harness boots lazily on first use)

# Start the Agent Protocol server (blocks; the canonical executor)
pux serve                        # FastAPI on http://127.0.0.1:9988

# OR: expose an org as an ACP stdio server — the editor IS the TUI
pux acp --org general            # drive from Zed / VS Code (vscode-acp) / Neovim
                                 # (sandbox self-boots lazily, like `pux direct`)

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

The Agent Protocol server reads `PUX_API_HOST` / `PUX_API_PORT`
(default `127.0.0.1:9988`), `PUX_API_DB` (default
`<project>/.pux/agent-protocol.sqlite`), `PUX_API_LOG` (default `info`), and
`PUX_MODEL` (provider/model, e.g. `mimo-v2.5`, `glm-5.2`).

**Why 9988 (the Agent Protocol server):** adjacent to the conventional Agent
Protocol port 8000, which is taken on this host.

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
│  - graph.py / orgs.py / native_tools.py         │   layer: fs/shell + 13
│  - container.py  SandboxContainer.ensure()      │   specialist tools ALL native
│  - docker_exec.py  DockerExecClient.exec()      │   (no MCP hop — direct
│  - context_offload.py + policy.py               │   docker exec)
└──────────────┬──────────────────────────────────┘
               │ Docker SDK (create / exec / stop)
┌──────────────▼──────────────────────────────────┐
│ pux-sandbox container                           │
│   Chrome + Xvfb + xdotool + tesseract +         │
│   supervisord + /workspace bind-mount +         │
│   backbone scripts (chmod 0444)                 │
└─────────────────────────────────────────────────┘
```

**There is no Go server.** Phase 8a–8f ported all 13 specialist tools into
`native_tools.py`; Phase 8g moved the container lifecycle into `container.py`;
Phase 8i deleted the Go MCP tree + `bridge.py`. Every model-visible path — fs,
shell, the 13 specialists, and the container lifecycle — lives in Python,
driving the sandbox directly over the Docker SDK.

## Harness layout (Python, deepagents)

```
harness/
├── pyproject.toml              # deepagents + langgraph + fastapi + uvicorn + httpx
├── pux_harness/
│   ├── graph.py                # build_graph(org) -> compiled deepagents graph
│   │                           # one shared DockerExecClient + PuxSandboxBackend per process
│   ├── server.py               # Agent Protocol server (FastAPI, :9988)
│   ├── cli.py                  # `pux` client (httpx → server)
│   ├── acp.py                  # ACP stdio server (`pux acp`) — editor = TUI (Phase 9)
│   ├── main.py                 # in-process runner (`pux direct`) + sandbox lifecycle
│   ├── sandbox.py              # PuxSandboxBackend(BaseSandbox) -> native fs tools
│   ├── docker_exec.py          # DockerExecClient: direct `docker exec` into the container
│   ├── container.py            # SandboxContainer: create/start/stop/remove + policy enforce
│   ├── native_tools.py         # 13 specialist StructuredTools (python/skills/vision/browser/desktop)
│   ├── context_offload.py      # ContextOffloadMiddleware + ctx_recall/ctx_search (Phase 7)
│   ├── ctx_store.py            # host-side stash for offloaded tool output
│   ├── model.py                # provider/model factory (PUX_MODEL)
│   ├── orgs.py                 # system-prompt builder + subagent loader + contract glue
│   ├── policy.py               # declarative policy resolver (Phase 6)
│   └── contract.py             # declarative org-contract enforcer (7 rules)
└── tests/
    ├── test_org_contract.py    # the all-orgs-green gate (rule 1–7)
    ├── test_server.py          # Agent Protocol routing (stub graph, no tokens)
    ├── test_acp.py             # ACP stdio handshake (subprocess, no tokens/Docker)
    ├── test_policy.py          # policy resolver parity (mirrors the deleted Go tests)
    ├── test_container.py       # SandboxContainer runtime decision table
    └── test_context_offload.py # offload + ctx_recall/ctx_search
```

**The deepagents seam (source-verified):** `create_deep_agent(model,
system_prompt, tools, subagents, backend, checkpointer)` (deepagents
`graph.py:270`). The `backend` flows into the main `FilesystemMiddleware`
**and** every subagent's — one backend serves the whole tree, so native
fs/shell tools (`ls/read_file/write_file/edit_file/glob/grep/execute`) are
available to every subagent regardless of its `tools:` whitelist.
`PuxSandboxBackend` subclasses `BaseSandbox` (Shape A — 4 abstract methods:
`execute`/`id`/`upload_files`/`download_files`); it inherits `ls/read/write/
edit/grep/glob` free, all routed through our `execute()` → **direct
`docker exec`** into the sandbox container.

**Adding an org:** drop `orgs/<name>/AGENTS.md` (CTO prompt **prose only** —
no frontmatter; its conventional role as agent *context*) plus an
`orgs/<name>/org.yaml` whose `agents: [slug…]` lists its specialists.
Optionally add `orgs/<name>/policy.yaml`. Run `--check-contract` to validate
(the contract also runs as a pytest gate). No harness-level per-org code —
org-bundled `*.py`/`Dockerfile`/`docker-compose.yml`/`bootstrap.sh` is the
org's sandbox payload, reached via the sandbox, never imported by the harness.

**Adding a subagent:** write **two files** under `.pi/agents/` (project data,
NOT the harness package — keeps the harness generic, mirrors how skills work):

- `<slug>.py` exporting a `SUBAGENT` dict — the deepagents-idiomatic config
  form. Required keys: `name`, `description`, `system_prompt`. Optional:
  `tools` (bare slugs like `["python", "browser_navigate"]`) and `skills`
  (source-root paths). `system_prompt` is read from the sibling `.md`:
  ```python
  from pathlib import Path
  SUBAGENT = {
      "name": "<slug>",
      "description": "action-oriented, what this subagent does",
      "tools": ["python"],
      "skills": [".pi/skills"],
      "system_prompt": Path(__file__).with_suffix(".md").read_text(),
  }
  ```
  The module MUST import only the stdlib (no `pux_harness.*` / Docker) so it
  stays loadable under `--check-contract` (offline, no tokens). Tool + skills
  resolution stays **central** (`orgs._resolve_tools` / `_resolve_skills`), so
  the `.py` never couples to the harness — that's what keeps it CI-safe.
- `<slug>.md` — system-prompt **prose only** (no frontmatter).

Then reference the slug from an org's `org.yaml`. Delegation is deepagents'
native `task(subagent_type="<slug>", description="…")`.

| Field | Python shape | Resolution (in `orgs.load_subagents`) |
|-------|-------------|---------------------------------------|
| `tools` | `["python", "browser_navigate"]` (bare slugs) | each entry → `pux_sandbox_<slug>` lookup against the specialist tool map; unknown fails loud. Native fs tools (`execute`/`read_file`/…) are NOT listed here — they come from the backend's `FilesystemMiddleware` for every subagent. |
| `skills` | `[".pi/skills"]` or `["orgs/invest/skills"]` | each entry is a **project-relative skills-ROOT directory** → mapped to a container-absolute path (`/sandbox/workspace/<path>`). deepagents' `SkillsMiddleware` resolves it against the **backend** (sandbox container) and scans every `<skill>/SKILL.md` beneath it. A source is a root, **not** an individual skill — passing `.pi/skills/source-citation` would load nothing (its only child is the `SKILL.md` file). Validated on host (the project is bind-mounted 1:1). |
| `model` | `"glm-5.2"` (bare shorthand) | `get_model(value)` → a `ChatOpenAI` instance. Omit to **inherit** the parent model (`create_deep_agent` injects it — `spec.get("model", model)`). A bare shorthand MUST route through `get_model`, NOT deepagents' `init_chat_model` (it fails on `glm-5.2` and can't carry our OpenCode Zen Go base_url/api_key). |

`middleware` is deliberately **not** a key: deepagents' `SubAgentMiddleware`
does not forward a raw spec's `middleware` key into the compiled specialist
(Phase 7), so setting it would be a silent no-op. Context-offload runs on the
main agent only.

**No legacy left behind (permanent).** The Phase-11 migration made the OLD
`.md`-with-frontmatter agent form and the `agents:`-on-AGENTS.md org form
HARD, PERMANENT contract failures — not a one-time cleanup. Two tripwires
block any future commit that reintroduces them: `no-legacy-agent-frontmatter`
(no `.pi/agents/*.md` may carry YAML frontmatter) and `no-legacy-org-roster`
(no `orgs/*/AGENTS.md` may carry frontmatter — the roster lives in
`org.yaml`). The `.py` form is the only valid agent-config path; the loader
has no fallback branch. See `feedback_no_legacy_left_behind`.

**Adding a skill:** skills follow the Agent-Skills spec — a `<kebab-name>/`
dir containing `SKILL.md` (frontmatter `name` == dir name, `description`
required) plus optional `scripts/`, `references/`, `assets/`. There are two
skill **roots**, both `SkillsMiddleware`-compatible:

- **Global** `.pi/skills/` — cross-cutting skills any agent can declare.
  Discoverable TWO ways: imperatively (`list_skills` / `load_skill` native
  tools, which scan ONLY this root) AND via a subagent's `skills: [".pi/skills"]`
  field (progressive disclosure). `source-citation` lives here.
- **Per-org** `orgs/<name>/skills/` — org-specific skills, declared ONLY via
  that org's specialists' `skills: ["orgs/<name>/skills"]` field
  (SkillsMiddleware, progressive disclosure). NOT browsable via `list_skills`
  — scoped discovery is the point (one org never browses another's skills).

A specialist consumes a skill declaratively: add the root path to its
`.pi/agents/<slug>.py` `SUBAGENT["skills"]`. `SkillsMiddleware` resolves the
root against the bind-mounted project, scans one level deep for every
`<skill>/SKILL.md`, and serves level-1 metadata at startup → the `SKILL.md`
body on invocation → `references/`/`scripts/` on demand. A source is a ROOT,
not an individual skill dir (`.pi/skills/source-citation` would load nothing).
Consolidate related capabilities into ONE skill that indexes its playbooks as
`references/` — fewer well-scoped skills outperform many.

The declarative **contract** (`contract.check_skill_roots`, rule 8) enforces
this globally: every `SKILL.md` must be Agent-Spec well-formed (`name` == dir,
kebab-case dir, non-empty `description`, parseable frontmatter — a
colon-space in an unquoted `description` breaks YAML plain-scalar parsing),
and no `.md` may sit loose directly under a root (the playbook-dump
regression — invisible to `SkillsMiddleware`). Run `--check-contract` to
validate; it's a pytest gate too.

## Lifecycle (`pux sandbox start` / `stop` / `status`)

The Docker sandbox lifecycle is **harness-owned** (Phase 8g). The Go
`task start/stop/status` surface is gone; `pux sandbox <cmd>` drives
`SandboxContainer` directly.

| Subcommand | Behavior |
|-----------|----------|
| `start` | `SandboxContainer.ensure()` — discover-by-label → reuse, else create + start. Idempotent. With `$PUX_ORG` set, the org policy is applied at create. |
| `status` | Discover the project's container by the `openshell.project-path` label; print name/image/status/network/runtime/org. Exit 1 if none running. |
| `stop` | `destroy()` — save-persisted + stop + remove. |
| `ensure` | Same as `start` (the path the exec client takes lazily on first tool use). |

**Single-tenant per project:** the container is discovered by the
`openshell.project-path=<abs-project>` label, so each project gets its own
sandbox. The exec client (`docker_exec.py`) self-boots via `ensure()` if no
container is found, so `pux sandbox start` is optional — the harness boots
lazily on first tool use.

## Tool surface

fs/shell is **deepagents-native** (via `PuxSandboxBackend.execute()` → **direct
`docker exec`** into the container — Phase 8a cut the MCP middleman); all 13
specialists are **native Python StructuredTools** too (Phase 8b–8f). Phase 8g
moved the container **lifecycle** (create/start/stop) into `container.py`, and
Phase 8i deleted the Go bridge entirely. **Every model-visible path is Python:**

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

## ACP (`pux acp`) — the editor is the TUI

`harness/pux_harness/acp.py` exposes `build_graph(org)` as an **Agent Client
Protocol** stdio server (`agentclientprotocol.com`) via
`deepagents-acp`'s `AgentServerACP`. An editor that speaks ACP — **Zed**, **VS
Code** (via `vscode-acp`), **Neovim** — connects to it; **the editor is the
TUI**, so Phase 9 ships zero UI code. Additive: touches nothing in `server.py`,
reverses no prior decision.

```bash
pux acp                # serve $PUX_ORG (or `general`) over stdio
pux acp --org invest   # serve a specific org
```

**Factory contract (source-verified):** `AgentServerACP(agent=factory)` where
`factory(context) -> CompiledStateGraph`. The server caches the first build —
one graph instance serves all sessions, keyed by `thread_id=session_id` in the
checkpointer — so the org is fixed at startup (first wins: `--org` → `$PUX_ORG`
→ `general`); `context.cwd` (the editor's project dir) is ignored because the
Pux sandbox workspace is the bind-mounted project, fixed by the container.
`MemorySaver` keys sessions; a persistent `AsyncSqliteSaver` (like `server.py`)
is a deliberate future option. The sandbox self-boots lazily on first tool use
(same path as `pux direct`), so `pux acp` needs no prior `pux sandbox start`.

**Editor env:** the editor process must have `OPENCODE_API_KEY` + `PUX_MODEL`
in its environment (a shell that sourced `.env`, or `bin/pux` which sources it).
The stdio server spends no tokens until the first `prompt`; `initialize` +
`new_session` are pure plumbing.

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

**The only path is Python** (`native_tools.py`). Add a new specialist as a
`StructuredTool` built in `native_tools.py` (it gets the shared
`DockerExecClient` for docker-exec); append its name to the `SPECIALISTS`
frozenset (which derives `SPECIALIST_TOOL_NAMES` for the contract's rule-4
resolver). Reference it from an agent's `tools:` frontmatter. No rebuild, no Go.

1. Write the StructuredTool in `native_tools.py` (see `pux_sandbox_python` or
   `_sb_post` for the curl-to-sb_server pattern, `_exec_desktop` for the
   xdotool pattern). Use `_result()` for JSON output — it sorts keys
   (`json.dumps(sort_keys=True, indent=2)`) for stable diffs.
2. Append the tool's short name to the `SPECIALISTS` frozenset — that drives
   both `build_native_specialists()` (the bound surface) and
   `SPECIALIST_TOOL_NAMES` (the contract resolver + `--check-contract` gate).
3. Reference it in the relevant agent's `tools:` frontmatter as
   `mcp:pux-sandbox/<name>` (the resolver strips the prefix and looks up
   `pux_sandbox_<name>`).
4. Add a routing test in `harness/tests/` exercising the real code path where
   feasible.

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
`{success:false, reason:"unavailable"}` — NOT a Python error, NOT an
`isError:true` envelope. The driving agent falls back to text-only
reasoning without breaking its loop.

## Browser (in-sandbox sb_server.py)

Five native tools wrap the sandbox's `sb_server.py` (persistent SeleniumBase
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

Four native tools wrap the sandbox's X11 desktop.

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

**Pipeline** — `harness/pux_harness/policy.py` is the *resolver* (ported 1:1
from the now-deleted Go package: `load/validate_env/env_vars/resolve_mounts/
egress_rules/resolve_tier`); `container.py::create()` is the *enforcer*
(Phase 8g ported `policy_hook.go::applyOrgPolicy` step-for-step). The harness
reads `PUX_ORG` and applies policy at container create. The no-model dry-run
`pux direct --org <X> --check-policy` resolves mounts/creds/egress/tier
without touching Docker.

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

**Verify gates:**

- `harness/tests/test_policy.py` — resolver parity cases (placeholder
  expansion, missing-env errors, optional vs required, hostname resolution,
  port range, IPv4 + IPv6 literals, DNS failure, sandbox.image+tier override,
  ResolveTier semantics, cookies_env injection pointer, shipped policies
  parse cleanly)
- `pux direct --org <X> --check-policy` — no-model dry-run (offline
  resolution + cred presence)
- E2E (proven 2026-07-02): twitter-agent with browser cookies — seed-cookies.sh
  runs at boot, 14 cookies persist, navigate to x.com → logged-in home feed;
  egress firewall drops example.com (3.9s timeout), allows api.x.com (404 from
  server, not firewall). game-studio bridge networking — 3 allow rules applied
  correctly, host-and-container return identical results per service state.

## Verification

**Harness contract** (`harness/tests/`, run with `uv run pytest -q` → **132
passing**):

- **Org contract** (`test_org_contract.py`): all 10 orgs green; rule-4
  tool-resolution against the static native surface (`NATIVE_FS_TOOLS` ∪
  `SPECIALIST_TOOL_NAMES` — no server, no Docker); each violation class fires.
- **Agent Protocol server** (`test_server.py`): pure helpers + HTTP routing
  with a stub graph (no tokens, no Docker) — locks the REST envelope + thread/run
  CRUD. The real LLM-driven run is proven end-to-end in the Phase 8i verify
  log (`pux direct --org general` with **no Go binary** → 15 harness modules via
  the researcher subagent).
- **Policy resolver** (`test_policy.py`): mirrors the deleted Go tests.
- **Container** (`test_container.py`): runtime decision table, cache-name
  determinism + live-match, env defaults, URL rejection.
- **Context offload** (`test_context_offload.py`): offload threshold + the
  ctx_recall/ctx_search exempt-from-re-offload fix.

**Verify gates before committing:**

- Harness: `cd harness && uv run pytest -q` + `uv run python -m
  pux_harness.main --check-contract` (exit 0)
- Boot check: `pux serve` + `pux agents` (server boots, lists 10 orgs) +
  `pux direct --org general "<forcing task>"` (real run returns ground truth)

## Pivot roadmap (pi-pivot branch)

Phases 0–11 shipped (2026-07-04): harness + native sandbox, declarative
contract, TS harness deleted, Agent Protocol server + client, all 10 orgs
ported to RUN on deepagents (Phase 5), policy engine Go→Python (Phase 6),
proactive context-offload (Phase 7), the entire Go sandbox re-hosted in
Python then deleted (Phase 8), ACP-first TUI (Phase 9), the declarative
subagent vocabulary (Phase 10), and subagents going Python-native —
`.pi/agents/<slug>.py` `SUBAGENT` dicts + `org.yaml` rosters replacing
`.md`-with-frontmatter, with the legacy form made a permanent contract
failure (Phase 11).

| Phase | What | Status |
|-------|------|--------|
| 5 | Port remaining 7 orgs to RUN on deepagents (delegation-forcing tasks) | **SHIPPED 2026-07-03** — all 10 orgs run E2E. Each `pux direct --org <name>` forcing task in `main.py:DEFAULT_TASKS` makes the CTO delegate via `task(subagent_type=<specialist>)` and drive a native fs/shell tool (`execute`/`read_file`/`glob`) against the org's own bundled content; every run returned the correct ground-truth answer. New structural test `test_every_org_has_a_forcing_task`. |
| 6 | Policy engine Go→Python (egress/creds/image+tier/browser) | **SHIPPED 2026-07-03** — `harness/pux_harness/policy.py` is a faithful 1:1 port of the (now-deleted) Go package (pure logic: load/validate_env/env_vars/resolve_mounts/egress_rules/resolve_tier). `tests/test_policy.py` mirrors the Go tests. Contract rule 5 runs the real engine as a deep-schema check (`load` + `resolve_mounts` — offline; `egress_rules` deliberately NOT called, it resolves DNS). Consumer: `pux direct --org <name> --check-policy`. Enforcement wiring (binds/env/caps/egress.conf staging at `ContainerCreate`) landed in **Phase 8g** (`container.py::create()`). |
| 7 | context-mode integration (ctx MCP + wrap_tool_call offload) | **SHIPPED 2026-07-03** — **native harness offload, NOT an external ctx-MCP bridge** (context-mode is a stdio Claude-Code bun plugin, unreachable over HTTP from the harness; meta-mcp `list_servers` confirmed). `harness/pux_harness/context_offload.py` `ContextOffloadMiddleware(AgentMiddleware)` measures each `wrap_tool_call`/`awrap_tool_call` result; a ToolMessage > `threshold` (default 8000 chars ≈ 2K tokens) gets stashed to `ctx_store.py` (host-side `<project>/.pux/ctx/<id>.txt+.json`; hex-only ids reject path-escape) and replaced with a preview + `ctx:<id>` handle. `ctx_recall` / `ctx_search` StructuredTools pull stashed bytes back on demand. This is the *proactive* complement to deepagents' own reactive `SummarizationMiddleware`. **Two findings the E2E surfaced + fixed:** (1) `ctx_recall`/`ctx_search` are exempt from offload — re-stashing trapped the agent in a recall→offload loop; (2) **main-agent-only** — deepagents' `SubAgentMiddleware` does not forward a raw SubAgent spec's `middleware` key into the compiled specialist (verified), so attaching it there is a silent no-op; subagent offload is a `CompiledSubAgent`-pre-compilation follow-up, not a shim. |
| 8 | Re-host sandbox in Python (`execute()`→docker exec; 13 specialist tools); wire policy enforcement here; delete Go MCP | **8a–8g SHIPPED 2026-07-03** — `docker_exec.py` `DockerExecClient` (container discovered by `openshell.project-path` label; `exec()` via Docker SDK `exec_run(tty=False)`); `native_tools.py` ports all 13 specialists (`_result` uses `sort_keys=True` → byte-stable JSON); `container.py` `SandboxContainer` owns the container lifecycle (faithful port of the deleted `manager.go::CreateSandbox`+`DestroySandbox`, CLI-mode slice) + policy **enforcement** (the part deferred from Phase 6 — binds/env/caps/egress.conf staging at create, porting `policy_hook.go::applyOrgPolicy` step-for-step). `ensure()` is the single-tenant gate; the exec client self-boots. `pux sandbox {start,stop,status}` CLI surface replaces `task start/stop/status`. |
| 8i | Delete the Go MCP tree + rewire the seam (LAST, after 8a–8g proven) | **SHIPPED 2026-07-03** — `git rm -r backend/` (79 Go files) + `harness/pux_harness/bridge.py` (the JSON-RPC client) + `scripts/smoke_mcp.py` (81 deletions total). The seam rewired: contract rule-4 tool-resolution now resolves against the static `NATIVE_FS_TOOLS` ∪ `SPECIALIST_TOOL_NAMES` (always-on, no live-bridge probe); `graph.py` builds specialists from `build_native_specialists()` and no longer references a bridge client; `main.py --check`/`--check-contract` run with no Go server; `main.py:DEFAULT_TASKS["general"]` repointed from counting deleted Go files to counting harness Python modules (ground truth 15). `Taskfile.yml` rewritten harness-focused (all Go build/run/smoke tasks gone). **Proven E2E**: stopped the Go binary + removed its container → `pux direct --org general` with **no Go binary present** → CTO delegates via `task(researcher)` → researcher runs a single native `execute find … -name '*.py'` → returns **15 harness modules** (exact ground truth), `legacy pux_sandbox fs/shell leaked: NONE`. pytest 130/130, `--check-contract` exit 0. |
| 9 | TUI/clients as Agent Protocol consumers (+ SSE streaming) | **ACP TRACK SHIPPED 2026-07-03** — `pux acp [--org X]` exposes `build_graph(org)` as a stdio ACP server (`harness/pux_harness/acp.py`, `deepagents-acp` `AgentServerACP(agent=factory)`); the editor (Zed / VS Code via vscode-acp / Neovim) IS the TUI — zero UI code. Org fixed at startup (`--org`→`$PUX_ORG`→`general`); factory caches the first build, sessions keyed by `thread_id=session_id` in `MemorySaver`; sandbox self-boots lazily. `test_acp.py` proves initialize+new_session over stdio (no tokens, no Docker). **Proven E2E** (`pux acp --org general` over stdio with glm-5.2): `prompt`→`stop_reason=end_turn`, 101 `session/update` frames streamed, CTO delegates via `task(researcher)`→native `execute find … -name '*.py'` (lazy sandbox boot)→verbatim **16 harness modules** (incl. the new `acp.py`); zero agent-side errors. Deferred: pux-serve SSE streaming + a terminal client of `pux serve` (Track 2/3) — ACP-first per the Phase-9 decision; the langgraph-api/Studio path remains reversible behind the same REST contract. |
| 10 | Declarative subagent vocabulary (rich `SubAgent` frontmatter) | **SHIPPED 2026-07-04** — frontmatter parser upgraded from the hand-rolled scalar splitter to real YAML (`orgs._split_frontmatter` → `yaml.safe_load`), so the full deepagents `SubAgent` vocabulary is expressible in `.pi/agents/<slug>.md`. `orgs.load_subagents` resolves five rich optional fields: `model` (via our `get_model` → `ChatOpenAI` instance, NOT `init_chat_model` which can't carry our base_url/api_key; omit → inherit parent), `skills` (project-relative skills-ROOT path → container-absolute `/sandbox/workspace/<path>`, activates `SkillsMiddleware` which scans the root for `<skill>/SKILL.md`; a source is a ROOT not an individual skill dir), `response_format` (JSON-schema dict passthrough), `permissions` (`list[FilesystemPermission]`, path-validated), `interrupt_on` (`dict[str,bool]`). `middleware` deliberately NOT passed (Phase 7: `SubAgentMiddleware` doesn't round-trip it). Contract gains offline validators for all five (`contract._validate_rich_fields`: `model-shape`/`skill-source-shape`+`skill-source-resolves`/`response-format-shape`/`permissions-shape`/`interrupt-on-shape`) + `KNOWN_AGENT_KEYS` extended. Shipped example: `.pi/agents/researcher.md` gains `skills: .pi/skills`. pytest 148/148 (was 133; +10 contract + 5 `test_load_subagents.py`); `--check-contract` + `--check` exit 0. Surfaced + fixed two latent bugs: (1) `game-studio-narrative-designer.md` had an unquoted `Two modes: brainstorm` colon in its `description:` — invalid under real YAML; quoted it (the only broken file across all 33 agent/org `.md`); (2) the E2E smoke surfaced that deepagents' `SkillsMiddleware` resolves skills against the BACKEND and treats each source as a skills-ROOT (scanning its children), so the original slug→individual-dir form silently loaded nothing — reshaped to ROOT-path semantics (`.pi/skills` → scans `source-citation/`). **Superseded by Phase 11** — the `.md`-frontmatter agent form and the `_validate_rich_fields` machinery were deleted; only `model`/`tools`/`skills` survived, now expressed as a Python `SUBAGENT` dict. |
| 11 | Subagents Python-native; org roster → `org.yaml`; legacy made permanent-failure | **SHIPPED 2026-07-04** — subagent config migrated from `.pi/agents/<slug>.md` (YAML frontmatter) to deepagents-idiomatic `.pi/agents/<slug>.py` exporting a `SUBAGENT` dict (`name`/`description`/`system_prompt` + optional `tools`/`skills`/`model`), with a sibling prose-only `<slug>.md` holding the prompt. The org roster moved OFF `orgs/<name>/AGENTS.md` frontmatter into `orgs/<name>/org.yaml` (`agents: [slug, …]`); `AGENTS.md` is pure CTO-prompt prose again. `orgs.load_subagents` loads each `.py` via `importlib.util.spec_from_file_location` (path-loaded — `.pi/` stays off `sys.path`); tool/skills/model resolution stays **central** so the modules import only stdlib and stay CI/offline-safe under `--check-contract`. Audit finding drove the scope: ZERO of 22 agents used any rich field (`response_format`/`permissions`/`interrupt_on`) — the entire Phase-10 rich-field resolver machinery resolved nothing, so it was deleted (only `model`/`tools`/`skills` survived). **No legacy left behind (the user's standing rule):** two PERMANENT contract tripwires block reintroduction — `no-legacy-agent-frontmatter` (no `.pi/agents/*.md` may carry YAML frontmatter) + `no-legacy-org-roster` (no `orgs/*/AGENTS.md` may carry frontmatter) — and the loader's dual-read fallback branch was deleted (provably unreachable behind the tripwires). `.gitignore` re-negated `!orgs/*/org.yaml` (the source-trap). pytest 156/156; `--check-contract` exit 0; `git archive HEAD` ships all 21 `.py` + 10 `org.yaml`. Plan: `~/.claude/plans/declarative-cooking-wolf.md`. |

## Branch strategy

- **`pi-pivot`** = current branch. The deepagents pivot. PRs here evolve the
  Python harness as the sole agent + sandbox layer.
- **`master`** = pre-pivot MVP. Slim Go MCP server with the in-process agent
  loop + history recorder + Bubble Tea TUI + dispatch surface. Frozen from
  the pi-pivot perspective.
- **`v0.2.0-pre-pi-mono`** = tag of master HEAD before the pi-mono pivot. Safety
  net. `git show v0.2.0-pre-pi-mono:backend/internal/agent/loop.go` works.
- **`dev`** + **`v0.1.0-fullstack-legacy`** = the older fullstack predecessor
  (TUI, web UI, CLI, multi-agent). Frozen.

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

## What's NOT here (deferred or dropped)

Dropped (deepagents does it natively, or wasn't pulling weight):

- ~~pi-mono TS harness (`bin/pux.mjs`, `.pi/extensions/*`, `pi-*` npm deps)~~ —
  replaced by the Python harness + Agent Protocol server (Phase 4)
- ~~In-process Go agent loop / Go dispatch surface / Go history recorder /
  Bubble Tea TUI~~ — replaced by deepagents + the Agent Protocol server
- ~~Go MCP server (`backend/`) + `bridge.py` JSON-RPC client + `smoke_mcp.py`~~ —
  the entire Go sandbox re-hosted in Python then deleted (Phase 8a–8i)
- ~~TOML org config~~ — replaced by per-org `AGENTS.md` markdown

Deferred (might land later if a concrete need emerges):

- SSE streaming for Agent Protocol runs (Phase 9)
- Multi-org orchestration (invest, twitter-agent, etc.) — current
  `.pi/agents/*.md` + `orgs/<name>/AGENTS.md` covers most cases
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Diligence evals, safeguard router

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
