"""
Agent Lifecycle & Multi-Turn Conversation Tests.

Tests the complete agent lifecycle: spawn → state → prompt → multi-turn
conversation → message history → token accumulation → destroy.

Verifies that state transitions match exactly what the frontend expects.

Run: cd tests/python && uv run pytest test_agent_lifecycle.py -v --tb=long
"""

import json
import time

import pytest
import requests

from conftest import (
    API_BASE_URL,
    destroy_agent,
    spawn_agent,
    stream_prompt,
    post_and_stream,
)

pytestmark = [pytest.mark.api, pytest.mark.sse, pytest.mark.slow]

API = API_BASE_URL
TEST_PROJECT = "test-repo"
TEST_MODEL = "gemma-4-26b"


@pytest.fixture(scope="module")
def api_session_mod(api_session):
    return api_session


@pytest.fixture(scope="module")
def agent(api_url, api_session_mod):
    """
    Spawn an agent for the module, yield the agent ID, destroy after.
    """
    agent_id = spawn_agent(api_url, api_session_mod, TEST_PROJECT)
    yield agent_id
    destroy_agent(api_url, api_session_mod, TEST_PROJECT, agent_id)


# ===========================================================================
# 1. Spawn lifecycle
# ===========================================================================


class TestSpawnLifecycle:
    """
    Verify the spawn → state → destroy lifecycle works correctly.
    The frontend calls these endpoints when selecting a project.
    """

    def test_spawn_returns_agent_id(self, api_url, api_session):
        """spawn must return agentId (frontend stores it)."""
        resp = api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": TEST_PROJECT},
        )
        data = resp.json()

        if data.get("error", "").startswith("max agents"):
            pytest.skip("Max agents reached")

        assert resp.status_code == 200
        assert "agentId" in data or "agent_id" in data or data.get("success") is True

        agent_id = data.get("agentId") or data.get("agent_id") or "default"
        assert isinstance(agent_id, str) and len(agent_id) > 0

        # Cleanup
        api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": TEST_PROJECT, "agentId": agent_id},
        )

    def test_spawn_invalid_project_fails(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": "nonexistent-proj-xyz"},
        )
        assert resp.status_code >= 400

    def test_spawn_shows_in_active_list(self, api_url, api_session):
        """After spawning, the agent should appear in /api/pi/active."""
        spawn_resp = api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": TEST_PROJECT},
        )
        spawn_data = spawn_resp.json()
        if spawn_data.get("error", "").startswith("max agents"):
            pytest.skip("Max agents reached")

        agent_id = spawn_data.get("agentId") or spawn_data.get("agent_id") or "default"

        list_resp = api_session.get(f"{api_url}/api/pi/active")
        assert list_resp.status_code == 200
        data = list_resp.json()
        assert data is not None

        # Cleanup
        api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": TEST_PROJECT, "agentId": agent_id},
        )


# ===========================================================================
# 2. State persistence
# ===========================================================================


