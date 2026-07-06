"""E2E for the proactive context / money-saver layer through the REAL agent loop.

What the sibling tests already cover, and the gap this file closes:

* ``test_context_offload`` exercises the middleware in ISOLATION (a
  ``SimpleNamespace`` request, not the agent loop).
* ``test_context_subagent`` proves offload FIRES through real ``create_agent``
  — but its scripted model calls ``big_tool`` once and stops. It never drives
  the retrieval surface, so the recovery half of the design is unit-tested
  only (``ctx_recall``/``ctx_search`` invoked directly, not in-loop).

The money-saver is a CYCLE: **stash → find → recover**, plus a measurable
token saving. This file drives all three legs through the real
``create_agent`` loop (real middleware, real store, real retrieval tools —
only the model is scripted, so no tokens, no Docker):

1. **MONEY SAVED, quantified.** An oversized tool result is stashed; the model
   sees only a bounded stub. We measure the model-visible chars with offload
   ON vs OFF and assert the saving (the whole point of the layer).
2. **RECALL IN-LOOP.** The scripted model reads the ``ctx:<id>`` handle out of
   the stub — exactly as a real model would — and calls ``ctx_recall``,
   recovering the full bytes through the real loop. And because
   ``ctx_recall`` is in ``_RETRIEVAL_TOOLS``, its result is NOT re-offloaded
   (proven: its content carries no second ``ctx:<id>`` handle).
3. **SEARCH IN-LOOP.** ``ctx_search`` finds the stashed blob by phrase and
   returns the handle — the path for when the agent remembers a detail but
   didn't keep a handle.

This retires the [[prepare-wiring-e2e-gap]] for the Phase-19 unified layer.
"""
from __future__ import annotations

import re
from typing import Any

import pytest
from langchain.agents import create_agent
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, ToolMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langchain_core.tools import StructuredTool
from pydantic import BaseModel, PrivateAttr

from pux_harness.context.events import EventStore
from pux_harness.context.middleware import ContextMiddleware
from pux_harness.context.tools import build_context_tools

# Big enough to clear the 8000-char offload threshold with room to spare.
BIG_BODY = "x" * 9000
BIG_MARKER = "BIG-BLOB-MARKER"
BIG_PAYLOAD = f"{BIG_MARKER} {BIG_BODY}"  # 9016 chars

_HANDLE_RE = re.compile(r"ctx:[0-9a-f]+")


def _offloaded(content: str) -> bool:
    """The offload signal is structural: a truncated result carries a
    ``ctx:<id>`` handle (the retrieval pointer), a normal result does not.
    Keying on the handle — not on stub phrasing — survives rewording."""
    return _HANDLE_RE.search(content) is not None


class _NoArgs(BaseModel):
    pass


def _big_tool() -> StructuredTool:
    def _run() -> str:
        return BIG_PAYLOAD

    return StructuredTool(
        name="big_tool", description="returns a large result", args_schema=_NoArgs,
        func=_run,
    )


class _ScriptedModel(BaseChatModel):
    """Walks a canned tool-call sequence, inspecting the message history to
    pull the dynamic ``ctx:<id>`` handle for the recall step (a real model
    does the same — read the stub, extract the handle, recall).

    ``_stop_after_big`` collapses the sequence to a single ``big_tool`` call
    then a terminator — for the money-saved measurement, which must isolate
    the offload step (no retrieval tools on that agent)."""

    _turn: int = PrivateAttr(default=0)
    _seen_handle: str = PrivateAttr(default="")
    _stop_after_big: bool = PrivateAttr(default=False)

    @property
    def _llm_type(self) -> str:
        return "scripted-cycle"

    def bind_tools(self, tools: Any, **kwargs: Any) -> "_ScriptedModel":
        return self

    def _generate(self, messages: Any, stop: Any = None, run_manager: Any = None,
                  **kwargs: Any) -> ChatResult:
        self._turn += 1
        if self._turn == 1:
            msg = AIMessage(content="", tool_calls=[{"name": "big_tool", "args": {}, "id": "c1"}])
        elif self._stop_after_big:
            msg = AIMessage(content="done")
        elif self._turn == 2:
            # Read the ctx:<id> handle out of the offload stub the loop just
            # produced (the last ToolMessage in history).
            handle = ""
            for m in reversed(messages):
                if isinstance(m, ToolMessage):
                    mm = re.search(r"(ctx:[0-9a-f]+)", m.content)
                    if mm:
                        handle = mm.group(1)
                        break
            assert handle, "scripted model found no ctx handle in the offload stub"
            self._seen_handle = handle
            msg = AIMessage(content="", tool_calls=[{"name": "ctx_recall", "args": {"handle": handle}, "id": "c2"}])
        elif self._turn == 3:
            msg = AIMessage(content="", tool_calls=[{"name": "ctx_search", "args": {"query": BIG_MARKER}, "id": "c3"}])
        else:
            msg = AIMessage(content="done")
        return ChatResult(generations=[ChatGeneration(message=msg)])


def _tms_by_name(trace: dict, name: str) -> list[ToolMessage]:
    return [m for m in trace["messages"] if isinstance(m, ToolMessage) and m.name == name]


# --- 1. money saved, quantified ------------------------------------------------


