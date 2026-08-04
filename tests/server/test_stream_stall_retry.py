"""Stream-stall node-level RetryPolicy — the LangGraph-native layer that
retries a stalled model node IN-PLACE, preserving all prior-node work.

The mechanic (proven in /tmp/proof_node_level_retry.py):
  - LangGraph's default retry_on EXCLUDES asyncio.TimeoutError (because in
    Python 3.10+ asyncio.TimeoutError IS TimeoutError IS a subclass of
    OSError, which the default predicate explicitly skips).
  - langchain-openai's StreamChunkTimeoutError (the concrete class raised
    after stream_chunk_timeout seconds of zero SSE chunks) subclasses
    asyncio.TimeoutError. So out-of-the-box, LangGraph does NOT retry it.
  - attach_stream_stall_retry attaches a RetryPolicy(retry_on=...) to the
    'model' node that DOES classify asyncio.TimeoutError as retryable.
  - On retry, the model node re-executes from its OWN beginning; nodes
    that completed before the stall are NOT re-run (checkpointer).

These tests prove the WIRING (build_graph attaches the policy to the
right node with the right shape and classifier). The mechanic itself is
LangGraph's responsibility and is verified in the standalone proof at
/tmp/proof_node_level_retry.py.
"""
from __future__ import annotations

import asyncio

import pytest
from deepagents import create_deep_agent
from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from langgraph.checkpoint.memory import MemorySaver
from langgraph.types import RetryPolicy

from pux_harness.agent.retry import (
    attach_stream_stall_retry,
    retry_on_stream_stall,
    stream_stall_retry_policy,
)


class _FakeModel(BaseChatModel):
    """Bare-minimum model so create_deep_agent can construct the graph."""
    @property
    def _llm_type(self) -> str: return "fake"
    def _generate(self, messages, stop=None, run_manager=None, **kwargs):
        return ChatResult(generations=[ChatGeneration(message=AIMessage(content="ok"))])
    def bind_tools(self, tools, **kwargs): return self


def _build_test_graph():
    return create_deep_agent(
        model=_FakeModel(), system_prompt="test", tools=[], checkpointer=MemorySaver(),
    )


# ---------------------------------------------------------------------------
# Classifier — single source of truth
# ---------------------------------------------------------------------------

def test_classifier_retries_transient_stalls():
    """The concrete exception types a real upstream stall surfaces."""
    assert retry_on_stream_stall(asyncio.TimeoutError("stall")) is True
    assert retry_on_stream_stall(ConnectionError("reset")) is True

    # langchain-openai's class names — checked by-name so we don't take a
    # hard dep on langchain-openai in this test file.
    Stream = type("APITimeoutError", (Exception,), {})
    assert retry_on_stream_stall(Stream("timeout")) is True

    Stream2 = type("StreamChunkTimeoutError", (asyncio.TimeoutError,), {})
    assert retry_on_stream_stall(Stream2()) is True  # subclass of TimeoutError


def test_classifier_skips_deterministic_errors():
    """Deterministic errors must NOT be retried — they won't change shape."""
    assert retry_on_stream_stall(TypeError("'NoneType'...")) is False
    assert retry_on_stream_stall(ValueError("bad input")) is False
    assert retry_on_stream_stall(AttributeError("'str' has no attr 'get'")) is False
    assert retry_on_stream_stall(KeyError("missing")) is False
    # Plain BadRequest (no 'stream'/'timeout' in message) is deterministic.
    Bad = type("BadRequestError", (Exception,), {})
    assert retry_on_stream_stall(Bad("invalid model: foo")) is False


def test_classifier_does_not_retry_sandbox_exec_timeout():
    """REGRESSION: the sandbox's own ExecTimeout is a deterministic tool-side
    timeout, NOT a model-stream stall — must NOT be retried.

    The ExecTimeout message ("exec timed out after 120s: ...") contains both
    "timed out" and "timeout", which the substring fallback at the bottom of
    retry_on_stream_stall would otherwise catch. That sent LangGraph into
    4 × 120s of useless retries of the SAME tool call (each one hitting the
    same wall-clock budget) before surfacing the misleading "⚠️ model stream
    stalled (ExecTimeout)" banner. Tool timeouts are deterministic; retrying
    cannot help.

    Defense-in-depth: even though ctx_execute / ctx_execute_file /
    ctx_batch_execute / ctx_fetch_and_index all catch ExecTimeout at the
    tool boundary now, other surfaces (raw fs scripts, dynamic tools,
    browser tools) may still let it escape. The classifier is the SINGLE
    gate — pin it here.
    """
    from pux_harness.sandbox.docker_exec import ExecTimeout
    exc = ExecTimeout("exec timed out after 120s: 'node -e ...'")
    assert retry_on_stream_stall(exc) is False, (
        "ExecTimeout is a deterministic tool timeout, not a model stream "
        "stall — must NOT be retried (would waste 4 × 120s)."
    )

    # Subclasses shouldn't be retried either (defense against future wrappers).
    class _WrappedExecTimeout(ExecTimeout):
        pass
    assert retry_on_stream_stall(_WrappedExecTimeout("wrapped timeout")) is False

    # A bare Exception whose MESSAGE happens to contain "timed out" is
    # ambiguous — the classifier keeps the existing substring behavior for
    # model-layer exceptions it can't identify by type. Only the concrete
    # ExecTimeout type is excluded.
    assert retry_on_stream_stall(Exception("upstream read timed out")) is True



