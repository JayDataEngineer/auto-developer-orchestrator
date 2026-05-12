"""
Vision verification fixtures for sandbox/desktop/browser tests.

Provides:
  - capture_desktop_screenshot / capture_browser_screenshot
  - observe_desktop — OCR + elements via desktop_observe endpoint
  - describe_with_vision — send screenshot to vision model for description
  - vision_assert — AI-powered visual assertion
  - assert_ocr_text_contains — OCR-based text assertion
  - vision_verifier fixture — convenience object with all methods
"""

import base64
import os

import pytest
import requests

MCP_VISION_URL = os.environ.get("MCP_VISION_URL", "http://100.86.69.57:30080")
LOCAL_MODEL_URL = os.environ.get("LOCAL_MODEL_URL", "http://localhost:8001")
WEBUI_VISUAL_URL = os.environ.get("WEBUI_VISUAL_URL", "http://localhost:9878")
TUI_VISUAL_URL = os.environ.get("TUI_VISUAL_URL", "http://localhost:9877")


def capture_desktop_screenshot(api_url, api_session, sandbox_id, format="json"):
    """Capture a screenshot from X11 desktop. Returns base64 PNG string."""
    resp = api_session.get(
        f"{api_url}/api/sandbox/{sandbox_id}/x11/screenshot",
        params={"format": format}, timeout=30,
    )
    resp.raise_for_status()
    if format == "png":
        return base64.b64encode(resp.content).decode("ascii")
    return resp.json()["image_b64"]


def capture_browser_screenshot(api_url, api_session, sandbox_id):
    """Capture a screenshot from browser (computer-use endpoint). Returns base64."""
    resp = api_session.get(
        f"{api_url}/api/sandbox/{sandbox_id}/computer-use/screenshot",
        params={"describe": "false"}, timeout=30,
    )
    resp.raise_for_status()
    data = resp.json()
    return data.get("image_b64") or data.get("screenshot", "")


