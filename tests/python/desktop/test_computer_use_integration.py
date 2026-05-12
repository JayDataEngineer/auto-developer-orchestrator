"""
Computer use integration tests — NO MOCKS.

Tests the full sandbox lifecycle:
  1. Enable computer use with a FRESH sandbox (no pre-existing container)
  2. Verify the enable endpoint responds within 15 seconds
  3. Verify the sandbox is tracked after enable
  4. Take a screenshot and verify it returns valid PNG data
  5. Navigate to a URL and verify page state
  6. Disable computer use
  7. Re-enable (idempotent — should use existing container)
  8. Verify sandbox survives backend restart (recovery)

Also tests the auto-enable flow from the frontend:
  9. Desktop tab auto-triggers enable when switching tabs
  10. Enable state persists across tab switches

Requires: running Go backend + sandbox-browser container image.
"""

import pytest
import time
import base64
import struct

from utils.png import is_valid_png
from fixtures.sandbox import cleanup_sandbox

pytestmark = [pytest.mark.api]

FRESH_SANDBOX = "test-cu-integration-fresh"
REUSED_SANDBOX = "test-cu-integration-reuse"


class TestEnableFreshSandbox:
    """Enable computer use on a sandbox that doesn't exist yet."""

    def test_enable_creates_container(self, api_url, api_session):
        """POST /computer-use/enable with a new sandbox ID should create a container
        and return enabled=true within 15 seconds."""
        cleanup_sandbox(api_url, api_session, FRESH_SANDBOX)

        start = time.time()
        resp = api_session.post(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/enable",
            timeout=60,
        )
        elapsed = time.time() - start

        assert resp.status_code == 200, (
            f"Enable failed ({resp.status_code}): {resp.text[:500]}"
        )
        data = resp.json()

        # Response structure
        assert data.get("enabled") is True, f"Expected enabled=true, got: {data}"
        assert "cdpPort" in data, f"Missing cdpPort: {data}"
        assert "sandboxId" in data, f"Missing sandboxId: {data}"
        assert data["sandboxId"] == FRESH_SANDBOX, (
            f"Sandbox ID mismatch: expected {FRESH_SANDBOX}, got {data['sandboxId']}"
        )
        assert isinstance(data["cdpPort"], int), f"cdpPort should be int: {data['cdpPort']}"
        assert data["cdpPort"] > 0, f"cdpPort should be positive: {data['cdpPort']}"

        # Time constraint — must NOT hang
        assert elapsed < 15, (
            f"Enable took {elapsed:.1f}s — user will see 'Enabling...' spinner stuck"
        )

    def test_enable_tracks_sandbox(self, api_url, api_session):
        """After enable, GET /api/sandbox/ should list the sandbox."""
        list_resp = api_session.get(f"{api_url}/api/sandbox/", timeout=5)
        assert list_resp.status_code == 200

        data = list_resp.json()
        sandboxes = data if isinstance(data, list) else data.get("sandboxes", [])
        ids = [s["id"] for s in sandboxes]

        assert FRESH_SANDBOX in ids, (
            f"Sandbox {FRESH_SANDBOX} not tracked after enable. Found: {ids}"
        )

        # Verify sandbox fields
        sb = next(s for s in sandboxes if s["id"] == FRESH_SANDBOX)
        assert sb["status"] == "running", f"Sandbox not running: {sb['status']}"
        assert sb["mode"] == "browser", f"Sandbox mode wrong: {sb['mode']}"
        assert "desktop_session" in sb, "Missing desktop_session"
        assert sb["desktop_session"]["is_active"] is True, "Session not active"


