# Pux MCP Server — Agent & Developer Guide

## What this is

Pux is an MCP (Model Context Protocol) server. It boots a Docker sandbox,
mounts your project, and exposes bash/file/python tools to any MCP-capable
LLM client over standard JSON-RPC.

**Scope:** single-tenant, localhost-only, no auth. One server = one project
= one sandbox. If you want to expose it beyond localhost, run a reverse
proxy (Caddy, Tailscale Funnel) in front.

The fullstack predecessor (TUI, web UI, CLI, multi-agent orchestration,
org system, skills system) lives on the `dev` branch. This branch
(`master`) is the slim MVP. The fullstack tag `v0.1.0-fullstack-legacy`
is a safety net.

## Quick start

```bash
task build              # Build the binary at backend/mcpserver
task run                # Foreground dev mode (signals propagate via exec)
task start              # Build + start in BACKGROUND (daemonize, returns immediately)
task stop               # SIGTERM the backgrounded server
task status             # Show running state (PID, addr, sandbox, uptime)
task smoke              # Build + start + smoke test + stop (exercises supervisor)
task test               # Go unit tests
task hooks              # Install pre-commit (gitleaks)
```

The server takes `--addr` (default `127.0.0.1:9987`) and `--project` (defaults
to `$PWD`). Env vars `PUX_MCP_ADDR` / `PUX_PROJECT_PATH` mirror them. The
`run` task honors `PUX_MCP_ADDR` — e.g. `PUX_MCP_ADDR=127.0.0.1:9999 task run`.

**Why 9987 (not 9876):** the sandbox's `sb_server.py` (browser-mode HTTP API)
listens on 9876 *inside* the container. Any org sandbox that boots with
`--network=host` (invest, deep-research-engine, etc.) leaks that listener to
the host's 9876. Defaulting pux-mcpserver to 9987 avoids the collision
out-of-the-box. Forcing `--addr 127.0.0.1:9876` while a host-networked org
sandbox is running will still fail with "address already in use" — pick
another port or stop the conflicting container.

## Lifecycle (`run` / `start` / `stop` / `status`)

The binary ships with four subcommands. `mcpserver` with no subcommand (or
with `--flags` only) defaults to `run` — that's the back-compat path, so
existing scripts and the `task smoke` of older revisions keep working.

| Subcommand | When to use |
|-----------|--------------|
| `run` | **Dev/foreground.** Boots in the terminal, sandbox + HTTP listener live until SIGINT/SIGINT. The `run` task uses `exec` so signals from a parent shell propagate directly (no orphan bash wrapper). |
| `start` | **Background/CI.** Daemonizes via `setsid` + `cmd.Process.Release()`, writes a PID file at `<project>/.pux/mcpserver.pid`, returns immediately. Refuses if a server is already running for that project unless `--force` stops it first. Logs default to discarded — pass `--log <path>` or set `PUX_MCP_LOG` to capture. |
| `stop` | Reads the PID file, SIGTERMs the server, polls up to `--wait` (default 10s), SIGKILLs if still alive. Removes the PID file. |
| `status` | Reports PID + addr + project + sandbox + container_id + uptime. `--live` adds an HTTP ping to verify the server actually responds (vs just "process exists"). |

**Stale-PID recovery:** if `start` finds a PID file but the process is dead
(crash, SIGKILL, OOM), it cleans up and proceeds. The PID file lingering
after a crash is the explicit signal — the next `start` will treat it as
stale rather than refuse boot.

**Single-tenant per project:** the PID file lives at `<project>/.pux/mcpserver.pid`
(one server per project). Override via `PUX_PID_FILE` for unusual layouts.

Set `PUX_AUDIT_LOG=/path/to/audit.jsonl` to append every tool call (args +
result + duration, secret-scrubbed) to a forensic log. Opt-in; default off.

Set `PUX_LLM_API_KEY` to enable the **dispatch surface** (the
`dispatch_task` / `get_task_status` / `list_orgs` MCP tools). When set, the
server-side agent loop runs against Anthropic's Messages API — an external
caller (Hermes / OpenClaw / Claude Code) can hand a task to a configured
"org" and the org's CTO does the work. Optional knobs: `PUX_LLM_BASE_URL`,
`PUX_LLM_MODEL` (default `claude-sonnet-4-6`), `PUX_LLM_MAX_TOKENS`. Absent
key = surface disabled, the other 19 tools still work.

