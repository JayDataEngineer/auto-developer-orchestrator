# Jules - Technical Reference

**Jules is Google's agentic, asynchronous AI coding assistant** — NOT this application.

This document clarifies what Jules is, so we never confuse it with the **Auto-Developer Orchestrator** (our app that integrates with Jules and other AI providers).

---

## Quick Summary

| Aspect | Jules | Auto-Developer Orchestrator (Our App) |
|--------|-------|---------------------------------------|
| **What it is** | Google's cloud-based AI coding agent | Local orchestration platform |
| **Provider** | Google | Self-hosted / Our project |
| **Execution** | Runs on Google Cloud VMs | Runs locally on your machine |
| **Access** | jules.google.com, CLI, API | http://localhost:3000 |
| **Purpose** | Autonomous task execution | Manage multiple repos, coordinate AI agents |

---

## 1. Technical Architecture

### Core Model
- **Powered by:** Gemini 2.5 Pro
- **Optimization:** Long-context windows for ingesting entire codebases

### Operational Mode
- **Asynchronous & Agentic:** Works independently in the background
- **Workflow:** You assign a task → Jules executes → Returns PR for review

### Infrastructure
- Clones your GitHub/GitLab repo into an **isolated Google Cloud VM**
- Analyzes, runs, and tests code safely
- Never affects your local machine until you approve changes

---

## 2. Core Capabilities

| Capability | Description |
|------------|-------------|
| **Task-Based Execution** | End-to-end tasks: refactoring, dependency updates, unit tests, bug fixes |
| **Planning & Reasoning** | Generates step-by-step plan before writing code |
| **Autonomous PR Generation** | Submits Pull Requests with full diff and change summary |
| **Proactive "Surface" Mode** | Responds to events (e.g., failed deployments) and proposes fixes automatically |

---

## 3. Integration & Interfaces

| Interface | Description |
|-----------|-------------|
| **Web UI** | Managed via [jules.google](https://jules.google) |
| **Jules CLI** | Start, stop, and monitor tasks from terminal |
| **Jules API** | Programmatically assign coding tasks to Jules |
| **Multimodal Output** | Can generate audio changelogs to narrate changes |

---

## 4. Comparison: Jules vs Traditional AI

| Feature | Traditional AI (Copilot/Cursor) | Google Jules |
|---------|--------------------------------|--------------|
| **Workflow** | Synchronous (Pair-programming) | Asynchronous (Delegation) |
| **Scope** | Line/Function completion | Full-task/Feature execution |
| **Interaction** | Real-time chat/IDE plugin | Web/CLI/API Dashboard |
| **Autonomy** | Suggests; you type | Plans and executes; you review |

---

## 5. Access & Tiers (2026)

| Tier | Task Limit | Features |
|------|------------|----------|
| **Free** | ~15 tasks/day | Basic Jules access |
| **Pro/Ultra** | 5x–20x limits | Higher quotas, Antigravity (multi-agent workflows) |

---

## 6. How Our App Integrates with Jules

The **Auto-Developer Orchestrator** is a local platform that:

1. **Manages multiple repositories** from a single dashboard
2. **Coordinates AI agents** including Jules, Gemini, Claude, OpenAI
3. **Tracks tasks** via `TODO_FOR_JULES.md` files
4. **Provides real-time terminal** for monitoring agent actions
5. **Handles local Git operations** and project scanning

### Integration Flow

```
┌─────────────────────────────────────────────────────────────┐
│  Auto-Developer Orchestrator (Local - Port 3000)            │
│  - Project dashboard                                        │
│  - Task management                                          │
│  - Multi-AI provider coordination                           │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ API Calls
                          ▼
┌─────────────────────────────────────────────────────────────┐
│  External AI Services                                       │
│  ┌─────────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐ │
│  │   Jules     │  │  Gemini  │  │  Claude  │  │ OpenAI   │ │
│  │  (Google)   │  │          │  │          │  │          │ │
│  └─────────────┘  └──────────┘  └──────────┘  └──────────┘ │
└─────────────────────────────────────────────────────────────┘
```

---

## Key Takeaway

> **Jules = External Google Cloud Service** (autonomous AI coding agent)
>
> **Auto-Developer Orchestrator = Our Local App** (orchestrates Jules + other AI providers)

Use Jules for **fire-and-forget tasks** where you define intent and Jules handles implementation, testing, and PR submission in an isolated cloud environment.

Use the Auto-Developer Orchestrator to **manage multiple projects**, track tasks, and coordinate between Jules and other AI providers from a single interface.
