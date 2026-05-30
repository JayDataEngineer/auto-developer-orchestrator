"""
Jobs API integration tests: one-shot task submission, polling, SSE streaming, cleanup.
Tests against the real backend — requires `task dev` running.
"""

import json
import time

import pytest
import requests

from conftest import API_BASE_URL

pytestmark = [pytest.mark.api, pytest.mark.llm]


class TestJobsSubmitAsync:
    """Submit async jobs and poll for results."""

    def test_submit_returns_job_id(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/jobs", json={
            "task": "echo hello from jobs api test",
            "project": ".",
        })
        assert resp.status_code == 202, f"Expected 202, got {resp.status_code}: {resp.text}"
        data = resp.json()
        assert data["success"] is True
        assert data["jobId"], "Expected jobId in response"
        assert data["status"] == "running"
        assert data["pollUrl"], "Expected pollUrl in response"

        # Cleanup
        api_session.delete(f"{api_url}/api/jobs/{data['jobId']}")

    def test_submit_with_name(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/jobs", json={
            "task": "echo named job test",
            "project": ".",
            "name": "e2e-named-job",
        })
        assert resp.status_code == 202, f"Expected 202: {resp.text}"
        data = resp.json()
        assert data["success"] is True

        # Cleanup
        api_session.delete(f"{api_url}/api/jobs/{data['jobId']}")

    def test_submit_with_full_sandbox(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/jobs", json={
            "task": "echo sandbox test",
            "project": ".",
            "full_sandbox": True,
        })
        assert resp.status_code == 202, f"Expected 202: {resp.text}"
        data = resp.json()
        assert data["success"] is True

        # Cleanup
        api_session.delete(f"{api_url}/api/jobs/{data['jobId']}")

    def test_submit_missing_task_returns_400(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/jobs", json={
            "project": ".",
        })
        assert resp.status_code == 400, f"Expected 400: {resp.text}"


class TestJobsPollStatus:
    """Submit a job, then poll GET /api/jobs/{id} until it completes."""

    def test_submit_and_poll_until_done(self, api_url, api_session):
        # Submit async
        submit_resp = api_session.post(f"{api_url}/api/jobs", json={
            "task": "respond with just the word 'pong'",
            "project": ".",
        })
        assert submit_resp.status_code == 202, f"Submit failed: {submit_resp.text}"
        job_id = submit_resp.json()["jobId"]

        # Poll until complete (max 120 seconds)
        final_status = None
        for _ in range(60):
            poll_resp = api_session.get(f"{api_url}/api/jobs/{job_id}")
            assert poll_resp.status_code == 200, f"Poll failed: {poll_resp.text}"
            data = poll_resp.json()
            status = data.get("status", "")
            if status not in ("running", "idle", "disabled"):
                final_status = data
                break
            time.sleep(2)

        assert final_status is not None, f"Job {job_id} did not complete within 120s"
        # After execution, manual jobs transition to "disabled" (Enabled=false)
        # but status may show as "idle" or "disabled" depending on success
        assert final_status["jobId"] == job_id

        # Cleanup
        api_session.delete(f"{api_url}/api/jobs/{job_id}")

    def test_poll_nonexistent_job_returns_404(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/jobs/oneshot-nonexistent-999")
        assert resp.status_code == 404


class TestJobsSSEStream:
    """Submit with wait=true and verify SSE stream."""

    def test_submit_with_wait_returns_sse(self, api_url):
        """Test that wait=true returns an SSE stream with proper events."""
        resp = requests.post(
            f"{api_url}/api/jobs",
            json={
                "task": "respond with just the word 'hello'",
                "project": ".",
                "wait": True,
                "timeout_seconds": 120,
            },
            headers={"Accept": "text/event-stream"},
            stream=True,
            timeout=180,
        )
        assert resp.status_code == 200, f"Expected 200 SSE: {resp.status_code} {resp.text[:500]}"
        assert "text/event-stream" in resp.headers.get("Content-Type", ""), \
            f"Expected SSE content type, got: {resp.headers.get('Content-Type')}"

        # Read SSE events and verify structure
        events = []
        for line in resp.iter_lines(decode_unicode=True):
            if line and line.startswith("event: "):
                events.append(line[7:])

        # Should have at least text_delta and done events
        assert len(events) > 0, "Expected at least one SSE event"
        assert "done" in events, f"Expected 'done' event, got: {events[:20]}"


class TestJobsDelete:
    """Test job deletion."""

    def test_delete_one_shot(self, api_url, api_session):
        # Create
        submit_resp = api_session.post(f"{api_url}/api/jobs", json={
            "task": "echo delete test",
            "project": ".",
        })
        assert submit_resp.status_code == 202
        job_id = submit_resp.json()["jobId"]

        # Delete
        del_resp = api_session.delete(f"{api_url}/api/jobs/{job_id}")
        assert del_resp.status_code == 200, f"Delete failed: {del_resp.text}"

        # Verify gone
        get_resp = api_session.get(f"{api_url}/api/jobs/{job_id}")
        assert get_resp.status_code == 404

    def test_delete_nonexistent_returns_404(self, api_url, api_session):
        resp = api_session.delete(f"{api_url}/api/jobs/oneshot-nonexistent-999")
        assert resp.status_code == 404

    def test_cannot_delete_scheduler_job(self, api_url, api_session):
        """Creating a regular scheduler job and trying to delete via /api/jobs should fail."""
        # Create a regular cron job via scheduler
        sched_resp = api_session.post(f"{api_url}/api/scheduler", json={
            "name": f"Protected Job {int(time.time())}",
            "project": "test",
            "message": "echo test",
            "scheduleType": "cron",
            "cronExpr": "0 0 9 * * *",
            "enabled": False,
        })
        if sched_resp.status_code not in (200, 201):
            pytest.skip("Could not create scheduler job")
        job_id = sched_resp.json()["job"]["id"]

        # Try to delete via jobs endpoint — should be forbidden
        del_resp = api_session.delete(f"{api_url}/api/jobs/{job_id}")
        assert del_resp.status_code == 403, f"Expected 403 for non-one-shot, got {del_resp.status_code}"

        # Cleanup via scheduler
        api_session.delete(f"{api_url}/api/scheduler/{job_id}")


class TestJobsValidation:
    """Test input validation."""

    def test_invalid_json(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/jobs",
            data="{bad json",
            headers={"Content-Type": "application/json"},
        )
        assert resp.status_code == 400

    def test_empty_body(self, api_url, api_session):
        resp = api_session.post(f"{api_url}/api/jobs", json={})
        assert resp.status_code == 400
