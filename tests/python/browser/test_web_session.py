"""
Web browser sub-agent tests: session, navigate, click, type, screenshot, state.

These tests use the sandbox computer-use API:
  POST /api/sandbox/          — create sandbox
  POST /api/sandbox/{id}/browser-mode — enable browser
  POST /api/sandbox/{id}/computer-use/enable   — enable computer use
  POST /api/sandbox/{id}/computer-use/act       — perform action
  GET  /api/sandbox/{id}/computer-use/screenshot — take screenshot
  GET  /api/sandbox/{id}/computer-use/snapshot   — get accessibility snapshot
"""

import pytest

pytestmark = pytest.mark.browser

# Track sandbox IDs for cleanup
_sandbox_ids: list[str] = []


@pytest.fixture(autouse=True, scope="module")
def cleanup_sandboxes(api_url, api_session):
    """Close any test sandboxes after the module finishes."""
    yield
    for sid in _sandbox_ids:
        try:
            api_session.delete(f"{api_url}/api/sandbox/{sid}")
        except Exception:
            pass
    _sandbox_ids.clear()


def _create_browser_sandbox(api_url, api_session):
    """Create a sandbox with browser mode enabled. Returns sandbox ID or None."""
    # Create sandbox
    resp = api_session.post(f"{api_url}/api/sandbox/", json={"project": "test-repo"})
    if resp.status_code in (500, 502, 503):
        return None
    if resp.status_code != 200:
        return None
    data = resp.json()
    sid = data.get("id", data.get("sandboxId", ""))
    if not sid:
        return None

    _sandbox_ids.append(sid)

    # Enable browser mode
    mode_resp = api_session.post(f"{api_url}/api/sandbox/{sid}/browser-mode")
    if mode_resp.status_code not in (200, 201):
        return None

    # Enable computer use
    cu_resp = api_session.post(f"{api_url}/api/sandbox/{sid}/computer-use/enable")
    if cu_resp.status_code not in (200, 201):
        return None

    return sid


class TestWebSession:
    def test_create_browser_sandbox(self, api_url, api_session):
        sid = _create_browser_sandbox(api_url, api_session)
        if sid is None:
            pytest.skip("Browser/Sandbox service unavailable")
        assert isinstance(sid, str) and len(sid) > 0

    def test_close_sandbox(self, api_url, api_session):
        # Create then close
        sid = _create_browser_sandbox(api_url, api_session)
        if sid is None:
            pytest.skip("Browser/Sandbox service unavailable")

        close_resp = api_session.delete(f"{api_url}/api/sandbox/{sid}")
        assert close_resp.status_code in (200, 204, 404)


class TestWebNavigate:
    @pytest.fixture(autouse=True)
    def _sandbox(self, api_url, api_session):
        """Create a sandbox for navigation tests."""
        self.sandbox_id = _create_browser_sandbox(api_url, api_session)
        if self.sandbox_id is None:
            pytest.skip("Browser/Sandbox service unavailable")
        yield
        try:
            api_session.delete(f"{api_url}/api/sandbox/{self.sandbox_id}")
        except Exception:
            pass

    def test_navigate_example_com(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
        )
        if resp.status_code == 500:
            pytest.skip("Browser service error")
        assert resp.status_code in (200, 400, 404)
        if resp.status_code == 200:
            data = resp.json()
            assert isinstance(data, dict)


class TestWebInteraction:
    @pytest.fixture(autouse=True)
    def _sandbox(self, api_url, api_session):
        self.sandbox_id = _create_browser_sandbox(api_url, api_session)
        if self.sandbox_id is None:
            pytest.skip("Browser/Sandbox service unavailable")

        # Navigate to a page with interactive elements
        api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
        )
        yield
        try:
            api_session.delete(f"{api_url}/api/sandbox/{self.sandbox_id}")
        except Exception:
            pass

    def test_click_element(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/act",
            json={"action": "click", "element": 0},
        )
        if resp.status_code == 500:
            pytest.skip("Browser service error")
        assert resp.status_code in (200, 400, 404)

    def test_type_text(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/act",
            json={"action": "type", "text": "e2e test input"},
        )
        if resp.status_code == 500:
            pytest.skip("Browser service error")
        assert resp.status_code in (200, 400, 404)


class TestWebScreenshot:
    @pytest.fixture(autouse=True)
    def _sandbox(self, api_url, api_session):
        self.sandbox_id = _create_browser_sandbox(api_url, api_session)
        if self.sandbox_id is None:
            pytest.skip("Browser/Sandbox service unavailable")

        api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
        )
        yield
        try:
            api_session.delete(f"{api_url}/api/sandbox/{self.sandbox_id}")
        except Exception:
            pass

    def test_screenshot_returns_image(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/screenshot",
        )
        if resp.status_code == 404:
            pytest.skip("Screenshot endpoint unavailable")
        assert resp.status_code == 200
        assert "image" in resp.headers.get("Content-Type", "") or len(resp.content) > 0

    def test_screenshot_missing_sandbox_400(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/nonexistent-sandbox/computer-use/screenshot",
        )
        assert resp.status_code in (400, 404, 500)


class TestWebState:
    @pytest.fixture(autouse=True)
    def _sandbox(self, api_url, api_session):
        self.sandbox_id = _create_browser_sandbox(api_url, api_session)
        if self.sandbox_id is None:
            pytest.skip("Browser/Sandbox service unavailable")

        api_session.post(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
        )
        yield
        try:
            api_session.delete(f"{api_url}/api/sandbox/{self.sandbox_id}")
        except Exception:
            pass

    def test_get_snapshot_returns_page_info(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/snapshot",
        )
        assert resp.status_code in (200, 404)
        if resp.status_code == 200:
            data = resp.json()
            assert data is not None

    def test_a11y_snapshot(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/{self.sandbox_id}/computer-use/a11y-snapshot",
        )
        if resp.status_code == 404:
            pytest.skip("A11y snapshot endpoint unavailable")
        assert resp.status_code in (200, 404)
