"""
E2E tests for the unified agent framework — all three modes:

1. Coding mode  — pure CLI (bash/file operations)
2. Browser mode — web automation (browse_to, click, type, read_page)
3. Computer mode — desktop automation (screenshot, click, type via X11)

Requires:
  - Go backend running on localhost:3847
  - llama-server running with vision + thinking on localhost:8001
  - At least one sandbox with browser mode enabled

Run:
  pytest tests/python/agent/test_unified_modes.py -v -s
  pytest tests/python/agent/test_unified_modes.py -v -s -k coding
  pytest tests/python/agent/test_unified_modes.py -v -s -k browser
  pytest tests/python/agent/test_unified_modes.py -v -s -k computer
"""

import json
import time

import pytest

from utils.sse import post_and_stream
from utils.contract import validate_sse_event


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def collect_prompt(session, api_url, project, message, agent_id=None, timeout=120):
    """Send a prompt and return (events, tool_calls, text_parts, errors)."""
    if agent_id is None:
        agent_id = f"e2e-{int(time.time()*1000)}"

    events = list(post_and_stream(
        session,
        f"{api_url}/api/pi/prompt",
        {
            "message": message,
            "project": project,
            "agentId": agent_id,
        },
        timeout=timeout,
    ))

    tool_starts = []
    tool_ends = []
    text_parts = []
    thinking_parts = []
    errors = []

    for event_type, data in events:
        if event_type == "tool_execution_start":
            tool_starts.append(data)
        elif event_type == "tool_execution_end":
            tool_ends.append(data)
        elif event_type == "text_delta":
            text_parts.append(data.get("text", ""))
        elif event_type == "thinking_delta":
            thinking_parts.append(data.get("text", ""))
        elif event_type == "error":
            errors.append(data.get("error", ""))

    return {
        "events": events,
        "tool_starts": tool_starts,
        "tool_ends": tool_ends,
        "text": "".join(text_parts),
        "thinking": "".join(thinking_parts),
        "errors": errors,
        "agent_id": agent_id,
    }


def validate_sse_events(events):
    """Validate all SSE events against the contract. Returns list of violations."""
    violations = []
    for event_type, data in events:
        v = validate_sse_event(event_type, data)
        if v:
            violations.extend(v)
    return violations


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture(scope="module")
def sandbox_with_browser(api_url, api_session):
    """Ensure a sandbox exists with browser mode enabled. Returns sandbox ID."""
    # List existing sandboxes
    resp = api_session.get(f"{api_url}/api/sandbox/", timeout=10)
    assert resp.status_code == 200
    sandboxes = resp.json()

    # Find one with browser mode
    for sb in sandboxes:
        sb_id = sb["id"]
        mode = sb.get("mode", "")
        if mode in ("browser", "desktop"):
            return sb_id

    # Enable browser mode on the first sandbox
    if sandboxes:
        sb_id = sandboxes[0]["id"]
        resp = api_session.post(
            f"{api_url}/api/sandbox/{sb_id}/browser-mode",
            json={},
            timeout=60,
        )
        assert resp.status_code in (200, 201), f"Failed to enable browser mode: {resp.text}"
        time.sleep(3)  # Wait for CDP to be ready
        return sb_id

    pytest.skip("No sandbox available for browser tests")


# ---------------------------------------------------------------------------
# Mode 1: Coding — pure CLI
# ---------------------------------------------------------------------------

