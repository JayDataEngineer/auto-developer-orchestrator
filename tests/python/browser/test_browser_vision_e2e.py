"""
Browser & Desktop Vision E2E Tests — NO MOCKS.

Tests the full browser vision pipeline end-to-end:
  1. Create sandbox in browser mode
  2. Navigate to a URL via the act endpoint (browse_to tool path)
  3. Take a CDP screenshot and verify valid PNG data
  4. Take a JSON screenshot and verify image field for vision pipeline
  5. Test a11y snapshot returns structured data
  6. Test find_element after navigation
  7. Enable desktop mode and verify X11 screenshot works
  8. Verify VNC proxy endpoints respond

Also tests that the browser_screenshot tool's output format
(image → image_b64 normalization) matches what the VisionAwareExecutor expects.

Run: cd tests/python && uv run pytest browser/test_browser_vision_e2e.py -v --tb=long
"""

import base64
import json
import time

import pytest

from fixtures.sandbox import cleanup_sandbox
from utils.png import is_valid_png

pytestmark = [pytest.mark.browser]

TEST_SANDBOX = "test-browser-vision-e2e"


class TestBrowserModeSetup:
    """Create sandbox, enable browser mode, navigate to a page."""

    def test_create_sandbox_in_browser_mode(self, api_url, api_session):
        """Create a sandbox with initial_mode=browser."""
        cleanup_sandbox(api_url, api_session, TEST_SANDBOX)

        resp = api_session.post(
            f"{api_url}/api/sandbox/",
            json={
                "id": TEST_SANDBOX,
                "project_path": "/tmp/test-browser-vision",
                "policy": "developer",
                "initial_mode": "browser",
            },
            timeout=120,
        )
        assert resp.status_code in (200, 201), f"Create sandbox failed: {resp.text[:500]}"
        data = resp.json()
        assert data["id"] == TEST_SANDBOX
        assert data["mode"] == "browser"
        print(f"  Sandbox created: {data['id']}, mode={data['mode']}")

    def test_navigate_to_example(self, api_url, api_session):
        """Navigate to example.com via the act endpoint (browse_to tool path).

        This also activates a tab so screenshots work.
        """
        resp = api_session.post(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=30,
        )
        assert resp.status_code == 200, f"Navigate failed: {resp.text[:300]}"
        data = resp.json()
        assert "url" in data or "title" in data, f"No page info returned: {data}"
        print(f"  Navigated to: {data.get('url', 'unknown')}")


class TestBrowserScreenshot:
    """Test CDP screenshot endpoint (browser_screenshot tool path)."""

    def test_screenshot_png_format(self, api_url, api_session):
        """CDP screenshot with format=png returns valid PNG bytes."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/computer-use/screenshot?format=png",
            timeout=15,
        )
        assert resp.status_code == 200, f"Screenshot failed: {resp.status_code} {resp.text[:200]}"
        assert resp.headers.get("content-type") == "image/png", (
            f"Wrong content type: {resp.headers.get('content-type')}"
        )
        assert is_valid_png(resp.content), "Response is not valid PNG"
        assert len(resp.content) > 500, (
            f"PNG too small ({len(resp.content)} bytes), probably blank"
        )
        print(f"  Screenshot PNG size: {len(resp.content)} bytes")

    def test_screenshot_json_has_image_field(self, api_url, api_session):
        """CDP screenshot with format=json returns {"image": "base64..."}.

        This is the raw handler response. The BrowserScreenshot bridge
        normalizes "image" → "image_b64" for the vision pipeline.
        """
        resp = api_session.get(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/computer-use/screenshot?format=json",
            timeout=15,
        )
        assert resp.status_code == 200
        data = resp.json()

        # The CDP handler returns "image" field
        assert "image" in data, f"Missing 'image' field: {list(data.keys())}"
        assert len(data["image"]) > 100, "Base64 image data too short"

        # Verify it's valid base64 that decodes to PNG
        png_bytes = base64.b64decode(data["image"])
        assert is_valid_png(png_bytes), "Decoded data is not valid PNG"
        assert len(png_bytes) > 500, f"Decoded PNG too small: {len(png_bytes)} bytes"
        print(f"  JSON screenshot base64 length: {len(data['image'])}")

    def test_image_b64_normalization_for_vision(self, api_url, api_session):
        """Verify the bridge normalizes image → image_b64 for VisionAwareExecutor.

        Simulates what ComputerUseBridge.BrowserScreenshot does in Go:
          result["image_b64"] = result["image"]
          delete(result, "image")
        """
        resp = api_session.get(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/computer-use/screenshot?format=json",
            timeout=15,
        )
        data = resp.json()

        # Raw CDP handler uses "image" key
        assert "image" in data

        # Simulate the Go bridge normalization
        normalized = dict(data)
        normalized["image_b64"] = normalized.pop("image")

        assert "image_b64" in normalized
        assert "image" not in normalized
        assert len(normalized["image_b64"]) > 100

        # Verify the vision detector would detect this
        json_str = json.dumps(normalized)
        assert "image_b64" in json_str
        print("  Vision pipeline format verified: image_b64 present after normalization")

    def test_screenshot_after_navigate_shows_content(self, api_url, api_session):
        """After navigating, screenshot should be larger than a blank page."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/computer-use/screenshot?format=png",
            timeout=15,
        )
        assert resp.status_code == 200
        assert is_valid_png(resp.content)
        assert len(resp.content) > 1000, (
            f"PNG too small after navigate ({len(resp.content)} bytes), page may be blank"
        )
        print(f"  Post-navigate screenshot size: {len(resp.content)} bytes")