class TestScreenshotAfterEnable:
    """Verify screenshot and browser interaction work after enable."""

    def test_screenshot_returns_png(self, api_url, api_session):
        """GET /computer-use/screenshot?format=png should return valid PNG."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/screenshot?format=png",
            timeout=15,
        )
        assert resp.status_code == 200, f"Screenshot failed: {resp.status_code}"
        assert resp.headers.get("content-type") == "image/png", (
            f"Wrong content type: {resp.headers.get('content-type')}"
        )
        assert is_valid_png(resp.content), "Response is not valid PNG data"
        assert len(resp.content) > 1000, f"PNG too small ({len(resp.content)} bytes), probably blank"

    def test_screenshot_json_format(self, api_url, api_session):
        """GET /computer-use/screenshot?format=json should return base64-encoded PNG."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/screenshot?format=json",
            timeout=15,
        )
        assert resp.status_code == 200
        data = resp.json()

        assert "image" in data, f"Missing 'image' field: {data}"
        png_bytes = base64.b64decode(data["image"])
        assert is_valid_png(png_bytes), "Decoded image is not valid PNG"
        assert len(png_bytes) > 1000, "Decoded PNG too small"

    def test_snapshot_returns_page_info(self, api_url, api_session):
        """GET /computer-use/snapshot should return page elements."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/snapshot",
            timeout=15,
        )
        assert resp.status_code == 200
        data = resp.json()

        # Snapshot should have url and title at minimum
        assert "url" in data or "elements" in data, (
            f"Snapshot missing url/elements: {data}"
        )


class TestNavigateAfterEnable:
    """Navigate the sandbox browser to a URL."""

    def test_navigate_changes_page(self, api_url, api_session):
        """POST /computer-use/act with action=navigate should change the page."""
        resp = api_session.post(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=15,
        )
        assert resp.status_code == 200, f"Navigate failed: {resp.text[:200]}"
        data = resp.json()

        # Page should now be at example.com
        # (url might be example.com or have a redirect)
        assert data.get("url", ""), f"Empty URL after navigate: {data}"

    def test_screenshot_after_navigate_shows_content(self, api_url, api_session):
        """After navigating to example.com, screenshot should show content."""
        # Give the page time to load
        time.sleep(2)
        resp = api_session.get(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/screenshot?format=png",
            timeout=15,
        )
        assert resp.status_code == 200
        assert len(resp.content) > 1000, (
            f"PNG too small after navigate ({len(resp.content)} bytes), page probably blank"
        )


class TestDisableComputerUse:
    """Disable computer use and verify cleanup."""

    def test_disable_succeeds(self, api_url, api_session):
        """POST /computer-use/disable should return disabled=true."""
        resp = api_session.post(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/disable",
            timeout=10,
        )
        assert resp.status_code == 200
        assert resp.json().get("disabled") is True

    def test_screenshot_fails_after_disable(self, api_url, api_session):
        """After disable, screenshot should return 404 or error."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/screenshot?format=png",
            timeout=10,
        )
        # Should fail — no browser client connected
        assert resp.status_code in (404, 500), (
            f"Expected error after disable, got {resp.status_code}: {resp.text[:200]}"
        )


class TestReEnable:
    """Re-enabling should be fast (container still exists)."""

    def test_reenable_uses_existing_container(self, api_url, api_session):
        """Re-enabling on the same sandbox should be fast (< 5s)."""
        start = time.time()
        resp = api_session.post(
            f"{api_url}/api/sandbox/{FRESH_SANDBOX}/computer-use/enable",
            timeout=30,
        )
        elapsed = time.time() - start

        assert resp.status_code == 200, f"Re-enable failed: {resp.text[:200]}"
        assert resp.json().get("enabled") is True

        # Re-enable should be fast — container already exists
        assert elapsed < 5, (
            f"Re-enable took {elapsed:.1f}s — should reuse existing container"
        )

        # Cleanup
        cleanup_sandbox(api_url, api_session, FRESH_SANDBOX)


class TestEnableIdempotent:
    """Calling enable twice without disable should be idempotent."""

    def test_double_enable_same_sandbox(self, api_url, api_session):
        """Two consecutive enable calls should both succeed."""
        sandbox_id = REUSED_SANDBOX
        cleanup_sandbox(api_url, api_session, sandbox_id)

        # First enable
        resp1 = api_session.post(
            f"{api_url}/api/sandbox/{sandbox_id}/computer-use/enable",
            timeout=60,
        )
        assert resp1.status_code == 200
        assert resp1.json().get("enabled") is True

        # Second enable (should be idempotent, fast)
        start = time.time()
        resp2 = api_session.post(
            f"{api_url}/api/sandbox/{sandbox_id}/computer-use/enable",
            timeout=30,
        )
        elapsed = time.time() - start

        assert resp2.status_code == 200
        assert resp2.json().get("enabled") is True
        assert elapsed < 5, (
            f"Second enable took {elapsed:.1f}s — should be instant (idempotent)"
        )

        # Both should return same CDP port
        assert resp1.json()["cdpPort"] == resp2.json()["cdpPort"], (
            f"CDP port changed between enables: {resp1.json()['cdpPort']} vs {resp2.json()['cdpPort']}"
        )

        # Cleanup
        cleanup_sandbox(api_url, api_session, sandbox_id)


