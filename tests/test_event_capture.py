"""Phase 8/19 — event capture pipeline (unified ``ContextMiddleware``).

Proves the ``EventStore`` (SQLite), the unified ``ContextMiddleware`` (sync +
async — does capture AND offload in one pass), and the ``ctx_search``
retrieval tool work correctly against a tmp-path database (no real
``.pux/events.sqlite``, no Docker, no model tokens).

Phase 19 folded the old separate ``EventCaptureMiddleware`` into
``ContextMiddleware`` and replaced the ``event_recent``/``event_query`` tool
pair with the unified ``ctx_search`` (the resume snapshot now owns the
chronological view; ``ctx_search`` covers query recall over blobs AND events).
The ``event_recent`` tool tests were dropped (the tool is intentionally gone —
its coverage moved to ``test_context_gaps.py`` snapshot tests); the
``event_query`` tests were ported to ``ctx_search``.
"""
from __future__ import annotations

import asyncio
from types import SimpleNamespace
from typing import Any

import pytest
from langchain_core.messages import ToolMessage

from pux_harness.context.events import EventStore, P1, P2, P3, P4
from pux_harness.context.middleware import ContextMiddleware
from pux_harness.context.tools import build_context_tools


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
    store.capture("tool_call", {"seq": 1}, thread_id="t1")
    store.capture("tool_call", {"seq": 2}, thread_id="t2")
    store.capture("tool_call", {"seq": 3}, thread_id="t1")
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


# --- ContextMiddleware (capture path) ----------------------------------------


def test_middleware_captures_tool_call(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store)

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
    m = ContextMiddleware(store)

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
    m = ContextMiddleware(store)

    m.wrap_tool_call(
        _req(name="execute"),
        lambda r: _tm("command failed", status="error"),
    )

    events = store.recent(event_type="tool_call")
    assert len(events) == 1
    assert events[0].data["success"] is False


def test_middleware_disabled(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store, enabled=False)

    m.wrap_tool_call(
        _req(name="grep"),
        lambda r: _tm("result"),
    )

    assert store.count() == 0  # nothing captured


def test_middleware_async_captures(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store)

    async def handler(_r):  # type: ignore[no-untyped-def]
        return _tm("async result", name="execute")

    result = asyncio.run(m.awrap_tool_call(_req(), handler))
    assert isinstance(result, ToolMessage)

    events = store.recent(event_type="tool_call")
    assert len(events) == 1
    assert events[0].data["tool"] == "execute"


def test_middleware_async_captures_error(tmp_path):
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store)

    async def handler(_r):  # type: ignore[no-untyped-def]
        raise RuntimeError("async boom")

    with pytest.raises(RuntimeError, match="async boom"):
        asyncio.run(m.awrap_tool_call(_req(), handler))

    errors = store.recent(event_type="error")
    assert len(errors) == 1
    assert errors[0].data["error"] == "async boom"


def test_middleware_skips_detail_for_retrieval_tools(tmp_path):
    """ctx_recall/ctx_search should not log output_preview (their job is to
    inject content, so re-stashing or preview-logging their output would trap
    the agent)."""
    store = EventStore(tmp_path / "events.db")
    m = ContextMiddleware(store)

    m.wrap_tool_call(
        _req(name="ctx_recall"),
        lambda r: _tm("lots of offloaded content..."),
    )

    events = store.recent(event_type="tool_call")
    assert len(events) == 1
    assert events[0].data["output_preview"] == ""  # skipped


# --- ctx_search retrieval tool (unified blobs + events) ----------------------


def test_ctx_search_finds_event(tmp_path):
    """ctx_search unifies blobs + events — an event's output_preview is
    searchable, surfaced as an [event] hit tagged with its type."""
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"output_preview": "authentication failed"})
    store.capture("tool_call", {"output_preview": "grep completed"})
    store.flush()

    _, search = build_context_tools(store)
    out = search.invoke({"query": "authentication"})
    assert "1 hit" in out
    assert "tool_call" in out  # the event type is surfaced in the [event] line


