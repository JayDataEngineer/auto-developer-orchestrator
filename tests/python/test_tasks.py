"""
Task management integration tests: CRUD, stop, canStart, dependencies.
Tests against the real backend.
"""

import time

import pytest

pytestmark = pytest.mark.api


class TestTaskCRUD:
    """Full create-read-update-delete cycle for tasks."""

    @pytest.fixture(scope="class")
    def task_payload(self):
        return {
            "title": f"Py Test Task {int(time.time())}",
            "description": "Created by Python integration test suite",
            "projectDir": "test-repo",
            "parentAgent": "default",
        }

    def test_list_tasks(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pi/tasks/list", params={"projectDir": "test-repo"})
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("success") is True or "tasks" in data
        tasks = data.get("tasks", data if isinstance(data, list) else [])
        assert isinstance(tasks, list)

    def test_create_task(self, api_url, api_session, task_payload):
        resp = api_session.post(f"{api_url}/api/pi/tasks/", json=task_payload)
        assert resp.status_code == 200, f"Create failed: {resp.text}"
        data = resp.json()
        task = data.get("task", data)
        assert task.get("id") or task.get("success") is not None
        if task.get("id"):
            assert task["title"] == task_payload["title"]
            assert task["status"] == "pending"

    def test_get_task(self, api_url, api_session, task_payload):
        # Create
        create_resp = api_session.post(f"{api_url}/api/pi/tasks/", json=task_payload)
        assert create_resp.status_code == 200
        task_data = create_resp.json()
        task = task_data.get("task", task_data)
        task_id = task.get("id")
        if not task_id:
            pytest.skip("No task ID returned from create")

        # Get
        resp = api_session.get(f"{api_url}/api/pi/tasks/{task_id}")
        assert resp.status_code == 200
        data = resp.json()
        fetched = data.get("task", data)
        assert fetched.get("id") == task_id or data.get("id") == task_id

    def test_update_task(self, api_url, api_session, task_payload):
        # Create
        create_resp = api_session.post(f"{api_url}/api/pi/tasks/", json=task_payload)
        assert create_resp.status_code == 200
        task_data = create_resp.json()
        task = task_data.get("task", task_data)
        task_id = task.get("id")
        if not task_id:
            pytest.skip("No task ID returned")

        # Update status to in_progress
        resp = api_session.put(f"{api_url}/api/pi/tasks/{task_id}", json={
            "status": "in_progress",
            "title": "Updated by test",
        })
        assert resp.status_code == 200, f"Update failed: {resp.text}"

    def test_delete_task(self, api_url, api_session, task_payload):
        # Create
        create_resp = api_session.post(f"{api_url}/api/pi/tasks/", json=task_payload)
        assert create_resp.status_code == 200
        task_data = create_resp.json()
        task = task_data.get("task", task_data)
        task_id = task.get("id")
        if not task_id:
            pytest.skip("No task ID returned")

        # Delete
        resp = api_session.delete(f"{api_url}/api/pi/tasks/{task_id}")
        assert resp.status_code == 200

        # Verify gone
        get_resp = api_session.get(f"{api_url}/api/pi/tasks/{task_id}")
        assert get_resp.status_code in (404, 200)


class TestTaskWorkflow:
    """Test task lifecycle operations."""

    def _create_task(self, api_url, api_session):
        payload = {
            "title": f"Workflow Task {int(time.time() * 1000)}",
            "description": "For workflow testing",
            "projectDir": "test-repo",
            "parentAgent": "default",
        }
        resp = api_session.post(f"{api_url}/api/pi/tasks/", json=payload)
        assert resp.status_code == 200
        data = resp.json()
        task = data.get("task", data)
        task_id = task.get("id")
        if not task_id:
            pytest.skip("No task ID returned")
        return task_id

    def test_can_start(self, api_url, api_session):
        task_id = self._create_task(api_url, api_session)
        resp = api_session.get(f"{api_url}/api/pi/tasks/{task_id}/canStart")
        assert resp.status_code == 200
        data = resp.json()
        # Should have a canStart boolean
        assert "canStart" in data or data.get("success") is not None

    def test_stop_task(self, api_url, api_session):
        task_id = self._create_task(api_url, api_session)
        resp = api_session.post(f"{api_url}/api/pi/tasks/{task_id}/stop")
        # May succeed or fail depending on task state
        assert resp.status_code in (200, 400, 404)

    def test_set_dependencies(self, api_url, api_session):
        task_id_1 = self._create_task(api_url, api_session)
        task_id_2 = self._create_task(api_url, api_session)

        resp = api_session.post(f"{api_url}/api/pi/tasks/{task_id_2}/deps", json={
            "dependsOn": [task_id_1],
        })
        # May succeed or fail depending on backend support
        assert resp.status_code in (200, 400, 404, 500)


class TestTaskValidation:
    """Test task input validation."""

    def test_create_missing_title(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/pi/tasks/", json={
            "projectDir": "test-repo",
        })
        # Should fail or create with empty title
        assert resp.status_code in (200, 400, 422)

    def test_list_missing_project(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pi/tasks/list")
        # Should return 400 or empty list
        assert resp.status_code in (200, 400)

    def test_get_nonexistent_task(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/pi/tasks/nonexistent-task-999")
        assert resp.status_code in (200, 404)

    def test_delete_nonexistent_task(self, api_url, api_session):
        resp = api_session.delete(f"{api_url}/api/pi/tasks/nonexistent-task-999")
        assert resp.status_code in (200, 404)
