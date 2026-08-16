# Isolation patterns — how tools, servers, and subagents get placed

This is the citable catalog for two decisions that used to be made by
habit: **where execution runs** (host / container / remote) and **which
tool schemas enter which context window**. Every claim below is either a
seam dcode/deepagents already ships, or is marked as not-yet-native with
its upstream path. Nothing here is a guard-rail inside a tool server.

Two axes, decided independently:

- **Execution isolation** — where code runs when a tool fires.
- **Context isolation** — which tool schemas a model is offered.

## Sandbox patterns (execution isolation)

### S1 — session sandbox
`dcode --sandbox opensandbox` (the `plugins/opensandbox` provider, entry
point `deepagents_code.sandbox_providers`).

The whole session — **including every subagent** — runs its
`ls/read_file/write_file/edit_file/glob/grep/execute` inside one
OpenSandbox container. Proved (2026-08-16): building the real
`create_cli_agent` with the real provider backend, every compiled
subagent's `FilesystemMiddleware` holds the *same* `CompositeBackend`
object as the main agent.

- **Use when:** the repo or task input is untrusted; you want zero host
  reach, delegation included; one container per session is acceptable.
- **Cost:** every file edit pays the container boundary; container
  lifecycle is the session's.

### S2 — on-demand sandbox (agent-driven, via MCP)
The `opensandbox` MCP server in the profile's `.mcp.json` (19 tools:
`sandbox_create`, `sandbox_connect`, `command_run`, `file_*`,
`sandbox_healthcheck`, ...).

A *trusted* session whose agents explicitly create/connect containers
when they need isolated execution or a service. This is today's coding
profile.

- **Use when:** the session is trusted, work is mostly local, and agents
  occasionally need a disposable environment.
- **Cost (measured):** the 19 schemas ride into **every** subagent of the
  profile, uniform — including agents that will never spawn a container.
  Any specialist can create containers; that capability sprawl is the
  price of S2 until per-subagent scoping is native (L3).

### S3 — workload sandbox (service hosting)
A persistent OpenSandbox container hosting an MCP server over HTTP,
consumed by a remote graph on Aegra; dcode reaches that graph through the
native `[async_subagents]` seam. The repo's instance is
`deployments/browser-specialist` (mc_browser + Chromium, 23h lease
renewed every 12h).

- **Use when:** the toolstack's schemas must **never** enter any session
  (42 browser tools); the toolstack is stateful across calls (cookies,
  tabs); its execution surface is untrusted (arbitrary Python escape
  hatch) and must be bounded by a container, not by rules.
- **Cost:** delegation round-trip instead of millisecond feedback; a
  deployment to run and keep healthy.
- **Worked example:** see `docs/browser-specialist-isolation.md`.

**S1 vs S2 vs S3 in one line each:** S1 sandboxes the session's own
hands; S2 hands the session a sandbox factory; S3 moves the whole
toolstack out of the session and into a sandbox.

## MCP segmentation ladder (context isolation)

| Level | Seam | Status |
|---|---|---|
| L0 | workspace `.mcp.json` — every server in every session | available; wrong for lanes |
| L1 | profile `.mcp.json` — only the lane's servers load | **in place** (all six profiles) |
| L2 | per-server trim: `allowedTools` in `.mcp.json` | **in place** (ray: 11 tools; web_research: 3) |
| L2.5 | model-keyed harness exclusion — the bridge below | **in place** (3 agents scoped, `make scoping-check`) |
| L3 | per-subagent `tools:` / `skills:` frontmatter | **not native** — upstream PR prepared (`feat/subagent-tools-skills-frontmatter`, langchain-ai/deepagents `libs/code/`) |
| L4 | spatial: server never enters any session (S3 + async subagent) | **in place** (browser) |

## The L2.5 bridge — model-keyed harness exclusion (shipped)

dcode re-runs harness-profile resolution against each subagent's **own**
model, and a profile's `excluded_tools` installs `_ToolExclusionMiddleware`
on exactly that subagent. That is deepagents' sanctioned seam, and it is
addressable *today* through frontmatter — no patches:

