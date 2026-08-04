"""Stall recovery — the "Zed freezes forever" fix.

The bug (locked here): when the upstream model stream stalls mid-generation
(TCP alive, zero SSE chunks), langchain-openai raises ``StreamChunkTimeoutError``
— a subclass of ``asyncio.TimeoutError`` — after ``stream_chunk_timeout``
seconds (default 120). That class is NOT an ``openai.APITimeoutError``, so it is
NOT in ``_TRANSIENT_EXCEPTIONS`` and the declared glm-5.2 → glm-5.1 fallback
chain does NOT absorb it. It walks straight out of the base ``prompt`` loop's
un-`try`-ed ``async for stream_chunk in agent.astream(...)``.

The ACP dispatcher runs each ``session/prompt`` on a DETACHED supervisor task,
and the IncomingMessage store only records status (``fail_incoming``) — it holds
no Future to reject and writes nothing to the wire. So the escaped exception is
swallowed by the supervisor's error log, the JSON-RPC ``session/prompt`` RESPONSE
is never sent, and the editor (Zed) spins forever. Symptom: "the response just
freezes forever" + the timeout message only visible in stderr.

The fix: ``_RegisteringAgentServerACP.prompt`` adds a last-resort
``except Exception`` after ``except _AskUserPause`` that logs the failure,
surfaces a short ``session/message`` notice via ``_log_text``, and returns a
terminal ``PromptResponse(stop_reason="end_turn")`` so the turn ALWAYS ends and
the editor un-freezes. ``CancelledError`` (a ``BaseException``) is intentionally
NOT caught — the cancel path must keep propagating.

These tests prove: (1) a stall ends the turn cleanly instead of propagating,
(2) the notice is surfaced to the user, (3) a real cancellation still
propagates (no regression to the cancel path).
"""
from __future__ import annotations

import asyncio
import logging
import types
from unittest.mock import AsyncMock, MagicMock

import pytest
from acp.schema import PromptResponse
from deepagents_acp.server import AgentServerACP

from pux_harness.acp import _AskUserPause, _RegisteringAgentServerACP


@pytest.fixture(autouse=True)
def _fast_retry(monkeypatch):
    """Zero out the retry backoff so the existing + new stall tests don't
    sleep 14s each. The production value (2.0 → 2/4/8s) is exercised only in
    real upstream-provider recovery scenarios, not in unit tests."""
    from pux_harness import acp
    monkeypatch.setattr(acp, "_PROMPT_BACKOFF_BASE", 0.0)


def _make_server() -> _RegisteringAgentServerACP:
    """A server with a mocked agent + store, identical setup to the
    prompt-serialization tests. ``_agent`` truthy → skip _reset_agent."""
    agent = MagicMock()
    agent.aget_state = AsyncMock(return_value=None)
    server = _RegisteringAgentServerACP(
        agent=agent, store=MagicMock(), org="coder"
    )
    server._agent = agent
    return server


def _ns() -> types.SimpleNamespace:
    """A prompt stand-in whose ``.content`` is None → title capture no-ops."""
    return types.SimpleNamespace(content=None)


# ``StreamChunkTimeoutError`` subclasses ``asyncio.TimeoutError`` on Py 3.11+
# (the langchain-openai _StreamChunkTimeoutBases tuple collapses to it), so a
# bare ``asyncio.TimeoutError`` exercises the EXACT same ``except Exception``
# path the real stall hits. Keep this faithful rather than inventing a fake.
def _stall_error() -> Exception:
    return asyncio.TimeoutError(
        "No streaming chunk received for 120.0s (model=glm-5.2, chunks_received=651)"
    )


def test_stall_exception_ends_turn_cleanly(monkeypatch):
    """THE freeze fix: when the base ``prompt`` raises (the stall), our wrapper
    must return a terminal ``PromptResponse(end_turn)`` — NOT propagate. A
    propagated exception orphans the session/prompt request and freezes the
    editor."""

    async def raising_super(self, prompt, session_id, message_id=None, **kwargs):
        raise _stall_error()

    monkeypatch.setattr(AgentServerACP, "prompt", raising_super)
    server = _make_server()
    # Best-effort notice: patch _log_text so it doesn't need a real _conn, and
    # so we can assert it was called.
    server._log_text = AsyncMock()

    result = asyncio.run(server.prompt(_ns(), "sess-stall"))

    assert isinstance(result, PromptResponse)
    assert result.stop_reason == "end_turn", (
        f"a stall must end the turn (got stop_reason={result.stop_reason!r}); "
        f"propagating the exception freezes the editor forever"
    )


