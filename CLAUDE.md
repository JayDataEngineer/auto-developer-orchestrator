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
task run                # Build + boot against $PWD
task smoke              # Build + boot + smoke test + teardown
task test               # Go unit tests
task hooks              # Install pre-commit (gitleaks)
```

The server takes `--addr` (default `127.0.0.1:9876`) and `--project` (defaults
to `$PWD`). Env vars `PUX_MCP_ADDR` / `PUX_PROJECT_PATH` mirror them.

Set `PUX_AUDIT_LOG=/path/to/audit.jsonl` to append every tool call (args +
result + duration, secret-scrubbed) to a forensic log. Opt-in; default off.

## Connecting MCP clients

Add to Claude Desktop (or any MCP client that supports HTTP transport):

```json
{
  "mcpServers": {
    "pux": { "url": "http://127.0.0.1:9876" }
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
│ pux-mcpserver (Go, localhost:9876)   │
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
│   └── internal/
│       ├── adapters/              # BashExecutor, FileOps (sandbox bridge)
│       ├── checkpoint/            # Git checkpointing (transitive)
│       ├── context/               # Context offloading (transitive)
│       ├── core/                  # core.Tool interface, hooks, events
│       ├── git/                   # Git ops (transitive)
│       ├── hooks/                 # Approval hooks (transitive)
│       ├── mcp/                   # MCP client (transitive — used by sandbox)
│       ├── mcpserver/             # ← THE NEW PACKAGE
│       │   ├── server.go          # JSON-RPC dispatch + tool registry
│       │   ├── transport.go       # HTTP handler
│       │   ├── session.go         # Session ID generator
│       │   ├── sandbox_python.go  # python tool (sandbox-aware)
│       │   ├── skills_tool.go     # list_skills / load_skill
│       │   ├── vision_tool.go     # describe_image (local ONNX vision)
│       │   └── browser_tool.go    # browser_* tools wrapping in-sandbox sb_server.py
│       ├── perms/                 # Permission checks (transitive)
│       ├── retry/                 # Provider retry (transitive)
│       ├── sandbox/               # Docker sandbox lifecycle
│       ├── sensitive/             # Secret scrubbing (transitive)
│       ├── session/               # Session storage (transitive)
│       ├── storage/               # Object storage (transitive)
│       └── tools/
│           ├── bash/              # Bash tool
│           ├── file/              # File tools (read/write/edit/grep/glob)
│           └── truncate/          # Output truncation
├── sandbox/                       # Dockerfile + scripts for pux-sandbox:latest
├── scripts/
│   ├── setup-hooks.sh             # Pre-commit install
│   └── smoke_mcp.py               # End-to-end smoke test
├── .github/workflows/ci.yml       # go-test + go-lint + secret-scan
├── Taskfile.yml                   # build / run / test / smoke / hooks
└── VERSION                        # 0.1.0-mvp
```

Packages marked "(transitive)" are needed because something in the deps
closure imports them — they're kept because they compile cleanly and don't
pull in heavyweight state. Pruning them further is a Phase 2 cleanup.

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

1. Write a Go type implementing `core.Tool` (`Name()`, `Description()`, `Schema()`, `Execute()`).
   See `mcpserver/sandbox_python.go` for a 100-LOC example.
2. In `cmd/mcpserver/main.go`, register it: `srv.RegisterTool(myTool)`.
3. If it needs sandbox access, take an `adapters.BashExecutor` or `adapters.FileOps`
   in the constructor — same pattern as `bash.New(exec)`.
4. Rebuild (`task build`) — the tool shows up in `tools/list` automatically.

There's no codegen, no manifest. The tool registry is the source of truth.

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

The contract is enforced by 33 Go tests in `mcpserver/`:

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
- Desktop automation (xdotool, VNC)
- Org system (per-domain config overlays — invest, twitter-agent, etc.)
- TUI (Ink), web UI (Vite), CLI (Cobra)
- Self-evolving script toolkit (`make_script` / `edit_script`)
- Transcript auditing, diligence evals, safeguard router
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
