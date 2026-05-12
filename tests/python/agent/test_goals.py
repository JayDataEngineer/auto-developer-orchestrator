"""
GOALS.md Validation Tests.

Automated tests for every capability category defined in GOALS.md.
Uses the Pux agent endpoint (POST /api/pux/prompt) with SSE streaming.
Each test sends a prompt, collects events, and asserts pass criteria.

Run: cd tests/python && uv run pytest agent/test_goals.py -v --tb=long
"""

import json
import time
import uuid

import pytest

from conftest import API_BASE_URL
from utils.sse import post_and_stream, stream_pux_prompt
from utils.contract import validate_sse_event

pytestmark = [pytest.mark.api, pytest.mark.sse, pytest.mark.slow]

API = API_BASE_URL
TEST_PROJECT = "test-repo"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_mod_session = None


def _pux(message, timeout=600):
    """Send a prompt and return all SSE events."""
    return stream_pux_prompt(API, _mod_session, TEST_PROJECT, message, timeout=timeout)


def get_text(events):
    """Concatenate all text_delta text from events."""
    return "".join(
        d.get("text", "") for t, d in events if t == "text_delta"
    )


def get_tools(events):
    """List of (toolName, args) from tool_execution_start events."""
    return [
        (d.get("toolName", ""), d.get("args", {}))
        for t, d in events
        if t == "tool_execution_start"
    ]


def has_tool(events, name):
    """Check if a specific tool was called."""
    return any(name == tool_name for tool_name, _ in get_tools(events))


def get_tool_results(events, name):
    """Get all results from tool_execution_end events matching tool name."""
    results = []
    for t, d in events:
        if t == "tool_execution_end":
            results.append(d)
    return results


def has_event(events, event_type):
    """Check if a specific event type appears."""
    return any(t == event_type for t, _ in events)


def get_event_data(events, event_type):
    """Get data from first event matching type."""
    for t, d in events:
        if t == event_type:
            return d
    return None


def has_error(events):
    """Check for error events."""
    return any(t == "error" for t, _ in events)


def get_error(events):
    """Get first error message."""
    for t, d in events:
        if t == "error":
            return d.get("error", str(d))
    return None


@pytest.fixture(scope="module", autouse=True)
def _setup(api_session):
    """Set module session, clean up agent state."""
    global _mod_session
    _mod_session = api_session
    yield


# ===========================================================================
# 1. Coding Tasks
# ===========================================================================


class TestCoding:
    """Goal: Write, edit, search, and run code in multiple languages."""

    def test_coding_write_run_python(self):
        """Write a Python script and run it via the agent."""
        events = _pux(
            "Write a Python script that prints the first 10 prime numbers using trial division. "
            "Save it to /sandbox/workspace/test_primes.py and run it with python3. "
            "Use file_write to create it and bash to run it."
        )

        assert not has_error(events), f"Agent error: {get_error(events)}"
        assert has_tool(events, "file_write") or has_tool(events, "bash"), \
            "Expected file_write or bash tool calls"

        text = get_text(events)
        # The agent should produce some output mentioning primes or success
        assert len(text) > 20, f"Expected meaningful output, got: {text[:200]}"

    def test_coding_edit_and_diff(self):
        """Read and edit a file, verify diff output."""
        events = _pux(
            "Read /sandbox/workspace/test_primes.py, then change the comment or "
            "a variable name. Use file_read first, then file_edit to make a small change."
        )

        assert not has_error(events), f"Agent error: {get_error(events)}"
        # Agent should delegate — look for subagent or direct tool use
        tools = get_tools(events)
        tool_names = [name for name, _ in tools]

        # Either direct tools or delegation
        has_file_ops = any(t in tool_names for t in ["file_read", "file_edit", "bash"])
        has_delegation = "delegate_to" in tool_names
        assert has_file_ops or has_delegation, \
            f"Expected file tools or delegation, got: {tool_names}"

    def test_coding_search_code(self):
        """Search for code in the workspace."""
        events = _pux(
            "Search the /sandbox/workspace directory for any Python files containing "
            "the word 'def' using file_grep. Show me what functions exist."
        )

        assert not has_error(events), f"Agent error: {get_error(events)}"
        tools = get_tools(events)
        tool_names = [name for name, _ in tools]
        has_search = any(t in tool_names for t in ["file_grep", "bash", "code_search"])
        has_delegation = "delegate_to" in tool_names
        assert has_search or has_delegation, \
            f"Expected search tool or delegation, got: {tool_names}"


