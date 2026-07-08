"""Tests for ``pux_harness.pack`` — the org pack command (``pux pack``).

Exercises the full pack pipeline: manifest-driven file collection,
shared-dep resolution, manifest generation, and archive structure.  Uses
``tmp_path`` for isolation; no Docker, no server, no tokens.

What ships is DECLARED via the pack manifest (``pux_harness.manifest``), not the
old hardcoded ``_collect_org_files`` allowlist (removed in P3 — see
``test_legacy_allowlist_collector_is_removed``). Collection goes through the
declarative default-deny collector here via ``_collect``.
"""
from __future__ import annotations

import json
import tarfile
from pathlib import Path

import pytest

from pux_harness.agent.orgs import discover_orgs
from pux_harness.manifest import collect_pack_files, load_manifest
from pux_harness.pack import (
    _build_manifest,
    _resolve_shared_agents,
    _resolve_shared_skills,
    _resolve_shared_sandbox,
    _resolve_tool_servers,
    pack_org,
)


def _collect(org_dir):
    """Manifest-driven collection — the successor to the removed allowlist
    (``_collect_org_files``). Every org-local collection in these tests goes
    through the declarative default-deny collector now."""
    return collect_pack_files(org_dir, load_manifest(org_dir))


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _tar_names(tar_path: Path) -> list[str]:
    """Sorted member names from a tar.gz (files only)."""
    with tarfile.open(tar_path, "r:gz") as tar:
        return sorted(m.name for m in tar.getmembers() if m.isfile())


def _read_manifest(tar_path: Path, org: str) -> dict:
    """Extract manifest.json from the archive."""
    with tarfile.open(tar_path, "r:gz") as tar:
        mf = tar.extractfile(f"{org}/manifest.json")
        assert mf is not None, "manifest.json missing from archive"
        return json.loads(mf.read())


def _read_file_from_tar(tar_path: Path, name: str) -> str:
    """Read a text file from the archive."""
    with tarfile.open(tar_path, "r:gz") as tar:
        f = tar.extractfile(name)
        assert f is not None, f"{name} missing from archive"
        return f.read().decode()


# ---------------------------------------------------------------------------
# Unit tests — file collection
# ---------------------------------------------------------------------------

class TestCollectOrgFiles:
    """The manifest collector (``collect_pack_files`` via ``_collect``) picks up
    every file in the org dir — the default-manifest path (no ``package:``
    block) reproduces the legacy allowlist's reach, now declaratively."""

    def test_general_has_core_files(self):
        from pux_harness.agent.orgs import _org_path
        org_dir = _org_path("general")
        files = _collect(org_dir)
        # general/ has AGENTS.md + org.yaml (agents are in _shared, not local)
        assert any("AGENTS.md" in k for k in files)
        assert any("org.yaml" in k for k in files)

    def test_twitter_agent_has_profile_and_policy(self):
        from pux_harness.agent.orgs import _org_path
        org_dir = _org_path("twitter-agent")
        files = _collect(org_dir)
        assert any("profile.yaml" in k for k in files)
        assert any("policy.yaml" in k for k in files)

    def test_general_policy_yaml_carries_protocols_declaration(self):
        """The org-level ``protocols:`` declaration travels in the pack.

        ``_collect`` returns ``{relname: Path}``; pack walks these collected
        files into the tarball, so whatever general declares in policy.yaml
        ships verbatim. general declares ``protocols: [acp, agui]`` — proving a
        packed org self-describes its client surface (the portability half of
        the protocols contract)."""
        from pux_harness.agent.orgs import _org_path
        org_dir = _org_path("general")
        files = _collect(org_dir)
        # Keys are full relative paths ("orgs/general/policy.yaml").
        pol_key = next((k for k in files if k.endswith("policy.yaml")), None)
        assert pol_key is not None, "general's policy.yaml not collected for pack"
        body = files[pol_key].read_text()
        assert "protocols:" in body, (
            f"protocols declaration missing from collected policy.yaml:\n{body}"
        )
        assert "- acp" in body and "- agui" in body


# ---------------------------------------------------------------------------
# Unit tests — shared dep resolution
# ---------------------------------------------------------------------------

class TestResolveSharedAgents:
    """``_resolve_shared_agents`` finds shared agents the org needs."""

    def test_shared_agents_for_general(self):
        files = _resolve_shared_agents("general")
        # general's roster includes "researcher" and "browser" — both _shared
        assert any("researcher.md" in k for k in files)
        assert any("browser.md" in k for k in files)

    def test_dev_bot_uses_local_only(self):
        """dev-bot defines all its agents locally — no shared agents needed."""
        files = _resolve_shared_agents("dev-bot")
        # dev-bot has dev-bot-explorer, code-worker, web-agent — all local
        assert not files