def test_offload_saves_model_visible_tokens(tmp_path):
    """The big_tool result the MODEL sees is a bounded stub with offload ON and
    the full 9016-char payload with it OFF. We measure both and assert the
    saving — the concrete 'money saved' the layer exists to deliver."""
    on_store = EventStore(tmp_path / "on.db")
    on_model = _ScriptedModel()
    on_model._stop_after_big = True  # isolate the offload step — no retrieval on this agent
    on_agent = create_agent(
        model=on_model, tools=[_big_tool()],
        middleware=[ContextMiddleware(on_store, threshold=8000, preview=500)],
    )
    on_trace = on_agent.invoke({"messages": [{"role": "user", "content": "run it"}]},
                               config={"recursion_limit": 25})
    on_big = _tms_by_name(on_trace, "big_tool")[-1].content
    on_chars = len(on_big)

    off_store = EventStore(tmp_path / "off.db")
    off_model = _ScriptedModel()
    off_model._stop_after_big = True
    off_agent = create_agent(
        model=off_model, tools=[_big_tool()], middleware=[],
    )
    off_trace = off_agent.invoke({"messages": [{"role": "user", "content": "run it"}]},
                                 config={"recursion_limit": 25})
    off_big = _tms_by_name(off_trace, "big_tool")[-1].content
    off_chars = len(off_big)

    assert _offloaded(on_big)
    assert BIG_BODY not in on_big          # stub holds only the ~500-char preview, not the 9000-char bulk
    assert on_chars < 2000                                          # bounded preview
    assert off_big.startswith(BIG_MARKER) and not _offloaded(off_big)
    assert BIG_BODY in off_big and off_chars == len(BIG_PAYLOAD)   # full payload inline

    saving = 100 * (off_chars - on_chars) / off_chars
    # Roughly 4 chars/token, so the saving in tokens is the same fraction.
    print(
        f"\n[money-saver] big_tool model-visible: ON={on_chars} chars "
        f"(~{on_chars // 4} tok)  OFF={off_chars} chars (~{off_chars // 4} tok)  "
        f"saved={saving:.1f}%"
    )
    # The stub must be a small fraction of the full payload — if not, offload
    # isn't paying for itself.
    assert saving > 75.0


# --- 2 + 3. the full stash → recall → search cycle through the real loop -------


def test_retrieval_cycle_fires_through_real_loop(tmp_path):
    """Drives the whole money-saver cycle through real ``create_agent``:
    big_tool is offloaded (stash) → ctx_recall recovers the full bytes by the
    handle the model read out of the stub (recover) → ctx_search finds the
    blob by phrase (find). Proves the recovery half is wired, not just the
    offload half — and that retrieval results aren't re-offloaded."""
    store = EventStore(tmp_path / "cycle.db")
    agent = create_agent(
        model=_ScriptedModel(),
        tools=[_big_tool(), *build_context_tools(store)],
        middleware=[ContextMiddleware(store, threshold=8000, preview=500)],
    )
    trace = agent.invoke({"messages": [{"role": "user", "content": "run the cycle"}]},
                         config={"recursion_limit": 25})

    # --- (a) stash: big_tool result was offloaded to a bounded stub ---------
    big_tms = _tms_by_name(trace, "big_tool")
    assert big_tms, "big_tool never ran"
    stub = big_tms[-1].content
    assert _offloaded(stub) and BIG_BODY not in stub   # preview only, not the 9000-char bulk
    handle = re.search(r"(ctx:[0-9a-f]+)", stub).group(1)

    # --- (b) recover: ctx_recall pulled the FULL bytes back in-loop ---------
    recall_tms = _tms_by_name(trace, "ctx_recall")
    assert recall_tms, "ctx_recall was never called in-loop"
    recalled = recall_tms[-1].content
    assert recalled == BIG_PAYLOAD  # the full original bytes, not a stub
    # Critical: the recall result was NOT re-offloaded (ctx_recall is exempt).
    # If it were, the agent would be trapped the instant it recovered a big
    # blob — content replaced with a stub pointing back at itself.
    assert not _offloaded(recalled)

    # --- (c) find: ctx_search rediscovered the blob by phrase ---------------
    search_tms = _tms_by_name(trace, "ctx_search")
    assert search_tms, "ctx_search was never called in-loop"
    search_out = search_tms[-1].content
    # ctx_search is unified (blobs + events): the stashed blob AND its
    # tool_call event (whose output_preview also contains the marker) both
    # match. Assert the recoverable [blob] hit is present and surfaced the
    # handle — the [event] hit is expected, not a bug.
    assert "[blob]" in search_out
    assert handle in search_out          # the stash's handle is surfaced
    assert BIG_MARKER in search_out      # the phrase matched inside the blob

    # The store agrees: the blob is recoverable by handle, and search finds
    # it. The marker also appears in events (big_tool's preview + ctx_search's
    # own args, which quote the query) — that's the unified surface working,
    # not double-stashing. The invariant that matters: exactly ONE blob.
    assert store.recall_blob(handle) == BIG_PAYLOAD
    hits = store.search_context(BIG_MARKER)
    blob_hits = [h for h in hits if h.kind == "blob"]
    assert len(blob_hits) == 1            # not double-stashed (recall wasn't re-offloaded)
    assert blob_hits[0].handle == handle  # and it's the one we recovered above