Set `PUX_HISTORY_DIR=/path/to/dir` to enable the **history sidecar** —
durable sqlite recording of dispatch-surface activity (task lifecycle,
agent-loop assistant messages, in-loop tool calls). When set, the server
opens `<dir>/history.sqlite` at boot and writes one row per observer event.
Read path is host-side only via the `pux-history` binary
(`pux-history list / show / search`). No MCP tool exposes the read path.
Fully deletable: drop the env var and the recorder is never constructed.

## Connecting MCP clients

Add to Claude Desktop (or any MCP client that supports HTTP transport):

```json
{
  "mcpServers": {
    "pux": { "url": "http://127.0.0.1:9987" }
  }
}
```

The server speaks MCP protocol version `2025-03-26`. Sessions are tracked
via the `Mcp-Session-Id` header (generated on `initialize`).

## Tool surface

| Tool | Schema source | Backed by |
|------|--------------|----------|
| `bash` | `tools/bash/bash.go` | adapters.BashExecutor → Docker exec |
| `file_read` / `file_write` / `file_edit` / `file_grep` / `file_glob` | `tools/file/file.go` | adapters.FileOps → Docker exec |
| `python` | `mcpserver/sandbox_python.go` | adapters.BashExecutor → `python3 -c` |
| `list_skills` / `load_skill` | `mcpserver/skills_tool.go` | skills package → host FS at `<project>/skills/` |
| `describe_image` | `mcpserver/vision_tool.go` | adapters.BashExecutor → `/usr/local/bin/describe_image.py` (local ONNX vision) |
| `browser_navigate` / `browser_click` / `browser_type` / `browser_screenshot` / `browser_evaluate` | `mcpserver/browser_tool.go` | adapters.BashExecutor → `curl` to in-sandbox `sb_server.py` (persistent SeleniumBase Chrome) |
| `desktop_screenshot` / `desktop_click` / `desktop_type` / `desktop_key` | `mcpserver/desktop_tool.go` | adapters.BashExecutor → `xdotool` + `/usr/local/bin/desktop_observe.py` (Xvfb DISPLAY=:99) |
| `dispatch_task` / `get_task_status` / `list_orgs` | `mcpserver/dispatch_tool.go` | agent.Loop → Anthropic Messages API + same sandbox tools as above (opt-in via `PUX_LLM_API_KEY`) |

All file paths are **inside the sandbox container**. Your project is mounted
at `/sandbox/workspace/`. The model sees that path; there is no host path
translation in the contract.

`/sandbox/` also contains read-only backbone scripts (`scripts.py`, etc.)
that ship with the sandbox image — the model can invoke them but can't edit
them (`chmod 0444`).

## Architecture (single-tenant)

```
┌─────────────────────────────────────┐
│ MCP Client (Claude / Hermes / ...)  │
└──────────────┬──────────────────────┘
               │ JSON-RPC 2.0 over HTTP
┌──────────────▼──────────────────────┐
│ pux-mcpserver (Go, localhost:9987)   │
│  - mcpserver.Server (tool registry)  │
│  - http.Handler (JSON-RPC dispatch)  │
└──────────────┬──────────────────────┘
               │ adapters.BashExecutor / FileOps
┌──────────────▼──────────────────────┘
│ sandbox.Manager → Docker exec       │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│ pux-sandbox container                │
│  - /workspace bind-mount             │
│  - /sandbox/scripts.py (read-only)   │
└─────────────────────────────────────┘
```

## Repo layout

