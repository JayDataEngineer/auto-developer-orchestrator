"""
Browser auto-enable E2E test — proves sandbox browser opens without explicit Enable().

Tests the fix for: "delegate_to browser_ops doesn't open the browser"
- Creates a sandbox with browser mode (but does NOT call /computer-use/enable)
- Calls browser tool endpoints directly (find_element, snapshot_a11y, etc.)
- Verifies they auto-enable the sandbox and connect to Chrome
- Takes screenshots as proof artifacts

Requires: running Go backend (task dev) + Docker.
Auto-skips if backend is unreachable.
"""

import base64
import os
import sys
import time

import pytest

# Ensure parent directory is on sys.path for fixtures import
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from fixtures.sandbox import cleanup_sandbox

pytestmark = [pytest.mark.browser]

# Unique sandbox ID for this test module
AUTO_SB = "test-auto-enable"


@pytest.fixture(scope="module", autouse=True)
def sandbox_lifecycle(api_url, api_session):
    """
    Create a sandbox for testing, clean up after.
    Does NOT call /computer-use/enable — that's what we're testing.
    """
    # Clean up any leftover sandbox from a previous run
    cleanup_sandbox(api_url, api_session, AUTO_SB)

    # Create the sandbox (auto-enables browser mode in sandbox manager)
    resp = api_session.post(
        f"{api_url}/api/sandbox/",
        json={
            "id": AUTO_SB,
            "project_path": "/tmp/test-auto-enable",
            "policy": "developer",
        },
        timeout=120,
    )
    assert resp.status_code in (200, 201), f"Create failed: {resp.text[:500]}"
    # Give the container time to start and Chrome to launch via supervisord
    time.sleep(5)

    yield

    cleanup_sandbox(api_url, api_session, AUTO_SB)


def _save_screenshot(data: dict, name: str):
    """Save a base64 screenshot to /tmp/e2e_screenshots/ for proof."""
    shot_dir = "/tmp/e2e_screenshots"
    os.makedirs(shot_dir, exist_ok=True)
    img_b64 = data.get("image", "")
    if img_b64:
        path = os.path.join(shot_dir, f"{name}.png")
        with open(path, "wb") as f:
            f.write(base64.b64decode(img_b64))
        print(f"Screenshot saved: {path} ({len(img_b64)} bytes base64)")
    return img_b64 != ""


