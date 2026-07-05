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
    _discover,
    _resolve_project,
    get_exec_client,
)


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