```
auto-developer-orchestrator/
├── backend/
│   ├── cmd/mcpserver/main.go      # Entry point
│   ├── cmd/pux-history/main.go    # History CLI (list/show/search the sqlite db)
│   ├── cmd/pux-tui/main.go        # Bubble Tea conversational TUI (chat with an org)
│   └── internal/
│       ├── adapters/              # BashExecutor, FileOps (sandbox bridge)
│       ├── agent/                 # AnthropicProvider + Loop + DelegateTool
│       ├── audit/                 # JSONL audit log (opt-in via PUX_AUDIT_LOG)
│       ├── core/                  # core.Tool, LLMProvider, ChatEvent, ToolError, observers
│       ├── history/               # sqlite history sidecar (opt-in via PUX_HISTORY_DIR)
│       │   ├── recorder.go        # implements TaskObserver + ChatObserver + ToolObserver
│       │   ├── store.go           # sqlite open + write ops
│       │   ├── query.go           # read API for cmd/pux-history + internal/tui
│       │   └── schema.sql         # tasks / messages / tool_calls + indexes
│       ├── mcpserver/             # MCP server + tool registry
│       │   ├── server.go          # JSON-RPC dispatch + tool registry
│       │   ├── transport.go       # HTTP handler
│       │   ├── session.go         # Session ID generator
│       │   ├── sandbox_python.go  # python tool (sandbox-aware)
│       │   ├── skills_tool.go     # list_skills / load_skill
│       │   ├── vision_tool.go     # describe_image (local ONNX vision)
│       │   ├── browser_tool.go    # browser_* tools wrapping in-sandbox sb_server.py
│       │   ├── desktop_tool.go    # desktop_* tools wrapping xdotool + desktop_observe.py
│       │   ├── dispatch_tool.go   # dispatch_task / get_task_status / list_orgs
│       │   ├── task_store.go      # in-memory task registry for dispatch
│       │   └── shell.go           # shared shQ shell-escape helper
│       ├── org/                   # Org + TOML loader for orgs/<name>/org.toml
│       ├── retry/                 # Provider retry
│       ├── sandbox/               # Docker sandbox lifecycle
│       ├── sensitive/             # Secret scrubbing
│       ├── skills/                # list_skills / load_skill package
│       ├── tui/                   # Bubble Tea conversational interface sidecar
│       │   ├── model.go           # top-level tea.Model + Update/View
│       │   ├── client.go          # MCP HTTP client (initialize + dispatch + status)
│       │   ├── conversation.go    # client-side turn accumulation + renderConversation
│       │   ├── history.go         # optional history pane (nil-safe via os.Stat probe)
│       │   └── styles.go          # lipgloss styles
│       └── tools/
│           ├── bash/              # Bash tool (Validator-based deny list)
│           ├── file/              # File tools (read/write/edit/grep/glob)
│           └── truncate/          # Output truncation
├── orgs/                          # org templates (shipped: _demo/)
│   └── _demo/                     #   example org: cto + researcher
├── sandbox/                       # Dockerfile + scripts for pux-sandbox:latest
├── scripts/
│   ├── setup-hooks.sh             # Pre-commit install
│   └── smoke_mcp.py               # End-to-end smoke test
├── .github/workflows/ci.yml       # go-test + go-lint + secret-scan
├── Taskfile.yml                   # build / run / test / smoke / hooks
└── VERSION                        # 0.1.0-mvp
```

Packages without a "(transitive)" marker are load-bearing — every directory
above is reachable from `cmd/mcpserver/main.go`. The fullstack predecessor
(hooks, checkpoint, context, session, storage, perms, mcp client, the old
core.Loop / TaskManager / SSE event pipeline) lives on the `dev` branch and
the `v0.1.0-fullstack-legacy` tag if any of it is needed again.

## Key code paths

| What | Where |
|------|-------|
| Wire format (JSON-RPC envelope) | `mcpserver/server.go` |
| HTTP transport | `mcpserver/transport.go` |
| Tool → MCP result shape | `mcpserver/server.go::handleToolsCall` (returns `{content: [{type:"text", text:...}], isError:bool}`) |
| Tool registration | `cmd/mcpserver/main.go::main` |
| Sandbox boot + teardown | `cmd/mcpserver/main.go::main` (signal-gated) |
| Sandbox lifecycle (Docker) | `sandbox/manager.go` |

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
   The spec carries name + description + schema + the per-tool logic
   (buildBody for browser, build+result for desktop). No new type, no new
   constructor — the dispatcher (`BrowserTool` / `DesktopTool`) handles
   registration via the spec slice.
2. The family's `RegisterXXXTools(srv, exec, cfg)` helper in the same file
   picks up the new spec automatically. No main.go change.
3. Add a test in the family's `_test.go` file using the spec-lookup helper
   (`newBrowserTool("browser_xxx", fake)` or `newDesktopTool(...)`).

There's no codegen, no manifest. The spec slice (for families) + the
registry (for standalone tools) are the source of truth.

## Dispatch surface (org → agent loop, opt-in)

When `PUX_LLM_API_KEY` is set at server boot, three additional MCP tools
are registered:

