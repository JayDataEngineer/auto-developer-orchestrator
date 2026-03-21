# Architecture Evolution

This document captures the **original vision** and **evolution** of the Auto-Developer Orchestrator.

---

## Original Vision (Game Plan)

### Overview
A Dockerized, end-to-end orchestration platform for managing AI coding tasks via GitHub.
The primary goal: **sequentially feed tasks from a markdown checklist (`TODO_FOR_JULES.md`) to Jules** (Google's AI coding agent).

**Target Ingestion:** Designed to be built rapidly using Google Stitch UI + robust backend API.

---

## Original Tech Stack

| Component | Technology | Purpose |
|-----------|------------|---------|
| **Frontend** | React + Tailwind CSS | Optimized for Google Stitch generation |
| **Backend** | **Go (Golang)** | Ultra-low latency, robust concurrency for git operations |
| **AI Gateway** | **LiteLLM** | Parse/generate/validate `TODO_FOR_JULES.md` files |
| **Deployment** | Docker Compose | Containerize Go binary, Node.js, git, gh CLI tools |

### Original Docker Architecture (3 Services)
```yaml
- backend:     Go API server (Port 8080)
- frontend:    Node/Nginx static file server
- litellm:     Local LLM proxy for checklist generation
```

---

## Original Operational Modes

### A. Full Auto Mode ("Set and Forget")
**Best for:** Fresh, un-forked, or low-risk projects

**Flow:**
1. Go Backend parses `TODO.md`
2. Identifies first pending task
3. Auto syncs git state (`git add .`, `git commit`, `git push`)
4. Creates GitHub Issue labeled for Jules
5. Polls PR status via GitHub API
6. Auto-merges PR when GitHub Actions/Tests pass
7. Unspools next task instantly

### B. Manual / Stepped Mode
**Best for:** Complicated, high-risk codebases

**Flow:**
1. Syncs git state automatically
2. Creates Issue for Jules with refined LLM prompts via LiteLLM
3. **PAUSES** when Jules opens PR
4. React UI shows "Review Stage" with:
   - Deep links to Jules Web UI (jules.google.com)
   - GitHub PR link
5. User clicks "Approve & Merge" → merges code → pulls latest → queues next task

---

## Original API Design (Go Backend - Port 8080)

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/status` | GET | Return git state, working tree, Auto/Manual toggle |
| `/api/checklist` | GET | Return parsed AST of `TODO_FOR_JULES.md` |
| `/api/dispatch` | POST | Trigger creation of next GitHub Issue |
| `/api/settings/mode` | POST | Toggle Auto/Manual mode |

---

## Original UI Views (for Google Stitch)

### 1. Dashboard Home
- **Dual-panel interface:**
  - Left: Raw Markdown checklist with checkboxes
  - Right: "Current Task" card with real-time logs from Go backend

### 2. Mode Toggle Ribbon
- Sticky header with glossy switch
- Toggle: "Manual Verification" ↔ "Full Auto Developer"

### 3. Action Modal (Manual Mode)
- Popup: "Jules has finished. Tests are passing."
- Buttons:
  - `[View in Jules Web UI]`
  - `[Merge & Proceed]`

---

## Evolution: What Changed

### Then vs Now

| Aspect | Original Vision | Current Implementation |
|--------|-----------------|------------------------|
| **Backend** | Go (Golang) | **Node.js + Express + TypeScript** |
| **AI Gateway** | LiteLLM container | **deepagentsjs** (LangChain) |
| **Deep Agents** | Not in original plan | **JavaScript deepagents** for TODO generation |
| **Docker Services** | 3 (backend, frontend, litellm) | **1** (Node.js app only) |
| **Ports** | 8080 (Go) | **3847** (Node.js) |
| **AI Integration** | Jules via GitHub Issues | **Jules + Gemini + Claude + OpenAI** |
| **TODO Generation** | LiteLLM parsing | **Deep Agent auto-generates TODOs** |

---

## What Stayed True to Vision

✅ **Core Purpose:** Orchestrate AI coding tasks via GitHub  
✅ **Jules Integration:** Feed tasks to Jules (Google's AI agent)  
✅ **TODO_FOR_JULES.md:** Central task checklist format  
✅ **Dockerized:** Single-command deployment  
✅ **Dual Modes:** Autonomous + Manual oversight  
✅ **Multi-Repo Support:** Manage multiple projects from one dashboard  

---

## Key Innovations Added

🆕 **Deep Agents Integration**
- Automatic TODO generation via `deepagentsjs`
- Agent analyzes codebase → creates `TODO_FOR_JULES.md` → feeds to Jules

🆕 **Multi-AI Provider Support**
- Not just Jules: Gemini, Claude, OpenAI integration
- Flexible AI routing based on task type

🆕 **Simplified Architecture**
- Single Node.js service instead of Go + Nginx + LiteLLM
- Easier development with hot reload

🆕 **Client-Side Deep Agents**
- No Python microservice needed
- `deepagentsjs` runs directly in Node.js environment

---

## Current Architecture (Simplified)

```
┌─────────────────────────────────────────────────────────────┐
│                    React Frontend                           │
│                     (Port 3847)                             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│              Express Backend (TypeScript)                   │
│  - Project management                                       │
│  - Task coordination                                        │
│  - Deep Agent TODO generation                               │
│  - AI provider routing                                      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│   External AI Services                                      │
│   - Google Jules (cloud) ← Original Vision                  │
│   - Gemini API                                                │
│   - Claude API                                                │
│   - OpenAI API                                                │
│   - deepagentsjs (local) ← New Innovation                   │
└─────────────────────────────────────────────────────────────┘
```

---

## Original Docker Compose (Never Implemented)

```yaml
# This was the ORIGINAL plan - never implemented
services:
  backend:
    image: golang:1.21
    command: go run main.go
    ports:
      - "8080:8080"
    volumes:
      - ~/.ssh:/root/.ssh
      - ~/.gitconfig:/root/.gitconfig
      - ./workspace:/workspace

  frontend:
    build: ./frontend
    ports:
      - "3000:80"

  litellm:
    image: ghcr.io/berriai/litellm:main-latest
    ports:
      - "4000:4000"
```

---

## Current Docker Compose (Single Service)

```yaml
# What we have NOW - simplified!
services:
  app:
    build:
      context: .
      dockerfile: Dockerfile.dev
    ports:
      - "3847:3847"
    volumes:
      - .:/app
      - ~/.ssh:/root/.ssh
      - ~/.gitconfig:/root/.gitconfig
    environment:
      - NODE_ENV=development
      - GEMINI_API_KEY=${GEMINI_API_KEY}
      - CLAUDE_API_KEY=${CLAUDE_API_KEY}
      - OPENAI_API_KEY=${OPENAI_API_KEY}
```

---

## Lessons Learned

1. **Node.js > Go for this use case**
   - Easier AI SDK integration (LangChain, deepagentsjs)
   - Faster iteration with hot reload
   - Single language stack (TypeScript everywhere)

2. **Deep Agents changed everything**
   - Original: Parse existing TODOs
   - Now: **Auto-generate TODOs** from codebase analysis

3. **Simplicity wins**
   - 3 services → 1 service
   - Less orchestration overhead
   - Faster development

4. **Jules is external, not integrated**
   - Original: Deep GitHub integration with Issues
   - Now: Jules is a **cloud service** we call via API

---

## Future Considerations

- [ ] Revisit Go backend if performance becomes critical
- [ ] Add LiteLLM for unified AI provider interface
- [ ] Implement original "Full Auto Mode" loop with GitHub polling
- [ ] Build the "Review Stage" modal for Manual Mode
- [ ] Add GitHub Actions integration for auto-merge on test pass

---

## Summary

**Original Vision:** Go-based, LiteLLM-powered, 3-service Docker orchestration for feeding TODOs to Jules via GitHub Issues.

**Current Reality:** Node.js/TypeScript single-service app with deepagentsjs for automatic TODO generation, supporting multiple AI providers including Jules.

**Core Mission Unchanged:** Orchestrate AI coding agents to autonomously improve codebases.
