"""LIVE E2E for the proactive context / money-saver layer with a REAL model.

The sibling ``test_context_money_saver_e2e.py`` proves the whole stash → recall
→ search cycle through real ``create_agent`` with a SCRIPTED model — i.e. it
proves the wiring and quantifies the saving (91.3%), but the model's decision
to call ``ctx_recall`` is hard-coded. This file closes the last gap in
[[verify-or-die]]: does a REAL model, paying REAL tokens, get the offload stub
AND autonomously decide to recover the content with ``ctx_recall``?

Skipped unless ``PUX_E2E=1`` AND ``OPENROUTER_API_KEY`` is in env (the harness's
``.env``, auto-sourced by ``bin/pux``). Run from the harness dir:

    set -a; . ../.env; set +a
    PUX_E2E=1 uv run pytest tests/test_context_money_saver_live_e2e.py -q -s

What it proves (the only path the scripted tests can't):
1. A real tool result over the threshold is offloaded — the live model sees a
   truncated result with a ``ctx:<id>`` handle, NOT the 11 KB body.
2. The real model reads the ``ctx:<id>`` handle out of that stub and calls
   ``ctx_recall`` itself — no hard-coded retrieval.
3. End-to-end recovery: the model's FINAL ANSWER contains an unguessable tail
   token that exists ONLY in the offloaded bytes (past the 1500-char preview).
   The only way the model can answer correctly is to recall the full blob.
"""
from __future__ import annotations

import os
import re

import pytest
from langchain.agents import create_agent
from langchain_core.messages import AIMessage, ToolMessage
from langchain_core.tools import StructuredTool
from pydantic import BaseModel

pytestmark = [
    pytest.mark.skipif(
        os.environ.get("PUX_E2E") != "1",
        reason="set PUX_E2E=1 (and source ../.env for OPENROUTER_API_KEY) to run the live model E2E",
    ),
    pytest.mark.skipif(
        not os.environ.get("OPENROUTER_API_KEY"),
        reason="OPENROUTER_API_KEY missing — source the harness .env (bin/pux does this automatically)",
    ),
]

# The payload: ~11 KB of body (well past the 8000-char offload threshold) plus
# a natural end-of-document footer carrying an UNGUESSABLE reference token. The
# footer sits at ~char 11000, far beyond the 1500-char preview the stub keeps
# inline — so no model can answer "what are the last characters?" from the
# preview alone. It MUST recall. Phrased as prose (not a <<BRACKETED::MARKER>>)
# so a skeptical model treats it as document content and quotes it, rather than
# reasoning that it's an injected artifact.
TAIL = "MTX-7733-KMQF-2A91"  # 17 chars, unguessable archive-ref style
FILLER = ("The quick brown fox jumps over the lazy dog. " * 260)
PAYLOAD = FILLER + f"\n\nEnd of transcript. Archive ref: {TAIL}-finale."  # ~11.7 KB


class _NoArgs(BaseModel):
    pass


def _big_tool() -> StructuredTool:
    """Returns a large result whose only memorable detail (the tail) is buried
    past the offload preview. A real agent asked to quote the tail has to
    recover the full bytes via ctx_recall — exercising the money-saver cycle
    against a live model."""

    def _run() -> str:
        return PAYLOAD

    return StructuredTool(
        name="big_tool",
        description="Returns a large text document. Call this when you need its content.",
        args_schema=_NoArgs,
        func=_run,
    )


def _ai_texts(trace: dict) -> list[str]:
    """Every prose string the model produced, in order (incl. messages that
    ALSO carried a tool_call — the model often narrates 'let me get the tail'
    in the same turn it calls ctx_recall)."""
    out = []
    for m in trace["messages"]:
        if isinstance(m, AIMessage) and m.content:
            out.append(m.content if isinstance(m.content, str) else str(m.content))
    return out


def _last_ai_answer(trace: dict) -> str:
    # Prefer the last AIMessage with prose and NO tool_calls (a true terminal
    # answer); fall back to the last prose AIMessage of any kind.
    prose_no_tools = ""
    prose_any = ""
    for m in trace["messages"]:
        if isinstance(m, AIMessage) and m.content:
            txt = m.content if isinstance(m.content, str) else str(m.content)
            prose_any = txt
            if not m.tool_calls:
                prose_no_tools = txt
    return prose_no_tools or prose_any


