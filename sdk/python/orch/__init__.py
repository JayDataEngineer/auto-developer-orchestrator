"""
Orchestrator Python SDK.

Usage:
    from orch import OrchestratorClient

    orch = OrchestratorClient(base_url="http://localhost:3847")

    # MCP tools
    results = orch.mcp_search("golang concurrency patterns")
    page = orch.mcp_scrape("https://example.com")

    # Sandbox tools
    output = orch.bash(sandbox_id="my-sandbox", command="ls -la")
    content = orch.file_read(sandbox_id="my-sandbox", path="/workspace/main.go")
    orch.file_write(sandbox_id="my-sandbox", path="/workspace/hello.txt", content="Hi")

    # Agent interaction
    response = orch.prompt(project="myproject", message="explain quicksort")

    # List available tools
    tools = orch.list_tools()
"""

from .client import OrchestratorClient, OrchestratorError, ToolResult

__all__ = ["OrchestratorClient", "OrchestratorError", "ToolResult"]
__version__ = "0.1.0"
