"""Proactive context-offload (unified ``ContextMiddleware``).

Proves the unified ``ContextMiddleware.wrap_tool_call`` (sync + async) stashes
oversized tool results and replaces them with a preview + ``ctx:<id>`` handle,
small results pass through untouched, ``ctx_recall``/``ctx_search`` retrieve
them, and the store rejects path-escape handles. All against a tmp-path store
(no real ``.pux/events.sqlite``, no Docker, no model tokens).

The unified ``ContextMiddleware`` (one pass: capture + offload) + ``EventStore`` (one
sqlite store for events AND blobs) replaced the old separate ``ContextOffloadMiddleware`` + ``CtxStore``.
These tests were ported from the earlier surface; the behaviors are unchanged — only the module paths moved.
"""
from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any

import pytest
from langchain_core.messages import ToolMessage

from pux_harness.context.events import EventStore
from pux_harness.context.middleware import ContextMiddleware, _offload
from pux_harness.context.tools import build_context_tools

BIG = "x" * 20_000  # well over the 8000-char default threshold
SMALL = "only a few hundred chars " * 5


def _tool(store, name: str):
    """Pull a single named tool from the built context-tools surface.

    ``build_context_tools`` returns 6 tools (recall/search/index/stats/doctor/
    purge); these tests only exercise recall + search, so a name lookup replaces
    the old 2-tuple positional unpack."""
    return {t.name: t for t in build_context_tools(store)}[name]


def _req(name: str = "execute", tcid: str = "call_1") -> SimpleNamespace:
    """A minimal stand-in for langchain's ToolCallRequest — the middleware only
    reads ``request.tool_call`` (a dict), so SimpleNamespace suffices."""
    return SimpleNamespace(tool_call={"name": name, "args": {}, "id": tcid})


def _tm(content: Any, tcid: str = "call_1", name: str = "execute") -> ToolMessage:
    return ToolMessage(content=content, tool_call_id=tcid, name=name)


# --- _offload: the pure core --------------------------------------------------


def test_offload_replaces_oversized_with_stub_and_handle(tmp_path):
    store = EventStore(tmp_path / "events.db")
    out = _offload(_tm(BIG), store, "execute", threshold=8000, preview=500)
    assert isinstance(out, ToolMessage)
    assert out.tool_call_id == "call_1"  # id preserved so the AIMessage still matches
    assert len(out.content) < len(BIG)
    assert _extract_handle(out.content)  # a ctx:<id> handle = the offload fired
    assert "ctx_recall(" in out.content
    assert "(first 500 shown)" in out.content
    # the full bytes live in the store, keyed by the handle in the stub
    handle = _extract_handle(out.content)
    assert store.recall_blob(handle) == BIG


def test_offload_passes_small_result_through_unchanged(tmp_path):
    store = EventStore(tmp_path / "events.db")
    original = _tm(SMALL)
    out = _offload(original, store, "execute", threshold=8000, preview=500)
    assert out is original  # same object, nothing stashed


def test_offload_threshold_zero_disables(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store, threshold=0)
    out = m.wrap_tool_call(_req(), lambda r: _tm(SMALL))
    assert out.content == SMALL  # not stashed even though it's a ToolMessage


def test_offload_leaves_non_text_and_non_toolmessage_alone(tmp_path):
    store = EventStore(tmp_path / "events.db")
    # multimodal content (list of blocks) — must pass through, offloading would lose it
    multimodal = _tm([{"type": "text", "text": BIG}])
    assert _offload(multimodal, store, "x", threshold=10, preview=5) is multimodal
    # a Command / arbitrary object — pass through
    arbitrary = {"not": "a tool message"}
    assert _offload(arbitrary, store, "x", threshold=10, preview=5) is arbitrary


# --- middleware wrap hooks (sync + async identical behavior) -----------------


def test_middleware_wrap_tool_call_stashes(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store, threshold=8000, preview=400)
    out = m.wrap_tool_call(_req(name="grep"), lambda r: _tm(BIG, name="grep"))
    assert isinstance(out, ToolMessage)
    handle = _extract_handle(out.content)
    assert store.recall_blob(handle) == BIG


def test_middleware_awrap_tool_call_stashes(tmp_path):
    """The server/runner use ainvoke → the ASYNC hook is the one that fires in
    production. Prove it offloads identically to the sync path."""
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store, threshold=8000, preview=400)

    async def handler(_r):  # type: ignore[no-untyped-def]
        return _tm(BIG, name="execute")

    out = asyncio.run(m.awrap_tool_call(_req(), handler))
    assert isinstance(out, ToolMessage)
    handle = _extract_handle(out.content)
    assert store.recall_blob(handle) == BIG


