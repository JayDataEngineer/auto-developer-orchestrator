"""Tests for pux_harness.agent.graph — the per-org graph builder.

``graph.build_graph`` is now THIN. It owns the runtime DEPS (the
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
    """Reset the process-wide sandbox singleton (``pux_harness.sandbox.exec``)
    between tests — ``graph.shared_backend`` delegates to it."""
    import pux_harness.sandbox.exec as _exec_mod
    _exec_mod._backend = None
    yield
    _exec_mod._backend = None


# --- shared_backend (the process-wide BaseSandbox singleton) -----------------


def test_shared_backend_creates_once():
    """One ``BaseSandbox`` per process: ``graph.shared_backend`` delegates to
    ``pux_harness.sandbox.exec.shared_backend`` (lazy — OpenShell or local);
    two calls yield the SAME object and the constructor runs once."""
    with patch("pux_harness.sandbox.exec._make_backend") as mock_make:
        mock_make.return_value = MagicMock(name="backend")
        b1 = graph_mod.shared_backend()
        b2 = graph_mod.shared_backend()
        assert b1 is b2
        mock_make.assert_called_once()


# --- build_graph: the thin delegation ---------------------------------------


def test_build_graph_delegates_to_stack_and_threads_plan(monkeypatch):
    """build_graph builds the deps, hands them to ``build_stack``, and threads
    the resolved ``StackPlan`` straight into ``create_deep_agent`` alongside
    the memory backend + checkpointer. It does NO assembly of its own."""
    mock_model = MagicMock(name="base_model")
    mock_backend = MagicMock(name="backend")
    mock_specialists = [MagicMock(name="spec1"), MagicMock(name="spec2")]
    mock_memory_backend = MagicMock(name="memory_backend")
    mock_memory_store = MagicMock(name="memory_store")

    fake_plan = StackPlan(
        supervisor_tools=[MagicMock(name="tool1")],
        supervisor_middleware=[MagicMock(name="mw1")],
        supervisor_prompt="You are the CTO.",
        subagents=[MagicMock(name="sub1")],
        supervisor_skills=[],  # empty -> skills=None (no SkillsMiddleware)
    )

    monkeypatch.setattr(graph_mod, "get_model", lambda *a, **k: mock_model)
    monkeypatch.setattr(graph_mod, "shared_backend", lambda: mock_backend)
    monkeypatch.setattr(graph_mod, "make_specialist_tools",
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
    assert captured_stack_kwargs["sandbox"] is mock_backend

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
    # supervisor skills threaded through; empty plan -> None (no
    # SkillsMiddleware mounted, byte-identical to a no-skills org).
    assert kw["skills"] is None
