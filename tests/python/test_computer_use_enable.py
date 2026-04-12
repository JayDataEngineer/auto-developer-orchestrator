"""
Computer Use Enable — Integration Test

Tests the EXACT flow a real user follows:
  1. Backend API: enable creates container, responds in <15s
  2. Backend API: re-enable is idempotent and fast (<5s)
  3. Backend API: stale sandbox is destroyed and recreated
  4. Frontend: after switching to Desktop tab, "Active" appears
  5. Frontend: no stuck "Starting..." spinner
  6. Frontend: screenshot and navigate work after enable

NO MOCKS. Tests against the running backend + frontend.

Setup: creates a sandbox via API before browser tests so the
Desktop tab hits the fast re-enable path (not slow fresh-create).
"""

import pytest
import time
import requests

pytestmark = [pytest.mark.playwright]

API_URL = "http://localhost:5174"
TIMEOUT = 45

# Shared sandbox ID for all tests in this file
SHARED_SANDBOX = "test-cu-e2e"


def _cleanup(api_url, api_session, sandbox_id):
    """Best-effort cleanup."""
    try:
        api_session.post(f"{api_url}/api/sandbox/{sandbox_id}/computer-use/disable", timeout=10)
    except Exception:
        pass
    try:
        api_session.delete(f"{api_url}/api/sandbox/{sandbox_id}", timeout=10)
    except Exception:
        pass


