"""
Python Microservice for Deep Agents

Main orchestrator with specialized subagents:
- Explorer: Deep code exploration
- TODO Generator: Creates prioritized task lists

Communicates with Go backend via HTTP/SSE
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

from deep_agent import generate_todos, AgentTodo

app = FastAPI(
    title="Deep Agent Service",
    description="Code analysis agent with Explorer + TODO Generator subagents",
    version="2.0.0"
)


class ChecklistRequest(BaseModel):
    """Request model for checklist generation"""
    project_path: str
    prompt: str | None = None


class ChecklistResponse(BaseModel):
    """Response model for checklist generation"""
    todos: List[Dict[str, Any]]
    markdown: str


@app.post("/api/v1/checklist/generate")
async def generate_checklist(request: ChecklistRequest) -> StreamingResponse:
    """
    Generate a TODO checklist from codebase analysis

    Streams SSE events:
    - log: Progress messages
    - tool_start: Tool execution events
    - subagent_call: Subagent delegation
    - todo_added: New TODO generated
    - complete: Analysis finished
    """

    async def event_generator() -> AsyncGenerator[str, None]:
        try:
            # Init
            yield f"data: {json.dumps({'event': 'log', 'message': 'DEEP AGENT: Starting analysis with Explorer + TODO Generator subagents...'})}\n\n"

            if request.prompt:
                yield f"data: {json.dumps({'event': 'log', 'message': f'Guidance: {request.prompt}'})}\n\n"

            # Call deep agent with streaming
            todo_generator = await generate_todos(
                project_path=request.project_path,
                prompt=request.prompt,
                streaming=True
            )

            async for event in todo_generator:
                event_type = event.get('type', 'unknown')

                if event_type == 'tool_start':
                    yield f"data: {json.dumps({'event': 'on_tool_start', 'name': event.get('name')})}\n\n"

                elif event_type == 'log':
                    yield f"data: {json.dumps({'event': 'log', 'message': event.get('message')})}\n\n"

                elif event_type == 'subagent_call':
                    subagent_name = event.get('name', 'unknown')
                    yield f"data: {json.dumps({'event': 'subagent_call', 'name': subagent_name, 'message': f'Delegating to {subagent_name} subagent...'})}\n\n"

                elif event_type == 'todo_added':
                    yield f"data: {json.dumps({'event': 'todo_added', 'todo': event.get('todo')})}\n\n"

                elif event_type == 'error':
                    yield f"data: {json.dumps({'event': 'error', 'message': event.get('message')})}\n\n"

            yield f"data: {json.dumps({'event': 'complete', 'message': 'DEEP AGENT: Analysis complete!'})}\n\n"

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


@app.post("/api/v1/checklist/generate-sync")
async def generate_checklist_sync(request: ChecklistRequest) -> ChecklistResponse:
    """Synchronous version - returns final TODO list"""
    try:
        result = await generate_todos(
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


@app.get("/health")
async def health_check():
    return {
        "status": "healthy",
        "service": "deep-agent-python",
        "version": "2.0.0",
        "subagents": ["explorer", "todo_generator"]
    }


@app.get("/api/v1/status")
async def get_status():
    return {
        "version": "2.0.0",
        "architecture": "deepagents-with-subagents",
        "subagents": {
            "explorer": "Deep code exploration and pattern detection",
            "todo_generator": "Creates prioritized TODO lists"
        },
        "model": os.getenv("DEEP_AGENT_MODEL", "gpt-4o"),
        "base_url": os.getenv("DEEP_AGENT_BASE_URL", "https://api.openai.com/v1")
    }


if __name__ == "__main__":
    port = int(os.getenv("PYTHON_SERVICE_PORT", "8080"))
    uvicorn.run("main:app", host="0.0.0.0", port=port, reload=True)