| Tool | Shape |
|------|-------|
| `list_orgs` | `{}` → `{orgs: [{name, description, roles}], count}` |
| `dispatch_task` | `{org_name, task_description}` → `{task_id, status:"pending"}` |
| `get_task_status` | `{task_id}` → `{status, result, error, round, transcript_tail, started_at, finished_at}` |

`dispatch_task` is async — it returns a `task_id` immediately and the
server runs the org's CTO loop in a background goroutine. Poll
`get_task_status` for completion (status moves `pending → running →
complete | failed`).

### Org layout

An org is a directory under `<project>/orgs/<name>/` containing an
`org.toml` + markdown prompt bodies:

```
orgs/<name>/
├── org.toml           # config (name, description, [cto], [[roles]])
├── cto.md             # CTO system prompt body
└── roles/<role>.md    # one per [[roles]] entry
```

See `orgs/_demo/` for the shipped example. Each `[cto]` / `[[roles]]`
block declares its own `prompt` (relative path), `max_rounds`, and
`tools` (whitelist of MCP tool names). Roles automatically inherit
recursion protection — `delegate_to` is forbidden in role whitelists
(the validator rejects it loudly at dispatch time).

### How the loop works

The CTO loop runs in the host process (not the sandbox). It calls the
Anthropic Messages API directly, dispatches `tool_use` blocks back
through the SAME registered MCP tools (so an agent loop calling `bash`
hits the exact same code path as an external MCP client calling `bash`),
and on `delegate_to` spawns a child loop with the role's prompt + filtered
tool whitelist. CTO receives the child's final response as the tool
result; the child's tool chatter is invisible to the CTO.

Per-org serialization: one task per org at a time (filesystem races).
Cross-org dispatches run concurrently. The dispatch goroutine holds a
per-org mutex for its duration; `dispatch_task` itself returns immediately.

### Wiring

`cmd/mcpserver/dispatch.go` contains the runtime: the `Dispatcher`
impl, per-org mutex map, role lookup, tool catalog → ToolExecutor
adapter, and the `OrgLister` impl. `main.go` constructs it when
`PUX_LLM_API_KEY` is set and registers the three MCP tools. SIGTERM
calls `TaskStore.Shutdown()` which cancels in-flight tasks (their
status moves to `failed: "server shutdown"`).

## History (sqlite sidecar, opt-in)

When `PUX_HISTORY_DIR` is set at server boot, the dispatch surface fires
observer events into a sqlite database at `<dir>/history.sqlite`. Three
observer interfaces (`internal/core/observer.go`) form the seam:

| Observer | Fired by | Records |
|----------|----------|---------|
| `TaskObserver` | `mcpserver/task_store.go` | task lifecycle: pending → running → complete / failed |
| `ChatObserver` | `agent/loop.go::Run` | one row per non-empty assistant turn |
| `ToolObserver` | `agent/loop.go::dispatchTools` | one row per in-loop tool dispatch |

All three are nil-safe — call sites check `!= nil` before firing. The
`history.Recorder` (in `internal/history/recorder.go`) implements all
three. CTO + delegated child loops inherit the same observers via
`DelegateTool`, so a delegation chain lands under one task ID.

**Read path** is host-side only — no MCP tool exposes it. The
`pux-history` binary (built via `go build ./cmd/pux-history`) reads the
same sqlite file:

```
pux-history list [--org NAME] [--limit N]    # most-recent tasks
pux-history show <task-id>                     # full transcript + tool calls
pux-history search <regex> [--org NAME]        # across messages + tool calls + task bodies
```

Default `--limit 50`. `PUX_HISTORY_DIR` selects the file (same path the
server writes to). Output is plain text, not JSON — operator tool, not API.

**Deletion-proof by contract.** `rm -rf internal/history/ cmd/pux-history/`
+ drop the 8 wiring lines in `cmd/mcpserver/main.go` + the 2 fields in
`dispatch.go` + the TaskStore observer plumbing. Server still builds, still
ships 19 MCP tools, zero history overhead. The seam is the contract.

**Scope:** task lifecycle + assistant messages + in-loop tool calls.
Scrubbed via `internal/sensitive.ScrubText` (same patterns as the audit
log). NOT recorded: system prompts, reasoning/thinking content, MCP-direct
tool calls (those go to the audit log if enabled), token usage. Each is a
small follow-up at the same seam.

## TUI (Bubble Tea conversational interface, opt-in)

