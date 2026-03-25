"""
Deep Agent - DRY Implementation with Multiple Workflows

This module provides a factory pattern for creating agents with different workflows.
Each workflow differs only in:
1. System prompt
2. Tools available  
3. Output validation schema
"""

import os
from pathlib import Path
from typing import AsyncGenerator, Dict, Any, Optional, Type
from dotenv import load_dotenv
from pydantic import BaseModel

load_dotenv()

from deepagents import createDeepAgent, LocalShellBackend
from langchain_openai import ChatOpenAI

from prompts import (
    WorkflowPrompts,
    WORKFLOWS,
    get_workflow,
    list_workflows,
    TodoList,
    ImplementationPlan,
    ReviewResult,
)

# Configuration from environment
DEEP_AGENT_MODEL = os.getenv("DEEP_AGENT_MODEL", "gpt-4o")
DEEP_AGENT_BASE_URL = os.getenv("DEEP_AGENT_BASE_URL", "https://api.openai.com/v1")
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY")


class AgentTodo:
    """Todo item model (for backwards compatibility)"""
    def __init__(self, id: str, content: str, status: str = "pending", priority: str = "medium"):
        self.id = id
        self.content = content
        self.status = status
        self.priority = priority

    def to_dict(self) -> Dict[str, Any]:
        return {
            "id": self.id,
            "content": self.content,
            "status": self.status,
            "priority": self.priority
        }


def create_explorer_subagent(chat_model: ChatOpenAI) -> Any:
    """Create the shared explorer subagent"""
    return createDeepAgent(
        name="explorer",
        model=chat_model,
        system_prompt=WorkflowPrompts.EXPLORER_PROMPT,
        backend=LocalShellBackend(
            root_dir=os.getcwd(),
            inherit_env=True,
        ),
    )


def create_agent(workflow_name: str = "todo_generator"):
    """
    Factory function to create an agent with the specified workflow.
    
    Args:
        workflow_name: Name of the workflow (todo_generator, implementer, reviewer, test_generator)
    
    Returns:
        Configured DeepAgent instance
    """
    workflow = get_workflow(workflow_name)
    
    # Create OpenAI-compatible chat model
    chat_model = ChatOpenAI(
        model=DEEP_AGENT_MODEL,
        api_key=OPENAI_API_KEY,
        base_url=DEEP_AGENT_BASE_URL,
    )
    
    # Apply structured output if schema is defined
    if workflow.get("output_schema"):
        chat_model = chat_model.with_structured_output(workflow["output_schema"])
    
    # Create explorer subagent (shared across all workflows)
    explorer_subagent = create_explorer_subagent(chat_model)
    
    # Create main agent with workflow-specific configuration
    agent = createDeepAgent(
        name=workflow["name"],
        model=chat_model,
        system_prompt=workflow["prompt"],
        subagents=[explorer_subagent],
        backend=LocalShellBackend(
            root_dir=os.getcwd(),
            inherit_env=True,
        ),
    )
    
    return agent


async def run_workflow(
    workflow_name: str,
    project_path: str,
    prompt: Optional[str] = None,
    streaming: bool = True
) -> AsyncGenerator[Dict[str, Any], None] | Dict[str, Any]:
    """
    Run a specific workflow.
    
    Args:
        workflow_name: Name of the workflow to run
        project_path: Path to the codebase
        prompt: Optional guidance prompt
        streaming: If True, yield events as they occur
    
    Returns:
        If streaming: AsyncGenerator yielding events
        If not streaming: Dict with workflow-specific results
    """
    agent = create_agent(workflow_name)
    
    # Build user message based on workflow
    user_message = _build_user_message(workflow_name, project_path, prompt)
    
    if streaming:
        return _stream_events(agent, user_message)
    else:
        return await _invoke_agent(agent, user_message, workflow_name, project_path)


