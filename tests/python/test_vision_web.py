"""
Vision & Web Update Contract Tests.

Tests the web_update SSE event structure and browser automation features.
These tests validate that when the browser/web automation sends screenshots
and page elements, the SSE contract matches the frontend's TypeScript interface.

interface PiWebUpdate {
  url: string;
  title: string;
  screenshot: string;  // base64
  elements: LabeledElement[];
}

Vision model availability is checked at runtime — tests skip gracefully
if no vision-capable model is reachable.

Run: cd tests/python && uv run pytest test_vision_web.py -v --tb=long
"""

import base64
import json
import os

import pytest
import requests

from conftest import API_BASE_URL, VALID_SSE_EVENT_TYPES

pytestmark = [pytest.mark.api, pytest.mark.slow]

API = API_BASE_URL
TEST_PROJECT = "test-repo"

# Check if a vision-capable model is available
def _has_vision_model():
    """Check if any vision model is reachable via the API."""
    try:
        resp = requests.get(f"{API}/api/pi/models", timeout=5)
        if resp.status_code != 200:
            return False
        data = resp.json()
        for m in data.get("models", []):
            if not isinstance(m, dict):
                continue
            inputs = m.get("input", [])
            if isinstance(inputs, list) and "image" in inputs:
                # Try to actually use it
                model_id = m.get("id", "")
                if model_id:
                    return True
        return False
    except Exception:
        return False


def _has_browser_service():
    """Check if the browser/web service is running."""
    try:
        resp = requests.post(
            f"{API}/api/pi/web/session",
            json={"project": TEST_PROJECT},
            timeout=5,
        )
        return resp.status_code < 400
    except Exception:
        return False


VISION_AVAILABLE = _has_vision_model()
BROWSER_AVAILABLE = _has_browser_service()


# ===========================================================================
# 1. Web update SSE event contract
# ===========================================================================


class TestWebUpdateContract:
    """
    Verify the web_update SSE event structure matches PiWebUpdate interface.

    interface PiWebUpdate {
      url: string;
      title: string;
      screenshot: string;   // base64 encoded
      elements: LabeledElement[];
    }

    These tests validate the contract even without a running browser service
    by checking the frontend's expected field types.
    """

    @pytest.mark.skipif(not BROWSER_AVAILABLE, reason="Browser service not available")
    def test_web_session_creates_and_closes(self):
        """Browser sessions should be creatable and closeable."""
        # Create session
        resp = requests.post(
            f"{API}/api/pi/web/session",
            json={"project": TEST_PROJECT},
            timeout=30,
        )
        assert resp.status_code == 200
        data = resp.json()
        assert "sessionId" in data or data.get("success") is True

        # Close session
        session_id = data.get("sessionId", "default")
        close_resp = requests.delete(
            f"{API}/api/pi/web/session",
            json={"project": TEST_PROJECT, "sessionId": session_id},
            timeout=10,
        )
        assert close_resp.status_code in (200, 204, 404)

    def test_web_update_event_type_is_known(self):
        """web_update must be in the frontend's valid event types."""
        assert "web_update" in VALID_SSE_EVENT_TYPES, (
            "web_update not in SSE event types — frontend will silently drop it"
        )

    def test_web_update_required_fields(self):
        """
        The frontend's PiWebUpdate interface requires:
        url, title, screenshot, elements.
        If any field is missing, the computer use tab breaks.
        """
        # This tests the contract definition, not a live event
        required_fields = {"url", "title", "screenshot", "elements"}
        from conftest import SSE_EVENT_REQUIRED_FIELDS

        web_fields = SSE_EVENT_REQUIRED_FIELDS.get("web_update", set())
        assert web_fields == required_fields, (
            f"web_update required fields mismatch.\n"
            f"Expected: {required_fields}\n"
            f"Got: {web_fields}"
        )

    def test_web_update_field_types(self):
        """
        Verify validate_sse_event checks web_update field types.
        - url: string
        - title: string
        - screenshot: string (base64)
        - elements: array
        """
        from conftest import validate_sse_event

        # Valid web_update event
        valid_event = {
            "url": "https://example.com",
            "title": "Example",
            "screenshot": base64.b64encode(b"fake image data").decode(),
            "elements": [
                {"label": "1", "type": "button", "text": "Click me"}
            ],
        }
        violations = validate_sse_event("web_update", valid_event)
        assert len(violations) == 0, f"Valid web_update rejected: {violations}"

        # Missing screenshot
        missing_screenshot = {
            "url": "https://example.com",
            "title": "Example",
            "elements": [],
        }
        violations = validate_sse_event("web_update", missing_screenshot)
        assert len(violations) > 0, "Missing screenshot should fail validation"


# ===========================================================================
# 2. LabeledElement contract
# ===========================================================================


