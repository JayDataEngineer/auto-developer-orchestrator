"""Host-side prep-hook runner (Phase 13). Exercises the real ``run_host_setup``
code path with the subprocess layer (``host_setup._run``) stubbed — no uv, no
venv creation, no network. The cache logic is proven by pre-staging the venv
marker so the install path is observable."""
from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace

import pytest

from pux_harness.sandbox import host_setup, policy


def _hook(**kw) -> policy.HostSetupHook:
    base = dict(
        name="h", helper_script="helper.py", python_deps=[], args=[], exports={}
    )
    base.update(kw)
    return policy.HostSetupHook(**base)


def _fake_run(
    helper_stdout: str = "COOKIE-B64\n",
    venv_rc: int = 0,
    install_rc: int = 0,
    helper_rc: int = 0,
):
    """A fake ``_run`` that records every command and returns canned results,
    branching on cmd[0]/cmd[1] to tell the uv-venv / uv-install / helper calls
    apart (exactly how the real runner tells them apart)."""
    calls: list[list[str]] = []

    def fake(cmd, cwd, env):
        calls.append(list(cmd))
        if cmd[0] == "uv" and "venv" in cmd:
            return SimpleNamespace(returncode=venv_rc, stdout="", stderr="venv-err")
        if cmd[0] == "uv" and "install" in cmd:
            return SimpleNamespace(returncode=install_rc, stdout="", stderr="install-err")
        return SimpleNamespace(
            returncode=helper_rc,
            stdout=helper_stdout,
            stderr="helper-err" if helper_rc else "",
        )

    fake.calls = calls
    return fake


def test_captures_stdout_into_exports(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / "helper.py").write_text("# fake")
    monkeypatch.setattr(host_setup, "_run", _fake_run(helper_stdout="COOKIE-B64\n"))
    pol = policy.Policy(host_setup=[_hook(exports={"TWITTER_COOKIES_B64": "stdout"})])
    out = host_setup.run_host_setup(pol, tmp_path)
    assert out == {"TWITTER_COOKIES_B64": "COOKIE-B64\n"}


def test_no_hooks_is_empty(tmp_path: Path) -> None:
    assert host_setup.run_host_setup(None, tmp_path) == {}
    assert host_setup.run_host_setup(policy.Policy(), tmp_path) == {}


def test_missing_helper_script_fails(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setattr(host_setup, "_run", _fake_run())
    pol = policy.Policy(host_setup=[_hook(exports={"X": "stdout"})])
    with pytest.raises(host_setup.HostSetupError, match="helper_script"):
        host_setup.run_host_setup(pol, tmp_path)


def test_bad_export_source_fails(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / "helper.py").write_text("# fake")
    monkeypatch.setattr(host_setup, "_run", _fake_run())
    pol = policy.Policy(host_setup=[_hook(exports={"X": "stderr"})])
    with pytest.raises(host_setup.HostSetupError, match="unsupported export"):
        host_setup.run_host_setup(pol, tmp_path)


def test_helper_nonzero_fails(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / "helper.py").write_text("# fake")
    monkeypatch.setattr(host_setup, "_run", _fake_run(helper_rc=2))
    pol = policy.Policy(host_setup=[_hook(exports={"X": "stdout"})])
    with pytest.raises(host_setup.HostSetupError, match="exited rc=2"):
        host_setup.run_host_setup(pol, tmp_path)


def test_venv_create_fail_fails(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / "helper.py").write_text("# fake")
    monkeypatch.setattr(host_setup, "_run", _fake_run(venv_rc=1))
    pol = policy.Policy(host_setup=[_hook(exports={"X": "stdout"})])
    with pytest.raises(host_setup.HostSetupError, match="uv venv failed"):
        host_setup.run_host_setup(pol, tmp_path)


def test_install_fail_fails(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / "helper.py").write_text("# fake")
    monkeypatch.setattr(host_setup, "_run", _fake_run(install_rc=1))
    pol = policy.Policy(
        host_setup=[_hook(python_deps=["browser-cookie3"], exports={"X": "stdout"})]
    )
    with pytest.raises(host_setup.HostSetupError, match="uv pip install failed"):
        host_setup.run_host_setup(pol, tmp_path)


def test_install_invoked_with_deps(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / "helper.py").write_text("# fake")
    fake = _fake_run()
    monkeypatch.setattr(host_setup, "_run", fake)
    pol = policy.Policy(
        host_setup=[
            _hook(
                python_deps=["browser-cookie3", "pycryptodome"],
                exports={"X": "stdout"},
            )
        ]
    )
    host_setup.run_host_setup(pol, tmp_path)
    install_calls = [c for c in fake.calls if c[0] == "uv" and "install" in c]
    assert len(install_calls) == 1
    assert "browser-cookie3" in install_calls[0]
    assert "pycryptodome" in install_calls[0]


def test_cache_skips_reinstall(tmp_path: Path, monkeypatch) -> None:
    # Pre-stage a matching venv + marker so the install path is NOT taken.
    (tmp_path / "helper.py").write_text("# fake")
    venv = tmp_path / ".pux" / "venvs" / "h"
    (venv / "bin").mkdir(parents=True)
    (venv / "bin" / "python").write_text("# fake")
    (venv / ".installed").write_text("browser-cookie3\npycryptodome\n")
    fake = _fake_run()
    monkeypatch.setattr(host_setup, "_run", fake)
    pol = policy.Policy(
        host_setup=[
            _hook(
                python_deps=["browser-cookie3", "pycryptodome"],
                exports={"X": "stdout"},
            )
        ]
    )
    out = host_setup.run_host_setup(pol, tmp_path)
    assert out == {"X": "COOKIE-B64\n"}
    # No uv call should fire — only the helper run (cmd[0] is the venv python).
    assert [c for c in fake.calls if c[0] == "uv"] == []


def test_dep_change_invalidates_cache(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / "helper.py").write_text("# fake")
    venv = tmp_path / ".pux" / "venvs" / "h"
    (venv / "bin").mkdir(parents=True)
    (venv / "bin" / "python").write_text("# fake")
    (venv / ".installed").write_text("old-dep\n")  # stale marker
    fake = _fake_run()
    monkeypatch.setattr(host_setup, "_run", fake)
    pol = policy.Policy(
        host_setup=[_hook(python_deps=["new-dep"], exports={"X": "stdout"})]
    )
    host_setup.run_host_setup(pol, tmp_path)
    install_calls = [c for c in fake.calls if c[0] == "uv" and "install" in c]
    assert len(install_calls) == 1
    assert "new-dep" in install_calls[0]
    # marker refreshed to the new dep list
    assert (venv / ".installed").read_text() == "new-dep\n"


def test_unnamed_hook_fails(tmp_path: Path, monkeypatch) -> None:
    (tmp_path / "helper.py").write_text("# fake")
    monkeypatch.setattr(host_setup, "_run", _fake_run())
    pol = policy.Policy(
        host_setup=[
            policy.HostSetupHook(name="", helper_script="helper.py", exports={"X": "stdout"})
        ]
    )
    with pytest.raises(host_setup.HostSetupError, match="needs a name"):
        host_setup.run_host_setup(pol, tmp_path)
