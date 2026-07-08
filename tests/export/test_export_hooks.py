"""``pack_org`` runs the PACK_HOOK_REGISTRY before writing the tarball (dynamic-tools P4).

E2E contract: a syntax-broken agent function or a leaked secret REFUSES the pack
(no tar written). The AST gate is deterministic (stdlib, always runs); the secret
gate runs the REAL ``gitleaks`` CLI (the live proof that a planted fake key refuses
the pack — skipped where gitleaks is absent; the scan LOGIC is unit-proven in
``pux-harness/tests/harness/test_pack_hooks.py`` via an injected runner).

Mirrors the foreign-root fixture in ``test_export_project_root.py`` — no Docker,
no server, no tokens. The planted secret is a synthetic canary, never a live key
([[no-live-keys-in-test-fixtures]]).
"""
from __future__ import annotations

import json
import shutil
import tarfile
from pathlib import Path

import pytest

from pux_harness.pack import pack_org
from pux_harness.pack_hooks import PackHookError

_ORCHESTRATOR_ROOT = Path(__file__).resolve().parents[2]
_HAS_GITLEAKS = shutil.which("gitleaks") is not None
# A SYNTHETIC canary in the GitHub-PAT format. Built by CONCATENATION at module
# load so the SOURCE text never holds a contiguous ``ghp_<36 alnum>`` token: the
# repo's own pre-commit gitleaks hook scans source, and a literal here would
# block the commit. At RUNTIME the two halves assemble into a full high-entropy
# PAT; when the test writes it into a shipped .py, gitleaks flags that file under
# BOTH the github-token and generic-api-key rules — the live pack-gate proof.
# Do NOT collapse this back to a single literal (it re-trips pre-commit). NOT a
# live key — a random high-entropy body with a tell-tale prefix.
_SECRET_CANARY = "ghp_" + "9aF8dK2mN4pQ7rT1vW3xY6zB8cE5fG4hJ2kL"


def _stage_org(root: Path, org: str = "acme", worker: str = "worker") -> Path:
    """A minimal synthetic org that ``pack_org`` can collect + pack."""
    org_dir = root / "orgs" / org
    (org_dir / "agents").mkdir(parents=True)
    (org_dir / "sandbox").mkdir(parents=True)
    (org_dir / "lib" / "functions").mkdir(parents=True)
    (org_dir / "AGENTS.md").write_text(f"# {org} CTO\n")
    (org_dir / "org.yaml").write_text(f"agents: [{worker}]\n")
    (org_dir / "policy.yaml").write_text("sandbox:\n  tier: isolated\n")
    (org_dir / "agents" / f"{worker}.md").write_text(
        f"---\nname: {worker}\ndescription: d\n---\n\nbody.\n"
    )
    # A clean shipped module (ast_check must pass on it).
    (org_dir / "sandbox" / "helper.py").write_text("def run():\n    return 1\n")
    return org_dir


def _manifest(tar_path: Path, org: str) -> dict:
    with tarfile.open(tar_path, "r:gz") as tar:
        f = tar.extractfile(f"{org}/manifest.json")
        assert f is not None
        return json.loads(f.read())


# ---------------------------------------------------------------------------
# Hook 1 — AST gate (deterministic; always runs)
# ---------------------------------------------------------------------------

def test_pack_refuses_on_broken_ast(tmp_path, monkeypatch):
    """A shipped ``.py`` that won't compile REFUSES the pack (ast_check fails
    FIRST — before gitleaks, before any tar byte is written)."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)  # CWD-based default must NOT find tmp org
    root = tmp_path / "foreign"
    root.mkdir()
    org_dir = _stage_org(root)
    # Plant a syntax-broken shipped module (collected via sandbox/**).
    (org_dir / "sandbox" / "broken.py").write_text("def run(\n    oops\n")

    out = tmp_path / "acme.tar.gz"
    with pytest.raises(PackHookError) as ei:
        pack_org("acme", output=out, project_root=root)
    assert ei.value.result.name == "ast_check"
    assert any("broken.py" in s and "SyntaxError" in s for s in ei.value.result.findings)
    # No archive written — the gate fires before the tarball.
    assert not out.exists()


def test_ast_gate_catches_lib_function(tmp_path, monkeypatch):
    """The design's named target — ``lib/functions/*.py`` (level-c dynamic tools).
    A broken graduated function refuses the pack just like a sandbox script."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    org_dir = _stage_org(root)
    (org_dir / "lib" / "functions" / "bad.py").write_text("def (\n")

    out = tmp_path / "acme.tar.gz"
    with pytest.raises(PackHookError) as ei:
        pack_org("acme", output=out, project_root=root)
    assert ei.value.result.name == "ast_check"
    assert any("lib/functions/bad.py" in s for s in ei.value.result.findings)


