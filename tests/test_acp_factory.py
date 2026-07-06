"""Tests for pux_harness.acp — the ACP stdio server entry point.

Tests _make_factory and main's org validation logic.
"""
from __future__ import annotations

from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from pux_harness.acp import _make_factory


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
