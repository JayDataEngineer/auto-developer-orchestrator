"""Tests for pux_harness.agent.graph — the per-org graph builder.

All tests mock Docker, models, and subagents; no real container or LLM needed.
"""
from __future__ import annotations

from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from pux_harness.agent import graph as graph_mod


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


# --- _log_rubric_evaluation ---------------------------------------------------


def test_log_rubric_evaluation_prints(capsys):
    graph_mod._log_rubric_evaluation({
        "iteration": 1,
        "result": "satisfied",
        "explanation": "All tests pass",
    })
    captured = capsys.readouterr()
    assert "satisfied" in captured.out
    assert "All tests pass" in captured.out


# --- build_graph ---------------------------------------------------------------


def test_build_graph_wires_everything(monkeypatch):
    """Verify build_graph calls all the right builders with correct args."""
    mock_model = MagicMock()
    mock_exec = MagicMock()
    mock_backend = MagicMock()
    mock_specialists = [MagicMock(name="spec1"), MagicMock(name="spec2")]
    mock_ctx_tools = [MagicMock(name="ctx1")]
    mock_subagents = [MagicMock(name="sub1")]
    mock_prompt = "You are the CTO."
    mock_memory_backend = MagicMock(name="memory_backend_factory")
    mock_memory_store = MagicMock(name="memory_store")

    monkeypatch.setattr(graph_mod, "get_model", lambda role, org: mock_model)
    monkeypatch.setattr(graph_mod, "shared_exec", lambda: mock_exec)
    monkeypatch.setattr(graph_mod, "shared_backend", lambda: mock_backend)
    monkeypatch.setattr(graph_mod, "build_native_specialists",
                        lambda exec_client, vision_model=None, org=None, backend=None: mock_specialists)
    monkeypatch.setattr(graph_mod, "build_ctx_tools", lambda: mock_ctx_tools)
    monkeypatch.setattr(graph_mod, "build_event_tools", lambda: [])
    monkeypatch.setattr(graph_mod, "build_system_prompt", lambda org: mock_prompt)
    monkeypatch.setattr(graph_mod, "load_profile", lambda org: None)
    monkeypatch.setattr(graph_mod, "load_subagents",
                        lambda org, specialists, profile=None: mock_subagents)
    monkeypatch.setattr(graph_mod, "load_rubric_gate", lambda org: None)
    monkeypatch.setattr(graph_mod, "RubricMiddleware", MagicMock)
    monkeypatch.setattr(graph_mod, "build_memory_backend",
                        lambda org, default_backend, store=None: (mock_memory_backend, mock_memory_store))
    monkeypatch.setattr(graph_mod, "MEMORY_SOURCES", ["/memories/AGENTS.md"])

    mock_create = MagicMock(return_value=MagicMock(name="compiled_graph"))
    monkeypatch.setattr(graph_mod, "create_deep_agent", mock_create)

    checkpointer = MagicMock(name="checkpointer")
    result = graph_mod.build_graph("general", checkpointer=checkpointer)

    mock_create.assert_called_once()
    call_kwargs = mock_create.call_args
    assert call_kwargs.kwargs["model"] is mock_model
    assert call_kwargs.kwargs["system_prompt"] == mock_prompt
    assert call_kwargs.kwargs["checkpointer"] is checkpointer
    assert call_kwargs.kwargs["backend"] is mock_memory_backend
    assert call_kwargs.kwargs["store"] is mock_memory_store
    assert call_kwargs.kwargs["memory"] == ["/memories/AGENTS.md"]
    assert call_kwargs.kwargs["subagents"] is mock_subagents


def test_build_graph_applies_profile_suffix(monkeypatch):
    """Profile suffix is appended to the system prompt."""
    monkeypatch.setattr(graph_mod, "get_model", lambda role, org: MagicMock())
    monkeypatch.setattr(graph_mod, "shared_exec", lambda: MagicMock())
    monkeypatch.setattr(graph_mod, "shared_backend", lambda: MagicMock())
    monkeypatch.setattr(graph_mod, "build_native_specialists",
                        lambda exec_client, vision_model=None, org=None, backend=None: [])
    monkeypatch.setattr(graph_mod, "build_ctx_tools", lambda: [])
    monkeypatch.setattr(graph_mod, "build_event_tools", lambda: [])
    monkeypatch.setattr(graph_mod, "build_system_prompt", lambda org: "Base prompt")
    monkeypatch.setattr(graph_mod, "load_subagents", lambda org, sp, profile=None: [])

    fake_profile = SimpleNamespace(
        base_system_prompt="",
        system_prompt_suffix="Custom suffix here",
        tool_description_overrides=None,
        excluded_tools=None,
    )
    monkeypatch.setattr(graph_mod, "load_profile", lambda org: fake_profile)
    monkeypatch.setattr(graph_mod, "load_rubric_gate", lambda org: None)
    monkeypatch.setattr(graph_mod, "build_memory_backend",
                        lambda org, default_backend, store=None: (MagicMock(), None))
    monkeypatch.setattr(graph_mod, "MEMORY_SOURCES", ["/memories/AGENTS.md"])

    mock_create = MagicMock(return_value=MagicMock())
    monkeypatch.setattr(graph_mod, "create_deep_agent", mock_create)

    graph_mod.build_graph("general", checkpointer=MagicMock())

    call_kwargs = mock_create.call_args.kwargs
    assert "Custom suffix here" in call_kwargs["system_prompt"]


def test_build_graph_replaces_base_prompt(monkeypatch):
    """Profile with base_system_prompt replaces the default."""
    monkeypatch.setattr(graph_mod, "get_model", lambda role, org: MagicMock())
    monkeypatch.setattr(graph_mod, "shared_exec", lambda: MagicMock())
    monkeypatch.setattr(graph_mod, "shared_backend", lambda: MagicMock())
    monkeypatch.setattr(graph_mod, "build_native_specialists",
                        lambda exec_client, vision_model=None, org=None, backend=None: [])
    monkeypatch.setattr(graph_mod, "build_ctx_tools", lambda: [])
    monkeypatch.setattr(graph_mod, "build_event_tools", lambda: [])
    monkeypatch.setattr(graph_mod, "build_system_prompt", lambda org: "Default")
    monkeypatch.setattr(graph_mod, "load_subagents", lambda org, sp, profile=None: [])

    fake_profile = SimpleNamespace(
        base_system_prompt="REPLACED prompt",
        system_prompt_suffix="",
        tool_description_overrides=None,
        excluded_tools=None,
    )
    monkeypatch.setattr(graph_mod, "load_profile", lambda org: fake_profile)
    monkeypatch.setattr(graph_mod, "load_rubric_gate", lambda org: None)
    monkeypatch.setattr(graph_mod, "build_memory_backend",
                        lambda org, default_backend, store=None: (MagicMock(), None))
    monkeypatch.setattr(graph_mod, "MEMORY_SOURCES", ["/memories/AGENTS.md"])

    mock_create = MagicMock(return_value=MagicMock())
    monkeypatch.setattr(graph_mod, "create_deep_agent", mock_create)

    graph_mod.build_graph("general", checkpointer=MagicMock())

    call_kwargs = mock_create.call_args.kwargs
    assert call_kwargs["system_prompt"] == "REPLACED prompt"