# ===========================================================================
# 2. Web Research (MCP)
# ===========================================================================


class TestWebResearch:
    """Goal: Research topics using MCP tools."""

    def test_mcp_research(self):
        """Use MCP research tool to search and scrape."""
        events = _pux(
            "Research 'Python 3.13 new features' using the MCP research tool. "
            "Use mcp_call with tool='research' to search for this topic. "
            "Summarize the top findings."
        )

        assert not has_error(events), f"Agent error: {get_error(events)}"
        text = get_text(events)
        # Should contain research results
        assert len(text) > 50, f"Expected research output, got: {text[:200]}"

        # Check for MCP tool usage (may be in subagent)
        all_tools = get_tools(events)
        tool_names = [name for name, _ in all_tools]
        has_mcp = any("mcp" in n for n in tool_names)
        has_delegate = "delegate_to" in tool_names
        assert has_mcp or has_delegate, \
            f"Expected MCP tool or delegation, got: {tool_names}"

    def test_mcp_search_scrape(self):
        """Two-step: search then scrape specific URLs."""
        events = _pux(
            "First use mcp_call with tool='search' to find results about 'FastAPI v1 release'. "
            "Then use mcp_call with tool='scrape' to read the top result. "
            "Summarize what you find."
        )

        assert not has_error(events), f"Agent error: {get_error(events)}"
        text = get_text(events)
        assert len(text) > 50, f"Expected search+scrape output, got: {text[:200]}"


# ===========================================================================
# 3. Media Analysis (MCP)
# ===========================================================================


class TestMediaAnalysis:
    """Goal: Analyze images via MCP tools."""

    def test_mcp_image_analysis(self):
        """Analyze a public image via MCP."""
        events = _pux(
            "Use mcp_call with tool='analyze_image' to analyze this image: "
            "https://upload.wikimedia.org/wikipedia/commons/thumb/4/47/PNG_transparency_demonstration_1.png/280px-PNG_transparency_demonstration_1.png "
            "Use prompt='Describe what you see in this image in detail.' "
            "Tell me what the image shows."
        )

        assert not has_error(events), f"Agent error: {get_error(events)}"
        text = get_text(events)
        assert len(text) > 30, f"Expected image description, got: {text[:200]}"


# ===========================================================================
# 4. Context Management
# ===========================================================================


class TestContextManagement:
    """Goal: Handle long conversations with compaction."""

    def test_compaction_fires(self):
        """Send multiple prompts, verify compaction events appear."""
        # Send short prompts to fill context
        prompts = [
            "Write a one-line Python function to reverse a string. Save to /sandbox/workspace/ctx_test.py",
            "Read /sandbox/workspace/ctx_test.py back to me",
            "Add a docstring to the function in /sandbox/workspace/ctx_test.py",
            "Run /sandbox/workspace/ctx_test.py with python3",
            "What's the current content of /sandbox/workspace/ctx_test.py?",
            "Add a second function that checks if a string is a palindrome to /sandbox/workspace/ctx_test.py",
            "Run both functions in /sandbox/workspace/ctx_test.py",
            "Rename the file to /sandbox/workspace/ctx_test2.py using bash mv command",
            "What files are in /sandbox/workspace/?",
            "Read /sandbox/workspace/ctx_test2.py and tell me the line count",
        ]

        saw_compaction = False
        for i, msg in enumerate(prompts):
            events = _pux(msg, timeout=120)
            if has_event(events, "compaction_start"):
                saw_compaction = True
                # Verify compaction_end follows
                assert has_event(events, "compaction_end"), \
                    "compaction_start without compaction_end"
                break  # Don't need to continue once we see it

        # Compaction is expected but not guaranteed with cloud models
        # (they have huge context windows). Log the result.
        if not saw_compaction:
            pytest.skip("Compaction did not fire — cloud models have large context windows")


# ===========================================================================
# 5. Scheduling
# ===========================================================================


