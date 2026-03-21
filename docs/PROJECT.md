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
              ┌───────────────┴───────────────┐
              ▼                               ▼
┌─────────────────────────┐     ┌─────────────────────────────┐
│   LangGraph Agent       │     │   External AI Services      │
│   (Python - Port 8194)  │     │   - Google Jules            │
│   - Deep agent tasks    │     │   - Gemini API              │
│   - File system ops     │     │   - Claude API              │
│                         │     │   - OpenAI API              │
└─────────────────────────┘     └─────────────────────────────┘
```

## Components

| Component | Technology | Port | Purpose |
|-----------|------------|------|---------|
| **Frontend** | React 19 + Vite | 3847 | Dashboard UI |
| **Backend** | Express + TypeScript | 3847 | API server, orchestration |
| **LangGraph Agent** | Python + LangGraph | 8194 | Autonomous agent tasks |
| **External AI** | Various APIs | - | Jules, Gemini, Claude, OpenAI |

## Project Structure

```
auto-developer-orchestrator/
├── src/                    # React frontend
│   ├── components/         # UI components
│   ├── lib/               # Utilities
│   └── App.tsx            # Main application
├── python_agent/          # LangGraph Python agent
│   ├── agent.py           # Agent implementation
│   └── langgraph.json     # LangGraph config
├── server.ts              # Express backend
├── docker-compose.dev.yml # Development Docker setup
├── Dockerfile.dev         # Dev Dockerfile with hot reload
├── Makefile               # Developer commands
└── docs/                  # Documentation
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
- **This App** = Local orchestrator that coordinates Jules + other AI providers
- **LangGraph Agent** = Python microservice for deep agent tasks

See `docs/JULES.md` for detailed Jules documentation.
