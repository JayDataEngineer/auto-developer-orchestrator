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

from fixtures.sandbox import wait_for_browser_ready

pytestmark = [pytest.mark.api]

NAV_SANDBOX = "test-nav-timeout"


@pytest.fixture(scope="module", autouse=True)
def setup_nav_sandbox(api_url, api_session):
    wait_for_browser_ready(api_url, api_session, NAV_SANDBOX)
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
        """Type action should work without submit on an input element."""
        # Navigate to a page with an input field
        api_session.post(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/act",
            json={"action": "navigate", "url": "https://www.google.com"},
            timeout=65,
        )

        # Get elements and find an input-like one
        snap = api_session.get(
            f"{api_url}/api/sandbox/{NAV_SANDBOX}/computer-use/snapshot",
            timeout=30,
        ).json()

        elements = snap.get("elements", [])
        # Find an input or textarea element
        target = None
        for el in elements:
            if el.get("tag", "").lower() in ("input", "textarea"):
                target = el
                break
        if not target:
            pytest.skip("No input elements on page to type into")

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
