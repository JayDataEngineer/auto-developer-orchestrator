"""Proof that GLM-5.2's string-shaped ``write_todos`` payloads no longer crash
the ACP turn — the deterministic "stall" at the planning boundary.

Root cause (captured verbatim in ``.pux/stall.log``)::

    File ".../deepagents_acp/server.py", line 433, in _handle_todo_update
      content = todo.get("content", "")
  AttributeError: 'str' object has no attribute 'get'

GLM-5.2 emits ``write_todos`` with ``todos: ["write design doc", ...]`` — a list
of PLAIN STRINGS — but deepagents_acp's ``_handle_todo_update`` assumes each
todo is a dict. The instant the model starts planning, the handler calls
``.get()`` on a string → AttributeError → the turn dies mid-stream (looked
identical to a network stall). ``pux_harness.acp._normalize_todos`` coerces
every item to the dict shape before the base handler sees it. These tests prove
the crash is impossible and the override delegates only dicts.

Sync style (``asyncio.run``) — mirrors the rest of the harness suite (no
pytest-asyncio dep).
"""
from __future__ import annotations

import asyncio

import pytest


def test_string_todos_become_dicts():
    """The exact failing payload shape (list of plain strings)."""
    from pux_harness.acp import _normalize_todos

    out = _normalize_todos(["write design doc", "review code"])
    assert out == [
        {"content": "write design doc", "status": "pending"},
        {"content": "review code", "status": "pending"},
    ]


def test_dict_todos_pass_through_unchanged():
    from pux_harness.acp import _normalize_todos

    out = _normalize_todos([{"content": "x", "status": "completed"}])
    assert out == [{"content": "x", "status": "completed"}]


def test_dict_missing_content_is_filled_with_empty():
    from pux_harness.acp import _normalize_todos

    out = _normalize_todos([{"status": "in_progress"}])
    assert out == [{"status": "in_progress", "content": ""}]


def test_non_list_top_level_is_salvaged():
    from pux_harness.acp import _normalize_todos

    out = _normalize_todos("just a string")
    assert out == [{"content": "just a string", "status": "pending"}]


def test_none_becomes_empty_list():
    from pux_harness.acp import _normalize_todos

    assert _normalize_todos(None) == []


def test_mixed_shapes_all_become_dicts():
    from pux_harness.acp import _normalize_todos

    out = _normalize_todos(["a str", {"content": "a dict"}, {"status": "completed"}, 42])
    assert all(isinstance(t, dict) for t in out)
    assert out[0]["content"] == "a str"
    assert out[3]["content"] == "42"  # scalar stringified, not crashed


def test_regression_base_handler_get_no_longer_crashes():
    """Reproduce the stall.log crash on the RAW payload, prove NORMALIZED is
    safe under the base handler's exact access pattern (``.get('content')`` +
    ``.get('status')`` on every item)."""
    from pux_harness.acp import _normalize_todos

    raw = ["write design doc", "review code"]
    # RAW — the exact crash from .pux/stall.log:
    with pytest.raises(AttributeError):
        for t in raw:
            t.get("content", "")  # 'str' has no .get

    # NORMALIZED — the base handler's access pattern is now safe:
    for t in _normalize_todos(raw):
        assert isinstance(t.get("content", ""), str)
        assert t.get("status", "pending") in ("pending", "in_progress", "completed")


def test_override_delegates_only_dicts(monkeypatch):
    """The override on ``_RegisteringAgentServerACP`` must hand ``super()`` a
    list of dicts only. Patches the real base handler with a recorder that
    REPRODUCES the crashing ``.get()`` and asserts it receives no non-dict."""
    from deepagents_acp.server import AgentServerACP

    from pux_harness.acp import _RegisteringAgentServerACP

    seen: dict = {}

    async def _recording_base(self, session_id, todos, *, log_plan=True):
        seen["todos"] = list(todos)
        seen["log_plan"] = log_plan
        for t in todos:  # the base behavior that crashed on strings
            t.get("content", "")
            t.get("status", "pending")

    monkeypatch.setattr(AgentServerACP, "_handle_todo_update", _recording_base)
    # Bypass the heavy __init__ — the override only needs `self` for super().
    srv = _RegisteringAgentServerACP.__new__(_RegisteringAgentServerACP)
    asyncio.run(
        srv._handle_todo_update(
            "sess-a0af08", ["write design doc", "review code"], log_plan=False,
        )
    )
    assert all(isinstance(t, dict) for t in seen["todos"])
    assert seen["log_plan"] is False
    assert seen["todos"][0]["content"] == "write design doc"