- An agent opts into the no-MCP tier with `model: openai:glm-5-turbo` —
  a real model on the same z.ai gateway (`.env`: `OPENAI_BASE_URL` +
  `OPENAI_API_KEY`, mirroring the pux-openai provider) that no session
  uses as its main model, so the key can never hit a main agent.
- `plugins/tool-scoping` registers the profile under that key via the
  `deepagents.harness_profiles` entry-point group, from
  `HarnessProfileConfig` YAML. The deny-list holds **199 MCP tool names**
  (equibles 62, github 44, godot 36, opensandbox 19, surreal 14,
  web_research 3, ray 11, nitter 10 — captured live, from `.mcp.json`
  trims, from in-repo source, from the Equibles C# checkout, or via a
  docker stdio handshake).
- Scoped agents keep the built-ins (`execute`/`read`/`write`/`task`,
  async-task middleware) and lose **every** MCP tool. Scoped today:
  `game-studio-docs-writer`, `task-planner` (game + coding),
  `web-agent` (coding — its browser work rides the browser-specialist
  async subagent, which is middleware, not MCP).

**Limits, stated plainly:**
- Deny-lists fail **open**. A server that adds a tool after capture leaks
  into scoped agents — so `make scoping-check`
  (`profiles/scoping_check.py`) is the tripwire: it rebuilds every
  profile through dcode's own assembly, asserts every scoped agent's
  *effective* MCP set is empty, that no unscoped agent picked up an
  exclusion middleware (key collision), that the deny-list covers every
  live tool name, that every `${VAR}` placeholder in every `.mcp.json`
  resolves (an unset URL is a silently-dead server), and WARNs on
  declared-but-empty servers (a down server must never masquerade as
  "scoped clean").
- The key is the **model**, not the agent: scoping is per-agent only
  because each scoped agent carries a distinct model string. Two tiers
  with different exclusion sets need two model strings.
- Agent files are shared across profiles (symlink union), so a scoped
  agent is scoped **everywhere it is rostered**. That is why
  `web-search` stays unscoped: research needs its web_research tools,
  and the frontmatter cannot distinguish profiles.
- `general-purpose` (auto-added by core) inherits the main model and is
  not addressable by the bridge — same known hole as L3's non-goals.
- Every server has an enumeration source — no open holes: live capture
  (godot, opensandbox, surreal, web_research, github, nitter, equibles),
  the `.mcp.json` trims (ray), the in-repo nitter source, the Equibles
  C# checkout (`~/Documents/programs/vendor/mcp/equibles-mcp`, served
  on :43181 by its compose override — `make infra-equibles`), or the
  official github docker image. `equibles` was captured live after the
  stack was stood up (62 tools served, 64 declared); `ray_inference` is
  the only server not running locally (Tailscale GPU cluster,
  bring-your-own) and its 11 declared tools are in the list.
- A second silent-death mode the check now fails on: an unset `${VAR}`
  placeholder in any `.mcp.json` (that is exactly how nitter sat at
  0 tools for days **behind a healthy container** — the URL variable was
  never set). The placeholder sweep is a hard failure, not a warn.

**Retirement:** when the upstream L3 PR lands, replace each `model:` line
with an explicit `tools:` allowlist, delete the plugin, the `.env`
gateway mirror, and this section; `make scoping-check` then changes to
assert allowlists instead of exclusions.

## Decision rules

1. **Measure the tax, then escalate.** Tax = schemas held × agents
   holding them. The audit below is the baseline; a specialist holding
   tools it will never call (docs-writer with 36 godot tools) is the
   signal to move up a level.
2. **Credentials and host reach decide urgency.** A server with auth
   material (`surreal` headers) or host-process reach (`godot-mcp-runtime`
   spawns the local Godot binary; `run_script` executes GDScript) must be
   deliberate about *who* holds it. Below L3 the only control is the
   profile; say so explicitly rather than pretending the persona scopes it.
3. **Statefulness demands a container** (S3 for never-enter schemas, S2
   for the rest). Browser cookies and warm sessions must not die with a
   tool call.
4. **Latency demands in-process.** Repo work (edit/run/verify loops)
   never goes behind an RPC boundary — the file-sync tax dwarfs the win.
