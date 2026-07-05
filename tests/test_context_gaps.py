"""Phase 10-12 — routing enforcement, snapshot builder, session guide.

Tests all three new modules against tmp-path databases (no Docker, no model).
"""
from __future__ import annotations

import asyncio
import re
from types import SimpleNamespace
from typing import Any

import pytest
from langchain_core.messages import SystemMessage, ToolMessage

from pux_harness.context.events import EventStore, P1, P2, P3


# -- Helpers -------------------------------------------------------------------


def _req(
    name: str = "execute",
    tcid: str = "call_1",
    args: dict | None = None,
    state: dict | None = None,
) -> SimpleNamespace:
    return SimpleNamespace(
        tool_call={"name": name, "args": args or {}, "id": tcid},
        state=state or {},
    )


def _tm(content: Any, tcid: str = "call_1", name: str = "execute") -> ToolMessage:
    return ToolMessage(content=content, tool_call_id=tcid, name=name)


# =============================================================================
# Phase 10: Routing Middleware
# =============================================================================


def test_routing_allows_clean_command(tmp_path):
    from pux_harness.context.sandbox_routing import RoutingMiddleware

    m = RoutingMiddleware()
    result = m.wrap_tool_call(
        _req(name="execute", args={"command": "python3 -c 'print(1)'"}),
        lambda r: _tm("1"),
    )
    assert isinstance(result, ToolMessage)
    assert result.content == "1"


def test_routing_denies_curl(tmp_path):
    from pux_harness.context.sandbox_routing import RoutingMiddleware, _DENY_MSG

    m = RoutingMiddleware()
    result = m.wrap_tool_call(
        _req(name="execute", args={"command": "curl https://example.com"}),
        lambda r: _tm("should not reach"),
    )
    assert isinstance(result, ToolMessage)
    assert _DENY_MSG in result.content


def test_routing_denies_wget(tmp_path):
    from pux_harness.context.sandbox_routing import RoutingMiddleware

    m = RoutingMiddleware()
    result = m.wrap_tool_call(
        _req(name="execute", args={"command": "wget https://example.com/file.tar.gz"}),
        lambda r: _tm("should not reach"),
    )
    assert "blocked" in result.content.lower() or "denied" in result.content.lower()


def test_routing_allows_curl_silent(tmp_path):
    """curl -s (silent) + file output should pass through."""
    from pux_harness.context.sandbox_routing import RoutingMiddleware

    m = RoutingMiddleware()
    result = m.wrap_tool_call(
        _req(name="execute", args={"command": "curl -s -o /dev/null https://example.com"}),
        lambda r: _tm("ok"),
    )
    assert result.content == "ok"


def test_routing_skips_non_intercepted_tools(tmp_path):
    """Tools not in intercept_tools should pass through untouched."""
    from pux_harness.context.sandbox_routing import RoutingMiddleware

    m = RoutingMiddleware()
    result = m.wrap_tool_call(
        _req(name="grep", args={"pattern": "curl"}),
        lambda r: _tm("found"),
    )
    assert result.content == "found"


def test_routing_disabled(tmp_path):
    from pux_harness.context.sandbox_routing import RoutingMiddleware

    m = RoutingMiddleware(enabled=False)
    result = m.wrap_tool_call(
        _req(name="execute", args={"command": "curl https://example.com"}),
        lambda r: _tm("passed"),
    )
    assert result.content == "passed"


def test_routing_logs_denied_to_event_store(tmp_path):
    from pux_harness.context.sandbox_routing import RoutingMiddleware

    store = EventStore(tmp_path / "events.db")
    m = RoutingMiddleware()

    # Override shared_event_store for this test.
    import pux_harness.context.sandbox_routing as mod
    original = mod.shared_event_store
    mod.shared_event_store = lambda: store  # type: ignore[assignment]
    try:
        m.wrap_tool_call(
            _req(name="execute", args={"command": "curl https://evil.com"}, state={"configurable": {"thread_id": "t1"}}),
            lambda r: _tm("nope"),
        )
        store.flush()
        events = store.recent(thread_id="t1")
        assert any(e.type == "routing_denied" for e in events)
    finally:
        mod.shared_event_store = original  # type: ignore[assignment]


