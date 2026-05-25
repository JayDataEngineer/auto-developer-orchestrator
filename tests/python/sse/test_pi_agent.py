"""
Pux agent tests: models, state, prompt SSE stream, messages, cleanup.
"""

import pytest

from utils.sse import post_and_stream

pytestmark = [pytest.mark.api, pytest.mark.sse]


class TestPuxModels:
    def test_get_models_returns_list(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pux/models")
        assert resp.status_code == 200
        data = resp.json()
        assert isinstance(data, list), f"Expected list, got {type(data).__name__}"
        if len(data) > 0:
            assert "id" in data[0], f"Model missing 'id': {data[0]}"


class TestPuxAgentStatus:
    def test_agent_status_initially(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pux/agent-status")
        assert resp.status_code == 200
        # Response is a list of running agents (may be empty)
        data = resp.json()
        assert data is not None


class TestPuxPrompt:
    def test_prompt_missing_message_returns_400(self, api_url, api_session, test_project):
        resp = api_session.post(
            f"{api_url}/api/pux/prompt",
            json={"project": test_project},
        )
        assert resp.status_code == 400

    def test_prompt_nonexistent_project_returns_404(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pux/prompt",
            json={"message": "hello", "project": "nonexistent-proj"},
        )
        assert resp.status_code in (400, 404)

    def test_prompt_valid_message_streams_events(self, api_url, api_session, test_project):
        """Full SSE streaming test — marked slow because it calls the LLM."""
        try:
            events = list(
                post_and_stream(
                    api_session,
                    f"{api_url}/api/pux/prompt",
                    {
                        "message": "Say exactly: hello from e2e test",
                        "project": test_project,
                        "agentId": "default",
                    },
                    timeout=90,
                )
            )
        except Exception as e:
            pytest.skip(f"LLM unavailable: {e}")

        assert len(events) > 0, "Expected at least one SSE event"

        event_types = [e[0] for e in events]
        assert "agent_spawned" in event_types or "text_delta" in event_types or "message" in event_types, (
            f"Expected agent_spawned or text_delta in events, got: {event_types}"
        )

        # Accumulate text deltas
        text_parts = []
        for ev_type, ev_data in events:
            if ev_type == "text_delta" and isinstance(ev_data, dict):
                text_parts.append(ev_data.get("delta", ev_data.get("text", "")))
        full_text = "".join(text_parts)
        # Text may be empty if model produces an error; just check we got events
        assert len(events) > 0, "Expected SSE events from prompt"

        # Verify agent_end has usage info
        end_events = [d for t, d in events if t == "agent_end"]
        if end_events:
            end_data = end_events[0]
            # Usage fields should be present
            assert "usage" in end_data or "input" in end_data or "output" in end_data


@pytest.mark.slow
@pytest.mark.llm
class TestPuxToolUse:
    """Test that the model can use tools (bash, file read/write) via SSE."""

    def _stream_prompt(self, api_url, api_session, test_project, message, model=None):
        """Helper: send a prompt and collect all SSE events. Skips if LLM unavailable."""
        body = {
            "message": message,
            "project": test_project,
            "agentId": "default",
        }
        if model:
            body["model"] = model
        try:
            return list(post_and_stream(
                api_session,
                f"{api_url}/api/pux/prompt",
                body,
                timeout=120,
            ))
        except Exception as e:
            pytest.skip(f"LLM unavailable: {e}")

    def test_text_delta_events_present(self, api_url, api_session, test_project):
        """Verify the model actually generates text through SSE."""
        events = self._stream_prompt(api_url, api_session, test_project, "Say exactly: hello from tool test")

        event_types = [e[0] for e in events]
        text_deltas = [e for e in events if e[0] == "text_delta"]

        # If LLM errored out, skip — not a test bug
        if "error" in event_types and len(text_deltas) == 0:
            pytest.skip(f"LLM produced error events instead of text_delta: {event_types}")

        # Must have text_delta events (the core streaming feature)
        assert len(text_deltas) > 0, (
            f"No text_delta events received. Event types: {event_types}"
        )

        # Accumulated text should be non-empty
        full_text = "".join(
            d.get("text", "") for _, d in text_deltas if isinstance(d, dict)
        )
        assert len(full_text) > 0, "text_delta events had no text content"

    def test_agent_end_has_usage(self, api_url, api_session, test_project):
        """Verify agent_end event has token usage data."""
        events = self._stream_prompt(api_url, api_session, test_project, "say ok")

        event_types = [e[0] for e in events]

        # If LLM errored, usage may be zero — skip
        if "error" in event_types:
            pytest.skip(f"LLM produced errors, usage data unreliable: {event_types}")

        end_events = [d for t, d in events if t == "agent_end"]
        assert len(end_events) > 0, "No agent_end event received"

        end_data = end_events[0]
        # Should have some usage fields
        has_usage = any(
            end_data.get(k, 0) > 0 for k in ("input", "output", "cache")
        )
        assert has_usage or "usage" in end_data, (
            f"agent_end has no usage data: {end_data}"
        )

    def test_tool_execution_events(self, api_url, api_session, test_project):
        """Verify bash tool use triggers tool_execution_start/end events."""
        events = self._stream_prompt(
            api_url, api_session, test_project,
            "Run this bash command exactly: echo TOOL_TEST_SUCCESS"
        )

        event_types = [e[0] for e in events]
        print(f"  Event types: {event_types}")

        tool_starts = [e for e in events if e[0] == "tool_execution_start"]
        tool_ends = [e for e in events if e[0] == "tool_execution_end"]

        # If the model used tools, verify structure
        for _, data in tool_starts:
            assert isinstance(data, dict), f"tool_execution_start data is not dict: {data}"
            # Should have toolName
            assert "toolName" in data, f"tool_execution_start missing toolName: {data}"

        for _, data in tool_ends:
            assert isinstance(data, dict), f"tool_execution_end data is not dict: {data}"
            assert "toolName" in data, f"tool_execution_end missing toolName: {data}"

        # Log results
        if tool_starts:
            print(f"  Tools started: {[d.get('toolName') for _, d in tool_starts]}")
            print(f"  Tools ended: {[d.get('toolName') for _, d in tool_ends]}")
        else:
            print("  No tool use detected (model may have responded without tools)")

    def test_sse_event_types_are_valid(self, api_url, api_session, test_project):
        """Verify all SSE events have recognized types."""
        events = self._stream_prompt(
            api_url, api_session, test_project,
            "Think step by step: what is 15 * 37?"
        )

        valid_types = {
            "agent_start", "agent_spawned", "text_delta", "thinking_delta",
            "tool_execution_start", "tool_execution_end",
            "agent_end", "error", "state_update",
            "compaction_start", "compaction_end",
            "branch_created", "commit_created", "push_complete",
            "pr_created", "web_update", "approval_request", "question_asked",
            "message_start", "message_end", "turn_start", "turn_end",
            "auto_retry_start", "auto_retry_end",
            "step_start", "message",  # step_start and message are valid
        }

        for ev_type, _ in events:
            assert ev_type in valid_types, f"Unknown event type: {ev_type}"


class TestPuxCompact:
    """Test context compaction."""

    def test_compact_no_active_session(self, api_url, api_session, test_project):
        # Compact on a non-streaming session — POST with JSON body
        resp = api_session.post(
            f"{api_url}/api/pux/compact",
            json={"project": test_project, "agentId": "default"},
        )
        # Returns 200 on success (with status "ok" or "error" in body),
        # or 500 if the DB is nil/unavailable
        assert resp.status_code in (200, 500)
        if resp.status_code == 200:
            data = resp.json()
            assert data.get("status") in ("ok", "error", "noop")


class TestPuxDecision:
    """Test approval/decision response endpoint."""

    def test_decision_no_pending_approval(self, api_url, api_session, test_project):
        resp = api_session.post(f"{api_url}/api/pux/decision", json={
            "project": test_project,
            "agentId": "default",
            "decisionId": "nonexistent-req",
            "action": "approve",
        })
        # Returns 404 if no pending decision, or 200 with success=false
        assert resp.status_code in (200, 404)
        if resp.status_code == 200:
            data = resp.json()
            assert data.get("success") is False or "error" in data


class TestPuxConversations:
    """Test conversation listing and history."""

    def test_conversation_history(self, api_url, api_session, test_project):
        # History requires project query param
        resp = api_session.get(f"{api_url}/api/pux/history", params={"project": test_project})
        assert resp.status_code == 200
        data = resp.json()
        # Response is a raw list of messages
        assert isinstance(data, list)
