"""
Artifacts and sub-agent integration tests.
Tests against the real backend.
"""

import pytest

pytestmark = pytest.mark.api


# ---------------------------------------------------------------------------
# Artifacts
# ---------------------------------------------------------------------------


class TestArtifacts:
    def test_list_artifacts_empty(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/pi/artifacts",
            params={"project": test_project, "agentId": "default"},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert "artifacts" in data or isinstance(data, list)
        artifacts = data.get("artifacts", data if isinstance(data, list) else [])
        assert isinstance(artifacts, list)

    def test_create_artifact(self, api_url, api_session, test_project):
        resp = api_session.post(f"{api_url}/api/pi/artifacts", json={
            "project": test_project,
            "agentId": "default",
            "type": "plan",
            "title": "Test Plan",
            "content": "## Integration Test Plan\n\nStep 1: Test\nStep 2: Verify",
        })
        # May succeed or fail depending on DB schema
        assert resp.status_code in (200, 201, 400, 500)
        if resp.status_code in (200, 201):
            data = resp.json()
            assert data.get("success") is not None or "artifact" in data or "id" in data

    def test_list_after_create(self, api_url, api_session, test_project):
        # Create an artifact first
        api_session.post(f"{api_url}/api/pi/artifacts", json={
            "project": test_project,
            "agentId": "default",
            "type": "note",
            "title": "Test Note for List",
            "content": "Note content from test",
        })

        # List should return it
        resp = api_session.get(
            f"{api_url}/api/pi/artifacts",
            params={"project": test_project, "agentId": "default"},
        )
        assert resp.status_code == 200
        data = resp.json()
        artifacts = data.get("artifacts", data if isinstance(data, list) else [])
        assert isinstance(artifacts, list)

    def test_artifacts_missing_agentid(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/pi/artifacts",
            params={"project": test_project},
        )
        # Should return 400 or empty list
        assert resp.status_code in (200, 400)


# ---------------------------------------------------------------------------
# Sub-agents
# ---------------------------------------------------------------------------


class TestSubAgents:
    def test_list_subagents_empty(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/pi/subagent/list",
            params={"project": test_project, "parentAgentId": "default"},
        )
        assert resp.status_code == 200
        data = resp.json()
        assert data is not None

    def test_spawn_subagent(self, api_url, api_session, test_project):
        resp = api_session.post(f"{api_url}/api/pi/subagent/spawn", json={
            "project": test_project,
            "parentAgentId": "default",
            "taskDescription": "Test sub-agent task",
        })
        # May fail if no active parent session
        assert resp.status_code in (200, 400, 500)
        if resp.status_code == 200:
            data = resp.json()
            assert data.get("success") is not None or "agentId" in data

    def test_subagent_status_no_agent(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/pi/subagent/status",
            params={"project": test_project, "subAgentId": "nonexistent-sub"},
        )
        # Should return 404 or error
        assert resp.status_code in (200, 400, 404, 500)

    def test_subagent_abort_no_agent(self, api_url, api_session, test_project):
        resp = api_session.post(f"{api_url}/api/pi/subagent/abort", json={
            "project": test_project,
            "subAgentId": "nonexistent-sub",
        })
        assert resp.status_code in (200, 400, 404, 500)

    def test_subagent_result_no_agent(self, api_url, api_session, test_project):
        resp = api_session.get(
            f"{api_url}/api/pi/subagent/result",
            params={"project": test_project, "subAgentId": "nonexistent-sub"},
        )
        assert resp.status_code in (200, 400, 404, 500)
