"""Event capture pipeline — structured events stored in SQLite (Phase 8).

Complements the message-level history that DeepAgents' SummarizationMiddleware
preserves.  Discrete events (file edits, git ops, decisions, errors, blockers)
are captured at the middleware layer and written to a per-project SQLite DB.
The agent can query them via ``event_recent`` / ``event_query`` tools, and the
structured snapshot builder (Phase 9) reads them to produce the compaction
injection.

Priority tiers (P1 critical → P4 low) drive budget enforcement in the snapshot:
P1 events are always included; P4 events are dropped first when the snapshot
must stay under its ≤2KB budget.

FTS5 is used for ranked retrieval (BM25) so the agent pulls back only the
relevant slice — fewer tokens recalled = lower cost per call.
"""
from __future__ import annotations

import json
import sqlite3
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

from pux_harness.agent.orgs import PROJECT_ROOT

EVENTS_DB = PROJECT_ROOT / ".pux" / "events.sqlite"


@dataclass(frozen=True)
class Event:
    """A single captured event."""

    id: int = 0
    ts: float = 0.0
    type: str = ""
    priority: int = 3  # P1-P4, default P3
    thread_id: str = ""
    data: dict[str, Any] = field(default_factory=dict)


# Priority constants -----------------------------------------------------------

P1 = 1  # Critical state — always preserved
P2 = 2  # Working state — preserved unless budget tight
P3 = 3  # Context — dropped first under budget pressure
P4 = 4  # Low — analytics/debug

# Event type → default priority mapping
EVENT_PRIORITIES: dict[str, int] = {
    # P1 — critical
    "task_started": P1,
    "task_completed": P1,
    "task_failed": P1,
    "decision_made": P1,
    "error": P1,
    "blocker": P1,
    # P2 — working state
    "file_modified": P2,
    "git_operation": P2,
    "tool_call": P2,
    # P3 — context
    "user_correction": P3,
    "approach_rejected": P3,
    "env_change": P3,
    # P4 — low
    "session_start": P4,
    "session_end": P4,
    "compaction": P4,
}


