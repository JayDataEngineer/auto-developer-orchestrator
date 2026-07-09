"""Integration test for the orchestrator org — the first real-world use of
``org.yaml extends:`` in the project.

Proves against the REAL project tree (not a temp fixture) that:
1. The orchestrator org is discovered.
2. Its extends chain resolves: ``general -> orchestrator``.
3. Its inherited roster is the union: general's ``[researcher, browser]`` +
   its own ``[task-planner]``.
4. Its AGENTS.md overlay chain concatenates general's + its own.
5. The contract checker passes with zero errors.
6. The task-planner agent spec resolves through the orchestrator's own
   ``agents/`` dir.
"""
from __future__ import annotations

from pathlib import Path

from pux_harness.agent.contract import check_org, discover_orgs
from pux_harness.agent.orgs import (
    build_system_prompt,
    org_agent_slugs,
    org_extends,
    org_extends_chain,
)
from pux_harness.kit.loaders import _load_agent_spec


# --- discovery --------------------------------------------------------------

def test_orchestrator_is_discovered() -> None:
    assert "orchestrator" in discover_orgs()


# --- extends chain ----------------------------------------------------------

def test_orchestrator_extends_general() -> None:
    assert org_extends("orchestrator") == "general"


def test_orchestrator_chain_is_general_then_orchestrator() -> None:
    assert org_extends_chain("orchestrator") == ["general", "orchestrator"]


# --- inherited roster -------------------------------------------------------

def test_orchestrator_roster_inherits_general_agents() -> None:
    """general's [researcher, browser] + orchestrator's own [task-planner]."""
    roster = org_agent_slugs("orchestrator")
    assert "researcher" in roster
    assert "browser" in roster
    assert "task-planner" in roster
    # general's agents come first (root->child order)
    assert roster.index("researcher") < roster.index("task-planner")
    assert roster.index("browser") < roster.index("task-planner")


# --- AGENTS.md overlay chain ------------------------------------------------

def test_orchestrator_system_prompt_includes_both_overlays() -> None:
    """build_system_prompt concatenates root AGENTS.md + general's overlay +
    orchestrator's overlay. Both org overlays must be present."""
    prompt = build_system_prompt("orchestrator")
    # general's overlay mentions "General Org"
    assert "General Org" in prompt
    # orchestrator's own overlay mentions orchestration
    assert "orchestrator" in prompt.lower()
    # general's overlay comes before orchestrator's (root->child order)
    assert prompt.index("General Org") < prompt.lower().index("orchestrator")


# --- agent resolution -------------------------------------------------------

def test_task_planner_resolves() -> None:
    """task-planner lives in orchestrator's own agents/ dir."""
    project_root = Path(__file__).resolve().parents[2]
    spec = _load_agent_spec("task-planner", "orchestrator", project_root)
    assert spec is not None
    assert "task-planner" in spec.get("name", "")
    assert "plan" in spec.get("description", "").lower()


# --- contract green gate ----------------------------------------------------

def test_orchestrator_contract_no_errors() -> None:
    violations = check_org("orchestrator")
    errors = [v for v in violations if v.severity == "error"]
    assert errors == [], f"orchestrator contract errors: {errors}"
