"""Phase 8 — event capture pipeline.

Proves the ``EventStore`` (SQLite), ``EventCaptureMiddleware`` (sync + async),
and ``event_recent`` / ``event_query`` tools work correctly against a tmp-path
database (no real ``.pux/events.sqlite``, no Docker, no model tokens).
"""
from __future__ import annotations

import asyncio
import time
from types import SimpleNamespace
from typing import Any

import pytest
from langchain_core.messages import ToolMessage

from pux_harness.context.event_middleware import EventCaptureMiddleware
from pux_harness.context.event_tools import build_event_tools
from pux_harness.context.events import EventStore, P1, P2, P3, P4


def _req(name: str = "execute", tcid: str = "call_1", args: dict | None = None) -> SimpleNamespace:
    return SimpleNamespace(
        tool_call={"name": name, "args": args or {}, "id": tcid},
        state={},
    )


def _tm(content: Any, tcid: str = "call_1", name: str = "execute", status: str = "") -> ToolMessage:
    kwargs: dict[str, Any] = {"content": content, "tool_call_id": tcid, "name": name}
    if status:
        kwargs["status"] = status
    return ToolMessage(**kwargs)


# --- EventStore core ----------------------------------------------------------


def test_capture_and_recent(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("task_started", {"task": "build login page"})
    store.capture("file_modified", {"path": "/src/auth.py"})
    store.capture("error", {"tool": "execute", "error": "timeout"}, priority=P1)
    store.flush()

    events = store.recent(limit=5)
    assert len(events) == 3
    # Newest first
    assert events[0].type == "error"
    assert events[0].priority == P1
    assert events[1].type == "file_modified"
    assert events[2].type == "task_started"


def test_recent_filter_by_type(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"tool": "grep"})
    store.capture("error", {"tool": "grep"})
    store.capture("tool_call", {"tool": "execute"})
    store.flush()

    errors = store.recent(event_type="error")
    assert len(errors) == 1
    assert errors[0].type == "error"


def test_recent_filter_by_priority(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("task_started", priority=P1)
    store.capture("tool_call", priority=P2)
    store.capture("compaction", priority=P4)
    store.flush()

    # Only P1-P2 (min_priority=2)
    events = store.recent(min_priority=P2)
    assert len(events) == 2
    types = {e.type for e in events}
    assert "compaction" not in types


def test_recent_filter_by_thread(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", thread_id="t1")
    store.capture("tool_call", thread_id="t2")
    store.capture("tool_call", thread_id="t1")
    store.flush()

    t1_events = store.recent(thread_id="t1")
    assert len(t1_events) == 2
    assert all(e.thread_id == "t1" for e in t1_events)


def test_event_priority_lookup(tmp_path):
    store = EventStore(tmp_path / "events.db")
    # Known types get default priorities
    rid = store.capture("task_started")
    store.flush()
    row = store.recent(event_type="task_started")[0]
    assert row.priority == P1  # task_started is P1

    # Unknown type defaults to P3
    store.capture("custom_event")
    store.flush()
    row = store.recent(event_type="custom_event")[0]
    assert row.priority == P3


# --- FTS5 search --------------------------------------------------------------


def test_query_fts5_ranked(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"output_preview": "authentication failed on login endpoint"})
    store.capture("tool_call", {"output_preview": "unrelated grep output"})
    store.capture("blocker", {"description": "blocked on auth token expiry"})
    store.flush()

    hits = store.query("auth")
    assert len(hits) >= 2
    # Most relevant should come first (BM25 ranking)
    assert hits[0].type in ("tool_call", "blocker")


def test_query_empty_result(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"output_preview": "nothing relevant"})
    store.flush()

    hits = store.query("zzznonexistent")
    assert len(hits) == 0


def test_query_with_thread_filter(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"output_preview": "auth error in thread one"}, thread_id="t1")
    store.capture("tool_call", {"output_preview": "auth error in thread two"}, thread_id="t2")
    store.flush()

    hits = store.query("auth error", thread_id="t1")
    assert len(hits) == 1
    assert hits[0].thread_id == "t1"


# --- EventCaptureMiddleware ---------------------------------------------------


