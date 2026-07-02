# Pux — Agent & Developer Guide

## What this is

Pux is **Pi-Mono driving a Docker sandbox MCP backend.** The agent layer
(orchestration, sessions, history, TUI, subagent delegation, skills) is
[pi-mono](https://github.com/badlogic/pi-mono) — pulled in as an npm dep.
The irreducible Go value (sandbox lifecycle, MCP wire protocol, tool
implementations) stays in `backend/`.

Two layers, one binary each:

- **`bin/pux.mjs`** (TS harness) — wraps pi-mono with our AGENTS.md system
  prompt, loads `pi-mcp-adapter` + `pi-subagents` + `.pi/extensions/*`, and
  exposes pux-specific subcommands (`dispatch`, `history`, `setup`,
  `teardown`).
- **`backend/mcpserver`** (Go binary) — boots a Docker sandbox, exposes
  bash/file/python/browser/desktop/vision tools over MCP at
  `http://127.0.0.1:9987`. Single-tenant, localhost-only, no auth.

The fullstack predecessor (in-process agent loop, Go TUI, Go history
recorder, dispatch surface) is gone — pi-mono does all of that better. The
pre-pivot HEAD is tagged `v0.2.0-pre-pi-mono` for safety. Branch: `pi-pivot`.

## Quick start

```bash
# One-time: build sandbox image
cd sandbox && docker build -t pux-sandbox:latest . && cd ..

# Boot the stack
pux setup                       # verifies Docker + image, builds + starts MCP server

# Drive it
pux                             # interactive TUI (pi-mono) against the cwd
pux --org _demo                 # interactive TUI with a CTO overlay (orgs/_demo/AGENTS.md)
pux dispatch --org _demo "task" # one-shot headless (= pux -p --org _demo "task")
pux --resume                    # pi-mono session picker (tree of past sessions)
pux --continue                  # resume most recent session

# History (pi-mono writes .jsonl sessions to ~/.pi/agent/sessions/)
pux history list                # shows recent sessions, suggests resume/continue commands

# Teardown
pux teardown                    # task stop
```

For direct Go-side work (bypassing the TS harness):

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
them.

**Why 9987 (not 9876):** the sandbox's `sb_server.py` (browser-mode HTTP API)
listens on 9876 *inside* the container. Any org sandbox that boots with
`--network=host` leaks that listener to the host's 9876. Defaulting
pux-mcpserver to 9987 avoids the collision out-of-the-box.

## Architecture

```
┌─────────────────────────────────────────────────┐
│ Operator                                        │
│   - pux                          (TUI)          │
│   - pux dispatch --org X "task"  (headless -p)  │
└──────────────┬──────────────────────────────────┘
               │
┌──────────────▼──────────────────────────────────┐
│ bin/pux.mjs (Node ≥ 22.19)                      │
│   - Loads pi-mono + extensions                  │
│   - .pi/extensions/pux-org-loader/ (--org flag) │
│   - node_modules/pi-mcp-adapter/ (MCP bridge)   │
│   - node_modules/pi-subagents/ (delegation)     │
│   - .pi/agents/*.md (specialist subagents)      │
│   - .pi/skills/* (skill markdown)               │
│   - AGENTS.md (system prompt)                   │
└──────────────┬──────────────────────────────────┘
               │ MCP JSON-RPC over HTTP (pi-mcp-adapter)
┌──────────────▼──────────────────────────────────┐
│ pux-mcpserver (Go, localhost:9987)              │
│   - MCP wire protocol                           │
│   - Tool registry (16 tools)                    │
│   - Lifecycle supervisor (run/start/stop)       │
└──────────────┬──────────────────────────────────┘
               │ docker exec
┌──────────────▼──────────────────────────────────┐
│ pux-sandbox container                           │
│   Chrome + Xvfb + xdotool + tesseract +         │
│   supervisord + /workspace bind-mount +         │
│   backbone scripts (chmod 0444)                 │
└─────────────────────────────────────────────────┘
```

## TS harness layout

```
auto-developer-orchestrator/
├── bin/pux.mjs                 # Launcher + subcommand intercepts
├── AGENTS.md                   # Root system prompt (pi-mono reads on boot)
├── .mcp.json                   # MCP client config (pux-sandbox server, directTools:true)
├── .pi/
│   ├── extensions/
│   │   └── pux-org-loader/     # --org flag: appends orgs/<name>/AGENTS.md
│   │       ├── index.ts        # 44 LOC extension
│   │       └── index.test.ts   # 5 vitest tests
│   ├── agents/                 # Specialist subagent files (rich frontmatter)
│   │   └── researcher.md       # example: read-only codebase investigator
│   └── skills/                 # Skill markdown (pi-mono native discovery)
│       └── source-citation/
│           └── SKILL.md        # example: cite file:line for every claim
├── orgs/                       # Per-org CTO overlays
│   └── _demo/
│       └── AGENTS.md           # Demo CTO body
├── package.json                # pi-coding-agent + pi-mcp-adapter + pi-subagents deps
├── tsconfig.json
└── vitest.config.ts
```

**Adding an org:** drop a directory under `orgs/<name>/` with an `AGENTS.md`
file. Its body gets appended to the system prompt on `--org <name>`. That's
the entire org loader — no TOML, no schema, no per-org config. If you need
per-org agents, ship them under `.pi/agents/<name>.md` with frontmatter
gating (e.g. `description: "Only load when --org=foo is set"` — pi-mono's
discovery picks up everything, so prompt-level gating is the contract).

**Adding a subagent:** write `.pi/agents/<name>.md` with frontmatter
(`name`, `description`, `tools`, `thinking`, `output`, `systemPromptMode`,
etc.) + body. pi-subagents discovers it automatically. Call via
`subagent({ agent: "<name>", task: "..." })` from the main session.

**Adding a skill:** write `.pi/skills/<name>/SKILL.md` with `name` +
`description` frontmatter + body. pi-subagents discovers it. Skills are
referenced from agent files via the `skills:` frontmatter field.

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
`v0.2.0-pre-pi-mono` tag if needed: `git show v0.2.0-pre-pi-mono:backend/internal/agent/loop.go`.

## Lifecycle (`run` / `start` / `stop` / `status`)

The binary ships with four subcommands. `mcpserver` with no subcommand (or
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

## Connecting MCP clients

The pux TS harness connects to the Go server via `.mcp.json`. To use the
sandbox from another MCP client (Claude Desktop, etc.):

```json
{
  "mcpServers": {
    "pux-sandbox": { "url": "http://127.0.0.1:9987" }
  }
}
```

The server speaks MCP protocol version `2025-03-26`. Sessions are tracked
via the `Mcp-Session-Id` header (generated on `initialize`).

## Tool surface (16 tools, all on the Go side)

| Tool | Schema source | Backed by |
|------|--------------|----------|
| `bash` | `tools/bash/bash.go` | adapters.BashExecutor → Docker exec |
| `file_read` / `file_write` / `file_edit` / `file_grep` / `file_glob` | `tools/file/file.go` | adapters.FileOps → Docker exec |
| `python` | `mcpserver/sandbox_python.go` | adapters.BashExecutor → `python3 -c` |
| `list_skills` / `load_skill` | `mcpserver/skills_tool.go` | skills package → host FS at `<project>/skills/` |
| `describe_image` | `mcpserver/vision_tool.go` | adapters.BashExecutor → `/usr/local/bin/describe_image.py` (local ONNX vision) |
| `browser_navigate` / `browser_click` / `browser_type` / `browser_screenshot` / `browser_evaluate` | `mcpserver/browser_tool.go` | adapters.BashExecutor → `curl` to in-sandbox `sb_server.py` |
| `desktop_screenshot` / `desktop_click` / `desktop_type` / `desktop_key` | `mcpserver/desktop_tool.go` | adapters.BashExecutor → `xdotool` + `/usr/local/bin/desktop_observe.py` |

All paths the tools report are **inside the sandbox container**. Your project
is bind-mounted at `/sandbox/workspace/`. pi-mono sees tools via
`pi-mcp-adapter` with `directTools: true` (each tool becomes a first-class
pi tool with a `pux_sandbox_*` prefix).

`/sandbox/` also contains read-only backbone scripts (`scripts.py`, etc.)
that ship with the sandbox image — the agent can invoke them but can't edit
them (`chmod 0444`).

## Agent layer (pi-mono)

The agent loop, sessions, history, TUI, and subagent delegation all live in
pi-mono + pi-subagents. Pux's contribution is the org loader extension
(44 LOC) + the AGENTS.md system prompt + the `.pi/agents/*.md` and
`.pi/skills/*/SKILL.md` files.

### Org mode (`--org <name>`)

When pux is launched with `--org <name>`, the pux-org-loader extension
appends `orgs/<name>/AGENTS.md` to the system prompt. You become the CTO of
that org — the body in that file carries the role.

Subagent delegation is pi-subagents' native `subagent` tool. Specialists
live under `.pi/agents/*.md` with rich frontmatter (`tools`, `model`,
`thinking`, `output`, `systemPromptMode`, `inheritSkills`, etc.). Spawn one
via `subagent({ agent: "researcher", task: "..." })` from the main session.

### Sessions & history

pi-mono writes sessions as JSON Lines at
`~/.pi/agent/sessions/<encoded-cwd>/<timestamp>_<uuid>.jsonl`. Each line is
one entry; the format is crash-safe (partial writes don't corrupt prior
turns). The tree is `id`/`parentId`-linked so `/fork` and `/branch` work.

Use pi's flags directly for navigation:

- `pux --resume` — interactive session picker (TUI)
- `pux --continue` — resume the most recent session in this cwd
- `pux --session <id>` — resume a specific session by partial UUID
- `pux --fork <id>` — fork a session into a new branch
- `/tree` inside the TUI — visualize the branching history

`pux history list` (in `bin/pux.mjs`) is a thin convenience wrapper that
just `ls`s the session directory and prints the most recent entries with
suggested resume commands.

### Compaction

When a session nears the model's context limit, pi-mono runs an LLM-generated
summary of older turns, preserving a recent verbatim window
(`keepRecentTokens`, default 20K tokens). Compaction never splits a turn
(user message + tool calls + tool results stay together). The raw
pre-compaction history stays in the .jsonl file — forking from an older
segment reconstructs the uncompacted state.

## Adding a new tool

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
opts that org into three independent layers; absence = today's behavior
(full egress, root-owning writes, no required creds). All three sections
optional — declare only what you need.

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
```

**Pipeline** (all in `backend/internal/policy/` + `sandbox/policy_hook.go`):

1. `pux --org X` → TS extension sets `PUX_ORG=X` in env
2. Go server reads `PUX_ORG`, calls `policy.Load(X, projectRoot)`
3. `ValidateEnv` checks required creds present → fail loud if missing
4. `ResolveMounts` expands `${VAR}` placeholders → fail loud if unset
5. Required + optional creds injected as `--env KEY=VALUE`
6. `RunAsHostUser` → `container.Config.User = "UID:GID"`
7. `egress.allow` non-empty → stages `<project>/.pux/egress.conf` +
   grants `NET_ADMIN` capability
8. Supervisor runs `apply-egress-policy.sh` at boot priority 15:
   `iptables -P OUTPUT DROP` + allowlist + always allow loopback/DNS/established

**Skipped for TierBridged** — host networking makes iptables-in-container
meaningless; operator explicitly chose host net for that sandbox.

**Verify gates** (baked into `task test`):

- `go test -race ./internal/policy/...` — 22 tests (placeholder expansion,
  missing-env errors, optional vs required, hostname resolution, port
  range, IPv4 + IPv6 literals, DNS failure)
- `task smoke` — boots real Docker, confirms no-policy path unchanged
- E2E: add `orgs/<name>/policy.yaml`, run `pux dispatch --org <name>`,
  confirm refused create when creds missing, allowed host succeeds,
  blocked host fails at network layer

## MCP transport contract

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

The Go contract is enforced by 60+ tests in `mcpserver/`:

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

The TS harness has 5 vitest tests covering pux-org-loader
(`.pi/extensions/pux-org-loader/index.test.ts`). Run with `npm test`.

**Verify gates before committing:**

- Go side: `task test` + `task smoke` (real Docker)
- TS side: `npm run typecheck` + `npm test`
- Boot check: `node bin/pux.mjs --help` (extension flags register) +
  `node bin/pux.mjs --list-models` (runtime boots with both extensions)

## Branch strategy

- **`pi-pivot`** = current branch. The pi-mono pivot. PRs here should keep
  both surfaces clean: minimal Go (sandbox + MCP), minimal TS (launcher +
  thin extensions on top of pi-mono).
- **`master`** = pre-pivot MVP. Slim Go MCP server with the in-process
  agent loop + history recorder + Bubble Tea TUI + dispatch surface. Frozen
  from the pi-pivot perspective.
- **`v0.2.0-pre-pi-mono`** = tag of master HEAD before the pivot. Safety
  net. `git show v0.2.0-pre-pi-mono:backend/internal/agent/loop.go` works.
- **`dev`** + **`v0.1.0-fullstack-legacy`** = the older fullstack predecessor
  (TUI, web UI, CLI, multi-agent). Frozen. Features migrated into master
  one at a time, then into pi-pivot as pi-mono dependencies.

## Testing harness rules

- "Should work" is banned. Verify with `task smoke` (real Docker) or a Go
  test that exercises the actual code path.
- Adding a tool → add a test in `mcpserver/server_test.go` covering its
  return shape (string vs map vs error).
- Changing the JSON-RPC envelope → update both `server.go` and the
  `server_test.go` table.
- Adding a TS extension → add a vitest test in
  `.pi/extensions/<name>/index.test.ts`.

## What's NOT here (deferred or dropped)

Dropped (pi-mono does it natively, or wasn't pulling weight):

- ~~In-process Go agent loop~~ — replaced by pi-mono's loop
- ~~Go dispatch surface (`dispatch_task` / `get_task_status` / `list_orgs`)~~ —
  replaced by `pux dispatch` + pi-mono's RPC mode
- ~~Go history recorder (sqlite sidecar + `pux-history` binary)~~ — replaced
  by pi-mono's .jsonl sessions + `pux history list` convenience wrapper
- ~~Bubble Tea TUI (`pux-tui`)~~ — replaced by pi-mono's TUI
- ~~TOML org config~~ — replaced by per-org `AGENTS.md` markdown

Deferred (might land later if a concrete need emerges):

- Multi-org orchestration (invest, twitter-agent, etc.) — current
  `.pi/agents/*.md` + `orgs/<name>/AGENTS.md` covers most cases
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Diligence evals, safeguard router
- Runtime MCP-server fallback URLs

## Conventions

- No co-authored-by Claude in git commits.
- Use astral uv for any Python environments (sandbox scripts, smoke test runner).
- Prefer 'prove' (integration-style) over 'assert' (unit-only) when feasible.
- "Verify or die" — no claiming a thing works without running it.
- IaC + self-bootstrap — every new service ships as docker-compose +
  bootstrap.sh, not manual `docker run`.
- No fallbacks, no deprecation aliases, no backwards-compat shims.

## Memory

Auto-memory lives at `~/.claude/projects/.../memory/`. The memory directory
tracks the strategic context — pivot rationale, fullstack lessons learned,
decisions deferred. Read `MEMORY.md` first when picking up context.
