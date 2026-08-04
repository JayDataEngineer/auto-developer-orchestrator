"""Per-session ``prompt()`` serialization — the "async seems broken" fix.

The bug (locked here): the ACP dispatcher spawns each ``session/prompt`` as its
own asyncio task with NO per-session serialization, and ``AgentServerACP`` keeps
``_cancelled`` as a SINGLE shared boolean on ``self``. Two prompts on one session
then race — two concurrent ``agent.astream`` runs on the same ``thread_id``
(corrupting the LangGraph checkpointer) + a shared ``_cancelled`` that aborts
the WRONG prompt's in-flight tool calls. Symptom: "all three task calls were
cancelled / another message came in" — the agent even confabulates an error
string that exists nowhere in our code, the acp lib, OR deepagents_acp.

The fix: ``_RegisteringAgentServerACP.prompt`` wraps its body in a per-session
``asyncio.Lock``. These tests pin the invariant — same-session prompts NEVER
overlap; different-session prompts DO run in parallel; a cancel that lands while
a prompt is queued is honored without running the agent.
"""
from __future__ import annotations

import asyncio
import types
from unittest.mock import AsyncMock, MagicMock

import pytest
from acp.schema import PromptResponse
from deepagents_acp.server import AgentServerACP

from pux_harness.acp import _RegisteringAgentServerACP


def _make_server() -> _RegisteringAgentServerACP:
    """A server with a mocked agent + store. ``_agent`` is set to a Mock whose
    ``aget_state`` returns ``None`` (no ask_user interrupt) so ``prompt()``
    falls straight through to ``super().prompt()`` — which the test patches."""
    agent = MagicMock()
    agent.aget_state = AsyncMock(return_value=None)
    server = _RegisteringAgentServerACP(
        agent=agent, store=MagicMock(), org="coder"
    )
    server._agent = agent  # truthy → skip _reset_agent + browser warmup
    return server


def _ns() -> types.SimpleNamespace:
    """A prompt stand-in whose ``.content`` is None → title capture short-
    circuits to "" and never touches the store."""
    return types.SimpleNamespace(content=None)


def test_prompt_serialized_within_a_session(monkeypatch):
    """THE race fix: three prompts on ONE session must run one-at-a-time —
    peak concurrency == 1. Without the lock this would be 3 (concurrent
    astream on one thread_id + a racy shared _cancelled flag)."""
    state = {"active": 0, "peak": 0}

    async def fake_super(self, prompt, session_id, message_id=None, **kwargs):
        state["active"] += 1
        state["peak"] = max(state["peak"], state["active"])
        await asyncio.sleep(0.05)  # force overlap IF serialization is absent
        state["active"] -= 1
        return PromptResponse(stop_reason="end_turn")

    monkeypatch.setattr(AgentServerACP, "prompt", fake_super)
    server = _make_server()

    async def _run() -> None:
        await asyncio.gather(*[server.prompt(_ns(), "sess-1") for _ in range(3)])

    asyncio.run(_run())

    assert state["peak"] == 1, (
        f"same-session prompts overlapped (peak={state['peak']}); the per-session "
        f"lock failed → concurrent astream + racy _cancelled (the 'async broken' bug)"
    )


def test_prompt_parallel_across_sessions(monkeypatch):
    """The lock is keyed by session_id — different sessions must STILL run in
    parallel (peak == 2). Serializing globally would be a regression."""
    state = {"active": 0, "peak": 0}

    async def fake_super(self, prompt, session_id, message_id=None, **kwargs):
        state["active"] += 1
        state["peak"] = max(state["peak"], state["active"])
        await asyncio.sleep(0.05)
        state["active"] -= 1
        return PromptResponse(stop_reason="end_turn")

    monkeypatch.setattr(AgentServerACP, "prompt", fake_super)
    server = _make_server()

    async def _run() -> None:
        await asyncio.gather(
            server.prompt(_ns(), "sess-A"),
            server.prompt(_ns(), "sess-B"),
        )

    asyncio.run(_run())

    assert state["peak"] == 2, (
        f"different-session prompts did NOT run in parallel (peak={state['peak']}); "
        f"the lock is too coarse — it serialized across sessions (a regression)"
    )


def test_prompt_cancelled_while_queued_returns_cancelled(monkeypatch):
    """A ``session/cancel`` that lands while a prompt is queued behind another
    is honored: the prompt returns ``stop_reason='cancelled'`` WITHOUT running
    the agent (the base ``prompt`` would reset ``_cancelled=False`` at its top
    and run anyway — losing the cancel)."""
    ran = {"v": False}

    async def fake_super(self, prompt, session_id, message_id=None, **kwargs):
        ran["v"] = True
        return PromptResponse(stop_reason="end_turn")

    monkeypatch.setattr(AgentServerACP, "prompt", fake_super)
    server = _make_server()
    server._cancelled = True  # cancel arrived before we acquired the lock

    resp = asyncio.run(server.prompt(_ns(), "sess-1"))

    assert resp.stop_reason == "cancelled"
    assert ran["v"] is False, "the agent ran despite a pending cancel"
