# Pux

**Pi-Mono driving a Docker sandbox MCP backend.** Pux is a TS harness around
[pi-mono](https://github.com/badlogic/pi-mono) that adds:

- A Docker sandbox with Chrome, Xvfb, xdotool, tesseract, supervisord.
- 16 MCP tools (bash, file_read/write/edit/grep/glob, python, browser_*,
  desktop_*, describe_image, list_skills, load_skill) backed by the sandbox.
- An org system: thin `--org <name>` overlay that appends `orgs/<name>/AGENTS.md`
  to the system prompt. Subagent delegation via pi-subagents.

Single-tenant, localhost-only, no auth. One pux process = one project = one
sandbox.

## Quick start

```bash
# 1. Clone + install TS deps
git clone <this-repo> pux && cd pux
npm install

# 2. Build the sandbox image (one-time, ~5 min)
cd sandbox && docker build -t pux-sandbox:latest . && cd ..

# 3. Boot the stack (verifies Docker, builds Go binary, starts MCP server)
pux setup

# 4. Drive it
pux                             # interactive TUI
pux --org _demo                 # interactive TUI with the demo CTO overlay
pux dispatch --org _demo "describe this project"  # one-shot headless

# 5. Teardown
pux teardown
```

The MCP server listens at `http://127.0.0.1:9987`. pux connects to it
automatically via `pi-mcp-adapter` (configured in `.mcp.json`).

## Subcommands

| Subcommand | What it does |
|------------|-------------|
| `pux` | Interactive TUI (pi-mono). |
| `pux --org <name>` | Interactive TUI with `orgs/<name>/AGENTS.md` appended to the system prompt. |
| `pux dispatch [...args]` | Alias for `pux -p [...args]` — headless one-shot. |
| `pux history list` | List recent pi-mono sessions from `~/.pi/agent/sessions/`. |
| `pux setup` | Verify Docker + sandbox image, build + start MCP server. |
| `pux teardown` | Stop the MCP server (`task stop`). |
| `pux --resume` | pi-mono session picker (TUI). |
| `pux --continue` | Resume most recent session in this cwd. |

Any other flag is passed through to pi-mono. Run `pux --help` to see
pi-mono's full option set plus the pux extension flags (`--org`,
`--mcp-config`).

## Tools exposed

All tools execute inside the Docker sandbox. The project is bind-mounted at
`/sandbox/workspace/`.

| Tool | What it does |
|------|-------------|
| `bash` | Execute a shell command in the sandbox |
| `file_read` / `file_write` / `file_edit` / `file_grep` / `file_glob` | File operations |
| `python` | Execute Python inside the sandbox |
| `browser_navigate` / `browser_click` / `browser_type` / `browser_screenshot` / `browser_evaluate` | Persistent SeleniumBase Chrome session |
| `desktop_screenshot` / `desktop_click` / `desktop_type` / `desktop_key` | Xvfb desktop automation (xdotool + OCR) |
| `describe_image` | Local ONNX vision (Qwen3.5-2B, opt-in via `scripts/bootstrap-vision.sh`) |
| `list_skills` / `load_skill` | Discover and load project-local skill markdown |

In the TS harness, tools are exposed via `pi-mcp-adapter` with
`directTools: true` — each tool becomes a first-class pi tool with a
`pux_sandbox_*` prefix. Agents reference them as `mcp:pux-sandbox/<tool>` in
their `tools:` frontmatter.

## Org system

Orgs are markdown-driven. Drop a directory under `orgs/<name>/` with an
`AGENTS.md`:

```
orgs/<name>/
└── AGENTS.md    # CTO system prompt body
```

`pux --org <name>` appends the body to the base system prompt. The main pi
session becomes the CTO.

Specialist subagents live under `.pi/agents/*.md` with rich frontmatter
(`tools`, `model`, `thinking`, `output`, `systemPromptMode`, etc.). The
shipped example is `.pi/agents/researcher.md` — a read-only codebase
investigator. Spawn one from the main session via the `subagent` tool:

```
subagent({ agent: "researcher", task: "list all .ts files under src/" })
```

See [pi-subagents](https://github.com/nicobailon/pi-subagents) for the full
agent/skill format and delegation patterns (parallel, chain, async, fork).

## Sessions & history

pi-mono writes sessions as JSON Lines at
`~/.pi/agent/sessions/<encoded-cwd>/<timestamp>_<uuid>.jsonl`. The format is
crash-safe and supports tree-structured branching (`/fork`, `/branch`,
`/tree`).

```bash
pux history list               # show recent sessions
pux --resume                   # interactive session picker
pux --continue                 # resume most recent
pux --session <partial-uuid>   # resume a specific session
```

## Smoke test

```bash
task smoke
```

Builds, boots the Go server via the supervisor, drives the full MCP contract
(initialize → tools/list → tools/call → bash echo + file roundtrip + python
sum), and tears down. Real Docker — catches container-side regressions that
unit tests miss.

## Architecture

```
┌─────────────────────────────────────────┐
│ pux (TS harness)                        │
│  bin/pux.mjs + .pi/extensions/          │
│  + AGENTS.md + .pi/agents/ + .pi/skills/│
└──────────────┬──────────────────────────┘
               │ MCP JSON-RPC (pi-mcp-adapter)
┌──────────────▼──────────────────────────┐
│ pux-mcpserver (Go, localhost:9987)      │
│  tool registry + lifecycle supervisor   │
└──────────────┬──────────────────────────┘
               │ docker exec
┌──────────────▼──────────────────────────┐
│ pux-sandbox container                   │
│  Chrome + Xvfb + xdotool + tesseract +  │
│  supervisord + /workspace bind-mount    │
└─────────────────────────────────────────┘
```

## Branch layout

- **`pi-pivot`** — current. Pi-Mono on top, slim Go MCP server below.
- **`master`** — pre-pivot MVP. Slim Go MCP server with in-process agent
  loop + history + TUI + dispatch surface.
- **`v0.2.0-pre-pi-mono`** — tag of master HEAD before this pivot. Safety net.
- **`dev`** + **`v0.1.0-fullstack-legacy`** — older fullstack predecessor.

## Status

The pi-mono pivot is the current line of work. The Go side (sandbox + MCP)
is stable. The TS side (org system, sample agents, sample skills) is
minimal-but-complete — drop more orgs / agents / skills as markdown files.

## License

See [LICENSE](LICENSE).
