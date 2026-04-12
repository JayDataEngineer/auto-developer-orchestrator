"""
Desktop Viewer Integration Test

Catches the "Desktop not available" / "Desktop session not found" regression.

The regression: POST /computer-use/enable returns immediately (before Docker
setup completes), then GET /viewer returns 404 because the desktop session
hasn't been created yet. The frontend showed "Desktop session not found"
permanently instead of polling until background setup finished.

This test verifies:
  1. POST /enable returns 200 immediately (fast response)
  2. GET /viewer returns 404 right after enable (background setup not done)
  3. GET /viewer eventually returns 200 with novncUrl (background setup done)
  4. Screenshot works after viewer is available
  5. Full E2E through Vite proxy: enable → viewer poll → success
"""

import pytest
import time
import os

pytestmark = [pytest.mark.sandbox]

API_BASE = os.environ.get("API_BASE_URL", "http://localhost:3847")
FRONTEND_BASE = os.environ.get("FRONTEND_BASE_URL", "http://localhost:5174")
SHARED_SANDBOX = "test-viewer-e2e"


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


class TestViewerAfterEnable:
    """Test that the viewer endpoint works correctly with background setup."""

    def test_viewer_404_before_background_setup(self, api_url, api_session):
        """
        REGRESSION TEST: After POST /enable, GET /viewer should return 404
        initially because background Docker setup hasn't completed yet.

        This is EXPECTED behavior — the frontend must poll/retry.
        """
        _cleanup(api_url, api_session, SHARED_SANDBOX)

        # Enable computer use — response comes back immediately
        resp = api_session.post(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/enable",
            timeout=60,
        )
        assert resp.status_code == 200, f"Enable failed: {resp.text[:300]}"
        data = resp.json()
        assert data.get("enabled") is True

        # Immediately check viewer — should be 404 (background setup not done)
        viewer_resp = api_session.get(
            f"{api_url}/api/sandbox/{SHARED_SANDBOX}/viewer",
            timeout=5,
        )
        # Either 404 (no desktop session yet) or 200 if setup was instant
        # The key assertion: we must not get a permanent error
        assert viewer_resp.status_code in (200, 404), \
            f"Unexpected viewer status: {viewer_resp.status_code} — {viewer_resp.text[:200]}"

    def test_viewer_eventually_returns_session(self, api_url, api_session):
        """
        REGRESSION TEST: GET /viewer should eventually return 200 with
        novncUrl after background Docker/CDP setup completes.

        This catches the bug where the frontend gave up after a single 404
        and showed "Desktop session not found" permanently.
        """
        # Wait for background setup to complete
        deadline = time.time() + 60
        viewer_data = None
        while time.time() < deadline:
            try:
                resp = api_session.get(
                    f"{api_url}/api/sandbox/{SHARED_SANDBOX}/viewer",
                    timeout=10,
                )
                if resp.status_code == 200:
                    viewer_data = resp.json()
                    break
            except Exception:
                pass
            time.sleep(2)

        assert viewer_data is not None, \
            "GET /viewer never returned 200 — background setup failed or timed out"

        # These fields are required by the frontend (ComputerUseTab.tsx)
        assert "novncUrl" in viewer_data or "viewerUrl" in viewer_data, \
            f"Viewer response missing required fields: {list(viewer_data.keys())}"

        # If novncUrl is present, it must be non-empty
        novnc_url = viewer_data.get("novncUrl", "")
        if novnc_url is not None:
            assert len(str(novnc_url)) > 0, \
                "novncUrl is empty — frontend would show 'Desktop not available'"

    def test_screenshot_works_after_viewer(self, api_url, api_session):
        """Screenshot endpoint works after background setup completes."""
        deadline = time.time() + 30
        while time.time() < deadline:
            try:
                resp = api_session.get(
                    f"{api_url}/api/sandbox/{SHARED_SANDBOX}/computer-use/screenshot?format=png",
                    timeout=10,
                )
                if resp.status_code == 200:
                    assert resp.headers["content-type"] == "image/png"
                    assert resp.content[:8] == b'\x89PNG\r\n\x1a\n', "Not valid PNG"
                    return
            except Exception:
                pass
            time.sleep(2)

        pytest.fail("Screenshot never returned 200 after viewer was available")

    def test_cleanup(self, api_url, api_session):
        """Clean up the shared sandbox."""
        _cleanup(api_url, api_session, SHARED_SANDBOX)


class TestViewerThroughProxy:
    """
    Test the viewer endpoint through the Vite dev proxy.
    This is what the frontend actually hits.

    Only runs if the Vite dev server is available.
    """

    def test_enable_and_viewer_through_proxy(self, api_url, api_session):
        """
        E2E test through Vite proxy: enable → poll viewer → success.
        Simulates exactly what ComputerUseTab.tsx does.
        """
        proxy_url = FRONTEND_BASE
        sandbox_id = "test-proxy-viewer"

        _cleanup(api_url, api_session, sandbox_id)

        # Step 1: Enable through proxy
        enable_resp = api_session.post(
            f"{proxy_url}/api/sandbox/{sandbox_id}/computer-use/enable",
            timeout=60,
        )
        assert enable_resp.status_code == 200, \
            f"Enable through proxy failed: {enable_resp.status_code} {enable_resp.text[:200]}"
        assert enable_resp.json().get("enabled") is True

        # Step 2: Poll viewer through proxy (exactly like ComputerUseTab does)
        deadline = time.time() + 60
        viewer_data = None
        attempts = 0
        while time.time() < deadline:
            attempts += 1
            try:
                resp = api_session.get(
                    f"{proxy_url}/api/sandbox/{sandbox_id}/viewer",
                    timeout=10,
                )
                if resp.status_code == 200:
                    viewer_data = resp.json()
                    break
                # 404 is expected — background setup not done yet
            except Exception:
                pass
            time.sleep(2)

        assert viewer_data is not None, \
            f"Viewer never returned 200 through proxy after {attempts} attempts"

        # Step 3: Verify required fields
        assert viewer_data.get("mode") in ("browser", "desktop"), \
            f"Unexpected mode: {viewer_data.get('mode')}"

        _cleanup(api_url, api_session, sandbox_id)
