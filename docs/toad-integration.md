# Toad ↔ Pux (coding org) integration — RETIRED SURFACE, kept for the toad-side facts

> **Status: RETIRED (2026-08 fold).** The ACP server this doc wired to is gone:
> `bin/pux` (the bash router), the harness ACP server, and the sandbox
> container were all removed with the pre-fold harness. The repo now serves no
> wire protocol of its own — the CLI is exactly `sync`/`check`/`compile`
> (`src/compiler/cli.py`) and the editor/TUI surface is dcode's
> `run_textual_app` + upstream `deepagents-acp`.
>
> What remains valid: the **toad-side knowledge** (agent-discovery quirks,
> the `toad acp "<cmd>"` escape hatch, the app-store TOML shape, the upgrade
> caveat) — toad 0.6.20 hasn't changed. If you ever need to drive an org from
> toad again, serve it via the upstream `deepagents-acp` package and adapt the
> template below. The pux-side mechanics are kept as the historical record so
> nobody re-derives them.

Original tested combination: `toad 0.6.20` as ACP client, the pre-fold harness
as ACP server (`deepagents-acp>=0.0.9`).

---

## Toad-side facts (STILL VALID — toad 0.6.20)

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

---

## Historical — the pux ACP server (pre-fold, retired)

- `bin/pux acp` → the pre-fold harness's `acp` entrypoint →
  `deepagents_acp.server.AgentServerACP` wrapping `build_graph(org)`.
- Org resolution (first wins): `--org` flag → `$PUX_ORG` → `general`.
- `bin/pux` self-resolved its own symlink, exported
  `PUX_PROJECT_ROOT="$REPO"`, `cd "$REPO"`, sourced `./.env` — safe from any
  cwd; the harness always saw this repo's `profiles/` tree.
- The stdout contract: the server pinned logging to stderr
  (`bootstrap_env_and_logging(pin_stderr=True)`) so no library log corrupted
  the JSON-RPC stream.
- The stdio/SSE transports were already moved upstream to `deepagents_acp`
  before the fold; the fold then removed the remaining wrapper.

**Post-fold equivalents:**
- `PUX_PROJECT_ROOT` survives as the root the workspace uses for `profiles/`
  discovery + graph compilation (`src/profiles/_paths.py`); `--org` / `$PUX_ORG`
  survive as the org selector in `uv run python src/run.py --org <org>`.
- The container half (the second env var `PUX_PROJECT_PATH`, bind-mounting your
  project at `/sandbox/workspace`) is **retired** with the Docker sandbox —
  the backend is deepagents' `LocalShellBackend` on the host fs
  (`src/sandbox/local.py`; `WORKSPACE_ROOT` constant survives in
  `src/sandbox/exec.py`).

---

## Wiring options (post-fold)

### Option A — one-shot (if an ACP server is running)

```bash
toad acp "<acp-server-command> --org coder"
```

With the pre-fold harness this was `toad acp "pux acp --org coder"`. Today you
would point it at a `deepagents-acp`-based server if one is running.

### Option B — permanent entry in toad's app store

Use this if you want an org to show up in toad's agent picker alongside
Claude Code / OpenHands / etc. You must write the TOML into the **installed
package's** data dir (the only place `read_agents()` looks in 0.6.20):

```bash
DST=$(python -c "import importlib.resources as r, toad.data; print(r.files('toad.data')/'agents')")
cp "$REPO/docs/toad-coder.toml" "$DST/pux.coder.local.toml"
```

Template (`docs/toad-coder.toml` — still in the repo):

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