def test_stall_surfaces_user_visible_notice(monkeypatch):
    """The user must see WHY the turn ended — a ``session/message`` update via
    ``_log_text`` is emitted before the terminal response. (PromptResponse has
    no body field, so the notice rides on the message stream, not the response.)"""

    async def raising_super(self, prompt, session_id, message_id=None, **kwargs):
        raise _stall_error()

    monkeypatch.setattr(AgentServerACP, "prompt", raising_super)
    server = _make_server()
    server._log_text = AsyncMock()

    asyncio.run(server.prompt(_ns(), "sess-stall"))

    server._log_text.assert_awaited_once()
    _, kwargs = server._log_text.call_args
    assert kwargs.get("session_id") == "sess-stall"
    text = kwargs.get("text", "")
    assert "stalled" in text.lower() or "ended early" in text.lower(), (
        f"notice must explain the stall (got {text!r})"
    )


def test_stall_is_logged_at_error_level(monkeypatch):
    """The full traceback must hit the operator's log so a real bug (vs. a
    benign stall) is diagnosable — not silently swallowed."""

    async def raising_super(self, prompt, session_id, message_id=None, **kwargs):
        raise _stall_error()

    monkeypatch.setattr(AgentServerACP, "prompt", raising_super)
    server = _make_server()
    server._log_text = AsyncMock()

    logger = logging.getLogger("pux.acp")
    records: list[logging.LogRecord] = []

    class _Handler(logging.Handler):
        def emit(self, record):
            records.append(record)

    handler = _Handler(level=logging.DEBUG)
    logger.addHandler(handler)
    prev_level = logger.level
    logger.setLevel(logging.DEBUG)
    try:
        asyncio.run(server.prompt(_ns(), "sess-stall"))
    finally:
        logger.removeHandler(handler)
        logger.setLevel(prev_level)

    exc_records = [r for r in records if r.levelno >= logging.ERROR]
    assert exc_records, "the stall must be logged at ERROR level (not swallowed)"
    assert any("sess-stall" in r.getMessage() for r in exc_records), (
        "the error log must name the affected session"
    )


def test_cancelled_error_still_propagates(monkeypatch):
    """Regression guard: ``asyncio.CancelledError`` is a ``BaseException``, NOT
    an ``Exception`` — so the last-resort ``except Exception`` must NOT swallow
    it. The cancel path (``session/cancel`` → ``stop_reason="cancelled"`` in the
    base loop, or a real task cancellation) must keep propagating."""

    async def cancelled_super(self, prompt, session_id, message_id=None, **kwargs):
        raise asyncio.CancelledError()

    monkeypatch.setattr(AgentServerACP, "prompt", cancelled_super)
    server = _make_server()
    server._log_text = AsyncMock()

    with pytest.raises(asyncio.CancelledError):
        asyncio.run(server.prompt(_ns(), "sess-cancel"))

    # And the notice must NOT have been emitted for a cancellation.
    server._log_text.assert_not_awaited()


def test_ask_user_pause_still_handled_first(monkeypatch):
    """Ordering guard: ``_AskUserPause`` must still be caught by its OWN clause
    (before the catch-all) and return end_turn — the stall handler must not
    shadow the ask_user resume mechanic."""

    async def raising_super(self, prompt, session_id, message_id=None, **kwargs):
        raise _AskUserPause()

    monkeypatch.setattr(AgentServerACP, "prompt", raising_super)
    server = _make_server()
    server._log_text = AsyncMock()

    result = asyncio.run(server.prompt(_ns(), "sess-ask"))

    assert result.stop_reason == "end_turn"
    # The stall notice is NOT emitted on an ask_user pause (only on the
    # catch-all), so _log_text was not called.
    server._log_text.assert_not_awaited()


# ---------------------------------------------------------------------------
# NEW: retry recovers. A transient stall MUST NOT end the turn — the wrapper
# re-enters super().prompt() and LangGraph resumes from the last checkpoint,
# so the user keeps their in-flight work and never sees the stall notice.
# ---------------------------------------------------------------------------

