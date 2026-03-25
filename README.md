# Auto-Developer Orchestrator

A high-performance, Dockerized orchestration platform for managing AI coding tasks via GitHub. Features a **Go backend** for concurrent operations, **Python deep agent microservice** for LangChain integration, and a **React 19 frontend** for the dashboard.

## Features

- **Multi-Repository Support**: Manage multiple projects from a single dashboard.
- **Local File System Integration**: Point the orchestrator to your local folder containing GitHub repositories.
- **Clone Repository**: Directly clone new repositories into your projects folder from the UI.
- **Autonomous Loop**: Automatically generate tasks, write code, run tests, and merge PRs.
- **AI Provider Integration**: Connect to Google JULES, Gemini, Claude, OpenAI via LiteLLM.
- **Real-time Terminal**: View live logs of agent actions and system status with SSE streaming.
- **Coverage Reports**: Track test coverage across your projects.
- **High Performance**: 8.5x faster concurrent requests, 4.7x less memory than Node.js.

## Quick Start

### Development (Hot Reload)

```bash
# Start all services with hot reload
make dev-up

# View logs
make logs

# Stop services
make dev-down
```

### Production

```bash
# Start production environment
make prod-up

# View logs
make logs

# Stop services
make prod-down
```

### Manual Setup

1. **Configure Environment**:
   ```bash
   cp .env.example .env
   ```
   Edit `.env` and add your API keys (JULES, OpenAI, Gemini, Claude, GitHub, etc.).

2. **Start Services**:
   ```bash
   # Go backend (Port 3847)
   cd go-backend && go run cmd/server/main.go

   # Python agent (Port 8080)
   cd python-agent && uvicorn main:app --reload

   # React frontend (Port 5173)
   npm run dev
   ```

3. **Access the Dashboard**:
   - Frontend: `http://localhost:5173`
   - Go API: `http://localhost:3847`
   - Python Agent: `http://localhost:8080`

## Project Structure

```
auto-developer-orchestrator/
├── go-backend/              # Go API server (chi router, SQLite)
│   ├── cmd/server/          # Main entry point
│   ├── internal/
│   │   ├── handlers/        # HTTP handlers
│   │   ├── storage/         # Database layer
│   │   └── git/             # Git operations
│   ├── Dockerfile           # Production build
│   └── Dockerfile.dev       # Development with hot reload
│
├── python-agent/            # Python deep agent microservice
│   ├── main.py              # FastAPI server
│   ├── deep_agent.py        # LangChain integration
│   ├── Dockerfile           # Production build
│   └── Dockerfile.dev       # Development build
│
├── src/                     # React 19 frontend (Vite)
│   ├── components/          # UI components
│   └── App.tsx              # Main application
│
├── docker-compose.go.yml    # Production orchestration
├── docker-compose.dev.go.yml # Development orchestration
├── Makefile.go              # Development commands
└── docs/                    # Documentation
    ├── MIGRATION.md         # Migration guide from Node.js
    └── QUICK_REFERENCE.md   # Quick reference card
```

## Adding a Project

Create a new directory with a `TODO_FOR_JULES.md` file:

```bash
mkdir -p projects/my-new-project
touch projects/my-new-project/TODO_FOR_JULES.md
```

Format `TODO_FOR_JULES.md` as a markdown checklist:

```markdown
- [ ] Setup initial routing
- [ ] Implement user authentication
- [ ] Write unit tests for auth service
```

## Configuration

Click the **Settings (gear icon)** to configure:
- **Full Automation Mode**: Enable/disable autonomous task execution
- **Auto-Task Generation**: Automatically generate tasks from project analysis
- **Test Generation Prompts**: Configure AI behavior for test creation
- **API Keys**: Manage AI provider credentials

## Environment Variables

### Core Variables

| Variable | Service | Description |
|----------|---------|-------------|
| `JULES_API_KEY` | Go Backend | Google Jules API key |
| `GITHUB_TOKEN` | Go Backend | GitHub personal access token |
| `OPENAI_API_KEY` | Python Agent | OpenAI API key for deep agents |
| `GEMINI_API_KEY` | Python Agent | Google Gemini API key |
| `CLAUDE_API_KEY` | Python Agent | Anthropic Claude API key |

### Go Backend

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | 3847 | Go backend server port |
| `DATABASE_URL` | /data/orchestrator.db | SQLite database path |
| `PYTHON_SERVICE_URL` | http://python-agent:8080 | Python microservice URL |
| `LITELLM_PROXY_URL` | http://litellm:4000 | LiteLLM proxy URL (shared infra) |

### Python Agent

| Variable | Default | Description |
|----------|---------|-------------|
| `PYTHON_SERVICE_PORT` | 8080 | Python service port |
| `DEEP_AGENT_MODEL` | gpt-4o | Model for deep agent |
| `DEEP_AGENT_BASE_URL` | https://api.openai.com/v1 | OpenAI-compatible API base |

See `.env.example` for the full list of configurable variables.

## Docker Deployment

### Production

```bash
docker-compose -f docker-compose.go.yml up -d
```

This starts:
- **Nginx** (Port 80/443) - Reverse proxy and static file server
- **Go Backend** (Internal) - REST API server
- **Python Agent** (Internal) - Deep agent service

### Development

```bash
docker-compose -f docker-compose.dev.go.yml up -d
```

This starts:
- **Go Backend** (Port 3847) - With hot reload (air)
- **Python Agent** (Port 8080) - With hot reload
- **Frontend** (Port 5173) - Vite dev server

## Makefile Commands

```bash
# Development
make dev-up          # Start development environment
make dev-down        # Stop development environment
make logs            # View all logs
make logs-go         # View Go backend logs only
make logs-python     # View Python agent logs only

# Production
make prod-up         # Start production environment
make prod-down       # Stop production environment

# Build & Test
make build           # Build Go binary
make test            # Run Go tests
make lint            # Run Go linter
make test-coverage   # Tests with coverage report

# Database
make db-backup       # Backup SQLite database
make db-restore      # Restore from backup

# Docker
make docker-clean    # Clean Docker resources
make docker-rebuild  # Rebuild images without cache
make health          # Check service health
```

Run `make help` for all available commands.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Nginx (Port 80/443)                      │
│  - SSL Termination                                          │
│  - Load Balancing                                           │
│  - Static File Serving                                      │
└────────────────────┬────────────────────────────────────────┘
                     │
     ┌───────────────┴───────────────┐
     │                               │
     ▼                               ▼
┌─────────────┐              ┌─────────────┐
│ Go Backend  │◄────────────►│ Python      │
│ Port 3847   │  REST/HTTP   │ Agent       │
│             │              │ Port 8080   │
│ - REST API  │              │ - LangChain │
│ - Git Ops   │              │ - Deep      │
│ - Jules API │              │   Agents    │
│ - SQLite    │              │             │
└─────────────┘              └─────────────┘
```

## Performance

| Metric | Node.js | Go | Improvement |
|--------|---------|-----|-------------|
| **Cold Start** | 2.1s | 0.05s | **42x faster** |
| **Memory (idle)** | 85MB | 18MB | **4.7x less** |
| **Concurrent Requests** | 1200ms | 140ms | **8.5x faster** |
| **P99 Latency** | 2500ms | 180ms | **13.8x faster** |

## Documentation

- **[Go Backend Guide](go-backend/README.md)** - Complete architecture documentation
- **[Migration Guide](docs/MIGRATION.md)** - Migration strategy from Node.js
- **[Quick Reference](docs/QUICK_REFERENCE.md)** - Quick reference card

## License

MIT
