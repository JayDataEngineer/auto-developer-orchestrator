# Pux MCP Server

A Model Context Protocol (MCP) server that exposes a sandboxed development
environment to any MCP-capable LLM client (Claude Desktop, Hermes, OpenClaw,
continue.dev, etc.). The server boots a Docker sandbox on startup, mounts
your project, and exposes `bash`, `file_read`, `file_write`, `file_edit`,
`file_grep`, `file_glob`, and `python` tools to the model over standard MCP.

**Scope:** single-tenant, localhost-only, no auth. Run one server per project.

## Quick start

```bash
# Build + boot against the current directory
task run

# Or explicitly:
task build
./backend/mcpserver --addr 127.0.0.1:9876 --project ~/code/myproject
```

The server expects `pux-sandbox:latest` (or `$OPENSHELL_IMAGE`) to be
available locally. See `sandbox/Dockerfile` to build it from scratch.

## Connect from a client

Add to your MCP client config (Claude Desktop, etc.):

```json
{
  "mcpServers": {
    "pux": {
      "url": "http://127.0.0.1:9876"
    }
  }
}
```

## Tools exposed

| Tool | What it does |
|------|-------------|
| `bash` | Execute a shell command in the sandbox |
| `file_read` | Read a file (with line numbers) |
| `file_write` | Write or overwrite a file |
| `file_edit` | sed-style find/replace |
| `file_grep` | ripgrep (with grep fallback) |
| `file_glob` | File pattern matching |
| `python` | Execute Python inside the sandbox (sandbox deps available) |

Files are relative to `/sandbox/workspace/` inside the container — that's
where your project is bind-mounted.

## Smoke test

```bash
task smoke
```

Builds, boots, drives the full MCP contract (initialize → tools/list →
tools/call → bash echo + file roundtrip + python sum), and tears down.

## Architecture

```
┌─────────────────────────────────────┐
│ MCP Client (Claude / Hermes / ...)  │
└──────────────┬──────────────────────┘
               │ JSON-RPC 2.0 over HTTP
               │ (Mcp-Session-Id header)
┌──────────────▼──────────────────────┐
│ pux-mcpserver (Go, localhost:9876)   │
│  - tool registry                     │
│  - JSON-RPC dispatch                 │
└──────────────┬──────────────────────┘
               │ Docker exec
┌──────────────▼──────────────────────┐
│ pux-sandbox container                │
│  - /workspace bind-mount             │
│  - /sandbox/scripts.py (read-only)   │
└─────────────────────────────────────┘
```

## Branch layout

- **`master`** — this MVP. Slim, focused, ~3000 LOC of Go.
- **`dev`** — fullstack branch with TUI, web UI, CLI, multi-agent orchestration,
  skills system, org overlays. Frozen, not deleted.
- **`v0.1.0-fullstack-legacy`** — tagged snapshot of fullstack HEAD before
  the MVP pivot. Safety net in case `dev` regresses.

## Status

Phase 1 surface (bash + file + python). Browser/desktop automation, multi-agent
orchestration, and the skills system are all on `dev` — they'll migrate back
incrementally once each is proven to fit the MCP contract cleanly.

## License

See [LICENSE](LICENSE).