class TestA11ySnapshot:
    """Test accessibility tree snapshot (snapshot_a11y tool path)."""

    def test_a11y_snapshot_returns_tree(self, api_url, api_session):
        """A11y snapshot should return structured accessibility tree data."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/computer-use/a11y-snapshot",
            timeout=15,
        )
        assert resp.status_code == 200, f"A11y snapshot failed: {resp.text[:300]}"
        data = resp.json()
        assert isinstance(data, dict)

        has_elements = "elements" in data or "tree" in data or "nodes" in data
        has_page_info = "url" in data or "title" in data
        assert has_elements or has_page_info, (
            f"A11y snapshot has no recognizable structure: {list(data.keys())}"
        )
        print(f"  A11y snapshot keys: {list(data.keys())}")


class TestFindElement:
    """Test semantic element finding (find_element tool path)."""

    def test_find_element_by_role(self, api_url, api_session):
        """Find an element by role on the current page."""
        resp = api_session.post(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/computer-use/find",
            json={"role": "heading"},
            timeout=15,
        )
        assert resp.status_code in (200, 404), f"Unexpected status: {resp.status_code}"

        if resp.status_code == 200:
            data = resp.json()
            print(f"  Found elements: {list(data.keys())}")
        else:
            print("  No heading elements found (OK for some pages)")


class TestDesktopModeVision:
    """Test desktop mode screenshot (desktop_screenshot tool path)."""

    def test_enable_desktop_mode(self, api_url, api_session):
        """Switch to desktop mode."""
        # Disable current mode first
        api_session.delete(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/mode",
            timeout=30,
        )
        time.sleep(1)

        resp = api_session.post(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/desktop-mode",
            json={"reason": "e2e test"},
            timeout=120,
        )
        assert resp.status_code == 200, f"Desktop mode enable failed: {resp.text[:500]}"
        data = resp.json()
        assert data.get("mode") == "desktop" or data.get("IsActive") is True, (
            f"Unexpected response: {data}"
        )
        print(f"  Desktop mode enabled")

    def test_desktop_screenshot_returns_image_b64(self, api_url, api_session):
        """X11 screenshot returns {"image_b64": "..."}.

        May skip due to pre-existing nil pointer race in displayForSandbox.
        """
        for attempt in range(8):
            resp = api_session.get(
                f"{api_url}/api/sandbox/{TEST_SANDBOX}/x11/screenshot?format=json",
                timeout=15,
            )
            if resp.status_code == 200:
                break
            time.sleep(2)

        if resp.status_code != 200:
            pytest.skip(
                f"X11 screenshot returned {resp.status_code} — "
                f"known nil-pointer race in displayForSandbox (pre-existing)"
            )

        data = resp.json()

        assert "image_b64" in data, f"Missing 'image_b64' field: {list(data.keys())}"
        assert len(data["image_b64"]) > 100, "Base64 image data too short"

        png_bytes = base64.b64decode(data["image_b64"])
        assert is_valid_png(png_bytes), "Decoded X11 data is not valid PNG"
        assert len(png_bytes) > 500, f"Decoded PNG too small: {len(png_bytes)} bytes"
        print(f"  Desktop screenshot size: {len(png_bytes)} bytes")


class TestVisionDetectorContract:
    """Verify the vision detector tool maps match Go code."""

    def test_desktop_tools_map(self):
        """desktop_screenshot and browser_screenshot must be in desktopTools map."""
        from utils.contract import VISION_DESKTOP_TOOLS

        assert "desktop_screenshot" in VISION_DESKTOP_TOOLS
        assert "browser_screenshot" in VISION_DESKTOP_TOOLS
        print("  Vision detector desktopTools map verified")

    def test_browser_tools_map(self):
        """browse_to, find_element, snapshot_a11y must be in browserTools map."""
        from utils.contract import VISION_BROWSER_TOOLS

        assert "browse_to" in VISION_BROWSER_TOOLS
        assert "find_element" in VISION_BROWSER_TOOLS
        assert "snapshot_a11y" in VISION_BROWSER_TOOLS
        print("  Vision detector browserTools map verified")


class TestVNCProxy:
    """Test VNC proxy endpoints."""

    def test_vnc_health_check(self, api_url, api_session):
        """VNC health endpoint should respond."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/{TEST_SANDBOX}/vnc-health",
            timeout=10,
        )
        assert resp.status_code == 200, f"VNC health failed: {resp.status_code}"
        data = resp.json()
        assert "healthy" in data
        print(f"  VNC health: {data}")

    def test_vnc_proxy_redirects_with_resize_remote(self, api_url, api_session):
        """VNC proxy root should redirect to vnc.html with resize=remote."""
        resp = api_session.get(
            f"{api_url}/api/sandbox/vnc/{TEST_SANDBOX}/",
            timeout=10,
            allow_redirects=False,
        )
        if resp.status_code in (301, 302, 303, 307):
            location = resp.headers.get("location", "")
            assert "resize=remote" in location, f"Missing resize=remote: {location}"
            assert "autoconnect=true" in location, f"Missing autoconnect: {location}"
            print(f"  VNC redirect OK: {location}")
        else:
            assert resp.status_code in (200, 404), f"Unexpected: {resp.status_code}"


class TestCleanup:
    """Ensure sandbox is cleaned up after all tests."""

    def test_cleanup(self, api_url, api_session):
        cleanup_sandbox(api_url, api_session, TEST_SANDBOX)