class TestCodingMode:
    """Test the coding/CLI mode — bash, file operations."""

    def test_echo_command(self, api_url, api_session, test_project):
        """Agent should run 'echo hello' and return the output."""
        result = collect_prompt(
            api_session, api_url, test_project,
            "Run the command: echo hello",
            agent_id="coding-echo",
        )

        # Should have tool calls
        assert len(result["tool_starts"]) > 0, "No tool calls made"

        # Should have bash tool call
        bash_tools = [t for t in result["tool_starts"] if t.get("toolName") == "bash"]
        assert len(bash_tools) > 0, f"No bash tool calls. Got: {[t.get('toolName') for t in result['tool_starts']]}"

        # Should have agent_spawned event
        spawned = [e for e, d in result["events"] if e == "agent_spawned"]
        assert len(spawned) >= 1, "Missing agent_spawned event"

        # Should have agent_end event
        agent_ends = [e for e, d in result["events"] if e == "agent_end"]
        assert len(agent_ends) >= 1, "Missing agent_end event"

        # No errors
        assert len(result["errors"]) == 0, f"Errors: {result['errors']}"

    def test_create_file(self, api_url, api_session, test_project):
        """Agent should create a file and report success."""
        result = collect_prompt(
            api_session, api_url, test_project,
            "Create a file called /tmp/test_e2e.txt with the content 'hello e2e'",
            agent_id="coding-file",
        )

        assert len(result["tool_starts"]) > 0, "No tool calls made"
        assert len(result["errors"]) == 0, f"Errors: {result['errors']}"

    def test_sse_contract(self, api_url, api_session, test_project):
        """All SSE events must satisfy the frontend contract."""
        result = collect_prompt(
            api_session, api_url, test_project,
            "echo test-contract",
            agent_id="coding-contract",
        )

        violations = validate_sse_events(result["events"])
        assert len(violations) == 0, f"SSE contract violations:\n" + "\n".join(violations)


# ---------------------------------------------------------------------------
# Mode 2: Browser — web automation
# ---------------------------------------------------------------------------

class TestBrowserMode:
    """Test the browser mode — browse_to, read_page, click, type."""

    def test_browse_to_page(self, api_url, api_session, test_project, sandbox_with_browser):
        """Agent should navigate to example.com and read the page."""
        result = collect_prompt(
            api_session, api_url, test_project,
            "Browse to https://example.com and tell me what the page heading says",
            agent_id="browser-navigate",
            timeout=180,
        )

        # Should have browse_to or computer_use tool calls
        web_tools = [
            t for t in result["tool_starts"]
            if t.get("toolName") in ("browse_to", "computer_use_act", "navigate")
        ]
        # Orchestrator might delegate — check subagent events
        subagent_starts = [e for e, d in result["events"] if e == "subagent_start"]

        assert len(web_tools) > 0 or len(subagent_starts) > 0, \
            f"No browser tool calls or subagent delegation. Tools: {[t.get('toolName') for t in result['tool_starts']]}"

        # Should complete without critical errors
        assert len(result["errors"]) == 0, f"Errors: {result['errors']}"

    def test_sse_contract(self, api_url, api_session, test_project, sandbox_with_browser):
        """Browser mode SSE events must satisfy the frontend contract."""
        result = collect_prompt(
            api_session, api_url, test_project,
            "Navigate to https://example.com",
            agent_id="browser-contract",
            timeout=120,
        )

        violations = validate_sse_events(result["events"])
        # Filter out violations from orchestrator events (plan_created etc are valid)
        critical = [v for v in violations if "Unknown event type" not in v]
        assert len(critical) == 0, f"SSE contract violations:\n" + "\n".join(critical)


# ---------------------------------------------------------------------------
# Mode 3: Computer — desktop automation
# ---------------------------------------------------------------------------

class TestComputerMode:
    """Test the computer/desktop mode — screenshot, click, type via X11."""

    def test_desktop_screenshot(self, api_url, api_session, test_project, sandbox_with_browser):
        """Agent should take a desktop screenshot and describe what it sees."""
        result = collect_prompt(
            api_session, api_url, test_project,
            "Take a desktop screenshot and describe what you see",
            agent_id="computer-screenshot",
            timeout=180,
        )

        # Should have screenshot-related tool calls or subagent delegation
        screenshot_tools = [
            t for t in result["tool_starts"]
            if t.get("toolName") in ("desktop_screenshot", "computer_use_screenshot", "screenshot")
        ]
        subagent_starts = [e for e, d in result["events"] if e == "subagent_start"]

        assert len(screenshot_tools) > 0 or len(subagent_starts) > 0, \
            f"No screenshot tool calls or subagent delegation. Tools: {[t.get('toolName') for t in result['tool_starts']]}"

    def test_sse_contract(self, api_url, api_session, test_project, sandbox_with_browser):
        """Computer mode SSE events must satisfy the frontend contract."""
        result = collect_prompt(
            api_session, api_url, test_project,
            "Take a screenshot",
            agent_id="computer-contract",
            timeout=120,
        )

        violations = validate_sse_events(result["events"])
        critical = [v for v in violations if "Unknown event type" not in v]
        assert len(critical) == 0, f"SSE contract violations:\n" + "\n".join(critical)
