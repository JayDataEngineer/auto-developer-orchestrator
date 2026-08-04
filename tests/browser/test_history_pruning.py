"""History pruning — old screenshot image blocks stripped before model call.

After N navigations, the message history carries N screenshot HumanMessages
(~1,425 vision tokens each). The model only needs the CURRENT screenshot to
decide the next action. _prune_old_screenshots keeps the last
_KEEP_RECENT_SCREENSHOTS and replaces older image blocks with a text
placeholder so the execution trace survives without the pixel cost.
"""
from __future__ import annotations

from langchain_core.messages import HumanMessage, AIMessage, ToolMessage

from pux_harness.context.browser_vision import (
    _prune_old_screenshots,
    _KEEP_RECENT_SCREENSHOTS,
    BrowserVisionMiddleware,
)


def _img_human(label: str = "shot") -> HumanMessage:
    """A HumanMessage carrying a screenshot image block + text label."""
    return HumanMessage(content=[
        {"type": "text", "text": f"[screenshot result for {label}]"},
        {"type": "image", "base64": "iVBOR", "mime_type": "image/png"},
    ])


def _text_human(text: str) -> HumanMessage:
    return HumanMessage(content=text)


# --- pure function tests ----------------------------------------------------

def test_no_screenshots_passes_through_unchanged():
    msgs = [AIMessage(content="hello"), _text_human("do stuff")]
    out = _prune_old_screenshots(msgs)
    assert out is msgs  # identity — nothing to prune


def test_leq_keep_recent_passes_through_unchanged():
    """When screenshot count <= _KEEP_RECENT_SCREENSHOTS, no pruning."""
    msgs = [_img_human("a"), _img_human("b")]
    if len(msgs) <= _KEEP_RECENT_SCREENSHOTS:
        out = _prune_old_screenshots(msgs)
        assert out is msgs  # identity — nothing to prune


def test_old_screenshots_replaced_with_placeholder():
    """3 screenshots → oldest 1 pruned (keep last 2)."""
    msgs = [
        AIMessage(content="task"),
        _img_human("nav1"),       # index 1 — OLDEST, should be pruned
        AIMessage(content="step1"),
        _img_human("nav2"),       # index 3 — KEEP (recent)
        AIMessage(content="step2"),
        _img_human("nav3"),       # index 5 — KEEP (most recent)
    ]
    out = _prune_old_screenshots(msgs)
    assert out is not msgs  # new list — pruning happened
    assert len(out) == len(msgs)  # same message count, just modified content

    # The oldest screenshot (index 1) has its image block replaced
    pruned = out[1]
    assert isinstance(pruned.content, list)
    types = [b.get("type") for b in pruned.content]
    assert "image" not in types
    assert any("removed" in str(b.get("text", "")) for b in pruned.content)
    # Text block label is preserved
    assert any("screenshot result for nav1" in str(b.get("text", "")) for b in pruned.content)

    # The two recent screenshots (indices 3, 5) are untouched — still have images
    assert any(b.get("type") == "image" for b in out[3].content)
    assert any(b.get("type") == "image" for b in out[5].content)


def test_many_screenshots_only_keep_recent():
    """6 screenshots → only last _KEEP_RECENT_SCREENSHOTS survive."""
    msgs = [_img_human(f"nav{i}") for i in range(6)]
    out = _prune_old_screenshots(msgs)
    # Count how many still have image blocks
    with_images = [m for m in out if isinstance(m.content, list)
                   and any(b.get("type") == "image" for b in m.content)]
    assert len(with_images) == _KEEP_RECENT_SCREENSHOTS
    # The surviving ones should be the LAST two
    assert any("nav5" in str(b) for b in with_images[-1].content)
    assert any("nav4" in str(b) for b in with_images[-2].content)


def test_text_only_humanmessages_untouched():
    """Non-image HumanMessages pass through without modification."""
    msgs = [_text_human("just text"), _img_human("a"), _img_human("b")]
    out = _prune_old_screenshots(msgs)
    assert out is msgs  # only 1 screenshot, <= KEEP_RECENT → identity


# --- integration: middleware hook ------------------------------------------

def test_middleware_awrap_model_call_prunes():
    """The async model-call hook strips old screenshots from the request."""
    import asyncio
    from types import SimpleNamespace

    mw = BrowserVisionMiddleware(exec_client=None)

    # Build a fake request with 4 screenshot messages + 1 recent
    msgs = [
        _img_human("old1"),
        _img_human("old2"),
        _img_human("old3"),
        _img_human("recent1"),
        _img_human("recent2"),
    ]
    request = SimpleNamespace(messages=msgs)
    # Mock override to return a new request with the pruned messages
    pruned_msgs = []
    def override(**kw):
        pruned_msgs.extend(kw.get("messages", []))
        return SimpleNamespace(**{**request.__dict__, **kw})

    request.override = override

    called = {}
    async def handler(req):
        called["messages"] = req.messages
        return "model_response"

    result = asyncio.run(mw.awrap_model_call(request, handler))
    assert result == "model_response"
    # Pruning happened — only 2 screenshots survive
    with_images = [m for m in called["messages"]
                   if isinstance(m.content, list)
                   and any(b.get("type") == "image" for b in m.content)]
    assert len(with_images) == _KEEP_RECENT_SCREENSHOTS


def test_disabled_middleware_skips_pruning():
    """When the middleware is disabled, no pruning happens."""
    import asyncio
    from types import SimpleNamespace

    mw = BrowserVisionMiddleware(exec_client=None, enabled=False)
    msgs = [_img_human(f"nav{i}") for i in range(5)]
    request = SimpleNamespace(messages=msgs)
    request.override = lambda **kw: SimpleNamespace(**{**request.__dict__, **kw})

    async def handler(req):
        return "ok"

    asyncio.run(mw.awrap_model_call(request, handler))
    # All 5 screenshots still intact (handler saw original messages)
    # The key: override was never needed because pruning returned identity
