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
- React 19 + Ink 6 + `@assistant-ui/react-ink` TUI via bun (`ts-tui-ink/`)
- Shares `PuxChatAdapter` and `usePuxStore` with web UI via `shared/` package
- Streams SSE from Go backend, renders thinking/tool calls/assistant text in terminal
- Files: `ts-tui-ink/src/` (app.tsx is root, components/ are Ink components)
- Run: `task chat` or `cd ts-tui-ink && bun run src/main.tsx --project myproject`
- Tests: `cd ts-tui-ink && bun test`

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
                    ┌─ TUI (Ink, bun)          ─┐
                    │  CLI (Cobra, Go)          │  Contract: SSE events
User ───────────────┤                           ├──→ @pux/shared → render
                    │  Web (Vite, React)        │
                    └───────────────────────────┘
                                 │
                          POST /api/pux/prompt
                                 │
                                 ▼
                    Go Kernel (3847) ──────── llama-server (8001)
                         │                        │
                    Docker Sandboxes         MCP Servers
                    ┌────┴────┐              ┌────┴────┐
                    Chrome CDP  xdotool      web       media
                    (browser)  (desktop)    (research) (vision)
```

The kernel's job is to manage contracts: agent loop, tool execution, sandbox lifecycle.
Each interface (TUI, CLI, web) is a VIEW of the same SSE stream. They share `PuxChatAdapter`
and `usePuxStore` from `shared/`. Rendering is the only thing that differs.

### Design Principles

1. **Pux is a contract system.** Pux is not a monolithic app like Pi-Mono. It is a Go kernel (a port of Pi-Mono concepts) that forms contracts with extensions, tools, the TUI, CLI, and web interface. Cleanliness comes from managing these contracts well, not from writing everything in one language. The primary contract is SSE events → `ChatState` → render. Any consumer that respects this contract is a valid interface.
2. **Kernel-based architecture.** The kernel is `config/prompt.md` (CTO system prompt template). Employees are add-ons in `config/roles/`. Shared capabilities are DRY packages in `config/tool_packages/`. Everything is template-driven, separated, and composable. New employees = new folder, new capabilities = new tool package.
3. **One agent, one loop, one model.** The orchestrator IS the agent. There is no separate "generalist mode" vs "orchestrator mode." Every prompt goes through the same agent loop. The model calls tools, reads results, calls more tools, then responds. The user sees one thinking block + one response.
4. **CTO/Employee split.** Pux (the CTO) only has delegation tools + basic bash/file ops. Browser, desktop, MCP, and vision tools live exclusively on employees. This forces the model to delegate instead of doing work itself. Employees have distinct, non-overlapping capabilities so the CTO picks the right person for the job.
5. **Simple over clever.** Flat agent loops beat deep hierarchies (Agent-S S3 proved this — 72.6% on OSWorld by removing DAG planning). One loop with reflection > nested orchestration.
6. **Pull from the best.** Reference repos in `reference/` contain proven patterns. Port the best ideas, don't reinvent.

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

### Organization System (Agent OS)

Pux is an **Agent OS** — the kernel provides the engine, external "Organizations" provide config overlays. When a project directory contains `pux.yaml`, the kernel enters org mode:

**All orgs live in `~/.pux/orgs/`** — single canonical location. Git-backed orgs are symlinked from their repo location.

```
~/.pux/orgs/
├── invest/                 → symlink to ~/Documents/programs/dev/invest
├── twitter-agent/          → symlink to ~/Documents/programs/dev/twitter-agent
├── tech-noir/              → symlink to ~/Documents/projects/creative/tech-noir/pux-org
├── dev-bot/                → moved directly (no git)
└── general/                → moved directly (no git)

