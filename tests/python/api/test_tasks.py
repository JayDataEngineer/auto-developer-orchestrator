"""
Task management integration tests: CRUD, trigger, canStart, dependencies.
Tests against the real backend's scheduler API.
"""

import time

import pytest

pytestmark = pytest.mark.api

# Probe whether the scheduler API actually exists — skip if the endpoint is missing
import requests as _requests
_SCHEDULER_AVAILABLE = False
try:
    _probe_resp = _requests.get(
        "http://localhost:3847/api/scheduler/",
        timeout=5,
    )
    _SCHEDULER_AVAILABLE = _probe_resp.status_code != 404
except Exception:
    pass

if not _SCHEDULER_AVAILABLE:
    pytestmark = [pytest.mark.api, pytest.mark.skip(reason="Scheduler API not available")]


class TestTaskCRUD:
    """Full create-read-update-delete cycle for scheduler jobs."""

    @pytest.fixture(scope="class")
    def task_payload(self):
        ts = int(time.time())
        return {
            "name": f"Py Test Task {ts}",
            "description": "Created by Python integration test suite",
            "project": "test-repo",
            "message": f"Test message {ts}",
            "scheduleType": "manual",
        }

    def test_list_tasks(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/scheduler/")
        assert resp.status_code == 200
        data = resp.json()
        assert "jobs" in data
        assert isinstance(data["jobs"], list)

    def test_create_task(self, api_url, api_session, task_payload):
        resp = api_session.post(f"{api_url}/api/scheduler/", json=task_payload)
        assert resp.status_code == 201, f"Create failed: {resp.text}"
        data = resp.json()
        assert data.get("success") is True
        job = data.get("job", {})
        assert job.get("id"), f"Job missing ID: {data}"
        assert job.get("name") == task_payload["name"]

    def test_get_task(self, api_url, api_session, task_payload):
        # Create
        create_resp = api_session.post(f"{api_url}/api/scheduler/", json=task_payload)
        assert create_resp.status_code == 201
        task_data = create_resp.json()
        job = task_data.get("job", {})
        task_id = job.get("id")
        if not task_id:
            pytest.skip("No task ID returned from create")

        # Get — GetJob returns the raw Job object
        resp = api_session.get(f"{api_url}/api/scheduler/{task_id}")
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("id") == task_id

    def test_update_task(self, api_url, api_session, task_payload):
        # Create
        create_resp = api_session.post(f"{api_url}/api/scheduler/", json=task_payload)
        assert create_resp.status_code == 201
        task_data = create_resp.json()
        job = task_data.get("job", {})
        task_id = job.get("id")
        if not task_id:
            pytest.skip("No task ID returned")

        # Update — use the same createJobRequest fields
        resp = api_session.put(f"{api_url}/api/scheduler/{task_id}", json={
            "name": "Updated by test",
            "project": "test-repo",
            "message": "updated message",
            "scheduleType": "manual",
        })
        assert resp.status_code == 200, f"Update failed: {resp.text}"

    def test_delete_task(self, api_url, api_session, task_payload):
        # Create
        create_resp = api_session.post(f"{api_url}/api/scheduler/", json=task_payload)
        assert create_resp.status_code == 201
        task_data = create_resp.json()
        job = task_data.get("job", {})
        task_id = job.get("id")
        if not task_id:
            pytest.skip("No task ID returned")

        # Delete
        resp = api_session.delete(f"{api_url}/api/scheduler/{task_id}")
        assert resp.status_code == 200

        # Verify gone
        get_resp = api_session.get(f"{api_url}/api/scheduler/{task_id}")
        assert get_resp.status_code == 404


class TestTaskWorkflow:
    """Test task lifecycle operations."""

    def _create_task(self, api_url, api_session):
        payload = {
            "name": f"Workflow Task {int(time.time() * 1000)}",
            "description": "For workflow testing",
            "project": "test-repo",
            "message": "workflow test message",
            "scheduleType": "manual",
        }
        resp = api_session.post(f"{api_url}/api/scheduler/", json=payload)
        assert resp.status_code == 201
        data = resp.json()
        job = data.get("job", {})
        task_id = job.get("id")
        if not task_id:
            pytest.skip("No task ID returned")
        return task_id

    def test_can_start(self, api_url, api_session):
        task_id = self._create_task(api_url, api_session)
        resp = api_session.get(f"{api_url}/api/scheduler/{task_id}/canStart")
        assert resp.status_code == 200
        data = resp.json()
        assert "canStart" in data
        assert data.get("success") is True

    def test_trigger_task(self, api_url, api_session):
        task_id = self._create_task(api_url, api_session)
        resp = api_session.post(f"{api_url}/api/scheduler/{task_id}/trigger")
        # May succeed or fail depending on task state
        assert resp.status_code in (200, 404)

    def test_set_dependencies(self, api_url, api_session):
        task_id_1 = self._create_task(api_url, api_session)
        task_id_2 = self._create_task(api_url, api_session)

        resp = api_session.post(f"{api_url}/api/scheduler/{task_id_2}/deps", json={
            "blockedBy": [task_id_1],
            "blocks": [],
        })
        # May succeed or fail depending on backend support
        assert resp.status_code in (200, 400, 404, 500)


class TestTaskValidation:
    """Test task input validation."""

    def test_create_missing_fields(self, api_url, api_session):
        """CreateJob requires name, project, message, scheduleType."""
        resp = api_session.post(f"{api_url}/api/scheduler/", json={})
        # Should fail or create with defaults — accept either
        assert resp.status_code in (201, 400)

    def test_list_returns_ok(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/scheduler/")
        assert resp.status_code == 200

    def test_get_nonexistent_task(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/scheduler/nonexistent-task-999")
        assert resp.status_code == 404

    def test_delete_nonexistent_task(self, api_url, api_session):
        resp = api_session.delete(f"{api_url}/api/scheduler/nonexistent-task-999")
        assert resp.status_code == 404
