"""
Desktop Observe + OCR Integration Tests.

Tests the desktop_observe endpoint which captures a screenshot with OCR
element detection, window enumeration, and interactive element labeling.

Requires: running Go backend + sandbox container with Xvfb + tesseract.
"""

import base64
import time

import pytest

from fixtures.sandbox import cleanup_sandbox, wait_for_desktop_ready
from fixtures.vision import (
    assert_ocr_text_contains,
    capture_desktop_screenshot,
    observe_desktop,
    vision_assert,
)
from utils.png import is_valid_png

pytestmark = [pytest.mark.desktop, pytest.mark.slow]

OBSERVE_SANDBOX = "test-desktop-observe"


@pytest.fixture(scope="module", autouse=True)
def setup_sandbox(api_url, api_session):
    """Enable desktop mode before tests, cleanup after."""
    cleanup_sandbox(api_url, api_session, OBSERVE_SANDBOX)
    wait_for_desktop_ready(api_url, api_session, OBSERVE_SANDBOX, timeout=90)
    yield
    cleanup_sandbox(api_url, api_session, OBSERVE_SANDBOX)


class TestDesktopObserveEndpoint:
    """Tests for GET /api/sandbox/{id}/x11/observe."""

    def test_observe_returns_valid_structure(self, api_url, api_session):
        result = observe_desktop(api_url, api_session, OBSERVE_SANDBOX)
        assert "image_b64" in result, "observe must return image_b64"
        assert "elements" in result, "observe must return elements list"
        assert "windows" in result, "observe must return windows list"
        assert "resolution" in result, "observe must return resolution"
        assert isinstance(result["elements"], list)
        assert isinstance(result["windows"], list)

    def test_elements_have_required_fields(self, api_url, api_session):
        result = observe_desktop(api_url, api_session, OBSERVE_SANDBOX)
        for el in result["elements"]:
            for field in ("id", "text", "x", "y", "w", "h", "cx", "cy"):
                assert field in el, f"Element missing required field: {field}"

    def test_image_is_valid_png(self, api_url, api_session):
        result = observe_desktop(api_url, api_session, OBSERVE_SANDBOX)
        png_bytes = base64.b64decode(result["image_b64"])
        assert is_valid_png(png_bytes), "Observe image is not valid PNG"

    def test_resolution_present(self, api_url, api_session):
        result = observe_desktop(api_url, api_session, OBSERVE_SANDBOX)
        res = result["resolution"]
        assert "width" in res and "height" in res, f"Missing resolution fields: {res}"
        assert res["width"] > 0 and res["height"] > 0

    def test_ocr_available_flag(self, api_url, api_session):
        """ocr_available should be a boolean."""
        result = observe_desktop(api_url, api_session, OBSERVE_SANDBOX)
        assert "ocr_available" in result
        assert isinstance(result["ocr_available"], bool)


class TestDesktopOCR:
    """Tests for OCR-based verification of desktop state."""

    def test_ocr_detects_desktop_elements(self, api_url, api_session):
        """After desktop mode starts, OCR should detect some text."""
        result = observe_desktop(api_url, api_session, OBSERVE_SANDBOX)
        elements = result.get("elements", [])
        ocr_available = result.get("ocr_available", False)
        if not ocr_available:
            pytest.skip("Tesseract not available in sandbox")
        # Desktop should have some elements (window titles, taskbar, etc.)
        assert len(elements) > 0, (
            "OCR available but no elements detected on desktop"
        )

    def test_element_coordinates_are_reasonable(self, api_url, api_session):
        """Element coordinates should be within screen bounds."""
        result = observe_desktop(api_url, api_session, OBSERVE_SANDBOX)
        if not result.get("ocr_available", False):
            pytest.skip("Tesseract not available")
        res = result["resolution"]
        max_x = res["width"]
        max_y = res["height"]
        for el in result["elements"]:
            assert el["cx"] >= 0 and el["cx"] <= max_x, (
                f"Element cx={el['cx']} out of bounds [0, {max_x}]"
            )
            assert el["cy"] >= 0 and el["cy"] <= max_y, (
                f"Element cy={el['cy']} out of bounds [0, {max_y}]"
            )


class TestDesktopObserveVision:
    """Vision verification of desktop state."""

    def test_vision_confirms_desktop(self, api_url, api_session):
        """Vision model should confirm a desktop environment is visible."""
        img_b64 = capture_desktop_screenshot(api_url, api_session, OBSERVE_SANDBOX)
        vision_assert(
            img_b64,
            "Is this a screenshot showing a desktop environment with a "
            "graphical user interface? Answer yes or no and describe briefly.",
        )

    def test_ocr_detects_opened_terminal(self, api_url, api_session):
        """Open a terminal, then verify OCR detects it."""
        if not observe_desktop(api_url, api_session, OBSERVE_SANDBOX).get(
            "ocr_available", False
        ):
            pytest.skip("Tesseract not available")

        # Open a terminal via xdotool
        api_session.post(
            f"{api_url}/api/sandbox/{OBSERVE_SANDBOX}/x11/keyboard",
            json={"action": "key", "key": "ctrl+alt+t"},
            timeout=10,
        )
        time.sleep(3)

        assert_ocr_text_contains(
            api_url, api_session, OBSERVE_SANDBOX, "terminal"
        )