def test_middleware_passes_small_through(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store)
    original = _tm(SMALL)
    out = m.wrap_tool_call(_req(), lambda r: original)
    assert out is original


# --- recall + search tools (bound to the same store) -------------------------


def test_ctx_recall_returns_full_then_not_found(tmp_path):
    store = EventStore(tmp_path / "events.db")
    recall = _tool(store, "ctx_recall")
    stash = store.stash_blob(BIG, tool="execute")
    assert recall.invoke({"handle": stash.handle}) == BIG
    # bare id also accepted
    assert recall.invoke({"handle": stash.id}) == BIG
    # garbage handle → friendly not-found, not an exception
    assert "no truncated result found" in recall.invoke({"handle": "ctx:deadbeef"})


def test_ctx_search_finds_stashed_blob(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.stash_blob("the alpha deployment failed at step 3", tool="execute")
    store.stash_blob("unrelated log line", tool="execute")
    search = _tool(store, "ctx_search")
    out = search.invoke({"query": "alpha deployment"})
    assert "1 hit" in out
    assert "alpha deployment failed" in out
    # empty query returns nothing (no accidental full dumps)
    assert "no prior tool output or event" in search.invoke({"query": "   "})


# --- store path safety -------------------------------------------------------


@pytest.mark.parametrize("bad", [
    "../etc/passwd", "ctx:../../etc/passwd", "ctx:..", "/abs/path",
    "ctx:GGGGGGGGGGGG",  # non-hex
    "",
])
def test_recall_rejects_path_escape(tmp_path, bad: str):
    store = EventStore(tmp_path / "events.db")
    (tmp_path / "etc").mkdir()
    (tmp_path / "secret.txt").write_text("SECRET")  # outside any stash
    # nothing stashed under a valid id, and no escape resolves to secret.txt
    assert store.recall_blob(bad) is None


def test_search_caps_results(tmp_path):
    store = EventStore(tmp_path / "events.db")
    for _ in range(7):
        store.stash_blob("needleneedle here", tool="execute")
    search = _tool(store, "ctx_search")
    out = search.invoke({"query": "needle", "limit": 3})
    assert "3 hit" in out


# --- retrieval tools are exempt (don't re-offload their own output) ------------


def test_retrieval_tools_are_exempt_from_offload(tmp_path):
    """ctx_recall/ctx_search exist to inject content INTO context, so their
    output must NOT be re-stashed — otherwise recalling a big result traps the
    agent in a recall->offload->recall loop. Surfaced by the E2E
    (ctx_recall(12K) returned 10K which got offloaded again); now exempt."""
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store, threshold=10, preview=5)  # tiny threshold
    for name in ("ctx_recall", "ctx_search"):
        out = m.wrap_tool_call(_req(name=name), lambda r: _tm(BIG, name=name))
        assert out.content == BIG, f"{name} output must pass through un-stashed"


def test_non_retrieval_tool_with_same_size_does_offload(tmp_path):
    """Contrast: a same-sized result from any other tool (e.g. execute) IS
    stashed — confirms the exemption above is scoped to retrieval tools only,
    not a general small-threshold bypass."""
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store, threshold=10, preview=5)
    out = m.wrap_tool_call(_req(name="execute"), lambda r: _tm(BIG, name="execute"))
    assert isinstance(out, ToolMessage)
    assert _extract_handle(out.content)  # offloaded → carries a ctx:<id> handle
    assert store.recall_blob(_extract_handle(out.content)) == BIG


# --- browser structural offload (cooperation with BrowserVisionMiddleware) ----


import json as _json
from langchain_core.messages import HumanMessage as _HumanMessage
from langgraph.types import Command as _Command


def _browser_payload(*, text="x" * 5000, links=None, images=None, url="https://x"):
    """A realistic browser page-result payload over the 8000-char threshold."""
    links = links or [{"text": f"link {i}", "url": f"https://x/{i}"} for i in range(30)]
    images = images or [{"src": f"https://x/{i}.png", "alt": ""} for i in range(50)]
    return {
        "ok": True,
        "page_data": {"title": "Some Page", "url": url, "text": text,
                       "links": links, "images": images},
        "element_map": [{"index": 1, "selector": "button#go"}],
        "screenshot_path": "/tmp/shot.png",
    }


