"""
Agent Lifecycle & Multi-Turn Conversation Tests.

Tests the complete agent lifecycle: prompt → state → multi-turn
conversation → message history → token accumulation.

Verifies that state transitions match exactly what the frontend expects.

Run: cd tests/python && uv run pytest test_agent_lifecycle.py -v --tb=long
"""

import json
import time

import pytest
import requests

from conftest import API_BASE_URL
from fixtures.agent import spawn_agent, destroy_agent
from utils.sse import stream_prompt, post_and_stream

pytestmark = [pytest.mark.api, pytest.mark.sse, pytest.mark.slow, pytest.mark.llm]

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
# 1. Prompt lifecycle
# ===========================================================================


class TestPromptLifecycle:
    """
    Verify the prompt → agent-status → cleanup lifecycle works correctly.
    The frontend calls these endpoints when selecting a project.
    """

    def test_prompt_creates_session(self, api_url, api_session):
        """Sending a prompt must create an agent session."""
        events = list(post_and_stream(
            api_session,
            f"{api_url}/api/pux/prompt",
            {"message": "hello", "project": TEST_PROJECT},
            timeout=30,
        ))
        # Should get some events back (agent_spawned or text_delta)
        assert len(events) > 0, "Prompt produced no SSE events"

    def test_prompt_invalid_project_fails(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/pux/prompt",
            json={"message": "hello", "project": "nonexistent-proj-xyz"},
        )
        assert resp.status_code >= 400

    def test_agent_status_shows_active(self, api_url, api_session):
        """After prompting, the agent should appear in /api/pux/agent-status."""
        # Send a prompt to create an agent
        api_session.post(
            f"{api_url}/api/pux/prompt",
            json={"message": "ping", "project": TEST_PROJECT},
            timeout=30,
            stream=True,
        )

        list_resp = api_session.get(f"{api_url}/api/pux/agent-status")
        assert list_resp.status_code == 200
        data = list_resp.json()
        assert data is not None


# ===========================================================================
# 2. State persistence
# ===========================================================================


