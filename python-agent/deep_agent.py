"""
Deep Agent Implementation using LangChain deepagents

This module wraps the deepagents library to provide:
- Codebase analysis
- TODO list generation
- Streaming event feedback
"""

import os
from pathlib import Path
from typing import AsyncGenerator, Dict, Any, List
from dotenv import load_dotenv

# Load environment variables
load_dotenv()

from deepagents import createDeepAgent, LocalShellBackend
from langchain_openai import ChatOpenAI

# Configuration from environment
DEEP_AGENT_MODEL = os.getenv("DEEP_AGENT_MODEL", "gpt-4o")
DEEP_AGENT_BASE_URL = os.getenv("DEEP_AGENT_BASE_URL", "https://api.openai.com/v1")
OPENAI_API_KEY = os.getenv("OPENAI_API_KEY")

# System prompt for TODO generation
TODO_GENERATOR_PROMPT = """You are an expert software architect and technical lead. Your job is to analyze a codebase and generate a comprehensive TODO list for improvement.

## Your Task

1. Analyze the codebase structure by exploring the files.
2. Identify technical debt, missing features, bugs, and architectural improvements.
3. Use the `write_todos` tool to maintain a structured list of 5-10 actionable tasks.
4. Also write your findings to a file called `TODO_FOR_JULES.md` for persistence.

## Task Guidelines

- Tasks should be specific, technical, and actionable.
- Mix of small (refactoring), medium (feature additions), and large (architectural) tasks.
- Include: bug fixes, performance optimizations, security audits, and testing gaps.
- Prioritize high-impact improvements.

## Capabilities

- You have an **Explorer Subagent** specifically designed for deep code exploration. Delegate broad analysis or searching tasks to it if needed.
- You have filesystem tools to read and list files.

## Process

1. First, explore the project structure using `ls` and `read_file`, or delegate to the explorer subagent.
2. Review key files: `package.json`, main source files, and configuration.
3. Use `write_todos` to record each task as you identify it.
4. Finally, write the summary to `TODO_FOR_JULES.md`.

Remember: This TODO list will be executed by Jules (an AI coding agent), so be specific and technical!"""


class AgentTodo:
    """Todo item model"""
    def __init__(self, id: str, content: str, status: str = "pending", priority: str = "medium"):
        self.id = id
        self.content = content
        self.status = status  # pending, in_progress, completed, cancelled
        self.priority = priority  # high, medium, low
    
    def to_dict(self) -> Dict[str, Any]:
        return {
            "id": self.id,
            "content": self.content,
            "status": self.status,
            "priority": self.priority
        }


def create_todo_agent():
    """Create the TODO generation deep agent"""
    
    # Create OpenAI-compatible chat model
    chat_model = ChatOpenAI(
        model=DEEP_AGENT_MODEL,
        api_key=OPENAI_API_KEY,
        base_url=DEEP_AGENT_BASE_URL,
    )
    
    # Create explorer subagent
    explorer_subagent = createDeepAgent(
        name="explorer",
        model=chat_model,
        system_prompt="You are a code exploration expert. Your job is to deeply analyze codebase structures, find patterns, and identify technical issues. Use your tools to search, read, and understand the code thoroughly.",
        backend=LocalShellBackend(
            root_dir=os.getcwd(),
            inherit_env=True,
        ),
    )
    
    # Create main TODO agent
    todo_agent = createDeepAgent(
        name="todo_generator",
        model=chat_model,
        system_prompt=TODO_GENERATOR_PROMPT,
        subagents=[explorer_subagent],
        backend=LocalShellBackend(
            root_dir=os.getcwd(),
            inherit_env=True,
        ),
    )
    
    return todo_agent


async def generate_todos(
    project_path: str,
    prompt: str | None = None,
    streaming: bool = True
) -> AsyncGenerator[Dict[str, Any], None] | Dict[str, Any]:
    """
    Generate TODO items from codebase analysis
    
    Args:
        project_path: Path to the codebase to analyze
        prompt: Optional guidance prompt
        streaming: If True, yield events as they occur; if False, return final result
    
    Returns:
        If streaming: AsyncGenerator yielding events
        If not streaming: Dict with 'todos' and 'markdown' keys
    """
    
    agent = create_todo_agent()
    
    # Build the user message
    user_message = f'Analyze the codebase at "{project_path}" and generate technical improvement tasks using the write_todos tool.'
    if prompt:
        user_message += f" Guidance: {prompt}"
    
    if streaming:
        # Stream events
        async def event_stream() -> AsyncGenerator[Dict[str, Any], None]:
            try:
                # Use stream method for real-time feedback
                stream = await agent.stream({
                    "messages": [{
                        "role": "user",
                        "content": user_message,
                    }],
                })
                
                async for chunk in stream:
                    # Parse chunk and yield appropriate events
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
        
        return event_stream()
    
    else:
        # Invoke and get final result
        result = await agent.invoke({
            "messages": [{
                "role": "user",
                "content": user_message,
            }],
        })
        
        # Extract structured todos from result
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
        
        # Read markdown file if it exists
        markdown_path = Path(project_path) / "TODO_FOR_JULES.md"
        markdown = ""
        if markdown_path.exists():
            markdown = markdown_path.read_text()
        elif todos:
            # Generate markdown from todos
            markdown = "\n".join([f"- [ ] {todo.content}" for todo in todos])
            markdown_path.write_text(markdown)
        
        return {
            'todos': [todo.to_dict() for todo in todos],
            'markdown': markdown
        }
