"""The UNION surface sync (``pux sync``) — the checked-in dcode workspace.

``pux sync`` emits the union dcode surface (every rostered agent across all
non-underscore orgs, the union of skills, the merged ``.mcp.json``) at the
project root — the workspace the repo IS. This suite proves the checked-in
tree stays in sync with the compiler output:

* in-process: ``check_sync`` / ``emit_union`` byte-compare the emission
  against the checked-in ``.deepagents/`` + ``.mcp.json``;
* subprocess: ``pux sync --check`` exits 0 on a clean tree and exits 1 on
  drift (mutation provocation).

The real-repo tests activate only when the workspace tree is present
(``_HAS_ORGS``); they skip when the suite runs standalone.
"""
from __future__ import annotations

import json
import shutil
import subprocess
from pathlib import Path

import pytest

from compiler.emit import check_sync, emit_union

_REPO = Path(__file__).resolve().parents[2]
_HAS_ORGS = (_REPO / "profiles" / "_shared" / "tool_servers.yaml").is_file()

pytestmark = pytest.mark.skipif(not _HAS_ORGS, reason="workspace tree not present")


def _sync_check_subprocess() -> subprocess.CompletedProcess[str]:
    """``pux sync --check`` as the operator runs it — from the repo root."""
    return subprocess.run(
        ["uv", "run", "pux", "sync", "--check"],
        cwd=_REPO, capture_output=True, text=True, timeout=300, check=False,
    )


def _checked_in_agent_mds() -> list[Path]:
    return sorted((_REPO / ".deepagents" / "agents").glob("*/AGENTS.md"))


def test_union_is_checked_in_clean():
    """The checked-in surface matches the compiler output with zero drift."""
    result = check_sync(project_root=_REPO)
    assert result["ok"], result
    assert result["drifted"] == []
    assert result["missing"] == []
    assert result["stale"] == []
    assert result["mcp_drift"] == []


def test_union_emission_is_byte_identical():
    """``emit_union(out=tmp)`` reproduces the checked-in tree exactly.

    The plan's (a): emit in-process to a tmp dir, then run the SAME
    ``--check`` comparator against it — the checked-in tree must come out
    clean byte-for-byte (and structurally for ``.mcp.json``)."""
    import tempfile

    with tempfile.TemporaryDirectory(prefix="pux-emit-") as td:
        tmp = Path(td)
        emitted = emit_union(project_root=_REPO, out=tmp)
        assert emitted["agents"], "union emitted no agents"
        assert emitted["skills"], "union emitted no skills"
        assert emitted["mcp"], "union emitted no mcp servers"
        result = check_sync(project_root=_REPO, out=tmp)
        assert result["ok"], result


def test_sync_check_subprocess_exits_zero():
    """The CLI gate the CI would run passes on the checked-in tree."""
    if shutil.which("uv") is None:
        pytest.skip("uv not on PATH")
    proc = _sync_check_subprocess()
    assert proc.returncode == 0, proc.stdout + proc.stderr


def test_mutation_provocation_is_detected_and_restore_is_clean():
    """A single byte of drift in a checked-in AGENTS.md fails the gate.

    Mutate, assert exit 1, restore, assert exit 0 again — proving the gate
    actually reads the checked-in tree (it cannot silently pass)."""
    if shutil.which("uv") is None:
        pytest.skip("uv not on PATH")
    targets = _checked_in_agent_mds()
    assert targets, "no checked-in agent AGENTS.md to provoke"
    victim = targets[0]
    original = victim.read_bytes()
    try:
        victim.write_bytes(original + b"\n")
        proc = _sync_check_subprocess()
        assert proc.returncode != 0, (
            "sync --check passed on a mutated tree — the gate is not reading "
            f"the checked-in {victim.relative_to(_REPO)}"
        )
        # the drift must NAME the mutated file (CLI paths are .deepagents-relative)
        assert victim.name in proc.stderr
    finally:
        victim.write_bytes(original)
    proc = _sync_check_subprocess()
    assert proc.returncode == 0, proc.stdout + proc.stderr


def test_mcp_json_servers_are_declared_by_orgs():
    """Every server in the checked-in .mcp.json is emitted from the union —
    the file is compiler output, not hand-kept (structural side of the sync)."""
    cfg = json.loads((_REPO / ".mcp.json").read_text())
    servers = set(cfg.get("mcpServers") or {})
    assert servers, ".mcp.json declares no servers"
    # every server key must round-trip through an emission
    import tempfile

    with tempfile.TemporaryDirectory(prefix="pux-mcp-") as td:
        tmp = Path(td)
        emit_union(project_root=_REPO, out=tmp)
        emitted = json.loads((tmp / ".mcp.json").read_text()).get("mcpServers") or {}
        assert set(emitted) == servers, (
            f"checked-in .mcp.json servers {sorted(servers)} differ from the "
            f"emitted union {sorted(emitted)}"
        )