def _build_user_message(workflow_name: str, project_path: str, prompt: Optional[str]) -> str:
    """Build user message based on workflow type"""
    messages = {
        "todo_generator": f'Analyze the codebase at "{project_path}" and generate technical improvement tasks using the write_todos tool.',
        "implementer": f'Implement the pending task for the codebase at "{project_path}". Read TODO_FOR_JULES.md for the task description.',
        "reviewer": f'Review the recent code changes in the codebase at "{project_path}". Check for bugs, security issues, and code quality.',
        "test_generator": f'Generate comprehensive tests for the codebase at "{project_path}". Focus on uncovered functionality.',
    }
    
    user_message = messages.get(workflow_name, f'Process the codebase at "{project_path}".')
    
    if prompt:
        user_message += f" Guidance: {prompt}"
    
    return user_message


async def _stream_events(agent: Any, user_message: str) -> AsyncGenerator[Dict[str, Any], None]:
    """Stream events from agent execution"""
    try:
        stream = await agent.stream({
            "messages": [{
                "role": "user",
                "content": user_message,
            }],
        })
        
        async for chunk in stream:
            if isinstance(chunk, dict):
                if 'tool_calls' in chunk:
                    for tool_call in chunk['tool_calls']:
                        yield {
                            'type': 'tool_start',
                            'name': tool_call.get('name', 'unknown')
                        }
                elif 'logs' in chunk:
                    for log in chunk['logs']:
                        yield {
                            'type': 'log',
                            'message': log
                        }
    except Exception as e:
        yield {'type': 'error', 'message': str(e)}


async def _invoke_agent(agent: Any, user_message: str, workflow_name: str, project_path: str) -> Dict[str, Any]:
    """Invoke agent and process results"""
    result = await agent.invoke({
        "messages": [{
            "role": "user",
            "content": user_message,
        }],
    })
    
    # Process results based on workflow type
    if workflow_name == "todo_generator":
        return _process_todo_result(result, project_path)
    elif workflow_name == "implementer":
        return _process_implementation_result(result)
    elif workflow_name == "reviewer":
        return _process_review_result(result)
    elif workflow_name == "test_generator":
        return _process_test_result(result)
    else:
        return {"result": result}


def _process_todo_result(result: Any, project_path: str) -> Dict[str, Any]:
    """Process TODO generator results"""
    todos_raw = getattr(result, 'todos', [])
    todos = [
        AgentTodo(
            id=todo.get('id', f'task-{i}'),
            content=todo.get('content', ''),
            status=todo.get('status', 'pending'),
            priority=todo.get('priority', 'medium')
        )
        for i, todo in enumerate(todos_raw)
    ]
    
    # Read/write markdown file
    markdown_path = Path(project_path) / "TODO_FOR_JULES.md"
    markdown = ""
    if markdown_path.exists():
        markdown = markdown_path.read_text()
    elif todos:
        markdown = "\n".join([f"- [ ] {todo.content}" for todo in todos])
        markdown_path.write_text(markdown)
    
    return {
        'todos': [todo.to_dict() for todo in todos],
        'markdown': markdown
    }


def _process_implementation_result(result: Any) -> Dict[str, Any]:
    """Process implementer results"""
    # Extract implementation plan if available
    plan = getattr(result, 'steps', [])
    files_modified = getattr(result, 'files_to_modify', [])
    
    return {
        'success': True,
        'steps_completed': plan if isinstance(plan, list) else [],
        'files_modified': files_modified if isinstance(files_modified, list) else [],
    }


def _process_review_result(result: Any) -> Dict[str, Any]:
    """Process reviewer results"""
    passed = getattr(result, 'passed', False)
    issues = getattr(result, 'issues', [])
    suggestions = getattr(result, 'suggestions', [])
    blocking = getattr(result, 'blocking_issues', [])
    
    return {
        'passed': passed,
        'issues': issues if isinstance(issues, list) else [],
        'suggestions': suggestions if isinstance(suggestions, list) else [],
        'blocking_issues': blocking if isinstance(blocking, list) else [],
    }


def _process_test_result(result: Any) -> Dict[str, Any]:
    """Process test generator results"""
    return {
        'success': True,
        'message': 'Tests generated and executed',
    }


# Backwards compatibility aliases
generate_todos = lambda project_path, prompt=None, streaming=True: run_workflow(
    "todo_generator", project_path, prompt, streaming
)

create_todo_agent = lambda: create_agent("todo_generator")
