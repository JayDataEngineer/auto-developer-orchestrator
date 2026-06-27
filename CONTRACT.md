# Pux MCP Server — Contract Specification

> Source of truth for the slim MVP on `master`. Everything in this codebase
> conforms to this document or it's a bug. The fullstack contract (TUI, web
> UI, CLI, sub-agents, organizations, skills) lives on the `dev` branch with
> its own contract — this document does not describe that surface.

## Philosophy

Pux on `master` is one thing: an MCP server. It speaks standard Model
Context Protocol to any MCP-capable client (Claude Desktop, Hermes, OpenClaw,
continue.dev). It does not render a UI, run agents, schedule prompts, or
orchestrate sub-agents — those concerns belong to the client on the other
end of the wire.

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
│ pux-mcpserver (Go, localhost:9876)       │
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
| Endpoint | Single URL (default `http://127.0.0.1:9876`) |
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

- Sub-agent orchestration (CTO + employee delegation loop)
- Browser automation (CDP, SeleniumBase, vision-in-the-loop)
- Desktop automation (xdotool, VNC)
- Skills system (backbone SKILL.md + discoverable skills)
- Org system (per-domain config overlays)
- TUI (Ink), web UI (Vite), CLI (Cobra)
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Transcript auditing, diligence evals, safeguard router
- SSE event stream (the slim MVP is request/response, not streaming)

Each of these will migrate back to `master` one feature at a time, after
it's proven to fit the MCP contract cleanly. When that happens, this
document gets a new section. Don't speculatively extend it.