def test_routing_async(tmp_path):
    from pux_harness.context.sandbox_routing import RoutingMiddleware

    m = RoutingMiddleware()

    async def handler(_r):
        return _tm("async ok")

    result = asyncio.run(m.awrap_tool_call(
        _req(name="execute", args={"command": "python3 test.py"}),
        handler,
    ))
    assert result.content == "async ok"


def test_routing_async_denies(tmp_path):
    from pux_harness.context.sandbox_routing import RoutingMiddleware

    m = RoutingMiddleware()

    async def handler(_r):
        return _tm("should not reach")

    result = asyncio.run(m.awrap_tool_call(
        _req(name="execute", args={"command": "wget https://example.com"}),
        handler,
    ))
    assert "blocked" in result.content.lower() or "denied" in result.content.lower()


# =============================================================================
# Phase 11: Snapshot Builder
# =============================================================================


def test_snapshot_empty_events():
    from pux_harness.context.snapshot import build_snapshot

    result = build_snapshot([])
    assert "empty" in result
    assert result.startswith("<session_snapshot")


def test_snapshot_includes_p1_sections():
    from pux_harness.context.snapshot import build_snapshot

    events = [
        SimpleNamespace(type="file_modified", category="file", priority=P1, data={"path": "/src/main.py"}),
        SimpleNamespace(type="task_started", category="task", priority=P1, data={"task": "implement auth"}),
        SimpleNamespace(type="error", category="error", priority=P1, data={"error": "timeout on DB"}),
    ]
    result = build_snapshot(events, thread_id="t1")
    assert "<files" in result
    assert "<tasks" in result
    assert "<errors" in result
    assert "/src/main.py" in result
    assert "implement auth" in result
    assert "timeout" in result


def test_snapshot_includes_p2_sections():
    from pux_harness.context.snapshot import build_snapshot

    events = [
        SimpleNamespace(type="git_operation", category="git", priority=P2, data={"operation": "commit"}),
        SimpleNamespace(type="decision_made", category="decision", priority=P2, data={"decision": "use FastAPI"}),
        SimpleNamespace(type="env_change", category="env", priority=P3, data={"cwd": "/workspace"}),
    ]
    result = build_snapshot(events)
    assert "<git" in result
    assert "<decisions" in result
    assert "<env" in result


def test_snapshot_budget_enforcement():
    """Snapshot should stay under MAX_SNAPSHOT_BYTES."""
    from pux_harness.context.snapshot import build_snapshot, MAX_SNAPSHOT_BYTES

    # Generate enough events to exceed budget.
    events = [
        SimpleNamespace(type="file_modified", category="file", priority=P1, data={"path": f"/src/file_{i}.py"})
        for i in range(50)
    ] + [
        SimpleNamespace(type="task_started", category="task", priority=P1, data={"task": f"task {i} " + "x" * 50})
        for i in range(20)
    ]
    result = build_snapshot(events, thread_id="t1")
    assert len(result.encode("utf-8")) <= MAX_SNAPSHOT_BYTES + 100  # small margin for closing tag


def test_snapshot_summary_line():
    from pux_harness.context.snapshot import build_snapshot

    events = [
        SimpleNamespace(type="file_modified", category="file", priority=P1, data={"path": "/a.py"}),
        SimpleNamespace(type="tool_call", category="data", priority=P3, data={"tool": "grep"}),
    ]
    result = build_snapshot(events, thread_id="t1")
    assert 'events="2"' in result
    assert 'thread="t1"' in result


def test_snapshot_search_tool_in_files():
    from pux_harness.context.snapshot import build_snapshot

    events = [
        SimpleNamespace(type="file_modified", category="file", priority=P1, data={"path": "/src/app.py"}),
    ]
    result = build_snapshot(events, search_tool="ctx_search")
    assert "ctx_search" in result