class TestResolveSharedSkills:
    """``_resolve_shared_skills`` finds skills referenced by agents."""

    def test_researcher_references_source_citation(self):
        files = _resolve_shared_skills("general")
        # The shared researcher agent has skills: ["orgs/_shared/skills"]
        if files:
            assert any("source-citation" in k for k in files)


class TestResolveSharedSandbox:
    """``_resolve_shared_sandbox`` finds host_setup helper scripts."""

    def test_twitter_agent_needs_cookie_extractor(self):
        files = _resolve_shared_sandbox("twitter-agent")
        assert any("extract_browser_cookies.py" in k for k in files)


class TestResolveToolServers:
    """``_resolve_tool_servers`` bundles the shared catalog for orgs that opt in.

    general is the shipped org that consumes a foreign MCP server (web_research)
    — so its pack MUST include the shared ``tool_servers.yaml`` catalog, or the
    packed org would silently lose its MCP capability (the declaration
    references a catalog the pack doesn't carry). This is the pack-side half of
    the MCP-shipping proof; the resolver-side half is in
    ``test_mcp_tool_servers.py`` Part 5."""

    def test_general_declaration_bundles_catalog(self):
        """general declares ``tool_servers: [web_research]`` → pack carries the
        shared catalog the declaration resolves against."""
        files = _resolve_tool_servers("general")
        assert "orgs/_shared/tool_servers.yaml" in files
        assert files["orgs/_shared/tool_servers.yaml"].is_file()

    def test_org_without_declaration_returns_empty(self):
        """An org whose policy.yaml has NO ``tool_servers:`` block packs
        nothing extra (the default, MCP-free state). video-production has a real
        policy.yaml (sandbox image, creds) but no tool_servers → empty, proving
        the ``policy.get('tool_servers')``-is-None branch, not just the
        no-policy-file branch."""
        files = _resolve_tool_servers("video-production")
        assert files == {}


# ---------------------------------------------------------------------------
# Unit tests — manifest
# ---------------------------------------------------------------------------

class TestBuildManifest:
    """``_build_manifest`` produces a valid inventory."""

    def test_manifest_has_required_keys(self):
        from pux_harness.agent.orgs import _org_path
        org_dir = _org_path("general")
        files = _collect(org_dir)
        files.update(_resolve_shared_agents("general"))
        manifest = _build_manifest("general", files)
        assert manifest["org"] == "general"
        assert "agent_roster" in manifest
        assert "categories" in manifest
        assert "files" in manifest
        assert manifest["total_files"] == len(files)


# ---------------------------------------------------------------------------
# Integration test — full pack
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("org", sorted(discover_orgs()))
def test_pack_org_produces_valid_archive(org, tmp_path):
    """Every org packs to a valid tar.gz with manifest + all expected files."""
    output = tmp_path / f"{org}.tar.gz"
    result = pack_org(org, output)
    assert result == output
    assert output.exists()
    assert output.stat().st_size > 0

    # Manifest is present and well-formed
    manifest = _read_manifest(output, org)
    assert manifest["org"] == org
    assert isinstance(manifest["files"], list)
    assert len(manifest["files"]) > 0

    # Root AGENTS.md is always included
    names = _tar_names(output)
    root_agents = f"{org}/AGENTS.md"
    assert root_agents in names, f"root AGENTS.md missing from {org} pack"

    # Org's own AGENTS.md is included
    org_agents = f"{org}/orgs/{org}/AGENTS.md"
    assert org_agents in names, f"org AGENTS.md missing from {org} pack"

    # Org's org.yaml is included
    org_yaml = f"{org}/orgs/{org}/org.yaml"
    assert org_yaml in names, f"org.yaml missing from {org} pack"


def test_pack_output_flag(tmp_path):
    """--output flag controls the output path."""
    output = tmp_path / "custom-name.tar.gz"
    result = pack_org("general", output)
    assert result == output
    assert output.exists()


def test_pack_default_name(tmp_path, monkeypatch):
    """Default output is <org>.tar.gz in the cwd."""
    from pux_harness.agent import orgs as _orgs_mod
    _real_root = Path(__file__).resolve().parent.parent.parent
    monkeypatch.setattr(_orgs_mod, "_orgs_dir", lambda: _real_root / "orgs")
    monkeypatch.chdir(tmp_path)
    result = pack_org("general")
    assert result == Path("general.tar.gz")
    assert result.exists()


def test_pack_unknown_org_raises(tmp_path):
    """Packing a nonexistent org raises FileNotFoundError."""
    with pytest.raises(FileNotFoundError, match="not found"):
        pack_org("nonexistent-xyz", tmp_path / "nope.tar.gz")


def test_archive_files_are_readable(tmp_path):
    """Every file in the archive can be read back as text (no corruption)."""
    output = tmp_path / "general.tar.gz"
    pack_org("general", output)
    with tarfile.open(output, "r:gz") as tar:
        for member in tar.getmembers():
            if member.isfile():
                f = tar.extractfile(member)
                assert f is not None
                data = f.read()
                # Should not raise
                data.decode("utf-8", errors="replace")


