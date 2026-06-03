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

## 🚀 Quick Start

### 1. Prerequisites
- **Go 1.26+**
- **Bun** (for TUI)
- **Docker** (optional, for sandboxes and local model server)
- **Task** ([taskfile.dev](https://taskfile.dev)) — `sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b ~/.local/bin`

### 2. Setup
```bash
git clone https://github.com/JayDataEngineer/auto-developer-orchestrator.git
cd auto-developer-orchestrator
task install
```

### 3. Run
```bash
task dev     # Start Go backend + Vite frontend
task chat    # Start TUI
```

Access the web UI at **http://localhost:5174**. Then add a provider (OpenRouter, local llama-server, etc.) through the model picker — no config files needed.

---

## 🏗 Build System (Taskfile)

The project uses **[Task](https://taskfile.dev)** for all operations. Run `task --list` to see everything available.

| Command | Description |
|---------|-------------|
| `task install` | Install all dependencies (JS, Go) |
| `task dev` | Start Go backend (3847) + Vite frontend (5174) |
| `task chat` | Start the TUI (terminal interface) |
| `task build` | Build server and CLI binaries |
| `task down` | Stop everything — backend, frontend, sandboxes |
| `task model` | Start local llama-server (requires NVIDIA GPU) |
| `task test-go` | Run Go unit tests |
| `task test-e2e` | Run Playwright E2E tests |
| `task infra-check` | Check health of shared infrastructure |

---

## 📂 Project Structure

- `/go-backend` — Go API server, agent loop, orchestrator, tool registry
- `/ts-tui-ink` — Terminal UI (React 19 + Ink 6 + @assistant-ui/react-ink)
- `/src` — Web frontend (Vite + React)
- `/shared` — Shared package (`@pux/shared`): PuxChatAdapter, Zustand store, SSE types
- `/config` — Kernel config: CTO prompt, employee roles, tool packages, worker definitions

---

## 📝 Documentation

- [Architecture Guide](ARCHITECTURE.md) - Deep dive into system design
- [Changelog](docs/CHANGELOG.md) - Implementation history and status
- [Manifesto](MANIFESTO.md) - Project vision and philosophy
