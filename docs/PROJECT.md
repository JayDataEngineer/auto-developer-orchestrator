# Auto-Developer Orchestrator - Project Overview

## What This Project Is

**Auto-Developer Orchestrator** is a Dockerized orchestration platform for managing AI coding tasks across multiple repositories. It integrates with external AI services including **Google's Jules**, Gemini, Claude, and OpenAI.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Frontend (React + Vite)                  │
│                     http://localhost:3847                   │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  Backend (Express + TypeScript)             │
│                      server.ts (Port 3847)                  │
│  - Project management                                       │
│  - Task coordination                                        │
│  - AI provider routing                                      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│   External AI Services                                      │
│   - Google Jules (cloud)                                    │
│   - Gemini API                                              │
│   - Claude API                                              │
│   - OpenAI API                                              │
│   - deepagentsjs (client-side)                              │
└─────────────────────────────────────────────────────────────┘
```

## Components

| Component | Technology | Port | Purpose |
|-----------|------------|------|---------|
| **Frontend** | React 19 + Vite | 3847 | Dashboard UI |
| **Backend** | Express + TypeScript | 3847 | API server, orchestration |
| **External AI** | Various APIs | - | Jules, Gemini, Claude, OpenAI, deepagentsjs |

## Project Structure

```
auto-developer-orchestrator/
├── src/                    # React frontend
│   ├── components/         # UI components
│   ├── lib/               # Utilities
│   └── App.tsx            # Main application
├── server.ts              # Express backend
├── docker-compose.dev.yml # Development Docker setup
├── Dockerfile.dev         # Dev Dockerfile with hot reload
├── Makefile               # Developer commands
├── ref_docs/              # Reference documentation (not git-tracked)
│   └── deepagentsjs/      # LangChain Deep Agents JS library
└── docs/                  # Project documentation
    ├── JULES.md          # Jules reference (external service)
    └── PROJECT.md        # This file
```

## Development Workflow

```bash
# Start development environment (hot reload enabled)
make dev-up

# View logs
make logs

# Stop environment
make dev-down
```

See `Makefile` for all commands.

## External Services

### Google Jules
- **What:** Autonomous AI coding agent (NOT part of this codebase)
- **Access:** jules.google.com, CLI, API
- **Integration:** Our app can dispatch tasks to Jules via API

### Other AI Providers
- **Gemini:** Google's AI model (client-side integration)
- **Claude:** Anthropic's AI assistant
- **OpenAI:** GPT models for code generation

## Key Files

| File | Purpose |
|------|---------|
| `TODO_FOR_JULES.md` | Task checklist for each project |
| `.env.example` | Environment variable template |
| `docker-compose.dev.yml` | Development environment config |
| `Makefile` | Developer commands |

## Important Distinctions

- **Jules** = External Google service (autonomous AI agent)
- **deepagentsjs** = LangChain's JavaScript library for deep agents (local reference in `ref_docs/`)
- **Auto-Developer Orchestrator** = Our local app that orchestrates Jules + other AI providers

See `docs/JULES.md` for detailed Jules documentation.
See `ref_docs/deepagentsjs/README.md` for Deep Agents documentation.