def test_snapshot_dedupes_files():
    """Same file path appearing twice should be deduped in the snapshot."""
    from pux_harness.context.snapshot import build_snapshot

    events = [
        SimpleNamespace(type="file_modified", category="file", priority=P1, data={"path": "/src/app.py"}),
        SimpleNamespace(type="file_modified", category="file", priority=P1, data={"path": "/src/app.py"}),
    ]
    result = build_snapshot(events)
    assert result.count("/src/app.py") == 1  # deduped


# =============================================================================
# Phase 12: Session Guide Middleware
# =============================================================================


def test_session_guide_builds_and_stores(tmp_path):
    from pux_harness.context.session_guide import SessionGuideMiddleware

    store = EventStore(tmp_path / "events.db")
    store.capture("task_started", {"task": "fix login bug"}, thread_id="t1")
    store.capture("file_modified", {"path": "/src/auth.py"}, thread_id="t1")
    store.flush()

    m = SessionGuideMiddleware(store)

    # Simulate a model call — should build and store snapshot.
    req = _req(state={"configurable": {"thread_id": "t1"}})
    called = [False]

    def handler(r):
        called[0] = True
        return _tm("ok")

    m.wrap_model_call(req, handler)
    assert called[0]

    # Snapshot should be stored.
    resume = store.get_resume("t1")
    assert resume is not None
    assert "session_snapshot" in resume["snapshot"]
    assert "fix login bug" in resume["snapshot"]


def test_session_guide_injects_on_resume(tmp_path):
    from pux_harness.context.session_guide import SessionGuideMiddleware

    store = EventStore(tmp_path / "events.db")
    # Store a snapshot for session t1.
    store.upsert_resume("t1", "<session_snapshot>previous work</session_snapshot>", 5)

    m = SessionGuideMiddleware(store)

    # Simulate a new session t2 that should claim t1's snapshot.
    state = {
        "messages": [SystemMessage(content="You are a coding assistant.")],
        "configurable": {"thread_id": "t2"},
    }
    result = m.before_agent(state, None)
    assert result is not None
    messages = result["messages"]
    assert len(messages) == 1
    content = messages[0].content
    assert "session_knowledge" in content
    assert "previous work" in content


def test_session_guide_no_snapshot_noop(tmp_path):
    from pux_harness.context.session_guide import SessionGuideMiddleware

    store = EventStore(tmp_path / "events.db")
    m = SessionGuideMiddleware(store)

    state = {
        "messages": [SystemMessage(content="System prompt")],
        "configurable": {"thread_id": "fresh-session"},
    }
    result = m.before_agent(state, None)
    assert result is None  # no snapshot to inject


def test_session_guide_disabled(tmp_path):
    from pux_harness.context.session_guide import SessionGuideMiddleware

    store = EventStore(tmp_path / "events.db")
    store.upsert_resume("t1", "<snapshot/>", 1)

    m = SessionGuideMiddleware(store, enabled=False)
    state = {
        "messages": [SystemMessage(content="System")],
        "configurable": {"thread_id": "t2"},
    }
    result = m.before_agent(state, None)
    assert result is None  # disabled, no injection


def test_session_guide_self_exclusion(tmp_path):
    """A session should not claim its own mid-flight snapshot."""
    from pux_harness.context.session_guide import SessionGuideMiddleware

    store = EventStore(tmp_path / "events.db")
    store.upsert_resume("t1", "<snapshot/>", 1)

    m = SessionGuideMiddleware(store)
    state = {
        "messages": [SystemMessage(content="System")],
        "configurable": {"thread_id": "t1"},
    }
    result = m.before_agent(state, None)
    assert result is None  # self-excluded


def test_session_guide_async(tmp_path):
    from pux_harness.context.session_guide import SessionGuideMiddleware

    store = EventStore(tmp_path / "events.db")
    store.upsert_resume("t1", "<snapshot>async test</snapshot>", 1)

    m = SessionGuideMiddleware(store)
    state = {
        "messages": [SystemMessage(content="System")],
        "configurable": {"thread_id": "t2"},
    }
    result = asyncio.run(m.abefore_agent(state, None))
    assert result is not None
    assert "session_knowledge" in result["messages"][0].content
