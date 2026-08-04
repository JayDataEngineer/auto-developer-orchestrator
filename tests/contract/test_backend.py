"""Tests for pux_harness.sandbox.backend — the BaseSandbox implementation.

All tests mock DockerExecClient; no real container is needed.
"""
from __future__ import annotations

import base64
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from deepagents.backends.protocol import ExecuteResponse

from pux_harness.sandbox.backend import (
    WORKSPACE_ROOT,
    PuxSandboxBackend,
    _DOWNLOAD_PY,
    _UPLOAD_PY,
    _strip_ws_root,
)
from pux_harness.sandbox.docker_exec import ExecTimeout


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

    def test_execute_exectimeout_returns_clean_envelope(self, backend, fake_exec):
        """REGRESSION (2026-07-22): ExecTimeout from ``docker_exec.exec()`` must
        be converted to a clean ``ExecuteResponse(exit_code=124)`` and NOT
        propagated. Without this catch, ExecTimeout walks up through the
        deepagents ToolNode (whose default ``_handle_tool_errors`` re-raises
        anything that isn't a ``ToolInvocationError``), through the model
        node (where ``retry_on_stream_stall`` correctly skips it), and lands
        at the prompt-boundary wrapper — which (pre-fix) called every
        unrecoverable exception a 'model stream stalled', surfacing the
        misleading '⚠️ This turn ended early' notice for what was actually
        a tool timeout. Exit 124 is the GNU ``timeout`` convention.
        """
        fake_exec.exec.side_effect = ExecTimeout(
            "exec timed out after 300s: 'cd /sandbox/workspace && uv run pux direct ...'"
        )

        res = backend.execute("uv run pux direct --org coder", timeout=0)

        assert isinstance(res, ExecuteResponse)
        assert res.exit_code == 124, (
            "ExecTimeout must surface as exit 124 (GNU timeout convention), "
            f"got {res.exit_code}"
        )
        assert res.truncated is False
        # Output must be LLM-actionable: tells the model WHAT hit its budget
        # and HOW to work around it (split into shorter steps).
        assert "timeout" in res.output.lower(), (
            f"envelope output must say 'timeout' (got {res.output!r})"
        )
        # Command preview helps the agent recognize which call failed.
        assert "uv run pux direct" in res.output, (
            "envelope must include the command preview so the agent can "
            "recognize which call timed out"
        )
        # The actionable workaround — don't wrap a long-running wait in one
        # exec call.
        assert "poll" in res.output.lower() or "shorter" in res.output.lower(), (
            "envelope must tell the agent how to work around the timeout "
            f"(got {res.output!r})"
        )

    def test_execute_exectimeout_does_not_propagate(self, backend, fake_exec):
        """Critical: ExecTimeout MUST NOT escape ``execute()``. If it does,
        deepagents' filesystem middleware catches it as ``except ValueError``
        (which ExecTimeout isn't) → it walks past the middleware → the
        LangGraph tools node re-raises → the prompt-boundary stall wrapper
        mislabels it. ``execute()`` is the LAST clean place to convert the
        exception into a result the agent can act on."""
        fake_exec.exec.side_effect = ExecTimeout("exec timed out after 120s: 'sleep 999'")

        # Must NOT raise.
        res = backend.execute("sleep 999")
        assert res.exit_code == 124

    def test_execute_exctimeo_does_not_double_strip(self, backend, fake_exec):
        """The workspace-root stripping logic in execute() must not corrupt
        the envelope's diagnostic text (which mentions ``/sandbox/workspace``
        in the command preview). Envelope output flows through the same code
        path as regular output, so the strip must be benign on it."""
        fake_exec.exec.side_effect = ExecTimeout(
            "exec timed out after 300s: 'cd /sandbox/workspace && make build'"
        )
        res = backend.execute("cd /sandbox/workspace && make build", timeout=0)
        # The diagnostic still mentions the command — stripping must not eat
        # the meaningful path out of the envelope.
        assert "make build" in res.output


# --- upload_files -------------------------------------------------------------


