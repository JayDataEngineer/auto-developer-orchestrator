from deepagents import create_deep_agent
import os

def list_directory(path: str) -> str:
    """List files in a directory."""
    try:
        return str(os.listdir(path))
    except Exception as e:
        return f"Error: {e}"

def read_file(filepath: str) -> str:
    """Read the contents of a file."""
    try:
        with open(filepath, 'r') as f:
            return f.read()[:5000]
    except Exception as e:
        return f"Error: {e}"

def grep_search(keyword: str) -> str:
    """Search for a keyword in the codebase."""
    # Mock implementation for safety, could use subprocess.run(['grep', '-r', keyword, '.'])
    return f"Searched for {keyword}"

# The create_deep_agent factory wires together the planner, tools, and sub-agents
app = create_deep_agent(
    tools=[list_directory, read_file, grep_search],
    system_prompt="You are an enterprise deep research agent analyzing a codebase to generate a technical to-do list."
)