`pux-tui` is the operator's chat window into an org. It's a Bubble Tea
program that points at a running pux-mcpserver over HTTP (same wire
contract as Claude Desktop) and lets the operator type messages to an
org's CTO. Use case: with a `game-maker-org`, the operator types "I want
a 2D platformer" → CTO responds → "use pixel art" → CTO responds again,
with full multi-turn context.

```bash
go run ./cmd/pux-tui --mcp-addr http://127.0.0.1:9987 --org _demo
```

**Conversation state lives client-side.** The dispatch surface is
intentionally stateless per-task — each Enter dispatches the full
accumulated conversation as a single `task_description` (markdown: one
`**User:**` / `**Assistant:**` block per turn). This is the only way to
get multi-turn chat against the slim MVP server. The CTO sees the whole
conversation verbatim on every turn.

**History pane** (`Ctrl+H`): toggleable list of recent tasks. If
`PUX_HISTORY_DIR` is set on the server AND `<dir>/history.sqlite` exists,
the pane lists tasks (via `history.Query.ListTasks`); arrow keys move,
`Enter` expands one to show its transcript. If history is unavailable,
the hint is hidden and `Ctrl+H` is a no-op. The pane is probed via
`os.Stat` (not `OpenQuery`, which would create the file as a side effect).

**Deletion-proof by contract.** `rm -rf internal/tui/ cmd/pux-tui/` →
MCP server still builds, still ships 19 MCP tools, dispatch surface
intact. **No wiring in `cmd/mcpserver/main.go`** — the TUI is a separate
binary that drives the server over HTTP, exactly like Claude Desktop.

**Import prohibition** (enforced by the package's own doc comment): the
TUI package imports only stdlib + `charmbracelet/{bubbletea,lipgloss,glamour}`
+ `internal/history` (for the optional pane). It MUST NOT import `agent/`,
`mcpserver/`, `sandbox/`, `org/`, `audit/`, `sensitive/`, `adapters/`,
`tools/`, or `core/`. Reach for any of those → the contract is broken.

## Vision (local ONNX, opt-in)

## Vision (local ONNX, opt-in)

`describe_image` runs local vision inference inside the sandbox via
Qwen3.5-2B-ONNX-OPT fp16. **No external MCP dependency** — the model
weights are operator-supplied, downloaded once via host-side script.

**Bootstrap:**
```bash
scripts/bootstrap-vision.sh                 # downloads to $PWD/.pux/models/
scripts/bootstrap-vision.sh --project DIR   # explicit project root
scripts/bootstrap-vision.sh --check         # exit 0 if ready, 1 if not
```

The script downloads ~5GB of fp16 weights, applies the known `patch_size: 16`
bug fix to `genai_config.json`, and verifies file integrity. Idempotent —
safe to re-run; uses HF resume support.

**Contract:** when the model is absent, `describe_image` returns
`{success:false, reason:"unavailable"}` — NOT a Go error, NOT an
`isError:true` envelope. The driving agent falls back to text-only
reasoning without breaking its loop. Operators reading transcripts see
the "run bootstrap-vision.sh" hint inside the result body.

**Three pieces:**
- `scripts/bootstrap-vision.sh` — host-side downloader (idempotent)
- `sandbox/scripts/describe_image.py` — backbone script (shipped in
  container at `/usr/local/bin/describe_image.py`)
- `backend/internal/mcpserver/vision_tool.go` — MCP tool wrapper

Model location (bind-mounted into sandbox):
`<project>/.pux/models/Qwen3.5-2B-ONNX-OPT/` → `/sandbox/workspace/.pux/models/Qwen3.5-2B-ONNX-OPT/`

## Browser (in-sandbox sb_server.py)

Five MCP tools wrap the sandbox's existing `sb_server.py` (persistent
SeleniumBase HTTP API on `127.0.0.1:9876` inside the container). The
MCP tools shell out to `curl` against that API; the Chrome session
persists across calls.

| Tool | Endpoint | Field contract |
|------|----------|---------------|
| `browser_navigate` | `/navigate` | `{url}` |
| `browser_click` | `/click` | `{index}` or `{selector}` (mutually exclusive) |
| `browser_type` | `/type` | `{text}` + (`{index}` or `{selector}`) |
| `browser_screenshot` | `/read` | `{}` (no args) — returns page + SoM labels + screenshot path |
| `browser_evaluate` | `/evaluate` | `{code}` — JavaScript expression, use `return` for explicit values |