def test_offload_trims_browser_payload_heavy_fields(tmp_path):
    """A browser ToolMessage over threshold gets its page_data.{text,links,images}
    stashed; the skeleton (ok, url, title, element_map, screenshot_path) stays
    inline so the agent can verify navigation + click by index without a recall."""
    store = EventStore(tmp_path / "events.db")
    payload = _browser_payload()
    full = _json.dumps(payload)
    assert len(full) > 8000  # over threshold
    out = _offload(
        _tm(full, name="pux_sandbox_browser_navigate"), store,
        "pux_sandbox_browser_navigate", threshold=8000, preview=500,
    )
    assert isinstance(out, ToolMessage)
    slim = _json.loads(out.content)
    # skeleton kept inline
    assert slim["ok"] is True
    assert slim["page_data"]["url"] == "https://x"
    assert slim["page_data"]["title"] == "Some Page"
    assert slim["element_map"] == payload["element_map"]
    assert slim["screenshot_path"] == "/tmp/shot.png"
    # heavy fields gone from inline view
    assert "links" not in slim["page_data"]
    assert "images" not in slim["page_data"]
    # 200-char text preview kept for orientation
    assert len(slim["page_data"]["text"]) <= 201
    # note + handle present
    assert "context_note" in slim
    handle = slim["context_note"].split("ctx_recall(")[1].split(")")[0].strip("'\"")
    # full payload recoverable
    assert _json.loads(store.recall_blob(handle)) == payload
    # slim is dramatically smaller
    assert len(out.content) < len(full) / 2


def test_offload_passes_small_browser_result_through(tmp_path):
    """A browser result under threshold (small page) stays inline — no stash
    friction for tiny pages where the body text is cheap enough to keep."""
    store = EventStore(tmp_path / "events.db")
    payload = {"ok": True, "page_data": {"title": "tiny", "url": "https://x", "text": "hi"},
               "element_map": [], "screenshot_path": "/tmp/s.png"}
    original = _tm(_json.dumps(payload), name="pux_sandbox_browser_navigate")
    out = _offload(original, store, "pux_sandbox_browser_navigate",
                   threshold=8000, preview=500)
    assert out is original  # untouched — under threshold


def test_offload_command_trims_text_keeps_image(tmp_path):
    """The real production path: BrowserVisionMiddleware (innermost) wraps the
    result as Command([text_tm, image_human]) BEFORE ContextMiddleware sees it.
    _offload must reach inside, trim the text TM, and rebuild the Command with
    the image HumanMessage PRESERVED."""
    store = EventStore(tmp_path / "events.db")
    payload = _browser_payload()
    tm = _tm(_json.dumps(payload), name="pux_sandbox_browser_navigate")
    # the companion image HumanMessage the vision middleware attached
    img_human = _HumanMessage(content=[
        {"type": "text", "text": "[screenshot result for call_1]"},
        {"type": "image", "base64": "iVBOR", "mime_type": "image/png"},
    ])
    command = _Command(update={"messages": [tm, img_human]})
    out = _offload(command, store, "pux_sandbox_browser_navigate",
                   threshold=8000, preview=500)
    assert isinstance(out, _Command)
    msgs = out.update["messages"]
    assert len(msgs) == 2
    # text TM was trimmed (slim JSON, heavy fields gone)
    slim = _json.loads(msgs[0].content)
    assert "links" not in slim["page_data"]
    assert "images" not in slim["page_data"]
    assert slim["ok"] is True
    # image HumanMessage is PRESERVED byte-for-byte (same object)
    assert msgs[1] is img_human
    assert msgs[1].content[1]["base64"] == "iVBOR"
    # tool_call_id preserved so the reducer still pairs it
    assert msgs[0].tool_call_id == "call_1"


def test_offload_command_unchanged_when_nothing_to_trim(tmp_path):
    """A Command whose text TM is under threshold passes through unchanged —
    no needless rebuild."""
    store = EventStore(tmp_path / "events.db")
    tm = _tm("small result", name="pux_sandbox_browser_type")
    img_human = _HumanMessage(content=[{"type": "text", "text": "x"}])
    command = _Command(update={"messages": [tm, img_human]})
    out = _offload(command, store, "pux_sandbox_browser_type",
                   threshold=8000, preview=500)
    assert out is command  # identity — nothing trimmed


# --- helper ------------------------------------------------------------------


def _extract_handle(stub: str) -> str:
    """Pull the ``ctx:<id>`` handle out of an offload stub message."""
    import re
    m = re.search(r"ctx_recall\('(ctx:[0-9a-f]+)'\)", stub)
    assert m, f"no handle found in stub: {stub!r}"
    return m.group(1)
