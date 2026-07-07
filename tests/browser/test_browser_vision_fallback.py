"""BrowserVisionMiddleware — the NON-multimodal-driver fallback (text pointer).

Companion to ``test_browser_vision.py`` (which proves the multimodal image-block
mode). When the driving model is text-only (the shipped DEFAULT tier's glm-5.2
supervisor), the middleware must NOT attach an image block the driver can't
read — instead it emits a TEXT POINTER telling the driver to call
``describe_image(image_path=<path>, prompt=...)``, which routes to the
multimodal role. Vision is delegated, never silently dropped.

Deterministic + fetch-free: the text-pointer mode never reads the container
(the image stays in-container; ``describe_image`` re-reads it on demand), so a
spying exec client proves NO fetch happened.
"""
from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

from langchain_core.messages import HumanMessage, ToolMessage
from langgraph.types import Command

from pux_harness.context.browser_vision import BrowserVisionMiddleware


class _SpyExec:
    """Stand-in for DockerExecClient that RECORDS every call — the text-pointer
    mode must issue ZERO exec calls (no base64 fetch)."""

    def __init__(self):
        self.calls: list[str] = []

    def exec(self, cmd: str):  # noqa: A003 — mirrors DockerExecClient.exec
        self.calls.append(cmd)
        raise AssertionError(
            f"text-pointer mode must not fetch the image, but exec was called: {cmd!r}"
        )


def _req(name: str) -> SimpleNamespace:
    return SimpleNamespace(tool_call={"name": name, "id": "call_9", "args": {}})


def _text_tm(payload: dict, *, name="pux_sandbox_browser_click") -> ToolMessage:
    return ToolMessage(
        content=json.dumps(payload), tool_call_id="call_9", name=name, status="success",
    )


def _handler_returning(tm: ToolMessage):
    def _h(_request):
        return tm
    return _h


def _messages(out) -> list:
    assert isinstance(out, Command)
    return out.update.get("messages", [])


# --- the fallback: text-pointer HumanMessage, NO image block, NO fetch -------

def test_text_pointer_mode_emits_describe_image_pointer_not_an_image_block():
    exec_client = _SpyExec()
    mw = BrowserVisionMiddleware(exec_client, multimodal_driver=False)
    path = "/tmp/sandbox/shot.png"
    payload = {"ok": True, "url": "https://x", "screenshot_path": path}
    tm = _text_tm(payload, name="pux_sandbox_browser_navigate")
    out = mw.wrap_tool_call(
        _req("pux_sandbox_browser_navigate"), _handler_returning(tm),
    )

    msgs = _messages(out)
    assert len(msgs) == 2, f"expected [ToolMessage, HumanMessage], got {len(msgs)}"

    # 1) the text ToolMessage is the ORIGINAL, unchanged.
    text_tm = msgs[0]
    assert text_tm is tm
    assert text_tm.content == json.dumps(payload)

    # 2) the companion is a TEXT-ONLY HumanMessage — no image block at all.
    human = msgs[1]
    assert isinstance(human, HumanMessage)
    assert isinstance(human.content, list)
    assert [b["type"] for b in human.content] == ["text"], (
        f"text-pointer mode must emit text only, got {human.content}"
    )
    body = human.content[0]["text"]
    # names the tool_call (so the model reads it as "the screenshot that tool
    # produced"), the in-container path, and the describe_image delegation.
    assert "call_9" in body
    assert "pux_sandbox_browser_navigate" in body
    assert path in body
    assert "describe_image" in body
    assert "image_path" in body
    # the path appears quoted as a valid call argument
    assert repr(path) in body
    # NO base64 fetch happened — the image never left the container.
    assert exec_client.calls == []


def test_text_pointer_mode_works_for_every_browser_tool():
    """The fallback is data-driven (any browser tool's screenshot_path), not
    per-tool — prove it for a second browser tool."""
    mw = BrowserVisionMiddleware(_SpyExec(), multimodal_driver=False)
    tm = _text_tm({"screenshot_path": "/tmp/x.png"}, name="pux_sandbox_browser_click")
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_click"), _handler_returning(tm))
    body = _messages(out)[1].content[0]["text"]
    assert "describe_image" in body
    assert "/tmp/x.png" in body


# --- honest passthroughs are unchanged by the flag ---------------------------

def test_text_pointer_mode_still_passes_through_non_browser_tools():
    mw = BrowserVisionMiddleware(_SpyExec(), multimodal_driver=False)
    tm = _text_tm({"screenshot_path": "/x.png"}, name="pux_sandbox_python")
    out = mw.wrap_tool_call(_req("pux_sandbox_python"), _handler_returning(tm))
    assert out is tm  # non-browser tool — untouched regardless of mode


def test_text_pointer_mode_still_passes_through_results_without_screenshot_path():
    mw = BrowserVisionMiddleware(_SpyExec(), multimodal_driver=False)
    tm = _text_tm({"ok": True, "result": "42"})  # no screenshot_path
    out = mw.wrap_tool_call(_req("pux_sandbox_browser_evaluate"), _handler_returning(tm))
    assert out is tm


# --- the default is multimodal (image-block) — the fallback is opt-in --------

def test_default_construction_is_multimodal_mode():
    """``multimodal_driver`` defaults to True — direct construction (e.g. in
    tests, or before stack wiring) keeps the image-block behavior."""
    mw = BrowserVisionMiddleware(_SpyExec())
    assert mw.multimodal_driver is True


# --- async path mirrors sync -------------------------------------------------

def test_async_text_pointer_mode_emits_pointer():
    mw = BrowserVisionMiddleware(_SpyExec(), multimodal_driver=False)
    tm = _text_tm({"screenshot_path": "/tmp/shot.png"})

    async def _h(_request):
        return tm

    out = asyncio.run(mw.awrap_tool_call(_req("pux_sandbox_browser_click"), _h))
    body = _messages(out)[1].content[0]["text"]
    assert "describe_image" in body
    assert "/tmp/shot.png" in body
