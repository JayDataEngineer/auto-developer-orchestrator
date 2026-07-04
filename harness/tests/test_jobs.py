"""In-sandbox prep-job runner (Phase 14). Exercises ``run_jobs`` with the exec
client stubbed — no Docker, no real container. Proves warn-and-continue, timeout
enforcement, and empty-policy no-op."""
from __future__ import annotations

from types import SimpleNamespace

import pytest

from pux_harness.sandbox import jobs, policy
from pux_harness.sandbox.docker_exec import ExecTimeout


def _spec(**kw) -> policy.JobSpec:
    base = dict(name="j", script="prep.py", args=[], timeout=0, description="")
    base.update(kw)
    return policy.JobSpec(**base)


def _fake_exec(results: dict[str, tuple[str, int]] | None = None):
    """A fake ``exec_client.exec`` that records calls and returns canned results.

    ``results`` maps command substrings to (output, exit_code). If a command
    doesn't match any key, returns ("", 0).
    """
    calls: list[str] = []

    def fake(command: str, *, timeout: int | None = None):
        calls.append(command)
        if results:
            for pattern, (out, rc) in results.items():
                if pattern in command:
                    return out, rc
        return "", 0

    fake.calls = calls
    return fake


def test_empty_jobs_returns_empty() -> None:
    ec = SimpleNamespace(exec=lambda cmd, **kw: ("", 0))
    assert jobs.run_jobs(None, ec) == []
    assert jobs.run_jobs(policy.Policy(), ec) == []


def test_single_ok(tmp_path: Path) -> None:
    script = tmp_path / "prep.py"
    script.write_text("print('done')")
    fake = _fake_exec({"prep.py": ("done\n", 0)})

    pol = policy.Policy(jobs=[_spec(script=str(script))])
    ec = SimpleNamespace(exec=fake)
    results = jobs.run_jobs(pol, ec)

    assert len(results) == 1
    assert results[0].name == "j"
    assert results[0].status == "ok"
    assert results[0].error is None
    assert results[0].duration >= 0


def test_warn_and_continue_on_failure(tmp_path: Path) -> None:
    script1 = tmp_path / "bad.py"
    script1.write_text("raise SystemExit(1)")
    script2 = tmp_path / "good.py"
    script2.write_text("print('ok')")

    call_count = [0]

    def fake_exec(command: str, *, timeout: int | None = None):
        call_count[0] += 1
        if "bad.py" in command:
            return "traceback\n", 1
        return "ok\n", 0

    pol = policy.Policy(jobs=[
        _spec(name="bad-job", script=str(script1)),
        _spec(name="good-job", script=str(script2)),
    ])
    ec = SimpleNamespace(exec=fake_exec)
    results = jobs.run_jobs(pol, ec)

    assert len(results) == 2
    assert results[0].name == "bad-job"
    assert results[0].status == "failed"
    assert results[1].name == "good-job"
    assert results[1].status == "ok"
    # Both ran — warn-and-continue
    assert call_count[0] == 2


def test_timeout_enforcement() -> None:
    def slow_exec(command: str, *, timeout: int | None = None):
        raise ExecTimeout(f"timed out after {timeout}s")

    pol = policy.Policy(jobs=[_spec(name="slow", script="sleep.py", timeout=5)])
    ec = SimpleNamespace(exec=slow_exec)
    results = jobs.run_jobs(pol, ec)

    assert len(results) == 1
    assert results[0].status == "timeout"
    assert "5s" in (results[0].error or "")


def test_unnamed_job_fails() -> None:
    pol = policy.Policy(jobs=[_spec(name="", script="x.py")])
    ec = SimpleNamespace(exec=lambda cmd, **kw: ("", 0))
    results = jobs.run_jobs(pol, ec)

    assert len(results) == 1
    assert results[0].status == "failed"
    assert "no name" in (results[0].error or "")


def test_no_script_fails() -> None:
    pol = policy.Policy(jobs=[_spec(name="noscript", script="")])
    ec = SimpleNamespace(exec=lambda cmd, **kw: ("", 0))
    results = jobs.run_jobs(pol, ec)

    assert len(results) == 1
    assert results[0].status == "failed"
    assert "no script" in (results[0].error or "")


def test_args_passed_through() -> None:
    calls: list[str] = []

    def fake_exec(command: str, *, timeout: int | None = None):
        calls.append(command)
        return "", 0

    pol = policy.Policy(jobs=[_spec(
        name="args-test",
        script="prep.py",
        args=["--input", "/data", "--output", "/out"],
    )])
    ec = SimpleNamespace(exec=fake_exec)
    jobs.run_jobs(pol, ec)

    assert len(calls) == 1
    assert "python3 prep.py --input /data --output /out" == calls[0]


def test_timeout_passed_to_exec() -> None:
    timeouts: list[int | None] = []

    def fake_exec(command: str, *, timeout: int | None = None):
        timeouts.append(timeout)
        return "", 0

    pol = policy.Policy(jobs=[_spec(name="t", script="x.py", timeout=120)])
    ec = SimpleNamespace(exec=fake_exec)
    jobs.run_jobs(pol, ec)

    assert timeouts == [120]


def test_no_timeout_passes_none() -> None:
    timeouts: list[int | None] = []

    def fake_exec(command: str, *, timeout: int | None = None):
        timeouts.append(timeout)
        return "", 0

    pol = policy.Policy(jobs=[_spec(name="t", script="x.py", timeout=0)])
    ec = SimpleNamespace(exec=fake_exec)
    jobs.run_jobs(pol, ec)

    assert timeouts == [None]


def test_unexpected_exception_captured() -> None:
    def exploding_exec(command: str, *, timeout: int | None = None):
        raise RuntimeError("docker daemon gone")

    pol = policy.Policy(jobs=[_spec(name="explode", script="x.py")])
    ec = SimpleNamespace(exec=exploding_exec)
    results = jobs.run_jobs(pol, ec)

    assert len(results) == 1
    assert results[0].status == "failed"
    assert "docker daemon gone" in (results[0].error or "")
