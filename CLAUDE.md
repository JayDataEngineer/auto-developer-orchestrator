# Orchestrator — Agent & Developer Guide

## Quick Start

```bash
task model             # Start llama-server (vision + thinking, default)
task dev               # Start Go backend + Vite frontend
# Ctrl+C to stop dev, then:
task model-down        # Stop llama-server
task down              # Stop everything (backend, frontend, sandboxes)
```

## Taskfile Commands

| Command | What it does |
|---------|-------------|
| `task model` | Start llama-server with vision (default). ~118 tok/s on RTX 4090 |
| `task model FLAGS=fast` | Vision, no thinking (faster per-token) |
| `task model FLAGS=text` | Text-only + thinking (more VRAM for context) |
| `task model FLAGS=bare` | Text-only, no thinking (minimal VRAM) |
| `task model-down` | Stop llama-server |
| `task model-status` | Check model server health and stats |
| `task dev` | Start Go backend (3847) + Vite frontend (5174) |
| `task down` | Kill everything — backend, frontend, all Docker sandboxes |
| `task test-go` | Run Go unit tests |
| `task test-e2e` | Run Playwright E2E tests |
| `task infra-check` | Check Traefik, LiteLLM, Langfuse health |
| `task --list` | Show all available tasks |

## Model Server

- **Container**: `ghcr.io/ggml-org/llama.cpp:server-cuda` on port 8001
- **Model**: Gemma 4 26B-A4B (MoE, 4B active params, IQ4_NL, 13GB) — **MULTIMODAL**
- **Vision**: Has mmproj (1.1GB). Loaded by default in `task model` and `task model FLAGS=fast`
- **Model**: `shared-docker-infra/models/llm/gemma-4-26B-A4B-it-UD-IQ4_NL.gguf`
- **mmproj**: `shared-docker-infra/models/vision/gemma-4-26B-A4B-it-mmproj-F16.gguf`
- **API**: OpenAI-compatible `/v1/chat/completions` — supports `image_url` content parts

## Go Backend

- **Port**: 3847
- **Build**: `cd go-backend && go build -o server ./cmd/server/` (no CGo)
- **CLI Build**: `cd go-backend && go build -o orch ./cmd/cli/` (the `orch` binary)
- **Non-fatal startup**: runs in sandbox/API-only mode when llama-server is down
- **PROJECT_ROOT**: must be set to repo root when running binary directly

## Interfaces — THREE ways to use the system

### 1. TUI (Terminal UI) — `task chat` or `orch`
- TypeScript pi-mono TUI via bun (`ts-tui-pi/`), spawned by `orch` chat command
- Streams SSE from Go backend, renders thinking/tool calls/assistant text in terminal
- Files: `ts-tui-pi/src/` (core/pux-agent-session.ts is the SSE bridge, modes/interactive/ is the TUI)
- Run: `task chat` or `cd ts-tui-pi && bun run src/main.ts --project myproject`
- Tests: `cd ts-tui-pi && bun test`

### 2. CLI (scripting) — `orch agent prompt "message"`
- Cobra subcommands for scripting/CI: `orch agent prompt`, `orch agent history`, `orch sandbox`, `orch project`, etc.
- `orch agent prompt "do the thing" -p myproject` — streams SSE as text or JSON (`-o json`)
- Files: `go-backend/internal/cli/cmd/` (agent.go, sandbox.go, etc.)
- SSE client: `go-backend/internal/cli/api/client.go`

### 3. Frontend (web) — Vite React app on port 5174
- `task dev` starts both Go backend + Vite frontend
- SSE via `fetch` + `ReadableStream.getReader()` in `src/hooks/useSSEStream.ts`
- State: `src/hooks/agentReducer.ts`, `src/hooks/usePuxAgent.ts`
- Vite proxies `/api/*` to Go backend on 3847

**When testing, use the CLI or TUI — NOT curl.** Curl is a last-resort debug tool with its own timeout issues.

## Architecture

