# Pux MCP Server — Contract Specification

> **FROZEN `master`-branch reference.** This document specifies the slim Go MCP
> server that lives on `master` (the pre-pivot MVP). It does **not** describe
> the `pi-pivot` branch, where that Go server is deleted (Phase 8i) and the
> agent + sandbox layer is the Python deepagents harness — see `CLAUDE.md` and
> `harness/README.md` for the current surface. Kept here as the frozen contract
> for `master`.
>
> **Live MCP server (pi-pivot):** the surface Hermes/OpenClaw connect to is the
> Python FastMCP wrapper at `harness/pux_harness/mcp_server.py` (`pux mcp`, SSE
> :9987), not this Go spec. A full rewrite of this document to match it is
> deferred to a separate PR.

> Source of truth for the slim MVP on `master`. Everything in this codebase
> conforms to this document or it's a bug. The fullstack contract (TUI, web
> UI, CLI, sub-agents, organizations, skills) lives on the `dev` branch with
> its own contract — this document does not describe that surface.

## Philosophy

Pux on `master` is one thing: an MCP server. It speaks standard Model
Context Protocol to any MCP-capable client (Claude Desktop, Hermes, OpenClaw,
continue.dev). It does not render a UI, schedule prompts, or own
persistence beyond the sandbox. It can optionally run a server-side agent
loop (the **dispatch surface**, see Contract 8) when `PUX_LLM_API_KEY` is
set — but the wire format stays standard MCP.

The contract is the wire format. The kernel (this server) produces and
consumes MCP JSON-RPC. The sandbox executes commands. The boundary is the
only thing that matters.

## Architecture

```
┌─────────────────────────────────────────┐
│ MCP Client (Claude / Hermes / OpenClaw) │
└──────────────────┬──────────────────────┘
                   │ JSON-RPC 2.0 over HTTP
                   │ (Mcp-Session-Id header)
┌──────────────────▼──────────────────────┐
│ pux-mcpserver (Go, localhost:9987)       │
│  - mcpserver.Server (tool registry)      │
│  - http.Handler (JSON-RPC dispatch)      │
└──────────────────┬──────────────────────┘
                   │ adapters.BashExecutor / FileOps
┌──────────────────▼──────────────────────┐
│ sandbox.Manager → Docker exec            │
└──────────────────┬──────────────────────┘
                   │
┌──────────────────▼──────────────────────┐
│ pux-sandbox container                    │
│  - /workspace bind-mount (your project)  │
│  - /sandbox/scripts.py (read-only)       │
└─────────────────────────────────────────┘
```

**Scope:** single-tenant, localhost-only, no auth. One server = one project
= one sandbox. If you want to expose it beyond localhost, run a reverse
proxy (Caddy, Tailscale Funnel) in front and add auth there.

---

## Contract 1: Transport

| Property | Value |
|----------|-------|
| Wire format | JSON-RPC 2.0 |
| Transport | HTTP POST (Streamable HTTP, MCP variant) |
| Endpoint | Single URL (default `http://127.0.0.1:9987`) |
| Session header | `Mcp-Session-Id` (generated on `initialize`) |
| Protocol version | `2025-03-26` |
| Max request body | 32 MiB |

CORS is `Access-Control-Allow-Origin: *` since this is localhost-only.
`OPTIONS` returns `204`; `GET` and `DELETE` are reserved (405 and 204
respectively).

---

## Contract 2: Methods

| Method | Request | Response | Notes |
|--------|---------|----------|-------|
| `initialize` | `{protocolVersion, clientInfo, capabilities}` | `{protocolVersion, serverInfo, capabilities}` | Generates `Mcp-Session-Id` header. |
| `notifications/initialized` | `{}` | (none — fire-and-forget) | Client signals ready. |
| `tools/list` | `{}` | `{tools: Tool[]}` | Static registry — same answer every session. |
| `tools/call` | `{name, arguments}` | `McpToolResult` (see Contract 4) | Errors return `isError: true` in the result envelope, NOT as JSON-RPC errors. |
| `ping` | `{}` | `{}` | Liveness probe. |
| (batch) | `[{...}, {...}]` | — | Rejected with `-32600 Invalid request`. |
| (unknown method) | any | — | `-32601 Method not found`. |
| (parse error) | malformed JSON | — | `-32700 Parse error`. |

