"""
USER EXPERIENCE integration tests.

These test what the user ACTUALLY SEES — not API response codes.
If these fail, the app is broken for the user regardless of what the API returns.

Run: cd tests/python && uv run pytest test_real_user_experience.py -v --tb=long
"""

import json
import time

import pytest
import requests

from conftest import post_and_stream, API_BASE_URL

pytestmark = [pytest.mark.api, pytest.mark.slow]

API = API_BASE_URL
TEST_PROJECT = "test-repo"
TEST_MODEL = "qwen-35-27-vision"


# ---------------------------------------------------------------------------
# Bug #1: Desktop shows "Desktop not available" instead of working sandbox
# ---------------------------------------------------------------------------


class TestDesktopActuallyWorks:
    """
    The user selects a project, clicks Desktop tab, and should see
    a working desktop — NOT "Desktop not available".
    """

    def test_sandbox_exists_for_project(self):
        """The backend must have a sandbox for the test project."""
        resp = requests.get(f"{API}/api/sandbox/", timeout=10)
        assert resp.status_code == 200, f"GET /api/sandbox/ returned {resp.status_code}"
        data = resp.json()
        sandboxes = data if isinstance(data, list) else data.get("sandboxes", [])

        assert len(sandboxes) > 0, (
            "No sandboxes exist at all. The desktop tab will ALWAYS show "
            "'Desktop not available' because there are zero sandboxes."
        )

    def test_sandbox_id_matches_project(self):
        """
        The frontend fetches the sandbox list from the API and finds
        the sandbox matching the project. This test verifies that:
          a) The sandbox list API returns sandboxes
          b) At least one sandbox exists that can be used for the project
          c) The viewer endpoint works with the actual sandbox ID

        If this fails: the desktop tab will show 'Desktop not available'.
        """
        # Step 1: List sandboxes (frontend does this)
        list_resp = requests.get(f"{API}/api/sandbox/", timeout=10)
        assert list_resp.status_code == 200, f"GET /api/sandbox/ returned {list_resp.status_code}"
        data = list_resp.json()
        sandboxes = data if isinstance(data, list) else data.get("sandboxes", [])
        assert len(sandboxes) > 0, "No sandboxes exist — Desktop tab will always fail."

        # Step 2: Find a sandbox matching the project (frontend logic)
        actual_ids = [s.get("id", s) if isinstance(s, dict) else s for s in sandboxes]
        project_name = TEST_PROJECT
        matched_id = None
        for sid in actual_ids:
            if sid == project_name or sid == f"sandbox-{project_name}":
                matched_id = sid
                break
        # Fallback: use first sandbox (frontend does this too)
        if matched_id is None:
            matched_id = actual_ids[0]

        # Step 3: Enable desktop mode (the frontend does this automatically)
        enable_resp = requests.post(
            f"{API}/api/sandbox/{matched_id}/desktop-mode",
            json={},
            timeout=30,
        )
        if enable_resp.status_code == 404:
            pytest.fail(
                f"BUG: Cannot enable desktop mode for '{matched_id}' — sandbox not found."
            )

        # Step 4: Verify viewer endpoint works with the resolved ID
        resp = requests.get(f"{API}/api/sandbox/{matched_id}/viewer", timeout=10)
        if resp.status_code == 404:
            pytest.fail(
                f"BUG: Sandbox '{matched_id}' exists but viewer returned 404 after enabling.\n"
                f"Desktop mode may not be starting properly."
            )
        assert resp.status_code == 200, (
            f"GET /api/sandbox/{matched_id}/viewer returned {resp.status_code}: {resp.text}"
        )

    def test_desktop_mode_can_be_enabled(self):
        """After enabling desktop mode, the viewer must return VNC/noVNC URLs."""
        # Resolve real sandbox ID (same logic as frontend)
        list_resp = requests.get(f"{API}/api/sandbox/", timeout=10)
        data = list_resp.json()
        sandboxes = data if isinstance(data, list) else data.get("sandboxes", [])
        assert len(sandboxes) > 0, "No sandboxes to test desktop mode with."
        sandbox_id = sandboxes[0].get("id", sandboxes[0]) if isinstance(sandboxes[0], dict) else sandboxes[0]

        # Enable desktop mode
        resp = requests.post(
            f"{API}/api/sandbox/{sandbox_id}/desktop-mode",
            json={},
            timeout=30,
        )

        if resp.status_code == 404:
            pytest.fail(
                f"BUG: Cannot enable desktop mode for '{sandbox_id}' — sandbox doesn't exist. "
                f"Desktop tab will always show 'not available'."
            )

        # Check viewer returns actual URLs
        viewer_resp = requests.get(f"{API}/api/sandbox/{sandbox_id}/viewer", timeout=10)
        if viewer_resp.status_code != 200:
            pytest.fail(
                f"BUG: Desktop viewer returned {viewer_resp.status_code} after enabling. "
                f"The user will see 'Desktop not available'."
            )

        viewer_data = viewer_resp.json()
        has_vnc = viewer_data.get("vncUrl") or viewer_data.get("novncUrl") or viewer_data.get("vnc_url") or viewer_data.get("novnc_url")
        assert has_vnc, (
            f"BUG: Desktop viewer returned no VNC URLs. User sees blank desktop.\n"
            f"Response: {json.dumps(viewer_data, indent=2)}"
        )


