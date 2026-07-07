"""Per-sandbox deps install hook — ``SandboxContainer._install_deps``.

Proves the in-container install step declared by ``policy.yaml``
``sandbox.deps.{apt,pip}``:

* empty deps -> no-op (zero exec calls; today's default for every org);
* apt-only / pip-only / both -> the right number of ``_exec`` calls with the
  right command shape (``apt-get install`` for apt, ``python3 -m pip install``
  for pip);
* independence: apt and pip are SEPARATE best-effort calls (a blocked Debian
  mirror does not also block a pypi attempt);
* ``shlex.quote``-ing: a package with a shell metacharacter is quoted (no
  command injection from an org policy).

We exercise ``_install_deps`` as an UNBOUND method with a stub ``self`` whose
``_exec`` records the scripts — no Docker, no real container. The real
``_exec``->``container.exec_run``->apt/pip path is a separate env-gated LIVE
proof (deferred).
"""
from __future__ import annotations

from types import SimpleNamespace

from pux_harness.sandbox import container, policy


def _stub_self():
    """A fake ``SandboxContainer`` self that records every ``_exec`` script."""
    calls: list[str] = []

    def fake_exec(_container, script):
        calls.append(script)

    return SimpleNamespace(_exec=fake_exec), calls


def _pol(**deps) -> policy.Policy:
    pol = policy.Policy()
    pol.sandbox.deps = policy.DepsSpec(**deps)
    return pol


def test_install_deps_noop_when_empty() -> None:
    self_, calls = _stub_self()
    container.SandboxContainer._install_deps(self_, object(), _pol())
    assert calls == []  # the common case: no deps declared, nothing runs


def test_install_deps_apt_only() -> None:
    self_, calls = _stub_self()
    container.SandboxContainer._install_deps(
        self_, object(), _pol(apt=["ripgrep", "jq"]))
    assert len(calls) == 1
    assert "apt-get update -qq" in calls[0]
    assert "apt-get install -y --no-install-recommends" in calls[0]
    assert "ripgrep" in calls[0] and "jq" in calls[0]
    assert "pip" not in calls[0]


def test_install_deps_pip_only() -> None:
    self_, calls = _stub_self()
    container.SandboxContainer._install_deps(
        self_, object(), _pol(pip=["rich", "httpx==0.27"]))
    assert len(calls) == 1
    assert "python3 -m pip install --no-cache-dir" in calls[0]
    assert "rich" in calls[0] and "httpx==0.27" in calls[0]
    assert "apt-get" not in calls[0]


def test_install_deps_both_are_independent_calls() -> None:
    self_, calls = _stub_self()
    container.SandboxContainer._install_deps(
        self_, object(), _pol(apt=["jq"], pip=["rich"]))
    # Two separate exec calls — apt does NOT short-circuit pip.
    assert len(calls) == 2
    assert "apt-get install" in calls[0]
    assert "pip install" in calls[1]


def test_install_deps_quotes_shell_metachars() -> None:
    # A package name carrying a shell metacharacter must be shlex-quoted so an
    # org policy can't inject into the apt/pip command line.
    self_, calls = _stub_self()
    container.SandboxContainer._install_deps(
        self_, object(), _pol(pip=["rich; rm -rf /"]))
    assert len(calls) == 1
    # shlex.quote wraps the hostile token in single quotes (literal).
    assert "'rich; rm -rf /'" in calls[0]
    # And the injection payload is NOT loose in the command.
    assert "rm -rf /" not in calls[0].replace("'rich; rm -rf /'", "")
