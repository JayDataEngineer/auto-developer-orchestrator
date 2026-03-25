# Taskfile Quick Reference

## Installation

If you don't have `task` installed:

```bash
# Go (recommended)
go install github.com/go-task/task/v3/cmd/task@latest

# Or using Homebrew (macOS)
brew install go-task

# Or using apt (Debian/Ubuntu)
sudo apt install task

# Or using snap
sudo snap install task --classic
```

## Quick Start

```bash
# Show all available tasks
task --list

# Run setup script
task setup

# Start production
task prod:up

# Start development
task dev:up

# Check health
task health
```

## Common Tasks

### Setup

| Task | Description |
|------|-------------|
| `task` | Show help |
| `task setup` | Run setup script |
| `task setup:check` | Check prerequisites |
| `task env` | Create `.env` from `.env.example` |

### Production

| Task | Description |
|------|-------------|
| `task prod:up` | Start production |
| `task prod:down` | Stop production |
| `task prod:restart` | Restart production |
| `task prod:logs` | View logs |
| `task prod:ps` | Check status |

### Development

| Task | Description |
|------|-------------|
| `task dev:up` | Start dev (hot reload) |
| `task dev:down` | Stop dev |
| `task dev:restart` | Restart dev |
| `task dev:logs` | View dev logs |
| `task dev:ps` | Check dev status |

### Shared Infrastructure

| Task | Description |
|------|-------------|
| `task infra:up` | Start shared-docker-infra |
| `task infra:down` | Stop shared-docker-infra |
| `task infra:litellm:up` | Start LiteLLM |
| `task infra:langfuse:up` | Start Langfuse |

### Health & Testing

| Task | Description |
|------|-------------|
| `task health` | Check all services |
| `task health:orchestrator` | Check orchestrator |
| `task health:litellm` | Check LiteLLM |
| `task health:langfuse` | Check Langfuse |
| `task test:litellm` | Test LiteLLM |
| `task test:orchestrator` | Test orchestrator API |
| `task test:e2e` | Run end-to-end test |

### Cleanup

| Task | Description |
|------|-------------|
| `task clean` | Clean Docker resources |
| `task clean:all` | Remove everything |
| `task clean:volumes` | Remove volumes |
| `task clean:images` | Remove images |

### Database

| Task | Description |
|------|-------------|
| `task db:backup` | Backup SQLite database |
| `task db:restore` | Restore from backup |

### Utilities

| Task | Description |
|------|-------------|
| `task shell:go` | Shell in Go container |
| `task shell:python` | Shell in Python container |
| `task restart` | Restart all |
| `task up` | Start production |
| `task down` | Stop all |
| `task ps` | Check status |
| `task logs` | View logs |

### Browser Shortcuts

| Task | Description |
|------|-------------|
| `task open:orchestrator` | Open orchestrator dashboard |
| `task open:langfuse` | Open Langfuse dashboard |
| `task open:litellm` | Open LiteLLM |
| `task open:traefik` | Open Traefik dashboard |
| `task open:all` | Open all dashboards |

## Workflow Examples

### First Time Setup

```bash
task setup:check
task env
# Edit .env with your API keys
task prod:up
task health
```

### Daily Development

```bash
task dev:up
task open:orchestrator
# ... work ...
task dev:down
```

### Troubleshooting

```bash
task health
task logs
task test:e2e
```

### Cleanup

```bash
task clean
```

## Makefile Compatibility

If you prefer `make`, the Taskfile is compatible with common Makefile patterns:

```bash
# Instead of: make prod-up
task prod:up

# Instead of: make dev-logs
task dev:logs

# Instead of: make health
task health
```

## Taskfile vs Make

| Feature | Make | Task |
|---------|------|------|
| Syntax | Makefile | YAML |
| Cross-platform | ❌ (Unix-only) | ✅ (Go binary) |
| Nested tasks | ❌ | ✅ |
| Variable interpolation | Limited | Full |
| Documentation | Manual | Built-in (`--list`) |

## More Info

- [Task Documentation](https://taskfile.dev)
- [Taskfile.yml](../Taskfile.yml)
