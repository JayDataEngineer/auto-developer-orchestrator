"""
Integration tests for the Orchestrator Python SDK.

Run: cd sdk/python && source .venv/bin/activate && python -m pytest tests/test_integration.py -v

Tests require the orchestrator backend running at localhost:3847.
MCP tests require MCP servers reachable (Ray cluster).
Sandbox tests require at least one Docker sandbox running.
"""

import json
import os

import pytest

from orch import OrchestratorClient, OrchestratorError, ToolResult

BASE_URL = os.environ.get("ORCH_URL", "http://localhost:3847")


@pytest.fixture
def orch():
    with OrchestratorClient(base_url=BASE_URL) as client:
        yield client


# ── Health ──────────────────────────────────────────────────────────


class TestHealth:
    def test_is_healthy(self, orch):
        assert orch.is_healthy() is True

    def test_health_payload(self, orch):
        h = orch.health()
        assert h["status"] == "ok"


# ── Tools listing ───────────────────────────────────────────────────


class TestToolsList:
    def test_list_tools(self, orch):
        tools = orch.list_tools()
        assert isinstance(tools, list)
        assert len(tools) >= 3

    def test_sandbox_tools_present(self, orch):
        names = [t["name"] for t in orch.list_tools()]
        assert "bash" in names
        assert "file_read" in names
        assert "file_write" in names

    def test_mcp_tools_present(self, orch):
        mcp = [t for t in orch.list_tools() if t["category"] == "mcp"]
        assert len(mcp) >= 5


# ── MCP tool execution ──────────────────────────────────────────────


class TestMCPTools:
    def test_exec_search(self, orch):
        result = orch.mcp_search("test query", max_results=1)
        assert isinstance(result, ToolResult)
        assert result.success is True
        assert len(result.text) > 10

    def test_exec_scrape(self, orch):
        result = orch.mcp_scrape("https://httpbin.org/get")
        assert result.success is True
        assert "httpbin" in result.text.lower()

    def test_exec_tool_generic(self, orch):
        result = orch.exec_tool("search", {"query": "hello world", "top_k": 1})
        assert result.success is True

    def test_exec_unknown_tool(self, orch):
        with pytest.raises(OrchestratorError):
            orch.exec_tool("nonexistent_tool_xyz", {})


# ── Sandbox tools ───────────────────────────────────────────────────


class TestSandboxTools:
    @pytest.fixture
    def sandbox_id(self, orch):
        sandboxes = orch.list_sandboxes()
        if not sandboxes:
            pytest.skip("No sandboxes running")
        return sandboxes[0]["id"]

    def test_bash(self, orch, sandbox_id):
        result = orch.bash(sandbox_id, "echo hello-sdk")
        assert result.success is True
        assert "hello-sdk" in result.text

    def test_file_write_read(self, orch, sandbox_id):
        path = "/tmp/orch_sdk_test.txt"
        content = "SDK integration test content"

        write_result = orch.file_write(sandbox_id, path, content)
        assert write_result.success is True

        read_result = orch.file_read(sandbox_id, path)
        assert read_result.success is True
        assert content in read_result.text

    def test_bash_failure(self, orch, sandbox_id):
        result = orch.bash(sandbox_id, "exit 42")
        # Tool exec succeeds (HTTP 200), but the command failed
        # The result text should contain the exit code info
        assert isinstance(result, ToolResult)

    def test_file_read_missing_path(self, orch, sandbox_id):
        result = orch.file_read(sandbox_id, "/nonexistent/path/xyz.txt")
        # Should still return a result (may succeed with error content)
        assert isinstance(result, ToolResult)


# ── Projects ────────────────────────────────────────────────────────


class TestProjects:
    def test_list_projects(self, orch):
        projects = orch.list_projects()
        assert isinstance(projects, list)


# ── Scheduler ───────────────────────────────────────────────────────


class TestScheduler:
    def test_list_jobs(self, orch):
        jobs = orch.list_jobs()
        assert isinstance(jobs, list)


# ── ToolResult helpers ──────────────────────────────────────────────


class TestToolResultHelpers:
    def test_raise_on_error_success(self):
        r = ToolResult({"success": True, "result": "ok"})
        assert r.raise_on_error() is r

    def test_raise_on_error_failure(self):
        r = ToolResult({"success": False, "error": "boom"})
        with pytest.raises(OrchestratorError):
            r.raise_on_error()

    def test_text_extraction_dict_result(self):
        r = ToolResult({"success": True, "result": {"output": "hello"}})
        assert r.text == "hello"

    def test_text_extraction_string_result(self):
        r = ToolResult({"success": True, "result": "plain text"})
        assert r.text == "plain text"

    def test_text_extraction_content_key(self):
        r = ToolResult({"success": True, "result": {"content": "file data"}})
        assert r.text == "file data"

    def test_repr(self):
        r = ToolResult({"success": True, "tool": "bash"})
        assert "bash" in repr(r)
        assert "ok" in repr(r)