# ---------------------------------------------------------------------------
# Hook provenance lands in manifest.json (deterministic via clean runner)
# ---------------------------------------------------------------------------

class _CleanProc:
    def __init__(self, returncode=0):
        self.returncode = returncode
        self.stdout = ""
        self.stderr = ""


def _clean_gitleaks_runner(cmd, **kw):
    """A gitleaks stand-in that reports CLEAN (version ok; detect → no findings).
    Used so the manifest-provenance wiring is provable without gitleaks installed."""
    if cmd[:2] == ["gitleaks", "version"]:
        return _CleanProc(0)
    if cmd[:2] == ["gitleaks", "detect"]:
        rpath = cmd[cmd.index("--report-path") + 1]
        Path(rpath).write_text("[]")
        return _CleanProc(0)
    return _CleanProc(1)


def test_clean_pack_records_hook_provenance(tmp_path, monkeypatch):
    """A clean pack writes manifest.json with ``provenance.hooks`` recording both
    gates passing (ast + gitleaks) — the audit surface P5 extends into a standalone
    ``provenance.json``."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    _stage_org(root)

    out = tmp_path / "acme.tar.gz"
    pack_org("acme", output=out, project_root=root,
             gitleaks_runner=_clean_gitleaks_runner)
    assert out.exists()

    prov = _manifest(out, "acme")["provenance"]
    assert prov["all_ok"] is True
    names = [h["name"] for h in prov["hooks"]]
    assert names == ["ast_check", "gitleaks"]
    assert all(h["ok"] for h in prov["hooks"])


# ---------------------------------------------------------------------------
# Hook 2 — gitleaks secret gate (LIVE; real gitleaks CLI)
# ---------------------------------------------------------------------------

@pytest.mark.skipif(not _HAS_GITLEAKS, reason="gitleaks CLI required for the live secret-gate proof")
def test_live_secret_refuses_pack(tmp_path, monkeypatch):
    """The P4 live proof: a planted synthetic API-key canary in a shipped module
    REFUSES the pack via the REAL ``gitleaks`` CLI (no injected runner)."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    org_dir = _stage_org(root)
    # Plant the canary in a shipped module (collected via lib/**).
    (org_dir / "lib" / "functions" / "leaky.py").write_text(
        f'API_KEY = "{_SECRET_CANARY}"\n\ndef run():\n    return API_KEY\n'
    )

    out = tmp_path / "acme.tar.gz"
    with pytest.raises(PackHookError) as ei:
        pack_org("acme", output=out, project_root=root)  # default registry, real gitleaks
    assert ei.value.result.name == "gitleaks"
    assert any("leaky.py" in s for s in ei.value.result.findings)
    assert not out.exists()


@pytest.mark.skipif(not _HAS_GITLEAKS, reason="gitleaks CLI required for the live clean-pack proof")
def test_live_clean_org_packs(tmp_path, monkeypatch):
    """Live clean path: real gitleaks scans the shipped files, finds nothing, and
    the pack succeeds with both hooks ok=True."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    _stage_org(root)

    out = tmp_path / "acme.tar.gz"
    pack_org("acme", output=out, project_root=root)  # real gitleaks, default registry
    assert out.exists()
    prov = _manifest(out, "acme")["provenance"]
    assert prov["all_ok"] is True
    assert [h["name"] for h in prov["hooks"]] == ["ast_check", "gitleaks"]
