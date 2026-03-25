# Python Deep Agent - Multi-Workflow System

## Overview

The Python Deep Agent now supports **multiple workflows** using a DRY (Don't Repeat Yourself) architecture. All workflows share the same Explorer subagent but differ in:

1. **System Prompt** - What the agent should do
2. **Available Tools** - What actions the agent can take
3. **Output Validation** - Structured output schema (optional)

## Available Workflows

### 1. TODO Generator (`todo_generator`)

**Purpose:** Analyze codebase and generate improvement tasks

**Prompt:** Expert software architect identifying technical debt

**Tools:** `write_todos`, `filesystem`, `explorer_subagent`

**Output Schema:** `TodoList` (tasks + summary)

**Use Case:** Initial codebase analysis, sprint planning

---

### 2. Implementer (`implementer`)

**Purpose:** Implement specific coding tasks from TODO list

**Prompt:** Expert developer following best practices

**Tools:** `update_todo`, `filesystem`, `shell`, `explorer_subagent`

**Output Schema:** `ImplementationPlan` (steps + files + risks)

**Use Case:** Executing TODO tasks, feature implementation

---

### 3. Reviewer (`reviewer`)

**Purpose:** Review code changes for quality and correctness

**Prompt:** Expert code reviewer checking for issues

**Tools:** `submit_review`, `filesystem`, `shell`, `explorer_subagent`

**Output Schema:** `ReviewResult` (passed + issues + suggestions)

**Use Case:** PR review, pre-merge validation

---

### 4. Test Generator (`test_generator`)

**Purpose:** Generate comprehensive tests for code changes

**Prompt:** Expert test engineer covering edge cases

**Tools:** `add_tests`, `filesystem`, `shell`, `explorer_subagent`

**Output Schema:** None (tests are written to filesystem)

**Use Case:** Post-implementation test coverage

---

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   FastAPI Main App                       │
│  /api/v1/workflow/run                                    │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│              deep_agent.py (Factory)                     │
│  create_agent(workflow_name)                             │
│  run_workflow(workflow_name, project_path, prompt)       │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│              prompts.py (Configuration)                  │
│  - WorkflowPrompts (all prompts)                         │
│  - WORKFLOWS (registry)                                  │
│  - Output schemas (TodoList, ReviewResult, etc.)         │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│           LangChain deepagents                           │
│  ┌─────────────────────────────────────────────┐         │
│  │  Main Agent (workflow-specific)             │         │
│  │  - System prompt from prompts.py            │         │
│  │  - Tools from prompts.py                    │         │
│  │  - Structured output (optional)             │         │
│  │                                             │         │
│  │  ┌─────────────────────────────────┐       │         │
│  │  │  Explorer Subagent (shared)     │       │         │
│  │  │  - Code exploration             │       │         │
│  │  │  - Pattern detection            │       │         │
│  │  └─────────────────────────────────┘       │         │
│  └─────────────────────────────────────────────┘         │
└─────────────────────────────────────────────────────────┘
```

---

## API Usage

### Run Workflow (Streaming)

```bash
curl -X POST http://localhost:8080/api/v1/workflow/run \
  -H "Content-Type: application/json" \
  -d '{
    "workflow": "todo_generator",
    "project_path": "/app/my-project",
    "prompt": "Focus on security issues"
  }'
```

**Response (SSE Stream):**
```
data: {"event": "log", "message": "DEEP AGENT: Starting Analyze codebase and generate TODO tasks..."}

data: {"event": "on_tool_start", "name": "explorer"}

data: {"event": "todo_added", "todo": {"id": "task-1", "content": "Add input validation", "priority": "high"}}

data: {"event": "complete", "message": "DEEP AGENT: Workflow complete!"}
```

### Run Workflow (Sync)

```bash
curl -X POST http://localhost:8080/api/v1/checklist/generate-sync \
  -H "Content-Type: application/json" \
  -d '{
    "workflow": "todo_generator",
    "project_path": "/app/my-project"
  }'
