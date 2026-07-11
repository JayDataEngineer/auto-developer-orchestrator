"""The portability contract that was missing: an exported archive must not just
contain files — it must RECOMPILE into a runnable graph from the unpacked tree.

``test_export_project_root.py`` proves ``pack_org(project_root=…)`` honors a
foreign root with *synthetic* tmp orgs. ``test_export.py`` proves every shipped
org *archives* with the right files. NEITHER proves the archive is
self-consistent enough that ``compile_org`` reconstructs the graph from it —
which is the whole point of an export. Before this suite + the
``_normalize_specialists_refs`` fix, 7/10 shipped orgs silently produced
archives that could not recompile: the tree was flattened
(``orgs/specialists/<name>`` -> ``orgs/<name>``) but agent ``skills:`` paths and
``policy.yaml`` ``script:``/``dockerfile:`` refs still pointed at the old
``specialists/`` path, so ``kit._resolve_skills`` raised ``KeyError`` on the
unpacked tree.

These tests pin both halves: the content-rewrite helper, and a full
export -> untar -> ``compile_org`` round-trip for EVERY shipped org (offline
scripted model, no Docker/keys).
"""
from __future__ import annotations

import io
import json
import tarfile
from pathlib import Path
import pytest
import yaml

from pux_harness.agent.orgs import discover_orgs
from pux_harness.pack import _TEXT_SUFFIXES, _normalize_specialists_refs, pack_org
from pux_harness.kit import compile_org
from pux_harness.kit._testing import ScriptedModel


# --- the content-rewrite helper --------------------------------------------

class TestNormalizeSpecialistsRefs:
    """``_normalize_specialists_refs`` flattens ``orgs/specialists/`` refs to
    match the flattened tree — the fix that makes archives self-consistent."""

    def test_rewrites_skill_frontmatter_path(self) -> None:
        src = b'---\nskills: ["orgs/specialists/invest/skills"]\n---\n'
        out = _normalize_specialists_refs(src)
        assert b"orgs/specialists/" not in out
        assert b'orgs/invest/skills' in out

    def test_rewrites_policy_script_and_dockerfile_paths(self) -> None:
        src = (
            b"sandbox:\n"
            b"  image:\n"
            b"    dockerfile: orgs/specialists/video-production/Dockerfile\n"
            b"    context: orgs/specialists/video-production\n"
            b"host_setup:\n"
            b"  - script: orgs/specialists/deep-research-engine/sandbox/x.py\n"
        )
        out = _normalize_specialists_refs(src)
        assert b"orgs/specialists/" not in out
        assert b"orgs/video-production/Dockerfile" in out
        assert b"context: orgs/video-production\n" in out
        assert b"orgs/deep-research-engine/sandbox/x.py" in out

    def test_preserves_shared_refs_and_non_path_prose(self) -> None:
        # ``_shared`` refs and ordinary prose are untouched; only the
        # ``orgs/specialists/`` prefix is flattened.
        src = b'skills: ["orgs/_shared/skills"]\nsee the skills dir\n'
        out = _normalize_specialists_refs(src)
        assert out == src

    def test_binary_and_non_utf8_pass_through(self) -> None:
        src = b"\x89PNG\r\n\x1a\norgs/specialists/x\x00\xff"
        assert _normalize_specialists_refs(src) == src

    def test_noop_when_prefix_absent(self) -> None:
        src = b"nothing to do here\n"
        assert _normalize_specialists_refs(src) == src


# --- the round-trip-compile contract ---------------------------------------

def _extract_archive(archive: Path, dest: Path) -> Path:
    """Untar <archive> into <dest>; return the top-level <org>/ dir."""
    with tarfile.open(archive, "r:gz") as tar:
        # Controlled test fixture — member paths are owned by pack_org.
        tar.extractall(dest, filter="data")  # type: ignore[arg-type]
    members = [p.name for p in dest.iterdir()]
    assert len(members) == 1, f"expected one top-level org dir, got {members}"
    return dest / members[0]


@pytest.mark.parametrize("org", sorted(discover_orgs()))
def test_exported_org_recompiles_from_unpack_tree(org, tmp_path, project_root):
    """Every shipped org: export -> untar to a FOREIGN tmp tree -> compile_org
    succeeds. tools=[] — this proves the graph STRUCTURE (agents/skills/policy)
    resolves against the unpacked tree; tool wiring is the consumer's job."""
    archive = tmp_path / f"{org}.tar.gz"
    pack_org(org, archive, project_root=project_root)

    unpack_root = _extract_archive(archive, tmp_path / "unpack")
    # compile from the FOREIGN unpacked tree — not the orchestrator root.
    graph = compile_org(org, project_root=unpack_root, model=ScriptedModel(), tools=[])
    assert graph is not None, f"{org}: compile_org returned None"
    # The compiled object must carry a graph (CompiledStateGraph / subclass).
    assert hasattr(graph, "ainvoke"), f"{org}: not a runnable graph: {type(graph)}"