def test_ctx_search_no_match(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"output_preview": "nothing relevant"})
    store.flush()

    _, search = build_context_tools(store)
    out = search.invoke({"query": "zzznonexistent"})
    assert "no prior tool output or event" in out  # unified empty-message phrasing


# --- v2: dedup ----------------------------------------------------------------


def test_dedup_skips_same_type_and_data(tmp_path):
    """Same type + same data within DEDUP_WINDOW should be deduped."""
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"tool": "grep"}, thread_id="t1")
    second = store.capture("tool_call", {"tool": "grep"}, thread_id="t1")
    store.flush()
    assert second == 0  # deduped, returns 0

    events = store.recent(thread_id="t1")
    assert len(events) == 1  # only one stored


def test_dedup_allows_different_data(tmp_path):
    """Same type but different data should NOT be deduped."""
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"tool": "grep"}, thread_id="t1")
    second = store.capture("tool_call", {"tool": "execute"}, thread_id="t1")
    store.flush()
    assert second > 0  # not deduped

    events = store.recent(thread_id="t1")
    assert len(events) == 2


def test_dedup_allows_different_type(tmp_path):
    """Different type but same data should NOT be deduped."""
    store = EventStore(tmp_path / "events.db")
    store.capture("tool_call", {"msg": "hello"}, thread_id="t1")
    second = store.capture("error", {"msg": "hello"}, thread_id="t1")
    store.flush()
    assert second > 0

    events = store.recent(thread_id="t1")
    assert len(events) == 2


# --- v2: FIFO eviction --------------------------------------------------------


def test_fifo_eviction(tmp_path):
    """Over MAX_EVENTS should evict lowest-priority then oldest."""
    from pux_harness.context.events import MAX_EVENTS_PER_SESSION

    store = EventStore(tmp_path / "events.db")
    # Fill to max with P3 events (low priority = evictable)
    for i in range(MAX_EVENTS_PER_SESSION):
        store.capture("tool_call", {"i": i}, priority=P3, thread_id="t1")
    # Add a P1 event (high priority = should survive eviction)
    store.capture("task_started", {"critical": True}, priority=P1, thread_id="t1")
    store.flush()

    events = store.recent(thread_id="t1")
    # Should be at most MAX_EVENTS (one P3 evicted to make room for P1)
    assert len(events) <= MAX_EVENTS_PER_SESSION
    # The P1 event should be present
    p1_events = [e for e in events if e.priority == P1]
    assert len(p1_events) == 1
    assert p1_events[0].type == "task_started"


# --- FTS5 ↔ events sync (triggers + one-time rebuild) -------------------------


def _fts_matches(store: EventStore, term: str) -> int:
    """Count events_fts hits for ``term`` via MATCH — the truthful probe for
    whether a row is still indexed. External-content FTS5 (``content=events``)
    HIDES rowids that have no backing ``events`` row from plain SELECT/JOIN,
    so a LEFT-JOIN orphan count is blind to them; only MATCH reports the
    stale index entry. This is exactly why an eviction that didn't propagate
    to events_fts is invisible to the naked eye yet still skews BM25."""
    conn = store._get_conn()
    return conn.execute(
        "SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH ?", (term,)
    ).fetchone()[0]


def test_eviction_keeps_fts_index_in_sync(tmp_path):
    """FIFO eviction DELETEs from events; the AFTER DELETE trigger must also
    remove the matching events_fts entry. Without it the evicted row's tokens
    stay in the index (MATCH-detectable) and pollute BM25 corpus stats. The
    evicted event's unique marker must therefore no longer MATCH; a
    surviving event's marker still must."""
    from pux_harness.context.events import MAX_EVENTS_PER_SESSION

    store = EventStore(tmp_path / "events.db")
    # Fill to the limit with evictable P3 events. uniq0 is oldest → evicted.
    for i in range(MAX_EVENTS_PER_SESSION):
        store.capture("tool_call", {"marker": f"uniq{i}"}, priority=P3, thread_id="t1")
    # One more → evicts the lowest-priority (P3), oldest event (uniq0).
    store.capture("task_started", {"critical": True}, priority=P1, thread_id="t1")
    store.flush()

    assert store.count(thread_id="t1") <= MAX_EVENTS_PER_SESSION  # an eviction ran
    assert _fts_matches(store, "uniq0") == 0   # evicted → gone from index
    assert _fts_matches(store, "uniq1") == 1   # survivor → still indexed