```
User → Vite (5174) → Go Backend (3847) → llama-server (8001)
                         ↓
                    Docker Sandboxes (OpenShell)
                         ↓
                    Chrome CDP (19222) → Browser Automation
                    X11 xdotool → Desktop Automation
```

### Design Principles

1. **Kernel-based architecture.** The system is a kernel + add-ons, like pi-mono. The kernel is `config/prompt.md` (CTO system prompt template). Employees are add-ons in `config/roles/`. Shared capabilities are DRY packages in `config/tool_packages/`. Everything is template-driven, separated, and composable. New employees = new folder, new capabilities = new tool package.
2. **One agent, one loop, one model.** The orchestrator IS the agent. There is no separate "generalist mode" vs "orchestrator mode." Every prompt goes through the same agent loop. The model calls tools, reads results, calls more tools, then responds. The user sees one thinking block + one response.
3. **CTO/Employee split.** Pux (the CTO) only has delegation tools + basic bash/file ops. Browser, desktop, MCP, and vision tools live exclusively on employees. This forces the model to delegate instead of doing work itself. Employees have distinct, non-overlapping capabilities so the CTO picks the right person for the job.
4. **Simple over clever.** Flat agent loops beat deep hierarchies (Agent-S S3 proved this — 72.6% on OSWorld by removing DAG planning). One loop with reflection > nested orchestration.
5. **Pull from the best.** Reference repos in `reference/` contain proven patterns. Port the best ideas, don't reinvent.

### Kernel Config Structure

```
config/
├── prompt.md              # CTO system prompt template ({{.Tools}}, {{.Agents}}, {{.SandboxID}})
├── models.json            # llama-server model profiles
├── roles/                 # Employee add-ons — one folder per employee
│   ├── sarah/             # Research Lead — web research only
│   │   ├── config.yaml    # imports: [research], max_rounds, temperature
│   │   └── prompt.md      # Employee-specific instructions
│   ├── jake/              # Web Operations — browser automation only
│   ├── marcus/            # Senior Developer — code editing only
│   ├── elena/             # Creative Director — image analysis only
│   ├── alex/              # IT Operations — shell commands only
│   └── ryan/              # Desktop Support — GUI desktop only
└── tool_packages/         # Shared capability groups (DRY)
    ├── browser.yaml       # Browser automation tools (bash for sb_server)
    ├── research.yaml      # MCP web-research server tools
    ├── vision.yaml        # MCP media-analysis server tools
    ├── shell.yaml         # Bash + basic file ops
    ├── code.yaml          # Bash + full file editing toolkit
    └── desktop.yaml       # Desktop GUI tools
```

Roles import tool packages — no tool duplication across employees. Adding a new employee = new folder + existing packages. Adding a new capability = new tool package.

### Agent Pipeline

- **Pux (CTO)** receives prompt, delegates to employees via `delegate_to` / `delegate_async`
- **CTO tools**: bash, file ops, memory, skills, delegate_to, delegate_async, collect_results
- **Employees** get their specific tool packages — browser, research, vision, code, shell, desktop
- **Sub-agents** run in separate agent loops with their role's tools + prompt
- **Vision-in-the-loop**: browser screenshots auto-described via vision provider chain
- **SoM labeler**: JS injection labels interactive elements with numbered boxes, 50-element cap

### Key Packages

| Package | Purpose |
|---------|---------|
| `go-backend/cmd/server` | Entry point, wiring |
| `go-backend/internal/llama` | Agent loop, orchestrator, HTTP engine, sessions, tool registry |
| `go-backend/internal/handlers` | HTTP handlers (agent, sandbox, computer-use, scheduler) |
| `go-backend/internal/browser` | CDP client, SoM labeler, vision client |
| `go-backend/internal/sandbox` | Docker sandbox lifecycle |
| `go-backend/internal/llama/grounding.go` | Coordinate normalization, cycle detection, element caching |

## E2E Tests

```bash
cd tests/python
python3 -m pytest test_web_forum_fillout.py -v    # Browser automation
python3 -m pytest test_sse_contract.py -v          # SSE event validation
```

Tests auto-skip when required services are unreachable.
