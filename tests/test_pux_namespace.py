"""Phase 7 — the ``pux-namespace-resolvable`` contract rule.

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

from pathlib import Path

import pytest

from pux_harness.agent import contract, orgs


# --- shared tree helpers (mirror test_org_extends.py) ----------------------


def _add_org(root: Path, name: str, *, extends: str | None = None,
             agents: list[str] | None = None, body: str = "# Org\n") -> Path:
    d = root / "orgs" / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "AGENTS.md").write_text(body)
    lines: list[str] = []
    if agents is not None:
        lines.append(f"agents: [{', '.join(agents)}]")
    if extends is not None:
        lines.append(f"extends: {extends}")
    if lines:
        (d / "org.yaml").write_text("\n".join(lines) + "\n")
    return d


def _add_agent_with_extends(root: Path, slug: str, org: str, extends: str) -> Path:
    """An agent ``.md`` whose frontmatter carries ``extends:`` (Phase 2/7)."""
    adir = root / "orgs" / org / "agents"
    adir.mkdir(parents=True, exist_ok=True)
    fm = ["---", f'name: "{slug}"', f'description: "{slug}"', f"extends: {extends}", "---"]
    path = adir / f"{slug}.md"
    path.write_text("\n".join(fm) + "\n\nbody\n")
    return path


@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """Scratch orgs/ tree; both ``contract._orgs_dir`` AND ``orgs._orgs_dir``
    patched (the contract's path helpers read ``contract._orgs_dir``, the
    orgs-shim delegates read ``orgs._orgs_dir``). ``_shared/agents`` exists."""
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    return tmp_path


def _pux_violations(violations):
    return [v for v in violations if v.rule == "pux-namespace-resolvable"]


# --- clean by default -------------------------------------------------------


def test_pux_namespace_clean_by_default():
    """No real org uses ``pux:`` today, so ``check_harness()`` reports zero
    ``pux-namespace-resolvable`` violations. This is the green baseline."""
    v = _pux_violations(contract.check_harness())
    assert v == []


def test_pux_namespace_resolvable_clean_on_valid_refs(fake_tree: Path):
    """Valid ``pux:`` references (``extends: pux:copilot-kit`` — the real base;
    ``pux:copilot-helper`` — the real library agent) resolve cleanly."""
    _add_org(fake_tree, "app", extends="pux:copilot-kit", agents=["pux:copilot-helper"])
    assert _pux_violations(contract._pux_namespace_resolvable()) == []


# --- each surface fires on a dangling reference ----------------------------


def test_fires_on_dangling_pux_org_extends(fake_tree: Path):
    """Surface 1 — an org ``extends: pux:<missing>`` that no library base
    provides fires a HARD error."""
    _add_org(fake_tree, "badorg", extends="pux:no-such-base")
    v = _pux_violations(contract._pux_namespace_resolvable())
    assert len(v) == 1
    assert v[0].severity == "error"
    assert "badorg" in v[0].message
    assert "pux:no-such-base" in v[0].message


def test_fires_on_dangling_pux_roster_slug(fake_tree: Path):
    """Surface 2 — a roster entry ``pux:<missing>`` no library agent provides
    fires a HARD error."""
    _add_org(fake_tree, "badorg", agents=["pux:no-such-agent"])
    v = _pux_violations(contract._pux_namespace_resolvable())
    assert len(v) == 1
    assert v[0].severity == "error"
    assert "pux:no-such-agent" in v[0].message


def test_fires_on_dangling_pux_agent_extends(fake_tree: Path):
    """Surface 3 — an agent ``.md`` ``extends: pux:<missing>`` no library agent
    provides fires a HARD error (the source-file scan catches it)."""
    _add_org(fake_tree, "badorg", agents=["spy"])
    _add_agent_with_extends(fake_tree, "spy", "badorg", extends="pux:no-such-agent")
    v = _pux_violations(contract._pux_namespace_resolvable())
    assert len(v) == 1
    assert v[0].severity == "error"
    assert "spy.md" in v[0].message
    assert "pux:no-such-agent" in v[0].message


def test_all_three_surfaces_report_together(fake_tree: Path):
    """A single org with all three dangling ``pux:`` refs reports three distinct
    violations — one per surface (no short-circuit; each surface is independent)."""
    _add_org(fake_tree, "multi", extends="pux:no-base", agents=["pux:no-agent"])
    _add_agent_with_extends(fake_tree, "pux:no-agent", "multi", extends="pux:no-base2")
    v = _pux_violations(contract._pux_namespace_resolvable())
    assert len(v) == 3
    msgs = "\n".join(x.message for x in v)
    assert "pux:no-base" in msgs        # org extends
    assert "pux:no-agent" in msgs       # roster slug
    assert "pux:no-base2" in msgs       # agent-extends
