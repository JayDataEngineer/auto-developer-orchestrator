# OpenCode (now Crush)

**Repo**: `reference/opencode/` — Go-based TUI CLI for AI coding (archived, moved to [Crush](https://github.com/charmbracelet/crush))

## What It Is

A terminal-based AI coding assistant built in Go with Charm's Bubble Tea TUI framework. Multi-provider support (OpenAI, Anthropic, Gemini, Bedrock, Groq, etc.), session management via SQLite, LSP integration, and MCP support.

## Key Insights

### 1. Go + Bubble Tea TUI Architecture
- Clean separation: `cmd/` (Cobra), `internal/tui/`, `internal/llm/`, `internal/config/`
- Bubble Tea for viewport management, textarea input, overlapping dialogs
- Permission dialog system with keyboard shortcuts (a/A/d for allow/session-allow/deny)

### 2. Multi-Provider LLM Abstraction
- Provider config per model: API key management, disable/enable toggles
- Agent-specific model assignment (coder, task, title generators)
- Local endpoint support for self-hosted models
- `model` field per agent type, not one-size-fits-all

### 3. Session Management
- SQLite for persistence (migrations, schema evolution)
- Auto-compact: triggers at 95% context window usage, creates new session with summary
- Session switching: `Ctrl+A` dialog with previous/next navigation
- JSON export format for scriptability

### 4. Custom Commands + Named Arguments
- Markdown files in `~/.config/opencode/commands/` become slash commands
- Subdirectory support for organization (`git/commit.md` -> `user:git:commit`)
- `$NAME` placeholders prompt for values at runtime
- Separate `user:` and `project:` namespaces

### 5. LSP Integration
- Per-language config (gopls, typescript-language-server, etc.)
- `diagnostics` tool exposed to the LLM
- Full LSP client implementation (completions, hover, definition) — though only diagnostics exposed

### 6. MCP (Model Context Protocol)
- Stdio and SSE transports
- Permission-gated tool access
- Tool discovery from servers

### 7. Non-Interactive Mode
- `-p` flag for single-prompt scripting
- `-f json` for programmatic JSON output
- `-q` for silent/no-spinner mode
- Reuses the same agent loop, just without TUI

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| Go + Bubble Tea TUI | `go-backend/internal/cli/tui/` | app.go, sse_reader.go, styles.go, messages.go — directly inspired |
| SSE streaming in TUI | `sse_reader.go` | Streams thinking/tool calls/assistant text |
| Multi-model support | `llama/` package | Single model (llama.cpp) vs multi-provider |
| CLI scripting mode | `go-backend/internal/cli/cmd/` | `orch agent prompt` — like `opencode -p` |
| Chat commands (/help, /reset) | TUI | Similar model |
| File tools (read/edit/write/glob/grep) | `tool_registry.go` | Similar to OpenCode's tools |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P1 | LSP integration for code intelligence | Medium | Our `code_search` uses regex; LSP is more accurate |
| P2 | SQLite session persistence | Medium | Currently JSON-based; SQLite enables richer queries |
| P2 | MCP server support | Medium | Extensible tool ecosystem |
| P2 | Custom user commands with named args | Low | Power-user feature for repetitive workflows |
| P2 | Auto-compact at context threshold | Low | Would improve long session reliability |
| P3 | Permission dialog with session-wide allow | Low | Already implicit, could be more explicit |
| P3 | Multi-provider model selection | High | Our architecture is single-model by design |
