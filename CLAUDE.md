# Orchestrator — Agent & Developer Guide

## Quick Start

```bash
task install           # Install dependencies (first time)
task dev               # Start Go backend + Vite frontend
task chat              # Start TUI
task down              # Stop everything
```

Then add a provider through the TUI or web UI model picker — no `.env` keys or config files needed. The system is blank until you bring your own LLM endpoint.

## Provider System

Pux doesn't ship with hardcoded model endpoints. Users add providers through the UI, which saves to `~/.pi/agent/settings.json`.

**Adding a provider:**
- **TUI**: `/model` → type provider name → select "Add provider" → enter baseUrl, API key, models
- **Web**: Click model name in composer → "Add Provider" → fill in details
- **API**: `POST /api/pux/providers` with `{id, baseUrl, apiKey, models: [{id, name, contextWindow}]}`

**Any OpenAI-compatible endpoint works:** local llama-server, OpenRouter, Together, Groq, vLLM, Ollama, etc.

**Model resolution** (`resolveEngineForModel`):
1. Settings.json providers (user-configured) — first match by model ID
2. Hardcoded dev engines (local llama-server, cluster, Gemini, OpenRouter) — only on dev machines

**Defaults** — two persistent model defaults set from the model picker:
- **Logic** (`defaultLogic`): CTO/orchestrator model (reasoning-heavy)
- **Worker** (`defaultWorker`): Sub-agent model (faster execution)
- **API**: `GET/PUT /api/pux/defaults` → `{logic, worker}`

**Resolution priority (CTO):** per-agent selection → inline `--model` → logic default → error
**Resolution priority (workers):** role `model:` field → worker default → CTO's engine

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
- **Build**: `cd backend && go build -o server ./cmd/server/` (no CGo)
- **CLI Build**: `cd backend && go build -o orch ./cmd/cli/` (the `orch` binary)
- **Non-fatal startup**: runs in sandbox/API-only mode when llama-server is down
- **PROJECT_ROOT**: must be set to repo root when running binary directly

## Repo Structure

```
auto-developer-orchestrator/
├── backend/              # Go kernel (was go-backend/)
│   ├── cmd/server/       # HTTP server entry point
│   ├── cmd/cli/          # CLI (orch binary)
│   └── internal/         # Agent loop, handlers, session, tools, sandbox
├── frontend/
│   ├── tui/              # Terminal UI (Ink 6, bun)
│   ├── web/              # Web UI (Vite, React)
│   └── shared/           # Shared TS between TUI and web (@pux/shared)
├── config/               # Agent configs (prompts, workers, capabilities)
├── infra/                # Dockerfiles, compose files, nginx.conf
├── extensions/           # TypeScript MCP extension servers
├── sandbox/              # Docker sandbox images and scripts
├── test-suite/           # AI-powered test organization
├── tests/                # E2E and integration tests
├── scripts/              # Helper scripts (visual testing, etc.)
├── sdk/                  # Python SDK for the API
├── Taskfile.yml          # All task commands
└── VERSION               # Current version
```

## Interfaces — THREE ways to use the system

### 1. TUI (Terminal UI) — `task chat` or `orch`
- React 19 + Ink 6 + `@assistant-ui/react-ink` TUI via bun (`frontend/tui/`)
- Shares `PuxChatAdapter` and `usePuxStore` with web UI via `frontend/shared/` package
- Streams SSE from Go backend, renders thinking/tool calls/assistant text in terminal
- Files: `frontend/tui/src/` (app.tsx is root, components/ are Ink components)
- Run: `task chat` or `cd frontend/tui && bun run src/main.tsx --project myproject`
- Tests: `cd frontend/tui && bun test`

### 2. CLI (scripting) — `orch agent prompt "message"`
- Cobra subcommands for scripting/CI: `orch agent prompt`, `orch agent history`, `orch sandbox`, `orch project`, etc.
- `orch agent prompt "do the thing" -p myproject` — streams SSE as text or JSON (`-o json`)
- Files: `backend/internal/cli/cmd/` (agent.go, sandbox.go, etc.)
- SSE client: `backend/internal/cli/api/client.go`

