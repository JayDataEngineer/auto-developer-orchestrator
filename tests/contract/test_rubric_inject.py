"""Rubric injection wiring — the prepare-wiring-e2e-gap proof (direct lane).

Drives the REAL ``main._run`` entry point (NOT a helper) and asserts the rubric
lands on the deepagents invoke state. This is the seam [[feedback_prepare_wiring_e2e_gap]]
demands: a wiring change proven only by an isolated unit test of a helper is
unproven — the injection must be driven through the actual call path the CLI hits.

The ``pux serve`` lane (formerly ``server._invoke_once``) was RETIRED with
``server.py`` in Aegra phase D (see ``[[server-py-retired]]``); prod runs on
Aegra, whose runs reach the graph via langgraph-api with no pux entry point to
inject from. So the in-process ``pux direct`` lane — ``main._run`` — is the one
REAL pux entry point left, and the one this proof drives. (The ``RubricMiddleware``
itself is gate-mounted in the graph build, exercised by ``tests/agent/test_stack.py``
+ ``test_graph.py``.)

What must hold for an org that opted into the gate (``dev-bot``):
- a bare task string (the default ``pux direct`` path) -> the harness injects the
  org's shipped ``rubric.default``, arming ``RubricMiddleware``;
- an operator ``--rubric`` override -> that text wins; the default is NOT injected
  (the override is authoritative);
- a no-gate org (``general`` — no ``profile.yaml``) -> NO ``rubric`` key on state
  at all (byte-identical regression; the middleware stays a no-op).

Heavy deps (graph build, Docker exec, the AsyncSqliteSaver lifespan, prep jobs)
are stubbed; the injection logic under test runs for real.
"""
from __future__ import annotations

import asyncio
import types
from typing import Any

from pux_harness import main
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


# --- main._run (the `pux direct` / in-process path) -------------------------

def _stub_run_deps(monkeypatch, tmp_path, graph: _CapturingGraph) -> None:
    """Stub _run's heavy deps: skip the graph build, the prep jobs, and Docker.

    ``_run`` now opens the shared thread store, so point PUX_API_DB at
    a tmp file (hermetic — never the operator's .pux/) and accept the new
    ``saver=`` kwarg on the _build_agent stub."""
    import pux_harness.threads as threads_mod  # noqa: PLC0415

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "rubric.sqlite")
    backend = types.SimpleNamespace(execute_log=[])
    monkeypatch.setattr(
        main, "_build_agent",
        lambda org, saver=None, mcp_tools=None: (graph, backend))
    monkeypatch.setattr(main, "shared_exec", lambda: None)
    # `_run` does `from pux_harness.sandbox.container import prepare` at call
    # time, so patching the module attr is what reaches it.
    import pux_harness.sandbox.container as container  # noqa: PLC0415

    monkeypatch.setattr(container, "prepare", lambda org, exec_client=None: [])
    # `_run` imports open_org_mcp at call time; patch it so no real MCP
    # server connection is attempted (hermetic).
    async def _no_mcp(org):
        return []

    import pux_harness.agent.mcp_client as mcp_client  # noqa: PLC0415
    monkeypatch.setattr(mcp_client, "open_org_mcp", _no_mcp)


def test_run_injects_default_rubric(monkeypatch, tmp_path):
    """main._run (the `pux direct` path) arms the gate with the org default when
    the operator passed no --rubric."""
    g = _CapturingGraph()
    _stub_run_deps(monkeypatch, tmp_path, g)

    asyncio.run(main._run("coder", "do the task", 60))

    assert g.captured["state"]["rubric"] == default_rubric("coder")


def test_run_rubric_override_wins(monkeypatch, tmp_path):
    """--rubric override reaches invoke state verbatim; the default is not
    injected."""
    g = _CapturingGraph()
    _stub_run_deps(monkeypatch, tmp_path, g)

    asyncio.run(main._run("coder", "do the task", 60, rubric="MINE"))

    assert g.captured["state"]["rubric"] == "MINE"


def test_run_no_rubric_for_ungated_org(monkeypatch, tmp_path):
    """A no-gate org gets no rubric key on state (byte-identical regression)."""
    g = _CapturingGraph()
    _stub_run_deps(monkeypatch, tmp_path, g)

    asyncio.run(main._run("general", "do the task", 60))

    assert "rubric" not in g.captured["state"]
