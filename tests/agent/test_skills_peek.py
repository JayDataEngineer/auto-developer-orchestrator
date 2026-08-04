"""Skills peeking is unified on the native deepagents path.

Two things changed:

1. The supervisor gets native ``SkillsMiddleware`` over the FOCUSED skills
   roots (``orgs/_shared/skills`` + this org's own), so the CTO gets
   canonical progressive disclosure (metadata in the prompt → ``read_file``
   for the body). ``build_stack`` now carries ``supervisor_skills`` and
   ``graph.build_graph`` threads it as ``skills=plan.supervisor_skills or
   None`` (None for a no-skills org → no SkillsMiddleware, byte-identical
    to the previous approach).
2. ``pux_sandbox_load_skill`` is GONE — bodies are no longer a second
   parallel surface. The ``skills-peek-via-read-file`` contract rule makes
   re-introduction a HARD failure.

The kit-level focused-set logic is proven in
``pux-harness/tests/test_supervisor_skills.py``; these tests prove the
wiring (build_stack → StackPlan → graph) and the permanent surface
removal.
"""
from __future__ import annotations

from pathlib import Path
from unittest.mock import MagicMock

import pytest
from langchain_core.tools import StructuredTool
from pydantic import BaseModel

from pux_harness.agent import contract, orgs, stack
from pux_harness.sandbox.tools import REGISTRY, SPECIALIST_TOOL_NAMES


class _NoArgs(BaseModel):
    """Empty args schema (mirrors tools.py's argument-less tool idiom)."""


def _mk_tool(name: str) -> StructuredTool:
    return StructuredTool(
        name=name, description="d", args_schema=_NoArgs, func=lambda: ""
    )


_SPECIALISTS = [_mk_tool("pux_sandbox_python")]


@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """Scratch orgs/ tree with org ``p`` + ``orgs/_shared/skills`` materialized
    so the supervisor skills root resolves (the FOCUSED set's shared half)."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "skills").mkdir(parents=True)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setenv("PUX_BROWSER_VISION", "0")

    d = tmp_path / "orgs" / "p"
    d.mkdir(parents=True)
    (d / "AGENTS.md").write_text("# p\n\nCTO prose.\n")
    (d / "org.yaml").write_text("agents: []\n")
    return tmp_path


@pytest.fixture
def stub_factory(monkeypatch):
    """Stub build_stack's heavy deps so the resolver + plan assembly run real
    without Docker / real middleware / model init (mirrors test_stack.py)."""
    monkeypatch.setattr(stack, "build_context_layer", lambda **kw: ([], []))
    monkeypatch.setattr(stack, "RoutingMiddleware", lambda: "ROUTE")
    monkeypatch.setattr(stack, "SessionGuideMiddleware", lambda: "GUIDE")
    monkeypatch.setattr(stack, "AuditMiddleware", lambda **kw: "AUDIT")
    monkeypatch.setattr(stack, "RubricMiddleware", lambda **kw: "RUBRIC")
    monkeypatch.setattr(stack, "get_model", lambda *a, **k: "MODEL")
    monkeypatch.setattr(stack, "build_grader_tools", lambda *a, **k: ["g1"])
    monkeypatch.setattr(orgs, "get_model", lambda *a, **k: "WORKER_MODEL")


# --- build_stack threads supervisor_skills (the wiring) --------------------


def test_build_stack_supervisor_skills_is_container_absolute_shared(fake_tree, stub_factory):
    """``build_stack`` populates ``supervisor_skills`` with the FOCUSED set,
    container-absolute (the harness pins ``/sandbox/workspace``). With only
    ``orgs/_shared/skills`` present, the supervisor gets exactly that root."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_skills == ["/sandbox/workspace/orgs/_shared/skills"]


def test_build_stack_supervisor_skills_grows_with_own_org_skills(fake_tree, stub_factory):
    """Materializing the org's own ``skills/`` grows the focused set (shared
    first, then own) — the CTO's middleware sees both roots."""
    (fake_tree / "orgs" / "p" / "skills").mkdir(parents=True)
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_skills == [
        "/sandbox/workspace/orgs/_shared/skills",
        "/sandbox/workspace/orgs/p/skills",
    ]