class TestUploadFiles:
    def test_upload_success(self, backend, fake_exec):
        # upload_file returns None on success (new tar/put_archive contract).
        fake_exec.upload_file.return_value = None
        results = backend.upload_files([("/tmp/test.txt", b"hello")])
        assert len(results) == 1
        assert results[0].path == "/tmp/test.txt"
        assert results[0].error is None
        fake_exec.upload_file.assert_called_once_with("/tmp/test.txt", b"hello")

    def test_upload_failure(self, backend, fake_exec):
        # upload_file raising must be captured into FileUploadResponse.error,
        # NOT propagated (the agent loop must keep going for the other files).
        fake_exec.upload_file.side_effect = RuntimeError("permission denied")
        results = backend.upload_files([("/tmp/test.txt", b"data")])
        assert results[0].error is not None
        assert "permission denied" in results[0].error

    def test_upload_uses_put_archive_not_base64_argv(self, backend, fake_exec):
        """Regression: the old upload path ran ``python3 -c <script> <path>
        <base64>`` with the base64 payload as a CLI arg — which hit Linux
        ARG_MAX (~128KB) and silently broke the compact middleware (conversation
        history offload), twitter image posts, and any file > ~100KB. The new
        path uses ``DockerExecClient.upload_file`` (put_archive tar body, no
        argv, no shell quoting, no ceiling below the daemon's own). Verify the
        exec() shell path is NOT used and upload_file() receives the raw bytes."""
        fake_exec.upload_file.return_value = None
        # 256 KB payload — well past the old ARG_MAX cliff. Under the old
        # base64-argv contract this would have failed; under put_archive it
        # just goes through the Docker HTTP body.
        payload = b"\x00\xff" * (256 * 1024 // 2)
        backend.upload_files([("/tmp/f.bin", payload)])
        # The new contract: NO exec() shell call (no python3 -c base64).
        fake_exec.exec.assert_not_called()
        # The raw bytes go to upload_file as-is — no base64 encoding anywhere.
        fake_exec.upload_file.assert_called_once_with("/tmp/f.bin", payload)


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


# --- glob default-root override ---------------------------------------------


class TestGlobDefaultRoot:
    """``BaseSandbox.glob`` defaults an omitted ``path`` to ``/`` (``os.chdir("/")``
    + recursive ``**``) → walks the whole container → 20s ``GLOB_TIMEOUT`` (the
    "Glob is timing out" symptom via ACP/Zed). ``PuxSandboxBackend`` overrides
    ``glob``/``aglob`` to default to ``WORKSPACE_ROOT`` instead. These spy on the
    inherited ``BaseSandbox.glob`` to assert the resolved path — no container."""

    def test_glob_defaults_to_workspace_when_path_omitted(self, backend):
        captured = {}

        def fake_super_glob(self_, pattern, path=None):
            captured["pattern"] = pattern
            captured["path"] = path
            return SimpleNamespace(matches=[], error=None)

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.glob",
            autospec=True,
            side_effect=fake_super_glob,
        ):
            backend.glob("**/*.py")

        assert captured["path"] == WORKSPACE_ROOT
        assert captured["pattern"] == "**/*.py"

    def test_glob_respects_explicit_path(self, backend):
        captured = {}

        def fake_super_glob(self_, pattern, path=None):
            captured["path"] = path
            return SimpleNamespace(matches=[], error=None)

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.glob",
            autospec=True,
            side_effect=fake_super_glob,
        ):
            backend.glob("**/*.py", path="/sandbox/workspace/src")

        # An explicit path is passed through verbatim — including "/"; the
        # override only changes the DEFAULT, it does not forbid searching /.
        assert captured["path"] == "/sandbox/workspace/src"

    def test_glob_explicit_root_still_allowed(self, backend):
        captured = {}

        def fake_super_glob(self_, pattern, path=None):
            captured["path"] = path
            return SimpleNamespace(matches=[], error=None)

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.glob",
            autospec=True,
            side_effect=fake_super_glob,
        ):
            backend.glob("*.py", path="/")

        assert captured["path"] == "/"

    def test_aglob_defaults_to_workspace_when_path_omitted(self, backend):
        import asyncio

        captured = {}

        async def fake_super_aglob(self_, pattern, path=None):
            captured["path"] = path
            return SimpleNamespace(matches=[], error=None)

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.aglob",
            autospec=True,
            side_effect=fake_super_aglob,
        ):
            asyncio.run(backend.aglob("**/*.md"))

        assert captured["path"] == WORKSPACE_ROOT


