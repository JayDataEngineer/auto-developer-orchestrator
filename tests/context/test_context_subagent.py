"""The context layer reaches SUBAGENTS (the prepare-wiring-e2e-gap proof).

This is the test that RETIRES the old claim that deepagents'
``SubAgentMiddleware`` silently drops a subagent spec's ``middleware`` key, so
context-offload "can only run on the main agent". That claim was wrong: verified
against deepagents 0.6.12, ``middleware/subagents.py:494`` builds
``middleware = list(spec.get("middleware", []))`` and ``:511`` passes it straight
into ``langchain.agents.create_agent`` — the SAME compilation path a main agent
uses. So a ``ContextMiddleware`` on a subagent spec intercepts that subagent's
own tool calls.

We prove it through the REAL entry point, not a helper ([[prepare-wiring-e2e-gap]]):

1. **The seam fires.** Compile a sub-agent via the real ``create_agent`` (the
   exact function ``subagents.py:511`` calls) with
   ``middleware=[ContextMiddleware(store)]`` + a tool that returns >threshold
   chars; invoke with a scripted model that calls the tool; assert the result
   was offloaded (stub message + blob in the store + ``ctx_recall`` recovers
   the full bytes).
2. **Control: no middleware key -> no offload.** The same agent built the OLD
   way (``middleware=[]``) returns the full content and stashes nothing —
   proving the ``middleware`` key is what makes it work, not some ambient
   global.
3. **Bridge: our spec feeds the real compiler.** ``orgs.load_subagents``
   produces the ``middleware`` list; passing THAT list to ``create_agent``
   fires the offload — so the wiring in ``agent/orgs.py`` actually reaches
   deepagents, end to end.

LLM- and Docker-free: a tiny ``BaseChatModel`` emits one canned ``tool_call``
then a terminator, so the agent loop runs exactly one tool invocation.
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
from pux_harness.context.layer import build_context_layer
from pux_harness.context.middleware import ContextMiddleware


class _NoArgs(BaseModel):
    """Empty args schema for the argument-less test tool."""


def _big_tool() -> StructuredTool:
    """A specialist tool whose result is comfortably over the 8000-char
    threshold — the kind of output the layer exists to offload."""
    payload = "BIG-BLOB-MARKER " + ("x" * 9000)

    def _run() -> str:
        return payload

    return StructuredTool(
        name="big_tool", description="returns a large result", args_schema=_NoArgs,
        func=_run,
    )


class _ScriptedToolModel(BaseChatModel):
    """Calls ``big_tool`` once, then emits a plain terminator so the agent loop
    ends after a single tool invocation. ``bind_tools`` returns ``self`` — the
    canned response ignores the bound schema, which is all the routing logic
    needs (it inspects ``AIMessage.tool_calls`` to decide edges)."""

    _calls: int = PrivateAttr(default=0)

    @property
    def _llm_type(self) -> str:
        return "scripted-tool"

    def bind_tools(self, tools: Any, **kwargs: Any) -> "_ScriptedToolModel":  # noqa: D401
        return self

    def _generate(self, messages: Any, stop: Any = None, run_manager: Any = None,
                  **kwargs: Any) -> ChatResult:
        self._calls += 1
        if self._calls == 1:
            msg = AIMessage(
                content="", tool_calls=[{"name": "big_tool", "args": {}, "id": "c1"}],
            )
        else:
            msg = AIMessage(content="done")
        return ChatResult(generations=[ChatGeneration(message=msg)])


def _extract_handle(stub: str) -> str:
    m = re.search(r"(ctx:[0-9a-f]+)", stub)
    assert m, f"no ctx handle in stub: {stub!r}"
    return m.group(1)


# --- 1. the seam fires --------------------------------------------------------


def test_subagent_with_context_middleware_offloads(tmp_path):
    """A sub-agent compiled with ``ContextMiddleware`` offloads the oversized
    tool result: the model sees the stub, the full bytes live in the store, and
    ``ctx_recall`` (same store) recovers them."""
    store = EventStore(tmp_path / "e.db")
    cm = ContextMiddleware(store, threshold=8000, preview=500)
    agent = create_agent(
        model=_ScriptedToolModel(), tools=[_big_tool()], middleware=[cm],
    )

    result = agent.invoke({"messages": [{"role": "user", "content": "run it"}]})

    tool_msgs = [m for m in result["messages"] if isinstance(m, ToolMessage)]
    assert tool_msgs, "expected the big_tool ToolMessage in the trace"
    stub = tool_msgs[-1]
    assert _extract_handle(stub.content)  # a ctx:<id> handle → model saw the stub, not the blob
    # Only a bounded PREVIEW is inline (boilerplate + ~500 chars), NOT the full
    # 9016-char payload — that's the whole point of offloading.
    assert len(stub.content) < 2000

    handle = _extract_handle(stub.content)
    full = store.recall_blob(handle)  # the full bytes live behind the handle
    assert full is not None
    assert full.startswith("BIG-BLOB-MARKER")
    assert len(full) == 9016  # 16-char marker + 9000-char body


# --- 2. control: no middleware key -> no offload ------------------------------


def test_subagent_without_middleware_does_not_offload(tmp_path):
    """The control: the SAME agent built the old way (``middleware=[]``) returns
    the full content and stashes nothing — proving the ``middleware`` key is what
    makes the layer fire, not an ambient global or the tool itself."""
    store = EventStore(tmp_path / "e.db")
    agent = create_agent(
        model=_ScriptedToolModel(), tools=[_big_tool()], middleware=[],
    )

    result = agent.invoke({"messages": [{"role": "user", "content": "run it"}]})

    tool_msgs = [m for m in result["messages"] if isinstance(m, ToolMessage)]
    assert tool_msgs
    assert tool_msgs[-1].content.startswith("BIG-BLOB-MARKER")  # full content inline
    # No ctx:<id> handle → not offloaded. (Raw regex, not _extract_handle —
    # that helper ASSERTS a handle exists, so it can't probe the negative case.)
    assert re.search(r"ctx:[0-9a-f]+", tool_msgs[-1].content) is None
    # And nothing was stashed: no blob handle exists to recall.
    assert store.recall_blob("ctx:deadbeefcafe") is None
    assert store.search_context("BIG-BLOB-MARKER") == []


# --- 3. bridge: load_subagents middleware feeds the real compiler -------------

@pytest.fixture
def fake_tree(tmp_path: Any, monkeypatch: pytest.MonkeyPatch) -> Any:
    """Minimal orgs/ tree so ``orgs.load_subagents`` produces a real spec whose
    ``middleware`` list we can hand to ``create_agent``."""
    from pux_harness.agent import orgs

    (tmp_path / "orgs" / "o" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "skills").mkdir(parents=True)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    (tmp_path / "orgs" / "o" / "AGENTS.md").write_text("# o\n")
    (tmp_path / "orgs" / "o" / "org.yaml").write_text("agents: [worker]\n")
    (tmp_path / "orgs" / "o" / "agents" / "worker.md").write_text(
        "---\nname: worker\ndescription: d\n---\n\nbody.\n"
    )
    return tmp_path


def test_load_subagents_middleware_fires_in_real_compiler(
    fake_tree: Any, monkeypatch: pytest.MonkeyPatch,
) -> None:
    """The ``middleware`` list ``orgs.load_subagents`` attaches to every spec is
    consumable by the real ``create_agent`` and FIRES — so the wiring in
    ``agent/orgs.py`` reaches deepagents end to end, not just the spec dict
    (which is all ``test_load_subagents`` asserts)."""
    from pux_harness.agent import orgs
    from pux_harness.context import events as ev

    # load_subagents builds the spec + binds the layer to the shared event store.
    # Redirect that store to a tmp path so the assertion is self-contained
    # (monkeypatch auto-restores it for the rest of the suite).
    monkeypatch.setattr(ev, "_store", EventStore(fake_tree / "shared.db"))

    # load_subagents takes the layer explicitly now (one way: the loader no
    # longer builds it). Build it AFTER the store redirect above so the layer
    # binds to the shared.db store the assertion below writes to.
    mw, ctx_tools = build_context_layer()
    subs = orgs.load_subagents(
        "o", [_big_tool()], subagent_middleware=mw, retrieval_tools=ctx_tools,
    )
    mw_list = subs[0]["middleware"]
    assert mw_list, "load_subagents must attach the context layer to every subagent"
    assert any(isinstance(m, ContextMiddleware) for m in mw_list)

    agent = create_agent(
        model=_ScriptedToolModel(), tools=[_big_tool()], middleware=mw_list,
    )
    result = agent.invoke({"messages": [{"role": "user", "content": "run it"}]})

    tool_msgs = [m for m in result["messages"] if isinstance(m, ToolMessage)]
    assert tool_msgs
    assert _extract_handle(tool_msgs[-1].content)  # ctx:<id> handle → the spec's middleware fired
