"""Tests for pux_harness.agent.graph — the per-org graph builder.

Phase 21: ``graph.build_graph`` is now THIN. It owns the runtime DEPS (the
model, the specialist tools, the loaded profile + rubric gate, the memory
backend, the checkpointer) and the final BINDING (``create_deep_agent``); it
does NO stack assembly — every middleware, the prompt, the tool list, and the
subagents are resolved by the factory ``stack.build_stack``. These tests pin
that thin shape: the singleton clients, the delegation to ``build_stack``, and
the plan → ``create_deep_agent`` threading.

All tests mock Docker, models, and the factory; no real container or LLM
needed. The factory's own resolution logic is tested in ``test_stack.py``; the
end-to-end wiring (build_graph → build_stack → create_deep_agent against a real
org tree) in ``test_profile.py``.
"""
from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest

from pux_harness.agent import graph as graph_mod
from pux_harness.agent.stack import StackPlan


@pytest.fixture(autouse=True)
def reset_singletons():
    """Reset module-level singletons between tests."""
    graph_mod._exec = None
    graph_mod._backend = None
    yield
    graph_mod._exec = None
    graph_mod._backend = None


# --- shared_exec / shared_backend ---------------------------------------------


def test_shared_exec_creates_once():
    with patch("pux_harness.agent.graph.get_exec_client") as mock_get:
        mock_get.return_value = MagicMock()
        e1 = graph_mod.shared_exec()
        e2 = graph_mod.shared_exec()
        assert e1 is e2
        mock_get.assert_called_once()


def test_shared_backend_creates_once():
    with patch("pux_harness.agent.graph.get_exec_client") as mock_get:
        mock_get.return_value = MagicMock()
        b1 = graph_mod.shared_backend()
        b2 = graph_mod.shared_backend()
        assert b1 is b2


# --- build_graph: the thin delegation (Phase 21) -----------------------------


def test_build_graph_delegates_to_stack_and_threads_plan(monkeypatch):
    """build_graph builds the deps, hands them to ``build_stack``, and threads
    the resolved ``StackPlan`` straight into ``create_deep_agent`` alongside
    the memory backend + checkpointer. It does NO assembly of its own."""
    mock_model = MagicMock(name="base_model")
    mock_exec = MagicMock(name="exec")
    mock_backend = MagicMock(name="backend")
    mock_specialists = [MagicMock(name="spec1"), MagicMock(name="spec2")]
    mock_memory_backend = MagicMock(name="memory_backend")
    mock_memory_store = MagicMock(name="memory_store")

    fake_plan = StackPlan(
        supervisor_tools=[MagicMock(name="tool1")],
        supervisor_middleware=[MagicMock(name="mw1")],
        supervisor_prompt="You are the CTO.",
        subagents=[MagicMock(name="sub1")],
    )

    monkeypatch.setattr(graph_mod, "get_model", lambda *a, **k: mock_model)
    monkeypatch.setattr(graph_mod, "shared_exec", lambda: mock_exec)
    monkeypatch.setattr(graph_mod, "shared_backend", lambda: mock_backend)
    monkeypatch.setattr(graph_mod, "build_native_specialists",
                        lambda *a, **k: mock_specialists)
    monkeypatch.setattr(graph_mod, "load_profile", lambda org: None)
    monkeypatch.setattr(graph_mod, "load_rubric_gate", lambda org: None)

    captured_stack_kwargs: dict = {}

    def _fake_build_stack(org, **kwargs):
        captured_stack_kwargs.update(kwargs)
        return fake_plan

    monkeypatch.setattr(graph_mod, "build_stack", _fake_build_stack)
    monkeypatch.setattr(graph_mod, "build_memory_backend",
                        lambda *a, **k: (mock_memory_backend, mock_memory_store))
    monkeypatch.setattr(graph_mod, "MEMORY_SOURCES", ["/memories/AGENTS.md"])

    mock_create = MagicMock(return_value=MagicMock(name="compiled_graph"))
    monkeypatch.setattr(graph_mod, "create_deep_agent", mock_create)

    checkpointer = MagicMock(name="checkpointer")
    graph_mod.build_graph("general", checkpointer=checkpointer)

    # build_stack got the resolved deps (the factory owns assembly from here).
    assert captured_stack_kwargs["specialists"] is mock_specialists
    assert captured_stack_kwargs["profile"] is None
    assert captured_stack_kwargs["rubric_gate"] is None
    assert captured_stack_kwargs["exec_client"] is mock_exec

    # create_deep_agent got the plan's fields verbatim + memory + checkpointer.
    mock_create.assert_called_once()
    kw = mock_create.call_args.kwargs
    assert kw["model"] is mock_model
    assert kw["system_prompt"] is fake_plan.supervisor_prompt
    assert kw["tools"] is fake_plan.supervisor_tools
    assert kw["middleware"] is fake_plan.supervisor_middleware
    assert kw["subagents"] is fake_plan.subagents
    assert kw["backend"] is mock_memory_backend
    assert kw["store"] is mock_memory_store
    assert kw["checkpointer"] is checkpointer
    assert kw["memory"] == ["/memories/AGENTS.md"]