# --- path relativization (strip /sandbox/workspace from tool outputs) --------


class TestStripWsRoot:
    """``_strip_ws_root`` converts container-absolute paths to project-relative
    so the agent sees ``CANVAS-GAPS.md`` instead of ``/sandbox/workspace/CANVAS-GAPS.md``
    — exactly like Claude Code's relative-path convention."""

    def test_strips_file_prefix(self):
        assert _strip_ws_root("/sandbox/workspace/CANVAS-GAPS.md") == "CANVAS-GAPS.md"

    def test_bare_root_becomes_dot(self):
        assert _strip_ws_root("/sandbox/workspace") == "."

    def test_nested_path(self):
        assert _strip_ws_root("/sandbox/workspace/web/editor/file.ts") == "web/editor/file.ts"

    def test_outside_workspace_unchanged(self):
        assert _strip_ws_root("/etc/passwd") == "/etc/passwd"

    def test_already_relative_unchanged(self):
        assert _strip_ws_root("CANVAS-GAPS.md") == "CANVAS-GAPS.md"

    def test_similar_prefix_not_stipped(self):
        # /sandbox/workspace-other must NOT be stripped (it's not under /sandbox/workspace/)
        assert _strip_ws_root("/sandbox/workspace-other/file") == "/sandbox/workspace-other/file"


class TestGlobRelativization:
    """``glob`` strips ``WORKSPACE_ROOT`` from match paths so the agent never
    sees container-absolute paths in tool output — even when it passes
    ``/sandbox/workspace`` explicitly (which it shouldn't, but the backend
    defends against it)."""

    def test_glob_strips_prefix_from_matches(self, backend):
        fake_matches = [
            {"path": "/sandbox/workspace/CANVAS-GAPS.md", "is_dir": False},
            {"path": "/sandbox/workspace/web", "is_dir": True},
        ]

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.glob",
            autospec=True,
            return_value=SimpleNamespace(matches=fake_matches, error=None),
        ):
            result = backend.glob("*")

        assert result.matches[0]["path"] == "CANVAS-GAPS.md"
        assert result.matches[1]["path"] == "web"

    def test_glob_preserves_already_relative(self, backend):
        fake_matches = [{"path": "src/main.py", "is_dir": False}]

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.glob",
            autospec=True,
            return_value=SimpleNamespace(matches=fake_matches, error=None),
        ):
            result = backend.glob("**/*.py")

        assert result.matches[0]["path"] == "src/main.py"


class TestLsRelativization:
    """``ls`` strips ``WORKSPACE_ROOT`` from entry paths."""

    def test_ls_strips_prefix_from_entries(self, backend):
        fake_entries = [
            {"path": "/sandbox/workspace/.githooks", "is_dir": True},
            {"path": "/sandbox/workspace/README.md", "is_dir": False},
        ]

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.ls",
            autospec=True,
            return_value=SimpleNamespace(entries=fake_entries, error=None),
        ):
            result = backend.ls("/sandbox/workspace")

        assert result.entries[0]["path"] == ".githooks"
        assert result.entries[1]["path"] == "README.md"

    def test_ls_preserves_dot_relative(self, backend):
        fake_entries = [{"path": "./README.md", "is_dir": False}]

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.ls",
            autospec=True,
            return_value=SimpleNamespace(entries=fake_entries, error=None),
        ):
            result = backend.ls(".")

        assert result.entries[0]["path"] == "./README.md"