# Each org:
my-org/
├── pux.yaml                # Org manifest (extensions_dir, skills_dir, staff_root, etc.)
├── MANIFESTO.md            # Prepended to CTO prompt
├── roles/                  # Org-specific employees
├── capabilities/           # Org-specific tool compositions
├── skills/                 # Org-scoped SKILL.md definitions
├── extensions/             # Org-scoped MCP extension servers (started at boot)
├── sandbox/                # Scripts mounted into sandbox
└── prompts/                # Scheduled prompt templates
```

**pux.yaml new fields:**
- `extensions_dir`: Org-scoped extension servers (TypeScript MCP). Discovered at startup.
- `skills_dir`: Org-scoped SKILL.md files. Loaded per-session, merged with kernel skills.

**How it works:**
1. `--org <name>` resolves via `~/.pux/orgs/<name>/` (primary) then legacy paths
2. Kernel detects `pux.yaml` → enters org mode
3. Org roles overlay kernel defaults; org capabilities merged; org skills merged
4. Org extensions discovered and started alongside kernel extensions at server boot
5. Role-level tool filtering ensures org tools only reach workers that import them

**Creating a new app = config only.** No Go code, no recompilation. Write YAML + Markdown, `--org` points to it.

### Agent Pipeline

- **Pux (CTO)** receives prompt, delegates to employees via `delegate_to` / `delegate_async`
- **CTO tools**: bash, file ops, memory, skills, delegate_to, delegate_async, collect_results
- **Employees** get their specific tool packages — browser, research, vision, code, shell, desktop
- **Sub-agents** run in separate agent loops with their role's tools + prompt
- **Vision-in-the-loop**: browser screenshots auto-described via vision provider chain
- **SoM labeler**: JS injection labels interactive elements with numbered boxes, 50-element cap

### Model Defaults (Logic & Worker)

The system supports two persistent model defaults configured from the model selector:

| Default | Purpose | API field |
|---------|---------|-----------|
| **Logic** | CTO/orchestrator — reasoning-heavy, planning | `defaultLogic` |
| **Worker** | Sub-agents/employees — faster execution | `defaultWorker` |

**How it works:**
1. Set defaults via the model picker dropdown (click the model name in the composer)
   - **Web**: dropdown shows current defaults at top, "Set as Logic Default" / "Set as Worker Default" buttons
   - **TUI**: `l` to set highlighted model as logic, `w` to set as worker
2. Defaults are persisted in `~/.pi/agent/settings.json` under `"defaults"`
3. Logic default is used for CTO prompts when no explicit model is selected
4. Worker default is used by `ProviderFactory` for sub-agents without a `model:` field in their role config
5. If a worker default isn't set, sub-agents fall through to the CTO's engine

**Resolution priority (CTO):** per-agent selection → inline `--model` → logic default → fallback chain
**Resolution priority (workers):** role `model:` field → worker default → CTO's engine

**API:**
- `GET /api/pux/defaults` — returns `{logic, worker}`
- `PUT /api/pux/defaults` — sets `{logic, worker}`

**Files:** `go-backend/internal/handlers/pux.go` (fields + Prompt wiring), `pux_models.go` (handlers + persistence), `pux_prompt.go` (ProviderFactory wiring), `shared/src/pux-store.ts` (state + actions), `src/web/src/components/assistant-ui/thread.tsx` (UI), `ts-tui-ink/src/components/model-picker.tsx` (TUI)

## Key Packages

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

## Remote LLM Access (Tech Noir Ray Cluster — Tailscale)

The cluster runs an LLM gateway behind Traefik on Tailscale node `100.86.69.57`.
Auto-loads the default model on first `/v1/chat/completions` request.

| Detail | Value |
|--------|-------|
| Cluster node | `100.86.69.57` (Tailscale) |
| Traefik NodePort | `30080` (web :80 → NodePort 30080) |
| Default model | `qwen3.6-27b-q5_k_s-32k` (BeeLlama.cpp with speculative decoding) |

**Endpoints** (from any machine on the Tailscale network):

| Method | Endpoint | What it does |
|--------|----------|-------------|
| POST | `/v1/llm/configure` | Select model & engine |
| POST | `/v1/chat/completions` | OpenAI-compatible chat (auto-loads default) |
| GET | `/v1/models` | List available models |
| * | `/llm/*` | Raw passthrough to llama-server HTTP API |

**Quick start:**
```bash
# 1. Configure a model (first time)
curl -X POST http://100.86.69.57:30080/v1/llm/configure \
  -H "Content-Type: application/json" \
  -d '{"model": "qwen3.6-27b-q5_k_s-32k"}'

# 2. Chat
curl http://100.86.69.57:30080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "qwen3.6-27b-q5_k_s-32k", "messages": [{"role": "user", "content": "Hello!"}]}'
```

If `config/local.yaml` has `secrets.api_key` set, include it: `-H "x-api-key: your-key"` or `?api_key=your-key`.

## TUI Visual Testing

```bash
task tui-visual                                          # Start visual testing server on :9877
curl http://localhost:9877/screenshot > /tmp/tui.png     # Take PNG screenshot
curl http://localhost:9877/screen                         # Get terminal buffer as JSON text
curl http://localhost:9877/observe                        # Combined: screenshot + text + logs
curl -X POST http://localhost:9877/input -d '{"text":"hello\n","wait":2}'  # Send input
curl -X POST http://localhost:9877/key -d '{"key":"escape"}'                # Send special key
```

**ALWAYS use visual testing when changing TUI rendering.** Unit tests do not catch display bugs.
The visual server runs the real TUI in a pty, captures the screen buffer, and serves PNG screenshots.
Use `/screenshot` to verify layout, `/screen` for text content, `/observe` for full state.
