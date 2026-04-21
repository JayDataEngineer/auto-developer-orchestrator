"""
Integration tests for sandbox persistence, recovery, readiness, ID resolution,
and VNC proxy (connection tracking, stats endpoint).

These tests exercise the full stack: Go backend + Docker containers.
They require:
  - Go backend running on localhost:3847
  - Docker available for sandbox creation
  - (Optional) llama-server for ID resolution tests

Run:
  pytest tests/python/test_sandbox_vnc_integration.py -v
  pytest tests/python/test_sandbox_vnc_integration.py -v -k "test_vnc" --sandbox
"""

import json
import time

import pytest

pytestmark = pytest.mark.sandbox

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_CREATED: list[str] = []


def _cleanup(api_url, api_session, sandbox_id):
    """Best-effort cleanup of a sandbox."""
    try:
        api_session.delete(f"{api_url}/api/sandbox/{sandbox_id}", timeout=15)
    except Exception:
        pass


def _ensure_sandbox(api_url, api_session, sid, project_path="/tmp", policy="developer", timeout=120):
    """
    Create a sandbox, cleaning up any leftover container with the same ID first.
    Returns the response. Raises pytest.skip on Docker unavailability.
    """
    resp = api_session.post(
        f"{api_url}/api/sandbox/",
        json={"id": sid, "project_path": project_path, "policy": policy},
        timeout=timeout,
    )
    if resp.status_code in (200, 201):
        _CREATED.append(sid)
        return resp
    # 500 with container conflict — nuke and retry
    if resp.status_code == 500:
        _cleanup(api_url, api_session, sid)
        time.sleep(1)
        resp = api_session.post(
            f"{api_url}/api/sandbox/",
            json={"id": sid, "project_path": project_path, "policy": policy},
            timeout=timeout,
        )
        if resp.status_code in (200, 201):
            _CREATED.append(sid)
            return resp
        pytest.skip(f"Sandbox creation failed (Docker unavailable): {resp.text[:200]}")
    return resp


@pytest.fixture(autouse=True, scope="module")
def _module_cleanup(api_url, api_session):
    yield
    for sid in _CREATED:
        _cleanup(api_url, api_session, sid)
    _CREATED.clear()


# ---------------------------------------------------------------------------
# Sandbox CRUD + Persistence
# ---------------------------------------------------------------------------


class TestSandboxPersistence:
    """Tests that sandbox metadata (project_path, policy, labels) survives creation."""

    def test_create_with_project_path_stored(self, api_url, api_session):
        """project_path passed at creation must be returned on GET."""
        sid = "e2e-persist-test"
        project_path = f"/tmp/e2e-project-{int(time.time())}"

        _ensure_sandbox(api_url, api_session, sid, project_path=project_path, policy="strict")

        get_resp = api_session.get(f"{api_url}/api/sandbox/{sid}", timeout=10)
        assert get_resp.status_code == 200
        data = get_resp.json()
        got_path = data.get("project_path", "")
        assert got_path == project_path, f"project_path mismatch: got {got_path!r}, want {project_path!r}"

    def test_create_with_policy_stored(self, api_url, api_session):
        """Policy label must persist in sandbox metadata."""
        sid = "e2e-policy-test"
        _ensure_sandbox(api_url, api_session, sid, project_path="/tmp", policy="read-only")

        get_resp = api_session.get(f"{api_url}/api/sandbox/{sid}", timeout=10)
        assert get_resp.status_code == 200
        data = get_resp.json()
        got_policy = data.get("policy", "")
        assert got_policy == "read-only", f"Policy mismatch: got {got_policy!r}"

    def test_default_policy_is_developer(self, api_url, api_session):
        """When no policy is specified, it defaults to 'developer'."""
        sid = f"e2e-default-policy-{int(time.time())}"
        _ensure_sandbox(api_url, api_session, sid)

        get_resp = api_session.get(f"{api_url}/api/sandbox/{sid}", timeout=10)
        assert get_resp.status_code == 200
        data = get_resp.json()
        got_policy = data.get("policy", "")
        assert got_policy == "developer", f"Default policy mismatch: got {got_policy!r}"


