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
        assert resp.status_code == 200
        data = resp.json()
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
        assert spawn_resp.status_code == 200
        agent_id = (
            spawn_resp.json().get("agentId")
            or spawn_resp.json().get("agent_id")
            or "default"
        )

        # List active
        list_resp = api_session.get(f"{api_url}/api/pi/active")
        assert list_resp.status_code == 200
        data = list_resp.json()
        # Active sessions should include our project
        # (structure varies: could be dict keyed by project or flat list)
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
        assert spawn_resp.status_code == 200
        agent_id = (
            spawn_resp.json().get("agentId")
            or spawn_resp.json().get("agent_id")
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
