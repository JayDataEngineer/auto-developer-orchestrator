"""Tests for pux_harness.acp — the ACP stdio server entry point.

Tests _make_factory and main's org validation logic.
"""
from __future__ import annotations

from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from pux_harness.acp import _capture_editor_cwd, _make_factory


# --- _capture_editor_cwd ------------------------------------------------------


def test_capture_editor_cwd_sets_project_path(tmp_path, monkeypatch):
    """A valid editor cwd is exported as ``PUX_PROJECT_PATH`` so the lazily-
    booted sandbox container mounts the editor's project (Claude-Code-style
    "spawn in folder"), not just the harness repo."""
    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    _capture_editor_cwd(str(tmp_path))
    import os

    assert os.environ["PUX_PROJECT_PATH"] == str(tmp_path)


def test_capture_editor_cwd_does_not_override_existing(tmp_path, monkeypatch):
    """An explicit ``PUX_PROJECT_PATH`` shell export wins (``setdefault``) —
    the operator's pin beats the editor's cwd."""
    monkeypatch.setenv("PUX_PROJECT_PATH", "/operator/pinned/path")
    _capture_editor_cwd(str(tmp_path))
    import os

    assert os.environ["PUX_PROJECT_PATH"] == "/operator/pinned/path"


def test_capture_editor_cwd_ignores_nonexistent(tmp_path, monkeypatch):
    """A cwd that isn't a real directory is silently dropped — falls through to
    the harness ``project_root()`` default. No env mutation."""
    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    _capture_editor_cwd(str(tmp_path / "does-not-exist"))
    import os

    assert "PUX_PROJECT_PATH" not in os.environ


def test_capture_editor_cwd_ignores_none(monkeypatch):
    """``None`` cwd (client sent nothing) is a no-op."""
    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    _capture_editor_cwd(None)
    import os

    assert "PUX_PROJECT_PATH" not in os.environ


# --- _make_factory ------------------------------------------------------------


def test_make_factory_returns_callable():
    factory = _make_factory("general", saver=object())
    assert callable(factory)


def test_make_factory_ignores_context():
    """The factory ignores context.cwd — org is fixed at server startup."""
    factory = _make_factory("invest", saver=object())
    fake_context = SimpleNamespace(cwd="/some/editor/dir")

    with patch("pux_harness.acp.build_graph") as mock_build:
        mock_build.return_value = MagicMock(name="graph")
        result = factory(fake_context)

        mock_build.assert_called_once()
        call_args = mock_build.call_args
        assert call_args.args[0] == "invest"  # org
        assert result is mock_build.return_value


def test_make_factory_uses_persistent_saver():
    """The factory threads the shared persistent saver into build_graph.

    ACP no longer mints an ephemeral MemorySaver — it reuses the
    ``AsyncSqliteSaver`` from ``open_thread_store`` so session checkpoints
    persist across ``pux acp`` restarts."""
    sentinel_saver = object()
    factory = _make_factory("general", saver=sentinel_saver)

    with patch("pux_harness.acp.build_graph") as mock_build:
        mock_build.return_value = MagicMock()
        factory(SimpleNamespace(cwd="/tmp"))

        checkpointer = mock_build.call_args.kwargs["checkpointer"]
        assert checkpointer is sentinel_saver


# --- main validation ----------------------------------------------------------


def test_main_unknown_org_exits(monkeypatch):
    """Unknown org should raise SystemExit(2)."""
    monkeypatch.setattr("sys.argv", ["pux", "acp", "--org", "nonexistent"])
    monkeypatch.setattr("pux_harness.acp.discover_orgs", lambda: {"general", "invest"})

    with pytest.raises(SystemExit, match="2"):
        from pux_harness.acp import main
        main()
