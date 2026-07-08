"""``pack_org(oci=True)`` emits a layered OCI artifact via the REAL ``oras`` CLI
(dynamic-tools P5) — the live end-to-end proof.

The unit mechanics (layer grouping, provenance shape, digest determinism) are
proven offline in ``pux-harness/tests/harness/test_oci.py`` via an injected
runner. HERE we prove the ACTUAL ``oras`` shell-out: the artifact is emitted to a
local ``oci-layout``, ``oras manifest fetch`` reads its layers, tampering a
learned function changes the library-layer digest (the integrity contract), and
``oras pull`` round-trips the content. Skipped where ``oras`` is absent
([[verify-or-die]] — the live proof is the point; download oras to run it).

No Docker, no server, no tokens. Mirrors the foreign-root fixture in
``test_export_hooks.py``.
"""
from __future__ import annotations

import json
import shutil
import subprocess
import tarfile
from pathlib import Path

import pytest

from pux_harness.oci import OciError
from pux_harness.pack import pack_org

_ORCHESTRATOR_ROOT = Path(__file__).resolve().parents[2]
_HAS_ORAS = shutil.which("oras") is not None


def _stage_org(root: Path, org: str = "acme", worker: str = "worker") -> Path:
    """A minimal synthetic org whose ``lib/`` carries a learned function (the
    integrity target) so the agent-library layer is non-empty."""
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
    (org_dir / "sandbox" / "helper.py").write_text("def run():\n    return 1\n")
    (org_dir / "lib" / "functions" / "learned.py").write_text(
        "def learned():\n    return 42\n"
    )
    return org_dir


def _clean_gitleaks_runner(cmd, **kw):
    """gitleaks stand-in reporting CLEAN so the pack's P4 hook passes without the
    binary (the OCI test's concern is the oras emit, not the secrets gate)."""
    class _P:
        def __init__(self, rc=0):
            self.returncode = rc
            self.stdout = ""
            self.stderr = ""
    if cmd[:2] == ["gitleaks", "version"]:
        return _P(0)
    if cmd[:2] == ["gitleaks", "detect"]:
        Path(cmd[cmd.index("--report-path") + 1]).write_text("[]")
        return _P(0)
    return _P(1)


def _manifest_digest(layout: Path, tag: str = "v1") -> dict:
    out = subprocess.run(
        ["oras", "manifest", "fetch", "--oci-layout", f"{layout}:{tag}"],
        capture_output=True, text=True, check=True,
    )
    return json.loads(out.stdout)


# ---------------------------------------------------------------------------

@pytest.mark.skipif(not _HAS_ORAS, reason="oras CLI required for the live OCI proof")
def test_pack_emits_oci_artifact(tmp_path, monkeypatch):
    """``pack_org(oci=True)`` writes a local oci-layout + provenance.json with the
    manifest digest + the 3 layered descriptors (config / source-code / library)."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    _stage_org(root)
    out = tmp_path / "acme.tar.gz"
    layout = tmp_path / "acme.oci"

    pack_org("acme", output=out, project_root=root, oci=layout,
             gitleaks_runner=_clean_gitleaks_runner)
    assert out.exists() and layout.is_dir()
    prov = json.loads((layout / "provenance.json").read_text())
    assert prov["artifact"]["digest"].startswith("sha256:")
    assert [layer["type"] for layer in prov["layers"]] == ["config", "source-code", "agent-library"]
    # the agent-library layer (the integrity target) has a real digest
    lib = next(layer for layer in prov["layers"] if layer["type"] == "agent-library")
    assert lib["digest"].startswith("sha256:") and lib["size"] > 0


@pytest.mark.skipif(not _HAS_ORAS, reason="oras CLI required for the live OCI proof")
def test_oci_layers_inspectable_via_oras(tmp_path, monkeypatch):
    """A consumer reads the artifact's layers + their SHA-256 digests with the
    standard ``oras manifest fetch`` — output is consumable by oras/crane/skopeo."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    _stage_org(root)
    layout = tmp_path / "acme.oci"
    pack_org("acme", output=tmp_path / "acme.tar.gz", project_root=root, oci=layout,
             gitleaks_runner=_clean_gitleaks_runner)

    manifest = _manifest_digest(layout)
    layers = manifest["layers"]
    assert len(layers) == 2  # source-code + agent-library (config is separate)
    media = {layer["mediaType"] for layer in layers}
    assert "application/vnd.pux.org.layer.agent-library.v1.tar" in media
    assert "application/vnd.pux.org.layer.source-code.v1.tar" in media
    # org.pux.* manifest annotations
    assert manifest["annotations"].get("org.pux.org") == "acme"