# ---------------------------------------------------------------------------
# Security regression — runtime data/ must NEVER leak into a pack
# ---------------------------------------------------------------------------
# org data/ holds runtime state (auth sessions, market data, campaign state) —
# frequently LIVE SECRETS (e.g. orgs/specialists/twitter-agent/data/
# .twitter-session.json browser cookies). Packing it would leak credentials
# into the .tar.gz. The manifest's HARD_EXCLUDE (data/** , .pux/**) is the new
# permanent form of the old hand-commented ``data``-absent-from-tuple contract.
# These tests make any leak a PERMANENT contract failure: a widened include
# glob or a resurrected allowlist fails here. See [[feedback_no_legacy_left_behind]].

_SECRET_SENTINEL = "LEAK-SENTINEL-do-not-ship"


def test_legacy_allowlist_collector_is_removed():
    """The hardcoded ``_collect_org_files`` allowlist is GONE (P3 manifest
    rework). Re-introducing a real collector at that name re-opens the
    implicit-include hole (what ships becomes a Python tuple again, not a
    declared manifest). The name is kept as a STUB that raises, so a stale
    ``from pux_harness.pack import _collect_org_files`` fails loudly — and this
    test fails if anyone turns the stub back into a working collector."""
    import pux_harness.pack as pack_mod

    # The symbol still exists (the stub), but it must NOT return a dict — a
    # resurrected allowlist would. It must raise.
    assert hasattr(pack_mod, "_collect_org_files")
    with pytest.raises(NotImplementedError, match="removed in P3"):
        pack_mod._collect_org_files(Path("/tmp"))


def test_manifest_collector_excludes_data_dir(tmp_path):
    """The manifest collector MUST NOT pick up ``data/`` (HARD_EXCLUDE prunes it
    during the walk — never even descended into). Source primitives alongside
    it still collect via the default-deny include globs."""
    org_dir = tmp_path / "acme"
    (org_dir / "agents").mkdir(parents=True)
    (org_dir / "data").mkdir()
    (org_dir / "AGENTS.md").write_text("# acme\n")
    (org_dir / "agents" / "worker.md").write_text("body\n")
    (org_dir / "data" / ".session.json").write_text(f'{{"cookies":"{_SECRET_SENTINEL}"}}')

    files = _collect(org_dir)

    # Source primitives still collected
    assert any(k.endswith("AGENTS.md") for k in files)
    assert any("agents/worker.md" in k for k in files)
    # data/ is NEVER collected, under any path
    leaked = [k for k in files if "/data/" in k or k.endswith("/data")]
    assert not leaked, f"data/ leaked into collection: {leaked}"


def test_pack_org_never_leaks_data_secrets(tmp_path, monkeypatch):
    """Full-pipeline guarantee: an org whose ``data/`` holds a secret packs an
    archive that does NOT contain the secret — not via the manifest collector,
    not via any shared-dep resolver, nowhere."""
    import pux_harness.pack as pack_mod

    root = tmp_path
    org_dir = root / "orgs" / "acme"
    (org_dir / "agents").mkdir(parents=True)
    (org_dir / "data").mkdir(parents=True)
    (org_dir / "AGENTS.md").write_text("# acme\n")
    (org_dir / "org.yaml").write_text("agents: [worker]\n")
    (org_dir / "agents" / "worker.md").write_text(
        "---\nname: worker\ndescription: d\n---\n\nbody.\n"
    )
    (org_dir / "data" / ".twitter-session.json").write_text(
        f'{{"cookies":"{_SECRET_SENTINEL}"}}'
    )

    # Point the pack module's orgs-layer bindings at the temp tree so pack_org
    # runs end-to-end against "acme" without touching the real orgs/. The project
    # root is threaded explicitly (pack_org's param) — no PROJECT_ROOT module
    # attribute to monkeypatch anymore (the snapshot was killed).
    monkeypatch.setattr(pack_mod, "discover_orgs", lambda: ["acme"])
    monkeypatch.setattr(pack_mod, "_org_path", lambda org: org_dir)
    monkeypatch.setattr(pack_mod, "_orgs_dir", lambda: root / "orgs")
    monkeypatch.setattr(pack_mod, "org_agent_slugs", lambda org: ["worker"])

    output = tmp_path / "acme.tar.gz"
    pack_mod.pack_org("acme", output, project_root=root)

    # The archive must contain no data/ members AND no trace of the sentinel.
    names = _tar_names(output)
    data_members = [n for n in names if "/data/" in n]
    assert not data_members, f"data/ members in archive: {data_members}"
    with tarfile.open(output, "r:gz") as tar:
        for member in tar.getmembers():
            if not member.isfile():
                continue
            f = tar.extractfile(member)
            assert f is not None
            assert _SECRET_SENTINEL not in f.read().decode("utf-8", "replace"), (
                f"secret sentinel leaked into archive member {member.name}"
            )
