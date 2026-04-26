# Insights from Reference Projects

Tracking what we've learned and adopted from each external project we study. Each file documents the key ideas, what we've already implemented, and the remaining gaps.

## The Projects

| Project | Focus | Status |
|---------|-------|--------|
| [browser-use](./browser-use.md) | CDP browser automation — hybrid element detection + watchdog architecture | 50% adopted |
| [claude-code](./claude-code.md) | Production agent architecture — tools, memory, sub-agents, system prompts | 60% adopted |
| [opencode](./opencode.md) | Go TUI CLI — Bubble Tea, SSE streaming, multi-provider | Heavily inspired |
| [openclaw](./openclaw.md) | Gateway + multi-channel + plugin architecture | Reference only |
| [pi-mono](./pi-mono.md) | Rust concept coding agent — TUI, extensions, skills, RPC mode | Architectural inspiration |
| [stagehand](./stagehand.md) | Browser toolkit — action caching, self-healing, observe/extract/act | 40% adopted |
| [agent-s](./agent-s.md) | GUI grounding — reflection, cycle detection, flat loops beat hierarchies | 30% adopted |
| [cua](./cua.md) | Computer use agent — coordinate normalization, callbacks, model loops | 40% adopted |
| [omniparser](./omniparser.md) | Vision screen parsing — YOLO + OCR + Florence-2 | 0% adopted |
| [os-symphony](./os-symphony.md) | Multi-agent desktop — routing, text span agent, verification | 10% adopted |
| [open-computer-use](./open-computer-use.md) | Desktop automation — fallback chains, CV detection, anti-detection | 10% adopted |
| [gemini-cli](./gemini-cli.md) | CLI agent — hierarchical memory, event-driven scheduler | 0% adopted |
| [computer-use-preview](./computer-use-preview.md) | Minimal computer use — pure coordinates, 3-turn screenshot limit | 50% adopted |

## Cross-Cutting Themes

1. **Flat loops > deep hierarchies** — Agent-S S3 proved this (72.6% OSWorld). Every reference converged here.
2. **Coordinate normalization is essential** — 0-1000 (Google/CUA) or 0-1 (OmniParser). Raw pixels break on resolution changes.
3. **Multi-method element detection wins** — DOM + AX tree + vision > any single approach. browser-use/Stagehand prove it.
4. **Screenshot preprocessing matters** — Resize, compress, limit history. Don't send raw full-res to the LLM.
5. **Memory should be hierarchical** — Project-scoped, session-scoped, not just ephemeral.
6. **Self-healing and caching are table stakes** — Stagehand + browser-use show clear patterns.
7. **Skills/plugins > monolithic features** — pi-mono, openclaw, claude-code all converge on extensibility.
