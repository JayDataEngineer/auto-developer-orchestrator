# Auto-Developer Orchestrator

The ultimate command center for AI-powered software engineering. A high-performance polyglot orchestrator that manages autonomous agents (Jules, Deep Agents) to explore, implement, and review code.

---

## 🛠 Architecture

- **Frontend**: React 19 + Vite + Tailwind CSS (Industrial UI)
- **Backend**: Go 1.24 (High-performance API + Git Ops + SQLite)
- **AI Service**: Python FastAPI (Deep Agent workflows via LangChain/uv)
- **Agents**: Google Jules (Cloud-based) + Local Deep Agents (Explorer, Implementer, Reviewer)

---

## 🚀 Quick Start (Local)

### 1. Prerequisites
- **Node.js 22+**
- **Go 1.24+**
- **Python 3.12+** (with `uv` installed)
- **Docker & Docker Compose** (optional, recommended for full infra)

### 2. Setup
```bash
# Clone and install dependencies
git clone https://github.com/JayDataEngineer/auto-developer-orchestrator.git
cd auto-developer-orchestrator
make install

# Configure environment
cp .env.example .env
# Edit .env with your JULES_API_KEY and GITHUB_TOKEN
```

### 3. Run
```bash
# Start the full stack locally (Native)
make dev

# OR start with Docker (Recommended for Traefik/LiteLLM/Langfuse integration)
make up
```
Access at **http://localhost:5174** (Native) or **http://orchestrator.local** (Docker).

---

## 🏗 Build System (Makefile)

The project uses a unified **Makefile** for all operations:

| Command | Description |
|---------|-------------|
| `make install` | Install all dependencies (JS, Go, Python) |
| `make dev` | Start everything locally (Native) |
| `make up` | Start Docker development environment |
| `make down` | Stop Docker environment |
| `make test` | Run all unit and E2E tests |
| `make lint` | Run all code formatters/linters |
| `make db-backup` | Backup the SQLite database |
| `make infra-check` | Check health of shared-docker-infra |

---

## 📂 Project Structure

- `/go-backend`: Go API server and business logic
- `/python-agent`: Python FastAPI service for complex AI workflows
- `/src`: React frontend source code
- `/projects`: Local storage for cloned repositories
- `/data`: Persistent storage for SQLite database and logs

---

## 📝 Documentation

- [Architecture Guide](ARCHITECTURE.md) - Deep dive into system design
- [Jules API Reference](docs/JULES.md) - Integration with Google Jules
- [Changelog](docs/CHANGELOG.md) - Implementation history and status
- [Manifesto](MANIFESTO.md) - Project vision and philosophy
