# Deep Agent Architecture

## Overview

The Auto-Developer Orchestrator uses a **polyglot architecture** with Go backend orchestration and Python deep agent services.

```
┌─────────────────────────────────────────────────────────────┐
│                      Frontend (React)                        │
│  http://localhost:5174                                       │
│  - Displays terminal output                                  │
│  - Shows TODO checklist                                      │
│  - Receives SSE stream                                       │
└──────────────────────┬──────────────────────────────────────┘
                       │ HTTP POST /api/ai/agent-checklist
                       │ Content-Type: text/event-stream (SSE)
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                  Go Backend (Port 3847)                      │
│  /go-backend/internal/handlers/checklist.go                  │
│                                                              │
│  GenerateChecklistStream():                                  │
│  1. Validates request (project, prompt)                      │
│  2. Gets project directory from SQLite DB                    │
│  3. Calls Python service via HTTP                            │
│  4. Streams SSE events from Python → Frontend                │
│  5. Handles errors gracefully                                │
└──────────────────────┬──────────────────────────────────────┘
                       │ HTTP POST http://localhost:8080/api/v1/checklist/generate
                       │ Content-Type: application/json
                       │ Body: { project_path, prompt }
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              Python Agent (Port 8080)                        │
│  /python-agent/main.py                                       │
│                                                              │
│  generate_checklist():                                       │
│  1. Receives project_path and prompt                         │
│  2. Calls generate_todos() with streaming                    │
│  3. Yields SSE events:                                       │
│     - log: Progress messages                                 │
│     - on_tool_start: Tool execution                          │
│     - subagent_call: Explorer subagent                       │
│     - todo_added: New TODO generated                         │
│     - complete: Analysis finished                            │
└──────────────────────┬──────────────────────────────────────┘
                       │ LangChain deepagents API
                       ▼
┌─────────────────────────────────────────────────────────────┐
│              LangChain DeepAgents                            │
│  /python-agent/deep_agent.py                                 │
│                                                              │
│  create_todo_agent():                                        │
│  - Main agent: todo_generator                                │
│  - Subagent: explorer (code exploration)                     │
│  - Model: OpenAI GPT-4o (or compatible)                      │
│  - Backend: LocalShellBackend (filesystem access)            │
│                                                              │
│  generate_todos():                                           │
│  - Streams agent events in real-time                         │
│  - Uses write_todos tool for structured output               │
│  - Writes TODO_FOR_JULES.md file                             │
└─────────────────────────────────────────────────────────────┘
```

## Request Flow

### 1. Frontend → Go Backend

**Endpoint:** `POST /api/ai/agent-checklist`

**Request:**
```json
{
  "project": "my-project",
  "prompt": "Analyze the codebase and generate improvement tasks"
}
```

**Response:** SSE Stream (`text/event-stream`)
```
data: {"event": "log", "message": "DEEP AGENT: Initializing..."}
data: {"event": "on_tool_start", "name": "read_file"}
data: {"event": "todo_added", "todo": {"id": "task-1", "content": "..."}}
data: {"event": "complete", "message": "Analysis complete!"}
```

### 2. Go Backend → Python Service

**Endpoint:** `POST http://localhost:8080/api/v1/checklist/generate`

**Request:**
```json
{
  "project_path": "/path/to/project",
  "prompt": "Analyze the codebase..."
}
```

**Response:** SSE Stream (forwarded to frontend)

### 3. Python Service → LangChain

**Call:** `generate_todos(project_path, prompt, streaming=True)`

**Events:**
- `tool_start`: Agent is using a tool
- `log`: Progress messages
- `subagent_call`: Explorer subagent delegated
- `todo_added`: New TODO item generated
- `error`: Error occurred
- `complete`: Analysis finished

## Components

### Go Backend (`checklist.go`)

```go
// GenerateChecklistStream handles SSE streaming
func (h *ChecklistHandler) GenerateChecklistStream(w http.ResponseWriter, r *http.Request) {
    // 1. Validate request
    // 2. Get project directory from DB
    // 3. Call Python service
    // 4. Stream SSE events to frontend
}
```

**Key features:**
- SSE streaming support
- Error handling
- Timeout (120 seconds)
- Database integration

### Python Service (`main.py`)

```python
@app.post("/api/v1/checklist/generate")
async def generate_checklist(request: ChecklistRequest) -> StreamingResponse:
    async def event_generator():
        async for event in generate_todos(...):
            yield format_sse_event(event)
    
    return StreamingResponse(event_generator(), media_type="text/event-stream")
```

**Key features:**
- FastAPI async endpoints
- StreamingResponse for SSE
- Event formatting

### Deep Agent (`deep_agent.py`)

```python
def create_todo_agent():
    todo_agent = createDeepAgent(
        name="todo_generator",
        model=ChatOpenAI(model="gpt-4o"),
        system_prompt=TODO_GENERATOR_PROMPT,
        subagents=[explorer_subagent],
        backend=LocalShellBackend(root_dir=os.getcwd()),
    )
    return todo_agent
```

**Key features:**
- LangChain deepagents integration
- Explorer subagent for code analysis
- LocalShellBackend for filesystem access
- write_todos tool for structured output

## Configuration

### Environment Variables

**Go Backend:**
```bash
PYTHON_SERVICE_URL=http://localhost:8080
DATABASE_URL=/data/orchestrator.db
```

**Python Service:**
```bash
DEEP_AGENT_MODEL=gpt-4o
DEEP_AGENT_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=sk-...
PYTHON_SERVICE_PORT=8080
```

## Testing

### Go Handler Tests

```bash
cd go-backend
go test ./internal/handlers/... -v -run TestAIHandler
```

**Tests:**
- `GenerateTests` - Validates AI test generation endpoint
- `RunTests` - Validates test execution endpoint

### Integration Test

```bash
# Start Python service
cd python-agent && uvicorn main:app --reload

# Start Go backend
cd go-backend && go run cmd/server/main.go

# Test endpoint
curl -X POST http://localhost:3847/api/ai/agent-checklist \
  -H "Content-Type: application/json" \
  -d '{"project": "test", "prompt": "Analyze"}'
```

## Frontend Integration

The React frontend receives SSE stream and displays:

```typescript
const res = await fetch('/api/ai/agent-checklist', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ prompt, project: selectedProject })
});

const reader = res.body.getReader();
while (!done) {
  const { value } = await reader.read();
  const chunk = decoder.decode(value);
  
  // Parse SSE events
  for (const line of chunk.split('\n\n')) {
    if (line.startsWith('data: ')) {
      const data = JSON.parse(line.slice(6));
      
      if (data.event === 'todo_added') {
        // Add TODO to list
      } else if (data.event === 'complete') {
        // Analysis finished
      }
    }
  }
}
```

## Architecture Benefits

1. **Separation of Concerns**
   - Go: API orchestration, database, git operations
   - Python: AI/ML, LangChain integration
   - React: UI/UX

2. **Streaming Support**
   - Real-time feedback via SSE
   - No polling required
   - Progressive UI updates

3. **Scalability**
   - Services can be scaled independently
   - Python service can use GPU if needed
   - Go handles concurrent requests efficiently

4. **Flexibility**
   - Easy to swap AI providers
   - Add new subagents without changing Go code
   - Frontend agnostic to backend complexity

## Future Enhancements

- [ ] gRPC between Go and Python (instead of HTTP)
- [ ] Redis cache for agent responses
- [ ] Queue system for long-running analyses
- [ ] Multiple Python workers for concurrency
- [ ] WebSocket for bi-directional communication