def test_classifier_retries_badrequest_with_stream_message():
    """Provider streams sometimes surface transient stalls as
    ``bad_request: stream stalled`` — those MUST retry."""
    Bad = type("BadRequestError", (Exception,), {})
    assert retry_on_stream_stall(Bad("stream stalled")) is True
    assert retry_on_stream_stall(Bad("upstream read timeout")) is True


# ---------------------------------------------------------------------------
# Policy factory
# ---------------------------------------------------------------------------

def test_stream_stall_retry_policy_shape():
    p = stream_stall_retry_policy()
    assert isinstance(p, RetryPolicy)
    assert p.max_attempts == 4
    assert p.initial_interval == 2.0
    assert p.backoff_factor == 2.0
    assert p.jitter is True
    assert callable(p.retry_on)
    # The classifier wired into the policy IS the shared one.
    assert p.retry_on(asyncio.TimeoutError("x")) is True


# ---------------------------------------------------------------------------
# Wiring — attach_stream_stall_retry + build_graph
# ---------------------------------------------------------------------------

def test_attach_to_model_node_only_by_default():
    """The stall originates in the model node; that's the default target.
    Other nodes (tools, middleware) keep their existing retry_policy."""
    g = _build_test_graph()
    assert g.nodes["model"].retry_policy is None  # before
    other_before = g.nodes["tools"].retry_policy
    attach_stream_stall_retry(g)
    assert g.nodes["model"].retry_policy is not None
    # We did NOT touch unrelated nodes.
    assert g.nodes["tools"].retry_policy == other_before


def test_attach_wraps_policy_in_a_list():
    """THE bug from /tmp/proof_deepagents_node_retry.py: assigning a bare
    RetryPolicy (a NamedTuple) makes langgraph's runtime iterate its FIELDS
    (initial_interval, backoff_factor, ...) as if they were policies, which
    crashes _should_retry_on with AttributeError: 'float' object has no
    attribute 'retry_on'. Must be wrapped in a list."""
    g = _build_test_graph()
    attach_stream_stall_retry(g)
    policy = g.nodes["model"].retry_policy
    assert isinstance(policy, list), (
        f"retry_policy must be a LIST — bare RetryPolicy crashes langgraph "
        f"runtime (iterates NamedTuple fields). Got {type(policy).__name__}"
    )
    assert len(policy) == 1
    assert isinstance(policy[0], RetryPolicy)


def test_attach_wires_classifier_that_retries_timeout():
    """The wired policy MUST classify asyncio.TimeoutError as retryable.
    This is the concrete class langchain-openai's StreamChunkTimeoutError
    subclasses — the actual trigger of the user-visible '⚠️ This turn
    ended early' symptom."""
    g = _build_test_graph()
    attach_stream_stall_retry(g)
    policy = g.nodes["model"].retry_policy[0]
    assert policy.retry_on(asyncio.TimeoutError("stall")) is True, (
        "model-node retry_policy MUST retry asyncio.TimeoutError — the "
        "StreamChunkTimeoutError subclass that fires after 120s of zero "
        "SSE chunks"
    )


def test_attach_is_idempotent():
    """Re-attaching replaces; does not accumulate."""
    g = _build_test_graph()
    attach_stream_stall_retry(g)
    attach_stream_stall_retry(g)
    attach_stream_stall_retry(g)
    assert len(g.nodes["model"].retry_policy) == 1, (
        "re-attach must replace, not accumulate"
    )


def test_attach_skips_missing_nodes_silently():
    """A no-skills org has a different node set; attach must not crash on
    a missing node name."""
    class _NoModelGraph:
        nodes = {"only_one": object()}
    g = _NoModelGraph()
    # Must not raise.
    attach_stream_stall_retry(g)


# ---------------------------------------------------------------------------
# END-TO-END — real graph + real checkpointer + real ACP entrypoint
# ---------------------------------------------------------------------------

