"""F3 — an exported archive is a RUNNABLE package, not just a file bundle.

``test_export_roundtrip_compile.py`` proves every shipped org *recompiles* — but
only because ``pux-harness`` is installed in the test venv. The whole point of an
export is that a consumer runs the org WITHOUT pux-harness installed. These tests
pin that contract: the archive vendors the slim kit + emits ``pyproject.toml`` /
``run.py`` / ``README.md``, so an unpacked tree compiles + runs from the VENDORED
kit alone (third-party deps only — no pux-harness on the path).

The decisive proof is the subprocess: it runs in the unpacked archive dir with
``pux_harness`` resolvable only from that dir, asserts the imported kit is the
VENDORED copy (``__file__`` under the unpack dir, NOT site-packages), and that it
compiles the org. That is the seam ``python run.py`` depends on.
"""
from __future__ import annotations

import subprocess
import sys
import tarfile
import tomllib
from pathlib import Path

import pytest

from pux_harness.agent.orgs import discover_orgs
from pux_harness.export import _KIT_DIR, _KIT_RUNTIME_FILES, export_org

# The kit files that MUST vendor (the portable compiler surface). Mirrors
# ``export._KIT_RUNTIME_FILES``; asserting here too catches a drift where the
# constant drops a file the runner/scaffold still needs.
_EXPECTED_SCAFFOLD = {
    "pux_harness/__init__.py",
    *(f"pux_harness/kit/{n}" for n in _KIT_RUNTIME_FILES),
    "run.py",
    "pyproject.toml",
    "README.md",
}


def _extract(archive: Path, dest: Path) -> Path:
    """Untar <archive> into <dest>; return the top-level <org>/ dir."""
    with tarfile.open(archive, "r:gz") as tar:
        tar.extractall(dest, filter="data")  # type: ignore[arg-type]
    members = [p.name for p in dest.iterdir()]
    assert len(members) == 1, f"expected one top-level org dir, got {members}"
    return dest / members[0]


# --- archive shape: the scaffold ships for every org -------------------------

@pytest.mark.parametrize("org", sorted(discover_orgs()))
def test_archive_carries_runnable_scaffold(org, tmp_path, project_root):
    """Every export vendors the slim kit + pyproject + run.py + README, and the
    manifest inventories them under ``runtime_scaffold``."""
    archive = tmp_path / f"{org}.tar.gz"
    export_org(org, archive, project_root=project_root)
    with tarfile.open(archive, "r:gz") as tar:
        names = {m.name.removeprefix(f"{org}/") for m in tar.getmembers() if m.isfile()}
        mf_file = tar.extractfile(f"{org}/manifest.json")
        assert mf_file is not None
        import json
        mf = json.loads(mf_file.read())
    missing = _EXPECTED_SCAFFOLD - names
    assert not missing, f"{org}: scaffold members missing from archive: {sorted(missing)}"

    # Manifest accounts for the scaffold under its own category.
    assert "runtime_scaffold" in mf["categories"], f"{org}: no runtime_scaffold category"
    scaffold_in_manifest = {
        s.removeprefix(f"{org}/") if s.startswith(f"{org}/") else s
        for s in mf["categories"]["runtime_scaffold"]
    }
    assert _EXPECTED_SCAFFOLD <= scaffold_in_manifest, (
        f"{org}: manifest runtime_scaffold missing members: "
        f"{sorted(_EXPECTED_SCAFFOLD - scaffold_in_manifest)}"
    )


# --- the vendored kit is a byte-identical snapshot of the source -------------

def test_vendored_kit_matches_source_kit(tmp_path, project_root):
    """The vendored kit files are byte-identical to the source kit — no drift.
    If a kit file changes, the snapshot travels with the next export. Guards that
    ``_KIT_RUNTIME_FILES`` stays in sync with the runtime surface."""
    archive = tmp_path / "general.tar.gz"
    export_org("general", archive, project_root=project_root)
    with tarfile.open(archive, "r:gz") as tar:
        for name in _KIT_RUNTIME_FILES:
            member = tar.extractfile(f"general/pux_harness/kit/{name}")
            assert member is not None, f"vendored kit missing {name}"
            vendored = member.read()
            source = (_KIT_DIR / name).read_bytes()
            assert vendored == source, (
                f"vendored kit/{name} drifted from source — re-export to refresh"
            )


# --- the decisive proof: the VENDORED kit compiles the org standalone --------