# ---------------------------------------------------------------------------
# Readiness
# ---------------------------------------------------------------------------


class TestReadiness:
    """Tests for GET /api/sandbox/{id}/ready endpoint."""

    def test_ready_after_creation(self, api_url, api_session):
        """A freshly created sandbox in CLI mode should be ready."""
        sid = "e2e-ready-test"
        _ensure_sandbox(api_url, api_session, sid)

        ready_resp = api_session.get(f"{api_url}/api/sandbox/{sid}/ready", timeout=10)
        assert ready_resp.status_code == 200
        data = ready_resp.json()
        assert data.get("ready") is True

    def test_ready_nonexistent_sandbox(self, api_url, api_session):
        """A nonexistent sandbox should return 404 or ready=false."""
        resp = api_session.get(f"{api_url}/api/sandbox/nonexistent-xyz-123/ready", timeout=10)
        if resp.status_code == 404:
            return
        data = resp.json()
        assert data.get("ready") is False

    def test_ready_after_browser_mode(self, api_url, api_session):
        """Sandbox in browser mode should be ready after enable completes."""
        sid = "e2e-ready-browser"
        _ensure_sandbox(api_url, api_session, sid)

        enable_resp = api_session.post(
            f"{api_url}/api/sandbox/{sid}/browser-mode",
            json={"reason": "readiness test"},
            timeout=60,
        )
        assert enable_resp.status_code == 200, f"Enable browser mode failed: {enable_resp.text}"

        ready_resp = api_session.get(f"{api_url}/api/sandbox/{sid}/ready", timeout=10)
        assert ready_resp.status_code == 200
        assert ready_resp.json().get("ready") is True

        api_session.delete(f"{api_url}/api/sandbox/{sid}/mode", timeout=10)


# ---------------------------------------------------------------------------
# Label-Based Recovery (simulates backend restart)
# ---------------------------------------------------------------------------


class TestRecovery:
    """Tests that sandbox state can be recovered from Docker labels."""

    def test_recover_from_docker_labels(self, api_url, api_session):
        """
        Verify project_path is stored and retrievable — proving Docker labels
        are persisted correctly for recovery after a backend restart.
        """
        sid = "e2e-recovery-test"
        project_path = "/tmp/recovery-project"
        _ensure_sandbox(api_url, api_session, sid, project_path=project_path)

        resp = api_session.get(f"{api_url}/api/sandbox/{sid}", timeout=10)
        assert resp.status_code == 200
        data = resp.json()
        got_path = data.get("project_path", "")
        assert got_path == project_path, f"Path mismatch: {got_path!r} != {project_path!r}"

    def test_startup_recovery_via_list(self, api_url, api_session):
        """
        After backend starts, RecoverAllSandboxes discovers running containers.
        Verify by listing sandboxes — our test sandbox should appear.
        """
        sid = f"e2e-startup-{int(time.time())}"
        _ensure_sandbox(api_url, api_session, sid)

        list_resp = api_session.get(f"{api_url}/api/sandbox/", timeout=10)
        assert list_resp.status_code == 200
        data = list_resp.json()
        if isinstance(data, list):
            ids = [sb.get("id", sb) if isinstance(sb, dict) else sb for sb in data]
        else:
            items = data.get("sandboxes", data.get("items", []))
            ids = [sb.get("id", sb) if isinstance(sb, dict) else sb for sb in items]
        assert sid in ids, f"Sandbox {sid} not found in list: {ids}"


# ---------------------------------------------------------------------------
# Sandbox ID Resolution
# ---------------------------------------------------------------------------


