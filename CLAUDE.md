# Orchestrator — Agent & Developer Guide

## Quick Start

```bash
# 1. Start the model server (vision + thinking, default mode)
./scripts/model.sh

# 2. Start Go backend + Vite frontend
./scripts/dev.sh

# Alternative model modes:
# ./scripts/model.sh fast    # vision, no thinking (faster per-token)
# ./scripts/model.sh text    # text-only + thinking
# ./scripts/model.sh bare    # text-only, no thinking (minimal VRAM)
```

## Model Server

- **Binary**: llama.cpp Docker container (`ghcr.io/ggml-org/llama.cpp:server-cuda`)
- **Model**: Gemma 4 26B-A4B (MoE, 4B active params, IQ4_NL quantization, 13GB)
- **Vision**: Gemma 4 26B IS MULTIMODAL. Has mmproj (vision encoder, 1.1GB). Loaded by default.
- **Model files**: `shared-docker-infra/models/llm/gemma-4-26B-A4B-it-UD-IQ4_NL.gguf`
- **mmproj file**: `shared-docker-infra/models/vision/gemma-4-26B-A4B-it-mmproj-F16.gguf`
- **Port**: 8001
- **API**: OpenAI-compatible at `/v1/chat/completions` — supports `image_url` content parts for vision

## Go Backend

- **Port**: 3847
- **Build**: `cd go-backend && go build -o server ./cmd/server/`
- **Run**: `PROJECT_ROOT=$(pwd) go-backend/server` (or `./scripts/dev.sh`)
- **No CGo required** — pure Go, talks to llama-server via HTTP
- **Non-fatal startup**: backend runs in sandbox/API-only mode when llama-server is down

## Architecture

```
User → Vite (5174) → Go Backend (3847) → llama-server (8001)
                         ↓
                    Docker Sandboxes (OpenShell image)
                         ↓
                    Chrome CDP (19222) → Browser Automation
```

### Agent Pipeline

- **Orchestrator** receives user prompt, creates plan, delegates to sub-agents
- **Sub-agents** (web, code, desktop) — ephemeral, yield results, VRAM freed
- **Vision-in-the-loop**: every browser macro tool (browse_to, read_page, click_element, type_text, search_web) auto-captures screenshot + vision description after page changes
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
| `go-backend/internal/scheduler` | CRON/recurring jobs |

## E2E Tests

```bash
cd tests/python
python3 -m pytest test_web_forum_fillout.py -v    # Browser automation (sandbox)
python3 -m pytest test_sse_contract.py -v          # SSE event validation
```

Tests auto-skip when required services (API, sandbox) are unreachable.

## Key Files

- **Startup**: `scripts/model.sh` (llama-server), `scripts/dev.sh` (Go + Vite)
- **Agent loop**: `go-backend/internal/llama/agent_loop.go`
- **Orchestrator**: `go-backend/internal/llama/orchestrator.go`
- **Computer use**: `go-backend/internal/handlers/computer_use.go`
- **CDP client**: `go-backend/internal/browser/sandbox_client.go`
- **SoM labeler**: `go-backend/internal/browser/labeler.go`
- **Vision client**: `go-backend/internal/browser/vision.go`
- **Model config**: `go-backend/internal/llama/config.go`
