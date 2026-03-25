"""
Python Microservice for Deep Agents

Main orchestrator with multiple workflows:
- todo_generator: Creates prioritized task lists
- implementer: Implements specific coding tasks
- reviewer: Reviews code changes
- test_generator: Generates comprehensive tests

All workflows share the Explorer subagent for code exploration.
"""

import asyncio
import json
import os
from pathlib import Path
from typing import AsyncGenerator, List, Dict, Any

from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel
import uvicorn

from deep_agent import run_workflow, list_workflows, AgentTodo
from prompts import WORKFLOWS

app = FastAPI(
    title="Deep Agent Service",
    description="Multi-workflow code analysis and implementation agent",
    version="3.0.0"
)


class WorkflowRequest(BaseModel):
    """Request model for workflow execution"""
    project_path: str
    prompt: str | None = None
    workflow: str = "todo_generator"


class ChecklistResponse(BaseModel):
    """Response model for checklist generation"""
    todos: List[Dict[str, Any]]
    markdown: str


@app.post("/api/v1/workflow/run")
async def run_workflow_endpoint(request: WorkflowRequest) -> StreamingResponse:
    """
    Run a specific workflow.

    Supported workflows:
    - todo_generator: Analyze codebase and generate TODO tasks
    - implementer: Implement specific coding tasks
    - reviewer: Review code changes
    - test_generator: Generate comprehensive tests
    """
    
    # Validate workflow
    if request.workflow not in list_workflows():
        raise HTTPException(
            status_code=400,
            detail=f"Unknown workflow: {request.workflow}. Available: {list_workflows()}"
        )
    
    async def event_generator() -> AsyncGenerator[str, None]:
        try:
            # Init
            workflow_desc = WORKFLOWS[request.workflow].get("description", "Processing")
            yield f"data: {json.dumps({'event': 'log', 'message': f'DEEP AGENT: Starting {workflow_desc}...'})}\n\n"

            if request.prompt:
                yield f"data: {json.dumps({'event': 'log', 'message': f'Guidance: {request.prompt}'})}\n\n"

            # Run workflow with streaming
            result_generator = await run_workflow(
                workflow_name=request.workflow,
                project_path=request.project_path,
                prompt=request.prompt,
                streaming=True
            )

            async for event in result_generator:
                event_type = event.get('type', 'unknown')

                if event_type == 'tool_start':
                    yield f"data: {json.dumps({'event': 'on_tool_start', 'name': event.get('name')})}\n\n"

                elif event_type == 'log':
                    yield f"data: {json.dumps({'event': 'log', 'message': event.get('message')})}\n\n"

                elif event_type == 'todo_added':
                    yield f"data: {json.dumps({'event': 'todo_added', 'todo': event.get('todo')})}\n\n"

                elif event_type == 'error':
                    yield f"data: {json.dumps({'event': 'error', 'message': event.get('message')})}\n\n"

            yield f"data: {json.dumps({'event': 'complete', 'message': 'DEEP AGENT: Workflow complete!'})}\n\n"

        except Exception as e:
            yield f"data: {json.dumps({'event': 'error', 'message': str(e)})}\n\n"

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no"
        }
    )


@app.post("/api/v1/checklist/generate")
async def generate_checklist(request: WorkflowRequest) -> StreamingResponse:
    """Legacy endpoint - runs todo_generator workflow"""
    request.workflow = "todo_generator"
    return await run_workflow_endpoint(request)


@app.post("/api/v1/checklist/generate-sync")
async def generate_checklist_sync(request: WorkflowRequest) -> ChecklistResponse:
    """Synchronous version - returns final TODO list"""
    try:
        result = await run_workflow(
            workflow_name="todo_generator",
            project_path=request.project_path,
            prompt=request.prompt,
            streaming=False
        )

        return ChecklistResponse(
            todos=result['todos'],
            markdown=result['markdown']
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/api/v1/workflows")
async def list_available_workflows():
    """List available workflows"""
    return {
        "workflows": [
            {
                "name": name,
                "description": WORKFLOWS[name].get("description", ""),
                "tools": WORKFLOWS[name].get("tools", []),
            }
            for name in list_workflows()
        ]
    }


@app.get("/health")
async def health_check():
    return {
        "status": "healthy",
        "service": "deep-agent-python",
        "version": "3.0.0",
        "workflows": list_workflows(),
        "subagents": ["explorer"]
    }


@app.get("/api/v1/status")
async def get_status():
    return {
        "version": "3.0.0",
        "architecture": "multi-workflow-deepagents",
        "workflows": {
            name: WORKFLOWS[name].get("description", "")
            for name in list_workflows()
        },
        "subagents": {
            "explorer": "Deep code exploration and pattern detection"
        },
        "model": os.getenv("DEEP_AGENT_MODEL", "gpt-4o"),
        "base_url": os.getenv("DEEP_AGENT_BASE_URL", "https://api.openai.com/v1")
    }


if __name__ == "__main__":
    port = int(os.getenv("PYTHON_SERVICE_PORT", "8080"))
    uvicorn.run("main:app", host="0.0.0.0", port=port, reload=True)
