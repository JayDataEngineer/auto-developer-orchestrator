# Toad ↔ Pux (coding org) integration

> Toad (`batrachian-toad`) is an ACP **client** TUI. Pux is an ACP **server**.
> They already speak the same protocol — wiring is just "point toad at `pux acp`."

This doc exists so we never have to reverse-engineer the wiring again.

Tested with: `toad 0.6.20`, `pux-harness` (master, `deepagents-acp>=0.0.9`).

---

## Prerequisite — put `pux` on PATH (one-time)

`pux` is NOT auto-installed. It's a bash launcher at `bin/pux` in this repo.
Symlink it onto PATH (the launcher self-resolves its own symlink, so it works
from any cwd):

```bash
ln -sf /home/user/Documents/programs/dev/auto-developer-orchestrator/bin/pux ~/.local/bin/pux
hash -r
which pux          # → /home/user/.local/bin/pux
pux                # prints usage → confirms PATH wiring
```

⚠️ **Do NOT run `pux acp` bare in a shell.** It's a stdio JSON-RPC **server** —
it blocks waiting for ACP protocol input on stdin. stdout stays clean (logging
is pinned to stderr via `bootstrap_env_and_logging(pin_stderr=True)` so no
library log corrupts the JSON-RPC stream). It is meant to be *spawned by an ACP
client* (toad / Aethna / Hermes / Zed / VS Code), not invoked by a human.

Quick sanity check it speaks ACP (no client needed) — feed it one handshake
line, then EOF:

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":true},"terminal":true},"clientInfo":{"name":"manual","title":"manual","version":"0"}}}' \
  | pux acp --org coder 2>/dev/null | head -1
