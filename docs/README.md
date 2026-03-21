# Documentation Index

## Core Documentation

| Document | Description |
|----------|-------------|
| [JULES.md](./JULES.md) | **必读** Technical reference for Google Jules (external AI service) |
| [PROJECT.md](./PROJECT.md) | Project overview and architecture |

## Quick Reference

### What is Jules?
Jules is **Google's external AI coding assistant** — NOT this application. See [JULES.md](./JULES.md) for full details.

### What is Auto-Developer Orchestrator?
This is a **local orchestration platform** that manages multiple repositories and coordinates between Jules and other AI providers.

### Architecture Summary
```
Local App (Port 3000) → External AI Services (Jules, Gemini, Claude, OpenAI)
                      → LangGraph Agent (Port 8123)
```

## Getting Started

1. Read [PROJECT.md](./PROJECT.md) for architecture overview
2. Read [JULES.md](./JULES.md) to understand Jules integration
3. See main [README.md](../README.md) for setup instructions