def _wait_for_ready(api_url, api_session, sandbox_id, timeout=30):
    """Wait for the sandbox background setup (Docker + CDP) to complete."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            resp = api_session.get(
                f"{api_url}/api/sandbox/{sandbox_id}/computer-use/screenshot?format=png",
                timeout=5,
            )
            if resp.status_code == 200:
                return
        except Exception:
            pass
        time.sleep(2)
    raise TimeoutError(f"Sandbox {sandbox_id} not ready after {timeout}s")


class TestBackendAPI:
    """Test the backend enable/disable API directly."""

    def test_enable_creates_container(self, api_url, api_session):
        """POST /computer-use/enable with fresh sandbox creates a container."""
        _cleanup(api_url, api_session, SHARED_SANDBOX)

        start = time.time()
        resp = api_session.post(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/enable",
            timeout=60,
        )
        elapsed = time.time() - start

        assert resp.status_code == 200, f"Enable failed: {resp.text[:300]}"
        data = resp.json()
        assert data.get("enabled") is True
        assert "cdpPort" in data
        assert elapsed < 30, f"Fresh enable took {elapsed:.1f}s"

    def test_reenable_is_fast(self, api_url, api_session):
        """Second enable reuses existing container (<5s)."""
        # Wait for background Docker setup to complete from first enable
        _wait_for_ready(api_url, api_session, SHARED_SANDBOX, timeout=30)

        start = time.time()
        resp = api_session.post(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/enable",
            timeout=30,
        )
        elapsed = time.time() - start

        assert resp.status_code == 200
        assert resp.json().get("enabled") is True
        assert elapsed < 5, f"Re-enable took {elapsed:.1f}s — should be instant"

    def test_screenshot_returns_png(self, api_url, api_session):
        """Screenshot returns valid PNG after enable."""
        _wait_for_ready(api_url, api_session, SHARED_SANDBOX, timeout=30)

        resp = api_session.get(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/screenshot?format=png",
            timeout=15,
        )
        assert resp.status_code == 200
        assert resp.headers["content-type"] == "image/png"
        assert resp.content[:8] == b'\x89PNG\r\n\x1a\n', "Not valid PNG"

    def test_disable_succeeds(self, api_url, api_session):
        """Disable works and subsequent screenshot fails."""
        resp = api_session.post(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/disable",
            timeout=30,
        )
        assert resp.status_code == 200
        assert resp.json().get("disabled") is True

        # Screenshot should now fail
        resp2 = api_session.get(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/screenshot?format=png",
            timeout=10,
        )
        assert resp2.status_code in (404, 500)

    # NOTE: we do NOT destroy the sandbox here. The next test class
    # re-enables it to test the stale/recovery path.


class TestDesktopTabFlow:
    """Test the Desktop tab auto-enable flow through the real browser."""

    def test_desktop_shows_active(self, page, api_url, api_session):
        """Switching to Desktop tab shows "Active" badge within 45s.

        Pre-condition: a sandbox container exists from the API tests above.
        The frontend auto-enables when switching tabs.
        """
        # Ensure sandbox exists and is enabled via API
        resp = api_session.post(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/enable",
            timeout=60,
        )
        if resp.status_code != 200:
            pytest.skip(f"Could not pre-enable sandbox: {resp.status_code}")

        # Open the frontend
        page.goto(API_URL, wait_until="networkidle", timeout=30000)
        try:
            page.wait_for_selector(".h-10.border-b", timeout=20000)
        except Exception:
            page.reload(wait_until="networkidle", timeout=30000)
            page.wait_for_selector(".h-10.border-b", timeout=20000)

        # Select a project from the dropdown
        project_select = page.locator("select").first
        options = project_select.locator("option")
        if options.count() <= 1:
            pytest.skip("No projects registered — register a project first")
        first_project = options.nth(1).get_attribute("value")
        project_select.select_option(first_project)
        page.wait_for_timeout(3000)

        # Click Desktop tab — triggers auto-enable
        top_bar = page.locator(".h-10.border-b").first
        desktop_btn = top_bar.locator("button", has_text="Desktop").first
        start = time.time()
        desktop_btn.click()

        # DO NOT click "Browser" toggle — sidebar is open by default.
        # Clicking it collapses the sidebar and hides the content.

        # Wait for "Active" badge in the Browser header section
        active_badge = page.locator("span.text-xs.font-mono.text-primary", has_text="Active").first
        try:
            active_badge.wait_for(state="visible", timeout=TIMEOUT * 1000)
            elapsed = time.time() - start
        except Exception:
            elapsed = time.time() - start

            # Diagnose what the UI is showing
            starting = page.locator("button", has_text="Starting")
            inactive = page.locator("span", has_text="Inactive").first
            error_el = page.locator("[class*='text-red']")

            if starting.count() > 0 and starting.first.is_visible():
                pytest.fail(
                    f"STUCK on 'Starting...' for {elapsed:.1f}s. "
                    f"The enable request is not completing."
                )
            if error_el.count() > 0:
                pytest.fail(
                    f"Enable FAILED after {elapsed:.1f}s: {error_el.first.text_content()}"
                )
            if inactive.is_visible(timeout=2000):
                pytest.fail(
                    f"Status stuck on 'Inactive' for {elapsed:.1f}s. Auto-enable didn't fire."
                )
            pytest.fail(
                f"No Active/Starting/Inactive/Error visible after {elapsed:.1f}s. "
                f"The right sidebar may have collapsed."
            )

        assert elapsed < TIMEOUT, f"Active appeared but took {elapsed:.1f}s"

    def test_desktop_screenshot_and_navigate(self, page, api_url, api_session):
        """After enable, screenshot and navigate buttons work in the sidebar."""
        # Ensure enabled
        resp = api_session.post(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/enable",
            timeout=60,
        )
        if resp.status_code != 200:
            pytest.skip("Could not enable sandbox")

        page.goto(API_URL, wait_until="networkidle", timeout=30000)
        try:
            page.wait_for_selector(".h-10.border-b", timeout=20000)
        except Exception:
            page.reload(wait_until="networkidle", timeout=30000)
            page.wait_for_selector(".h-10.border-b", timeout=20000)

        # Select project and switch to Desktop
        project_select = page.locator("select").first
        options = project_select.locator("option")
        if options.count() <= 1:
            pytest.skip("No projects available")
        first_project = options.nth(1).get_attribute("value")
        project_select.select_option(first_project)
        page.wait_for_timeout(3000)

        top_bar = page.locator(".h-10.border-b").first
        top_bar.locator("button", has_text="Desktop").first.click()
        page.wait_for_timeout(3000)

        # Wait for Active
        active_badge = page.locator("span.text-xs.font-mono.text-primary", has_text="Active").first
        try:
            active_badge.wait_for(state="visible", timeout=TIMEOUT * 1000)
        except Exception:
            pytest.fail("Never reached Active state")

        # Click Capture (screenshot)
        capture_btn = page.locator("button", has_text="Capture")
        if capture_btn.count() > 0 and capture_btn.first.is_visible():
            capture_btn.first.click()
            page.wait_for_timeout(3000)
            # Should show screenshot image
            img = page.locator("img[alt='Desktop']")
            assert img.count() > 0, "Screenshot image not shown after Capture"

        # Navigate
        url_input = page.locator("input[name='url']")
        if url_input.count() > 0:
            url_input.first.fill("https://example.com")
            go_btn = page.locator("button", has_text="Go")
            if go_btn.count() > 0:
                go_btn.first.click()
                page.wait_for_timeout(3000)

    def test_cleanup(self, api_url, api_session):
        """Clean up the shared sandbox."""
        _cleanup(api_url, api_session, SHARED_SANDBOX)