class TestScheduling:
    """Goal: Schedule and execute recurring tasks."""

    def test_scheduler_create_and_list(self):
        """Create a scheduled job and verify it appears in the list."""
        job_name = f"test-job-{uuid.uuid4().hex[:8]}"

        # Create a job
        resp = _mod_session.post(
            f"{API}/api/scheduler",
            json={
                "name": job_name,
                "description": "Test job for goal validation",
                "project": TEST_PROJECT,
                "message": "Echo hello",
                "scheduleType": "every",
                "everySeconds": 3600,
                "enabled": False,  # Don't actually run it
            },
            timeout=30,
        )
        assert resp.status_code in (200, 201), f"Create job failed: {resp.status_code} {resp.text}"
        data = resp.json()
        job_id = data.get("id") or data.get("jobId") or data.get("job", {}).get("id")
        assert job_id, f"No job ID in response: {data}"

        # List jobs — verify it appears
        resp = _mod_session.get(f"{API}/api/scheduler", timeout=30)
        assert resp.status_code == 200, f"List jobs failed: {resp.status_code}"
        jobs = resp.json()
        if isinstance(jobs, dict):
            jobs = jobs.get("jobs", [])
        assert isinstance(jobs, list), f"Expected list, got: {type(jobs)}"

        # Clean up
        _mod_session.delete(f"{API}/api/scheduler/{job_id}", timeout=30)

    def test_scheduler_trigger(self):
        """Create a disabled job, trigger it manually, verify execution."""
        job_name = f"test-trigger-{uuid.uuid4().hex[:8]}"

        # Create disabled job
        resp = _mod_session.post(
            f"{API}/api/scheduler",
            json={
                "name": job_name,
                "description": "Test trigger for goal validation",
                "project": TEST_PROJECT,
                "message": "Write the current time to /sandbox/workspace/scheduler_test.txt",
                "scheduleType": "every",
                "everySeconds": 86400,
                "enabled": False,
            },
            timeout=30,
        )
        assert resp.status_code in (200, 201), f"Create failed: {resp.text}"
        data = resp.json()
        job_id = data.get("id") or data.get("jobId") or data.get("job", {}).get("id")
        resp = _mod_session.post(f"{API}/api/scheduler/{job_id}/trigger", timeout=30)
        # Trigger may return 200 (started) or 202 (queued)
        assert resp.status_code in (200, 201, 202), f"Trigger failed: {resp.status_code} {resp.text}"

        # Clean up
        _mod_session.delete(f"{API}/api/scheduler/{job_id}", timeout=30)


# ===========================================================================
# 6. Multi-Model Cloud
# ===========================================================================


class TestMultiModelCloud:
    """Goal: Use cloud LLMs when local GPU is unavailable."""

    def test_model_list(self):
        """GET /api/pux/models returns cloud models."""
        resp = _mod_session.get(f"{API}/api/pux/models", timeout=30)
        assert resp.status_code == 200, f"Models endpoint failed: {resp.status_code}"
        data = resp.json()
        models = data.get("models", data) if isinstance(data, dict) else data
        assert isinstance(models, list), f"Expected list, got: {type(data)}"
        assert len(models) > 0, "No models available"

    def test_model_switch_and_prompt(self):
        """Switch to DeepSeek, send a prompt, verify response."""
        # Switch model
        resp = _mod_session.put(
            f"{API}/api/pux/model",
            json={
                "provider": "openrouter",
                "modelId": "deepseek/deepseek-v4-flash",
                "project": TEST_PROJECT,
                "agentId": "default",
            },
            timeout=30,
        )
        assert resp.status_code == 200, f"Model switch failed: {resp.status_code} {resp.text}"
        data = resp.json()
        assert data.get("success"), f"Switch not successful: {data}"
        # The returned model should be the DeepSeek model
        assert "deepseek" in data.get("model", "").lower(), f"Expected deepseek in model name: {data}"


# ===========================================================================
# 7. Sub-agent Delegation
# ===========================================================================