def test_rebuild_clears_preexisting_fts_orphans(tmp_path):
    """An existing DB upgraded in place may carry orphaned events_fts entries
    from pre-trigger evictions. Opening with the new schema must rebuild the
    index once (gated by PRAGMA user_version) — repopulating from the events
    content table drops any phantom — and never run that rebuild again."""
    db = tmp_path / "events.db"
    store = EventStore(db)
    store.capture("tool_call", {"marker": "real"}, thread_id="t1")
    store.flush()

    conn = store._get_conn()
    # Inject a phantom index entry whose rowid is absent from events —
    # simulates a pre-fix eviction orphan. (MATCH sees it; SELECT does not.)
    conn.execute(
        "INSERT INTO events_fts(rowid, type, data, thread_id) VALUES (?, ?, ?, ?)",
        (999999, "tool_call", '{"marker":"ghosttoken"}', "t1"),
    )
    conn.commit()
    assert _fts_matches(store, "ghosttoken") == 1   # precondition: phantom present

    # Force the gated rebuild to run again, then re-open the same DB.
    conn.execute("PRAGMA user_version = 0")
    conn.commit()
    store.close()

    reopened = EventStore(db)
    reopened.capture("tool_call", {"marker": "warmup"}, thread_id="t1")  # forces _init_schema
    reopened.flush()
    conn2 = reopened._get_conn()

    assert _fts_matches(reopened, "ghosttoken") == 0   # rebuild dropped the phantom
    assert _fts_matches(reopened, "real") == 1         # real row survived
    assert conn2.execute("PRAGMA user_version").fetchone()[0] == 1  # gated: won't run again


# --- v2: session_resume -------------------------------------------------------


def test_upsert_and_claim_resume(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.upsert_resume("sess-1", "<session_guide>snapshot</session_guide>", 42)

    # Claim it
    result = store.claim_latest_unconsumed_resume(exclude_session="sess-2")
    assert result is not None
    assert result["session_id"] == "sess-1"
    assert "snapshot" in result
    assert result["event_count"] == 42

    # Second claim should return None (already consumed)
    result2 = store.claim_latest_unconsumed_resume()
    assert result2 is None


def test_upsert_replaces_existing(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.upsert_resume("sess-1", "first", 10)
    store.upsert_resume("sess-1", "second", 20)

    result = store.get_resume("sess-1")
    assert result is not None
    assert result["snapshot"] == "second"
    assert result["event_count"] == 20


def test_resume_exclude_self(tmp_path):
    """A session should not claim its own mid-flight snapshot."""
    store = EventStore(tmp_path / "events.db")
    store.upsert_resume("sess-1", "snapshot", 5)

    # Exclude sess-1 — should not claim its own row
    result = store.claim_latest_unconsumed_resume(exclude_session="sess-1")
    assert result is None


def test_get_resume_missing(tmp_path):
    store = EventStore(tmp_path / "events.db")
    assert store.get_resume("nonexistent") is None


# --- v2: category field -------------------------------------------------------


def test_category_auto_populated(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("task_started", {"task": "x"})
    store.capture("tool_call", {"tool": "grep"})
    store.capture("error", {"msg": "fail"})
    store.flush()

    events = store.recent()
    cats = {e.type: e.category for e in events}
    assert cats["task_started"] == "task"
    assert cats["tool_call"] == "data"
    assert cats["error"] == "error"


def test_category_explicit_override(tmp_path):
    store = EventStore(tmp_path / "events.db")
    store.capture("custom_event", {"x": 1}, category="special")
    store.flush()

    events = store.recent()
    assert events[0].category == "special"
