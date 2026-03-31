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

### 2. Python Deep Agent Service (`/python-agent`)
- **Language**: Python 3.12 (uv managed)
- **Framework**: FastAPI
- **LLM SDK**: LangChain / LangGraph
- **Key Responsibilities**:
  - Code exploration and pattern matching
  - Intelligent TODO/checklist generation
  - Multi-agent workflows (Explorer, Implementer, Reviewer)
  - Test generation and code refinement

### 3. Frontend (`/src`)
- **Language**: TypeScript + React 19
- **Style**: Tailwind CSS + Motion (Glassmorphism Industrial Aesthetic)
- **Key Responsibilities**:
  - Real-time terminal display
  - Interactive project management
  - AI configuration and monitoring
  - Workflow visualization

---

## 🔄 Core Workflows

### 1. Project Initialization
1. User enters repo URL → Go Backend → `git clone` to `/projects`.
2. Python Agent scans `/projects/[repo]` → Pattern detection → Task generation.
3. Generated tasks stored in SQLite → Streamed to Frontend via SSE.

### 2. Autonomous Task Execution (Jules)
1. User clicks "Dispatch" → Go Backend → Jules API (POST `/sessions`).
2. Jules session starts → Go Polling Engine monitors status.
3. Jules creates PR → Go Backend updates state → User reviews and merges.
4. After merge, Go prompts "Run Post-Merge Diagnostics" → AI Test Generation.

### 3. Deep Agent Execution (Implementation)
1. Python Agent → Explorer finds target files.
2. Python Agent → Implementer generates code diffs.
3. Python Agent → Reviewer validates diffs against standards.
4. Go Backend applies changes via `git apply` / CLI.

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