@pytest.mark.parametrize("org", sorted(discover_orgs()))
def test_exported_archive_has_no_stale_specialists_refs(org, tmp_path, project_root):
    """The consistency invariant: after export, NO text file in the archive
    references ``orgs/specialists/`` — the tree and every content ref agree on
    the flattened ``orgs/<name>/`` layout. A stale ref would dangle on unpack."""
    archive = tmp_path / f"{org}.tar.gz"
    pack_org(org, archive, project_root=project_root)

    offenders: list[str] = []
    with tarfile.open(archive, "r:gz") as tar:
        for m in tar.getmembers():
            if not m.isfile() or not m.name.endswith(_TEXT_SUFFIXES):
                continue
            text = tar.extractfile(m).read().decode("utf-8", "replace")
            if "orgs/specialists/" in text:
                offenders.append(m.name)
    assert not offenders, (
        f"{org}: stale 'orgs/specialists/' refs in archive: {offenders}"
    )


# --- shared-script capture contract (host_setup helper_script + jobs script) --

def _shared_script_refs_from_policy(text: str) -> list[str]:
    """Every ``_shared`` file ref a policy declares under ``host_setup``
    (``helper_script``) or ``jobs`` (``script``). Hand-parsed leniently,
    mirroring ``export._resolve_shared_sandbox`` — NOT ``policy_mod.load``
    (which env-substitutes and would raise on an unset ``${VAR}``)."""
    pol = yaml.safe_load(text) or {}
    if not isinstance(pol, dict):
        return []
    refs: list[str] = []
    for key, field in (("host_setup", "helper_script"), ("jobs", "script")):
        block = pol.get(key) or []
        if not isinstance(block, list):
            continue
        for entry in block:
            if isinstance(entry, dict):
                ref = entry.get(field, "")
                if isinstance(ref, str) and "_shared" in ref:
                    refs.append(ref)
    return refs


@pytest.mark.parametrize("org", sorted(discover_orgs()))
def test_exported_archive_captures_every_shared_script_ref(org, tmp_path, project_root):
    """No shared sandbox script referenced by ``host_setup`` or ``jobs`` may
    drop out of the archive. Regression contract: ``jobs:`` (the newer JobSpec
    mechanism, ``sandbox/policy.py``) was missed by
    ``_resolve_shared_sandbox`` — coder/general's ``warmup_browser.py``
    silently disappeared and the pre-run job FileNotFound'd at serve time. The
    round-trip-compile test does NOT catch it: ``compile_org`` never reads
    ``jobs:`` (a serve-time concern), so the graph compiles fine without the
    script. This reads the policy FROM the archive and checks every shared ref
    resolves to a member — host-independent (the archive's own self-consistency).
    """
    archive = tmp_path / f"{org}.tar.gz"
    pack_org(org, archive, project_root=project_root)
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        names = [m.name for m in members]
        policy_member = next(
            (m for m in members if m.isfile() and m.name.endswith("/policy.yaml")),
            None,
        )
        policy_text = (
            tar.extractfile(policy_member).read().decode("utf-8", "replace")
            if policy_member is not None else ""
        )
    if policy_member is None:
        pytest.skip(f"{org}: no policy.yaml in archive")
    refs = _shared_script_refs_from_policy(policy_text)
    if not refs:
        pytest.skip(f"{org}: policy declares no shared host_setup/jobs scripts")

    missing = [ref for ref in refs if not any(n.endswith(ref) for n in names)]
    assert not missing, (
        f"{org}: shared script refs declared in policy but missing from archive: "
        f"{missing} — _resolve_shared_sandbox must capture jobs scripts too, "
        f"not just host_setup helper_script"
    )


def test_coder_export_includes_warmup_browser_job_script(tmp_path, project_root):
    """The named regression anchor. coder's ``jobs:`` block references
    ``orgs/_shared/sandbox/warmup_browser.py``. Before the fix this was the one
    shared file that silently dropped — kept as a focused, legible guard so the
    parametrized invariant above always has a concrete failing example if the
    bug returns."""
    archive = tmp_path / "coder.tar.gz"
    pack_org("coder", archive, project_root=project_root)
    with tarfile.open(archive, "r:gz") as tar:
        names = [m.name for m in tar.getmembers()]
    assert any("warmup_browser.py" in n for n in names), (
        "coder jobs: warmup_browser.py missing from export — "
        "_resolve_shared_sandbox must capture jobs scripts, not just host_setup"
    )