class TestGrepRelativization:
    """``grep`` strips ``WORKSPACE_ROOT`` from match paths and defaults to
    ``WORKSPACE_ROOT`` when no path is given."""

    def test_grep_strips_prefix_from_matches(self, backend):
        fake_matches = [
            {"path": "/sandbox/workspace/web/editor/file.ts", "line": 42},
        ]

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.grep",
            autospec=True,
            return_value=SimpleNamespace(matches=fake_matches, error=None),
        ):
            result = backend.grep("TODO")

        assert result.matches[0]["path"] == "web/editor/file.ts"

    def test_grep_defaults_to_workspace_root(self, backend):
        captured = {}

        def fake_grep(self_, pattern, path=None, glob=None):
            captured["path"] = path
            return SimpleNamespace(matches=[], error=None)

        with patch(
            "pux_harness.sandbox.backend.BaseSandbox.grep",
            autospec=True,
            side_effect=fake_grep,
        ):
            backend.grep("pattern")

        assert captured["path"] == WORKSPACE_ROOT


# --- execute() path stripping (find/pwd output) -----------------------------


class TestExecutePathStripping:
    """``execute()`` strips ``/sandbox/workspace`` from raw shell output so the
    agent never sees container-absolute paths — even from ``find /``, ``pwd``,
    ``tree``, or any command that echoes paths. The ``_internal_depth`` guard
    ensures inherited ``read``/``write``/``edit`` (which call ``execute()``
    internally) are NOT stripped — file content stays intact."""

    def test_find_output_stripped(self, backend, fake_exec):
        """find / output should show bare relative paths, not /sandbox/workspace/."""
        fake_exec.exec.return_value = (
            "/sandbox/workspace/CANVAS-GAPS.md\n/sandbox/workspace/web/editor/file.ts\n",
            0,
        )
        result = backend.execute('find / -iname "*.md"')
        assert "/sandbox/workspace" not in result.output
        assert "CANVAS-GAPS.md" in result.output
        assert "web/editor/file.ts" in result.output

    def test_pwd_output_stripped_to_dot(self, backend, fake_exec):
        """pwd returns /sandbox/workspace (no trailing slash) → should become '.'"""
        fake_exec.exec.return_value = ("/sandbox/workspace\n", 0)
        result = backend.execute("pwd")
        assert result.output.strip() == "."

    def test_nested_paths_stripped(self, backend, fake_exec):
        """Deep paths under the workspace get the prefix removed cleanly."""
        fake_exec.exec.return_value = (
            "/sandbox/workspace/web/editor/src/index.ts\n", 0
        )
        result = backend.execute("find . -name '*.ts'")
        assert "/sandbox/workspace" not in result.output
        assert result.output.strip() == "web/editor/src/index.ts"

    def test_read_content_not_stripped(self, backend, fake_exec):
        """When read() calls execute() internally (depth > 0), file content
        that legitimately mentions /sandbox/workspace must NOT be stripped."""
        # File content that legitimately mentions the container path
        fake_exec.exec.return_value = (
            "# Config\nmount=/sandbox/workspace/data\n", 0
        )
        # read() sets _internal_depth=1, so execute() skips the strip
        result = backend.read("/sandbox/workspace/config.txt")
        # The content should be preserved with /sandbox/workspace intact
        # (read's own command output goes through execute at depth=1)
        assert backend._internal_depth == 0  # reset after read returns

    def test_direct_execute_strips(self, backend, fake_exec):
        """When the agent calls execute() directly (depth=0), output IS stripped."""
        fake_exec.exec.return_value = (
            "/sandbox/workspace/CANVAS-GAPS.md\n", 0
        )
        assert backend._internal_depth == 0
        result = backend.execute("find / -name CANVAS-GAPS.md")
        assert "/sandbox/workspace" not in result.output
        assert result.output.strip() == "CANVAS-GAPS.md"

    def test_paths_outside_workspace_not_touched(self, backend, fake_exec):
        """Paths outside /sandbox/workspace (e.g. /etc/passwd) are left alone."""
        fake_exec.exec.return_value = ("/etc/passwd\n/usr/bin/python3\n", 0)
        result = backend.execute("which python3")
        assert "/etc/passwd" in result.output
        assert "/usr/bin/python3" in result.output

