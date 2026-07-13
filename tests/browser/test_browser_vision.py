"""BrowserVisionMiddleware — multimodal image-block attachment for browser tools.

Deterministic, browser-free proofs of the decision logic that turns a string
ToolMessage carrying ``screenshot_path`` into a ``Command`` whose state update
appends BOTH the original text ToolMessage (so the tool_call still gets its
paired string result) AND a companion ``HumanMessage`` carrying the screenshot
as a native image block (the gateway-compatible form: the shipped OpenCode-Zen
gateway rejects image-in-tool but accepts image-in-user). Plus the honest-failure
paths and the env gate. The live "real PNG out of real Chrome, image block
reaches the model" proof lives under ``PUX_E2E=1`` in ``tests/integration/``.
"""
from __future__ import annotations

import base64
import json
from types import SimpleNamespace

import pytest
from langchain_core.messages import HumanMessage, ToolMessage
from langgraph.types import Command

from pux_harness.context.browser_vision import (
    BrowserVisionMiddleware,
    browser_vision_enabled,
)

# A tiny valid 1×1 PNG so the live-decode assertion below is meaningful.
_PNG_B64 = base64.b64encode(
    b"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01"
    b"\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01"
    b"\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82"
).decode()


class _FakeExec:
    """Stand-in for DockerExecClient: returns a canned (out, code) per call."""

    def __init__(self, out: str = _PNG_B64, code: int = 0):
        self.out, self.code = out, code
        self.calls: list[str] = []

    def exec(self, cmd: str):  # noqa: A003 — mirrors DockerExecClient.exec
        self.calls.append(cmd)
        return self.out, self.code


def _req(name: str, *, tc_id: str = "call_7") -> SimpleNamespace:
    return SimpleNamespace(tool_call={"name": name, "id": tc_id, "args": {}})


def _text_tm(payload: dict, *, name="pux_sandbox_browser_click", tc_id="call_7") -> ToolMessage:
    return ToolMessage(
        content=json.dumps(payload), tool_call_id=tc_id, name=name, status="success",
    )


def _handler_returning(tm: ToolMessage):
    def _h(_request):
        return tm
    return _h


def _messages(out) -> list:
    """Pull the appended-message list out of a Command, or [] for passthrough."""
    assert isinstance(out, Command)
    return out.update.get("messages", [])


# --- passthrough: non-browser tools are never touched ------------------------
def test_non_browser_tool_passes_through_untouched():
    mw = BrowserVisionMiddleware(_FakeExec())
    tm = _text_tm({"screenshot_path": "/x.png"}, name="pux_sandbox_python")
    out = mw.wrap_tool_call(_req("pux_sandbox_python"), _handler_returning(tm))
    assert out is tm  # identity — not even rebuilt
    assert out.content == tm.content


def test_disabled_middleware_is_a_pass_through():
    mw = BrowserVisionMiddleware(_FakeExec(), enabled=False)
    tm = _text_tm({"screenshot_path": "/x.png"})
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_click"), _handler_returning(tm))
    assert out is tm


# --- the happy path: screenshot_path → Command([text ToolMessage, image HumanMessage]) ---
def test_browser_result_with_screenshot_emits_command_with_image_human_message():
    exec_client = _FakeExec()
    mw = BrowserVisionMiddleware(exec_client)
    payload = {"ok": True, "url": "https://x", "screenshot_path": "/tmp/shot.png"}
    tm = _text_tm(payload, name="pux_sandbox_browser_navigate")
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_navigate"), _handler_returning(tm))

    msgs = _messages(out)
    assert len(msgs) == 2, f"expected [ToolMessage, HumanMessage], got {len(msgs)}"

    # 1) the text ToolMessage is the ORIGINAL — tool_call_id/name/status intact
    text_tm = msgs[0]
    assert isinstance(text_tm, ToolMessage)
    assert text_tm is tm  # the exact object is preserved
    assert text_tm.content == json.dumps(payload)  # text-only, unchanged
    assert text_tm.tool_call_id == "call_7"
    assert text_tm.name == "pux_sandbox_browser_navigate"
    assert text_tm.status == "success"

    # 2) a companion HumanMessage carries the screenshot as a native image block
    human = msgs[1]
    assert isinstance(human, HumanMessage)
    assert isinstance(human.content, list)
    assert [b["type"] for b in human.content] == ["text", "image"]
    # label names the tool + tool_call_id so the model reads it as the result
    assert "call_7" in human.content[0]["text"]
    assert "pux_sandbox_browser_navigate" in human.content[0]["text"]
    # image block is the canonical deepagents ContentBlock shape
    img = human.content[1]
    assert img["base64"] == _PNG_B64
    assert img["mime_type"] == "image/png"
    # the cap-bound bytes decode to a real PNG header
    assert base64.b64decode(img["base64"])[:8] == b"\x89PNG\r\n\x1a\n"
    # fetched the right file, shell-quoted
    assert any("base64 -w0 /tmp/shot.png" in c for c in exec_client.calls)