### 3. Frontend (web) — Vite React app on port 5174
- `task dev` starts both Go backend + Vite frontend
- SSE via `fetch` + `ReadableStream.getReader()` in `frontend/web/src/hooks/useSSEStream.ts`
- State: `frontend/web/src/hooks/agentReducer.ts`, `frontend/web/src/hooks/usePuxAgent.ts`
- Vite proxies `/api/*` to Go backend on 3847

**When testing, use the CLI or TUI — NOT curl.** Curl is a last-resort debug tool with its own timeout issues.

## Architecture

```
                    ┌─ TUI (Ink, bun)          ─┐
                    │  CLI (Cobra, Go)          │  Contract: SSE events
User ───────────────┤                           ├──→ @pux/shared → render
                    │  Web (Vite, React)        │
                    │  External Agents (API)    │
                    └───────────────────────────┘
                                 │
                     POST /api/pux/prompt  (TUI, CLI, web)
                     POST /api/jobs        (external agents)
                                 │
                                 ▼
                    Go Kernel (3847) ──────── LLM Providers (user-configured)
                         │                   settings.json: OpenRouter, local,
                    Docker Sandboxes         cluster, Gemini, vLLM, Ollama...
                    ┌────┴────┐
                    Chrome CDP  xdotool      MCP Servers
                    (browser)  (desktop)    ┌────┴────┐
                                               web    media
```

The kernel's job is to manage contracts: agent loop, tool execution, sandbox lifecycle.
Each interface (TUI, CLI, web) is a VIEW of the same SSE stream. They share `PuxChatAdapter`
and `usePuxStore` from `frontend/shared/`. Rendering is the only thing that differs.

### Jobs API (External Agents)

One-shot task submission for external agents (Hermes, CI pipelines, other tools).

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/jobs` | POST | Submit a one-shot task |
| `/api/jobs/{id}` | GET | Poll job status + output |
| `/api/jobs/{id}` | DELETE | Cancel/cleanup |

**Request:** `{"task": "Do x...", "project": "~/demad/", "org": "coder", "full_sandbox": true}`
- `task` (required) — the agent prompt
- `project`, `org`, `model` — optional overrides
- `full_sandbox` — sandboxOnly mode
- `wait` — if true, SSE streams the full response (same contract as `/api/pux/prompt`)
- `timeout_seconds` — max execution time (default: 600, cap: 1800)

**Async** (`wait=false`, default): Returns `202 Accepted` with `{"jobId": "...", "pollUrl": "/api/jobs/..."}`. Poll `GET /api/jobs/{id}` for status + output.

**Sync** (`wait=true`): SSE stream with `text_delta`, `thinking_delta`, `tool_execution_start`, `tool_execution_end`, `done` events. Same contract as `/api/pux/prompt`.

**Auth:** Optional API key via `X-API-Key` header or `?api_key=`. Configured in `~/.pi/agent/settings.json` → `jobsApiKey`.

**Auto-cleanup:** One-shot jobs are deleted after 24 hours. `CleanupExpiredOneShots` runs every 30 minutes.

**Files:** `handlers/jobs.go`, `handlers/jobs_test.go`, `scheduler/scheduler.go` (cleanup)

### Design Principles

1. **Pux is a contract system.** Pux is not a monolithic app like Pi-Mono. It is a Go kernel (a port of Pi-Mono concepts) that forms contracts with extensions, tools, the TUI, CLI, and web interface. Cleanliness comes from managing these contracts well, not from writing everything in one language. The primary contract is SSE events → `ChatState` → render. Any consumer that respects this contract is a valid interface.
2. **Kernel-based architecture.** The kernel is `config/prompt.md` (CTO system prompt template). Employees are add-ons in `config/roles/`. Shared capabilities are DRY packages in `config/tool_packages/`. Everything is template-driven, separated, and composable. New employees = new folder, new capabilities = new tool package.
3. **One agent, one loop, one model.** The orchestrator IS the agent. There is no separate "generalist mode" vs "orchestrator mode." Every prompt goes through the same agent loop. The model calls tools, reads results, calls more tools, then responds. The user sees one thinking block + one response.
4. **CTO/Employee split.** Pux (the CTO) only has delegation tools + basic bash/file ops. Browser, desktop, MCP, and vision tools live exclusively on employees. This forces the model to delegate instead of doing work itself. Employees have distinct, non-overlapping capabilities so the CTO picks the right person for the job.
5. **Simple over clever.** Flat agent loops beat deep hierarchies (Agent-S S3 proved this — 72.6% on OSWorld by removing DAG planning). One loop with reflection > nested orchestration.
6. **Pull from the best.** Port proven patterns from established agent frameworks — don't reinvent.

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

### Model Defaults

See **Provider System** section above for how logic/worker defaults work.
**TUI**: `l` to set logic default, `w` to set worker default in model picker.
**API**: `GET/PUT /api/pux/defaults` → `{logic, worker}`

## Key Packages

| Package | Purpose |
|---------|---------|
| `backend/cmd/server` | Entry point, wiring |
| `backend/internal/llama` | Agent loop, orchestrator, HTTP engine, sessions, tool registry |
| `backend/internal/handlers` | HTTP handlers (agent, sandbox, computer-use, scheduler) |
| `backend/internal/browser` | CDP client, SoM labeler, vision client |
| `backend/internal/sandbox` | Docker sandbox lifecycle |
| `backend/internal/llama/grounding.go` | Coordinate normalization, cycle detection, element caching |

## E2E Tests

```bash
# Playwright (web frontend, mocked backend)
task test-e2e

