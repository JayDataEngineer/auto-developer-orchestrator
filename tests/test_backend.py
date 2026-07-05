"""Tests for pux_harness.sandbox.backend — the BaseSandbox implementation.

All tests mock DockerExecClient; no real container is needed.
"""
from __future__ import annotations

import base64
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from pux_harness.sandbox.backend import PuxSandboxBackend, _DOWNLOAD_PY, _UPLOAD_PY


@pytest.fixture
def fake_exec():
    """A mock DockerExecClient."""
    return MagicMock()


@pytest.fixture
def backend(fake_exec):
    return PuxSandboxBackend(exec_client=fake_exec)


# --- execute ------------------------------------------------------------------


class TestExecute:
    def test_execute_returns_output_and_exit_code(self, backend, fake_exec):
        fake_exec.exec.return_value = ("hello\n", 0)
        res = backend.execute("echo hello")
        assert res.output == "hello\n"
        assert res.exit_code == 0
        assert res.truncated is False

    def test_execute_logs_command(self, backend, fake_exec):
        fake_exec.exec.return_value = ("", 0)
        backend.execute("ls")
        assert "ls" in backend.execute_log

    def test_execute_passes_timeout(self, backend, fake_exec):
        fake_exec.exec.return_value = ("", 0)
        backend.execute("cmd", timeout=30)
        fake_exec.exec.assert_called_once_with("cmd", timeout=30)


# --- upload_files -------------------------------------------------------------


class TestUploadFiles:
    def test_upload_success(self, backend, fake_exec):
        fake_exec.exec.return_value = ("", 0)
        results = backend.upload_files([("/tmp/test.txt", b"hello")])
        assert len(results) == 1
        assert results[0].path == "/tmp/test.txt"
        assert results[0].error is None

    def test_upload_failure(self, backend, fake_exec):
        fake_exec.exec.return_value = ("permission denied", 1)
        results = backend.upload_files([("/tmp/test.txt", b"data")])
        assert results[0].error is not None
        assert "permission denied" in results[0].error

    def test_upload_encodes_base64(self, backend, fake_exec):
        fake_exec.exec.return_value = ("", 0)
        backend.upload_files([("/tmp/f.bin", b"\x00\xff")])
        cmd = fake_exec.exec.call_args[0][0]
        assert "python3 -c" in cmd
        # The base64 of \x00\xff is "AP8="
        assert "AP8=" in cmd


# --- download_files -----------------------------------------------------------


class TestDownloadFiles:
    def test_download_success(self, backend, fake_exec):
        content = base64.b64encode(b"hello world").decode()
        fake_exec.exec.return_value = (content, 0)
        results = backend.download_files(["/tmp/test.txt"])
        assert len(results) == 1
        assert results[0].content == b"hello world"
        assert results[0].error is None

    def test_download_failure(self, backend, fake_exec):
        fake_exec.exec.return_value = ("not found", 1)
        results = backend.download_files(["/tmp/missing.txt"])
        assert results[0].content is None
        assert results[0].error is not None

    def test_download_bad_base64(self, backend, fake_exec):
        fake_exec.exec.return_value = ("!!!not-base64!!!", 0)
        results = backend.download_files(["/tmp/f.txt"])
        assert results[0].content is None
        assert "b64 decode failed" in results[0].error


# --- id property --------------------------------------------------------------


class TestId:
    def test_id_returns_hostname(self, backend, fake_exec):
        fake_exec.exec.return_value = ("abc-123\n", 0)
        assert backend.id == "abc-123"

    def test_id_cached(self, backend, fake_exec):
        fake_exec.exec.return_value = ("host\n", 0)
        _ = backend.id
        _ = backend.id
        assert fake_exec.exec.call_count == 1  # only called once


# --- execute_log --------------------------------------------------------------


def test_execute_log_bounded(backend, fake_exec):
    fake_exec.exec.return_value = ("", 0)
    for i in range(3000):
        backend.execute(f"cmd{i}")
    assert len(backend.execute_log) == 2048  # maxlen