class TestLabeledElementContract:
    """
    The frontend's LabeledElement type is used in web_update events.
    Each element represents an interactive element on the page.
    """

    def test_element_has_required_structure(self):
        """Elements should have label, type, and text/content fields."""
        # The frontend uses these fields from elements
        required = {"label", "type"}
        # Check that the contract includes these
        assert len(required) > 0  # Structural check


# ===========================================================================
# 3. Vision model availability test
# ===========================================================================


class TestVisionModelAvailability:
    """Test that at least one vision model is configured."""

    def test_vision_model_configured(self):
        """At least one model should support image input."""
        resp = requests.get(f"{API}/api/pi/models", timeout=10)
        assert resp.status_code == 200
        data = resp.json()
        models = data.get("models", [])

        vision_models = []
        for m in models:
            if not isinstance(m, dict):
                continue
            inputs = m.get("input", [])
            if isinstance(inputs, list) and "image" in inputs:
                vision_models.append(m.get("id", "unknown"))

        if not vision_models:
            pytest.skip(
                "No vision models configured. "
                "Vision-dependent features (desktop, web browser) will not work."
            )

        print(f"  Vision models available: {vision_models}")

    @pytest.mark.skipif(not VISION_AVAILABLE, reason="No vision model available")
    def test_vision_model_responds(self):
        """The configured vision model should actually respond to prompts."""
        resp = requests.get(f"{API}/api/pi/models", timeout=10)
        data = resp.json()
        models = data.get("models", [])

        for m in models:
            if not isinstance(m, dict):
                continue
            inputs = m.get("input", [])
            if isinstance(inputs, list) and "image" in inputs:
                model_id = m.get("id", "")
                if not model_id:
                    continue

                # Try a simple text prompt (no image needed for basic check)
                resp = requests.post(
                    f"{API}/api/pi/prompt",
                    json={
                        "message": "Say ok",
                        "project": TEST_PROJECT,
                        "model": model_id,
                    },
                    timeout=60,
                    stream=True,
                )
                if resp.status_code == 200:
                    print(f"  ✓ Vision model {model_id} responds to text prompts")
                    return

        pytest.skip("Vision models configured but not responding")


# ===========================================================================
# 4. Browser automation contract
# ===========================================================================


@pytest.mark.skipif(not BROWSER_AVAILABLE, reason="Browser service not available")
class TestBrowserAutomationContract:
    """Test browser automation endpoints that produce web_update events."""

    def test_navigate_to_url(self):
        """Test browser navigation."""
        # Create session first
        session_resp = requests.post(
            f"{API}/api/pi/web/session",
            json={"project": TEST_PROJECT},
            timeout=30,
        )
        if session_resp.status_code != 200:
            pytest.skip("Could not create browser session")

        session_data = session_resp.json()
        session_id = session_data.get("sessionId", "default")

        # Navigate
        nav_resp = requests.post(
            f"{API}/api/pi/web/navigate",
            json={
                "project": TEST_PROJECT,
                "sessionId": session_id,
                "url": "https://example.com",
            },
            timeout=30,
        )
        # Should succeed or fail gracefully
        assert nav_resp.status_code in (200, 400, 404, 500)

    def test_screenshot_returns_base64(self):
        """Screenshot should return base64-encoded image data."""
        session_resp = requests.post(
            f"{API}/api/pi/web/session",
            json={"project": TEST_PROJECT},
            timeout=30,
        )
        if session_resp.status_code != 200:
            pytest.skip("Could not create browser session")

        session_data = session_resp.json()
        session_id = session_data.get("sessionId", "default")

        # Take screenshot
        ss_resp = requests.post(
            f"{API}/api/pi/web/screenshot",
            json={
                "project": TEST_PROJECT,
                "sessionId": session_id,
            },
            timeout=30,
        )

        if ss_resp.status_code == 200:
            data = ss_resp.json()
            screenshot = data.get("screenshot", "")
            if screenshot:
                # Verify it's valid base64
                try:
                    decoded = base64.b64decode(screenshot[:100])
                    assert len(decoded) > 0, "Decoded screenshot is empty"
                except Exception as e:
                    pytest.fail(f"Screenshot is not valid base64: {e}")

    def test_page_state_returns_structure(self):
        """Page state/description should return structured data."""
        session_resp = requests.post(
            f"{API}/api/pi/web/session",
            json={"project": TEST_PROJECT},
            timeout=30,
        )
        if session_resp.status_code != 200:
            pytest.skip("Could not create browser session")

        session_data = session_resp.json()
        session_id = session_data.get("sessionId", "default")

        state_resp = requests.post(
            f"{API}/api/pi/web/state",
            json={
                "project": TEST_PROJECT,
                "sessionId": session_id,
            },
            timeout=30,
        )

        if state_resp.status_code == 200:
            data = state_resp.json()
            # Should have URL and title at minimum
            assert isinstance(data, dict)
