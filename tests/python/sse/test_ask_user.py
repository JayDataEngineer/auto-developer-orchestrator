"""
ask_user SSE Pipeline Tests — backend decision flow.

Verifies that:
1. The LLM calls ask_user when prompted
2. A decision_request SSE event is emitted with correct structure
3. POST /api/pux/decision resolves the pending decision
4. The tool returns the user's answer to the LLM
5. The model produces a final response incorporating the answer

Run: cd tests/python && uv run pytest sse/test_ask_user.py -v
"""

import json
import time

import pytest

from conftest import API_BASE_URL
from utils.sse import post_and_stream, collect_events

pytestmark = [pytest.mark.api, pytest.mark.sse, pytest.mark.slow, pytest.mark.llm]

API = API_BASE_URL
TEST_PROJECT = "auto-developer-orchestrator"

# Prompt engineered to reliably trigger ask_user on most models
ASK_USER_PROMPT = (
    'Call the ask_user tool with question="Pick a number" '
    'and options=["1","2"]. Do not respond yourself - only use the tool.'
)


def _stream_ask_user(session):
    """Send the ask_user trigger prompt and yield SSE events.

    Returns a generator; the caller decides when to stop (e.g. after
    collecting the decision_request event).
    """
    return post_and_stream(
        session,
        f"{API}/api/pux/prompt",
        {"message": ASK_USER_PROMPT, "project": TEST_PROJECT},
        timeout=120,
    )


class TestAskUserSSEEvents:
    """Verify decision_request SSE event structure and resolution."""

    def test_decision_request_event_emitted(self, api_session):
        """ask_user must emit a decision_request SSE event."""
        decision_events = []
        tool_start_events = []

        for event_type, data in _stream_ask_user(api_session):
            if event_type == "tool_execution_start":
                if data.get("toolName") == "ask_user":
                    tool_start_events.append(data)
            if event_type == "decision_request":
                decision_events.append(data)
                break  # Got it — stop streaming
            if event_type in ("agent_end", "error"):
                break

        assert len(tool_start_events) > 0, (
            "Model did not call ask_user tool. "
            "Tool start events: "
            f"{[e.get('toolName') for e in collect_events([('t', d) for d in []], 'tool_execution_start')]}"
        )
        assert len(decision_events) > 0, (
            "ask_user was called but no decision_request event was emitted"
        )

    def test_decision_request_payload_structure(self, api_session):
        """decision_request must contain decisionId, sourceTool, title, hint, options."""
        decision = None
        for event_type, data in _stream_ask_user(api_session):
            if event_type == "decision_request":
                decision = data
                break
            if event_type in ("agent_end", "error"):
                break

        if decision is None:
            pytest.skip("Model did not call ask_user (decision_request not received)")

        assert "decisionId" in decision, "Missing decisionId"
        assert decision["sourceTool"] == "ask_user", f"Expected sourceTool=ask_user, got {decision.get('sourceTool')}"
        assert decision["hint"] == "question", f"Expected hint=question, got {decision.get('hint')}"
        assert "title" in decision, "Missing title (question text)"
        assert isinstance(decision.get("options"), list), "options must be an array"
        assert len(decision["options"]) >= 2, "Must have at least 2 options"

    def test_decision_resolution_and_tool_result(self, api_session):
        """Resolving a decision must produce a tool_execution_end with the answer."""
        decision_id = None
        events_after_resolve = []

        for event_type, data in _stream_ask_user(api_session):
            if event_type == "decision_request" and decision_id is None:
                decision_id = data["decisionId"]
                # Resolve immediately
                resp = api_session.post(
                    f"{API}/api/pux/decision",
                    json={
                        "decisionId": decision_id,
                        "action": "answer",
                        "value": "1",
                    },
                    timeout=10,
                )
                assert resp.status_code == 200, f"Decision resolution failed: {resp.status_code}"
                assert resp.json().get("success") is True, f"Unexpected response: {resp.json()}"

            if decision_id is not None:
                events_after_resolve.append((event_type, data))

            if event_type in ("agent_end",) and decision_id is not None:
                break
            if event_type == "error" and decision_id is not None:
                break

        if decision_id is None:
            pytest.skip("Model did not call ask_user")

        # Find the tool_execution_end for ask_user
        tool_ends = [
            d for t, d in events_after_resolve
            if t == "tool_execution_end" and d.get("toolName") == "ask_user"
        ]
        assert len(tool_ends) > 0, "No tool_execution_end for ask_user after resolution"

        result = tool_ends[0].get("result", {})
        if isinstance(result, str):
            result = json.loads(result)
        assert result.get("response") == "1", f"Expected response='1', got {result}"

    def test_decision_resolution_returns_not_found_for_unknown(self, api_session):
        """Resolving a non-existent decision must return 404."""
        resp = api_session.post(
            f"{API}/api/pux/decision",
            json={
                "decisionId": "nonexistent_decision_id_12345",
                "action": "answer",
                "value": "test",
            },
            timeout=10,
        )
        assert resp.status_code == 404, f"Expected 404, got {resp.status_code}"

    def test_full_ask_user_round_trip(self, api_session):
        """End-to-end: prompt → ask_user → decision_request → resolve → model response."""
        decision_id = None
        all_events = []

        for event_type, data in _stream_ask_user(api_session):
            all_events.append((event_type, data))

            if event_type == "decision_request" and decision_id is None:
                decision_id = data["decisionId"]
                api_session.post(
                    f"{API}/api/pux/decision",
                    json={"decisionId": decision_id, "action": "answer", "value": "1"},
                    timeout=10,
                )

            if event_type == "agent_end":
                break
            if event_type == "error" and decision_id is not None:
                break

        if decision_id is None:
            pytest.skip("Model did not call ask_user")

        # Verify the full event sequence
        types = [t for t, _ in all_events]
        assert "tool_execution_start" in types, "Missing tool_execution_start"
        assert "decision_request" in types, "Missing decision_request"
        assert "tool_execution_end" in types, "Missing tool_execution_end"
        assert "agent_end" in types, "Missing agent_end"

        # Verify model got the answer and responded
        text_deltas = collect_events(all_events, "text_delta")
        text = " ".join(d.get("text", "") for d in text_deltas)
        # Model should mention the answer "1" somewhere
        assert "1" in text or "selected" in text.lower() or "picked" in text.lower(), (
            f"Model response doesn't reference the answer. Text: {text[:300]}"
        )
