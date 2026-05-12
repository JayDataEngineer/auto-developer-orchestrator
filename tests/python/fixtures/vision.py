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

    return VisionVerifier()
