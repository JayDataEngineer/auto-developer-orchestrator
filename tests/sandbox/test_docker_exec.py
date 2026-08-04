"""Tests for pux_harness.sandbox.docker_exec — the direct Docker exec path.

All tests mock the Docker SDK; no real container is needed.
"""
from __future__ import annotations

import concurrent.futures
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from pux_harness.sandbox.docker_exec import (
    DockerExecClient,
    ExecTimeout,
    _DEFAULT_TIMEOUT,
    _NO_TIMEOUT_CEILING,
    _discover,
    _resolve_project,
    _resolve_timeout,
    get_exec_client,
)


# --- _resolve_timeout --------------------------------------------------------


class TestResolveTimeout:
    """``_resolve_timeout`` is the SINGLE gate that prevents the
    ``timeout=0`` footgun. deepagents' filesystem middleware documents
    ``timeout=0`` to the model as "Use 0 for no-timeout execution on backends
    that support it" (filesystem.py:414/1683). Without this helper,
    ``future.result(timeout=0)`` trips INSTANTLY and the failure surfaces as
    a misleading "model stream stalled" notice (see ``test_prompt_stall_recovery``
    regression tests for the user-visible symptom).
    """

    def test_none_uses_default_timeout(self):
        assert _resolve_timeout(None) == _DEFAULT_TIMEOUT == 120

    def test_zero_uses_no_timeout_ceiling(self):
        """The deepagents API contract: ``timeout=0`` means 'no-timeout
        execution'. We use the Docker SDK's HTTP read ceiling (300s) as the
        effective deadline since we can't actually block forever."""
        assert _resolve_timeout(0) == _NO_TIMEOUT_CEILING == 300

    def test_negative_uses_no_timeout_ceiling(self):
        """Negative timeouts are nonsense; treat them the same as 0
        (the 'no-timeout' ceiling) rather than crashing."""
        assert _resolve_timeout(-1) == _NO_TIMEOUT_CEILING
        assert _resolve_timeout(-999) == _NO_TIMEOUT_CEILING

    def test_positive_uses_value_verbatim(self):
        assert _resolve_timeout(5) == 5
        assert _resolve_timeout(60) == 60
        assert _resolve_timeout(3600) == 3600  # deepagents max_execute_timeout

    def test_zero_does_not_instant_fail(self):
        """THE regression: ``_resolve_timeout(0)`` MUST be > 0. The pre-fix
        ``effective_timeout = timeout if timeout is not None else _DEFAULT_TIMEOUT``
        returned 0 verbatim, ``future.result(timeout=0)`` raised
        ``concurrent.futures.TimeoutError`` immediately, and the user saw a
        fake 'stream stalled' notice for a command that never had a chance
        to run. Proven in ``.pux/stall.log`` (2026-07-22T02:02:42, session
        67f05375..., org coder): ``exec timed out after 0s``."""
        assert _resolve_timeout(0) > 0


# --- _resolve_project ---------------------------------------------------------


def test_resolve_project_default_is_repo_root(monkeypatch):
    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    result = _resolve_project()
    assert result.endswith("auto-developer-orchestrator")


def test_resolve_project_env_override(monkeypatch):
    monkeypatch.setenv("PUX_PROJECT_PATH", "/custom/path")
    assert _resolve_project() == "/custom/path"


# --- _discover ----------------------------------------------------------------


def test_discover_returns_container_name():
    fake_container = SimpleNamespace(name="pux-sandbox-general")
    fake_client = MagicMock()
    fake_client.containers.list.return_value = [fake_container]

    result = _discover(fake_client, "/my/project")
    assert result == "pux-sandbox-general"
    fake_client.containers.list.assert_called_once_with(
        filters={"label": "openshell.project-path=/my/project", "status": "running"}
    )


def test_discover_returns_none_when_empty():
    fake_client = MagicMock()
    fake_client.containers.list.return_value = []
    assert _discover(fake_client, "/my/project") is None


def test_discover_raises_on_multiple_containers():
    fake_client = MagicMock()
    fake_client.containers.list.return_value = [
        SimpleNamespace(name="a"),
        SimpleNamespace(name="b"),
    ]
    with pytest.raises(RuntimeError, match="single-tenant invariant violated"):
        _discover(fake_client, "/my/project")


def test_discover_raises_on_docker_api_error():
    from docker.errors import APIError

    fake_client = MagicMock()
    fake_client.containers.list.side_effect = APIError("daemon down")
    with pytest.raises(RuntimeError, match="docker list failed"):
        _discover(fake_client, "/my/project")


# --- DockerExecClient ---------------------------------------------------------


