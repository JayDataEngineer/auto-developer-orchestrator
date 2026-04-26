# Pi (pi-mono)

**Repo**: `reference/pi-mono/` — Minimal terminal coding agent by Mario Zechner. OpenClaw's agent runtime is built on pi.

## What It Is

A "minimal terminal coding harness" — aggressively extensible through TypeScript extensions, skills, prompt templates, and themes. Deliberately avoids baking in features (no sub-agents, no plan mode, no MCP, no permission popups) and instead provides an extension API so users build what they want. Runs in four modes: interactive TUI, print/JSON, RPC (for process integration), and SDK (for embedding).

## Key Insights

### 1. Extensibility Over Features
- **"Adapt pi to your workflows, not the other way around"**
- Extensions API: `registerTool`, `registerCommand`, event hooks (`tool_call`, `stop`, etc.), UI components
- Skills: Agent Skills standard (SKILL.md) with auto-loading
- Prompt templates: `/name` expansion with `{{placeholders}}`
- Themes: JSON files, hot-reload on change
- Pi Packages: bundle and share extensions/skills/themes via npm or git

### 2. Deliberate Non-Features
Philosophy of refusing to build what extensions can do:
- **No MCP** — "Build CLI tools with READMEs, or build an extension that adds MCP support"
- **No sub-agents** — "Spawn pi instances via tmux, or build your own with extensions"
- **No permission popups** — "Run in a container, or build your own confirmation flow"
- **No plan mode** — "Write plans to files, or build it with extensions"
- **No built-in to-dos** — "They confuse models. Use a TODO.md file"

This keeps the core aggressively small and forces the ecosystem to fill gaps.

### 3. Session Tree Model
- Sessions as JSONL files with tree structure (id, parentId) — not linear history
- In-place branching: `/tree` to jump to any point, `/fork` to create new session branch
- No copying files for branches — single JSONL with parent pointers
- Compaction: lossy summarization of older messages, full history preserved in JSONL

### 4. RPC Mode
- stdin/stdout JSONL for non-Node.js integration
- OpenClaw uses this to embed the pi agent inside their Gateway
- Enables any language (Go, Rust, Python) to drive the agent
- Strict LF-delimited JSONL framing

### 5. Provider/Model Abstraction
- Unified multi-provider API (OpenAI, Anthropic, Google, Bedrock, Mistral, Groq, etc.)
- Subscription support (OAuth) + API key support
- Provider interface: `stream<Provider>()` returns `AssistantMessageEventStream`
- Standardized events: `text`, `tool_call`, `thinking`, `usage`, `stop`
- Model cycling: `Ctrl+P`/`Shift+Ctrl+P` to cycle scoped models

### 6. Message Queue (Steering)
- Submit messages while agent is working
- **Steering** (Enter): delivered after current tool execution finishes
- **Follow-up** (Alt+Enter): delivered only after agent finishes all work
- Abort restores queued messages to editor

### 7. Context Files
- `AGENTS.md` / `CLAUDE.md` loaded from: `~/.pi/agent/`, parent dirs (walking up), cwd
- `SYSTEM.md` for replacing system prompt
- `APPEND_SYSTEM.md` for appending without replacing

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| Bubble Tea TUI | `go-backend/internal/cli/tui/` | Go version, but same TUI framework concept |
| AGENTS.md/CLAUDE.md loading | `persona_prompts.go:37` | Walk up from cwd |
| Session management | `llama/` package | Basic model, not tree-based |
| Sub-agent delegation | `agent_loop.go` | Via `delegate_to` |
| File tools (read/write/edit) | `tool_registry.go` | Same basic set |
| Bash execution | Sandbox package | Docker-based, similar to pi's sandbox concept |
| SSE streaming | `handlers/` | Similar to pi's streaming events |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P1 | Extension API for custom tools | Medium | Pi's single best feature. Let users add tools without modifying core |
| P1 | Skill system (SKILL.md auto-loading) | Medium | Agent Skills standard is a converging ecosystem pattern |
| P2 | Session tree model (branching/forking) | Medium | Better than linear history for exploration |
| P2 | Prompt templates with placeholders | Low | Power-user productivity |
| P2 | Message queue / steering | Medium | Submit follow-ups while agent works |
| P3 | Pi Packages (npm/git sharing) | High | Community ecosystem if we ever open up |
| P3 | RPC mode for language-agnostic integration | Medium | Would let Python/TS frontends drive the agent |
| P3 | Theme system with hot-reload | Low | Nice-to-have UX polish |

### Key Architectural Insight
Pi's philosophy of "extensibility over features" is the strongest takeaway. Our `delegate_to` and tool registry are already halfway there — making them a true extension API (with hooks, custom UI, event handlers) would be more impactful than adding more built-in tools.