def test_stall_is_retried_then_succeeds(monkeypatch):
    """The headline behavior: a transient stream stall is retried, NOT
    surfaced as an ended turn. The user keeps their work; no notice is
    emitted; the model's actual response is returned."""

    calls = {"n": 0}

    async def flaky_super(self, prompt, session_id, message_id=None, **kwargs):
        calls["n"] += 1
        if calls["n"] < 3:
            raise _stall_error()
        return PromptResponse(stop_reason="end_turn", content=[{"type": "text", "text": "recovered"}])

    monkeypatch.setattr(AgentServerACP, "prompt", flaky_super)
    server = _make_server()
    server._log_text = AsyncMock()

    result = asyncio.run(server.prompt(_ns(), "sess-retry"))

    assert calls["n"] == 3, f"must retry twice then succeed (got {calls['n']} attempts)"
    assert isinstance(result, PromptResponse)
    server._log_text.assert_not_awaited(), (
        "no stall notice should be emitted when retry succeeds — the user must "
        "never see that a stall happened"
    )


def test_deterministic_error_is_not_retried(monkeypatch):
    """A deterministic error (TypeError / ValidationError / AttributeError)
    will not change shape across attempts — retrying only delays the
    inevitable end_turn. Must call super().prompt() EXACTLY ONCE."""

    calls = {"n": 0}

    async def bug_super(self, prompt, session_id, message_id=None, **kwargs):
        calls["n"] += 1
        raise AttributeError("'str' object has no attribute 'get'")

    monkeypatch.setattr(AgentServerACP, "prompt", bug_super)
    server = _make_server()
    server._log_text = AsyncMock()

    result = asyncio.run(server.prompt(_ns(), "sess-bug"))

    assert calls["n"] == 1, (
        f"deterministic error must not be retried (got {calls['n']} calls) — "
        "retrying a TypeError just wastes time"
    )
    assert isinstance(result, PromptResponse)
    assert result.stop_reason == "end_turn"


# ---------------------------------------------------------------------------
# NEW (2026-07-22): notice text honestly describes WHICH layer raised.
# The pre-fix notice unconditionally said "model stream stalled" for EVERY
# unrecoverable exception — sending operators to check provider health when
# the real cause was a tool-side ExecTimeout or an AttributeError in a
# middleware. These tests pin the per-exception-type notice branches.
# ---------------------------------------------------------------------------


def test_exectimeout_notice_does_not_say_stream_stall(monkeypatch):
    """REGRESSION (2026-07-22, session 67f05375..., org coder): an
    ``ExecTimeout`` reached the prompt boundary and surfaced as
    ``⚠️ This turn ended early — the model stream stalled (ExecTimeout)``,
    which is a lie on two counts:
      1. ExecTimeout is a TOOL timeout, not a model stream stall.
      2. The agent's command (recursive ``uv run pux``) never had a chance
         to run — it was killed by ``future.result(timeout=0)`` instantly.

    The notice must now say "tool call hit the sandbox wall-clock timeout"
    and explicitly call out that it is NOT a stream stall.
    """
    from pux_harness.sandbox.docker_exec import ExecTimeout

    async def timeout_super(self, prompt, session_id, message_id=None, **kwargs):
        raise ExecTimeout(
            "exec timed out after 0s: 'cd /sandbox/workspace && uv run pux direct ...'"
        )

    monkeypatch.setattr(AgentServerACP, "prompt", timeout_super)
    server = _make_server()
    server._log_text = AsyncMock()

    asyncio.run(server.prompt(_ns(), "sess-tool-timeout"))

    server._log_text.assert_awaited_once()
    _, kwargs = server._log_text.call_args
    text = kwargs.get("text", "")

    assert "NOT a model stream stall" in text, (
        f"ExecTimeout notice must explicitly disambiguate from a stream stall "
        f"(got {text!r})"
    )
    assert "wall-clock timeout" in text, (
        f"notice must name the actual cause: tool-side wall-clock timeout "
        f"(got {text!r})"
    )
    # Pre-fix misleading text must NOT appear.
    assert "model stream stalled" not in text, (
        f"the misleading 'model stream stalled' phrase must NOT appear on a "
        f"tool-timeout notice (got {text!r})"
    )