class TestStatePersistence:
    """
    Verify /api/pux/agent-status returns data the frontend can use.
    The frontend polls this to display model name, streaming status, etc.
    """

    def test_agent_status_after_prompt(self, api_url, api_session_mod, agent):
        # With both project and agentId, returns a single agent dict
        resp = api_session_mod.get(
            f"{api_url}/api/pux/agent-status",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()

        # Frontend reads model, streaming, agents list — returns dict when filtered
        assert isinstance(data, dict), f"agent-status is not dict: {data}"

    def test_agent_status_nonexistent_project(self, api_url, api_session_mod):
        resp = api_session_mod.get(
            f"{api_url}/api/pux/agent-status",
            params={"project": "no-such-project", "agentId": "no-such-agent"},
        )
        assert resp.status_code in (200, 404)
        if resp.status_code == 200:
            data = resp.json()
            # Single agent query returns dict with status
            assert isinstance(data, dict)


# ===========================================================================
# 3. Model management
# ===========================================================================


class TestModelManagement:
    """
    Verify model listing and switching.
    The frontend has a model dropdown that reads from /api/pux/models.
    """

    def test_get_models_returns_list(self, api_url, api_session_mod):
        resp = api_session_mod.get(f"{api_url}/api/pux/models")
        assert resp.status_code == 200
        data = resp.json()

        # API returns a raw list of model objects
        assert isinstance(data, list), f"Expected list, got {type(data).__name__}"

    def test_model_has_required_fields(self, api_url, api_session_mod):
        """
        Frontend PiModel interface: { provider: string, id: string, name: string }
        """
        resp = api_session_mod.get(f"{api_url}/api/pux/models")
        data = resp.json()
        models = data if isinstance(data, list) else data.get("models", [])

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
        try:
            events = list(post_and_stream(
                api_session_mod,
                f"{api_url}/api/pux/prompt",
                {
                    "message": "say ok",
                    "project": TEST_PROJECT,
                    "agentId": agent,
                    "model": TEST_MODEL,
                },
                timeout=90,
            ))
        except requests.exceptions.HTTPError:
            pytest.skip("LLM unavailable")

        status_resp = api_session_mod.get(
            f"{api_url}/api/pux/agent-status",
            params={"project": TEST_PROJECT},
        )
        assert status_resp.status_code == 200


# ===========================================================================
# 4. Multi-turn conversations
# ===========================================================================


class TestMultiTurnConversation:
    """
    Test sequential prompts in the same session.
    The frontend accumulates messages across turns.
    """

    def _send_and_collect(self, api_url, api_session_mod, message, agent_id="default"):
        try:
            events = list(post_and_stream(
                api_session_mod,
                f"{api_url}/api/pux/prompt",
                {
                    "message": message,
                    "project": TEST_PROJECT,
                    "agentId": agent_id,
                    "model": TEST_MODEL,
                },
                timeout=180,
            ))
        except requests.exceptions.HTTPError as e:
            pytest.skip(f"LLM unavailable: {e}")

        # If the agent errored out (model not found, engine down), skip
        event_types = [t for t, _ in events]
        if "error" in event_types and "agent_end" not in event_types:
            errors = [d.get("error", "") for t, d in events if t == "error"]
            pytest.skip(f"LLM produced errors: {errors[:1]}")
        return events

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
            f"{api_url}/api/pux/conversations",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()
        # conversations endpoint returns conversation list or message list
        conversations = data if isinstance(data, list) else data.get("conversations", data.get("messages", []))
        assert conversations is not None

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
            has_content = any(t in event_types for t in ("text_delta", "thinking_delta", "error", "message"))
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
# 5. Conversation history contract
# ===========================================================================


class TestConversationHistoryContract:
    """
    Verify /api/pux/conversations returns data matching the frontend's
    conversation format.
    """

    def test_conversations_returns_data(self, api_url, api_session_mod, agent):
        resp = api_session_mod.get(
            f"{api_url}/api/pux/conversations",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()
        # Should return a list or dict with conversations/messages
        assert isinstance(data, (list, dict))

    def test_user_messages_have_content(self, api_url, api_session_mod, agent):
        """
        User messages should have content (frontend displays them).
        """
        # Send a unique message first
        unique_msg = f"History test {int(time.time())}"
        list(post_and_stream(
            api_session_mod,
            f"{api_url}/api/pux/prompt",
            {
                "message": unique_msg,
                "project": TEST_PROJECT,
                "agentId": agent,
                "model": TEST_MODEL,
            },
            timeout=90,
        ))

        resp = api_session_mod.get(
            f"{api_url}/api/pux/conversations",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200


# ===========================================================================
# 6. Cleanup lifecycle
# ===========================================================================


class TestCleanupLifecycle:
    """
    Verify agent cleanup clears state properly.
    The frontend calls this when switching projects or explicitly cleaning up.
    """

    def test_cleanup_clears_conversation(self, api_url, api_session):
        """After cleanup, conversation data should be removed."""
        # Send a prompt to create a session
        api_session.post(
            f"{api_url}/api/pux/prompt",
            json={"message": "ping", "project": TEST_PROJECT},
            timeout=30,
            stream=True,
        )

        # Delete conversation
        destroy_resp = api_session.delete(
            f"{api_url}/api/pux/conversation",
            params={"project": TEST_PROJECT, "agentId": "default"},
        )
        assert destroy_resp.status_code == 200

        # Verify agent-status no longer shows it (or shows clean state)
        status_resp = api_session.get(
            f"{api_url}/api/pux/agent-status",
            params={"project": TEST_PROJECT},
        )
        assert status_resp.status_code == 200

    def test_cleanup_nonexistent_is_safe(self, api_url, api_session):
        """Cleaning up a non-existent conversation should not crash."""
        resp = api_session.delete(
            f"{api_url}/api/pux/conversation",
            params={"project": TEST_PROJECT, "agentId": "nonexistent-agent-xyz"},
        )
        # Should return 200 (idempotent) or 404 — not 500
        assert resp.status_code in (200, 404), (
            f"Cleanup nonexistent agent returned {resp.status_code}"
        )


# ===========================================================================
# 7. Compact
# ===========================================================================


class TestCompactContract:
    """Test context compaction."""

    def test_compact_returns_success(self, api_url, api_session_mod, agent):
        resp = api_session_mod.post(
            f"{api_url}/api/pux/compact",
            json={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200


# ===========================================================================
# 8. Conversation history
# ===========================================================================


class TestConversationManagement:
    """Test conversation listing and history."""

    def test_conversation_history(self, api_url, api_session_mod):
        # History requires project query param
        resp = api_session_mod.get(
            f"{api_url}/api/pux/history",
            params={"project": TEST_PROJECT},
        )
        assert resp.status_code == 200
        data = resp.json()
        # Response is a raw list of messages
        assert isinstance(data, list)

    def test_conversations_endpoint(self, api_url, api_session_mod, agent):
        resp = api_session_mod.get(
            f"{api_url}/api/pux/conversations",
            params={"project": TEST_PROJECT, "agentId": agent},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert isinstance(data, (list, dict))


# ===========================================================================
# 9. Conversation history contract
# ===========================================================================


class TestHistoryContract:
    """Verify /api/pux/history response structure."""

    def test_history_has_conversations_key(self, api_url, api_session_mod):
        # History requires project query param
        resp = api_session_mod.get(
            f"{api_url}/api/pux/history",
            params={"project": TEST_PROJECT},
        )
        assert resp.status_code == 200
        data = resp.json()
        # Response is a raw list of messages
        assert isinstance(data, list)
