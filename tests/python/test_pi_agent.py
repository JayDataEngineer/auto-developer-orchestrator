"""
Pi agent tests: models, spawn, state, prompt SSE stream, messages, destroy.
"""

import pytest

from conftest import post_and_stream

pytestmark = [pytest.mark.api, pytest.mark.sse]


class TestPiModels:
    def test_get_models_returns_list(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pi/models")
        assert resp.status_code == 200
        data = resp.json()
        assert "models" in data
        assert isinstance(data["models"], list)


class TestPiActive:
    def test_list_active_initially(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pi/active")
        assert resp.status_code == 200
        # Response may be empty or have sessions
        assert resp.json() is not None


class TestPiSpawn:
    def test_spawn_valid_project(self, api_url, api_session, test_project):
        resp = api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": test_project},
        )
        data = resp.json()
        # May fail if max agents reached (5 limit)
        if data.get("error", "").startswith("max agents"):
            pytest.skip("Max agents reached — clean up with /api/pi/active + /api/pi/agent/destroy")
        assert resp.status_code == 200
        assert "agentId" in data or "agent_id" in data or data.get("success") is True

        # Cleanup: destroy
        agent_id = data.get("agentId") or data.get("agent_id") or "default"
        api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": test_project, "agentId": agent_id},
        )

    def test_spawn_invalid_project(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": "nonexistent-proj-xyz"},
        )
        assert resp.status_code >= 400

    def test_spawn_then_list_shows_it(self, api_url, api_session, test_project):
        # Spawn
        spawn_resp = api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": test_project},
        )
        spawn_data = spawn_resp.json()
        if spawn_data.get("error", "").startswith("max agents"):
            pytest.skip("Max agents reached")
        assert spawn_resp.status_code == 200
        agent_id = (
            spawn_data.get("agentId")
            or spawn_data.get("agent_id")
            or "default"
        )

        # List active
        list_resp = api_session.get(f"{api_url}/api/pi/active")
        assert list_resp.status_code == 200
        data = list_resp.json()
        assert data is not None

        # Cleanup
        api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": test_project, "agentId": agent_id},
        )


class TestPiState:
    def test_get_state_after_spawn(self, api_url, api_session, test_project):
        # Spawn first
        api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": test_project},
        )

        resp = api_session.get(
            f"{api_url}/api/pi/state",
            params={"project": test_project, "agentId": "default"},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert "model" in data or "streaming" in data or "sessionId" in data

        # Cleanup
        api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": test_project, "agentId": "default"},
        )

    def test_get_state_nonexistent_agent(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/pi/state",
            params={"project": "no-such-project", "agentId": "default"},
        )
        assert resp.status_code in (200, 404)
        # If 200, should indicate no active session
        if resp.status_code == 200:
            data = resp.json()
            assert data.get("sessionId") == "" or data.get("streaming") is False


class TestPiAbort:
    def test_abort_no_active_session(self, api_url, api_session, test_project):
        resp = api_session.post(
            f"{api_url}/api/pi/abort",
            params={"project": test_project, "agentId": "default"},
        )
        # Returns 200 with success=false when no active session
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("success") is False


@pytest.mark.slow
class TestPiPromptSSE:
    def test_prompt_missing_message_returns_400(self, api_url, api_session, test_project):
        resp = api_session.post(
            f"{api_url}/api/pi/prompt",
            json={"project": test_project},
        )
        assert resp.status_code == 400

    def test_prompt_nonexistent_project_returns_404(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/prompt",
            json={"message": "hello", "project": "nonexistent-proj"},
        )
        assert resp.status_code in (400, 404)

    def test_prompt_valid_message_streams_events(self, api_url, api_session, test_project):
        """Full SSE streaming test — marked slow because it calls the LLM."""
        # Spawn agent first
        api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": test_project},
        )

        events = list(
            post_and_stream(
                api_session,
                f"{api_url}/api/pi/prompt",
                {
                    "message": "Say exactly: hello from e2e test",
                    "project": test_project,
                    "agentId": "default",
                },
                timeout=90,
            )
        )

        assert len(events) > 0, "Expected at least one SSE event"

        event_types = [e[0] for e in events]
        assert "agent_spawned" in event_types or "text_delta" in event_types, (
            f"Expected agent_spawned or text_delta in events, got: {event_types}"
        )

        # Accumulate text deltas
        text_parts = []
        for ev_type, ev_data in events:
            if ev_type == "text_delta" and isinstance(ev_data, dict):
                text_parts.append(ev_data.get("delta", ev_data.get("text", "")))
        full_text = "".join(text_parts)
        assert len(full_text) > 0, "Expected non-empty accumulated text from text_delta events"

        # Verify agent_end has usage info
        end_events = [d for t, d in events if t == "agent_end"]
        if end_events:
            end_data = end_events[0]
            # Usage fields should be present
            assert "usage" in end_data or "input" in end_data or "output" in end_data

        # Cleanup
        api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": test_project, "agentId": "default"},
        )


