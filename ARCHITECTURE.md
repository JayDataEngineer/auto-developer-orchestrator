# System Architecture - Auto-Developer Orchestrator

## 🌊 Overview

The Auto-Developer Orchestrator is a high-performance system for autonomous code manipulation. It follows a **thick backend, thin frontend** pattern where most state lives in Go (SQLite) and business logic is split between Go (execution) and Python (intelligence).

---

## 🏗 Component Breakdown

### 1. Go Backend (`/go-backend`)
- **Language**: Go 1.24
- **Framework**: chi (REST API)
- **Database**: SQLite (local file persistence)
- **Key Responsibilities**:
  - API and routing
  - Git Operations (Hybrid: `go-git` for reads, CLI for writes)
  - Jules API dispatch and session polling
  - CLI command execution (safe/whitelisted)
  - Persistent state management (automation modes, task indices)
  - **OpenShell Sandbox Management** (AI agent isolation)

### 2. Pi Agent (Binary)
- **Engine**: `pi` CLI (Rust-based)
- **Interface**: RPC mode
- **Sandbox**: Runs inside OpenShell container (per-project isolation)
- **Key Responsibilities**:
  - Autonomous code exploration and manipulation
  - Intelligent task execution and PR generation
  - Context-aware file editing using the "Vibe Coder" engine
  - Real-time progress updates via RPC stdout
  - **Computer Use Mode**: On-demand desktop (VNC + Chrome) for visual tasks

### 3. Frontend (`/src`)
- **Language**: TypeScript + React 19
- **Style**: Tailwind CSS + Motion (Glassmorphism Industrial Aesthetic)
- **Key Responsibilities**:
  - Real-time terminal and agent progress display
  - Interactive project management
  - AI configuration and monitoring
  - Workflow visualization (Zen Mode)
  - **Desktop Viewer Popup**: Dual-pane CDP + VNC viewer for Computer Use Mode

### 4. OpenShell Sandbox Manager
- **Image**: `nvidia/openshell:latest`
- **Isolation Layers**:
  - **Filesystem**: Landlock LSM (read-only /usr, /etc, /bin; read-write /sandbox/workspace)
  - **Network**: Proxy enforcement (allowlist: GitHub, AI APIs, Docker registry)
  - **Process**: Seccomp BPF (block ptrace, mount, privilege escalation)
  - **Identity**: Non-root user (sandbox:sandbox)
- **Desktop Mode** (On-Demand):
  - Xvfb (virtual display)
  - x11vnc (VNC server)
  - Google Chrome (CDP-enabled)
  - noVNC (web-based viewer)
- **Resource Usage**:
  - CLI mode: ~50-100MB RAM, 1-3s startup
  - Desktop mode: ~500MB-1GB RAM, 10-30s startup

---

## 🔄 Core Workflows

### 1. Project Initialization
1. User enters repo URL → Go Backend → `git clone` to `/projects`.
2. Go Backend triggers `pi` scan on `/projects/[repo]` → Pattern detection.
3. Generated tasks stored in SQLite → Streamed to Frontend via SSE.

### 2. Autonomous Task Execution (Pi Agent)
1. User sends message in Agent View → Go Backend → Spawns `pi --mode rpc`.
2. `pi` analyzes codebase and proposes changes.
3. `pi` creates local branches and commits changes.
4. Go Backend monitors `pi` output → Streams updates to Frontend via SSE.

---

## ⚙️ Shared Infrastructure Integration

The orchestrator is designed to work as a control plane within a larger **LocalAI stack** (`shared-docker-infra`):

| Component | Shared Service |
|-----------|----------------|
| **Routing** | Traefik (orchestrator.local) |
| **Model Proxy** | LiteLLM (OpenAI-compatible) |
| **Observability** | Langfuse (Tracing) |
| **Load Balancer** | Traefik (Entrypoints http/https) |

---

## 🔒 Security Measures

- **CLI Whitelisting**: Only approved commands can be executed via the CLI handler.
- **Directory Isolation**: All Git/File operations are jailed within the `/projects` hierarchy.
- **Input Sanitization**: All user-provided paths and commands are strictly validated.
- **SSE Streaming**: Real-time feedback prevents "black box" execution.
