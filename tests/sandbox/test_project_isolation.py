"""Project-isolation invariant: the sandbox must NEVER silently bind the harness
repo as the edit target when an agent was spawned against a different project.

The bug (locked here): ``resolve_project_path()`` / ``_resolve_project()`` fall
back to ``project_root()`` (the orchestrator repo) when ``PUX_PROJECT_PATH`` is
unset. A launcher that forgets to pin the edit target (any future launcher)
would then let a coder org spawned against, say,
the ``ray`` project edit the orchestrator's own files — a cross-project leak.

Three layers must hold (defense in depth):
  1. The launcher (``bin/pux`` — the package entry Zed calls by name over PATH,
     and ``scripts/start_pux_aegra.sh`` for the aegra backend) captures the
     caller's CWD and exports ``PUX_PROJECT_PATH`` BEFORE cd-ing into the
     harness repo (which would otherwise discard the editor's project).
     The ACP wire itself is shim-free: Zed → ``pux acp`` → stdio ACP.
  2. ``acp.run_acp`` (the one Python chokepoint every editor path funnels through)
     derives ``PUX_PROJECT_PATH`` from the process CWD if a launcher forgot, and
     LOGS the bind loudly so a wrong target is visible, not silent.
  3. The sandbox resolvers (``container.resolve_project_path``,
     ``docker_exec._resolve_project``) emit a loud stderr WARNING when they fall
     back to the harness repo — the historic single-repo path stays supported,
     but it is never silent.
"""
from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]


# --- Layer 3: sandbox resolver warns loudly on fallback ----------------------


def test_resolve_project_path_unset_warns_and_falls_back(monkeypatch, capsys):
    """No PUX_PROJECT_PATH → loud stderr WARNING + harness-repo fallback."""
    from pux_harness.sandbox import container

    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    # Force a recognizable harness root so the assertion is independent of where
    # the test runs.
    monkeypatch.setattr(container, "project_root", lambda: Path("/hardcoded/harness"))

    result = container.resolve_project_path()
    assert result == "/hardcoded/harness"
    err = capsys.readouterr().err
    assert "PUX_PROJECT_PATH unset" in err
    assert "/hardcoded/harness" in err


def test_resolve_project_path_explicit_is_silent(monkeypatch, capsys):
    """An explicit PUX_PROJECT_PATH is honored with NO warning."""
    from pux_harness.sandbox import container

    monkeypatch.setenv("PUX_PROJECT_PATH", "/explicit/project")
    monkeypatch.setattr(container, "project_root", lambda: Path("/hardcoded/harness"))

    assert container.resolve_project_path() == "/explicit/project"
    assert "PUX_PROJECT_PATH unset" not in capsys.readouterr().err


def test_docker_exec_resolve_project_warns_on_fallback(monkeypatch, capsys):
    """The docker-exec path warns on the same fallback (parity with container)."""
    from pux_harness.sandbox import docker_exec

    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    monkeypatch.setattr(docker_exec, "project_root", lambda: Path("/hardcoded/harness"))

    assert docker_exec._resolve_project() == "/hardcoded/harness"
    assert "PUX_PROJECT_PATH unset" in capsys.readouterr().err


# --- Layer 2: run_acp derives + logs, never silent ---------------------------


@pytest.fixture
def _acp_no_serve(monkeypatch):
    """Neutralize run_acp's serving side so we can test its guard in isolation."""
    monkeypatch.setattr("pux_harness.acp.bootstrap_env_and_logging",
                        lambda **kw: None)
    monkeypatch.setattr("pux_harness.acp.discover_orgs", lambda: {"coder"})

    def _swallow(coro):
        coro.close()  # avoid "coroutine never awaited" noise
        return None

    monkeypatch.setattr("pux_harness.acp.asyncio.run", _swallow)
    # Stub project_root so the "harness root" line is deterministic.
    monkeypatch.setattr("pux_harness.acp.project_root",
                        lambda: Path("/hardcoded/harness"))


def test_run_acp_derives_project_path_from_cwd(_acp_no_serve, monkeypatch, tmp_path, capsys):
    """If a launcher forgot PUX_PROJECT_PATH, run_acp pins it to the process CWD
    (the editor's project) and logs loudly — never silently falls back to the
    harness repo."""
    from pux_harness.acp import run_acp

    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    monkeypatch.chdir(tmp_path)  # simulate the editor's project as CWD

    run_acp("coder")

    assert os.environ["PUX_PROJECT_PATH"] == str(tmp_path)
    err = capsys.readouterr().err
    assert "PUX_PROJECT_PATH was unset" in err
    assert str(tmp_path) in err


def test_run_acp_keeps_explicit_project_path(_acp_no_serve, monkeypatch, capsys):
    """An explicit PUX_PROJECT_PATH (set by bin/pux) is honored and
    NOT overwritten — and the bind is still logged."""
    from pux_harness.acp import run_acp

    monkeypatch.setenv("PUX_PROJECT_PATH", "/caller/pinned/ray-project")
    run_acp("coder")

    assert os.environ["PUX_PROJECT_PATH"] == "/caller/pinned/ray-project"
    err = capsys.readouterr().err
    # Loud confirmation either way.
    assert "/caller/pinned/ray-project" in err
    assert "separate project" in err


def test_run_acp_flags_bind_to_harness_repo(_acp_no_serve, monkeypatch, capsys):
    """When the edit target IS the harness repo itself, the log flags it plainly
    so a misbind is obvious in the editor's stderr."""
    from pux_harness.acp import run_acp

    monkeypatch.setenv("PUX_PROJECT_PATH", "/hardcoded/harness")
    run_acp("coder")

    err = capsys.readouterr().err
    assert "THE HARNESS REPO ITSELF" in err


# --- Layer 1: launchers pin PUX_PROJECT_PATH (static tripwire) ---------------
#
# If any of these break, a future edit re-opened the cross-project leak. Each
# launcher must export PUX_PROJECT_PATH from the caller's CWD (or an explicit
# override) BEFORE cd-ing into the harness repo.


@pytest.mark.parametrize("rel", [
    "bin/pux",
    "scripts/start_pux_aegra.sh",
])
def test_launcher_pins_project_path(rel):
    text = (REPO_ROOT / rel).read_text()
    # Must export PUX_PROJECT_PATH, honoring an existing value, defaulting to the
    # caller's CWD ($PWD) — not the harness repo.
    assert "PUX_PROJECT_PATH" in text, f"{rel} no longer pins PUX_PROJECT_PATH"
    assert "${PUX_PROJECT_PATH" in text or "${PUX_PROJECT_PATH:-" in text, (
        f"{rel} must honor an existing PUX_PROJECT_PATH (use :- default expansion)")


def test_bin_pux_captures_cwd_before_cd():
    """bin/pux must capture $PWD into PUX_PROJECT_PATH BEFORE `cd $REPO` — the cd
    would otherwise discard the caller's project."""
    lines = (REPO_ROOT / "bin/pux").read_text().splitlines()
    pp_line = next(i for i, ln in enumerate(lines)
                   if "PUX_PROJECT_PATH" in ln and "export" in ln)
    cd_line = next(i for i, ln in enumerate(lines) if ln.strip() == 'cd "$REPO"')
    assert pp_line < cd_line, (
        "bin/pux: PUX_PROJECT_PATH export must come BEFORE `cd $REPO`")