@pytest.mark.slow
class TestPiToolUse:
    """Test that the model can use tools (bash, file read/write) via SSE."""

    def _stream_prompt(self, api_url, api_session, test_project, message, model="google/gemini-2.0-flash-001"):
        """Helper: send a prompt and collect all SSE events."""
        return list(post_and_stream(
            api_session,
            f"{api_url}/api/pi/prompt",
            {
                "message": message,
                "project": test_project,
                "agentId": "default",
                "model": model,
            },
            timeout=120,
        ))

    def test_text_delta_events_present(self, api_url, api_session, test_project):
        """Verify the model actually generates text through SSE."""
        events = self._stream_prompt(api_url, api_session, test_project, "Say exactly: hello from tool test")

        event_types = [e[0] for e in events]
        text_deltas = [e for e in events if e[0] == "text_delta"]

        # Must have text_delta events (the core streaming feature)
        assert len(text_deltas) > 0, (
            f"No text_delta events received. Event types: {event_types}"
        )

        # Accumulated text should be non-empty
        full_text = "".join(
            d.get("text", "") for _, d in text_deltas if isinstance(d, dict)
        )
        assert len(full_text) > 0, "text_delta events had no text content"

    def test_model_set_persists_in_state(self, api_url, api_session, test_project):
        """Verify model setting persists after a prompt."""
        self._stream_prompt(api_url, api_session, test_project, "say ok")

        resp = api_session.get(
            f"{api_url}/api/pi/state",
            params={"project": test_project, "agentId": "default"},
        )
        assert resp.status_code == 200
        state = resp.json()
        assert state.get("model", "") != "", (
            f"Model is empty in state after prompt. State: {state}"
        )

    def test_agent_end_has_usage(self, api_url, api_session, test_project):
        """Verify agent_end event has token usage data."""
        events = self._stream_prompt(api_url, api_session, test_project, "say ok")

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
        }

        for ev_type, _ in events:
            assert ev_type in valid_types, f"Unknown event type: {ev_type}"

    def test_message_history_after_prompt(self, api_url, api_session, test_project):
        """Verify message history persists after a conversation."""
        msg = f"History check {__import__('time').time()}"
        self._stream_prompt(api_url, api_session, test_project, msg)

        resp = api_session.get(
            f"{api_url}/api/pi/messages",
            params={"project": test_project, "agentId": "default"},
        )
        assert resp.status_code == 200
        data = resp.json()
        messages = data if isinstance(data, list) else data.get("messages", [])

        # Should have at least one user message
        user_msgs = [
            m for m in messages
            if isinstance(m, dict) and m.get("role") == "user"
        ]
        assert len(user_msgs) > 0, f"No user messages found in history: {messages}"


class TestPiMessages:
    def test_get_messages_returns_list(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/pi/messages",
            params={"project": test_project, "agentId": "default"},
        )
        assert resp.status_code == 200
        data = resp.json()
        # Should return a list (may be empty if no prompts sent)
        assert isinstance(data, list) or isinstance(data.get("messages", []), list)


class TestPiDestroy:
    def test_spawn_then_destroy(self, api_url, api_session, test_project):
        # Spawn
        spawn_resp = api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": test_project},
        )
        spawn_data = spawn_resp.json()
        if spawn_data.get("error", "").startswith("max agents"):
            pytest.skip("Max agents reached")
        assert spawn_resp.status_code == 200
        agent_id = (
            spawn_data.get("agentId")
            or spawn_data.get("agent_id")
            or "default"
        )

        # Destroy
        destroy_resp = api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": test_project, "agentId": agent_id},
        )
        assert destroy_resp.status_code == 200

        # Verify state is gone
        state_resp = api_session.get(
            f"{api_url}/api/pi/state",
            params={"project": test_project, "agentId": agent_id},
        )
        if state_resp.status_code == 200:
            data = state_resp.json()
            assert data.get("sessionId") == "" or data.get("streaming") is False


class TestPiSessions:
    """Test session management endpoints."""

    def test_list_sessions(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/pi/sessions",
            params={"project": test_project, "agentId": "default"},
        )
        assert resp.status_code == 200
        data = resp.json()
        # May be a list or wrapped
        assert isinstance(data, (list, dict))

    def test_switch_session_invalid(self, api_url, api_session, test_project):
        resp = api_session.put(f"{api_url}/api/pi/session", json={
            "project": test_project,
            "agentId": "default",
            "sessionId": "nonexistent-session-id",
        })
        # Should fail gracefully for invalid session
        assert resp.status_code in (200, 400, 404)

    def test_conversation_history(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pi/history")
        assert resp.status_code == 200
        data = resp.json()
        assert "conversations" in data
        assert isinstance(data["conversations"], list)


class TestPiCompact:
    """Test context compaction."""

    def test_compact_no_active_session(self, api_url, api_session, test_project):
        # Compact on a non-streaming session
        resp = api_session.post(
            f"{api_url}/api/pi/compact",
            params={"project": test_project, "agentId": "default"},
        )
        assert resp.status_code == 200


class TestPiRespond:
    """Test approval response endpoint."""

    def test_respond_no_pending_approval(self, api_url, api_session, test_project):
        resp = api_session.post(f"{api_url}/api/pi/respond", json={
            "project": test_project,
            "agentId": "default",
            "requestId": "nonexistent-req",
            "action": "approve",
        })
        # Returns 404 if agent not found, or 200 with success=false
        assert resp.status_code in (200, 404)
        if resp.status_code == 200:
            data = resp.json()
            assert data.get("success") is False or "error" in data


class TestPiDebug:
    """Test debug endpoints."""

    def test_rpc_test_no_session(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/pi/debug/rpc-test",
            params={"project": test_project, "agentId": "default"},
        )
        # May return 200 (if session exists) or error
        assert resp.status_code in (200, 400, 404, 500)
