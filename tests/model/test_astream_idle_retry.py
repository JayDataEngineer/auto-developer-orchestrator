"""PROVES the mid-stream stall recovery in ``ReasoningChatOpenAI.astream``.

The bug (locked here, reproduced at the model layer with NO graph): on long
large-context generations the provider intermittently goes silent mid-stream —
chunks flow at ≤9s gaps, then the TCP connection stays open but ZERO SSE bytes
arrive. With langchain's ``stream_chunk_timeout`` DISABLED (its
``StreamChunkTimeoutError`` is raised outside the SDK retry loop and would
escape → editor freeze), nothing caught the dead stream → a 180s freeze.

The fix is an idle watchdog + transparent pre-output retry in
``ReasoningChatOpenAI.astream``. These tests prove the four cases by simulating
dead streams (a source that yields N chunks then blocks forever) — NO network.
``ChatOpenAI.astream`` (the ``super()`` target) is monkeypatched to return
controlled async iterators per attempt.
"""
from __future__ import annotations

import asyncio
from typing import Any

import pytest
from langchain_openai import ChatOpenAI

from pux_harness.agent.reasoning import ReasoningChatOpenAI, _StreamIdle


def _model() -> ReasoningChatOpenAI:
    # dummy construct — no network call is made (super().astream is patched)
    return ReasoningChatOpenAI(
        model="x", base_url="http://x", api_key="x", timeout=1, max_retries=0
    )


async def _yield_then_stall(chunks: list[Any]) -> Any:
    """Yield the given chunks, then block forever (a dead stream)."""
    for c in chunks:
        yield c
    await asyncio.Event().wait()  # never set → simulates the open-but-dead conn


async def _healthy(chunks: list[Any]) -> Any:
    for c in chunks:
        yield c


def _patch_parent(monkeypatch, factory):
    """Make ``super().astream(...)`` (ChatOpenAI.astream) return ``factory()``."""
    async def _fake_astream(self, *args, **kwargs):
        return factory()
    # astream must be an async function returning an async iterator; LangChain's
    # is an async generator, so wrap accordingly.
    def _astream(self, *args, **kwargs):
        return factory()
    monkeypatch.setattr(ChatOpenAI, "astream", _astream)


@pytest.mark.asyncio
async def test_pre_output_stall_retries_and_recovers(monkeypatch):
    """THE fix: a stall BEFORE any output (reasoning/TTFT phase) is retried
    transparently. Attempt 1 yields nothing then dies; attempt 2 streams
    normally. The caller sees ONE healthy stream — no error, no freeze."""
    monkeypatch.setenv("PUX_STREAM_IDLE_TIMEOUT_S", "0.3")
    monkeypatch.setenv("PUX_STREAM_IDLE_RETRIES", "2")
    calls = {"n": 0}

    def factory():
        calls["n"] += 1
        if calls["n"] == 1:
            return _yield_then_stall([])  # dies before any chunk
        return _healthy(["A", "B", "C"])  # retry succeeds

    _patch_parent(monkeypatch, factory)
    out = []
    async for chunk in _model().astream("ignored"):
        out.append(chunk)
    assert out == ["A", "B", "C"], f"retry should be transparent; got {out}"
    assert calls["n"] == 2, f"exactly one retry; got {calls['n']} attempts"


@pytest.mark.asyncio
async def test_post_output_stall_raises_no_retry(monkeypatch):
    """A stall AFTER chunks were already yielded must NOT retry (retrying would
    duplicate output already shown to the user). It raises ``_StreamIdle`` so
    ``acp.prompt``'s except-clause ends the turn cleanly."""
    monkeypatch.setenv("PUX_STREAM_IDLE_TIMEOUT_S", "0.3")
    monkeypatch.setenv("PUX_STREAM_IDLE_RETRIES", "2")
    calls = {"n": 0}

    def factory():
        calls["n"] += 1
        return _yield_then_stall(["already-shown"])  # yields one, then dies

    _patch_parent(monkeypatch, factory)
    out = []
    with pytest.raises(_StreamIdle):
        async for chunk in _model().astream("ignored"):
            out.append(chunk)
    assert out == ["already-shown"], "the pre-stall chunks must still be yielded"
    assert calls["n"] == 1, "post-output stall must NOT retry"


@pytest.mark.asyncio
async def test_pre_output_stall_exhausting_retries_raises(monkeypatch):
    """If every retry also stalls before output, ``_StreamIdle`` propagates
    (never an infinite retry loop / never a silent hang)."""
    monkeypatch.setenv("PUX_STREAM_IDLE_TIMEOUT_S", "0.2")
    monkeypatch.setenv("PUX_STREAM_IDLE_RETRIES", "2")

    def factory():
        return _yield_then_stall([])  # every attempt dies pre-output

    _patch_parent(monkeypatch, factory)
    with pytest.raises(_StreamIdle):
        async for _ in _model().astream("ignored"):
            pass


@pytest.mark.asyncio
async def test_healthy_stream_no_overhead(monkeypatch):
    """A healthy stream (no idle) completes with zero retries — the watchdog
    never false-fires on a flowing stream."""
    monkeypatch.setenv("PUX_STREAM_IDLE_TIMEOUT_S", "0.3")
    monkeypatch.setenv("PUX_STREAM_IDLE_RETRIES", "2")
    calls = {"n": 0}

    def factory():
        calls["n"] += 1
        return _healthy(["x", "y", "z"])

    _patch_parent(monkeypatch, factory)
    out = [c async for c in _model().astream("ignored")]
    assert out == ["x", "y", "z"]
    assert calls["n"] == 1, "no retry on a healthy stream"


@pytest.mark.asyncio
async def test_disabled_is_raw_passthrough(monkeypatch):
    """``PUX_STREAM_IDLE_TIMEOUT_S=0`` disables the guard → raw super().astream,
    no retry, no idle detection (back-compat / escape hatch)."""
    monkeypatch.setenv("PUX_STREAM_IDLE_TIMEOUT_S", "0")
    calls = {"n": 0}

    def factory():
        calls["n"] += 1
        return _healthy(["only"])

    _patch_parent(monkeypatch, factory)
    out = [c async for c in _model().astream("ignored")]
    assert out == ["only"]
    assert calls["n"] == 1