def test_build_stack_no_skills_org_is_empty(tmp_path: Path, monkeypatch, stub_factory):
    """A no-skills org → ``supervisor_skills == []``; the binding turns that
    into ``skills=None`` (no SkillsMiddleware) — byte-identical to the previous approach."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    d = tmp_path / "orgs" / "q"
    d.mkdir(parents=True)
    (d / "AGENTS.md").write_text("# q\n\nCTO.\n")
    (d / "org.yaml").write_text("agents: []\n")
    plan = stack.build_stack(
        "q", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_skills == []


def test_build_graph_threads_skills_none_when_empty(monkeypatch):
    """``build_graph`` passes ``skills=plan.supervisor_skills or None`` — an
    empty plan yields ``None`` so deepagents mounts no SkillsMiddleware (the
    byte-identical no-skills path). The non-empty thread is exercised by the
    build_stack tests above + the graph plan field."""
    from pux_harness.agent import graph as graph_mod
    from pux_harness.agent.stack import StackPlan

    fake_plan = StackPlan(
        supervisor_tools=[_mk_tool("pux_sandbox_python")],
        supervisor_middleware=[],
        supervisor_prompt="P",
        subagents=[],
        supervisor_skills=[],  # empty -> skills=None
    )
    monkeypatch.setattr(graph_mod, "get_model", lambda *a, **k: MagicMock())
    monkeypatch.setattr(graph_mod, "shared_exec", lambda: MagicMock())
    monkeypatch.setattr(graph_mod, "shared_backend", lambda: MagicMock())
    monkeypatch.setattr(graph_mod, "build_native_specialists", lambda *a, **k: [])
    monkeypatch.setattr(graph_mod, "load_profile", lambda org: None)
    monkeypatch.setattr(graph_mod, "load_rubric_gate", lambda org: None)
    monkeypatch.setattr(graph_mod, "build_stack", lambda org, **kw: fake_plan)
    monkeypatch.setattr(graph_mod, "build_memory_backend",
                        lambda *a, **k: (MagicMock(), MagicMock()))
    monkeypatch.setattr(graph_mod, "MEMORY_SOURCES", [])
    captured: dict = {}
    monkeypatch.setattr(
        graph_mod, "create_deep_agent",
        lambda **kw: captured.update(kw) or MagicMock(),
    )
    graph_mod.build_graph("general", checkpointer=MagicMock())
    assert captured["skills"] is None


# --- pux_sandbox_load_skill is GONE (the permanent surface removal) ---------


def test_load_skill_absent_from_registry_and_specialist_names():
    """``load_skill`` is not a declared ToolSpec, and ``pux_sandbox_load_skill``
    is absent from the derived specialist surface. Bodies
    peek via native ``read_file`` now."""
    slugs = {s.slug for s in REGISTRY}
    assert "load_skill" not in slugs
    assert "pux_sandbox_load_skill" not in SPECIALIST_TOOL_NAMES
    # list_skills is still there (the discovery aid that complements the
    # middleware); only the body-LOAD specialist was removed.
    assert "pux_sandbox_list_skills" in SPECIALIST_TOOL_NAMES


def test_contract_skills_peek_tripwire_clean_by_default():
    """The ``skills-peek-via-read-file`` rule is silent today (load_skill is
    gone). This is the green baseline for the reintroduction test below."""
    violations = [v for v in contract.check_harness()
                  if v.rule == "skills-peek-via-read-file"]
    assert violations == []


def test_contract_skills_peek_tripwire_fires_on_reintroduction(monkeypatch):
    """If ``pux_sandbox_load_skill`` sneaks back into the derived specialist
    surface, the contract rule fires a HARD error — the registry IS the single
    source of tool truth, so a tool not in ``SPECIALIST_TOOL_NAMES`` is not a
    specialist surface. Re-introduction must be loud (no-legacy-left-behind)."""
    poisoned = SPECIALIST_TOOL_NAMES | {"pux_sandbox_load_skill"}
    monkeypatch.setattr(contract, "SPECIALIST_TOOL_NAMES", poisoned)
    violations = [v for v in contract.check_harness()
                  if v.rule == "skills-peek-via-read-file"]
    assert len(violations) == 1
    assert violations[0].severity == "error"
    assert "load_skill" in violations[0].message