```

**Response:**
```json
{
  "todos": [
    {"id": "task-1", "content": "Add input validation", "priority": "high"}
  ],
  "markdown": "- [ ] Add input validation"
}
```

### List Workflows

```bash
curl http://localhost:8080/api/v1/workflows
```

**Response:**
```json
{
  "workflows": [
    {
      "name": "todo_generator",
      "description": "Analyze codebase and generate TODO tasks",
      "tools": ["write_todos", "filesystem", "explorer_subagent"]
    },
    {
      "name": "implementer",
      "description": "Implement specific coding tasks",
      "tools": ["update_todo", "filesystem", "shell", "explorer_subagent"]
    }
  ]
}
```

---

## Python SDK Usage

### Using the Factory

```python
from deep_agent import create_agent, run_workflow

# Create specific workflow agent
reviewer = create_agent("reviewer")

# Run workflow with streaming
async for event in run_workflow(
    workflow_name="reviewer",
    project_path="/app/my-project",
    streaming=True
):
    print(event)

# Run workflow sync
result = await run_workflow(
    workflow_name="todo_generator",
    project_path="/app/my-project",
    streaming=False
)
print(result['todos'])
```

### Using Prompts Directly

```python
from prompts import WORKFLOWS, get_workflow, WorkflowPrompts

# Get workflow config
workflow = get_workflow("reviewer")
print(workflow['prompt'])
print(workflow['tools'])
print(workflow['output_schema'])

# List all workflows
from prompts import list_workflows
print(list_workflows())  # ['todo_generator', 'implementer', 'reviewer', 'test_generator']
```

---

## Adding New Workflows

### Step 1: Add Output Schema (if needed)

```python
# prompts.py
class MigrationPlan(BaseModel):
    """Migration plan output"""
    steps: List[str] = Field(description="Migration steps")
    risks: List[str] = Field(description="Potential risks")
    rollback_plan: str = Field(description="Rollback procedure")
```

### Step 2: Add Prompt Definition

```python
# prompts.py
class WorkflowPrompts:
    MIGRATOR_PROMPT = """You are an expert migration specialist..."""
    MIGRATOR_NAME = "migrator"
    MIGRATOR_TOOLS = ["create_migration", "filesystem", "shell"]
```

### Step 3: Register Workflow

```python
# prompts.py
WORKFLOWS = {
    # ... existing workflows ...
    "migrator": {
        "name": WorkflowPrompts.MIGRATOR_NAME,
        "prompt": WorkflowPrompts.MIGRATOR_PROMPT,
        "tools": WorkflowPrompts.MIGRATOR_TOOLS,
        "output_schema": MigrationPlan,
        "description": "Plan and execute code migrations",
    },
}
```

That's it! The factory automatically picks up the new workflow.

---

## Output Schemas

### TodoList
```python
class TodoItem(BaseModel):
    id: str
    content: str
    priority: Literal["high", "medium", "low"]
    estimated_complexity: Literal["small", "medium", "large"]

class TodoList(BaseModel):
    tasks: List[TodoItem]
    summary: str
```

### ImplementationPlan
```python
class ImplementationPlan(BaseModel):
    steps: List[str]
    files_to_modify: List[str]
    risks: List[str]
```

### ReviewResult
```python
class ReviewResult(BaseModel):
    passed: bool
    issues: List[str]
    suggestions: List[str]
    blocking_issues: List[str]
```

---

## Explorer Subagent (Shared)

All workflows share the same Explorer subagent:

**Purpose:** Deep code exploration and pattern detection

**Capabilities:**
- List directory structures
- Read file contents
- Search for patterns (grep)
- Execute shell commands for analysis

**Prompt:**
```
You are an expert code exploration agent. Your job is to deeply 
analyze codebase structures, find patterns, and identify technical issues.
```

This DRY design means improvements to the Explorer automatically benefit all workflows.

---

## Environment Variables

```bash
# Model configuration
DEEP_AGENT_MODEL=gpt-4o
DEEP_AGENT_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=sk-...

# Service configuration
PYTHON_SERVICE_PORT=8080
```

---

## Backwards Compatibility

All legacy endpoints and functions still work:

```python
# Old way (still works)
from deep_agent import generate_todos, create_todo_agent

result = await generate_todos("/app/my-project")
agent = create_todo_agent()

# API
POST /api/v1/checklist/generate  # Still works
POST /api/v1/checklist/generate-sync  # Still works
```

---

## Summary

**Before:** One agent, hardcoded prompt, no validation

**After:** 
- 4 workflows (easily extensible)
- Centralized prompts in `prompts.py`
- Factory pattern in `deep_agent.py`
- Structured output validation
- DRY (Explorer subagent shared)
- Backwards compatible
