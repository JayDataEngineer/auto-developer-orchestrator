"""Consolidation — the 7 pure org/agent loaders in ``orgs.py`` are thin
delegates to ``pux_harness.kit.loaders``.

Two guarantees, both ``verify-or-die``:

* **Wiring (low-rot, no golden strings):** every harness wrapper produces the
  SAME result as the kit function called directly with the harness
  ``project_root`` (``orgs._orgs_dir().parent``). This proves the wrappers are
  pure delegates — it survives org-doc edits (unlike a frozen snapshot) and is
  the structural invariant the delegation relies on.
* **Tripwire lock-in (``no-duplicate-loaders-in-orgs``):** the AST tripwire is
  green on the real ``orgs.py`` and fires when a delegated name is re-pasted as
  a real implementation, when the kit import is removed, and stays quiet when a
  delegate's body mentions the old logic only in a docstring.

A one-off byte-identical diff of the FULL delegatable surface (pre vs post
migration) was run separately (10 orgs / 26 slugs / 21
skills-specs, 387920 bytes identical) — that's the migration's regression
guarantee; these tests are the permanent lock-in.
"""
from __future__ import annotations

from pathlib import Path

import pytest

from pux_harness.kit import loaders as kit
from pux_harness.agent import contract, orgs
from pux_harness.agent.contract import (
    _DELEGATED_ORGS_LOADERS,
    _no_duplicate_loaders_in_orgs,
    _scan_orgs_for_duplicate_loaders,
)


# --- wiring: each harness wrapper == kit direct call (pure delegate) --------


def _root() -> Path:
    """The project_root the harness delegates with."""
    return orgs._orgs_dir().parent


def test_delegates_match_kit_directly():
    """Every delegated wrapper returns the SAME value as the kit function called
    directly with the harness project_root — the pure-delegate invariant."""
    root = _root()
    assert orgs.discover_orgs() == kit.discover_orgs(root)
    for org in orgs.discover_orgs():
        assert str(orgs._org_path(org)) == str(kit._org_path(org, root))
        assert [str(p) for p in orgs._agent_search_dirs(org)] == [
            str(p) for p in kit._agent_search_dirs(org, root)
        ]
        assert orgs.org_agent_slugs(org) == kit.org_agent_slugs(org, root)
        assert orgs.load_org_prompt(org) == kit.load_org_prompt(org, root)
        for slug in orgs.org_agent_slugs(org):
            assert orgs._load_agent_spec(slug, org) == kit._load_agent_spec(
                slug, org, root
            )
            spec = orgs._load_agent_spec(slug, org)
            if spec and spec.get("skills"):
                assert orgs._resolve_skills(spec["skills"], slug) == (
                    kit._resolve_skills(
                        spec["skills"], slug,
                        project_root=root,
                        workspace_root=orgs._WORKSPACE_ROOT,
                    )
                )


def test_load_root_prompt_is_intentionally_exempt():
    """``load_root_prompt`` is NOT a delegate: it reads the root AGENTS.md from
    ``PROJECT_ROOT`` (immune to the ``_orgs_dir()`` seam), so a tempdir test that
    patches ``_orgs_dir`` still gets the real base prompt. Delegating it would
    make the kit return ``""`` against a tempdir with no AGENTS.md — changing
    ``build_system_prompt``. The tripwire must therefore NOT list it."""
    assert "load_root_prompt" not in _DELEGATED_ORGS_LOADERS
    # And the delegated set is exactly the 7 byte-safe forwarders.
    assert _DELEGATED_ORGS_LOADERS == frozenset({
        "_org_path", "_agent_search_dirs", "discover_orgs", "org_agent_slugs",
        "load_org_prompt", "_resolve_skills", "_load_agent_spec",
    })


# --- tripwire: green on real repo -----------------------------------------


def test_no_duplicate_loaders_on_real_repo():
    """The real ``orgs.py`` passes the tripwire — all 7 are thin delegates."""
    vs = _no_duplicate_loaders_in_orgs()
    assert vs == [], vs