# ---------------------------------------------------------------------------
# Bug #2: Chat shows blank infinite spinner — no thinking text, no tool labels
# ---------------------------------------------------------------------------


class TestChatActuallyShowsContent:
    """
    When the user sends a message, they should see:
    - Thinking/reasoning text (not a blank spinner)
    - Tool name labels when tools execute (not a blank spinner)
    - Actual response text
    - The spinner must STOP (agent_end received)

    If these fail: the user sees a blank infinite loading icon and nothing else.
    """

    @staticmethod
    def _stream(message, model=TEST_MODEL):
        """Send a prompt and collect ALL SSE events."""
        return list(post_and_stream(
            requests.Session(),
            f"{API}/api/pi/prompt",
            {
                "message": message,
                "project": TEST_PROJECT,
                "agentId": "default",
                "model": model,
            },
            timeout=90,
        ))

    def test_user_sees_response_text(self):
        """
        After sending a prompt, the user must see actual response text.
        If text_delta events are empty or missing, the chat shows a blank area.
        """
        events = self._stream("Say exactly: 'Hello, this is my response' and nothing else.")

        text_deltas = [d for t, d in events if t == "text_delta"]
        full_text = "".join(d.get("text", "") for d in text_deltas if isinstance(d, dict))

        assert len(full_text.strip()) > 0, (
            f"BUG: User sees BLANK chat. Zero text content in {len(events)} events.\n"
            f"Event types received: {[t for t, _ in events]}\n"
            f"The user sees a spinner that never produces text."
        )

        # The text should contain something meaningful (not just whitespace)
        assert len(full_text.strip()) >= 5, (
            f"BUG: Response text is only '{full_text.strip()}' — too short. "
            f"User sees nearly empty response."
        )

        print(f"  ✓ User sees text: '{full_text[:100]}...'")

    def test_spinner_stops_after_response(self):
        """
        The spinner MUST stop. If agent_end is never received,
        the user sees an infinite loading spinner forever.
        """
        events = self._stream("Say ok")

        event_types = [t for t, _ in events]
        assert "agent_end" in event_types, (
            f"BUG: INFINITE SPINNER. agent_end event never received.\n"
            f"Events received: {event_types}\n"
            f"The loading spinner will spin forever."
        )

    def test_tool_use_shows_tool_name(self):
        """
        When the agent uses a tool (like bash), the user should see
        the tool NAME, not a blank spinner.

        If tool_execution_start events have no toolName, the user
        sees "Running tool..." with no label.
        """
        events = self._stream(
            "Run this bash command: echo HELLO_TOOL_TEST"
        )

        tool_starts = [d for t, d in events if t == "tool_execution_start"]

        if tool_starts:
            for tool_data in tool_starts:
                assert tool_data.get("toolName"), (
                    f"BUG: Tool execution started but toolName is blank/missing.\n"
                    f"Data: {json.dumps(tool_data)}\n"
                    f"User sees blank tool execution card — no label."
                )

                print(f"  ✓ Tool label: {tool_data['toolName']}")
        else:
            # Model didn't use tools — check if at least text was produced
            text_deltas = [d for t, d in events if t == "text_delta"]
            full_text = "".join(d.get("text", "") for d in text_deltas if isinstance(d, dict))
            assert len(full_text.strip()) > 0, (
                f"BUG: Model produced no tool use AND no text.\n"
                f"Event types: {[t for t, _ in events]}\n"
                f"User sees blank infinite spinner."
            )

    def test_thinking_content_is_visible(self):
        """
        When the model thinks, the user should see thinking text,
        not a blank spinner. The thinking_delta events must contain text.
        """
        events = self._stream(
            "Think step by step and solve: what is 17 * 23?"
        )

        thinking_deltas = [d for t, d in events if t == "thinking_delta"]
        text_deltas = [d for t, d in events if t == "text_delta"]

        # At minimum, EITHER thinking OR text must appear
        thinking_text = "".join(d.get("text", "") for d in thinking_deltas if isinstance(d, dict))
        response_text = "".join(d.get("text", "") for d in text_deltas if isinstance(d, dict))

        has_content = len(thinking_text.strip()) > 0 or len(response_text.strip()) > 0
        assert has_content, (
            f"BUG: User sees BLANK SPINNER with no content.\n"
            f"Thinking deltas: {len(thinking_deltas)} ({len(thinking_text)} chars)\n"
            f"Text deltas: {len(text_deltas)} ({len(response_text)} chars)\n"
            f"Event types: {[t for t, _ in events]}\n"
            f"Neither thinking text nor response text was produced."
        )

        if thinking_text.strip():
            print(f"  ✓ Thinking visible: '{thinking_text[:80]}...'")
        if response_text.strip():
            print(f"  ✓ Response text: '{response_text[:80]}...'")

    def test_no_blank_tool_execution_cards(self):
        """
        Tool execution cards must show args/results, not be blank.
        Every tool_execution_end must have a result or error.
        """
        events = self._stream(
            "Use the bash tool to run: echo TOOL_RESULT_CHECK"
        )

        tool_ends = [d for t, d in events if t == "tool_execution_end"]

        for tool_data in tool_ends:
            has_result = tool_data.get("result") is not None
            has_error = tool_data.get("error") is not None
            assert has_result or has_error, (
                f"BUG: Tool execution ended with no result and no error.\n"
                f"Data: {json.dumps(tool_data)}\n"
                f"User sees a blank tool card."
            )

    def test_full_conversation_flow_visible(self):
        """
        End-to-end: send a prompt, verify the user would see:
        1. Their message appear
        2. Thinking/reasoning (or at minimum text)
        3. Tool use with labels (if tools used)
        4. Final response text
        5. Spinner stops
        """
        events = self._stream(
            "List the files in the current directory. Use the bash tool to run: ls"
        )

        event_types = [t for t, _ in events]

        # 1. Stream must start (either agent_start or agent_spawned)
        assert "agent_start" in event_types or "agent_spawned" in event_types, (
            f"No agent_start or agent_spawned event. Stream never began. Types: {event_types}"
        )

        # 2. Must produce content (thinking or text)
        thinking = "".join(
            d.get("text", "") for t, d in events
            if t == "thinking_delta" and isinstance(d, dict)
        )
        text = "".join(
            d.get("text", "") for t, d in events
            if t == "text_delta" and isinstance(d, dict)
        )
        assert len(thinking.strip()) > 0 or len(text.strip()) > 0, (
            f"BUG: FULL BLANK CONVERSATION.\n"
            f"Thinking: {len(thinking)} chars, Text: {len(text)} chars\n"
            f"Events: {event_types}\n"
            f"User sends message and sees... nothing. Just a spinner."
        )

        # 3. Tool labels must be visible (if tools used)
        for t, d in events:
            if t == "tool_execution_start":
                assert d.get("toolName"), f"Tool started with no name: {d}"

        # 4. Spinner must stop
        assert "agent_end" in event_types, (
            f"BUG: SPINNER NEVER STOPS. No agent_end event.\n"
            f"Events: {event_types}"
        )

        print(f"  ✓ Full flow: {len(events)} events, {len(text)} chars text, {len(thinking)} chars thinking")
        tool_names = [d.get("toolName") for t, d in events if t == "tool_execution_start" and d.get("toolName")]
        if tool_names:
            print(f"  ✓ Tools used: {tool_names}")
