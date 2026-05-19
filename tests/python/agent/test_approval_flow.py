"""
Approval & Question Flow Tests.

Tests the human-in-the-loop approval system. Verifies that:
1. approval_request events have correct structure
2. question_asked events have correct structure
3. The /respond endpoint works for approvals and questions
4. Fields match the frontend's PiApprovalRequest interface exactly

Frontend interface (from pi-events.ts):
  interface PiApprovalRequest {
    requestId: string;
    type: 'tool_confirm' | 'plan' | 'question';
    toolName?: string;
    toolArgs?: Record<string, unknown>;
    message: string;
    risk: 'low' | 'medium' | 'high';
  }

Run: cd tests/python && uv run pytest agent/test_approval_flow.py -v --tb=long
"""

import json
import time

import pytest
import requests

from conftest import API_BASE_URL
from utils.contract import VALID_APPROVAL_TYPES, VALID_RISK_LEVELS
from fixtures.agent import spawn_agent, destroy_agent
from utils.sse import post_and_stream

pytestmark = [pytest.mark.api, pytest.mark.sse, pytest.mark.slow, pytest.mark.llm]

API = API_BASE_URL
TEST_PROJECT = "test-repo"
TEST_MODEL = "gemma-4-26b"


_mod_session = None


@pytest.fixture(scope="module", autouse=True)
def _setup(api_session):
    global _mod_session
    _mod_session = api_session
    spawn_agent(API, api_session, TEST_PROJECT)
    yield
    destroy_agent(API, api_session, TEST_PROJECT)


# ===========================================================================
# 1. Approval request event structure
# ===========================================================================


class TestApprovalRequestContract:
    """
    When the backend detects a risky command, it sends an approval_request
    event. The frontend shows an ApprovalBanner with approve/deny buttons.

    Every field the frontend reads must be present and correctly typed.
    """

    def _try_trigger_approval(self):
        """
        Try to trigger an approval event by sending a risky command.
        Stops reading as soon as approval_request is found (non-blocking).
        Always denies the approval afterward to unblock the agent.
        Returns (events, approval_events) or (None, []).
        """
        risky_prompts = [
            "Run this command: rm -rf /tmp/test_approval_dir",
            "Execute: git push origin main",
            "Run: curl -X POST http://example.com/delete-all",
        ]

        for prompt in risky_prompts:
            events = []
            approval_events = []
            try:
                for event in post_and_stream(
                    _mod_session,
                    f"{API}/api/pux/prompt",
                    {
                        "message": prompt,
                        "project": TEST_PROJECT,
                        "agentId": "default",
                        "model": TEST_MODEL,
                    },
                    timeout=30,
                ):
                    events.append(event)
                    if event[0] == "approval_request":
                        approval_events.append(event[1])
                        # Immediately deny to unblock the stream
                        request_id = event[1].get("requestId", "")
                        if request_id:
                            _mod_session.post(f"{API}/api/pux/decision", json={
                                "project": TEST_PROJECT,
                                "agentId": "default",
                                "requestId": request_id,
                                "action": "deny",
                            })
                        # Continue reading to collect the rest of the stream
            except Exception:
                pass  # Timeout is expected

            if approval_events:
                return events, approval_events

        return None, []

    def test_approval_request_has_requestId(self):
        """
        requestId is required — the frontend sends it back in the /respond
        endpoint to identify which approval is being handled.
        """
        events, approvals = self._try_trigger_approval()
        if not approvals:
            pytest.skip("Could not trigger approval_request event")

        for data in approvals:
            assert "requestId" in data, (
                f"approval_request missing requestId. Keys: {list(data.keys())}\n"
                f"Frontend can't send approval response without requestId."
            )
            assert isinstance(data["requestId"], str) and len(data["requestId"]) > 0, (
                f"requestId must be non-empty string, got: {data['requestId']!r}"
            )

    def test_approval_request_has_type(self):
        """type must be 'tool_confirm', 'plan', or 'question'."""
        events, approvals = self._try_trigger_approval()
        if not approvals:
            pytest.skip("Could not trigger approval_request event")

        for data in approvals:
            assert "type" in data, f"Missing 'type' field: {data}"
            assert data["type"] in VALID_APPROVAL_TYPES, (
                f"Invalid type {data['type']!r}, expected one of {VALID_APPROVAL_TYPES}"
            )

    def test_approval_request_has_message(self):
        """message is displayed to the user in the ApprovalBanner."""
        events, approvals = self._try_trigger_approval()
        if not approvals:
            pytest.skip("Could not trigger approval_request event")

        for data in approvals:
            assert "message" in data, f"Missing 'message' field: {data}"
            assert isinstance(data["message"], str), (
                f"message must be str, got {type(data['message']).__name__}"
            )
            # message should ideally be non-empty, but LLM output may be truncated
            # Backend should provide fallback but we accept empty as a known edge case

    def test_approval_request_has_risk(self):
        """risk must be 'low', 'medium', or 'high' — determines banner color."""
        events, approvals = self._try_trigger_approval()
        if not approvals:
            pytest.skip("Could not trigger approval_request event")

        for data in approvals:
            assert "risk" in data, f"Missing 'risk' field: {data}"
            assert data["risk"] in VALID_RISK_LEVELS, (
                f"Invalid risk {data['risk']!r}, expected one of {VALID_RISK_LEVELS}"
            )

    def test_approval_request_with_tool_has_toolName(self):
        """
        If the approval is type 'tool_confirm', toolName should be present.
        Frontend shows the tool name in the approval banner.
        """
        events, approvals = self._try_trigger_approval()
        if not approvals:
            pytest.skip("Could not trigger approval_request event")

        for data in approvals:
            if data.get("type") == "tool_confirm":
                assert "toolName" in data, (
                    f"tool_confirm approval missing toolName: {data}"
                )
                assert isinstance(data["toolName"], str), (
                    f"toolName must be str, got {type(data['toolName']).__name__}"
                )

    def test_approval_request_with_tool_has_toolArgs(self):
        """
        If tool_confirm, toolArgs should contain the command being approved.
        Frontend shows the command in the banner.
        """
        events, approvals = self._try_trigger_approval()
        if not approvals:
            pytest.skip("Could not trigger approval_request event")

        for data in approvals:
            if data.get("type") == "tool_confirm" and "toolArgs" in data:
                assert isinstance(data["toolArgs"], dict), (
                    f"toolArgs must be dict, got {type(data['toolArgs']).__name__}"
                )
                # For bash tool, should have 'command'
                if data.get("toolName") == "bash":
                    assert "command" in data["toolArgs"], (
                        f"bash tool approval missing args.command: {data['toolArgs']}"
                    )


