"""
Core client for the Orchestrator REST API.
"""

import json
from typing import Any, Optional

import httpx


class OrchestratorError(Exception):
    """Error from the orchestrator API."""

    def __init__(self, message: str, status_code: int = 0, tool: str = ""):
        self.message = message
        self.status_code = status_code
        self.tool = tool
        super().__init__(message)


class ToolResult:
    """Result from a direct tool execution call."""

    def __init__(self, data: dict):
        self.success: bool = data.get("success", False)
        self.tool: str = data.get("tool", "")
        self.error: Optional[str] = data.get("error")
        self._result = data.get("result")

    @property
    def data(self) -> Any:
        """Raw result data."""
        return self._result

    @property
    def text(self) -> str:
        """Result as string."""
        if isinstance(self._result, str):
            return self._result
        if isinstance(self._result, dict):
            # MCP tools return {result: "..."}
            if "result" in self._result:
                return str(self._result["result"])
            # Bash returns {output: "..."}
            if "output" in self._result:
                return str(self._result["output"])
            # File read returns {content: "..."}
            if "content" in self._result:
                return str(self._result["content"])
        return json.dumps(self._result, indent=2) if self._result else ""

    def raise_on_error(self) -> "ToolResult":
        """Raise if the tool call failed."""
        if not self.success:
            raise OrchestratorError(self.error or "tool failed", tool=self.tool)
        return self

    def __repr__(self) -> str:
        status = "ok" if self.success else f"error: {self.error}"
        return f"ToolResult(tool={self.tool!r}, {status})"


class AgentResponse:
    """Response from an agent prompt (SSE stream collected)."""

    def __init__(self, text: str, tool_calls: list[dict], events: list[dict]):
        self.text = text
        self.tool_calls = tool_calls
        self.events = events

    def __repr__(self) -> str:
        return f"AgentResponse(text={len(self.text)} chars, {len(self.tool_calls)} tool calls)"


