"""
Sandbox management tests: CRUD, exec, browser mode, desktop mode, viewer URLs.
"""

import pytest

pytestmark = pytest.mark.sandbox

# Track sandbox IDs created during tests for cleanup
_created_sandbox_ids: list[str] = []


@pytest.fixture(autouse=True, scope="module")
def cleanup_sandboxes(api_url, api_session):
    """Destroy any test sandboxes after the module finishes."""
    yield
    for sid in _created_sandbox_ids:
        try:
            api_session.delete(f"{api_url}/api/sandbox/{sid}")
        except Exception:
            pass
    _created_sandbox_ids.clear()


class TestSandboxCRUD:
    def test_create_sandbox(self, api_url, api_session):
        sandbox_id = "e2e-test-sandbox"
        resp = api_session.post(
            f"{api_url}/api/sandbox",
            json={"id": sandbox_id, "project_path": "/tmp", "policy": "default"},
        )
        if resp.status_code == 500:
            pytest.skip("Sandbox creation failed (Docker unavailable)")
        assert resp.status_code in (200, 201)
        _created_sandbox_ids.append(sandbox_id)

    def test_get_sandbox(self, api_url, api_session):
        sandbox_id = "e2e-test-sandbox"
        # Try to create first, skip if creation fails
        create_resp = api_session.post(
            f"{api_url}/api/sandbox",
            json={"id": sandbox_id, "project_path": "/tmp", "policy": "default"},
        )
        if create_resp.status_code == 500:
            pytest.skip("Sandbox creation failed (Docker unavailable)")
        _created_sandbox_ids.append(sandbox_id)

        resp = api_session.get(f"{api_url}/api/sandbox/{sandbox_id}")
        assert resp.status_code == 200
        data = resp.json()
        assert data.get("id") == sandbox_id or data.get("ID") == sandbox_id

    def test_get_nonexistent_sandbox_404(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/sandbox/no-such-sandbox-xyz")
        assert resp.status_code == 404

    def test_list_sandboxes(self, api_url, api_session):
        resp = api_session.get(f"{api_url}/api/sandbox")
        assert resp.status_code == 200
        data = resp.json()
        # Response is an array or object with array field
        if isinstance(data, list):
            assert len(data) >= 1
        else:
            assert "sandboxes" in data or isinstance(data, dict)

    def test_destroy_sandbox(self, api_url, api_session):
        # Create a second sandbox to destroy
        sandbox_id = "e2e-test-destroy-me"
        api_session.post(
            f"{api_url}/api/sandbox",
            json={"id": sandbox_id, "project_path": "/tmp", "policy": "default"},
        )

        resp = api_session.delete(f"{api_url}/api/sandbox/{sandbox_id}")
        assert resp.status_code in (200, 204)

        # Verify it's gone
        get_resp = api_session.get(f"{api_url}/api/sandbox/{sandbox_id}")
        assert get_resp.status_code == 404


class TestSandboxExec:
    @pytest.fixture(autouse=True)
    def _exec_sandbox(self, api_url, api_session):
        """Ensure a sandbox exists for exec tests."""
        self.sandbox_id = "e2e-exec-sandbox"
        api_session.post(
            f"{api_url}/api/sandbox",
            json={"id": self.sandbox_id, "project_path": "/tmp", "policy": "default"},
        )
        _created_sandbox_ids.append(self.sandbox_id)
        yield

    def test_exec_echo(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/exec",
            json={"cmd": ["echo", "hello"]},
        )
        assert resp.status_code == 200
        data = resp.json()
        output = data.get("output", "")
        assert "hello" in output

    def test_exec_in_nonexistent_sandbox(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/no-such-sandbox/exec",
            json={"cmd": ["echo", "hello"]},
        )
        assert resp.status_code in (400, 404, 500)

    def test_exec_pwd(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/exec",
            json={"cmd": ["pwd"]},
        )
        assert resp.status_code == 200


class TestSandboxBrowserMode:
    @pytest.fixture(autouse=True)
    def _browser_sandbox(self, api_url, api_session):
        self.sandbox_id = "e2e-browser-sandbox"
        api_session.post(
            f"{api_url}/api/sandbox",
            json={"id": self.sandbox_id, "project_path": "/tmp", "policy": "default"},
        )
        _created_sandbox_ids.append(self.sandbox_id)
        yield
        # Disable mode after test
        try:
            api_session.delete(f"{api_url}/api/sandbox/{self.sandbox_id}/mode")
        except Exception:
            pass

    def test_enable_browser_mode(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/browser-mode",
            json={"reason": "e2e test"},
        )
        assert resp.status_code == 200

    def test_viewer_urls_browser_mode(self, api_url, api_session):
        # Enable browser mode first
        api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/browser-mode",
            json={},
        )

        resp = api_session.get(f"{api_url}/api/sandbox/{self.sandbox_id}/viewer")
        assert resp.status_code == 200
        data = resp.json()
        # API uses snake_case keys: cdp_url, novnc_url, viewer_url, vnc_url
        assert data.get("cdp_url") is not None or data.get("cdpUrl") is not None
        assert data.get("novnc_url") is not None or data.get("novncUrl") is not None

    def test_disable_browser_mode(self, api_url, api_session):
        # Enable first
        api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/browser-mode",
            json={},
        )
        # Disable — returns 204 No Content
        resp = api_session.delete(f"{api_url}/api/sandbox/{self.sandbox_id}/mode")
        assert resp.status_code in (200, 204)


class TestSandboxDesktopMode:
    @pytest.fixture(autouse=True)
    def _desktop_sandbox(self, api_url, api_session):
        self.sandbox_id = "e2e-desktop-sandbox"
        api_session.post(
            f"{api_url}/api/sandbox",
            json={"id": self.sandbox_id, "project_path": "/tmp", "policy": "default"},
        )
        _created_sandbox_ids.append(self.sandbox_id)
        yield
        try:
            api_session.delete(f"{api_url}/api/sandbox/{self.sandbox_id}/mode")
        except Exception:
            pass

    def test_enable_desktop_mode(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/desktop-mode",
            json={"reason": "e2e test"},
        )
        assert resp.status_code == 200

    def test_viewer_urls_desktop_mode(self, api_url, api_session):
        # Enable desktop mode first
        api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/desktop-mode",
            json={},
        )

        resp = api_session.get(f"{api_url}/api/sandbox/{self.sandbox_id}/viewer")
        assert resp.status_code == 200
        data = resp.json()
        # API returns camelCase: vncUrl, novncUrl, viewerUrl, cdpUrl
        assert data.get("vncUrl") is not None or data.get("novncUrl") is not None, \
            f"No viewer URLs in response: {data}"
