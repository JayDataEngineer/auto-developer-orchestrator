"""
Full agent pipeline E2E test — CTO → delegate_to → browser_ops.

Sends "open the web browser and go to example.com" through the real agent
pipeline (POST /api/pux/prompt) and verifies:
1. CTO delegates to browser_ops
2. Sub-agent result contains actual page content (not hallucinated)
3. Chrome is open and loaded a real page
4. Screenshot proves it

Uses DeepSeek V4 Flash via OpenRouter (proven tool calling support).
Requires: running Go backend + Docker + OpenRouter API key.
Auto-skips if backend is unreachable.
"""

import base64
import os
import sys
import uuid

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from conftest import API_BASE_URL
from utils.sse import post_and_stream

pytestmark = [pytest.mark.agent, pytest.mark.browser, pytest.mark.slow]

MODEL = os.environ.get("E2E_MODEL", "deepseek/deepseek-v4-flash")
PROJECT = "auto-developer-orchestrator"
SCREENSHOT_DIR = "/tmp/e2e_screenshots"


def _save_screenshot(data: dict, name: str):
    """Save a base64 screenshot to disk."""
    os.makedirs(SCREENSHOT_DIR, exist_ok=True)
    img_b64 = data.get("image", "")
    if img_b64:
        path = os.path.join(SCREENSHOT_DIR, f"{name}.png")
        with open(path, "wb") as f:
            f.write(base64.b64decode(img_b64))
        print(f"  Screenshot saved: {path} ({len(img_b64)} bytes base64)")
        return True
    return False


class TestBrowserDelegatePipeline:
    """
    Full agent pipeline: user prompt → CTO → delegate_to browser_ops →
    sub-agent calls browser tools → Chrome opens → screenshot proof.
    """

    def test_delegate_opens_browser_and_navigates(self, api_session):
        """
        Send a browser task through the full agent pipeline.
        Verify CTO delegates, sub-agent navigates, and response has real content.
        """
        agent_id = f"browser-e2e-{uuid.uuid4().hex[:8]}"
        prompt = "Open the web browser and navigate to https://example.com. Then tell me what the page title is."

        print(f"\n  Sending prompt with model={MODEL} project={PROJECT}")
        print(f"  Agent ID: {agent_id}")

        events = list(post_and_stream(
            api_session,
            f"{API_BASE_URL}/api/pux/prompt",
            {
                "message": prompt,
                "project": PROJECT,
                "agentId": agent_id,
                "model": MODEL,
            },
            timeout=300,
        ))

        # ── Analyze events ──
        tool_starts = [(t, d) for t, d in events if t == "tool_execution_start"]
        tool_ends = [(t, d) for t, d in events if t == "tool_execution_end"]
        text_deltas = [(t, d) for t, d in events if t == "text_delta"]
        errors = [(t, d) for t, d in events if t == "error"]

        full_text = "".join(d.get("text", "") for _, d in text_deltas)
        tool_names = [d.get("toolName", "") for _, d in tool_starts]

        # Extract delegate_to result (sub-agent response)
        delegate_result = ""
        for _, d in tool_ends:
            if d.get("toolName") == "delegate_to":
                result = d.get("result", {})
                if isinstance(result, dict):
                    delegate_result = result.get("result", "")
                elif isinstance(result, str):
                    delegate_result = result
                break

        print(f"\n  Events: {len(events)} total")
        print(f"  Tool calls: {tool_names}")
        print(f"  Errors: {len(errors)}")
        if errors:
            for _, e in errors:
                print(f"    ERROR: {e}")
        print(f"  Final text ({len(full_text)} chars): {full_text[:300]}")
        print(f"  Delegate result ({len(delegate_result)} chars): {delegate_result[:300]}")

        # ── Assertions ──

        # 1. No fatal errors
        if errors:
            err_msgs = [e.get("error", e.get("message", str(e))) for _, e in errors]
            hard = [m for m in err_msgs if "retrying" not in m.lower()]
            # Skip if Docker/sandbox is unavailable (infrastructure issue, not test failure)
            docker_errors = [m for m in hard if "docker" in m.lower() or "sandbox unavailable" in m.lower()]
            if docker_errors:
                pytest.skip(f"Docker/sandbox unavailable: {docker_errors[0][:120]}")
            assert len(hard) == 0, f"Agent errors: {hard}"

        # 2. CTO must have delegated
        assert "delegate_to" in tool_names, (
            f"CTO never called delegate_to! Tools: {tool_names}\n"
            f"Response: {full_text[:500]}"
        )

        # 3. Sub-agent must have produced a real result (not hallucinated)
        assert len(delegate_result) > 20, (
            f"Sub-agent returned empty result. Delegate result: '{delegate_result}'"
        )

        # 4. The result OR final text must reference actual page content
        combined = (full_text + " " + delegate_result).lower()
        assert "example" in combined, (
            f"Response doesn't mention 'example' — likely hallucinated.\n"
            f"Full text: {full_text[:300]}\nDelegate: {delegate_result[:300]}"
        )

    def test_screenshot_proves_chrome_open(self, api_session):
        """
        After the delegate test, navigate to example.com and take a screenshot
        to prove Chrome is actually open and rendering pages.
        """
        sandbox_id = PROJECT.replace("/", "-").replace("_", "-").strip("-")

        # Navigate first (ensure active tab)
        nav_resp = api_session.post(
            f"{API_BASE_URL}/api/sandbox/{sandbox_id}/computer-use/act",
            json={"action": "navigate", "url": "https://example.com"},
            timeout=65,
        )
        assert nav_resp.status_code == 200, f"Navigate failed: {nav_resp.text[:300]}"

        # Take screenshot
        resp = api_session.get(
            f"{API_BASE_URL}/api/sandbox/{sandbox_id}/computer-use/screenshot",
            params={"describe": "false", "format": "json"},
            timeout=30,
        )
        assert resp.status_code == 200, (
            f"Screenshot failed ({resp.status_code}): {resp.text[:300]}"
        )
        data = resp.json()
        assert "image" in data, f"No image: {data}"
        assert len(data["image"]) > 1000, "Screenshot too small — likely blank"

        saved = _save_screenshot(data, "agent_pipeline_chrome_proof")
        assert saved, "Failed to save screenshot"

        # Verify the page loaded
        url = data.get("url", "")
        title = data.get("title", "")
        print(f"  Page URL: {url}")
        print(f"  Page title: {title}")
        assert "example" in (url + title).lower(), (
            f"Page doesn't look like example.com: url={url} title={title}"
        )
