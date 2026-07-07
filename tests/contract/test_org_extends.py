"""Org-level inheritance (``org.yaml extends:``) at the RUNTIME layer.

Scope is deliberately narrow and NON-duplicative. The sibling kit file
(``pux-harness/tests/test_org_inheritance.py``) owns the kit-level mechanism
(chain walking, roster union, search-dirs, overlay concat). THIS file owns what
only the orchestrator-integration layer can prove:

* the pux-shim delegates (``orgs.org_extends`` / ``orgs.org_extends_chain``)
  route through ``orgs._orgs_dir()`` (monkeypatched) — the contract tests'
  patch reaches them;
* the profile deep-merge (``_deep_merge_profile``) is the ONE universal rule
  (dict per-key / list union / scalar delta-wins) — proven in isolation against
  hand-built dicts, then through ``_resolved_profile_yaml`` → ``load_profile``
  / ``load_middleware_overrides`` / ``load_rubric_gate`` / the model-role
  resolver (a child inherits a parent's profile fields and overrides the keys
  it restates);
* the three contract rules — ``org-extends-resolvable`` (no such org / parent
  needs AGENTS.md), ``org-extends-acyclic`` (cycle), ``org-extends-policy``
  (warn on a policy-less child — policy is NEVER inherited);
* an INHERITED roster slug resolves through the chain-aware search dirs at
  contract time (Rule 3 sees the parent's roster + resolves the parent's agent
  ``.md``).
"""
from __future__ import annotations

from pathlib import Path

import pytest

from pux_harness.agent import contract, orgs

from tests.conftest import add_agent, add_org, write_profile


# --- pux-shim delegates (route through orgs._orgs_dir) ---------------------

def test_shim_org_extends_none_when_no_extends(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "solo", body="# Solo\n", agents=["a"])
    assert orgs.org_extends("solo") is None


