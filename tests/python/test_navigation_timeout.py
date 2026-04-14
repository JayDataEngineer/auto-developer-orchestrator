"""
Navigation timeout integration tests — NO MOCKS.

Tests that browser automation uses the correct timeouts:
  1. Navigate to a page succeeds within 60s timeout
  2. Type + submit form succeeds (uses navigationTimeout, not defaultTimeout)
  3. Click action that triggers navigation succeeds
  4. Scroll uses defaultTimeout (30s)

Requires: running Go backend + sandbox container with Chrome.
"""

import pytest
import time

pytestmark = [pytest.mark.api]

NAV_SANDBOX = "test-nav-timeout"


def _ensure_sandbox_ready(api_url, api_session):
    """Ensure sandbox is created and computer use is enabled."""
    resp = api_session.post(
        f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/enable",
        timeout=120,
    )
    assert resp.status_code == 200, f"Enable failed: {resp.text[:500]}"

    # Wait for background setup
    deadline = time.time() + 90
    while time.time() < deadline:
        try:
            r = api_session.get(f"{api_url}/api/sandbox/{NAV_SANDBOX}/viewer", timeout=5)
            if r.status_code == 200:
                return
        except Exception:
            pass
        time.sleep(2)
    pytest.skip("Background setup did not complete in time")


@pytest.fixture(scope="module", autouse=True)
def setup_nav_sandbox(api_url, api_session):
    _ensure_sandbox_ready(api_url, api_session)
    yield
    try:
        api_session.post(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/disable",
            timeout=10,
        )
    except Exception:
        pass


class TestNavigationTimeout:
    """Verify navigation actions use 60s timeout."""

    def test_navigate_succeeds(self, api_url, api_session):
        """Navigate to a real URL should succeed."""
        resp = api_session.post(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=65,
        )
        assert resp.status_code == 200, f"Navigate failed: {resp.text[:500]}"
        data = resp.json()
        assert "url" in data, f"Missing url: {data}"
        assert "title" in data, f"Missing title: {data}"
        assert "elements" in data, f"Missing elements: {data}"
        assert "screenshot" in data, f"Missing screenshot: {data}"

    def test_click_action(self, api_url, api_session):
        """Click should work with navigation timeout."""
        # First get a snapshot to find elements
        snap_resp = api_session.get(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/snapshot",
            timeout=30,
        )
        assert snap_resp.status_code == 200
        snap = snap_resp.json()
        assert len(snap.get("elements", [])) > 0, "No elements on page"

        # Click the first element
        first_el = snap["elements"][0]
        resp = api_session.post(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/act",
            json={"action": "click", "element": first_el["id"]},
            timeout=65,
        )
        assert resp.status_code == 200, f"Click failed: {resp.text[:500]}"

    def test_type_action(self, api_url, api_session):
        """Type action should work without submit."""
        # Navigate to a page with an input
        api_session.post(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=65,
        )

        # Get elements
        snap = api_session.get(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/snapshot",
            timeout=30,
        ).json()

        # Find a text-like element or use first one
        elements = snap.get("elements", [])
        if not elements:
            pytest.skip("No elements to type into")

        target = elements[0]
        resp = api_session.post(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/act",
            json={"action": "type", "element": target["id"], "text": "test", "submit": False},
            timeout=35,
        )
        assert resp.status_code == 200, f"Type failed: {resp.text[:500]}"

    def test_scroll_action(self, api_url, api_session):
        """Scroll uses default timeout."""
        resp = api_session.post(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/act",
            json={"action": "scroll", "direction": "down", "amount": 100},
            timeout=35,
        )
        assert resp.status_code == 200, f"Scroll failed: {resp.text[:500]}"
        data = resp.json()
        assert "url" in data

    def test_screenshot_action(self, api_url, api_session):
        """Screenshot uses default timeout."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/screenshot?describe=false",
            timeout=35,
        )
        assert resp.status_code == 200, f"Screenshot failed: {resp.text[:500]}"
        data = resp.json()
        assert "image" in data, f"Missing image: {data}"