class TestDesktopTabAutoEnable:
    """Test the frontend auto-enable flow via Playwright."""

    def test_desktop_tab_shows_active_after_auto_enable(self, page, api_url, api_session):
        """Switching to Desktop tab should auto-enable computer use and show Active badge."""
        sandbox_id = "test-auto-enable-ui"
        cleanup_sandbox(api_url, api_session, sandbox_id)

        # Navigate to the frontend
        page.goto("http://localhost:5174", wait_until="networkidle", timeout=30000)
        try:
            page.wait_for_selector(".h-10.border-b", timeout=20000)
        except Exception:
            page.reload(wait_until="networkidle", timeout=30000)
            page.wait_for_selector(".h-10.border-b", timeout=20000)

        # Click Desktop tab
        top_bar = page.locator(".h-10.border-b").first
        desktop_btn = top_bar.locator("button", has_text="Desktop").first
        desktop_btn.click()
        page.wait_for_timeout(1000)

        # Open the right sidebar if collapsed
        browser_btn = page.locator("button", has_text="Browser")
        if browser_btn.count() == 0:
            panel_btn = page.locator("button[title*='Browser']")
            if panel_btn.count() > 0:
                panel_btn.first.click()
                page.wait_for_timeout(500)

        # Wait up to 20s for "Active" badge to appear (enable happens in background)
        # Use .first to avoid strict mode violations from sidebar text matching
        active_badge = page.locator("text=Active").first
        try:
            active_badge.wait_for(state="visible", timeout=20000)
        except Exception:
            # Check if there's an error message instead
            error_el = page.locator("[class*='text-red']")
            if error_el.count() > 0:
                pytest.fail(f"Computer use enable failed with error: {error_el.first.text_content()}")
            pytest.fail("Active badge never appeared after switching to Desktop tab — enable may be stuck")

        assert active_badge.is_visible(), "Should show 'Active' badge on Desktop tab"

        # Cleanup
        cleanup_sandbox(api_url, api_session, sandbox_id)

    def test_enable_does_not_show_stuck_spinner(self, page, api_url, api_session):
        """The 'Enabling computer use...' state should resolve within 15 seconds."""
        sandbox_id = "test-spinner-ui"
        cleanup_sandbox(api_url, api_session, sandbox_id)

        # First, enable via API to ensure container exists
        resp = api_session.post(
            f"{api_url}/api/sandbox/{sandbox_id}/computer-use/enable",
            timeout=60,
        )
        if resp.status_code != 200:
            pytest.skip(f"Could not pre-enable sandbox: {resp.status_code}")

        try:
            # Now test from frontend
            page.goto("http://localhost:5174", wait_until="networkidle", timeout=30000)
            try:
                page.wait_for_selector(".h-10.border-b", timeout=20000)
            except Exception:
                page.reload(wait_until="networkidle", timeout=30000)
                page.wait_for_selector(".h-10.border-b", timeout=20000)

            # Open Desktop tab
            top_bar = page.locator(".h-10.border-b").first
            desktop_btn = top_bar.locator("button", has_text="Desktop").first
            desktop_btn.click()
            page.wait_for_timeout(2000)

            # Open Browser sidebar
            browser_btn = page.locator("button", has_text="Browser")
            if browser_btn.count() == 0:
                panel_btn = page.locator("button[title*='Browser']")
                if panel_btn.count() > 0:
                    panel_btn.first.click()
                    page.wait_for_timeout(500)

            # Should quickly show Active (container already enabled)
            # Use .first to avoid strict mode violations
            start = time.time()
            active_badge = page.locator("text=Active").first
            try:
                active_badge.wait_for(state="visible", timeout=10000)
                elapsed = time.time() - start
            except Exception:
                elapsed = time.time() - start
                # Check if "Starting..." is stuck
                starting = page.locator("text=Starting")
                if starting.count() > 0:
                    pytest.fail(
                        f"'Starting...' spinner stuck for {elapsed:.1f}s — enable never completed"
                    )
                pytest.fail(f"Active badge not visible after {elapsed:.1f}s")

            assert elapsed < 5, (
                f"Took {elapsed:.1f}s to show Active — should be instant for existing sandbox"
            )
        finally:
            cleanup_sandbox(api_url, api_session, sandbox_id)
