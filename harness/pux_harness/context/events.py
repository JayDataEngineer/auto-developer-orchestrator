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

Schema additions (v2, modeled after mksglu/context-mode SessionDB):
- ``category`` — groups event types by domain (file, error, git, etc.)
- ``data_hash`` — SHA-256 dedup hash (skip if same type+hash in last N events)
- ``session_resume`` table — compaction snapshots for cross-session rehydration
- FIFO eviction at 1000 events per session
"""
from __future__ import annotations

import hashlib
import json
import sqlite3
import time
from dataclasses import dataclass, field
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
    # v2 additions (backward-compatible — old callers never set these)
    category: str = ""
    data_hash: str = ""
    created_at: str = ""


# Priority constants -----------------------------------------------------------

P1 = 1  # Critical state — always preserved
P2 = 2  # Working state — preserved unless budget tight
P3 = 3  # Context — dropped first under budget pressure
P4 = 4  # Low — analytics/debug

# Event type → default priority mapping (original API, preserved)
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

# Category grouping (v2, for snapshot builder)
EVENT_CATEGORIES: dict[str, str] = {
    "task_started": "task",
    "task_completed": "task",
    "task_failed": "task",
    "file_modified": "file",
    "git_operation": "git",
    "tool_call": "data",
    "error": "error",
    "blocker": "error",
    "decision_made": "decision",
    "user_correction": "decision",
    "approach_rejected": "decision",
    "env_change": "env",
    "session_start": "data",
    "session_end": "data",
    "compaction": "data",
}

# Dedup + eviction constants
MAX_EVENTS_PER_SESSION = 1000
DEDUP_WINDOW = 5


def _data_hash(data: str) -> str:
    """SHA-256 dedup hash (first 16 hex chars)."""
    return hashlib.sha256(data.encode("utf-8")).hexdigest()[:16].upper()


class EventStore:
    """Append-only SQLite store for structured agent events.

    Original API preserved for backward compatibility with event_middleware
    and event_tools.  v2 additions (category, data_hash, session_resume,
    dedup, FIFO eviction) are additive — old callers unaffected.

    Indexes:
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
            self._conn.execute("PRAGMA synchronous=NORMAL")
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
                data TEXT NOT NULL DEFAULT '{}',
                category TEXT NOT NULL DEFAULT '',
                data_hash TEXT NOT NULL DEFAULT ''
            );
            CREATE INDEX IF NOT EXISTS idx_events_thread
                ON events(thread_id, ts);
            CREATE INDEX IF NOT EXISTS idx_events_type
                ON events(type, ts);

            CREATE TABLE IF NOT EXISTS session_resume (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                session_id TEXT NOT NULL UNIQUE,
                snapshot TEXT NOT NULL,
                event_count INTEGER NOT NULL,
                created_at TEXT NOT NULL DEFAULT (datetime('now')),
                consumed INTEGER NOT NULL DEFAULT 0
            );
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
        # Migrate: add category + data_hash columns to existing tables.
        try:
            cols = {r["name"] for r in conn.execute("PRAGMA table_xinfo(events)").fetchall()}
            if "category" not in cols:
                conn.execute("ALTER TABLE events ADD COLUMN category TEXT NOT NULL DEFAULT ''")
            if "data_hash" not in cols:
                conn.execute("ALTER TABLE events ADD COLUMN data_hash TEXT NOT NULL DEFAULT ''")
        except sqlite3.OperationalError:
            pass

    # -- write -----------------------------------------------------------------

    def capture(
        self,
        event_type: str,
        data: Any = None,
        *,
        priority: int | None = None,
        thread_id: str = "",
        category: str = "",
    ) -> int:
        """Capture an event. Returns the event id.

        ``data`` accepts a dict (legacy) or a string (v2).  Dicts are
        JSON-serialized.  If *priority* is ``None`` it is looked up from
        ``EVENT_PRIORITIES`` (defaulting to P3 for unknown types).

        v2: dedup skips if same type+data_hash in last DEDUP_WINDOW events.
        v2: FIFO eviction of lowest-priority event when over MAX_EVENTS.
        """
        if priority is None:
            priority = EVENT_PRIORITIES.get(event_type, P3)
        if category == "":
            category = EVENT_CATEGORIES.get(event_type, "data")

        # Normalize data to string for storage + hashing.
        if isinstance(data, dict):
            data_str = json.dumps(data, ensure_ascii=False, default=str)
        elif isinstance(data, str):
            data_str = data
        else:
            data_str = json.dumps(data, ensure_ascii=False, default=str) if data is not None else ""

        dhash = _data_hash(data_str)
        now = time.time()
        conn = self._get_conn()

        # v2 dedup: skip if same type+hash in last N events for this thread.
        if thread_id:
            dup = conn.execute(
                "SELECT 1 FROM ("
                "  SELECT type, data_hash FROM events"
                "  WHERE thread_id = ? ORDER BY id DESC LIMIT ?"
                ") AS recent WHERE recent.type = ? AND recent.data_hash = ? LIMIT 1",
                (thread_id, DEDUP_WINDOW, event_type, dhash),
            ).fetchone()
            if dup:
                return 0

        # v2 FIFO eviction of lowest-priority (then oldest) event.
        if thread_id:
            count_row = conn.execute(
                "SELECT COUNT(*) AS cnt FROM events WHERE thread_id = ?",
                (thread_id,),
            ).fetchone()
            if count_row and count_row["cnt"] >= MAX_EVENTS_PER_SESSION:
                conn.execute(
                    "DELETE FROM events WHERE id = ("
                    "  SELECT id FROM events WHERE thread_id = ?"
                    "  ORDER BY priority ASC, id ASC LIMIT 1"
                    ")",
                    (thread_id,),
                )

        cur = conn.execute(
            "INSERT INTO events (ts, type, priority, thread_id, data, category, data_hash) "
            "VALUES (?, ?, ?, ?, ?, ?, ?)",
            (now, event_type, priority, thread_id, data_str, category, dhash),
        )
        rowid = cur.lastrowid
        # Sync to FTS index (best-effort).
        try:
            conn.execute(
                "INSERT INTO events_fts(rowid, type, data, thread_id) "
                "VALUES (?, ?, ?, ?)",
                (rowid, event_type, data_str, thread_id),
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
        params: list[int | str] = [min_priority]
        if thread_id:
            clauses.append("thread_id = ?")
            params.append(thread_id)
        if event_type:
            clauses.append("type = ?")
            params.append(event_type)
        where = " AND ".join(clauses)
        params.append(limit)
        rows = conn.execute(
            f"SELECT id, ts, type, priority, thread_id, data, category, data_hash "
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
                    "SELECT e.id, e.ts, e.type, e.priority, e.thread_id, e.data, "
                    "       e.category, e.data_hash "
                    "FROM events_fts f "
                    "JOIN events e ON e.id = f.rowid "
                    "WHERE events_fts MATCH ? AND e.thread_id = ? "
                    "ORDER BY rank LIMIT ?",
                    [fts_query, thread_id, limit],
                ).fetchall()
            else:
                rows = conn.execute(
                    "SELECT e.id, e.ts, e.type, e.priority, e.thread_id, e.data, "
                    "       e.category, e.data_hash "
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
        params: list[int | str] = [like]
        if thread_id:
            clauses.append("thread_id = ?")
            params.append(thread_id)
        where = " AND ".join(clauses)
        params.append(limit)
        rows = conn.execute(
            f"SELECT id, ts, type, priority, thread_id, data, category, data_hash "
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

    # -- v2: resume snapshot ---------------------------------------------------

    def upsert_resume(self, session_id: str, snapshot: str, event_count: int) -> None:
        """Store a compaction snapshot (replaces any existing for this session)."""
        conn = self._get_conn()
        conn.execute(
            "INSERT INTO session_resume (session_id, snapshot, event_count) "
            "VALUES (?, ?, ?) "
            "ON CONFLICT(session_id) DO UPDATE SET "
            "  snapshot = excluded.snapshot, "
            "  event_count = excluded.event_count, "
            "  created_at = datetime('now'), "
            "  consumed = 0",
            (session_id, snapshot, event_count),
        )
        conn.commit()

    def claim_latest_unconsumed_resume(
        self, exclude_session: str = ""
    ) -> dict[str, Any] | None:
        """Atomically pick the newest unconsumed snapshot and mark it consumed."""
        conn = self._get_conn()
        row = conn.execute(
            "UPDATE session_resume "
            "SET consumed = 1 "
            "WHERE id = ("
            "  SELECT id FROM session_resume "
            "  WHERE consumed = 0 AND session_id != ? "
            "  ORDER BY created_at DESC, id DESC LIMIT 1"
            ") "
            "RETURNING session_id, snapshot, event_count, consumed",
            (exclude_session,),
        ).fetchone()
        if row is None:
            return None
        return {
            "session_id": row["session_id"],
            "snapshot": row["snapshot"],
            "event_count": row["event_count"],
        }

    def get_resume(self, session_id: str) -> dict[str, Any] | None:
        conn = self._get_conn()
        row = conn.execute(
            "SELECT snapshot, event_count, consumed "
            "FROM session_resume WHERE session_id = ?",
            (session_id,),
        ).fetchone()
        if row is None:
            return None
        return {
            "snapshot": row["snapshot"],
            "event_count": row["event_count"],
            "consumed": bool(row["consumed"]),
        }

    @staticmethod
    def _row_to_event(row: sqlite3.Row) -> Event:
        # Parse data: try JSON first (dict legacy), fall back to raw string.
        raw_data = row["data"]
        try:
            parsed_data = json.loads(raw_data)
        except (json.JSONDecodeError, TypeError):
            parsed_data = {"raw": raw_data}

        return Event(
            id=row["id"],
            ts=row["ts"],
            type=row["type"],
            priority=row["priority"],
            thread_id=row["thread_id"],
            data=parsed_data if isinstance(parsed_data, dict) else {"raw": parsed_data},
            category=row["category"] if "category" in row.keys() else "",
            data_hash=row["data_hash"] if "data_hash" in row.keys() else "",
        )


# -- process-wide singleton ---------------------------------------------------

_store: EventStore | None = None


def shared_event_store() -> EventStore:
    """One process-wide event store at ``<project>/.pux/events.sqlite``."""
    global _store
    if _store is None:
        _store = EventStore(EVENTS_DB)
    return _store
