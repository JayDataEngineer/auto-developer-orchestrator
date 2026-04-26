# OpenClaw

**Repo**: `reference/openclaw/` — Personal AI assistant with multi-channel, multi-agent, and plugin architecture

## What It Is

A personal AI assistant you run on your own devices. Built on pi-mono's agent runtime (RPC mode), with a Gateway WebSocket control plane that connects 24+ messaging channels (WhatsApp, Telegram, Slack, Discord, Signal, iMessage, Matrix, Teams, etc.), voice wake + talk mode, a live Canvas, browser control, and a plugin ecosystem.

## Key Insights

### 1. Gateway Control Plane
- Single WebSocket server (`ws://127.0.0.1:18789`) as the backbone
- All clients (macOS app, CLI, WebChat, iOS/Android) connect via WS
- Session management, presence, config, cron, webhooks all through WS
- Clear protocol: `node.list`, `node.describe`, `node.invoke`, `sessions_patch`
- Tailscale Serve/Funnel for remote access

### 2. Multi-Channel Architecture
- 24+ messaging channels unified under one Gateway
- Per-channel DM pairing for security (pairing codes, allowlists)
- Group routing with mention gating, reply tags, activation modes
- Channel-agnostic agent: the model doesn't know or care which channel the message came from

### 3. Plugin/Extension Ecosystem
- Bundled workspace plugins + third-party plugin support
- `openclaw/plugin-sdk/*` as the only public cross-package contract
- Plugin types: channel plugins, provider plugins, tool plugins
- Skills registry (ClawHub) for community skill discovery and auto-install
- Strict boundary enforcement between core and plugins

### 4. Pi Agent Runtime (RPC Mode)
- Uses pi-mono's agent in RPC mode (stdin/stdout JSONL)
- Tool streaming and block streaming over RPC
- Session branches, forks, trees — all from pi's session model
- Separate agent instances per workspace

### 5. Node Model (Device Abstraction)
- macOS/iOS/Android each act as "nodes" with advertised capabilities
- `system.run` on macOS (with TCC permission handling)
- Camera, screen recording, location, notifications — all via `node.invoke`
- Permission maps advertised over WS, gate execution

### 6. Security Model
- DM pairing by default (unknown senders get pairing code)
- `dmPolicy` per channel: `pairing`, `open` (with allowlist)
- Sandbox modes: `main` (full access), `non-main` (Docker sandbox for groups)
- Tool allowlist/denylist per session type
- Doctor command for security audits

### 7. Session & Presence
- Typing indicators, presence states, session pruning
- Session tools: `sessions_list`, `sessions_history`, `sessions_send`
- Agent-to-agent communication across sessions
- Model failover and provider rotation

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| WebSocket SSE for streaming | `handlers/` | Agent responses stream over SSE, not WS |
| Session management | `llama/` package | Basic session scoping |
| Sub-agent delegation | `agent_loop.go` | `delegate_to` tool |
| Docker sandboxes | `sandbox/` package | Per-session sandbox isolation |
| Browser control | `browser/` package | CDP-based via chromedp |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P2 | Gateway-style control plane with WS | High | Would unify all interfaces (TUI, CLI, web) under one protocol |
| P2 | Plugin/extension architecture | High | pi-mono + openclaw show the power of extensibility |
| P2 | Skills registry / skill discovery | Medium | Community-driven capability expansion |
| P3 | Multi-channel support | High | Not our core use case (developer tool, not personal assistant) |
| P3 | Node model (device apps) | Very High | macOS/iOS/Android apps are a different product category |
| P3 | Voice wake / talk mode | High | Different use case |
| P3 | Canvas / A2UI | High | Visual workspace is interesting but not core |

### Key Architectural Insight
OpenClaw's cleanest insight for us: **the Gateway as a control plane**. Our backend is already close — it serves the agent loop, manages sessions and tools. Making the protocol richer (WS with typed events) and adding a plugin boundary would enable the TUI, CLI, and web frontend to be first-class consumers rather than afterthoughts.