class OrchestratorClient:
    """Client for the Auto-Developer Orchestrator API."""

    def __init__(self, base_url: str = "http://localhost:3847", timeout: int = 120):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self._client = httpx.Client(
            base_url=self.base_url,
            timeout=httpx.Timeout(timeout, read=600.0),
        )

    # ── Health ──────────────────────────────────────────────────────────

    def health(self) -> dict:
        """Check orchestrator health."""
        return self._get("/api/health")

    def is_healthy(self) -> bool:
        """Returns True if the orchestrator is reachable and healthy."""
        try:
            return self._get("/api/health").get("status") == "ok"
        except Exception:
            return False

    # ── Tool execution (direct, no agent loop) ─────────────────────────

    def list_tools(self) -> list[dict]:
        """List all available tools (MCP + sandbox)."""
        resp = self._get("/api/tools")
        return resp.get("tools", [])

    def exec_tool(self, tool: str, args: dict = None,
                  sandbox_id: str = "", timeout: int = 0) -> ToolResult:
        """Execute a tool directly. Returns ToolResult."""
        payload = {"tool": tool, "args": args or {}}
        if sandbox_id:
            payload["sandbox_id"] = sandbox_id
        if timeout:
            payload["timeout"] = timeout
        data = self._post("/api/tools/exec", payload)
        return ToolResult(data)

    # ── MCP tools ───────────────────────────────────────────────────────

    def mcp_search(self, query: str, max_results: int = 5) -> ToolResult:
        """Search the web via MCP."""
        return self.exec_tool("mcp_search", {
            "query": query,
            "top_k": max_results,
        })

    def mcp_scrape(self, url: str) -> ToolResult:
        """Scrape a URL and return clean markdown."""
        return self.exec_tool("mcp_scrape", {"url": url})

    def mcp_research(self, query: str, max_results: int = 3) -> ToolResult:
        """Research a topic: search + scrape top results."""
        return self.exec_tool("mcp_research", {
            "query": query,
            "max_results": max_results,
        })

    def mcp_crawl(self, url: str, max_depth: int = 2, max_pages: int = 20) -> ToolResult:
        """Deep crawl a site following links."""
        return self.exec_tool("mcp_crawl", {
            "url": url,
            "max_depth": max_depth,
            "max_pages": max_pages,
        })

    def mcp_extract(self, url: str, schema_type: str = "news") -> ToolResult:
        """Extract structured data from a page."""
        return self.exec_tool("mcp_extract", {
            "url": url,
            "schema_type": schema_type,
        })

    def mcp_analyze_image(self, image_url: str, prompt: str = "Describe this image") -> ToolResult:
        """Analyze an image via MCP."""
        return self.exec_tool("mcp_analyze_image", {
            "imageSource": image_url,
            "prompt": prompt,
        })

    # ── Sandbox tools ───────────────────────────────────────────────────

    def bash(self, sandbox_id: str, command: str, timeout: int = 60) -> ToolResult:
        """Execute a bash command in a sandbox."""
        return self.exec_tool("bash", {"command": command},
                              sandbox_id=sandbox_id, timeout=timeout)

    def file_read(self, sandbox_id: str, path: str,
                  offset: int = 0, limit: int = 0) -> ToolResult:
        """Read a file from a sandbox."""
        args = {"file_path": path}
        if offset:
            args["offset"] = offset
        if limit:
            args["limit"] = limit
        return self.exec_tool("file_read", args, sandbox_id=sandbox_id)

    def file_write(self, sandbox_id: str, path: str, content: str) -> ToolResult:
        """Write a file to a sandbox."""
        return self.exec_tool("file_write", {
            "file_path": path,
            "content": content,
        }, sandbox_id=sandbox_id)

    # ── Sandbox management ──────────────────────────────────────────────

    def list_sandboxes(self) -> list:
        """List all sandboxes."""
        return self._get("/api/sandbox/")

    def get_sandbox(self, sandbox_id: str) -> dict:
        """Get sandbox details."""
        return self._get(f"/api/sandbox/{sandbox_id}")

    def create_sandbox(self, project: str, **kwargs) -> dict:
        """Create a new sandbox."""
        payload = {"project": project, **kwargs}
        return self._post("/api/sandbox/", payload)

    def destroy_sandbox(self, sandbox_id: str) -> dict:
        """Destroy a sandbox."""
        return self._delete(f"/api/sandbox/{sandbox_id}")

    def sandbox_exec(self, sandbox_id: str, command: str) -> str:
        """Execute command in sandbox (raw exec API)."""
        data = self._post(f"/api/sandbox/{sandbox_id}/exec", {
            "cmd": ["bash", "-c", command],
        })
        return data.get("output", data.get("stdout", ""))

    # ── Agent interaction ───────────────────────────────────────────────

    def prompt(self, project: str, message: str, agent_id: str = "default",
               timeout: int = 600) -> AgentResponse:
        """Send a prompt to the orchestrator agent and collect the full response."""
        url = "/api/pux/prompt"
        payload = {
            "message": message,
            "project": project,
            "agentId": agent_id,
        }

        text_parts = []
        tool_calls = []
        events = []

        with self._client.stream(
            "POST", url, json=payload,
            headers={"Accept": "text/event-stream"},
            timeout=httpx.Timeout(timeout),
        ) as resp:
            if resp.status_code != 200:
                body = resp.read().decode()
                raise OrchestratorError(f"Agent prompt failed: {body}", resp.status_code)

            for line in resp.iter_lines():
                if not line.startswith("data: "):
                    continue
                data_str = line[6:]
                if data_str == "[DONE]":
                    break
                try:
                    evt = json.loads(data_str)
                except json.JSONDecodeError:
                    continue

                evt_type = evt.get("type", "")
                events.append(evt)

                if evt_type == "text_delta":
                    text_parts.append(evt.get("text", ""))
                elif evt_type == "tool_execution_start":
                    tool_calls.append({
                        "tool": evt.get("toolName", ""),
                        "args": evt.get("args", {}),
                    })

        return AgentResponse(
            text="".join(text_parts),
            tool_calls=tool_calls,
            events=events,
        )

    def prompt_stream(self, project: str, message: str, agent_id: str = "default",
                      timeout: int = 600):
        """Send a prompt and yield SSE events as they arrive."""
        url = "/api/pux/prompt"
        payload = {
            "message": message,
            "project": project,
            "agentId": agent_id,
        }

        with self._client.stream(
            "POST", url, json=payload,
            headers={"Accept": "text/event-stream"},
            timeout=httpx.Timeout(timeout),
        ) as resp:
            if resp.status_code != 200:
                body = resp.read().decode()
                raise OrchestratorError(f"Agent prompt failed: {body}", resp.status_code)

            for line in resp.iter_lines():
                if not line.startswith("data: "):
                    continue
                data_str = line[6:]
                if data_str == "[DONE]":
                    break
                try:
                    evt = json.loads(data_str)
                except json.JSONDecodeError:
                    continue
                yield evt

    # ── Scheduler ───────────────────────────────────────────────────────

    def list_jobs(self) -> list:
        """List scheduled jobs."""
        resp = self._get("/api/scheduler")
        return resp.get("jobs", [])

    def create_job(self, name: str, project: str, message: str,
                   schedule_type: str = "manual", cron_expr: str = "",
                   every_seconds: int = 0, **kwargs) -> dict:
        """Create a scheduled job."""
        payload = {
            "name": name,
            "project": project,
            "message": message,
            "scheduleType": schedule_type,
            "cronExpr": cron_expr,
            "everySeconds": every_seconds,
            **kwargs,
        }
        return self._post("/api/scheduler", payload)

    def trigger_job(self, job_id: str) -> dict:
        """Manually trigger a scheduled job."""
        return self._post(f"/api/scheduler/{job_id}/trigger", {})

    # ── Projects ────────────────────────────────────────────────────────

    def list_projects(self) -> list:
        """List registered projects."""
        resp = self._get("/api/projects")
        return resp.get("projects", [])

    def register_project(self, name: str, path: str = "", repo_url: str = "") -> dict:
        """Register a new project."""
        payload = {"name": name}
        if path:
            payload["path"] = path
        if repo_url:
            payload["repoUrl"] = repo_url
        return self._post("/api/projects/register", payload)

    # ── Internal helpers ────────────────────────────────────────────────

    def _get(self, path: str) -> dict:
        resp = self._client.get(path)
        return self._handle_response(resp)

    def _post(self, path: str, payload: dict) -> dict:
        resp = self._client.post(path, json=payload)
        return self._handle_response(resp)

    def _delete(self, path: str) -> dict:
        resp = self._client.delete(path)
        return self._handle_response(resp)

    @staticmethod
    def _handle_response(resp: httpx.Response) -> dict:
        if resp.status_code >= 400:
            try:
                body = resp.json()
                raise OrchestratorError(
                    body.get("error", body.get("message", f"HTTP {resp.status_code}")),
                    status_code=resp.status_code,
                )
            except json.JSONDecodeError:
                raise OrchestratorError(f"HTTP {resp.status_code}: {resp.text[:200]}",
                                        status_code=resp.status_code)
        return resp.json()

    def close(self):
        """Close the HTTP client."""
        self._client.close()

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()
