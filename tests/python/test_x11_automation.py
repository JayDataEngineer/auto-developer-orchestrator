"""
X11 Desktop Automation integration tests — NO MOCKS.

Tests X11 automation endpoints via xdotool in the sandbox:
  1. Resolution — xdotool getdisplaygeometry
  2. Screenshot (JSON + PNG formats)
  3. Mouse click at coordinates
  4. Keyboard type text
  5. Keyboard press special key
  6. Active window detection

Requires: running Go backend + sandbox container with xdotool installed.
"""

import pytest
import base64
import time
import struct

pytestmark = [pytest.mark.api]

X11_SANDBOX = "test-x11-automation"


def _is_valid_png(data: bytes) -> bool:
    return data[:8] == b'\x89PNG\r\n\x1a\n'


def _ensure_sandbox_ready(api_url, api_session):
    """Ensure sandbox is created and computer use is enabled."""
    # Try enabling computer use (idempotent)
    resp = api_session.post(
        f"{api_url}/api/sandbox/{X11_SANDBOX}/computer-use/enable",
        timeout=120,
    )
    assert resp.status_code == 200, f"Enable failed: {resp.text[:500]}"

    # Wait for background setup to complete (xdotool install etc.)
    deadline = time.time() + 90
    while time.time() < deadline:
        try:
            r = api_session.get(f"{api_url}/api/sandbox/{X11_SANDBOX}/viewer", timeout=5)
            if r.status_code == 200:
                return
        except Exception:
            pass
        time.sleep(2)
    pytest.skip("Background setup did not complete in time")


@pytest.fixture(scope="module", autouse=True)
def setup_x11_sandbox(api_url, api_session):
    """Module-scoped setup — creates sandbox once for all X11 tests."""
    _ensure_sandbox_ready(api_url, api_session)
    yield
    # Cleanup
    try:
        api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/computer-use/disable",
            timeout=10,
        )
    except Exception:
        pass


class TestX11Resolution:
    """GET /api/sandbox/{id}/x11/resolution"""

    def test_returns_width_and_height(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/resolution",
            timeout=15,
        )
        assert resp.status_code == 200, f"Resolution failed: {resp.text}"
        data = resp.json()
        assert "width" in data, f"Missing width: {data}"
        assert "height" in data, f"Missing height: {data}"
        # Should be numeric strings
        w, h = int(data["width"]), int(data["height"])
        assert w > 0, f"Width should be positive: {w}"
        assert h > 0, f"Height should be positive: {h}"

    def test_reasonable_resolution(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/resolution",
            timeout=15,
        )
        data = resp.json()
        w, h = int(data["width"]), int(data["height"])
        # Should be at least 640x480 (typical Xvfb default)
        assert w >= 640, f"Width too small: {w}"
        assert h >= 480, f"Height too small: {h}"


class TestX11Screenshot:
    """GET /api/sandbox/{id}/x11/screenshot"""

    def test_json_format_returns_base64(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/screenshot",
            timeout=30,
        )
        assert resp.status_code == 200, f"Screenshot failed: {resp.text}"
        data = resp.json()
        assert "image" in data, f"Missing image field: {data}"
        # Verify it's valid base64
        png_bytes = base64.b64decode(data["image"])
        assert _is_valid_png(png_bytes), "Screenshot is not valid PNG"

    def test_png_format_returns_raw_image(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/screenshot?format=png",
            timeout=30,
        )
        assert resp.status_code == 200, f"PNG screenshot failed: {resp.text}"
        assert resp.headers.get("content-type", "").startswith("image/"), (
            f"Expected image content-type, got: {resp.headers.get('content-type')}"
        )
        assert _is_valid_png(resp.content), "Raw screenshot is not valid PNG"


class TestX11Mouse:
    """POST /api/sandbox/{id}/x11/mouse"""

    def test_click_at_coordinates(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/mouse",
            json={"action": "click", "x": 100, "y": 100, "button": 1},
            timeout=15,
        )
        assert resp.status_code == 200, f"Click failed: {resp.text}"
        data = resp.json()
        assert data.get("success") is True, f"Click not successful: {data}"

    def test_move_only(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/mouse",
            json={"action": "move", "x": 200, "y": 150},
            timeout=15,
        )
        assert resp.status_code == 200, f"Move failed: {resp.text}"
        data = resp.json()
        assert data.get("success") is True

    def test_invalid_action(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/mouse",
            json={"action": "double-click", "x": 100, "y": 100},
            timeout=15,
        )
        assert resp.status_code == 400, f"Should reject invalid action: {resp.text}"


class TestX11Keyboard:
    """POST /api/sandbox/{id}/x11/keyboard"""

    def test_type_text(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/keyboard",
            json={"action": "type", "text": "hello world"},
            timeout=15,
        )
        assert resp.status_code == 200, f"Type failed: {resp.text}"
        data = resp.json()
        assert data.get("success") is True

    def test_press_key(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/keyboard",
            json={"action": "key", "key": "Return"},
            timeout=15,
        )
        assert resp.status_code == 200, f"Key press failed: {resp.text}"
        data = resp.json()
        assert data.get("success") is True

    def test_key_combo(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/keyboard",
            json={"action": "key", "key": "ctrl+a"},
            timeout=15,
        )
        assert resp.status_code == 200, f"Key combo failed: {resp.text}"

    def test_missing_text_for_type(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/keyboard",
            json={"action": "type"},
            timeout=15,
        )
        assert resp.status_code == 400

    def test_missing_key_for_key_action(self, api_url, api_session):
        resp = api_session.post(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/keyboard",
            json={"action": "key"},
            timeout=15,
        )
        assert resp.status_code == 400


class TestX11ActiveWindow:
    """GET /api/sandbox/{id}/x11/active-window"""

    def test_returns_window_info(self, api_url, api_session):
        resp = api_session.get(
            f"{api_url}/api/sandbox/{X11_SANDBOX}/x11/active-window",
            timeout=15,
        )
        assert resp.status_code == 200, f"Active window failed: {resp.text}"
        data = resp.json()
        assert "windowId" in data, f"Missing windowId: {data}"
        assert "windowName" in data, f"Missing windowName: {data}"