The coding org from the auto-developer-orchestrator repo, served over ACP.
Includes dev-bot-explorer + the non-skippable RubricMiddleware ship-gate.
'''
# run_command is the ACP-server command to spawn; toad pipes JSON-RPC to it.
# ⚠️ ADAPT: the pre-fold value was ".../bin/pux acp --org coder" — bin/pux is
# gone; point this at your deepagents-acp server command.
run_command."*" = "<your-acp-server> --org coder"
```

Then launch directly by short name:

```bash
toad -a pux-coder
```

⚠️ **Upgrade caveat**: `uv tool install -U batrachian-toad` replaces the package
dir and wipes anything you dropped in `toad/data/agents/`. Re-copy the TOML
after upgrading, or — better — script it (Makefile target below).

⚠️ **Why we can't use `~/.config/toad/agents/`**: that path does not exist in
toad 0.6.20. If a future toad release adds a user overlay (likely, given the
schema comments), switch Option B to that directory and drop the upgrade
caveat. Verify with:

```bash
python -c "import toad.agents, inspect; print(inspect.getsource(toad.agents.read_agents))"
```

---

## Makefile target (so it's not just prose)

```make
.PHONY: toad-install-coder
toad-install-coder: ## Install the coder org into toad's app store (re-run after toad upgrades)
	@DST=$$(python -c "import importlib.resources as r; print(r.files('toad.data')/'agents')") && \
	cp docs/toad-coder.toml "$$DST/pux.coder.local.toml" && \
	echo "Installed pux.coder.local → $$DST"
```

---

## Troubleshooting (historical rows marked; toad-side rows still valid)

| Symptom | Cause / fix |
|---|---|
| "agent not found" after toad upgrade | Option B TOML was wiped by `uv tool install -U`. Re-run `make toad-install-coder`. |
| (RETIRED) "Failed to initialize agent" / hangs on first prompt | The pre-fold `pux acp` printed to stdout — its logging was pinned to stderr; wrappers must not echo to stdout. N/A today. |
| (RETIRED) Agent can't see the org tree / "unknown org" | `PUX_PROJECT_ROOT` not set; `bin/pux` set it. Today: run the workspace from the repo root — `src/profiles/_paths.py` resolves the project root itself. |
| (RETIRED) Sandbox never boots / first tool call times out | The container sandbox; `make sandbox` pre-built the image. Retired with Docker. |
| Want a different org (invest / dre / game-studio) | Change `--org` (Option A) or clone the TOML with a new `identity`/`short_name` (Option B). One TOML per org. |
| Multiple toad sessions, different orgs | Each `toad acp` invocation is an independent agent process. |
| (RETIRED) Agent operates on the orchestrator repo, not my project | The pre-fold container bind-mount followed the editor's cwd via `PUX_PROJECT_PATH`. Retired with the container. |
| (RETIRED) `load_skill` finds nothing on an external project | Skills lived under the in-container org-tree mount. Retired with the container. |

---

## Why not MCP? (decision record)

The pre-fold harness *also* spoke MCP (`pux mcp` — Agent Protocol REST wrapped
as an SSE server). That was a **different** integration: MCP gives an *editor's*
agent access to pux's tools. Toad wants the *opposite* — toad wants to **be**
the editor driving a coding agent. That's ACP. The MCP surface is retired too;
the reasoning stands: ACP for editors, MCP for tools.

---

## Reference pointers

- `toad/agents.py` — `read_agents()`; the **only** place agents are loaded from.
- `toad/agent_schema.py` — `Agent` TypedDict + `run_command` shape.
- `toad/cli.py` — `acp` subcommand: identity `f"{cmd.split()[0].lower()}.custom.batrachian.ai"`.
- `docs/toad-coder.toml` — the app-store template (adapt `run_command`, see above).
- `profiles/specialists/coder/` — the org itself (`AGENTS.md`, `org.yaml`,
  `profile.yaml`, `policy.yaml`, `agents/*.md`).
- `src/run.py` — the launch (`--org`, `build_org_agent`, `run_textual_app`);
  `src/profiles/_paths.py` — `PUX_PROJECT_ROOT` / `profiles/` discovery.

---

## ACP is the universal pattern (not just a toad integration) — decision record

> "PUX should be deployed as an ACP server. That is how Aethna / Hermes
> communicates. A universal pattern." — the architectural framing.

That framing drove the pre-fold server (`pux acp --org <org>` as the canonical,
editor-agnostic integration point; the stdout contract and threading model
owned server-side). The fold kept the *conclusion* and deleted the *machinery*:
the editor surface is now dcode's own TUI, and any ACP client needing the
org graph would talk to upstream `deepagents-acp` directly. No per-client glue
in this repo either way.
