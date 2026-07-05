"""Phase 7 — proactive context-offload gate.

Proves the ``ContextOffloadMiddleware.wrap_tool_call`` (sync + async) stashes
oversized tool results and replaces them with a preview + ``ctx:<id>`` handle,
small results pass through untouched, ``ctx_recall``/``ctx_search`` retrieve
them, and the store rejects path-escape handles. All against a tmp-path store
(no real ``.pux/ctx``, no Docker, no model tokens).
"""
from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any

import pytest
from langchain_core.messages import ToolMessage

from pux_harness.context.offload import (
    ContextOffloadMiddleware,
    build_ctx_tools,
    _offload,
)
from pux_harness.context.store import CtxStore

BIG = "x" * 20_000  # well over the 8000-char default threshold
SMALL = "only a few hundred chars " * 5


def _req(name: str = "execute", tcid: str = "call_1") -> SimpleNamespace:
    """A minimal stand-in for langchain's ToolCallRequest — the middleware only
    reads ``request.tool_call`` (a dict), so SimpleNamespace suffices."""
    return SimpleNamespace(tool_call={"name": name, "args": {}, "id": tcid})


def _tm(content: Any, tcid: str = "call_1", name: str = "execute") -> ToolMessage:
    return ToolMessage(content=content, tool_call_id=tcid, name=name)


# --- _offload: the pure core --------------------------------------------------


def test_offload_replaces_oversized_with_stub_and_handle(tmp_path):
    store = CtxStore(tmp_path)
    out = _offload(_tm(BIG), store, "execute", threshold=8000, preview=500)
    assert isinstance(out, ToolMessage)
    assert out.tool_call_id == "call_1"  # id preserved so the AIMessage still matches
    assert len(out.content) < len(BIG)
    assert "ctx-offload" in out.content
    assert "ctx_recall(" in out.content
    assert "Preview (first 500 chars)" in out.content
    # the full bytes live in the store, keyed by the handle in the stub
    handle = _extract_handle(out.content)
    assert store.recall(handle) == BIG


def test_offload_passes_small_result_through_unchanged(tmp_path):
    store = CtxStore(tmp_path)
    original = _tm(SMALL)
    out = _offload(original, store, "execute", threshold=8000, preview=500)
    assert out is original  # same object, nothing stashed
    assert not list((tmp_path).glob("*.txt"))


def test_offload_threshold_zero_disables(tmp_path):
    store = CtxStore(tmp_path)
    m = ContextOffloadMiddleware(store, threshold=0)
    out = m.wrap_tool_call(_req(), lambda r: _tm(SMALL))
    assert out.content == SMALL  # not stashed even though it's a ToolMessage


def test_offload_leaves_non_text_and_non_toolmessage_alone(tmp_path):
    store = CtxStore(tmp_path)
    # multimodal content (list of blocks) — must pass through, offloading would lose it
    multimodal = _tm([{"type": "text", "text": BIG}])
    assert _offload(multimodal, store, "x", threshold=10, preview=5) is multimodal
    # a Command / arbitrary object — pass through
    arbitrary = {"not": "a tool message"}
    assert _offload(arbitrary, store, "x", threshold=10, preview=5) is arbitrary


# --- middleware wrap hooks (sync + async identical behavior) -----------------


def test_middleware_wrap_tool_call_stashes(tmp_path):
    store = CtxStore(tmp_path)
    m = ContextOffloadMiddleware(store, threshold=8000, preview=400)
    out = m.wrap_tool_call(_req(name="grep"), lambda r: _tm(BIG, name="grep"))
    assert isinstance(out, ToolMessage)
    handle = _extract_handle(out.content)
    assert store.recall(handle) == BIG


def test_middleware_awrap_tool_call_stashes(tmp_path):
    """The server/runner use ainvoke → the ASYNC hook is the one that fires in
    production. Prove it offloads identically to the sync path."""
    store = CtxStore(tmp_path)
    m = ContextOffloadMiddleware(store, threshold=8000, preview=400)

    async def handler(_r):  # type: ignore[no-untyped-def]
        return _tm(BIG, name="execute")

    out = asyncio.run(m.awrap_tool_call(_req(), handler))
    assert isinstance(out, ToolMessage)
    handle = _extract_handle(out.content)
    assert store.recall(handle) == BIG