def test_vendored_kit_compiles_org_standalone(tmp_path, project_root):
    """Unpack a real org's archive to a FOREIGN tmp tree, then in a subprocess
    whose cwd is that tree, import ``compile_org`` and compile the org. The
    subprocess asserts the imported kit is the VENDORED copy (``__file__`` under
    the unpack dir, NOT the installed pux-harness) — proving the archive runs
    without pux-harness on the path."""
    archive = tmp_path / "general.tar.gz"
    export_org("general", archive, project_root=project_root)
    unpack = _extract(archive, tmp_path / "unpack")

    script = (
        "import sys;\n"
        "import pux_harness.kit as k;\n"
        "import os;\n"
        f"kit_file = os.path.abspath(k.__file__);\n"
        f"unpack = {str(unpack)!r};\n"
        "assert kit_file.startswith(unpack), "
        f"'vendored kit not used: ' + kit_file + ' vs ' + unpack;\n"
        "from pux_harness.kit import compile_org;\n"
        "from pux_harness.kit._testing import ScriptedModel;\n"
        "g = compile_org('general', model=ScriptedModel(), tools=[], project_root='.');\n"
        "assert hasattr(g, 'ainvoke'), type(g);\n"
        "print('VENDORED-KIT-OK')\n"
    )
    result = subprocess.run(
        [sys.executable, "-c", script],
        cwd=str(unpack),
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, (
        f"vendored-kit subprocess failed:\n--stdout--\n{result.stdout}\n"
        f"--stderr--\n{result.stderr}"
    )
    assert "VENDORED-KIT-OK" in result.stdout, result.stdout


def test_runner_check_compiles_offline(tmp_path, project_root):
    """``python run.py --check`` from the unpacked archive compiles the org with
    a scripted model — the offline smoke a consumer runs with no key. Proves the
    runner wires the vendored kit + org together end-to-end."""
    archive = tmp_path / "general.tar.gz"
    export_org("general", archive, project_root=project_root)
    unpack = _extract(archive, tmp_path / "unpack")

    result = subprocess.run(
        [sys.executable, "run.py", "--check"],
        cwd=str(unpack),
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, (
        f"run.py --check failed:\n--stdout--\n{result.stdout}\n--stderr--\n{result.stderr}"
    )
    assert "OK: general compiled" in result.stdout, result.stdout


# --- pyproject is a valid, lean, runnable manifest ---------------------------

def test_pyproject_declares_lean_runtime_deps(tmp_path, project_root):
    """The emitted ``pyproject.toml`` parses, declares the kit's runtime deps,
    and carries NO heavy harness deps (docker / fastapi / uvicorn / ag-ui /
    fastmcp / deepagents-acp) — the export must stay slim + runnable."""
    archive = tmp_path / "general.tar.gz"
    export_org("general", archive, project_root=project_root)
    with tarfile.open(archive, "r:gz") as tar:
        member = tar.extractfile("general/pyproject.toml")
        assert member is not None, "pyproject.toml missing from archive"
        data = tomllib.loads(member.read().decode("utf-8"))

    deps = set(data["project"]["dependencies"])
    # The kit's runtime third-party surface must all be declared.
    for required in ("deepagents", "langchain-openai", "langgraph", "pyyaml",
                     "python-dotenv"):
        assert any(d.split("[")[0].split(">")[0].split("<")[0].split("=")[0].strip() == required
                   or d.startswith(required) for d in deps), (
            f"runtime dep {required!r} missing from pyproject: {sorted(deps)}"
        )
    # No heavy harness deps may ride along — the export is slim + runnable.
    forbidden = {"docker", "fastapi", "uvicorn", "ag-ui-langgraph", "fastmcp",
                 "deepagents-acp", "langchain-mcp-adapters"}
    leaked = {f for f in forbidden if any(d.startswith(f) for d in deps)}
    assert not leaked, f"heavy deps leaked into export pyproject: {leaked}"
    assert data["project"]["name"] == "general-pux"
    assert data["build-system"]["build-backend"] == "hatchling.build"


def test_runner_seamless_dotenv_load(tmp_path, project_root, monkeypatch):
    """The exported runner loads the consumer's ``./.env`` (the F3 seam that
    makes a foreign install seamless): ``bootstrap_env_and_logging()`` is called
    at the top of ``run.py``'s ``main()``. Verified statically (the call is
    present + the vendored kit ships the helper) — the live load behavior is
    pinned in ``tests/harness/test_bootstrap.py``."""
    archive = tmp_path / "general.tar.gz"
    export_org("general", archive, project_root=project_root)
    with tarfile.open(archive, "r:gz") as tar:
        run_py = tar.extractfile("general/run.py").read().decode("utf-8")
        bootstrap_src = tar.extractfile(
            "general/pux_harness/kit/_bootstrap.py"
        ).read().decode("utf-8")
    assert "bootstrap_env_and_logging()" in run_py, (
        "run.py must call bootstrap_env_and_logging() at startup"
    )
    assert "from pux_harness.kit import bootstrap_env_and_logging" in run_py
    # The vendored helper IS the seamless seam (find_dotenv(usecwd=True)).
    assert "usecwd=True" in bootstrap_src
