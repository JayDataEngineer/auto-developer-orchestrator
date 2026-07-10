"""``pack_org(project_root=…)`` honors a FOREIGN project root.

This is the portability contract: ``pack_org`` must package an org from
*wherever it lives* — a standalone consumer app's tree that is NOT the
orchestrator — using ONLY the ``project_root`` argument. Before the env-pin
fix, ``project_root`` was accepted but silently ignored: every downstream
resolver (``discover_orgs`` / ``_org_path`` / ``_resolve_shared_agents`` /
``_resolve_tool_servers``) funneled through ``kit._paths.project_root()`` ←
``$PUX_PROJECT_ROOT`` ← CWD, so exporting from a foreign root raised
``FileNotFoundError`` (the org was never discovered there). The sibling test
``test_pack_org_never_leaks_data_secrets`` masked the bug by monkeypatching
those resolvers onto a tmp tree.

These tests prove the fix WITHOUT monkeypatching any resolver: the process
CWD is pointed at the real orchestrator root (so the CWD-based default would
resolve the orchestrator's orgs/, where the foreign org does NOT exist), and
``project_root`` alone must redirect discovery. No Docker, no server.
"""
from __future__ import annotations

import json
import os
import tarfile
from pathlib import Path

import pytest

from pux_harness.pack import pack_org

# The real orchestrator root — where the process CWD is parked so the default
# (CWD-based) resolver would look HERE, not in the foreign tmp tree. "foreign"
# is deliberately not an orchestrator org.
_ORCHESTRATOR_ROOT = Path(__file__).resolve().parents[2]
_ENV = "PUX_PROJECT_ROOT"
_SECRET = "FOREIGN-ROOT-SECRET-do-not-ship"


def _stage_foreign_org(root: Path, org: str, agent: str = "worker") -> Path:
    """Build a complete synthetic org under ``root/orgs/<org>/``.

    Covers every recursive dir the export walks (agents / skills / sandbox /
    config) plus a ``data/`` secret that MUST be excluded — so a foreign-root
    export round-trips every primitive and still honors the credential-leak
    contract."""
    org_dir = root / "orgs" / org
    (org_dir / "agents").mkdir(parents=True)
    (org_dir / "skills" / org / "references").mkdir(parents=True)
    (org_dir / "sandbox" / "tools").mkdir(parents=True)
    (org_dir / "config").mkdir(parents=True)
    (org_dir / "data").mkdir(parents=True)

    (org_dir / "AGENTS.md").write_text(f"# {org} CTO\n")
    (org_dir / "org.yaml").write_text(f"agents: [{agent}]\n")
    (org_dir / "policy.yaml").write_text("sandbox:\n  tier: isolated\n")
    (org_dir / "agents" / f"{agent}.md").write_text(
        f"---\nname: {agent}\ndescription: d\n---\n\nbody.\n"
    )
    (org_dir / "skills" / org / "SKILL.md").write_text(
        "---\nname: skill\ndescription: d\n---\n\n# skill\n"
    )
    (org_dir / "skills" / org / "references" / "ref.md").write_text("# ref\n")
    (org_dir / "sandbox" / "tools" / "tools.yaml").write_text(
        "tools:\n  - name: do_thing\n    script: do_thing.py\n    returns: text\n"
    )
    (org_dir / "sandbox" / "do_thing.py").write_text("print('ok')\n")
    (org_dir / "config" / "settings.json").write_text("{}\n")
    # Runtime secret — must NEVER ship.
    (org_dir / "data" / ".session.json").write_text(f'{{"token":"{_SECRET}"}}')
    return org_dir


def _tar_files(tar_path: Path) -> list[str]:
    with tarfile.open(tar_path, "r:gz") as tar:
        return sorted(m.name for m in tar.getmembers() if m.isfile())


def _manifest(tar_path: Path, org: str) -> dict:
    with tarfile.open(tar_path, "r:gz") as tar:
        f = tar.extractfile(f"{org}/manifest.json")
        assert f is not None
        return json.loads(f.read())


# ---------------------------------------------------------------------------
# The core portability proof
# ---------------------------------------------------------------------------