@pytest.mark.skipif(not _HAS_ORAS, reason="oras CLI required for the live OCI proof")
def test_oci_tamper_detection_live(tmp_path, monkeypatch):
    """THE integrity contract, via REAL oras: mutate a learned function → the
    agent-library layer's SHA-256 digest changes (a tampered pack is detectable
    on verify). The manifest digest also moves (content-addressed)."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    org_dir = _stage_org(root)
    layout_a = tmp_path / "a.oci"
    layout_b = tmp_path / "b.oci"
    pack_org("acme", output=tmp_path / "a.tar.gz", project_root=root, oci=layout_a,
             gitleaks_runner=_clean_gitleaks_runner)

    # Tamper the learned function (the agent "learned" something different).
    (org_dir / "lib" / "functions" / "learned.py").write_text(
        "def learned():\n    return 999  # TAMPERED\n")
    pack_org("acme", output=tmp_path / "b.tar.gz", project_root=root, oci=layout_b,
             gitleaks_runner=_clean_gitleaks_runner)

    prov_a = json.loads((layout_a / "provenance.json").read_text())
    prov_b = json.loads((layout_b / "provenance.json").read_text())
    lib_a = next(layer for layer in prov_a["layers"] if layer["type"] == "agent-library")["digest"]
    lib_b = next(layer for layer in prov_b["layers"] if layer["type"] == "agent-library")["digest"]
    assert lib_a != lib_b, "tampering the library did NOT change its digest — integrity broken"
    assert prov_a["artifact"]["digest"] != prov_b["artifact"]["digest"]


@pytest.mark.skipif(not _HAS_ORAS, reason="oras CLI required for the live OCI proof")
def test_oci_pull_round_trips_content(tmp_path, monkeypatch):
    """``oras pull`` recovers the layer tars; the agent-library tar contains the
    learned function (the consumer unpacks an artifact with a ``lib/`` folder)."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    _stage_org(root)
    layout = tmp_path / "acme.oci"
    pack_org("acme", output=tmp_path / "acme.tar.gz", project_root=root, oci=layout,
             gitleaks_runner=_clean_gitleaks_runner)

    pulled = tmp_path / "pulled"
    pulled.mkdir()
    subprocess.run(["oras", "pull", "--oci-layout", f"{layout}:v1", "-o", str(pulled)],
                   capture_output=True, check=True)
    # the library layer tar is recovered; extract it → the learned function ships
    lib_tar = next(pulled.glob("agent-library.tar"), None)
    assert lib_tar is not None, "agent-library.tar not recovered by oras pull"
    with tarfile.open(lib_tar) as tar:
        names = tar.getnames()
    assert any(n.endswith("lib/functions/learned.py") for n in names), names


def test_pack_org_oci_failclear_when_oras_absent(tmp_path, monkeypatch):
    """``oras`` absent → ``OciError`` (fail-clear; no silent skip). The validated
    .tar.gz STILL ships — pack_org writes it before the OCI emit. Proven via an
    injected runner that reports oras unavailable (no need to uninstall the binary)."""
    monkeypatch.chdir(_ORCHESTRATOR_ROOT)
    root = tmp_path / "foreign"
    root.mkdir()
    _stage_org(root)
    out = tmp_path / "acme.tar.gz"

    class _NoOras:
        def __call__(self, cmd, **kw):
            class _P:
                returncode = 0 if cmd[:2] != ["oras", "version"] else 1
                stdout = ""
                stderr = ""
            return _P()

    with pytest.raises(OciError, match="oras binary not found"):
        pack_org("acme", output=out, project_root=root, oci=tmp_path / "acme.oci",
                 gitleaks_runner=_clean_gitleaks_runner, oras_runner=_NoOras())
    # the tarball shipped despite the OCI refusal (pack writes it first)
    assert out.exists()