def test_unrecoverable_error_notice_names_the_actual_exception(monkeypatch):
    """For unrecoverable non-timeout errors (AttributeError, KeyError,
    ValueError, etc.) the notice must surface the actual exception name +
    message so the operator knows where to look. Pre-fix text just said
    'model stream stalled (AttributeError)' which obscured the cause."""
    calls = {"n": 0}

    async def bug_super(self, prompt, session_id, message_id=None, **kwargs):
        calls["n"] += 1
        raise AttributeError("'str' object has no attribute 'get'")

    monkeypatch.setattr(AgentServerACP, "prompt", bug_super)
    server = _make_server()
    server._log_text = AsyncMock()

    asyncio.run(server.prompt(_ns(), "sess-attr-err"))

    server._log_text.assert_awaited_once()
    _, kwargs = server._log_text.call_args
    text = kwargs.get("text", "")

    assert "AttributeError" in text, (
        f"notice must name the exception type (got {text!r})"
    )
    assert "object has no attribute 'get'" in text, (
        f"notice must surface the exception message (got {text!r})"
    )
    assert "deterministic" in text.lower(), (
        f"notice must say this is deterministic (won't change on retry) so "
        f"the operator knows not to wait/retry (got {text!r})"
    )
    # Pre-fix misleading label must NOT appear.
    assert "model stream stalled" not in text


def test_exhausted_retry_still_says_stream_stall(monkeypatch):
    """Sanity: when the exception IS a real recoverable stream stall that
    genuinely exhausted retries, the notice KEEPS calling it a stream stall.
    The new branching must not regress the original case."""

    async def always_stalls(self, prompt, session_id, message_id=None, **kwargs):
        raise _stall_error()

    monkeypatch.setattr(AgentServerACP, "prompt", always_stalls)
    server = _make_server()
    server._log_text = AsyncMock()

    asyncio.run(server.prompt(_ns(), "sess-stall-exhaust"))

    _, kwargs = server._log_text.call_args
    text = kwargs.get("text", "")
    assert "model stream stalled" in text, (
        f"real stream stalls that exhausted retries must keep the original "
        f"'model stream stalled' wording (got {text!r})"
    )


def test_exhausted_retry_notice_mentions_checkpointed_resume(monkeypatch):
    """When retries truly exhaust, the notice must tell the user the truth:
    their work is checkpointed and re-sending RESUMES — it does not restart.
    The old notice ('Re-send your message to retry') left the user believing
    their in-flight progress was lost."""

    async def always_stalls(self, prompt, session_id, message_id=None, **kwargs):
        raise _stall_error()

    monkeypatch.setattr(AgentServerACP, "prompt", always_stalls)
    server = _make_server()
    server._log_text = AsyncMock()

    asyncio.run(server.prompt(_ns(), "sess-exhaust"))

    server._log_text.assert_awaited_once()
    _, kwargs = server._log_text.call_args
    text = kwargs.get("text", "")
    assert "checkpointed" in text.lower(), (
        f"notice must say work is checkpointed (got {text!r}) — this is the "
        "user's explicit demand: progress must never look lost to a stall"
    )
    assert "resume" in text.lower(), (
        f"notice must say re-send resumes (got {text!r})"
    )
    # Attempts count surfaces so the operator knows how much patience was given.
    from pux_harness import acp
    assert f"{acp._PROMPT_MAX_ATTEMPTS} attempt" in text, (
        f"notice must name the attempt count (got {text!r})"
    )


def test_retry_count_on_exhaustion_matches_max_attempts(monkeypatch):
    """The retry loop must exhaust at exactly _PROMPT_MAX_ATTEMPTS — no more
    (wasted time), no fewer (insufficient patience before surfacing)."""

    calls = {"n": 0}

    async def always_stalls(self, prompt, session_id, message_id=None, **kwargs):
        calls["n"] += 1
        raise _stall_error()

    monkeypatch.setattr(AgentServerACP, "prompt", always_stalls)
    server = _make_server()
    server._log_text = AsyncMock()

    asyncio.run(server.prompt(_ns(), "sess-count"))

    from pux_harness import acp
    assert calls["n"] == acp._PROMPT_MAX_ATTEMPTS, (
        f"expected exactly {acp._PROMPT_MAX_ATTEMPTS} attempts, got {calls['n']}"
    )