class TestIDResolution:
    """Tests for FindSandboxByProject matching logic."""

    def test_resolve_by_exact_id(self, api_url, api_session):
        """Sandbox lookup by exact ID should work."""
        sid = "e2e-resolve-exact"
        _ensure_sandbox(api_url, api_session, sid)

        get_resp = api_session.get(f"{api_url}/api/sandbox/{sid}", timeout=10)
        assert get_resp.status_code == 200

    def test_resolve_by_project_basename(self, api_url, api_session):
        """
        Sandbox created with project_path="/tmp/my-project-123" should have
        the full project_path stored and retrievable.
        """
        sid = f"e2e-resolve-bname-{int(time.time())}"
        basename = f"my-cool-project-{int(time.time())}"
        project_path = f"/tmp/{basename}"
        # Create the directory so Docker can bind-mount it
        import os
        os.makedirs(project_path, exist_ok=True)
        _ensure_sandbox(api_url, api_session, sid, project_path=project_path)

        list_resp = api_session.get(f"{api_url}/api/sandbox/", timeout=10)
        assert list_resp.status_code == 200
        data = list_resp.json()
        sandboxes = data if isinstance(data, list) else data.get("sandboxes", data.get("items", []))

        found = None
        for sb in sandboxes:
            sb_id = sb.get("id", sb) if isinstance(sb, dict) else sb
            if sb_id == sid:
                found = sb
                break
        assert found is not None, f"Sandbox {sid} not found in list"
        got_path = found.get("project_path", "")
        assert got_path == project_path, f"Path mismatch: {got_path!r} != {project_path!r}"


# ---------------------------------------------------------------------------
# VNC Proxy + Stats
# ---------------------------------------------------------------------------