@pytest.fixture
def _e2e_sqlite(tmp_path):
    """Real on-disk sqlite checkpointer (the production class)."""
    import aiosqlite
    from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
    db_path = tmp_path / "e2e.sqlite"

    async def _setup():
        conn = await aiosqlite.connect(str(db_path))
        await conn.execute("PRAGMA journal_mode=WAL")
        await conn.execute("PRAGMA busy_timeout=5000")
        saver = AsyncSqliteSaver(conn)
        await saver.setup()
        return conn, saver
    return _setup


class _StallingModel(BaseChatModel):
    """Real BaseChatModel that raises asyncio.TimeoutError (the parent of
    langchain-openai's StreamChunkTimeoutError) on the first 2 calls, then
    succeeds. Counts every call so the test can prove retries fired."""
    state: dict = {"calls": 0}

    @property
    def _llm_type(self) -> str: return "stalling-e2e"

    def bind_tools(self, tools, **kwargs): return self

    def _generate(self, messages, stop=None, run_manager=None, **kwargs):
        self.state["calls"] += 1
        if self.state["calls"] < 3:
            raise asyncio.TimeoutError(
                "No streaming chunk received for 120.0s (chunks_received=0)"
            )
        return ChatResult(generations=[
            ChatGeneration(message=AIMessage(content="Recovered after retry."))
        ])


def test_e2e_real_stall_through_real_acp_entrypoint(tmp_path):
    """THE headline e2e test.

    Builds a real deepagents graph (same wiring as build_graph), uses a real
    on-disk AsyncSqliteSaver (the production class), injects a model that
    raises asyncio.TimeoutError twice then succeeds, then pushes a real
    prompt through _RegisteringAgentServerACP.prompt() — the production ACP
    entrypoint that calls super().prompt() internally.

    Asserts:
      1. The model retried past the stall (3+ calls).
      2. No stall-notice TEXT was emitted across all _log_text calls.
      3. prompt() returned normally (no exception propagated).
      4. Work persisted on disk.
    """
    import asyncio
    from unittest.mock import AsyncMock, MagicMock
    from deepagents import create_deep_agent
    from acp.schema import TextContentBlock
    from pux_harness.acp import _RegisteringAgentServerACP

    async def _run():
        import aiosqlite
        from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver
        db_path = tmp_path / "e2e.sqlite"
        conn = await aiosqlite.connect(str(db_path))
        await conn.execute("PRAGMA journal_mode=WAL")
        await conn.execute("PRAGMA busy_timeout=5000")
        saver = AsyncSqliteSaver(conn); await saver.setup()

        model = _StallingModel()
        graph = create_deep_agent(
            model=model, system_prompt="test", tools=[], checkpointer=saver,
        )
        attach_stream_stall_retry(graph)  # *** production wiring ***

        # Real production ACP entrypoint.
        server = _RegisteringAgentServerACP(agent=graph, store=MagicMock(), org="test")
        server._log_text = AsyncMock()
        server._agent = graph
        server._maybe_warmup_browser = lambda: None

        prompt_blocks = [TextContentBlock(type="text", text="hello")]
        result = await server.prompt(prompt_blocks, "session-e2e")

        # Disk check
        cur = await conn.execute("SELECT COUNT(*) FROM checkpoints WHERE thread_id = ?", ("session-e2e",))
        n_cp = (await cur.fetchone())[0]
        cur = await conn.execute("SELECT COUNT(*) FROM writes WHERE thread_id = ?", ("session-e2e",))
        n_w = (await cur.fetchone())[0]
        await conn.close()
        return result, model.state["calls"], server._log_text.call_args_list, n_cp, n_w

    result, model_calls, log_calls, n_cp, n_w = asyncio.run(_run())

    # 1. Model retried past the stall.
    assert model_calls >= 3, (
        f"retry did not fire — model only called {model_calls}x. Expected ≥3 "
        "(2 stalls + success)."
    )

    # 2. No stall-notice text in any _log_text call.
    stall_markers = ("ended early", "stream stalled", "didn't recover", "Re-send")
    for call in log_calls:
        text = (call.kwargs or {}).get("text", "")
        for marker in stall_markers:
            assert marker not in text, (
                f"stall notice was emitted: {text!r} (marker {marker!r}). "
                "The retry should have made the stall transparent."
            )

    # 3. prompt() returned a PromptResponse (not raised).
    assert result is not None
    assert hasattr(result, "stop_reason")

    # 4. Work persisted on disk.
    assert n_cp > 0 and n_w > 0, (
        f"nothing persisted on disk (cp={n_cp}, writes={n_w})"
    )