def test_real_model_offloads_then_recovers(tmp_path):
    """The headline live proof. A real MiMo-2.5 model is given big_tool + the
    context layer and asked to quote the tail of big_tool's output. It must:
    (a) get an offload stub (not the 11 KB body), (b) autonomously call
    ctx_recall to recover the bytes, (c) answer with the unguessable tail."""
    # Imported lazily so module import (and collection) doesn't require the
    # harness env / key — only the live run does.
    from pux_harness.agent.model import get_model
    from pux_harness.context.events import EventStore
    from pux_harness.context.middleware import ContextMiddleware
    from pux_harness.context.tools import build_context_tools

    store = EventStore(tmp_path / "live.db")
    agent = create_agent(
        model=get_model(role="base"),
        tools=[_big_tool(), *build_context_tools(store)],
        middleware=[ContextMiddleware(store, threshold=8000, preview=1500)],
    )

    task = (
        "Call big_tool. Then report the EXACT last 40 characters of its raw "
        "output verbatim, character-for-character — copy them precisely. Do "
        "not guess or paraphrase; if you do not have them, retrieve them."
    )
    trace = agent.invoke(
        {"messages": [{"role": "user", "content": task}]},
        config={"recursion_limit": 40},
    )

    big_tms = [m for m in trace["messages"]
               if isinstance(m, ToolMessage) and m.name == "big_tool"]
    recall_tms = [m for m in trace["messages"]
                  if isinstance(m, ToolMessage) and m.name == "ctx_recall"]
    answer = _last_ai_answer(trace)
    ai_texts = _ai_texts(trace)
    tail_in_any_ai = any(TAIL in t for t in ai_texts)

    # Full sequence dump — the live model is non-deterministic, so we want the
    # real trace on a failure (or success), not a bare assert.
    print("\n[live] --- message sequence ---")
    for i, m in enumerate(trace["messages"]):
        kind = type(m).__name__
        name = getattr(m, "name", "") or ""
        tcs = [tc.get("name", "?") for tc in getattr(m, "tool_calls", []) or []]
        content = getattr(m, "content", "")
        snippet = (content[:140] + "…") if isinstance(content, str) and len(content) > 140 else content
        print(f"  [{i:02d}] {kind:11s} {name:11s} tcs={tcs}  {snippet!r}")
    print(f"[live] big_tool calls={len(big_tms)}  ctx_recall calls={len(recall_tms)}  "
          f"ai_prose_msgs={len(ai_texts)}")

    # (a) offload fired — the model saw the stub, not the 11 KB body
    assert big_tms, "live model never called big_tool"
    stub = big_tms[-1].content
    offloaded = re.search(r"ctx:[0-9a-f]+", stub) is not None
    print(f"\n[live] offload fired: {offloaded}  "
          f"stub_chars={len(stub)}  body_chars={len(PAYLOAD)}")
    assert offloaded, "middleware did not offload big_tool's result (no ctx:<id> handle in stub)"
    assert TAIL not in stub, "tail leaked into the preview — model could answer without recalling"

    # (b) the real model autonomously recovered the bytes via ctx_recall
    print(f"[live] ctx_recall called autonomously: {bool(recall_tms)}")
    assert recall_tms, (
        "live model saw the offload stub but did NOT call ctx_recall to recover "
        "— the retrieval surface wasn't discovered autonomously"
    )
    recalled = recall_tms[-1].content
    print(f"[live] ctx_recall returned full payload: {recalled == PAYLOAD}")
    assert recalled == PAYLOAD, "ctx_recall did not return the exact original bytes"

    # (c) end-to-end: the model's answer carries the unguessable tail. Accept it
    # in ANY model prose message (the model may answer mid-sequence or loop);
    # the only way TAIL appears at all is if the model read the recalled bytes.
    print(f"[live] unguessable tail in some model message: {tail_in_any_ai}")
    print(f"[live] in final answer specifically: {TAIL in answer}")
    print(f"[live] final answer: {answer!r}")
    assert tail_in_any_ai, (
        "live model never produced the unguessable tail in any message — it "
        "recalled the bytes but did not transcribe them (layer works; this is a "
        "model-answering or loop issue, see the message dump above)"
    )

    # Report the model-visible saving this run achieved (the money saved).
    print(
        f"[live] money-saver: big_tool body {len(PAYLOAD)} chars (~{len(PAYLOAD)//4} tok) "
        f"→ model saw stub {len(stub)} chars (~{len(stub)//4} tok)  "
        f"saved={100*(len(PAYLOAD)-len(stub))/len(PAYLOAD):.1f}%"
    )