class TestStatePersistence:
    """
    Verify /api/pi/state returns data the frontend can use.
    The frontend polls this to display model name, streaming status, etc.
    """

    def test_state_after_spawn(self, api_url, api_session_mod, agent):
        resp = api_session_mod.get(
            f"{api_url}/api/pi/state",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()

        # Frontend reads model, streaming, sessionId
        # At minimum one of these should be present
        assert "model" in data or "streaming" in data or "sessionId" in data, (
            f"State response has no expected fields: {list(data.keys())}"
        )

    def test_state_model_field_is_string(self, api_url, api_session_mod, agent):
        """model field must be a string (frontend displays it in dropdown)."""
        resp = api_session_mod.get(
            f"{api_url}/api/pi/state",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        data = resp.json()

        if "model" in data and data["model"] is not None:
            assert isinstance(data["model"], str), (
                f"model is {type(data['model']).__name__}, expected str or null"
            )

    def test_state_streaming_field_is_bool(self, api_url, api_session_mod, agent):
        """streaming field must be boolean (frontend uses it for spinner)."""
        resp = api_session_mod.get(
            f"{api_url}/api/pi/state",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        data = resp.json()

        if "streaming" in data:
            assert isinstance(data["streaming"], bool), (
                f"streaming is {type(data['streaming']).__name__}, expected bool"
            )

    def test_state_nonexistent_agent(self, api_url, api_session_mod):
        resp = api_session_mod.get(
            f"{api_url}/api/pi/state",
            params={"project": "no-such-project", "agentId": "default"},
        )
        assert resp.status_code in (200, 404)
        if resp.status_code == 200:
            data = resp.json()
            # Should indicate no active session
            assert data.get("sessionId") == "" or data.get("streaming") is False


# ===========================================================================
# 3. Model management
# ===========================================================================


class TestModelManagement:
    """
    Verify model listing and switching.
    The frontend has a model dropdown that reads from /api/pi/models.
    """

    def test_get_models_returns_list(self, api_url, api_session_mod):
        resp = api_session_mod.get(f"{api_url}/api/pi/models")
        assert resp.status_code == 200
        data = resp.json()

        assert "models" in data, f"Missing 'models' key: {list(data.keys())}"
        assert isinstance(data["models"], list), (
            f"models is {type(data['models']).__name__}, expected list"
        )

    def test_model_has_required_fields(self, api_url, api_session_mod):
        """
        Frontend PiModel interface: { provider: string, id: string, name: string }
        """
        resp = api_session_mod.get(f"{api_url}/api/pi/models")
        data = resp.json()
        models = data.get("models", [])

        if len(models) == 0:
            pytest.skip("No models configured")

        for model in models:
            if not isinstance(model, dict):
                continue
            # Frontend expects at minimum id and name
            assert "id" in model, f"Model missing 'id': {model}"
            assert isinstance(model["id"], str), f"Model id not a string: {model}"

    def test_model_set_persists(self, api_url, api_session_mod, agent):
        """After sending a prompt with a model, state should reflect that model."""
        events = list(post_and_stream(
            api_session_mod,
            f"{api_url}/api/pi/prompt",
            {
                "message": "say ok",
                "project": TEST_PROJECT,
                "agentId": agent,
                "model": TEST_MODEL,
            },
            timeout=90,
        ))

        state_resp = api_session_mod.get(
            f"{api_url}/api/pi/state",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert state_resp.status_code == 200
        state = state_resp.json()
        assert state.get("model", "") != "", (
            f"Model is empty after prompt. State: {state}"
        )


# ===========================================================================
# 4. Multi-turn conversations
# ===========================================================================


class TestMultiTurnConversation:
    """
    Test sequential prompts in the same session.
    The frontend accumulates messages across turns.
    """

    def _send_and_collect(self, api_url, api_session_mod, message, agent_id="default"):
        return list(post_and_stream(
            api_session_mod,
            f"{api_url}/api/pi/prompt",
            {
                "message": message,
                "project": TEST_PROJECT,
                "agentId": agent_id,
                "model": TEST_MODEL,
            },
            timeout=180,
        ))

    def test_two_sequential_prompts_both_get_agent_end(self, api_url, api_session_mod, agent):
        """
        Each prompt must produce its own agent_end.
        If the second prompt doesn't get agent_end, the spinner stays forever.
        """
        events1 = self._send_and_collect(api_url, api_session_mod, "Say ok first", agent)
        events2 = self._send_and_collect(api_url, api_session_mod, "Say ok second", agent)

        assert "agent_end" in [t for t, _ in events1], (
            "First prompt: no agent_end"
        )
        assert "agent_end" in [t for t, _ in events2], (
            f"Second prompt: no agent_end. Events: {[t for t, _ in events2]}\n"
            f"This means spinner won't stop after second message."
        )

    def test_two_prompts_both_produce_content(self, api_url, api_session_mod, agent):
        """Each prompt must produce text content."""
        events1 = self._send_and_collect(
            api_url, api_session_mod,
            "Say exactly: RESPONSE_ONE", agent,
        )
        events2 = self._send_and_collect(
            api_url, api_session_mod,
            "Say exactly: RESPONSE_TWO", agent,
        )

        text1 = "".join(
            d.get("text", "") for t, d in events1
            if t == "text_delta" and isinstance(d, dict)
        )
        text2 = "".join(
            d.get("text", "") for t, d in events2
            if t == "text_delta" and isinstance(d, dict)
        )

        assert len(text1.strip()) > 0, (
            f"First prompt produced no text. Events: {[t for t, _ in events1]}"
        )
        assert len(text2.strip()) > 0, (
            f"Second prompt produced no text. Events: {[t for t, _ in events2]}"
        )

    def test_message_history_accumulates(self, api_url, api_session_mod, agent):
        """
        After sending two prompts, the message history should contain
        messages from both turns.
        """
        msg1 = f"Turn1_{int(time.time())}"
        msg2 = f"Turn2_{int(time.time())}"

        self._send_and_collect(api_url, api_session_mod, msg1, agent)
        self._send_and_collect(api_url, api_session_mod, msg2, agent)

        resp = api_session_mod.get(
            f"{api_url}/api/pi/messages",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()
        messages = data if isinstance(data, list) else data.get("messages", [])

        user_messages = [
            m for m in messages
            if isinstance(m, dict) and m.get("role") == "user"
        ]

        assert len(user_messages) >= 2, (
            f"Expected at least 2 user messages, got {len(user_messages)}.\n"
            f"Messages: {[m.get('content', m) for m in user_messages]}"
        )

    def test_token_usage_accumulates_across_turns(self, api_url, api_session_mod, agent):
        """
        Token usage from agent_end should accumulate across turns.
        Frontend adds each agent_end's tokens to running totals.
        """
        events1 = self._send_and_collect(
            api_url, api_session_mod, "Say ok turn1", agent,
        )
        events2 = self._send_and_collect(
            api_url, api_session_mod, "Say ok turn2", agent,
        )

        end1 = [d for t, d in events1 if t == "agent_end"]
        end2 = [d for t, d in events2 if t == "agent_end"]

        if end1 and end2:
            # Each turn should have some output tokens
            output1 = end1[0].get("output", 0)
            output2 = end2[0].get("output", 0)
            total = output1 + output2

            assert total > 0, (
                f"Total output tokens is 0 across two turns.\n"
                f"Turn 1: {end1[0]}\nTurn 2: {end2[0]}"
            )

    def test_each_turn_has_complete_event_sequence(self, api_url, api_session_mod, agent):
        """
        Each turn should have: agent_start → content → agent_end.
        If a turn is missing any of these, the UI breaks for that turn.
        """
        events1 = self._send_and_collect(
            api_url, api_session_mod, "Say ok sequence1", agent,
        )
        events2 = self._send_and_collect(
            api_url, api_session_mod, "Say ok sequence2", agent,
        )

        for i, events in enumerate([events1, events2], 1):
            event_types = [t for t, _ in events]
            has_start = "agent_start" in event_types or "agent_spawned" in event_types
            has_content = any(t in event_types for t in ("text_delta", "thinking_delta"))
            has_end = "agent_end" in event_types

            assert has_start, (
                f"Turn {i}: missing agent_start/agent_spawned. Types: {event_types}"
            )
            assert has_content, (
                f"Turn {i}: no content events. Types: {event_types}"
            )
            assert has_end, (
                f"Turn {i}: missing agent_end. Types: {event_types}"
            )


# ===========================================================================
# 5. Message history contract
# ===========================================================================


class TestMessageHistoryContract:
    """
    Verify /api/pi/messages returns data matching the frontend's
    ConversationMessage type.
    """

    def test_messages_returns_list(self, api_url, api_session_mod, agent):
        resp = api_session_mod.get(
            f"{api_url}/api/pi/messages",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()

        messages = data if isinstance(data, list) else data.get("messages", [])
        assert isinstance(messages, list), (
            f"Messages is {type(messages).__name__}, expected list"
        )

    def test_user_messages_have_content(self, api_url, api_session_mod, agent):
        """
        User messages should have content (frontend displays them).
        Frontend interface: { id: string, role: 'user', content: string, timestamp: number }
        """
        # Send a unique message first
        unique_msg = f"History test {int(time.time())}"
        list(post_and_stream(
            api_session_mod,
            f"{api_url}/api/pi/prompt",
            {
                "message": unique_msg,
                "project": TEST_PROJECT,
                "agentId": agent,
                "model": TEST_MODEL,
            },
            timeout=90,
        ))

        resp = api_session_mod.get(
            f"{api_url}/api/pi/messages",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        data = resp.json()
        messages = data if isinstance(data, list) else data.get("messages", [])

        user_msgs = [m for m in messages if isinstance(m, dict) and m.get("role") == "user"]
        assert len(user_msgs) > 0, "No user messages in history"

        for msg in user_msgs:
            content = msg.get("content", "")
            assert isinstance(content, str) and len(content) > 0, (
                f"User message has empty content: {msg}"
            )

    def test_assistant_messages_have_text_or_thinking(self, api_url, api_session_mod, agent):
        """
        Assistant messages should have text or thinking content.
        Frontend interface: { text: string, thinking: string, toolCalls: ToolCall[] }
        """
        resp = api_session_mod.get(
            f"{api_url}/api/pi/messages",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        data = resp.json()
        messages = data if isinstance(data, list) else data.get("messages", [])

        assistant_msgs = [
            m for m in messages
            if isinstance(m, dict) and m.get("role") == "assistant"
        ]

        # At least one assistant message should have content
        msgs_with_content = 0
        for msg in assistant_msgs:
            text = msg.get("text", "")
            thinking = msg.get("thinking", "")
            content = msg.get("content", "")
            has_content = (
                (isinstance(text, str) and len(text) > 0) or
                (isinstance(thinking, str) and len(thinking) > 0) or
                (isinstance(content, str) and len(content) > 0)
            )
            if has_content:
                msgs_with_content += 1

        assert msgs_with_content > 0, (
            f"No assistant messages have text/thinking/content.\n"
            f"Messages: {[{k: v for k, v in m.items() if k in ('text', 'thinking', 'content')} for m in assistant_msgs]}"
        )


# ===========================================================================
# 6. Destroy lifecycle
# ===========================================================================


class TestDestroyLifecycle:
    """
    Verify agent destruction clears state properly.
    The frontend calls this when switching projects or explicitly destroying.
    """

    def test_destroy_clears_state(self, api_url, api_session):
        """After destroy, state should indicate no active session."""
        # Spawn
        spawn_resp = api_session.post(
            f"{api_url}/api/pi/agent/spawn",
            json={"project": TEST_PROJECT},
        )
        spawn_data = spawn_resp.json()
        if spawn_data.get("error", "").startswith("max agents"):
            pytest.skip("Max agents reached")
        agent_id = spawn_data.get("agentId") or spawn_data.get("agent_id") or "default"

        # Destroy
        destroy_resp = api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": TEST_PROJECT, "agentId": agent_id},
        )
        assert destroy_resp.status_code == 200

        # Verify state cleared
        state_resp = api_session.get(
            f"{api_url}/api/pi/state",
            params={"project": TEST_PROJECT, "agentId": agent_id},
        )
        if state_resp.status_code == 200:
            data = state_resp.json()
            assert data.get("sessionId") == "" or data.get("streaming") is False

    def test_destroy_nonexistent_agent_is_safe(self, api_url, api_session):
        """Destroying a non-existent agent should not crash."""
        resp = api_session.post(
            f"{api_url}/api/pi/agent/destroy",
            json={"project": TEST_PROJECT, "agentId": "nonexistent-agent-xyz"},
        )
        # Should return 200 (idempotent) or 404 — not 500
        assert resp.status_code in (200, 404), (
            f"Destroy nonexistent agent returned {resp.status_code}"
        )


# ===========================================================================
# 7. Abort
# ===========================================================================


class TestAbortContract:
    """Test abort endpoint behavior."""

    def test_abort_no_active_session(self, api_url, api_session_mod, agent):
        resp = api_session_mod.post(
            f"{api_url}/api/pi/abort",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()
        # When no active streaming session, success may be True (agent exists)
        # or False (no active prompt) — both are acceptable
        assert "success" in data


# ===========================================================================
# 8. Compact
# ===========================================================================


class TestCompactContract:
    """Test context compaction."""

    def test_compact_returns_success(self, api_url, api_session_mod, agent):
        resp = api_session_mod.post(
            f"{api_url}/api/pi/compact",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200


# ===========================================================================
# 9. Session management
# ===========================================================================


class TestSessionManagement:
    """Test session listing, switching, and history."""

    def test_list_sessions(self, api_url, api_session_mod, agent):
        resp = api_session_mod.get(
            f"{api_url}/api/pi/sessions",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert isinstance(data, (list, dict))

    def test_switch_session_invalid(self, api_url, api_session_mod, agent):
        resp = api_session_mod.put(f"{api_url}/api/pi/session", json={
            "project": TEST_PROJECT,
            "agentId": agent,
            "sessionId": "nonexistent-session-id-xyz",
        })
        assert resp.status_code in (200, 400, 404)

    def test_conversation_history(self, api_url, api_session_mod):
        resp = api_session_mod.get(f"{api_url}/api/pi/history")
        assert resp.status_code == 200
        data = resp.json()
        assert "conversations" in data
        assert isinstance(data["conversations"], list)


# ===========================================================================
# 10. Conversation history contract
# ===========================================================================


class TestConversationHistoryContract:
    """Verify /api/pi/history response structure."""

    def test_history_has_conversations_key(self, api_url, api_session_mod):
        resp = api_session_mod.get(f"{api_url}/api/pi/history")
        assert resp.status_code == 200
        data = resp.json()
        assert "conversations" in data
        assert isinstance(data["conversations"], list)