def test_shim_org_extends_reads_parent(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "base", body="# Base\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    assert orgs.org_extends("child") == "base"


def test_shim_org_extends_chain_root_to_child(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "base", body="# Base\n")
    add_org(fake_orgs_tree, "mid", body="# Mid\n", extends="base")
    add_org(fake_orgs_tree, "kid", body="# Kid\n", extends="mid")
    assert orgs.org_extends_chain("kid") == ["base", "mid", "kid"]


def test_shim_org_extends_chain_cycle_raises(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "a", body="# A\n", extends="b")
    add_org(fake_orgs_tree, "b", body="# B\n", extends="a")
    with pytest.raises(ValueError, match="extends cycle"):
        orgs.org_extends_chain("a")


# --- _deep_merge_profile (the ONE universal merge rule, in isolation) ------
#
# dict per-key (recurse) / list union (dedup, base order) / scalar delta-wins.
# Type-mismatch falls back to delta-wins (the child explicitly restated it).
# The same rule composes EVERY profile field — that universality is the point.

from pux_harness.agent.profile import _deep_merge_profile  # noqa: E402


def test_deep_merge_dict_per_key_recurse() -> None:
    base = {"a": {"x": 1, "y": 2}, "b": 1}
    delta = {"a": {"y": 99, "z": 3}}
    assert _deep_merge_profile(base, delta) == {"a": {"x": 1, "y": 99, "z": 3}, "b": 1}


def test_deep_merge_list_union_dedup_base_order() -> None:
    assert _deep_merge_profile(["a", "b"], ["b", "c"]) == ["a", "b", "c"]


def test_deep_merge_scalar_delta_wins() -> None:
    assert _deep_merge_profile("base", "child") == "child"
    assert _deep_merge_profile(1, 2) == 2


def test_deep_merge_type_mismatch_delta_wins() -> None:
    # child restated with a different type -> child wins (honest resolution).
    assert _deep_merge_profile({"a": 1}, ["x"]) == ["x"]
    assert _deep_merge_profile(["a"], {"b": 2}) == {"b": 2}


def test_deep_merge_middleware_scope_blocks_union() -> None:
    # middleware.supervisor.{add,remove} union down the chain — the universal
    # rule applied to a real pux block (proves add accumulates, remove unions).
    base = {"middleware": {"supervisor": {"add": ["context"], "remove": ["routing"]}}}
    delta = {"middleware": {"supervisor": {"add": ["audit"], "remove": ["session_guide"]}}}
    assert _deep_merge_profile(base, delta) == {
        "middleware": {"supervisor": {"add": ["context", "audit"],
                                      "remove": ["routing", "session_guide"]}},
    }


def test_deep_merge_general_purpose_subagent_fieldwise() -> None:
    # the native general_purpose_subagent dict merges key-by-key (child wins).
    base = {"general_purpose_subagent": {"enabled": True, "description": "base"}}
    delta = {"general_purpose_subagent": {"enabled": False}}
    assert _deep_merge_profile(base, delta) == {
        "general_purpose_subagent": {"enabled": False, "description": "base"},
    }


# --- _resolved_profile_yaml + load_* (chain inheritance at runtime) --------

def test_resolved_profile_inherits_parent_suffix(fake_orgs_tree: Path) -> None:
    from pux_harness.agent.profile import _resolved_profile_yaml
    add_org(fake_orgs_tree, "base", body="# Base\n")
    write_profile(fake_orgs_tree, "base", 'system_prompt_suffix: "PARENT SUFFIX"\n')
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    merged = _resolved_profile_yaml("child")
    assert merged is not None
    assert merged["system_prompt_suffix"] == "PARENT SUFFIX"


def test_resolved_profile_child_overrides_suffix(fake_orgs_tree: Path) -> None:
    from pux_harness.agent.profile import _resolved_profile_yaml
    add_org(fake_orgs_tree, "base", body="# Base\n")
    write_profile(fake_orgs_tree, "base", 'system_prompt_suffix: "PARENT"\n')
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    write_profile(fake_orgs_tree, "child", 'system_prompt_suffix: "CHILD"\n')
    assert _resolved_profile_yaml("child")["system_prompt_suffix"] == "CHILD"


def test_resolved_profile_none_when_chain_has_no_profile(fake_orgs_tree: Path) -> None:
    from pux_harness.agent.profile import _resolved_profile_yaml
    add_org(fake_orgs_tree, "base", body="# Base\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    assert _resolved_profile_yaml("child") is None


def test_load_profile_excluded_tools_union(fake_orgs_tree: Path) -> None:
    # excluded_tools is a native list field -> UNION down the chain.
    from pux_harness.agent.profile import load_profile
    add_org(fake_orgs_tree, "base", body="# Base\n")
    write_profile(fake_orgs_tree, "base", "excluded_tools: [pux_sandbox_alpha]\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    write_profile(fake_orgs_tree, "child", "excluded_tools: [pux_sandbox_beta]\n")
    cfg = load_profile("child")
    assert cfg is not None
    assert set(cfg.excluded_tools) == {"pux_sandbox_alpha", "pux_sandbox_beta"}


def test_load_profile_tool_description_overrides_per_key(fake_orgs_tree: Path) -> None:
    from pux_harness.agent.profile import load_profile
    add_org(fake_orgs_tree, "base", body="# Base\n")
    write_profile(fake_orgs_tree, "base",
                   "tool_description_overrides:\n  pux_sandbox_alpha: \"base desc\"\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    write_profile(fake_orgs_tree, "child",
                   "tool_description_overrides:\n"
                   "  pux_sandbox_alpha: \"child desc\"\n"
                   "  pux_sandbox_beta: \"new\"\n")
    cfg = load_profile("child")
    assert cfg.tool_description_overrides == {
        "pux_sandbox_alpha": "child desc",   # child wins
        "pux_sandbox_beta": "new",           # added
    }


def test_load_middleware_overrides_child_inherits_and_extends(fake_orgs_tree: Path) -> None:
    from pux_harness.agent.profile import load_middleware_overrides
    add_org(fake_orgs_tree, "base", body="# Base\n")
    write_profile(fake_orgs_tree, "base",
                   "middleware:\n  supervisor:\n    remove: [routing]\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    write_profile(fake_orgs_tree, "child",
                   "middleware:\n  supervisor:\n    add: [audit]\n")
    ov = load_middleware_overrides("child")
    assert "routing" in ov.supervisor_remove      # inherited
    assert "audit" in ov.supervisor_add           # own


def test_load_rubric_gate_child_inherits_and_overrides(fake_orgs_tree: Path) -> None:
    from pux_harness.agent.profile import load_rubric_gate
    add_org(fake_orgs_tree, "base", body="# Base\n")
    write_profile(fake_orgs_tree, "base",
                   "rubric:\n  enabled: true\n  max_iterations: 3\n"
                   "  default: \"parent rubric\"\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    write_profile(fake_orgs_tree, "child", "rubric:\n  max_iterations: 5\n")
    gate = load_rubric_gate("child")
    assert gate is not None
    assert gate.enabled is True               # inherited
    assert gate.max_iterations == 5           # child wins (scalar)
    assert gate.default == "parent rubric"    # inherited


def test_model_role_override_child_inherits_and_overrides(fake_orgs_tree: Path) -> None:
    from pux_harness.agent.model import _org_role_override
    add_org(fake_orgs_tree, "base", body="# Base\n")
    write_profile(fake_orgs_tree, "base",
                   "models:\n  base_model: parent-base\n  worker_model: parent-worker\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    write_profile(fake_orgs_tree, "child",
                   "models:\n  worker_model: child-worker\n")
    assert _org_role_override("child", "base_model") == "parent-base"     # inherited
    assert _org_role_override("child", "worker_model") == "child-worker"  # child wins


def test_build_system_prompt_via_shim_inherits_overlay(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "base", body="BASE OVERLAY")
    add_org(fake_orgs_tree, "child", body="CHILD OVERLAY", extends="base")
    prompt = orgs.build_system_prompt("child")
    assert prompt.index("BASE OVERLAY") < prompt.index("CHILD OVERLAY")


# --- contract rules: org-extends-resolvable / acyclic / policy -------------

def test_contract_org_extends_resolvable_no_such_org(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "orphan", body="# O\n", extends="ghost")
    rules = {v.rule for v in contract.check_org("orphan")}
    assert "org-extends-resolvable" in rules


def test_contract_org_extends_resolvable_parent_needs_agents_md(fake_orgs_tree: Path) -> None:
    # parent dir exists but has no AGENTS.md -> not a valid base org.
    (fake_orgs_tree / "orgs" / "naked").mkdir(parents=True)
    add_org(fake_orgs_tree, "child", body="# C\n", extends="naked")
    rules = {v.rule for v in contract.check_org("child")}
    assert "org-extends-resolvable" in rules


def test_contract_org_extends_acyclic(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "a", body="# A\n", extends="b")
    add_org(fake_orgs_tree, "b", body="# B\n", extends="a")
    rules = {v.rule for v in contract.check_org("a")}
    assert "org-extends-acyclic" in rules


def test_contract_org_extends_policy_warns_on_policyless_child(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "base", body="# Base\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")  # no policy
    vs = contract.check_org("child")
    warns = [v for v in vs if v.rule == "org-extends-policy"]
    assert len(warns) == 1, vs
    assert warns[0].severity == "warn"


def test_contract_org_extends_policy_no_warn_when_child_has_policy(fake_orgs_tree: Path) -> None:
    add_org(fake_orgs_tree, "base", body="# Base\n")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base",
             policy="egress:\n  allow: []\n")
    vs = contract.check_org("child")
    assert not any(v.rule == "org-extends-policy" for v in vs), vs


def test_contract_no_policy_warn_for_non_extending_org(fake_orgs_tree: Path) -> None:
    # a standalone org with no policy is fine (existing behavior preserved).
    add_org(fake_orgs_tree, "solo", body="# Solo\n")
    vs = contract.check_org("solo")
    assert not any(v.rule == "org-extends-policy" for v in vs), vs


def test_contract_inherited_roster_resolves_via_rule3(fake_orgs_tree: Path) -> None:
    # child inherits alpha from base; alpha's .md lives in BASE's agents dir.
    # Rule 3 must resolve it through the chain-aware search dirs — NO
    # agent-resolves error (proves the inherited roster is validated, not just
    # the own one).
    add_org(fake_orgs_tree, "base", body="# Base\n", agents=["alpha"])
    add_agent(fake_orgs_tree, "alpha", "base")
    add_org(fake_orgs_tree, "child", body="# Child\n", extends="base")
    errs = [v for v in contract.check_org("child") if v.severity == "error"]
    assert errs == [], f"inherited slug should resolve cleanly: {errs}"


def test_contract_clean_child_inherits_clean_parent(fake_orgs_tree: Path) -> None:
    # A well-formed child extending a well-formed parent produces ZERO errors
    # (one org-extends-policy WARN — the child ships no policy of its own).
    add_org(fake_orgs_tree, "base", body="# Base\n", agents=["alpha"])
    add_agent(fake_orgs_tree, "alpha", "base")
    add_org(fake_orgs_tree, "child", body="# Child\n", agents=["beta"], extends="base")
    add_agent(fake_orgs_tree, "beta", "child")
    vs = contract.check_org("child")
    assert [v for v in vs if v.severity == "error"] == []
    # both the inherited + the own slug resolved (no agent-resolves).
    assert not any(v.rule == "agent-resolves" for v in vs)