Standard JSON-RPC error codes are used for protocol-level failures. Tool
execution failures use the `isError` flag in the result envelope so the
client can surface them as model-visible results (and let the model reason
about the failure) rather than transport failures.

---

## Contract 3: Tools

The tool registry is the source of truth. `tools/list` reflects exactly
what's registered in `cmd/mcpserver/main.go::main` at boot.

| Tool | Schema source | Backed by | Notes |
|------|--------------|----------|-------|
| `bash` | `tools/bash/bash.go` | `adapters.BashExecutor` → Docker exec | Single-quote shell-escaped. |
| `file_read` | `tools/file/file.go` | `adapters.FileOps` → Docker exec | Line numbers in output. |
| `file_write` | `tools/file/file.go` | `adapters.FileOps` → Docker exec | Overwrite-only (no append). |
| `file_edit` | `tools/file/file.go` | `adapters.FileOps` → Docker exec | sed-style find/replace. |
| `file_grep` | `tools/file/file.go` | `adapters.FileOps` → Docker exec | ripgrep primary, grep fallback. |
| `file_glob` | `tools/file/file.go` | `adapters.FileOps` → Docker exec | Standard glob syntax. |
| `python` | `mcpserver/sandbox_python.go` | `adapters.BashExecutor` → `python3 -c` | Sandbox-installed deps available. |
| `list_skills` | `mcpserver/skills_tool.go` | `skills.Discover` → host FS at `<project>/skills/` | Returns metadata only (no body). |
| `load_skill` | `mcpserver/skills_tool.go` | `skills.Load` → host FS at `<project>/skills/<name>/SKILL.md` | Returns full markdown body. |
| `describe_image` | `mcpserver/vision_tool.go` | `adapters.BashExecutor` → `/usr/local/bin/describe_image.py` | Local ONNX vision (Qwen3.5-2B-ONNX-OPT fp16). Graceful degradation — missing model returns `success:false, reason:"unavailable"`, not an error. |
| `browser_navigate` | `mcpserver/browser_tool.go` | `adapters.BashExecutor` → `curl POST /navigate` to in-sandbox `sb_server.py` | Opens URL in persistent Chrome. Returns title/url/text + SoM-labeled element map + screenshot path. |
| `browser_click` | `mcpserver/browser_tool.go` | `adapters.BashExecutor` → `curl POST /click` to `sb_server.py` | Click by SoM `index` (integer) or CSS `selector`. Returns post-click page state. |
| `browser_type` | `mcpserver/browser_tool.go` | `adapters.BashExecutor` → `curl POST /type` to `sb_server.py` | CDP character-by-character typing (React-safe). Requires `text` + (`index` or `selector`). |
| `browser_screenshot` | `mcpserver/browser_tool.go` | `adapters.BashExecutor` → `curl POST /read` to `sb_server.py` | Fresh page state + SoM labels + screenshot path. Free — doesn't navigate. |
| `browser_evaluate` | `mcpserver/browser_tool.go` | `adapters.BashExecutor` → `curl POST /evaluate` to `sb_server.py` | Power-tool escape hatch — runs `code` (JavaScript) in the page context. Returns `{result, type}`. |
| `desktop_screenshot` | `mcpserver/desktop_tool.go` | `adapters.BashExecutor` → `/usr/local/bin/desktop_observe.py` (X11 + tesseract) | Captures DISPLAY=:99 as base64 PNG + OCR text elements (with cx/cy center coords) + window list + resolution. |
| `desktop_click` | `mcpserver/desktop_tool.go` | `adapters.BashExecutor` → `xdotool mousemove + click` | Clicks at pixel `(x, y)` (pick from desktop_screenshot's element.cx/cy). Optional `button`: 1=left (default), 2=middle, 3=right. |
| `desktop_type` | `mcpserver/desktop_tool.go` | `adapters.BashExecutor` → `xdotool type` | Types text into focused window. Real X11 key events (works in any app). Optional `clear=true` (default) Ctrl+A + Delete first. |
| `desktop_key` | `mcpserver/desktop_tool.go` | `adapters.BashExecutor` → `xdotool key` | Presses a key combo (`Return`, `ctrl+c`, `alt+Tab`, `Escape`, `super`). For text use desktop_type. |
| `dispatch_task` | `mcpserver/dispatch_tool.go` | `agent.Loop` → Anthropic Messages API + same sandbox tools | **Opt-in via `PUX_LLM_API_KEY`.** Async — returns `{task_id, status:"pending"}` immediately, the org's CTO loop runs in a goroutine. |
| `get_task_status` | `mcpserver/dispatch_tool.go` | `mcpserver.TaskStore` lookup | **Opt-in.** Polls `{task_id}` → `{status, result, error, round, transcript_tail, started_at, finished_at}`. |
| `list_orgs` | `mcpserver/dispatch_tool.go` | `org.Loader.LoadAll` | **Opt-in.** Returns `{orgs: [{name, description, roles}], count}`. |

All file paths are **inside the sandbox container**. The project is
bind-mounted at `/sandbox/workspace/`. The model sees that path verbatim;
there is no host path translation in the contract.

`/sandbox/` also contains read-only backbone scripts (`scripts.py`, etc.)
that ship with the sandbox image — the model can invoke them but cannot
edit them (`chmod 0444`).

### Skills (host-side backbone context)

Skills are operator-authored markdown files at `<project>/skills/<name>/SKILL.md`.
Each carries YAML frontmatter (`name`, `description`) and a markdown body.
The model discovers them via `list_skills` (cheap — metadata only) and
reads a specific body via `load_skill(name)`. This is the progressive-
disclosure pattern: list first, load on demand.

Skills are NOT model-mutable through this surface — they're host-side
backbone context, distinct from in-sandbox artifacts. The model can still
edit them via `file_write` (they're bind-mounted at
`/sandbox/workspace/skills/`), but that's an operator concern, not the
skill tools' job.

### Vision (opt-in, graceful degradation)

`describe_image` runs local vision inference via Qwen3.5-2B-ONNX-OPT fp16
loaded inside the sandbox. No external MCP dependency — the model weights
are an OPTIONAL operator-supplied artifact.

The model directory lives at `<project>/.pux/models/Qwen3.5-2B-ONNX-OPT/`
and is bind-mounted into the sandbox at
`/sandbox/workspace/.pux/models/...`. Operators download it via
`scripts/bootstrap-vision.sh` from the host.

**Contract:** when the model is absent, `describe_image` returns a
structured result with `success:false` and `reason:"unavailable"` — NOT a
Go-level error and NOT an `isError:true` envelope. The driving agent must
be able to fall back to text-only reasoning without its loop breaking. The
friendly "run bootstrap-vision.sh" message lives inside the result body so
the operator (reading transcripts) can see it.

The same applies to the `deps_missing` reason: if the sandbox image is
somehow missing `onnxruntime-genai`, the tool reports the state
gracefully rather than crashing.

Genuine failures (corrupt image, ONNX runtime crash, OOM) DO surface as
`success:false, reason:"inference_failed"`. These are observable but
non-fatal — the calling client can retry or skip.

### Browser (in-sandbox sb_server, persistent Chrome)

The five `browser_*` tools wrap the sandbox's existing `sb_server.py` —
a persistent HTTP API in front of SeleniumBase/Chrome. The server runs
INSIDE the sandbox container on `127.0.0.1:9876` (supervisord-managed);
the MCP tools shell out to `curl` against it. The browser session
persists across calls — `browser_navigate` opens the page, then any
subsequent `browser_click` / `browser_type` / `browser_screenshot` /
`browser_evaluate` operates on that page until you navigate again.

**Set-of-Marks labels.** `/navigate` and `/read` return an `element_map`
of clickable/typable elements with bounding boxes + an integer `index`.
Pass that integer to `browser_click(index=N)` or
`browser_type(index=N, text=...)` — the sb_server resolves the index to
the current selector at call time (more robust than CSS selectors
across reflows).

**Field-name contract** (matches the sb_server's HTTP API exactly):
- `browser_navigate`: `{url}`
- `browser_click`: `{index}` or `{selector}` (mutually exclusive)
- `browser_type`: `{text}` + (`{index}` or `{selector}`)
- `browser_screenshot`: `{}` (no args)
- `browser_evaluate`: `{code}` — JavaScript expression; use `return` for
  explicit values. Result shape: `{ok, result, type}`.

**Errors propagate as Go errors.** Unlike `describe_image` (which
degrades gracefully when the model is missing), the browser tools have
NO graceful-degradation path — they need `sb_server` up. Failures
(timeout, malformed response, exec failure) return `isError: true` with
the endpoint name in the message so the failing call is obvious in
transcripts.

### Desktop (Xvfb DISPLAY=:99, xdotool + OCR)

Four MCP tools wrap the sandbox's X11 desktop. The sandbox already
boots Xvfb at DISPLAY=:99 + fluxbox wm + xdotool + scrot + tesseract
via supervisord when `EnableBrowserMode` runs (browser mode and desktop
mode share the same Xvfb). These tools drive arbitrary desktop apps via
pixel coordinates.

| Tool | Field contract |
|------|---------------|
| `desktop_screenshot` | `{}` (no args) — returns `{image_b64, elements[], windows[], resolution, ocr_available}` |
| `desktop_click` | `{x, y, button?}` — button default 1 (left); 2=middle, 3=right |
| `desktop_type` | `{text, clear?}` — clear default true (Ctrl+A + Delete first) |
| `desktop_key` | `{keys}` — xdotool key combo like `Return`, `ctrl+c`, `alt+Tab` |

**Pixel coordinates are the contract, not text labels.** OCR text
positions are non-deterministic across runs — clicking "by text" via a
cached index would drift. The model picks `element.cx, element.cy` from
the latest desktop_screenshot and passes those to `desktop_click`.

**Errors propagate as Go errors** (same as browser tools — no graceful
degradation; the desktop tools need Xvfb up). Failures surface with the
operation name (`desktop click`, `desktop type`, etc.) in the message.

### 3.1 Adding a tool

1. Write a Go type implementing `core.Tool` (`Name()`, `Description()`,
   `Schema()`, `Execute(ctx, args) (any, error)`). See
   `mcpserver/sandbox_python.go` for a ~100-LOC example.
2. In `cmd/mcpserver/main.go::main`, register it: `srv.RegisterTool(myTool)`.
3. If it needs sandbox access, take an `adapters.BashExecutor` or
   `adapters.FileOps` in the constructor — same pattern as `bash.New(exec)`.
4. Add a test in `mcpserver/server_test.go` covering its return shape.
5. Rebuild (`task build`) — the tool shows up in `tools/list` automatically.

There is no codegen, no manifest, no Yaml. The registry is the source of
truth.

### 3.2 Tool result shape

Every tool returns `(any, error)` from `Execute`. The MCP server wraps
non-error returns into `{content: [{type: "text", text: stringify(v)}],
isError: false}`. Errors return `isError: true` with the error message
as the text content.

`stringifyResult` handles: strings (passthrough), maps/structs (JSON),
`[]byte` (string), everything else (`fmt.Sprintf("%v", v)`).

---

## Contract 4: Sandbox lifecycle

The server boots exactly one sandbox at startup and tears it down on
SIGTERM/SIGINT.

| Event | Behavior |
|-------|----------|
| Server start | `mgr.CreateSandbox(...)` blocks until container is healthy. |
| Tool call | `docker exec` against the running container ID. |
| SIGTERM / SIGINT | (1) destroy sandbox (30s budget), (2) shutdown HTTP (5s budget), (3) exit. |
| `--keep-alive` flag | Skip destroy on shutdown — leaves the container running for reuse. |
| `--sandbox-id <id>` flag | Adopt an existing container instead of creating a new one. |

If the destroy goroutine is scheduled after the HTTP server returns, it
never runs — the container leaks. The signal handler in
`cmd/mcpserver/main.go` runs destroy BEFORE HTTP shutdown, in a goroutine,
with main blocking on a `done` channel. Don't reorder it.

---

## Contract 5: Permission layer (bash only)

The `bash` tool runs commands through `perms.CheckBashPermission` before
exec. Two tiers:

| Pattern | Behavior | Examples |
|---------|----------|---------|
| Secret-adjacent reads | Hard-deny | `cat .env`, `cat ~/.ssh/id_rsa`, `cat ~/.aws/credentials`, `vim .env`, `cp .env /tmp/leak` |
| Everything else | Allow | `ls`, `go test`, `python3 -c`, `git status` |

Denial list is regex-driven, defined in `tools/bash/permissions.go`. Add
patterns there, not in ad-hoc command checks.

Output is also scrubbed (`sensitive.ScrubText`) before returning to the
model — defense in depth in case hard-deny misses something. Scrubber
patterns live in `internal/sensitive/scrubber.go`.

---

## Contract 6: Session semantics

| Behavior | Detail |
|----------|--------|
| Session ID generation | 16 random bytes, hex-encoded, returned in `Mcp-Session-Id` header on `initialize` response. |
| Session tracking | In-memory map keyed by session ID. |
| Mismatched session | `400` with `-32000` "session not found" error. |
| Session lifetime | Tied to server process. No persistence across restarts. |

Multi-tenant isolation is out of scope. One server, one sandbox, one
project. Run multiple servers on different ports if you need more.

---

## Contract 7: Audit log (opt-in)

| Behavior | Detail |
|----------|--------|
| Opt-in | `PUX_AUDIT_LOG=/path/to/audit.jsonl`. Empty (default) = no audit. |
| Format | JSONL, one entry per line. |
| Entry shape | `{ts, session_id, tool, args, result, error, duration_ms}`. |
| Scope | Successful tool dispatches only. Parse errors + unknown-tool lookups are wire-level failures, not model actions — they're NOT audited. |
| Secret scrubbing | `args` / `result` / `error` pass through `sensitive.ScrubText` before write. A leaked key in bash output becomes `[REDACTED_API_KEY]`. |
| Size cap | Each field capped at 4096 bytes (`...[truncated]` marker). 1 MiB of bash output → ~10 KiB audit line. |
| Concurrency | Mutex-serialized writes. Safe under N goroutines. |

This is the forensic record of "what did the model do to my code?" —
distinct from the client-owned conversation log and the server-owned zap
log (which is debug-level transport telemetry).

---

## Contract 8: Dispatch surface (opt-in via `PUX_LLM_API_KEY`)

When the env var is set at server boot, three additional tools land:
`dispatch_task`, `get_task_status`, `list_orgs`. They expose the
**server-side agent loop**: an external MCP client describes a task, the
server runs its own LLM provider (Anthropic Messages API), and the
configured "org" does the work.

### Async lifecycle

`dispatch_task` is **async** — it returns `{task_id, status: "pending"}`
immediately. The server spawns a goroutine that:

1. Loads the org (`<project>/orgs/<name>/org.toml`).
2. Validates the CTO/role tool whitelists against the registered catalog.
3. Builds a Plan/Act/Observe agent loop with the CTO's system prompt + the
   CTO's whitelisted tools (plus `delegate_to` if the org declares roles).
4. Runs the loop against Anthropic. Tool calls dispatch back through the
   SAME registered MCP tools (an agent calling `bash` hits the same code
   path as an external client calling `bash`).
5. Updates the task store: `pending → running → complete | failed`.

### Org layout

```
<project>/orgs/<name>/
├── org.toml           # name + description + [cto] + [[roles]]
├── cto.md             # CTO system prompt body
└── roles/<role>.md    # one per [[roles]] entry
```

The shipped `orgs/specialists/_demo/` is the canonical example: a CTO + a `researcher`
role. Per-org config knobs:

| Field | Default | Notes |
|-------|---------|-------|
| `sandbox_image` | `""` (= `pux-sandbox:latest`) | Per-org specialized sandbox image override. |
| `[cto].max_rounds` | `30` | Hard cap on Plan/Act/Observe rounds. |
| `[cto].tools` | (required) | Whitelist of registered MCP tool names. Must include `delegate_to` when roles are declared. |
| `[[roles]].tools` | (required) | Whitelist; MUST NOT include `delegate_to` (recursion guard, enforced). |

### Per-org serialization

One task per org at a time (filesystem races in the shared sandbox). The
dispatch goroutine holds a per-org mutex for its duration; `dispatch_task`
itself returns immediately. Cross-org dispatches run concurrently.

### Cancellation

`context.WithCancel` per task. SIGTERM calls `TaskStore.Shutdown()` which
cancels every in-flight task — their status moves to `failed:
"server shutdown"`. The provider honors the context; tools inherit it
via `errgroup.WithContext`.

### Multi-agent (`delegate_to`)

The CTO's tool list can include `delegate_to(role, task)`. From the
CTO's view it's synchronous — call it, get back the role's final
response text. Internally the tool builds a child Loop with:

- The role's system prompt (markdown body)
- The role's tool whitelist FILTERED against the same catalog
- No `delegate_to` (recursion guard, enforced at dispatch time)

The child's tool chatter is invisible to the CTO; only the final text
response is returned. The child can run any number of rounds within its
own `max_rounds` budget.

### Out of scope for v1 dispatch

- Multi-provider (only Anthropic for v1; OpenAI/Ollama can come later
  via `core.LLMProvider`).
- DB persistence (in-memory `TaskStore`; tasks lost on restart).
- SSE streaming to client (MCP transport is request/response; use
  `transcript_tail` for progress).
- Per-org sandbox isolation (orgs share the project sandbox).

---

## Compliance rules

1. **The server only speaks MCP.** No proprietary JSON-RPC methods, no
   non-MCP HTTP endpoints (health checks aside).
2. **The tool registry is the source of truth.** `tools/list` reflects
   exactly what's registered at boot. No dynamic tool addition.
3. **Tool errors are MCP results, not JSON-RPC errors.** A failed `bash`
   call returns `isError: true` so the model can react; a malformed
   request returns a JSON-RPC `-3xx0` error.
4. **All file paths are sandbox-internal.** Host path translation would
   violate the contract — the client never sees host paths.
5. **The sandbox is one-per-server.** Multi-tenant isolation requires a
   reverse proxy + auth at the boundary, not in this server.

---

## What this contract does NOT cover (deferred to dev branch)

- Multi-provider agent loop (only Anthropic on master; OpenAI/Ollama via `core.LLMProvider` later)
- DB persistence for dispatched tasks (in-memory only)
- SSE streaming to MCP client (use `transcript_tail` polling)
- Per-org sandbox isolation (orgs share the project sandbox)
- Desktop automation beyond xdotool pixel-coords (no VNC, no native window APIs)
- Skills system beyond list/load (no progressive-disclosure triggers)
- Org-level config overlays (no per-domain env / image overrides yet)
- TUI (Ink), web UI (Vite), CLI (Cobra)
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Transcript auditing, diligence evals, safeguard router

Each of these will migrate back to `master` one feature at a time, after
it's proven to fit the MCP contract cleanly. When that happens, this
document gets a new section. Don't speculatively extend it.