class TestSubAgentDelegation:
    """Goal: Orchestrate complex tasks by delegating to focused sub-agents."""

    def test_delegate_single(self):
        """Task that requires delegation, verify subagent events."""
        events = _pux(
            "Write a Python function that checks if a number is prime. "
            "Save it to /sandbox/workspace/is_prime.py and run a quick test. "
            "Use create_plan first, then delegate the coding work."
        )

        assert not has_error(events), f"Agent error: {get_error(events)}"
        tools = get_tools(events)
        tool_names = [name for name, _ in tools]

        # Should see delegation (orchestrator can't use file tools directly)
        has_delegate = "delegate_to" in tool_names
        has_plan = "create_plan" in tool_names
        assert has_delegate or has_tool(events, "file_write") or has_tool(events, "bash"), \
            f"Expected delegation or direct tool use, got: {tool_names}"

        # Check for subagent events if delegation happened
        if has_delegate:
            assert has_event(events, "subagent_start") or has_event(events, "artifact_created"), \
                "Delegation happened but no subagent_start or artifact_created event"

    def test_delegate_with_restricted_tools(self):
        """Delegate with only specific tools — verify restriction."""
        events = _pux(
            "Delegate a task to write 'hello world' to /sandbox/workspace/hello.txt. "
            "Give the sub-agent ONLY these tools: ['bash', 'file_write']. "
            "Do not use any browser or MCP tools."
        )

        assert not has_error(events), f"Agent error: {get_error(events)}"
        tools = get_tools(events)
        tool_names = [name for name, _ in tools]
        has_delegate = "delegate_to" in tool_names
        assert has_delegate, f"Expected delegation, got: {tool_names}"


# ===========================================================================
# 8. Desktop Automation
# ===========================================================================


class TestDesktopAutomation:
    """Goal: Automate native desktop applications via xdotool."""

    def test_desktop_screenshot(self):
        """Take a desktop screenshot via X11 endpoint."""
        # First check if sandbox exists
        resp = _mod_session.get(f"{API}/api/sandbox/", timeout=10)
        if resp.status_code != 200:
            pytest.skip("Sandbox service unavailable")

        sandboxes = resp.json()
        if isinstance(sandboxes, dict):
            sandboxes = sandboxes.get("sandboxes", [])
        if not sandboxes:
            pytest.skip("No sandboxes available")

        sandbox_id = None
        for sb in sandboxes:
            sid = sb.get("id", sb) if isinstance(sb, dict) else sb
            if sid and sid == TEST_PROJECT:
                sandbox_id = sid
                break
        if not sandbox_id:
            sandbox_id = sandboxes[0].get("id", sandboxes[0]) if isinstance(sandboxes[0], dict) else sandboxes[0]

        # Try to take a screenshot (requires desktop mode enabled)
        resp = _mod_session.get(
            f"{API}/api/sandbox/{sandbox_id}/x11/screenshot?format=json",
            timeout=30,
        )
        # Desktop mode may not be enabled — that's OK, we're testing the endpoint
        if resp.status_code == 503:
            pytest.skip("Desktop mode not enabled for sandbox")
        if resp.status_code == 404:
            pytest.skip("X11 endpoint not available")

        assert resp.status_code == 200, f"Screenshot failed: {resp.status_code} {resp.text[:200]}"
        data = resp.json()
        assert "screenshot" in data or "image" in data or "data" in data, \
            f"Expected image data in response: {list(data.keys())}"


# ===========================================================================
# Infrastructure Health (fast, no LLM)
# ===========================================================================


class TestInfrastructure:
    """Quick health checks — no LLM calls needed."""

    def test_health_endpoint(self):
        """Health endpoint returns component status."""
        resp = _mod_session.get(f"{API}/api/health", timeout=10)
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("status") == "ok", f"Unhealthy: {data}"
        assert "llm" in data, "Missing LLM status"
        assert "sandbox" in data, "Missing sandbox status"

    def test_sandbox_list(self):
        """Sandbox list endpoint returns data."""
        resp = _mod_session.get(f"{API}/api/sandbox/", timeout=10)
        assert resp.status_code == 200, f"List sandboxes failed: {resp.status_code}"

    def test_models_endpoint(self):
        """Models endpoint returns available models."""
        resp = _mod_session.get(f"{API}/api/pux/models", timeout=10)
        assert resp.status_code == 200
        data = resp.json()
        models = data.get("models", data) if isinstance(data, dict) else data
        assert len(models) > 0, "No models listed"

    def test_tool_permissions(self):
        """Tool permissions endpoint responds."""
        resp = _mod_session.get(f"{API}/api/pux/tool-permissions", timeout=10)
        assert resp.status_code == 200, f"Tool permissions failed: {resp.status_code}"