5. **Prompted is not scoped.** A persona that *names* a server grants
   nothing and forbids nothing. The game persona says "godot for scenes";
   the measurement says all 11 subagents hold all 50 tools.

## The audit (measured 2026-08-16, real assembly per profile)

Method: dcode's own `resolve_and_load_mcp_tools` → `create_cli_agent`,
capture hook on `create_subagent`, dedupe the double assembly pass.
Remote/opt-in servers that served 0 tools in the run are noted — they
ride the same mechanism when up.

| profile | subagents compiled | MCP schemas per subagent | servers (live count) |
|---|---|---|---|
| coding | 7 | 19 | opensandbox:19 (github: token absent in prove env) |
| research | 7 | 17 | surreal:14, web_research:3 (equibles, nitter down/opt-in) |
| invest | 4 | 17 | surreal:14, web_research:3 (equibles down) |
| game | 11 | **50** | godot-mcp-runtime:36, surreal:14 (ray down → 61 when up) |
| media | 6 | 14 | surreal:14 (ray down → 25 when up) |
| social | 4 | 14 | surreal:14 (nitter down) |

Uniform in every profile — `general-purpose` included, and including the
agents whose prompts name no server at all (coding 0/6, social 0/3,
media 3/5 name one).

## Target state per profile (authored; the zero-MCP rows are SHIPPED via the L2.5 bridge, the rest blocked on L3)

The mapping below is the intent we will author as `tools:` frontmatter
the day the upstream PR ships. It follows each specialist's role, not
its prompt's incidental mentions. `[]` means the agent keeps only the
filesystem scaffolding — a deliberate downgrade from "inherits 50 tools".

**game** (from 50/subagent):
- gameplay-programmer → godot (all 36) + surreal
- technical-artist → godot (inspect + runtime subset) + ray + surreal
- qa-tester → godot (run_project, get_debug_output, simulate_input,
  get_ui_elements, take_screenshot, validate)
- renderer → ray + godot (take_screenshot, run_project, get_debug_output)
- art-specialist → ray + surreal
- creative, design-researcher, narrative-designer → surreal
- docs-writer, task-planner → no MCP tools (**shipped** — L2.5 bridge)

**media** (from 14–25/subagent): artist → ray + surreal;
pipeline-engineer → ray + surreal; director → surreal; video-renderer →
ray; video-scriptwriter → surreal.

**research**: dre-auditor/synthesizer/writer → surreal; web-search →
web_research; researcher, explorer → web_research + surreal.

**invest**: invest-researcher → equibles + web_research + surreal;
invest-trader → equibles + surreal; researcher → web_research + surreal.

**coding**: code-worker → github + opensandbox; coder-explorer,
explorer → github; web-agent, task-planner → no MCP tools (**shipped** —
L2.5 bridge; S2's factory stays with the worker that executes untrusted
code); web-search keeps its tools — it is rostered in research too,
where web_research is its whole function, and agent files are shared
across profiles so frontmatter cannot scope it per-profile (a
profile-local agent copy would be the L3-era fix if coding ever needs
it scoped).

**social**: twitter-drafter → nitter + surreal; smp-writer,
telegram-drafter → surreal.

Known hole: `general-purpose` (auto-added by core) inherits everything
and is not frontmatter-addressable — an upstream follow-up, tracked with
the PR's non-goals.

## Ops pointers

- S1: `dcode --sandbox opensandbox` (after
  `uv pip install --python "$(uv tool dir)/deepagents-code/bin/python" ./plugins/opensandbox`
  — re-run after any dcode upgrade).
- S2: `make sandbox` (server on :8080), `make sandbox-status`.
- S3: `make aegra-sandbox-image` once, then `make aegra` / `aegra-status`
  / `aegra-sandbox-kill`.
- L2.5: `uv pip install --python "$(uv tool dir)/deepagents-code/bin/python"
  ./plugins/tool-scoping` (same re-run-after-upgrade rule as S1), then
  `make scoping-check` — run it after any `.mcp.json`, `allowedTools`, or
  server upgrade: the deny-list is capture-based and fails open. After a
  server change, regenerate with
  `plugins/tool-scoping/regenerate.py` and **reinstall** (the venv holds a
  copy, not a link — editing the repo YAML alone changes nothing at
  runtime).