def observe_desktop(api_url, api_session, sandbox_id):
    """Call desktop_observe endpoint. Returns dict with image, elements, windows."""
    resp = api_session.get(
        f"{api_url}/api/sandbox/{sandbox_id}/x11/observe", timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def describe_with_vision(image_b64, prompt, model_url=None):
    """
    Send a base64 image to a vision model for description.
    Tries MCP hub first, falls back to local llama-server.
    Returns the model's text response.
    """
    model_url = model_url or MCP_VISION_URL

    payload = {
        "messages": [{
            "role": "user",
            "content": [
                {"type": "text", "text": prompt},
                {"type": "image_url", "image_url": {
                    "url": f"data:image/png;base64,{image_b64}"
                }},
            ],
        }],
        "max_tokens": 500,
    }

    # Try MCP Hub (OpenAI-compatible)
    try:
        resp = requests.post(
            f"{model_url}/v1/chat/completions",
            json={**payload, "model": "qwen3.6-27b-q5_k_s"},
            timeout=30,
        )
        if resp.status_code == 200:
            return resp.json()["choices"][0]["message"]["content"]
    except Exception:
        pass

    # Fallback to local llama-server
    try:
        resp = requests.post(
            f"{LOCAL_MODEL_URL}/v1/chat/completions",
            json=payload,
            timeout=30,
        )
        if resp.status_code == 200:
            return resp.json()["choices"][0]["message"]["content"]
    except Exception:
        pass

    pytest.skip("No vision model available for verification")


def vision_assert(image_b64, assertion_prompt, model_url=None):
    """
    Assert that a visual condition is true using AI vision.
    Raises AssertionError if the model says the condition is not met.
    """
    description = describe_with_vision(image_b64, assertion_prompt, model_url)
    lower = description.lower().strip()
    negatives = ["no", "not visible", "cannot see", "does not show",
                 "is not present", "not found"]
    if any(neg in lower for neg in negatives):
        raise AssertionError(
            f"Vision assertion failed: {assertion_prompt}\n"
            f"Model response: {description}"
        )
    return description


def assert_ocr_text_contains(api_url, api_session, sandbox_id, expected_text):
    """Use desktop_observe to OCR the desktop, assert expected text appears."""
    result = observe_desktop(api_url, api_session, sandbox_id)
    elements_text = " ".join(
        el.get("text", "") for el in result.get("elements", [])
    )
    combined = elements_text.lower()
    assert expected_text.lower() in combined, (
        f"Expected text '{expected_text}' not found in OCR output.\n"
        f"Elements: {elements_text[:500]}"
    )


def assert_ocr_text_not_contains(api_url, api_session, sandbox_id, forbidden_text):
    """Assert that forbidden text does NOT appear in the desktop OCR."""
    result = observe_desktop(api_url, api_session, sandbox_id)
    elements_text = " ".join(
        el.get("text", "") for el in result.get("elements", [])
    )
    combined = elements_text.lower()
    assert forbidden_text.lower() not in combined, (
        f"Forbidden text '{forbidden_text}' found in OCR output.\n"
        f"Elements: {elements_text[:500]}"
    )


# ---------------------------------------------------------------------------
# WebUI visual testing (via webui_visual.py server on :9878)
# ---------------------------------------------------------------------------


def capture_webui_screenshot(visual_url=None):
    """Capture a screenshot from the WebUI visual testing server. Returns base64."""
    visual_url = visual_url or WEBUI_VISUAL_URL
    resp = requests.get(f"{visual_url}/screenshot", timeout=30)
    resp.raise_for_status()
    return base64.b64encode(resp.content).decode("ascii")


def observe_webui(visual_url=None):
    """Call WebUI /observe endpoint. Returns dict with screenshot, text, logs."""
    visual_url = visual_url or WEBUI_VISUAL_URL
    resp = requests.get(f"{visual_url}/observe", timeout=30)
    resp.raise_for_status()
    return resp.json()


def webui_goto(url, visual_url=None):
    """Navigate the WebUI to a URL."""
    visual_url = visual_url or WEBUI_VISUAL_URL
    resp = requests.post(
        f"{visual_url}/goto", json={"url": url}, timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def webui_click(selector=None, x=None, y=None, visual_url=None):
    """Click on the WebUI by selector or coordinates."""
    visual_url = visual_url or WEBUI_VISUAL_URL
    body = {}
    if selector:
        body["selector"] = selector
    if x is not None and y is not None:
        body["x"] = x
        body["y"] = y
    resp = requests.post(f"{visual_url}/click", json=body, timeout=30)
    resp.raise_for_status()
    return resp.json()


def webui_type(text, selector="body", visual_url=None):
    """Type text into a WebUI element."""
    visual_url = visual_url or WEBUI_VISUAL_URL
    resp = requests.post(
        f"{visual_url}/input", json={"text": text, "selector": selector}, timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def webui_press_key(key, visual_url=None):
    """Press a key on the WebUI."""
    visual_url = visual_url or WEBUI_VISUAL_URL
    resp = requests.post(
        f"{visual_url}/key", json={"key": key}, timeout=30,
    )
    resp.raise_for_status()
    return resp.json()


def assert_webui_visible(prompt, visual_url=None):
    """AI-powered visual assertion for the WebUI."""
    img_b64 = capture_webui_screenshot(visual_url)
    return vision_assert(img_b64, prompt)


def assert_webui_text_contains(expected_text, visual_url=None):
    """Assert expected text appears in the WebUI's visible content."""
    result = observe_webui(visual_url)
    screen_text = result.get("screen", "").lower()
    assert expected_text.lower() in screen_text, (
        f"Expected text '{expected_text}' not found in WebUI.\n"
        f"Screen text (first 500 chars): {screen_text[:500]}"
    )


# ---------------------------------------------------------------------------
# TUI visual testing (via tui_visual.py server on :9877)
# ---------------------------------------------------------------------------


def observe_tui(visual_url=None):
    """Call TUI /observe endpoint. Returns dict with screenshot, screen text, logs."""
    visual_url = visual_url or TUI_VISUAL_URL
    resp = requests.get(f"{visual_url}/observe", timeout=30)
    resp.raise_for_status()
    return resp.json()


def assert_tui_visible(prompt, visual_url=None):
    """AI-powered visual assertion for the TUI."""
    visual_url = visual_url or TUI_VISUAL_URL
    resp = requests.get(f"{visual_url}/observe", timeout=30)
    resp.raise_for_status()
    data = resp.json()
    img_b64 = data.get("screenshot_base64")
    if not img_b64:
        pytest.skip("TUI visual server returned no screenshot")
    return vision_assert(img_b64, prompt)


def assert_tui_text_contains(expected_text, visual_url=None):
    """Assert expected text appears in the TUI's screen buffer."""
    visual_url = visual_url or TUI_VISUAL_URL
    resp = requests.get(f"{visual_url}/observe", timeout=30)
    resp.raise_for_status()
    screen_text = resp.json().get("screen", "").lower()
    assert expected_text.lower() in screen_text, (
        f"Expected text '{expected_text}' not found in TUI screen.\n"
        f"Screen (first 500 chars): {screen_text[:500]}"
    )


@pytest.fixture
def vision_verifier(api_url, api_session):
    """
    Fixture providing vision verification methods.

    Usage:
        vision_verifier.assert_visible(sandbox_id, "Is a terminal window visible?")
        vision_verifier.assert_ocr(sandbox_id, "File")
    """
    class VisionVerifier:
        def capture_desktop(self, sandbox_id):
            return capture_desktop_screenshot(api_url, api_session, sandbox_id)

        def capture_browser(self, sandbox_id):
            return capture_browser_screenshot(api_url, api_session, sandbox_id)

        def observe(self, sandbox_id):
            return observe_desktop(api_url, api_session, sandbox_id)

        def assert_visible(self, sandbox_id, prompt, mode="desktop"):
            if mode == "desktop":
                img = self.capture_desktop(sandbox_id)
            else:
                img = self.capture_browser(sandbox_id)
            return vision_assert(img, prompt)

        def assert_ocr(self, sandbox_id, expected_text):
            return assert_ocr_text_contains(
                api_url, api_session, sandbox_id, expected_text
            )

        def assert_not_ocr(self, sandbox_id, forbidden_text):
            return assert_ocr_text_not_contains(
                api_url, api_session, sandbox_id, forbidden_text
            )

        # WebUI methods (use webui_visual.py server)
        def capture_webui(self):
            return capture_webui_screenshot()

        def observe_webui(self):
            return observe_webui()

        def assert_webui_visible(self, prompt):
            return assert_webui_visible(prompt)

        def assert_webui_text(self, expected_text):
            return assert_webui_text_contains(expected_text)

        # TUI methods (use tui_visual.py server)
        def observe_tui(self):
            return observe_tui()

        def assert_tui_visible(self, prompt):
            return assert_tui_visible(prompt)

        def assert_tui_text(self, expected_text):
            return assert_tui_text_contains(expected_text)

    return VisionVerifier()
