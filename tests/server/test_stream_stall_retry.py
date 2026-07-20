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
