"""Rubric injection wiring (Phase 17.B.4) — the prepare-wiring-e2e-gap proof.

Drives the REAL ``server._execute`` + ``main._run`` entry points (NOT a helper)
and asserts the rubric lands on the deepagents invoke state. This is the seam
[[feedback_prepare_wiring_e2e_gap]] demands: a wiring change proven only by an
isolated unit test of a helper is unproven — the injection must be driven
through the actual call path the operator + the CLI hit.

What must hold for an org that opted into the gate (``dev-bot``):
- a bare task string (the default ``pux dispatch`` / ``pux direct`` path) -> the
  harness injects the org's shipped ``rubric.default``, arming
  ``RubricMiddleware``;
- an operator ``--rubric`` override -> that text wins; the default is NOT
  injected (the override is authoritative);
- a no-gate org (``general`` — no ``profile.yaml``) -> NO ``rubric`` key on
  state at all (byte-identical regression; the middleware stays a no-op).

Heavy deps (graph build, Docker exec, the AsyncSqliteSaver lifespan, prep jobs)
are stubbed; the injection logic under test runs for real.
"""
from __future__ import annotations

import asyncio
import types
from typing import Any

from pux_harness import main, server
from pux_harness.agent.profile import default_rubric


class _CapturingGraph:
    """Stub compiled graph: records the invoke state, returns a one-message
    result so ``_run``'s ``messages[-1]`` + ``backend.execute_log`` paths
    complete without touching Docker or a real model."""

    def __init__(self) -> None:
        self.captured: dict[str, Any] = {}

    async def ainvoke(self, state: dict, config: Any = None) -> dict[str, Any]:
        self.captured["state"] = state
        self.captured["config"] = config
        # A minimal final message — _run reads .content off messages[-1].
        msg = types.SimpleNamespace(content="ok", type="ai", name="", tool_calls=None)
        return {"messages": [msg]}


# --- server._execute (the `pux dispatch` / Agent Protocol path) -------------

def test_execute_injects_default_rubric_for_gated_org(monkeypatch):
    """dev-bot (gate enabled) + a bare task string -> the server injects the
    org's shipped default rubric onto invoke state. This is what arms
    RubricMiddleware for an opted-in org without operator coaching."""
    g = _CapturingGraph()
    monkeypatch.setattr(server, "_get_graph", lambda org: g)

    asyncio.run(server._execute("dev-bot", "t1", "do the task", 60))

    state = g.captured["state"]
    assert state["rubric"] == default_rubric("dev-bot")
    # the task still flows through as the user message
    assert state["messages"][0]["content"] == "do the task"


def test_execute_rubric_override_wins(monkeypatch):
    """A caller-supplied ``rubric`` key (the ``--rubric`` path; ``_normalize_input``
    passes a messages-dict through as-is) is authoritative — the org default is
    NOT injected on top of it."""
    g = _CapturingGraph()
    monkeypatch.setattr(server, "_get_graph", lambda org: g)

    raw = {"messages": [{"role": "user", "content": "x"}], "rubric": "MINE"}
    asyncio.run(server._execute("dev-bot", "t1", raw, 60))

    assert g.captured["state"]["rubric"] == "MINE"


def test_execute_no_rubric_for_ungated_org(monkeypatch):
    """general (no profile.yaml, no rubric block) -> NO rubric key on state.
    Byte-identical to today; RubricMiddleware stays a no-op even if mounted."""
    g = _CapturingGraph()
    monkeypatch.setattr(server, "_get_graph", lambda org: g)

    asyncio.run(server._execute("general", "t1", "do the task", 60))

    assert "rubric" not in g.captured["state"]


# --- main._run (the `pux direct` / in-process path) -------------------------

def _stub_run_deps(monkeypatch, graph: _CapturingGraph) -> None:
    """Stub _run's heavy deps: skip the graph build, the prep jobs, and Docker."""
    backend = types.SimpleNamespace(execute_log=[])
    monkeypatch.setattr(main, "_build_agent", lambda org, mcp_tools=None: (graph, backend))
    monkeypatch.setattr(main, "shared_exec", lambda: None)
    # `_run` does `from pux_harness.sandbox.container import prepare` at call
    # time, so patching the module attr is what reaches it.
    import pux_harness.sandbox.container as container  # noqa: PLC0415

    monkeypatch.setattr(container, "prepare", lambda org, exec_client=None: [])


def test_run_injects_default_rubric(monkeypatch):
    """main._run (the `pux direct` path) arms the gate with the org default when
    the operator passed no --rubric."""
    g = _CapturingGraph()
    _stub_run_deps(monkeypatch, g)

    asyncio.run(main._run("dev-bot", "do the task", 60))

    assert g.captured["state"]["rubric"] == default_rubric("dev-bot")


def test_run_rubric_override_wins(monkeypatch):
    """--rubric override reaches invoke state verbatim; the default is not
    injected."""
    g = _CapturingGraph()
    _stub_run_deps(monkeypatch, g)

    asyncio.run(main._run("dev-bot", "do the task", 60, rubric="MINE"))

    assert g.captured["state"]["rubric"] == "MINE"


def test_run_no_rubric_for_ungated_org(monkeypatch):
    """A no-gate org gets no rubric key on state (byte-identical regression)."""
    g = _CapturingGraph()
    _stub_run_deps(monkeypatch, g)

    asyncio.run(main._run("general", "do the task", 60))

    assert "rubric" not in g.captured["state"]