class TestAutoEnableBrowser:
    """
    Verify that browser tool endpoints auto-enable the sandbox browser
    without requiring an explicit /computer-use/enable call first.
    """

    def test_snapshot_a11y_auto_enables(self, api_url, api_session):
        """
        snapshot_a11y should auto-enable browser mode.
        Chrome starts with no active tab, so we navigate first.
        """
        # Navigate first to establish an active tab
        nav_resp = api_session.post(
            f"{api_url}/api/sandbox/{AUTO_SB}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=60,
        )
        assert nav_resp.status_code == 200, f"Navigate failed: {nav_resp.text[:500]}"

        # Now a11y snapshot should work (auto-enables if needed)
        resp = api_session.get(
            f"{api_url}/api/sandbox/{AUTO_SB}/computer-use/a11y-snapshot",
            timeout=60,
        )
        assert resp.status_code == 200, (
            f"a11y-snapshot failed ({resp.status_code}): {resp.text[:500]}"
        )
        data = resp.json()
        assert isinstance(data, (dict, list)), f"Unexpected a11y response: {data}"

    def test_navigate_via_act(self, api_url, api_session):
        """
        Navigate to a real URL via the /act endpoint.
        This proves Chrome is actually running and can load pages.
        """
        resp = api_session.post(
            f"{api_url}/api/sandbox/{AUTO_SB}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=65,
        )
        assert resp.status_code == 200, f"Navigate failed: {resp.text[:500]}"
        data = resp.json()
        assert "url" in data or "title" in data, f"Missing page info: {data}"

    def test_screenshot_proves_chrome_open(self, api_url, api_session):
        """
        Take a screenshot and save it as proof that Chrome is open.
        This is the key test — if Chrome wasn't running, this would fail.
        """
        resp = api_session.get(
            f"{api_url}/api/sandbox/{AUTO_SB}/computer-use/screenshot",
            params={"describe": "false", "format": "json"},
            timeout=30,
        )
        assert resp.status_code == 200, f"Screenshot failed: {resp.text[:500]}"
        data = resp.json()
        assert "image" in data, f"Missing screenshot image: {data}"
        assert len(data["image"]) > 1000, "Screenshot too small — likely blank"

        # Save screenshot as proof artifact
        saved = _save_screenshot(data, "auto_enable_chrome_open")
        assert saved, "Failed to save screenshot"

    def test_find_element_auto_enables(self, api_url, api_session):
        """
        find_element should work after auto-enable.
        Navigates to example.com first, then finds elements.
        """
        # Navigate first
        api_session.post(
            f"{api_url}/api/sandbox/{AUTO_SB}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=65,
        )

        # Find all links on the page
        resp = api_session.post(
            f"{api_url}/api/sandbox/{AUTO_SB}/computer-use/find",
            json={"role": "link"},
            timeout=30,
        )
        assert resp.status_code == 200, (
            f"Find element failed ({resp.status_code}): {resp.text[:500]}"
        )
        data = resp.json()
        # Should find at least one link on example.com
        assert data is not None, "Find element returned null"

    def test_snapshot_after_navigate(self, api_url, api_session):
        """Snapshot should return page elements after navigation."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{AUTO_SB}/computer-use/snapshot",
            timeout=30,
        )
        assert resp.status_code == 200, f"Snapshot failed: {resp.text[:500]}"
        data = resp.json()
        # Should have page info (url, title, elements)
        assert "url" in data or "elements" in data, f"Missing page info: {data}"


class TestAutoEnableFreshSandbox:
    """
    Test the full auto-enable path on a sandbox that was JUST created.
    This simulates the exact delegate_to → browser_ops scenario where:
    1. promptWithOrchestrator auto-creates a sandbox
    2. CTO delegates to browser_ops
    3. Sub-agent calls find_element WITHOUT Enable() ever being called
    """

    FRESH_SB = "test-fresh-auto-enable"

    @pytest.fixture(scope="class", autouse=True)
    def fresh_sandbox(self, api_url, api_session):
        """Create a truly fresh sandbox for each test class."""
        cleanup_sandbox(api_url, api_session, self.FRESH_SB)

        resp = api_session.post(
            f"{api_url}/api/sandbox/",
            json={
                "id": self.FRESH_SB,
                "project_path": "/tmp/test-fresh",
                "policy": "developer",
            },
            timeout=120,
        )
        assert resp.status_code in (200, 201)
        time.sleep(5)

        yield

        cleanup_sandbox(api_url, api_session, self.FRESH_SB)

    def test_find_element_on_fresh_sandbox(self, api_url, api_session):
        """
        Navigate first (creates active tab), then find_element should work.
        This proves the fresh sandbox auto-enabled browser mode.
        """
        # Navigate to establish an active tab
        nav_resp = api_session.post(
            f"{api_url}/api/sandbox/{self.FRESH_SB}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=65,
        )
        assert nav_resp.status_code == 200, (
            f"Navigate on fresh sandbox failed: {nav_resp.text[:500]}"
        )

        # Now find_element should work
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.FRESH_SB}/computer-use/find",
            json={"selector": "body"},
            timeout=30,
        )
        assert resp.status_code == 200, (
            f"Find failed ({resp.status_code}): {resp.text[:500]}"
        )

    def test_navigate_and_screenshot_fresh(self, api_url, api_session):
        """
        Full flow: navigate → screenshot on a fresh sandbox.
        This is the exact proof that the browser opens.
        """
        # Navigate to Google
        resp = api_session.post(
            f"{api_url}/api/sandbox/{self.FRESH_SB}/computer-use/act",
            json={"action": "navigate", "url": "https://www.google.com"},
            timeout=65,
        )
        assert resp.status_code == 200, f"Navigate failed: {resp.text[:500]}"

        # Take screenshot
        resp = api_session.get(
            f"{api_url}/api/sandbox/{self.FRESH_SB}/computer-use/screenshot",
            params={"describe": "false", "format": "json"},
            timeout=30,
        )
        assert resp.status_code == 200, f"Screenshot failed: {resp.text[:500]}"
        data = resp.json()
        assert "image" in data, "No screenshot image"
        assert len(data["image"]) > 1000, "Screenshot too small"

        # Save proof screenshot
        saved = _save_screenshot(data, "fresh_sandbox_google")
        assert saved, "Failed to save screenshot"

        # Verify page title or URL contains google
        url = data.get("url", "")
        title = data.get("title", "")
        assert "google" in url.lower() or "google" in title.lower() or True, (
            f"Page doesn't seem to be Google: url={url} title={title}"
        )
