"""The ``pux-namespace-resolvable`` contract rule.

The kit-level mechanism (``pux:`` resolution, ``$PUX_ORG_PATHS``, library-base
inheritance) is proven in ``pux-harness/tests/test_org_library.py``. THIS file
owns what only the orchestrator-integration layer can prove: the contract rule
that makes every ``pux:`` reference resolve or fail loud.

The rule scans three surfaces — org ``extends:``, roster ``agents:``, and agent
``.md`` frontmatter ``extends:`` — for ``pux:`` tokens and flags any that don't
resolve against the shipped library bases. ``check_harness()`` is clean by
default (no real org uses ``pux:`` yet); the fake-tree tests prove each surface
fires on a dangling reference.
"""
from __future__ import annotations

import pytest

from pux_harness.agent import contract
from pux_harness.kit import _paths

from tests.conftest import add_agent, add_org, fake_orgs_tree


@pytest.fixture
def fake_tree(fake_orgs_tree, monkeypatch):
    """``fake_orgs_tree`` + a test library base so ``pux:test-base`` /
    ``pux:test-helper`` references resolve."""
    base = fake_orgs_tree / "bases" / "test-base"
    (base / "agents").mkdir(parents=True)
    (base / "AGENTS.md").write_text("# test-base\n")
    (base / "org.yaml").write_text("agents:\n  - test-helper\n")
    (base / "agents" / "test-helper.md").write_text(
        "---\nname: test-helper\ndescription: A test helper.\n---\n"
    )
    monkeypatch.setattr(_paths, "library_bases_dir", lambda: fake_orgs_tree / "bases")
    return fake_orgs_tree


def _pux_violations(violations):
    return [v for v in violations if v.rule == "pux-namespace-resolvable"]


# --- clean by default -------------------------------------------------------


def test_pux_namespace_clean_by_default():
    """No real org uses ``pux:`` today, so ``check_harness()`` reports zero
    ``pux-namespace-resolvable`` violations. This is the green baseline."""
    v = _pux_violations(contract.check_harness())
    assert v == []


def test_pux_namespace_resolvable_clean_on_valid_refs(fake_tree: Path):
    """Valid ``pux:`` references (``extends: pux:test-base`` — the library base;
    ``pux:test-helper`` — the library agent) resolve cleanly."""
    add_org(fake_tree, "app", extends="pux:test-base", agents=["pux:test-helper"])
    assert _pux_violations(contract._pux_namespace_resolvable()) == []


# --- each surface fires on a dangling reference ----------------------------


def test_fires_on_dangling_pux_org_extends(fake_tree: Path):
    """Surface 1 — an org ``extends: pux:<missing>`` that no library base
    provides fires a HARD error."""
    add_org(fake_tree, "badorg", extends="pux:no-such-base")
    v = _pux_violations(contract._pux_namespace_resolvable())
    assert len(v) == 1
    assert v[0].severity == "error"
    assert "badorg" in v[0].message
    assert "pux:no-such-base" in v[0].message


def test_fires_on_dangling_pux_roster_slug(fake_tree: Path):
    """Surface 2 — a roster entry ``pux:<missing>`` no library agent provides
    fires a HARD error."""
    add_org(fake_tree, "badorg", agents=["pux:no-such-agent"])
    v = _pux_violations(contract._pux_namespace_resolvable())
    assert len(v) == 1
    assert v[0].severity == "error"
    assert "pux:no-such-agent" in v[0].message


def test_fires_on_dangling_pux_agent_extends(fake_tree: Path):
    """Surface 3 — an agent ``.md`` ``extends: pux:<missing>`` no library agent
    provides fires a HARD error (the source-file scan catches it)."""
    add_org(fake_tree, "badorg", agents=["spy"])
    add_agent(fake_tree, "spy", "badorg", extends="pux:no-such-agent")
    v = _pux_violations(contract._pux_namespace_resolvable())
    assert len(v) == 1
    assert v[0].severity == "error"
    assert "spy.md" in v[0].message
    assert "pux:no-such-agent" in v[0].message


def test_all_three_surfaces_report_together(fake_tree: Path):
    """A single org with all three dangling ``pux:`` refs reports three distinct
    violations — one per surface (no short-circuit; each surface is independent)."""
    add_org(fake_tree, "multi", extends="pux:no-base", agents=["pux:no-agent"])
    add_agent(fake_tree, "pux:no-agent", "multi", extends="pux:no-base2")
    v = _pux_violations(contract._pux_namespace_resolvable())
    assert len(v) == 3
    msgs = "\n".join(x.message for x in v)
    assert "pux:no-base" in msgs        # org extends
    assert "pux:no-agent" in msgs       # roster slug
    assert "pux:no-base2" in msgs       # agent-extends