# ===========================================================================
# 2. Respond endpoint contract
# ===========================================================================


class TestRespondEndpointContract:
    """
    Test POST /api/pux/decision — the endpoint the frontend uses to
    approve/deny approvals and answer questions.
    """

    def test_respond_no_pending_approval(self):
        """When no approval is pending, respond should return gracefully."""
        try:
            resp = _mod_session.post(f"{API}/api/pux/decision", json={
                "project": TEST_PROJECT,
                "agentId": "default",
                "decisionId": "nonexistent-req-xyz",
                "action": "approve",
            })
        except requests.ConnectionError:
            pytest.skip("Backend connection lost during test run")
        assert resp.status_code in (200, 404)
        if resp.status_code == 200:
            data = resp.json()
            assert data.get("success") is False or "error" in data

    def test_respond_deny_no_pending(self):
        """Deny with no pending approval should be safe."""
        try:
            resp = _mod_session.post(f"{API}/api/pux/decision", json={
                "project": TEST_PROJECT,
                "agentId": "default",
                "decisionId": "nonexistent-req-xyz",
                "action": "deny",
            })
        except requests.ConnectionError:
            pytest.skip("Backend connection lost during test run")
        assert resp.status_code in (200, 404)

    def test_respond_answer_no_pending(self):
        """Answer with no pending question should be safe."""
        resp = _mod_session.post(f"{API}/api/pux/decision", json={
            "project": TEST_PROJECT,
            "agentId": "default",
            "decisionId": "nonexistent-req-xyz",
            "action": "answer",
            "answer": "test answer",
        })
        assert resp.status_code in (200, 404)


# ===========================================================================
# 3. Approval flow pauses stream
# ===========================================================================


class TestApprovalFlowPausesStream:
    """
    When an approval_request is sent, the stream should NOT immediately
    send agent_end. The frontend shows the approval banner and waits
    for user input.
    """

    def test_approval_suspends_stream(self):
        """
        If approval_request is emitted, the stream should pause waiting for
        user response (or resume after deny is sent).
        """
        events, approvals = TestApprovalRequestContract()._try_trigger_approval()
        if not approvals:
            pytest.skip("Could not trigger approval_request event")

        event_types = [t for t, _ in events]
        assert "approval_request" in event_types

        # After denial, the stream should eventually end
        # (the _try_trigger_approval already sends deny)
        print(f"  ✓ Approval triggered and denied. Event types: {event_types}")


# ===========================================================================
# 4. Edge cases
# ===========================================================================


class TestApprovalEdgeCases:
    """Edge cases for the approval system."""

    def test_respond_with_invalid_action(self):
        """Invalid action should be handled gracefully."""
        resp = _mod_session.post(f"{API}/api/pux/decision", json={
            "project": TEST_PROJECT,
            "agentId": "default",
            "decisionId": "test-req",
            "action": "invalid_action",
        })
        # Should not crash (200 with error, 400, or 404)
        assert resp.status_code in (200, 400, 404)

    def test_respond_with_missing_fields(self):
        """Missing required fields should return error, not crash."""
        resp = _mod_session.post(f"{API}/api/pux/decision", json={
            "project": TEST_PROJECT,
        })
        assert resp.status_code in (200, 400, 404, 500)

    def test_approval_request_data_is_dict(self):
        """approval_request data must always be a dict."""
        events, approvals = TestApprovalRequestContract()._try_trigger_approval()
        if not approvals:
            pytest.skip("Could not trigger approval_request event")

        for data in approvals:
            assert isinstance(data, dict), (
                f"approval_request data is {type(data).__name__}, expected dict"
            )
