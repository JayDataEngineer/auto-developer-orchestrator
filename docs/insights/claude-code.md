# Claude Code

**Source**: `reference/claude-code/`, `reference/claude-code-intel/CLAUDE_CODE_ARCHITECTURE.md`,
`reference/claude-code-leak/` — Anthropic's production AI coding agent

## What It Is

Anthropic's internal/production coding agent. 184 tool modules, 40 distinct tools, 6 built-in sub-agents, and a sophisticated system prompt architecture. The reference study is based on `claw-code-parity` — a clean-room Python/Rust rewrite that reverse-engineers the architecture.

## Key Insights

### 1. System Prompt Structure
Ordered, composable sections:
1. **Intro** — role definition
2. **Style** — tone, spacing, output constraints
3. **System** — tool execution rules, system reminders, prompt injection defense
4. **Tasks** — guidelines for code changes, security, verification
5. **Actions** — reversibility, blast radius
6. **Environment Context** — model, cwd, date, platform
7. **Project Context** — git status, instruction files
8. **CLAUDE.md/AGENTS.md** — merged instruction files (max 4K chars/file, 12K total)
9. **Runtime Config** — settings
10. **Plugins/Skills** — appended sections

### 2. Tool System (184 modules, 40 distinct tools)
- **BashTool** — 18 submodules (permissions, security, sandbox, sed validation, mode validation)
- **FileRead/Edit/Write** — offset/limit reads, old/new string replacement, replace_all
- **Glob/Grep** — file pattern matching and ripgrep-style search
- **WebFetch/WebSearch** — URL content and web search
- **LSPTool** — goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol
- **Task Management** — create, list, update, stop, team coordination
- **MCP** — tool proxy, OAuth flow, stdio/sse/http/ws transports

### 3. Sub-Agent Architecture
6 built-in sub-agents:
- `exploreAgent` — codebase exploration
- `generalPurposeAgent` — general coding tasks
- `planAgent` — planning and architecture
- `verificationAgent` — testing/verification
- `claudeCodeGuideAgent` — help/documentation
- `statuslineSetup` — IDE status line

Each agent is forked, has its own memory, and can be resumed.

### 4. Permission System
- **ReadOnly** — can only read files and search
- **WorkspaceWrite** — can modify workspace files
- **DangerFullAccess** — full system access
- **Prompt** — requires approval for dangerous ops
- Permission handlers: coordinator, interactive, swarm worker
- Rule-based allow/deny/ask with hook system

### 5. Memory Architecture
- Typed categories: User Preferences, Project Facts, Feedback, Reference
- autoDream: background memory consolidation engine (forked subagent)
- Memory sections with deduplication by content hash

### 6. Session Management
- JSON files in `.port_sessions/`
- `max_turns`: 8, `max_budget_tokens`: 2000, `compact_after_turns`: 12
- TranscriptStore with append, compact, replay, flush

### 7. Advanced Features
- **KAIROS**: privileged access mode
- **ULTRAPLAN**: remote Opus planning (30 min time budget)
- **COORDINATOR_MODE**: multi-agent orchestration

## What We've Implemented

| Feature | Where | Notes |
|---------|-------|-------|
| File tools (read before edit) | `tool_registry.go:44,580`, `agent_loop.go:750` | Exact Claude Code pattern |
| Memory sections (typed categories) | `memory.go:71` | Preferences, Facts, Feedback, Reference |
| Project context loading | `persona_prompts.go:37` | AGENTS.md/CLAUDE.md from ancestor chain |
| Sub-agent delegation | `agent_loop.go` | `delegate_to` tool |
| Code search tools | `code_search.go:39` | find_references, find_definition, list_symbols, hover |
| System prompt structure | `persona_prompts.go` | Similar layered approach |
| Turn-based agent loop | `agent_loop.go` | max_turns, timeout |
| Context compaction | `agent_loop.go` | 4 turns -> keep 2 |

## Gaps

| Priority | Feature | Effort | Why |
|----------|---------|--------|-----|
| P1 | LSP integration (not just regex/ripgrep) | Medium | More reliable than grep for code understanding |
| P1 | autoDream-style background memory consolidation | Medium | Would improve long-term context |
| P2 | Permission modes (ReadOnly/Workspace/Full) | Medium | Security surface important for shared use |
| P2 | Tool hooks system (pre/post execution) | Medium | Enable extension without modifying core |
| P2 | COORDINATOR_MODE multi-agent orchestration | High | Advanced, but powerful for large tasks |
| P3 | BashTool security submodules (sed validation, etc.) | High | Our bash runs in Docker sandbox, less critical |
| P3 | Task management with cron/team coordination | High | Not our core use case |