def test_middleware_captures_tool_call(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = EventCaptureMiddleware(store)

    result = m.wrap_tool_call(
        _req(name="grep", args={"pattern": "TODO"}),
        lambda r: _tm("found 3 matches", name="grep"),
    )
    assert isinstance(result, ToolMessage)

    events = store.recent(event_type="tool_call")
    assert len(events) == 1
    assert events[0].data["tool"] == "grep"
    assert events[0].data["success"] is True
    assert "elapsed_s" in events[0].data


def test_middleware_captures_error(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = EventCaptureMiddleware(store)

    with pytest.raises(ValueError, match="boom"):
        m.wrap_tool_call(
            _req(name="execute"),
            lambda r: (_ for _ in ()).throw(ValueError("boom")),
        )

    errors = store.recent(event_type="error")
    assert len(errors) == 1
    assert errors[0].data["error"] == "boom"
    assert errors[0].data["tool"] == "execute"
    assert errors[0].priority == P1


def test_middleware_captures_error_result(tmp_path):
    """ToolMessage with status='error' should be captured as success=False."""
    store = EventStore(tmp_path / "events.db")
    m = EventCaptureMiddleware(store)

    m.wrap_tool_call(
        _req(name="execute"),
        lambda r: _tm("command failed", status="error"),
    )

    events = store.recent(event_type="tool_call")
    assert len(events) == 1
    assert events[0].data["success"] is False


def test_middleware_disabled(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = EventCaptureMiddleware(store, enabled=False)

    m.wrap_tool_call(
        _req(name="grep"),
        lambda r: _tm("result"),
    )

    assert store.count() == 0  # nothing captured


def test_middleware_async_captures(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = EventCaptureMiddleware(store)

    async def handler(_r):  # type: ignore[no-untyped-def]
        return _tm("async result", name="execute")

    result = asyncio.run(m.awrap_tool_call(_req(), handler))
    assert isinstance(result, ToolMessage)

    events = store.recent(event_type="tool_call")
    assert len(events) == 1
    assert events[0].data["tool"] == "execute"


def test_middleware_async_captures_error(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = EventCaptureMiddleware(store)

    async def handler(_r):  # type: ignore[no-untyped-def]
        raise RuntimeError("async boom")

    with pytest.raises(RuntimeError, match="async boom"):
        asyncio.run(m.awrap_tool_call(_req(), handler))

    errors = store.recent(event_type="error")
    assert len(errors) == 1
    assert errors[0].data["error"] == "async boom"


def test_middleware_skips_detail_for_retrieval_tools(tmp_path):
    """event_recent/event_query/ctx_recall/ctx_search should not log output_preview."""
    store = EventStore(tmp_path / "events.db")
    m = EventCaptureMiddleware(store)

    m.wrap_tool_call(
        _req(name="event_recent"),
        lambda r: _tm("lots of events..."),
    )

    events = store.recent(event_type="tool_call")
    assert len(events) == 1
    assert events[0].data["output_preview"] == ""  # skipped


# --- Agent tools --------------------------------------------------------------


def test_event_recent_tool(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("task_started", {"task": "fix bug"})
    store.capture("error", {"error": "timeout"})
    store.flush()

    recent_tool, _ = build_event_tools(store)
    out = recent_tool.invoke({"event_type": "", "limit": 10})
    assert "task_started" in out
    assert "error" in out


def test_event_recent_tool_filtered(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("task_started", {"task": "fix bug"})
    store.capture("error", {"error": "timeout"})
    store.flush()

    recent_tool, _ = build_event_tools(store)
    out = recent_tool.invoke({"event_type": "error", "limit": 10})
    assert "error" in out
    assert "task_started" not in out


def test_event_query_tool(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"output_preview": "authentication failed"})
    store.capture("tool_call", {"output_preview": "grep completed"})
    store.flush()

    _, query_tool = build_event_tools(store)
    out = query_tool.invoke({"query": "authentication"})
    assert "1 hit" in out
    assert "tool_call" in out


def test_event_query_tool_no_match(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"output_preview": "nothing relevant"})
    store.flush()

    _, query_tool = build_event_tools(store)
    out = query_tool.invoke({"query": "zzznonexistent"})
    assert "no events matched" in out


def test_event_recent_empty(tmp_path):
    store = EventStore(tmp_path / "events.db")
    recent_tool, _ = build_event_tools(store)
    out = recent_tool.invoke({"event_type": "", "limit": 10})
    assert "no events recorded yet" in out
