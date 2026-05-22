# PUX - AiO Ai Harness Hub

Pux is a system crafted for maximum, streamlined control over the AI system that you use.

## Make Pux your own with Text Files

Through the WebUI, agents and orchestrators can be made on the fly. Get your agent just the way you like it.

Tasks can be specific, like coding, or something more generalized, such as a specialized, no code deep research pipeline.

Proudly unopinionated.

## Tooling to get the Job Done

Native to Pux is a sandboxed desktop, browser, text editor, and vision toolkit. This gives an edge to Pux when dealing with web development, or integrating it with tools such as telegram or game development.

There is a WebUI, as well as an early alpha release of the Terminal Interface.

Coming soon, scheduled jobs. This allows jobs to be run entirely in a safe, sandboxed environment.

## Stay Soverign at any Step

In the fast paced corporate world of today, it is easy to get 'locked in' to a certain provider. Issues with providers like cosumer-unfriendly billing practices by companies such as Google AI can be far behind, at your own discretion. You can use Opus for planning, and a local Qwen model for execution.

---

## 🛠 Architecture

- **Web Frontend**: A minimalist webui desinged to both manage and use the PUX system.
- **Go Backend**: As opposed to a pure Typescript or Python backedn, Pux is built on a contract system. This gives the TUI, WebUI, and backend a simple language to speak while keeping Go speed for heavy, rapid tool use.
- **Extensions**: An extension system desinged to allow more advanced functionality being added

---

## 🚀 Quick Start (Local)

### 1. Prerequisites
- **Node.js 22+**
- **Go 1.24+**
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
- [Changelog](docs/CHANGELOG.md) - Implementation history and status
- [Manifesto](MANIFESTO.md) - Project vision and philosophy