def test_middleware_passes_small_through(tmp_path):
    store = CtxStore(tmp_path)
    m = ContextOffloadMiddleware(store)
    original = _tm(SMALL)
    out = m.wrap_tool_call(_req(), lambda r: original)
    assert out is original


# --- recall + search tools (bound to the same store) -------------------------


def test_ctx_recall_returns_full_then_not_found(tmp_path):
    store = CtxStore(tmp_path)
    recall, _ = build_ctx_tools(store)
    stash = store.stash(BIG, tool="execute")
    assert recall.invoke({"handle": stash.handle}) == BIG
    # bare id also accepted
    assert recall.invoke({"handle": stash.id}) == BIG
    # garbage handle → friendly not-found, not an exception
    assert "no stashed content" in recall.invoke({"handle": "ctx:deadbeef"})


def test_ctx_search_finds_stashed_blob(tmp_path):
    store = CtxStore(tmp_path)
    store.stash("the alpha deployment failed at step 3", tool="execute")
    store.stash("unrelated log line", tool="execute")
    _, search = build_ctx_tools(store)
    out = search.invoke({"query": "alpha deployment"})
    assert "1 hit" in out
    assert "alpha deployment failed" in out
    # empty query returns nothing (no accidental full dumps)
    assert "no stashed result" in search.invoke({"query": "   "})


# --- store path safety -------------------------------------------------------


@pytest.mark.parametrize("bad", [
    "../etc/passwd", "ctx:../../etc/passwd", "ctx:..", "/abs/path",
    "ctx:GGGGGGGGGGGG",  # non-hex
    "",
])
def test_recall_rejects_path_escape(tmp_path, bad: str):
    store = CtxStore(tmp_path)
    (tmp_path / "etc").mkdir()
    (tmp_path / "secret.txt").write_text("SECRET")  # outside any stash
    # nothing stashed under a valid id, and no escape resolves to secret.txt
    assert store.recall(bad) is None


def test_search_caps_results(tmp_path):
    store = CtxStore(tmp_path)
    for _ in range(7):
        store.stash("needleneedle here", tool="execute")
    _, search = build_ctx_tools(store)
    out = search.invoke({"query": "needle", "limit": 3})
    assert "3 hit" in out


# --- retrieval tools are exempt (don't re-offload their own output) ------------


def test_retrieval_tools_are_exempt_from_offload(tmp_path):
    """ctx_recall/ctx_search exist to inject content INTO context, so their
    output must NOT be re-stashed — otherwise recalling a big result traps the
    agent in a recall->offload->recall loop. Surfaced by the Phase 7 E2E
    (ctx_recall(12K) returned 10K which got offloaded again); now exempt."""
    store = CtxStore(tmp_path)
    m = ContextOffloadMiddleware(store, threshold=10, preview=5)  # tiny threshold
    for name in ("ctx_recall", "ctx_search"):
        out = m.wrap_tool_call(_req(name=name), lambda r: _tm(BIG, name=name))
        assert out.content == BIG, f"{name} output must pass through un-stashed"


def test_non_retrieval_tool_with_same_size_does_offload(tmp_path):
    """Contrast: a same-sized result from any other tool (e.g. execute) IS
    stashed — confirms the exemption above is scoped to retrieval tools only,
    not a general small-threshold bypass."""
    store = CtxStore(tmp_path)
    m = ContextOffloadMiddleware(store, threshold=10, preview=5)
    out = m.wrap_tool_call(_req(name="execute"), lambda r: _tm(BIG, name="execute"))
    assert isinstance(out, ToolMessage)
    assert "ctx-offload" in out.content
    assert store.recall(_extract_handle(out.content)) == BIG


# --- helper ------------------------------------------------------------------


def _extract_handle(stub: str) -> str:
    """Pull the ``ctx:<id>`` handle out of an offload stub message."""
    import re
    m = re.search(r"ctx_recall\('(ctx:[0-9a-f]+)'\)", stub)
    assert m, f"no handle found in stub: {stub!r}"
    return m.group(1)
