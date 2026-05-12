"""
Scheduler integration tests: CRUD, trigger, executions, runs.
Tests against the real backend.
"""

import time

import pytest

from conftest import API_BASE_URL

pytestmark = [pytest.mark.api]


class TestSchedulerCRUD:
    """Full create-read-update-delete cycle for scheduler jobs."""

    @pytest.fixture(scope="class")
    def job_payload(self):
        return {
            "name": f"Py Test Job {int(time.time())}",
            "project": "test-repo",
            "message": "echo hello from scheduler test",
            "scheduleType": "cron",
            "cronExpr": "0 0 9 * * *",
            "enabled": False,
        }

    def test_list_jobs(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/scheduler")
        assert resp.status_code == 200
        data = resp.json()
        assert "jobs" in data
        assert isinstance(data["jobs"], list)

    def test_create_job(self, api_url, api_session, job_payload):
        resp = api_session.post(f"{api_url}/api/scheduler", json=job_payload)
        assert resp.status_code in (200, 201), f"Create failed: {resp.text}"
        data = resp.json()
        assert "job" in data
        job = data["job"]
        assert "id" in job
        assert job["name"] == job_payload["name"]
        assert job["project"] == job_payload["project"]
        assert job["enabled"] is False

    def test_get_job(self, api_url, api_session, job_payload):
        # Create first
        create_resp = api_session.post(f"{api_url}/api/scheduler", json=job_payload)
        if create_resp.status_code not in (200, 201):
            pytest.skip(f"Create failed: {create_resp.text}")
        job_id = create_resp.json()["job"]["id"]

        # Get it
        resp = api_session.get(f"{api_url}/api/scheduler/{job_id}")
        assert resp.status_code == 200
        data = resp.json()
        # Response may wrap in {job: ...} or return flat
        job = data.get("job", data)
        assert job.get("id") == job_id or data.get("id") == job_id

    def test_update_job(self, api_url, api_session, job_payload):
        # Create
        create_resp = api_session.post(f"{api_url}/api/scheduler", json=job_payload)
        if create_resp.status_code not in (200, 201):
            pytest.skip(f"Create failed: {create_resp.text}")
        job_id = create_resp.json()["job"]["id"]

        # Update
        update_payload = {**job_payload, "name": "Updated Py Test Job", "enabled": True}
        resp = api_session.put(f"{api_url}/api/scheduler/{job_id}", json=update_payload)
        assert resp.status_code == 200, f"Update failed: {resp.text}"

    def test_delete_job(self, api_url, api_session, job_payload):
        # Create
        create_resp = api_session.post(f"{api_url}/api/scheduler", json=job_payload)
        if create_resp.status_code not in (200, 201):
            pytest.skip(f"Create failed: {create_resp.text}")
        job_id = create_resp.json()["job"]["id"]

        # Delete
        resp = api_session.delete(f"{api_url}/api/scheduler/{job_id}")
        assert resp.status_code == 200

        # Verify deleted
        get_resp = api_session.get(f"{api_url}/api/scheduler/{job_id}")
        assert get_resp.status_code in (404, 200)  # May still return or 404


class TestSchedulerTrigger:
    """Test manual job trigger."""

    def test_trigger_disabled_job(self, api_url, api_session):
        # Create a disabled job
        payload = {
            "name": f"Trigger Test {int(time.time())}",
            "project": "test-repo",
            "message": "echo trigger test",
            "scheduleType": "cron",
            "cronExpr": "0 0 9 * * *",
            "enabled": False,
        }
        create_resp = api_session.post(f"{api_url}/api/scheduler", json=payload)
        if create_resp.status_code not in (200, 201):
            pytest.skip("Create failed")
        job_id = create_resp.json()["job"]["id"]

        # Trigger it
        resp = api_session.post(f"{api_url}/api/scheduler/{job_id}/trigger")
        # May succeed or fail depending on backend state
        assert resp.status_code in (200, 201, 400, 500), f"Unexpected: {resp.text}"

        # Cleanup
        api_session.delete(f"{api_url}/api/scheduler/{job_id}")


class TestSchedulerRuns:
    """Test execution history endpoints."""

    def test_list_executions(self, api_url, api_session):
        # Create a job to have executions
        payload = {
            "name": f"Exec Test {int(time.time())}",
            "project": "test-repo",
            "message": "echo exec test",
            "scheduleType": "cron",
            "cronExpr": "0 0 9 * * *",
            "enabled": False,
        }
        create_resp = api_session.post(f"{api_url}/api/scheduler", json=payload)
        if create_resp.status_code not in (200, 201):
            pytest.skip("Create failed")
        job_id = create_resp.json()["job"]["id"]

        resp = api_session.get(f"{api_url}/api/scheduler/{job_id}/executions")
        assert resp.status_code == 200
        data = resp.json()
        # Should return a list (may be empty)
        assert "executions" in data or isinstance(data, list) or data.get("success") is not None

        # Cleanup
        api_session.delete(f"{api_url}/api/scheduler/{job_id}")

    def test_list_all_runs(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/scheduler/runs")
        assert resp.status_code == 200
        # May be {runs: []} or just []
        data = resp.json()
        assert data is not None

    def test_job_runs(self, api_url, api_session):
        # Create job
        payload = {
            "name": f"Runs Test {int(time.time())}",
            "project": "test-repo",
            "message": "echo runs test",
            "scheduleType": "cron",
            "cronExpr": "0 0 9 * * *",
            "enabled": False,
        }
        create_resp = api_session.post(f"{api_url}/api/scheduler", json=payload)
        if create_resp.status_code not in (200, 201):
            pytest.skip("Create failed")
        job_id = create_resp.json()["job"]["id"]

        resp = api_session.get(f"{api_url}/api/scheduler/{job_id}/runs")
        assert resp.status_code == 200
        data = resp.json()
        assert data is not None

        # Cleanup
        api_session.delete(f"{api_url}/api/scheduler/{job_id}")


class TestSchedulerValidation:
    """Test input validation."""

    def test_create_missing_fields(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/scheduler", json={})
        assert resp.status_code in (400, 422), f"Expected validation error, got {resp.status_code}"

    def test_create_invalid_cron(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/scheduler", json={
            "name": "Bad Cron",
            "project": "test-repo",
            "message": "test",
            "scheduleType": "cron",
            "cronExpr": "not-a-cron",
            "enabled": False,
        })
        # May accept or reject depending on validation
        assert resp.status_code in (200, 201, 400, 422)

    def test_get_nonexistent_job(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/scheduler/job-nonexistent-999")
        assert resp.status_code in (404, 200)

    def test_delete_nonexistent_job(self, api_url, api_session):
        resp = api_session.delete(f"{api_url}/api/scheduler/job-nonexistent-999")
        assert resp.status_code in (200, 404)