**Set-of-Marks labels:** `/navigate` and `/read` return an `element_map`
of interactive elements with bounding boxes + an integer `index`. Pass
that integer to `browser_click(index=N)` or `browser_type(index=N,
text=...)` — the sb_server resolves the index to the current selector
at call time, robust against reflows.

**Errors propagate as Go errors** (no graceful-degradation path — the
tools need `sb_server` up). Timeouts, malformed responses, exec
failures all return `isError:true` with the endpoint name in the
message.

## Desktop (Xvfb DISPLAY=:99, xdotool + OCR)

Four MCP tools wrap the sandbox's X11 desktop. The sandbox already
boots Xvfb + fluxbox + xdotool + scrot + tesseract (supervisord-managed,
auto-enabled alongside browser mode). The tools drive arbitrary desktop
apps via pixel coordinates.

| Tool | Field contract |
|------|---------------|
| `desktop_screenshot` | `{}` — returns `{image_b64, elements[], windows[], resolution, ocr_available}` |
| `desktop_click` | `{x, y, button?}` — button default 1 (left); 2=middle, 3=right |
| `desktop_type` | `{text, clear?}` — clear default true (Ctrl+A + Delete first) |
| `desktop_key` | `{keys}` — xdotool key combo like `Return`, `ctrl+c`, `alt+Tab` |

**Pixel coordinates are the contract.** OCR text positions drift across
runs, so we click by `(x, y)` from the latest `desktop_screenshot`'s
`element.cx, element.cy`. The model picks the coord from the elements
list or the visible image.

**Errors propagate as Go errors** (same as browser tools — no graceful
degradation; the desktop tools need Xvfb up). Failures surface with the
operation name (`desktop click`, `desktop type`, etc.) in the message.

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

The contract is enforced by 70+ Go tests in `mcpserver/`:

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
  (float/int/string) + button validation, type shell-escaping + clear
  flag, key combo building
- **History sidecar** (8 tests): full task lifecycle (pending → running →
  complete/failed), assistant-message round ordering, tool-call fields +
  error path, regex search across task/message/tool sources, org filter,
  secret scrubbing, idempotent Close

Plus a real end-to-end smoke test (`task smoke`) that boots against a live
Docker container and exercises every tool. **Run `task smoke` before
committing tool registry changes** — it's the only test that catches
real-Docker bugs.

## Branch strategy

- **`master`** = this MVP. PRs here should keep the surface small and the
  contract clean.
- **`dev`** = fullstack branch (TUI, web, CLI, orgs, multi-agent). Frozen
  from this MVP's perspective — features migrate over one at a time when
  they fit cleanly.
- **`v0.1.0-fullstack-legacy`** = tag of fullstack HEAD before the pivot.
  Safety net.

## Testing harness rules

- "Should work" is banned. Verify with `task smoke` (real Docker) or a Go
  test that exercises the actual code path.
- Adding a tool → add a test in `mcpserver/server_test.go` covering its
  return shape (string vs map vs error).
- Changing the JSON-RPC envelope → update both `server.go` and the
  `server_test.go` table.

## What's NOT here (deferred to dev branch)

- Multi-agent orchestration (CTO + employee delegation loop)
- Org system (per-domain config overlays — invest, twitter-agent, etc.)
- ~~TUI (Ink), web UI (Vite)~~ — TUI shipped (Bubble Tea); see `pux-tui` above
- CLI (Cobra) — `pux-history` + `pux-tui` cover the operator surface
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Diligence evals, safeguard router
- Runtime MCP-server fallback URLs (planned — see plan file)

Each will migrate back to master one feature at a time, after it's proven
to fit the MCP contract cleanly.

## Conventions

- No co-authored-by Claude in git commits.
- Use astral uv for any Python environments (sandbox scripts, smoke test runner).
- Prefer 'prove' (integration-style) over 'assert' (unit-only) when feasible.
- "Verify or die" — no claiming a thing works without running it.
- IaC + self-bootstrap — every new service ships as docker-compose +
  bootstrap.sh, not manual `docker run`. (For this MVP, the sandbox image
  is the only infrastructure; build via `sandbox/Dockerfile`.)

## Memory

Auto-memory lives at `~/.claude/projects/.../memory/`. The memory directory
tracks the strategic context — pivot rationale, fullstack lessons learned,
decisions deferred to dev. Read `MEMORY.md` first when picking up context.