class TestDockerExecClient:
    def _make_client(self, **kwargs):
        with patch("pux_harness.sandbox.docker_exec.docker") as mock_docker:
            mock_docker.from_env.return_value = MagicMock()
            return DockerExecClient(container="test-container", **kwargs)

    def test_exec_success(self):
        client = self._make_client()
        fake_result = SimpleNamespace(output=b"hello\n", exit_code=0)
        client._client.containers.get.return_value.exec_run.return_value = fake_result

        out, code = client.exec("echo hello")
        assert out == "hello\n"
        assert code == 0

    def test_exec_nonzero_exit(self):
        client = self._make_client()
        fake_result = SimpleNamespace(output=b"error\n", exit_code=1)
        client._client.containers.get.return_value.exec_run.return_value = fake_result

        out, code = client.exec("bad command")
        assert out == "error\n"
        assert code == 1

    def test_exec_bytes_output_decoded(self):
        client = self._make_client()
        fake_result = SimpleNamespace(output=b"\xc3\xa9l\xc3\xa8ve", exit_code=0)
        client._client.containers.get.return_value.exec_run.return_value = fake_result

        out, _ = client.exec("echo stuff")
        assert isinstance(out, str)

    def test_exec_timeout_raises(self):
        client = self._make_client()
        with patch.object(concurrent.futures.ThreadPoolExecutor, "submit") as mock_submit:
            future = MagicMock()
            future.result.side_effect = concurrent.futures.TimeoutError()
            mock_submit.return_value = future

            with pytest.raises(ExecTimeout, match="timed out"):
                client.exec("sleep 999", timeout=5)

    def test_exec_no_timeout_uses_direct_call(self):
        client = self._make_client()
        fake_result = SimpleNamespace(output=b"ok", exit_code=0)
        client._client.containers.get.return_value.exec_run.return_value = fake_result

        out, code = client.exec("echo ok")
        assert out == "ok"

    def test_exec_timeout_zero_uses_no_timeout_ceiling(self):
        """REGRESSION (2026-07-22): the deepagents filesystem middleware
        forwards ``timeout=0`` to ``execute(timeout=0)`` when the LLM votes
        for "no-timeout execution" (the documented contract). The pre-fix
        code passed 0 verbatim to ``future.result(timeout=0)`` which tripped
        INSTANTLY — every long-running agent command appeared to "stall"
        without ever running. ``_resolve_timeout`` must coerce 0 → the SDK
        HTTP ceiling so the command actually has time to execute."""
        client = self._make_client()
        fake_result = SimpleNamespace(output=b"done", exit_code=0)

        captured_timeouts: list[int] = []

        class _FakeFuture:
            def result(self, timeout=None):
                captured_timeouts.append(timeout)
                return fake_result

        with patch.object(
            concurrent.futures.ThreadPoolExecutor, "submit", return_value=_FakeFuture()
        ):
            out, code = client.exec("uv run pux direct --org coder", timeout=0)

        assert out == "done"
        assert code == 0
        assert captured_timeouts == [_NO_TIMEOUT_CEILING], (
            f"timeout=0 must reach future.result() as {_NO_TIMEOUT_CEILING}s "
            f"(SDK ceiling), not 0 (instant fail). Got {captured_timeouts}."
        )

    def test_exec_timeout_negative_uses_no_timeout_ceiling(self):
        """Defense: negative timeouts (nonsense caller input) must not crash
        or instant-fail either; coerce to the same SDK ceiling."""
        client = self._make_client()
        fake_result = SimpleNamespace(output=b"ok", exit_code=0)

        captured_timeouts: list[int] = []

        class _FakeFuture:
            def result(self, timeout=None):
                captured_timeouts.append(timeout)
                return fake_result

        with patch.object(
            concurrent.futures.ThreadPoolExecutor, "submit", return_value=_FakeFuture()
        ):
            client.exec("echo ok", timeout=-42)

        assert captured_timeouts == [_NO_TIMEOUT_CEILING]

    def test_container_not_found_raises(self):
        from docker.errors import NotFound

        client = self._make_client()
        client._client.containers.get.side_effect = NotFound("gone")
        with pytest.raises(RuntimeError, match="vanished mid-run"):
            client.exec("echo hi")


# --- get_exec_client / shared_exec -------------------------------------------


def test_get_exec_client_returns_client():
    with patch("pux_harness.sandbox.docker_exec.docker") as mock_docker:
        mock_docker.from_env.return_value = MagicMock()
        client = get_exec_client(container="x")
        assert isinstance(client, DockerExecClient)
        assert client._container == "x"