class EventStore:
    """Append-only SQLite store for structured agent events.

    Two indexes support the main query patterns:
    - ``idx_events_thread`` — resume a session (thread_id + ts range)
    - ``idx_events_type`` — filter by event type

    FTS5 virtual table ``events_fts`` enables BM25-ranked keyword search
    across event data.
    """

    def __init__(self, db_path: str | Path) -> None:
        self.db_path = Path(db_path)
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._conn: sqlite3.Connection | None = None

    def _get_conn(self) -> sqlite3.Connection:
        if self._conn is None:
            self._conn = sqlite3.connect(str(self.db_path))
            self._conn.execute("PRAGMA journal_mode=WAL")
            self._conn.execute("PRAGMA busy_timeout=3000")
            self._conn.row_factory = sqlite3.Row
            self._init_schema(self._conn)
        return self._conn

    @staticmethod
    def _init_schema(conn: sqlite3.Connection) -> None:
        conn.executescript(
            """
            CREATE TABLE IF NOT EXISTS events (
                id INTEGER PRIMARY KEY,
                ts REAL NOT NULL,
                type TEXT NOT NULL,
                priority INTEGER NOT NULL DEFAULT 3,
                thread_id TEXT NOT NULL DEFAULT '',
                data TEXT NOT NULL DEFAULT '{}'
            );
            CREATE INDEX IF NOT EXISTS idx_events_thread
                ON events(thread_id, ts);
            CREATE INDEX IF NOT EXISTS idx_events_type
                ON events(type, ts);
            """
        )
        # FTS5 for ranked search — created separately so a missing FTS5
        # build doesn't block the main table.
        try:
            conn.execute(
                """
                CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
                    type, data, thread_id,
                    content=events, content_rowid=id
                )
                """
            )
        except sqlite3.OperationalError:
            pass  # FTS5 not compiled in — degrade to LIKE

    # -- write -----------------------------------------------------------------

    def capture(
        self,
        event_type: str,
        data: dict[str, Any] | None = None,
        *,
        priority: int | None = None,
        thread_id: str = "",
    ) -> int:
        """Capture an event. Returns the event id.

        If *priority* is ``None`` it is looked up from ``EVENT_PRIORITIES``
        (defaulting to P3 for unknown types).
        """
        if priority is None:
            priority = EVENT_PRIORITIES.get(event_type, P3)
        now = time.time()
        data_json = json.dumps(data or {}, ensure_ascii=False)
        conn = self._get_conn()
        cur = conn.execute(
            "INSERT INTO events (ts, type, priority, thread_id, data) "
            "VALUES (?, ?, ?, ?, ?)",
            (now, event_type, priority, thread_id, data_json),
        )
        rowid = cur.lastrowid
        # Sync to FTS index (best-effort).
        try:
            conn.execute(
                "INSERT INTO events_fts(rowid, type, data, thread_id) "
                "VALUES (?, ?, ?, ?)",
                (rowid, event_type, data_json, thread_id),
            )
        except sqlite3.OperationalError:
            pass
        return rowid  # type: ignore[return-value]

    def flush(self) -> None:
        """Commit any pending writes."""
        if self._conn is not None:
            self._conn.commit()

    # -- read ------------------------------------------------------------------

    def recent(
        self,
        *,
        thread_id: str = "",
        event_type: str = "",
        limit: int = 20,
        min_priority: int = P4,
    ) -> list[Event]:
        """Return the most recent events, newest first.

        Filters:
        - *thread_id*: only events for this thread (empty = all threads)
        - *event_type*: only events of this type (empty = all types)
        - *min_priority*: only events with priority ≤ this value (lower = more critical)
        """
        conn = self._get_conn()
        clauses: list[str] = ["priority <= ?"]
        params: list[Any] = [min_priority]
        if thread_id:
            clauses.append("thread_id = ?")
            params.append(thread_id)
        if event_type:
            clauses.append("type = ?")
            params.append(event_type)
        where = " AND ".join(clauses)
        params.append(limit)
        rows = conn.execute(
            f"SELECT id, ts, type, priority, thread_id, data "
            f"FROM events WHERE {where} ORDER BY ts DESC LIMIT ?",
            params,
        ).fetchall()
        return [self._row_to_event(r) for r in rows]

    def query(
        self,
        search: str,
        *,
        thread_id: str = "",
        limit: int = 10,
    ) -> list[Event]:
        """BM25-ranked search across event data.

        Each word is matched as a prefix (``auth*`` matches ``authentication``)
        so partial stems still hit.  Falls back to LIKE if FTS5 is unavailable.
        """
        conn = self._get_conn()
        # Build FTS5 query: each word gets a trailing * for prefix matching.
        fts_query = " ".join(w.strip() for w in search.split() if w.strip())
        fts_query = " ".join(f"{w}*" for w in fts_query.split() if w)
        if not fts_query:
            return []

        # Try FTS5 first.
        try:
            if thread_id:
                rows = conn.execute(
                    "SELECT e.id, e.ts, e.type, e.priority, e.thread_id, e.data "
                    "FROM events_fts f "
                    "JOIN events e ON e.id = f.rowid "
                    "WHERE events_fts MATCH ? AND e.thread_id = ? "
                    "ORDER BY rank LIMIT ?",
                    [fts_query, thread_id, limit],
                ).fetchall()
            else:
                rows = conn.execute(
                    "SELECT e.id, e.ts, e.type, e.priority, e.thread_id, e.data "
                    "FROM events_fts f "
                    "JOIN events e ON e.id = f.rowid "
                    "WHERE events_fts MATCH ? "
                    "ORDER BY rank LIMIT ?",
                    [fts_query, limit],
                ).fetchall()
            return [self._row_to_event(r) for r in rows]
        except sqlite3.OperationalError:
            pass  # FTS5 not available — fall through to LIKE

        # LIKE fallback.
        like = f"%{search}%"
        clauses: list[str] = ["data LIKE ?"]
        params = [like]
        if thread_id:
            clauses.append("thread_id = ?")
            params.append(thread_id)
        where = " AND ".join(clauses)
        params.append(limit)
        rows = conn.execute(
            f"SELECT id, ts, type, priority, thread_id, data "
            f"FROM events WHERE {where} ORDER BY ts DESC LIMIT ?",
            params,
        ).fetchall()
        return [self._row_to_event(r) for r in rows]

    def count(self, *, thread_id: str = "") -> int:
        """Total event count, optionally filtered by thread."""
        conn = self._get_conn()
        if thread_id:
            row = conn.execute(
                "SELECT COUNT(*) FROM events WHERE thread_id = ?", (thread_id,)
            ).fetchone()
        else:
            row = conn.execute("SELECT COUNT(*) FROM events").fetchone()
        return row[0] if row else 0

    def close(self) -> None:
        if self._conn is not None:
            self._conn.close()
            self._conn = None

    @staticmethod
    def _row_to_event(row: sqlite3.Row) -> Event:
        return Event(
            id=row["id"],
            ts=row["ts"],
            type=row["type"],
            priority=row["priority"],
            thread_id=row["thread_id"],
            data=json.loads(row["data"]),
        )


# -- process-wide singleton ---------------------------------------------------

_store: EventStore | None = None


def shared_event_store() -> EventStore:
    """One process-wide event store at ``<project>/.pux/events.sqlite``."""
    global _store
    if _store is None:
        _store = EventStore(EVENTS_DB)
    return _store