def test_check_harness_remains_clean_on_real_repo():
    """Registering the new tripwire didn't dirty the global green gate."""
    vs = [v for v in contract.check_harness() if v.severity == "error"]
    assert vs == [], vs


# --- tripwire: provocation (drives the AST scanner against temp files) -----


_GOOD = (
    "from pux_harness.kit import loaders as _aloaders\n"
    "from pathlib import Path\n"
    "PROJECT_ROOT = Path('.')\n"
    "def _orgs_dir():\n"
    "    return PROJECT_ROOT / 'orgs'\n"
    "def _org_path(name):\n"
    "    return _aloaders._org_path(name, _orgs_dir().parent)\n"
    "def _agent_search_dirs(org):\n"
    "    return _aloaders._agent_search_dirs(org, _orgs_dir().parent)\n"
    "def discover_orgs():\n"
    "    return _aloaders.discover_orgs(_orgs_dir().parent)\n"
    "def org_agent_slugs(name):\n"
    "    return _aloaders.org_agent_slugs(name, _orgs_dir().parent)\n"
    "def load_org_prompt(name):\n"
    "    return _aloaders.load_org_prompt(name, _orgs_dir().parent)\n"
    "def _resolve_skills(raw, slug):\n"
    "    return _aloaders._resolve_skills(raw, slug, project_root=_orgs_dir().parent)\n"
    "def _load_agent_spec(slug, org):\n"
    "    return _aloaders._load_agent_spec(slug, org, _orgs_dir().parent)\n"
)


def test_tripwire_clean_on_pure_delegates(tmp_path):
    """A clean orgs.py (all 7 forward to ``_aloaders``) emits nothing."""
    fake = tmp_path / "orgs.py"
    fake.write_text(_GOOD)
    assert _scan_orgs_for_duplicate_loaders(fake) == []


def test_tripwire_fires_on_reimplementation(tmp_path):
    """Re-pasting the old logic into one delegated name (here ``_org_path`` stops
    delegating) is a HARD failure naming that function — the duplication can't
    silently return."""
    bad = _GOOD.replace(
        "def _org_path(name):\n"
        "    return _aloaders._org_path(name, _orgs_dir().parent)\n",
        "def _org_path(name):\n"
        "    top = _orgs_dir() / name\n"
        "    return top if top.is_dir() else top  # re-implemented, not a delegate\n",
    )
    fake = tmp_path / "orgs.py"
    fake.write_text(bad)
    vs = _scan_orgs_for_duplicate_loaders(fake)
    assert len(vs) == 1, vs
    assert vs[0].rule == "no-duplicate-loaders-in-orgs"
    assert vs[0].severity == "error"
    assert "'_org_path'" in vs[0].message


def test_tripwire_fires_when_kit_import_removed(tmp_path):
    """Dropping the kit.loaders import (the verbatim-copy return)
    is itself a failure — delegation was removed."""
    src = _GOOD.replace("from pux_harness.kit import loaders as _aloaders\n", "")
    fake = tmp_path / "orgs.py"
    fake.write_text(src)
    vs = _scan_orgs_for_duplicate_loaders(fake)
    rules = {v.rule for v in vs}
    assert rules == {"no-duplicate-loaders-in-orgs"}, vs
    assert any("no longer imports" in v.message for v in vs)


def test_tripwire_no_false_positive_on_docstring_mention(tmp_path):
    """A delegate whose docstring describes the OLD logic (but still returns the
    ``_aloaders`` call) is clean — AST, not regex."""
    src = _GOOD.replace(
        "def discover_orgs():\n"
        "    return _aloaders.discover_orgs(_orgs_dir().parent)\n",
        "def discover_orgs():\n"
        '    """Sorted names; the OLD body was sorted(_scan_orgs(_orgs_dir())...)."""\n'
        "    return _aloaders.discover_orgs(_orgs_dir().parent)\n",
    )
    fake = tmp_path / "orgs.py"
    fake.write_text(src)
    assert _scan_orgs_for_duplicate_loaders(fake) == []