# Playwright (web frontend, real backend)
task test-integration

# Python (agent, API, browser, desktop, SSE, TUI)
task test-python
task test-python-api       # API only
task test-python-sse       # SSE contract
task test-tui-e2e          # TUI visual (requires task tui-visual)
task test-webui-e2e        # WebUI chat (Playwright + route mocking)
```

Tests auto-skip when required services are unreachable.

## TUI Regression Tests

**ALWAYS run before committing TUI changes.** Catches bugs that have already been fixed.

```bash
cd frontend/tui && bun test tests/regression.test.tsx
```

Covers: store shape (toggleThinking, providerRetry), event ordering (text→tool→text interleaving), agent rounds (restored agents have rounds), thinking rendering (no blockquote bars), @pux/shared symlink resolves correctly, provider retry (500 classified as transient).

Full TUI test suite (includes regression + all other tests):
```bash
cd frontend/tui && bun test tests/
```

Note: `b-flash-pty.test.ts` needs port 9877 free (kill `task tui-visual` first).

## TUI Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Ctrl+P` | Toggle thinking blocks collapse/expand |
| `Ctrl+T` | Cycle TUI views (chat → agents → tools → files) |
| `Ctrl+O` | Agent selector overlay |
| `Ctrl+B` | Background current foreground task |
| `Ctrl+C` ×2 | Quit |
| `Esc` ×2 (running) | Cancel running agents |
| `Esc` ×2 (idle) | Open rewind overlay (history) |

## Provider Retry

LLM provider errors (500, 502, 503, 504, rate limit, overloaded) are automatically retried with exponential backoff. Configurable in `~/.pi/agent/settings.json`:

```json
{
  "providerRetries": 5
}
```

Default: 5 retries. Backoff: 2s base, 60s max, with jitter. Shows "Retrying (2/5) in 4s — HTTP 500" in both TUI and WebUI during retry.

## TUI Rewind

Double-Escape when the agent is idle opens the rewind overlay. Shows a list of prior user messages (checkpoints). Select one, then choose "Restore conversation" to navigate the session tree back to that point. The TUI reloads with truncated history.

- **Backend**: `GET/POST /api/pux/rewind` — lists checkpoints and navigates session tree
- **Session tree**: `Navigate(nodeID)` moves the current pointer, `GetUserCheckpoints()` returns user messages
- **Files**: `backend/internal/handlers/pux_rewind.go`, `frontend/tui/src/components/rewind-overlay.tsx`
- **Not yet**: file checkpointing ("Restore code"), "Summarize from here"

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