# → {"jsonrpc":"2.0","id":1,"result":{"agentCapabilities":{"loadSession":true,...},"protocolVersion":1}}
```

A JSON-RPC response on stdout = the server is healthy and any ACP client can
drive it.

---

## TL;DR

`cd` into the project you want to work on, then:

```bash
toad acp "pux acp --org coder"
```

That's it. Toad spawns `pux acp --org coder` as a subprocess, speaks ACP over
stdio, and the **coder** org's compiled deepagents graph is the agent. Toad
passes your cwd as the ACP `session/new` cwd; pux exports it as
`PUX_PROJECT_PATH` so the sandbox mounts **your project** at
`/sandbox/workspace` (Claude-Code-style "spawn in folder"). The sandbox
self-boots lazily on first tool call (same as `pux direct`), so no prior
`pux sandbox start` is needed.

To target a different org, just change `--org` (`invest`, `deep-research-engine`,
`game-studio`, …). Or `export PUX_ORG=coder` and shrink it to `toad acp "pux acp"`.

---

## How the two pieces fit

### Toad — ACP client

- Toad launches an agent **process** and talks JSON-RPC over the process's
  stdin/stdout. ACP is the *only* protocol toad supports
  (`agent_schema.py`: `type AgentProtocol = Literal["acp"]`).
- Agent discovery (`toad/agents.py::read_agents`) iterates TOML files in the
  **installed package** at `toad/data/agents/*.toml` via
  `importlib.resources.files("toad.data").joinpath("agents").iterdir()`.
- ⚠️ **There is no user-overlay directory in toad 0.6.20.** Dropping a TOML in
  `~/.config/toad/` does **nothing** — `read_agents()` never looks there. The
  `~/.config/toad/toad.json` file only stores `anon_id`.
- The escape hatch is the **`toad acp "<cmd>"`** subcommand
  (`toad/cli.py`): it wraps any ACP-speaking command as a one-shot agent with
  identity `f"{first_word}.custom.batrachian.ai"`. No TOML required.

### Pux — ACP server

- `bin/pux acp` → `python -m pux_harness.acp` → `deepagents_acp.server.AgentServerACP`
  wrapping `build_graph(org)`.
- Org resolution (first wins): `--org` flag → `$PUX_ORG` → `general`.
- `bin/pux` self-resolves its own symlink, exports
  `PUX_PROJECT_ROOT="$REPO"`, `cd "$REPO"`, sources `./.env`. So it is safe to
  invoke from any cwd — the harness always sees this repo's `orgs/` tree.
- The stdout contract is enforced: `acp.py::run_acp` calls
  `bootstrap_env_and_logging(pin_stderr=True)` first, pinning the root logger
  to stderr so no library log ever corrupts the JSON-RPC stream.

Net: **`pux acp` is a drop-in for any ACP editor** (Zed / vscode-acp / Neovim /
**toad**).

---

## Wiring options

### Option A — one-shot (recommended, zero persistence)

```bash
# cd into the project you want the agent to work on, then:
toad acp "pux acp --org coder"
```

**The agent spawns in your cwd — Claude-Code-style.** Toad passes your launch
dir as `cwd` in the ACP `session/new`. The pux ACP server exports it as
`PUX_PROJECT_PATH` (`_capture_editor_cwd` in `acp.py`), so the lazily-booted
sandbox container mounts **that folder** at `/sandbox/workspace`. The coder
agent's `read_file`/`edit_file`/`execute`/`glob`/`grep` then operate on YOUR
project, not the orchestrator repo. Each unique project path gets its own
container (keyed by the `openshell.project-path` Docker label), so multi-project
isolation is automatic.

```bash
cd ~/my-project && toad acp "pux acp --org coder"   # agent sees ~/my-project
```

You can also pin the project explicitly via env (wins over the editor's cwd):

```bash
PUX_PROJECT_PATH=~/my-project toad acp "pux acp --org coder"
```

**How the cwd→workspace seam works (the two-env-var split):**

| Env var | What it controls | Who sets it |
|---|---|---|
| `PUX_PROJECT_ROOT` | where `orgs/` + the harness live **on the host** (graph compilation, system prompt) | `bin/pux` → the orchestrator repo |
| `PUX_PROJECT_PATH` | what the **container** bind-mounts at `/sandbox/workspace` (the agent's fs surface) | `_capture_editor_cwd(cwd)` from ACP `session/new`; falls back to `PUX_PROJECT_ROOT` |

The harness compiles the coder org from the orchestrator repo's `orgs/` tree
(host-side, via `PUX_PROJECT_ROOT`) regardless of what the container mounts.
Only the container's workspace follows the editor's cwd. This is why the agent
keeps its full system prompt + rubric gate + subagents when pointed at an
external project — the org spec isn't in the container, it's on the host.

**Known gap: skills.** The `load_skill` tool lists skills by reading
`/sandbox/workspace/orgs/...` *inside the container*. When the workspace is an
external project (no `orgs/` tree), skill discovery returns empty. The core
tools (read/edit/exec/glob/grep) are unaffected. To close this, mount the
orchestrator repo read-only at `/sandbox/harness` and remap the skill path —
tracked separately.

Flags that matter (from `toad acp --help`):
- `-t/--title "Coder"` — pretty label in toad's status bar.
- `-d/--project-dir <path>` or the positional `PATH` — the cwd toad passes to
  the agent. **Honored** by pux (exported as `PUX_PROJECT_PATH` on the first
  `session/new`).
- `-s/--serve` — launch as a web app instead of TUI.

Persist it as a shell alias so you don't retype it:

```bash
# ~/.bashrc or ~/.zshrc
alias toad-coder='toad acp "pux acp --org coder"'
```

### Option B — permanent entry in toad's app store

Use this if you want `coder` to show up in toad's agent picker alongside
Claude Code / OpenHands / etc. You must write the TOML into the **installed
package's** data dir (the only place `read_agents()` looks in 0.6.20):

```bash
REPO=/home/user/Documents/programs/dev/auto-developer-orchestrator
DST=$(python -c "import importlib.resources as r, toad.data; print(r.files('toad.data')/'agents')")
cp "$REPO/docs/toad-coder.toml" "$DST/pux.coder.local.toml"
```

Template (`docs/toad-coder.toml`):

```toml
# Schema: toad/agent_schema.py. Filename MUST be <identity>.toml.
identity = "pux.coder.local"
name = "Pux — Coder (dev-bot)"
short_name = "pux-coder"
url = "https://github.com/JayDataEngineer/auto-developer-orchestrator"
protocol = "acp"
type = "coding"
author_name = "JayDataEngineer"
author_url = "https://github.com/JayDataEngineer"
publisher_name = "JayDataEngineer"
publisher_url = "https://github.com/JayDataEngineer"
description = "The local Pux coder org (Claude-Code-equivalent dev-bot CTO + rubric verify-gate) over ACP."
tags = []
help = '''
# Pux — Coder

The coding org from the auto-developer-orchestrator repo, served over ACP by
`pux acp --org coder`. Includes dev-bot-explorer + the non-skippable
RubricMiddleware ship-gate.
'''
# Absolute path so toad can launch it from any cwd.
# run_command is the ACP-server command to spawn; toad pipes JSON-RPC to it.
run_command."*" = "/home/user/Documents/programs/dev/auto-developer-orchestrator/bin/pux acp --org coder"
```

Then launch directly by short name:

```bash
toad -a pux-coder
```

⚠️ **Upgrade caveat**: `uv tool install -U batrachian-toad` replaces the package
dir and wipes anything you dropped in `toad/data/agents/`. Re-copy the TOML
after upgrading, or — better — script it:

```bash
# post-upgrade hook (run after `uv tool install -U batrachian-toad`)
make toad-install-coder   # see Makefile target below
```

⚠️ **Why we can't use `~/.config/toad/agents/`**: that path does not exist in
toad 0.6.20. If a future toad release adds a user overlay (likely, given the
schema comments), switch Option B to that directory and drop the upgrade
caveat. Verify with:

```bash
python -c "import toad.agents, inspect; print(inspect.getsource(toad.agents.read_agents))"
```

---

## Makefile target (so it's not just prose)

Suggested addition to the repo `Makefile`:

```make
.PHONY: toad-install-coder toad-acp-coder
toad-install-coder: ## Install the coder org into toad's app store (re-run after toad upgrades)
	@DST=$$(python -c "import importlib.resources as r; print(r.files('toad.data')/'agents')") && \
	cp docs/toad-coder.toml "$$DST/pux.coder.local.toml" && \
	echo "Installed pux.coder.local → $$DST"

toad-acp-coder: ## Launch toad wired to `pux acp --org coder` (one-shot, no install)
	toad acp "pux acp --org coder"
```

---

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `toad` shows "Failed to initialize agent" / hangs on first prompt | `pux acp` printed to stdout. Don't run a custom wrapper that echoes — `pux_harness.acp` already pins logging to stderr. If you wrap it, your wrapper must not touch stdout. |
| `toad acp "pux acp"` serves the **general** org | Org resolution fell through to default. Pass `--org coder` or `export PUX_ORG=coder`. |
| Agent can't see `orgs/` / "unknown org" | `PUX_PROJECT_ROOT` not set. Don't bypass `bin/pux` — it sets the env var. If you call `python -m pux_harness.acp` directly, export `PUX_PROJECT_ROOT=/path/to/this/repo` first. |
| Sandbox never boots / first tool call times out | Docker daemon down or `pux-sandbox` image missing. Run `make sandbox` once, then retry. `pux acp` lazily boots the sandbox on first tool use. |
| `toad -a pux-coder` says "agent not found" after upgrade | Option B TOML was wiped by `uv tool install -U`. Re-run `make toad-install-coder`. |
| Want a different org (invest / dre / game-studio) | Change `--org` (Option A) or clone the TOML with a new `identity`/`short_name` (Option B). One TOML per org. |
| Multiple toad sessions, different orgs | `toad acp "pux acp --org invest"` in another terminal. Each `toad acp` invocation is an independent agent process. |
| Agent operates on the orchestrator repo, not my project | Your editor didn't pass a cwd (or it isn't a dir). `cd ~/my-project` before launching toad, or `PUX_PROJECT_PATH=~/my-project toad acp "pux acp --org coder"`. Verify with `docker ps --format '{{.Label "openshell.project-path"}}'`. |
| `load_skill` finds nothing on an external project | Expected: skills live at `/sandbox/workspace/orgs/...` in-container; an external project has no `orgs/` tree. Core tools (read/edit/exec/glob/grep) are unaffected. |
| Two containers running after switching projects | Correct — each unique project path gets its own container (keyed by the `openshell.project-path` label). `docker stop` the old one if you want to reclaim memory. |

---

## Why not MCP?

Pux *also* speaks MCP (`pux mcp` — wraps the Agent Protocol REST API as an SSE
server on :9987). That's a **different** integration: MCP gives an *editor's*
agent (e.g. Claude Code inside Zed) access to pux's tools. Toad wants the
*opposite* — toad wants to **be** the editor driving a coding agent. That's ACP.
For toad, always use `pux acp`, never `pux mcp`.

---

## Reference pointers (so future-you can re-derive this)

- `toad/agents.py` — `read_agents()`; the **only** place agents are loaded from.
- `toad/agent_schema.py` — `Agent` TypedDict + `run_command` shape.
- `toad/cli.py` — `acp` subcommand: identity `f"{cmd.split()[0].lower()}.custom.batrachian.ai"`.
- `bin/pux` — the bash router; `acp)` branch → `python -m pux_harness.acp`.
- `pux_harness/acp.py` — the ACP server; `--org` flag, stdout-pinned logging.
- `orgs/specialists/coder/` — the org itself (`AGENTS.md`, `org.yaml`,
  `profile.yaml`, `policy.yaml`, `agents/*.md`).

---

## ACP is the universal pattern (not just a toad integration)

> "PUX should be deployed as an ACP server. That is how Aethna / Hermes
> communicates. A universal pattern." — the architectural framing.

Toad is one client of the pux ACP surface, not a special case. **`pux acp
--org <org>` is the canonical, editor-agnostic integration point.** Every
ACP-speaking client can drive every pux org the same way:

| Client        | What it is                                  | How it consumes `pux acp`                          |
|---------------|---------------------------------------------|----------------------------------------------------|
| **Toad**      | Terminal TUI (`batrachian-toad`)            | `toad acp "pux acp --org coder"` — see TL;DR above |
| **Aethna**    | Orchestrator/agent client                   | spawns `pux acp --org <org>` over stdio JSON-RPC   |
| **Hermes**    | Agent daemon                                | same — `pux acp` is the editor surface it drives   |
| **Zed / VS Code / Neovim** | ACP-aware editors              | `pux acp --org <org>` (the original design target) |

Why this matters: the **stdout contract** (`pux_harness.acp` pins logging to
stderr via `bootstrap_env_and_logging(pin_stderr=True)` so no library log can
corrupt the JSON-RPC stream) and the **threading model** (`thread_id =
session_id` in the checkpointer; `session/list` enumerates an org's sessions
across `pux acp` process restarts) are owned once, server-side. Any ACP client
that respects the protocol gets a working, resumable, org-scoped agent — no
per-client glue.

### Operating consequence

Run **one `pux acp` per org** you want on the editor bus. The org is fixed at
startup (`--org` / `$PUX_ORG`), so multi-org deployments run N processes. The
shared thread store (`threads.open_thread_store`) makes session resume work
across those process restarts. Do **not** try to multiplex orgs inside one
`pux acp` — the ACP `session/new` doesn't take an org param by design (the
graph is compiled once at boot).

### Transports moved upstream

The `pux acp` and `pux mcp` stdio/SSE transports have been removed from this
repo — they are the same concern (external client → agent) and now live in the
upstream `deepagents_acp` package. The Agent Protocol HTTP surface (Aegra
`serve`) is the single runtime; editors connect via the upstream ACP adapter.
