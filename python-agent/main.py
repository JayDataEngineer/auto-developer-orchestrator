"""
Python Microservice for LangChain Deep Agents

This service handles all deep agent operations including:
- Codebase analysis
- TODO list generation
- Subagent coordination

Communicates with Go backend via gRPC
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

app = FastAPI(title="Deep Agent Service", version="1.0.0")


class ChecklistRequest(BaseModel):
    """Request model for checklist generation"""
    project_path: str
    prompt: str | None = None


class ChecklistResponse(BaseModel):
    """Response model for checklist generation"""
    todos: List[AgentTodo]
    markdown: str


class SSEEvent(BaseModel):
    """Server-Sent Event model"""
    event: str
    data: Dict[str, Any] | None = None
    message: str | None = None


@app.post("/api/v1/checklist/generate")
async def generate_checklist(request: ChecklistRequest) -> StreamingResponse:
    """
    Generate a TODO checklist from codebase analysis
    
    Streams SSE events for real-time feedback:
    - log: Progress messages
    - on_tool_start: Tool execution events
    - final_result: Structured TODO list
    """
    
    async def event_generator() -> AsyncGenerator[str, None]:
        try:
            # Send initialization event
            yield f"data: {json.dumps({'event': 'log', 'message': 'DEEP AGENT: Initializing LangChain connection...'})}\n\n"
            
            if request.prompt:
                yield f"data: {json.dumps({'event': 'log', 'message': f'Using guidance prompt: {request.prompt}'})}\n\n"
            
            # Call deep agent with streaming
            todo_generator = generate_todos(
                project_path=request.project_path,
                prompt=request.prompt
            )
            
            async for event in todo_generator:
                if event.get('type') == 'tool_start':
                    yield f"data: {json.dumps({'event': 'on_tool_start', 'name': event.get('name')})}\n\n"
                elif event.get('type') == 'log':
                    yield f"data: {json.dumps({'event': 'log', 'message': event.get('message')})}\n\n"
                elif event.get('type') == 'todo_added':
                    yield f"data: {json.dumps({'event': 'todo_added', 'todo': event.get('todo')})}\n\n"
            
            # Final result is handled by the caller
            yield f"data: {json.dumps({'event': 'complete'})}\n\n"
            
        except Exception as e:
            yield f"data: {json.dumps({'error': str(e)})}\n\n"
    
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
    """
    Synchronous version - returns final TODO list without streaming
    """
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
    """Health check endpoint"""
    return {"status": "healthy", "service": "deep-agent-python"}


@app.get("/api/v1/status")
async def get_status():
    """Get service status"""
    return {
        "version": "1.0.0",
        "deepagents_version": "0.1.0",
        "model": os.getenv("DEEP_AGENT_MODEL", "gpt-4o"),
        "base_url": os.getenv("DEEP_AGENT_BASE_URL", "https://api.openai.com/v1")
    }


if __name__ == "__main__":
    port = int(os.getenv("PYTHON_SERVICE_PORT", "8080"))
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=port,
        reload=True,
        log_level="info"
    )