def test_mime_is_jpeg_for_jpg_paths():
    mw = BrowserVisionMiddleware(_FakeExec())
    tm = _text_tm({"screenshot_path": "/tmp/shot.JPG"})
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_screenshot"), _handler_returning(tm))
    img = _messages(out)[1].content[1]
    assert img["mime_type"] == "image/jpeg"


# --- honest failure: no screenshot, no path, bad fetch → text unchanged ------
def test_browser_result_without_screenshot_path_is_untouched():
    mw = BrowserVisionMiddleware(_FakeExec())
    tm = _text_tm({"ok": True, "result": "42", "type": "str"})  # /evaluate shape
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_evaluate"), _handler_returning(tm))
    assert out is tm  # evaluate returns a scalar — no screenshot to attach


def test_fetch_failure_leaves_text_result_in_place():
    mw = BrowserVisionMiddleware(_FakeExec(out="", code=1))  # base64 exit 1
    tm = _text_tm({"ok": True, "screenshot_path": "/tmp/shot.png"})
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_click"), _handler_returning(tm))
    assert out is tm  # honest: no image rather than a fake one


def test_non_json_content_is_untouched():
    mw = BrowserVisionMiddleware(_FakeExec())
    tm = ToolMessage(content="type failed: selector not found",
                     tool_call_id="c", name="pux_sandbox_browser_type", status="error")
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_type"), _handler_returning(tm))
    assert out is tm


def test_oversized_screenshot_is_dropped():
    # 6 MiB of base64 → would decode past the 4 MiB cap → no image block.
    mw = BrowserVisionMiddleware(_FakeExec(out="A" * (6 * 1024 * 1024)))
    tm = _text_tm({"ok": True, "screenshot_path": "/tmp/huge.png"})
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_click"), _handler_returning(tm))
    assert out is tm


# --- async path mirrors sync -------------------------------------------------
def test_async_path_emits_command_with_image_human_message():
    import asyncio

    mw = BrowserVisionMiddleware(_FakeExec())
    payload = {"ok": True, "screenshot_path": "/tmp/shot.png"}
    tm = _text_tm(payload)

    async def _h(_request):
        return tm

    out = asyncio.run(mw.awrap_tool_call(_req("pux_sandbox_browser_click"), _h))
    msgs = _messages(out)
    assert len(msgs) == 2
    assert isinstance(msgs[1], HumanMessage)
    assert [b["type"] for b in msgs[1].content] == ["text", "image"]


# --- env gate ----------------------------------------------------------------
def test_browser_vision_enabled_default_on(monkeypatch):
    monkeypatch.delenv("PUX_BROWSER_VISION", raising=False)
    assert browser_vision_enabled() is True


def test_browser_vision_enabled_opt_out(monkeypatch):
    monkeypatch.setenv("PUX_BROWSER_VISION", "0")
    assert browser_vision_enabled() is False


# --- per-action screenshot policy --------------------------------------------
# Only tools whose slug is in _SCREENSHOT_SLUGS get a screenshot; everything
# else returns text-only (the return value / SoM map is ground truth, the
# agent calls browser_screenshot explicitly when it wants to look). This cuts
# ~60% of vision tokens (type/scroll/evaluate/extract are high-frequency).

@pytest.mark.parametrize("slug", [
    "type", "press", "wait", "scroll", "scroll_into_view",
    "evaluate", "extract", "find_text", "upload", "download",
    "save_session", "restore_session", "a11y", "dropdown_options",
    "iframe", "tabs", "close_tab", "save_screenshot", "warmup_history",
    "solve_captcha",
])
def test_text_only_slugs_skip_screenshot(slug):
    """High-frequency non-visual slugs must NOT attach a screenshot — the result
    passes through as the raw text ToolMessage (no Command, no image fetch)."""
    exec_client = _FakeExec()
    mw = BrowserVisionMiddleware(exec_client)
    name = f"pux_sandbox_browser_{slug}"
    tm = _text_tm({"ok": True, "screenshot_path": "/tmp/shot.png"}, name=name)
    out = mw.wrap_tool_call(_req(name), _handler_returning(tm))
    assert out is tm  # passthrough — no enrichment, no fetch
    assert exec_client.calls == []  # zero base64 fetches


@pytest.mark.parametrize("slug", [
    "navigate", "search", "go_back", "new_tab", "switch_tab",
    "click", "click_at", "hover", "drag", "select_dropdown",
    "accept_cookies", "uc", "screenshot",
])
def test_visual_slugs_attach_screenshot(slug):
    """Slugs in the screenshot policy set DO attach the image (Command with the
    companion HumanMessage)."""
    mw = BrowserVisionMiddleware(_FakeExec())
    name = f"pux_sandbox_browser_{slug}"
    tm = _text_tm({"ok": True, "screenshot_path": "/tmp/shot.png"}, name=name)
    out = mw.wrap_tool_call(_req(name), _handler_returning(tm))
    assert isinstance(out, Command)  # enriched
    msgs = out.update["messages"]
    assert len(msgs) == 2
    assert isinstance(msgs[1], HumanMessage)
    assert [b["type"] for b in msgs[1].content] == ["text", "image"]
