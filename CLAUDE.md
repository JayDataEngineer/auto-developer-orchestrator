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
- **Non-fatal startup**: runs in sandbox/API-only mode when llama-server is down
- **PROJECT_ROOT**: must be set to repo root when running binary directly

## Architecture

```
User → Vite (5174) → Go Backend (3847) → llama-server (8001)
                         ↓
                    Docker Sandboxes (OpenShell)
                         ↓
                    Chrome CDP (19222) → Browser Automation
```

### Agent Pipeline

- **Orchestrator** receives prompt, creates plan, delegates to sub-agents
- **Sub-agents** (web, code, desktop) — ephemeral, yield results, VRAM freed
- **Vision-in-the-loop**: every browser macro tool auto-captures screenshot + vision description after page changes
- **SoM labeler**: JS injection labels interactive elements with numbered boxes, 50-element cap
- **Macro tools**: `browse_to(url)`, `read_page()`, `click_element(id)`, `type_text(element, text, submit)`, `search_web(query)`

### Key Packages

| Package | Purpose |
|---------|---------|
| `go-backend/cmd/server` | Entry point, wiring |
| `go-backend/internal/llama` | Agent loop, orchestrator, HTTP engine, sessions |
| `go-backend/internal/handlers` | HTTP handlers (pi, sandbox, computer-use, scheduler) |
| `go-backend/internal/browser` | CDP client, SoM labeler, vision client |
| `go-backend/internal/sandbox` | Docker sandbox lifecycle |
| `go-backend/internal/pi` | Pi agent pool, sub-agent manager |

## E2E Tests

```bash
cd tests/python
python3 -m pytest test_web_forum_fillout.py -v    # Browser automation
python3 -m pytest test_sse_contract.py -v          # SSE event validation
```

Tests auto-skip when required services are unreachable.
