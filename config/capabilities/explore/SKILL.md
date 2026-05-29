# Exploration Methodology

## Goal
Map the relevant parts of a codebase and return a condensed brief so other agents
can work without burning their own context on discovery.

## Workflow

1. **Scope**: Use `file_glob` to understand the directory tree. Focus on the subsystem
   relevant to the task — don't map the entire repo unless asked.
   ```
   file_glob pattern="internal/handlers/*.go"
   ```

2. **Find**: Use `file_grep` to locate key types, interfaces, and function signatures.
   ```
   file_grep pattern="func.*Handler.*http" path="internal/handlers/"
   file_grep pattern="type.*struct" path="internal/handlers/"
   ```

3. **Read with purpose**: Read the parts that matter for the task:
   - Package declaration and imports (first 20 lines)
   - Type definitions and interfaces — include FULL definitions
   - Function bodies that are relevant to the task — paste them verbatim
   - Skip implementation bodies only if they're clearly unrelated to the task

4. **Synthesize**: Return a structured brief (see output format below)

## Output Format

Always end with a `## Codebase Brief` section containing:

```
## Codebase Brief

### File Tree
internal/handlers/
  health.go       — HealthHandler, GET /health
  pux.go          — PuxHandler, routes /api/pux/*
  pux_sse.go      — SSE streaming helpers

### Key Types
- HealthHandler struct { db *Database }
- PuxHandler struct { llama Engine, sandbox *Manager, ... }

### Function Signatures & Bodies
```go
func NewHealthHandler(db *Database) *HealthHandler {
    return &HealthHandler{db: db}
}

func (h *HealthHandler) RegisterRoutes(r chi.Router) {
    r.Get("/health", h.HandleHealth)
}
```

### Patterns
- All handlers follow chi.Router pattern: r.Get("/path", h.Method)
- Responses are JSON via json.NewEncoder(w).Encode(...)
- Errors returned as {"error": "message", "success": false}
```

## Rules
- NEVER create, edit, or write files — read-only scout
- Keep the brief under 4000 words
- Focus on STRUCTURE and CODE — include actual function/type bodies, not summaries
- If the codebase is large, scope exploration to the relevant subsystem
- Always include the file tree first so downstream agents know what exists