def test_export_from_foreign_root_without_monkeypatch(tmp_path, monkeypatch):
    """``pack_org(project_root=foreign)`` discovers + packages an org that
    does NOT exist in the orchestrator, with CWD parked at the orchestrator.

    Without the env-pin fix this raises FileNotFoundError (the org is absent
    from the CWD-resolved orchestrator orgs/). With it, every primitive is
    reconstructed from the foreign tree."""
    foreign = tmp_path / "consumer-app"
    _stage_foreign_org(foreign, "foreign")
    (foreign / "AGENTS.md").write_text("# consumer root prompt\n")

    # Park CWD at the orchestrator + clear any inherited PUX_PROJECT_ROOT so
    # the ONLY signal pointing at `foreign` is the project_root argument.
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    monkeypatch.delenv(_ENV, raising=False)
    assert "foreign" not in {
        p.name for p in (_ORCHESTRATOR_ROOT / "orgs").iterdir()
    }, "test前提 broken: 'foreign' must not be a real orchestrator org"

    output = tmp_path / "foreign.tar.gz"
    result = pack_org("foreign", output, project_root=foreign)

    assert result == output
    assert output.is_file()
    names = _tar_files(output)

    # Every primitive reconstructed from the FOREIGN tree (archive-relative,
    # orgs/specialists/<n>/ normalized to orgs/<n>/). NOTE: no top-level
    # ``foreign/AGENTS.md`` — the root AGENTS.md is a dev guide now, not a
    # packaged runtime base prompt (the base flows via the flattened chain
    # overlay baked into the org's own AGENTS.md).
    expected = [
        "foreign/orgs/foreign/AGENTS.md",
        "foreign/orgs/foreign/org.yaml",
        "foreign/orgs/foreign/policy.yaml",
        "foreign/orgs/foreign/agents/worker.md",
        "foreign/orgs/foreign/skills/foreign/SKILL.md",
        "foreign/orgs/foreign/skills/foreign/references/ref.md",
        "foreign/orgs/foreign/sandbox/tools/tools.yaml",
        "foreign/orgs/foreign/sandbox/do_thing.py",
        "foreign/orgs/foreign/config/settings.json",
        "foreign/manifest.json",
    ]
    missing = [e for e in expected if e not in names]
    assert not missing, f"foreign-root export dropped primitives: {missing}"

    # Credential-leak contract holds even from a foreign root.
    assert not any("/data/" in n for n in names), "data/ leaked"
    with tarfile.open(output, "r:gz") as tar:
        for m in tar.getmembers():
            if not m.isfile():
                continue
            f = tar.extractfile(m)
            assert f is not None
            assert _SECRET not in f.read().decode("utf-8", "replace"), (
                f"secret leaked into {m.name}")

    mf = _manifest(output, "foreign")
    assert mf["org"] == "foreign"
    assert mf["agent_roster"] == ["worker"]
    # The manifest inventories primitives AND the runtime scaffold (F3: the
    # vendored slim kit + pyproject + run.py + README turn the archive into a
    # runnable package). Every primitive is still accounted for; the scaffold
    # adds the rest.
    primitive_rels = [
        e[len("foreign/"):] for e in expected if e != "foreign/manifest.json"
    ]
    assert all(rel in mf["files"] for rel in primitive_rels), (
        f"primitive missing from manifest files: "
        f"{[r for r in primitive_rels if r not in mf['files']]}"
    )
    assert mf["categories"]["runtime_scaffold"], "runtime scaffold not emitted"
    assert mf["total_files"] == len(primitive_rels) + len(
        mf["categories"]["runtime_scaffold"]
    )


def test_project_root_env_is_restored_no_bleed(tmp_path, monkeypatch):
    """The env-pin is per-call: ``PUX_PROJECT_ROOT`` is restored to its prior
    value (unset here) after export, so consecutive exports with DIFFERENT
    foreign roots never contaminate each other (plan Risk #3)."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    monkeypatch.delenv(_ENV, raising=False)

    root_a = tmp_path / "app-a"
    root_b = tmp_path / "app-b"
    _stage_foreign_org(root_a, "alpha", agent="alpha-agent")
    _stage_foreign_org(root_b, "beta", agent="beta-agent")

    out_a = tmp_path / "a.tar.gz"
    out_b = tmp_path / "b.tar.gz"
    pack_org("alpha", out_a, project_root=root_a)
    # Env must be unset again before the second call — proves restore fired.
    assert _ENV not in os.environ, "PUX_PROJECT_ROOT bled past pack_org"
    pack_org("beta", out_b, project_root=root_b)
    assert _ENV not in os.environ, "PUX_PROJECT_ROOT bled past second export"

    names_a = _tar_files(out_a)
    names_b = _tar_files(out_b)
    # No cross-contamination: alpha's archive has alpha-agent, never beta's.
    assert any("alpha-agent.md" in n for n in names_a)
    assert not any("beta" in n for n in names_a), "beta contaminated alpha export"
    assert any("beta-agent.md" in n for n in names_b)
    assert not any("alpha" in n for n in names_b), "alpha contaminated beta export"


def test_foreign_root_default_uses_cwd_when_unset(tmp_path, monkeypatch):
    """Symmetry: with ``project_root`` unset and CWD at the foreign tree, the
    default resolver (CWD) is used — so the env-pin only redirects when an
    explicit root is given, never overrides a legitimate CWD default."""
    foreign = tmp_path / "cwd-consumer"
    _stage_foreign_org(foreign, "cwdorg")
    monkeypatch.chdir(foreign)
    monkeypatch.delenv(_ENV, raising=False)

    output = tmp_path / "cwdorg.tar.gz"
    pack_org("cwdorg", output)  # no project_root kwarg
    assert output.is_file()
    assert "cwdorg/orgs/cwdorg/org.yaml" in _tar_files(output)


def test_archive_extracts_to_a_readable_tree(tmp_path, monkeypatch):
    """PERMANENT contract (regression for the 0o644-dir-mode bug): the archive
    must unpack to a tree a consumer can actually READ and compile from. The
    top-level dir entry carries the execute bit; listing members is not
    enough — a dir without traverse permission makes every child EACCES."""
    foreign = tmp_path / "extract-consumer"
    _stage_foreign_org(foreign, "xorg")
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    monkeypatch.delenv(_ENV, raising=False)

    output = tmp_path / "xorg.tar.gz"
    pack_org("xorg", output, project_root=foreign)

    unpack = tmp_path / "unpack"
    unpack.mkdir()
    with tarfile.open(output, "r:gz") as tar:
        tar.extractall(unpack)  # noqa: S202 — controlled test fixture

    # The org's org.yaml is readable through the unpacked tree — proves the
    # dir entries carry traverse permission (the bug made this EACCES).
    org_yaml = unpack / "xorg" / "orgs" / "xorg" / "org.yaml"
    assert org_yaml.is_file()
    assert "agents: [worker]" in org_yaml.read_text()
    # Every agent + skill + sandbox file survives a real filesystem walk.
    shipped = {str(p.relative_to(unpack)) for p in unpack.rglob("*") if p.is_file()}
    assert "xorg/orgs/xorg/agents/worker.md" in shipped
    assert "xorg/orgs/xorg/skills/xorg/SKILL.md" in shipped
    assert "xorg/orgs/xorg/sandbox/tools/tools.yaml" in shipped