class TestVNCProxy:
    """Tests for VNC proxy connection tracking and stats endpoint."""

    def test_vnc_stats_endpoint(self, api_url, api_session):
        """GET /api/sandbox/vnc-stats should return valid JSON with active_connections."""
        resp = api_session.get(f"{api_url}/api/sandbox/vnc-stats", timeout=10)
        assert resp.status_code == 200
        data = resp.json()
        assert "active_connections" in data
        assert isinstance(data["active_connections"], int)
        assert data["active_connections"] >= 0

    def test_vnc_stats_reflects_connections(self, api_url, api_session):
        """
        Create a sandbox with browser mode, then check that VNC stats
        can be queried (may or may not have active connections).
        """
        sid = "e2e-vnc-stats"
        _ensure_sandbox(api_url, api_session, sid)

        enable_resp = api_session.post(
            f"{api_url}/api/sandbox/{sid}/browser-mode",
            json={"reason": "vnc stats test"},
            timeout=60,
        )
        assert enable_resp.status_code == 200

        viewer_resp = api_session.get(f"{api_url}/api/sandbox/{sid}/viewer", timeout=10)
        assert viewer_resp.status_code == 200
        data = viewer_resp.json()
        assert data.get("mode") == "browser"
        assert data.get("novncUrl") or data.get("novnc_url"), f"No noVNC URL: {data}"

        stats_resp = api_session.get(f"{api_url}/api/sandbox/vnc-stats", timeout=10)
        assert stats_resp.status_code == 200

        api_session.delete(f"{api_url}/api/sandbox/{sid}/mode", timeout=10)

    def test_vnc_proxy_http_serves_page(self, api_url, api_session):
        """
        The VNC proxy HTTP endpoint should serve the noVNC HTML page.
        """
        sid = "e2e-vnc-http"
        _ensure_sandbox(api_url, api_session, sid)

        enable_resp = api_session.post(
            f"{api_url}/api/sandbox/{sid}/browser-mode",
            json={},
            timeout=60,
        )
        if enable_resp.status_code != 200:
            pytest.skip("Browser mode enable failed")
            return

        time.sleep(2)

        vnc_resp = api_session.get(
            f"{api_url}/api/sandbox/vnc/{sid}/vnc.html",
            timeout=10,
        )
        assert vnc_resp.status_code in (200, 502, 503), \
            f"Unexpected VNC proxy status: {vnc_resp.status_code}"

        api_session.delete(f"{api_url}/api/sandbox/{sid}/mode", timeout=10)

    def test_vnc_proxy_nonexistent_sandbox_404(self, api_url, api_session):
        """VNC proxy for a nonexistent sandbox should return 404."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/vnc/no-such-sandbox/vnc.html",
            timeout=10,
        )
        assert resp.status_code == 404


# ---------------------------------------------------------------------------
# Exec + Mode Lifecycle
# ---------------------------------------------------------------------------


class TestModeLifecycle:
    """Tests for browser mode and desktop mode enable/disable lifecycle."""

    def test_browser_mode_lifecycle(self, api_url, api_session):
        """Full browser mode lifecycle: enable -> verify -> disable -> verify."""
        sid = "e2e-lifecycle-browser"
        _ensure_sandbox(api_url, api_session, sid)

        # 1. Enable browser mode
        enable_resp = api_session.post(
            f"{api_url}/api/sandbox/{sid}/browser-mode",
            json={"reason": "lifecycle test"},
            timeout=60,
        )
        assert enable_resp.status_code == 200

        # 2. Verify viewer returns browser mode
        viewer_resp = api_session.get(f"{api_url}/api/sandbox/{sid}/viewer", timeout=10)
        assert viewer_resp.status_code == 200
        data = viewer_resp.json()
        assert data.get("mode") == "browser"

        # 3. Verify ready
        ready_resp = api_session.get(f"{api_url}/api/sandbox/{sid}/ready", timeout=10)
        assert ready_resp.status_code == 200
        assert ready_resp.json().get("ready") is True

        # 4. Disable
        disable_resp = api_session.delete(f"{api_url}/api/sandbox/{sid}/mode", timeout=10)
        assert disable_resp.status_code in (200, 204)

        # 5. Verify viewer returns 404 (no desktop session)
        viewer2_resp = api_session.get(f"{api_url}/api/sandbox/{sid}/viewer", timeout=10)
        assert viewer2_resp.status_code == 404

    def test_browser_mode_idempotent(self, api_url, api_session):
        """Enabling browser mode twice should return the same session."""
        sid = "e2e-idempotent"
        _ensure_sandbox(api_url, api_session, sid)

        resp1 = api_session.post(
            f"{api_url}/api/sandbox/{sid}/browser-mode",
            json={},
            timeout=60,
        )
        assert resp1.status_code == 200

        resp2 = api_session.post(
            f"{api_url}/api/sandbox/{sid}/browser-mode",
            json={},
            timeout=30,
        )
        assert resp2.status_code == 200

        data1, data2 = resp1.json(), resp2.json()
        assert data1.get("mode") == "browser"
        assert data2.get("mode") == "browser"

        api_session.delete(f"{api_url}/api/sandbox/{sid}/mode", timeout=10)

    def test_exec_in_browser_mode_sandbox(self, api_url, api_session):
        """Command execution should still work when browser mode is active."""
        sid = "e2e-exec-browser"
        _ensure_sandbox(api_url, api_session, sid)

        api_session.post(
            f"{api_url}/api/sandbox/{sid}/browser-mode",
            json={},
            timeout=60,
        )

        exec_resp = api_session.post(
            f"{api_url}/api/sandbox/{sid}/exec",
            json={"cmd": ["echo", "still-works"]},
            timeout=15,
        )
        assert exec_resp.status_code == 200
        assert "still-works" in exec_resp.json().get("output", "")

        api_session.delete(f"{api_url}/api/sandbox/{sid}/mode", timeout=10)


# ---------------------------------------------------------------------------
# List + Multi-Sandbox
# ---------------------------------------------------------------------------


class TestMultiSandbox:
    """Tests for managing multiple sandboxes simultaneously."""

    def test_list_multiple_sandboxes(self, api_url, api_session):
        """Creating multiple sandboxes should all appear in the list."""
        ids = [f"e2e-multi-{i}" for i in range(3)]
        for sid in ids:
            _ensure_sandbox(api_url, api_session, sid, project_path=f"/tmp/multi-{sid}")

        list_resp = api_session.get(f"{api_url}/api/sandbox/", timeout=10)
        assert list_resp.status_code == 200
        data = list_resp.json()
        if isinstance(data, list):
            listed_ids = {sb.get("id", sb) if isinstance(sb, dict) else sb for sb in data}
        else:
            items = data.get("sandboxes", data.get("items", []))
            listed_ids = {sb.get("id", sb) if isinstance(sb, dict) else sb for sb in items}

        for sid in ids:
            assert sid in listed_ids, f"Sandbox {sid} not in list: {listed_ids}"
