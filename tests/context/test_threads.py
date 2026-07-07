"""Unified thread store — drives the REAL ``open_thread_store``.

Proves: (1) the saver + index tables exist after entry; (2) the live connection
carries the ``busy_timeout=5000`` pragma; (3) ``register_thread`` round-trips
through ``pux_threads``; (4) the saver SHARES the index's connection (the
single-connection property); (5) the LOAD-BEARING multi-process case — two
``open_thread_store()`` contexts open on the SAME file at once and both write
without ``database is locked``; (6) a thread written by one store is visible to
a fresh store opened later on the same file (the cross-runtime claim).

(5) is the proof the raw ``AsyncSqliteSaver(conn)`` form + ``busy_timeout`` fix
exists for: ``from_conn_string`` would open the saver's OWN connection (no
busy_timeout) and a second store would intermittently raise ``OperationalError``.

Sync tests driving async logic via ``asyncio.run`` — the harness suite is
all-sync (no pytest-asyncio dep), mirroring ``test_acp.py``.
"""
from __future__ import annotations

import asyncio
import json

import aiosqlite

from pux_harness.threads import ThreadStore, open_thread_store


async def _tables(conn: aiosqlite.Connection) -> list[str]:
    cur = await conn.execute(
        "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
    rows = [r[0] for r in await cur.fetchall()]
    await cur.close()
    return rows


def test_open_creates_saver_and_index_tables(tmp_path):
    async def go():
        db = tmp_path / "ts.sqlite"
        async with open_thread_store(db) as store:
            tables = await _tables(store.db)
            assert "pux_threads" in tables
            assert "checkpoints" in tables
            assert "writes" in tables
            assert isinstance(store, ThreadStore)

    asyncio.run(go())


def test_busy_timeout_pragma_set_on_live_connection(tmp_path):
    """The shared connection carries busy_timeout=5000 — the pragma the
    from_conn_string form would NOT set on the saver's writes."""
    async def go():
        db = tmp_path / "ts.sqlite"
        async with open_thread_store(db) as store:
            cur = await store.db.execute("PRAGMA busy_timeout")
            row = await cur.fetchone()
            await cur.close()
            assert row[0] == 5000
            # the saver holds the SAME connection, so its writes inherit the pragma
            assert store.saver.conn is store.db

    asyncio.run(go())


def test_register_thread_round_trips(tmp_path):
    async def go():
        db = tmp_path / "ts.sqlite"
        async with open_thread_store(db) as store:
            await store.register_thread("tid-1", "general", {"source": "direct"})
            cur = await store.db.execute(
                "SELECT org, metadata FROM pux_threads WHERE thread_id = ?", ("tid-1",))
            row = await cur.fetchone()
            await cur.close()
            assert row[0] == "general"
            assert json.loads(row[1]) == {"source": "direct"}

    asyncio.run(go())


def test_register_thread_idempotent(tmp_path):
    """Resuming an existing thread (pux direct --thread <id>) re-registers it
    without a duplicate-key failure — INSERT OR IGNORE."""
    async def go():
        db = tmp_path / "ts.sqlite"
        async with open_thread_store(db) as store:
            await store.register_thread("tid", "general")
            await store.register_thread("tid", "general")  # no raise
            cur = await store.db.execute("SELECT COUNT(*) FROM pux_threads")
            count = (await cur.fetchone())[0]
            await cur.close()
            assert count == 1

    asyncio.run(go())


def test_two_stores_concurrent_writes_no_lock(tmp_path):
    """LOAD-BEARING: two open_thread_store() contexts on the SAME file both
    write concurrently — no `database is locked`. This is the multi-process
    case (pux direct + pux serve) the single-connection + busy_timeout fix
    exists for. Both stores open their own connection; WAL + busy_timeout
    serialize the writers instead of erroring."""

    async def go():
        db = tmp_path / "shared.sqlite"
        async with open_thread_store(db) as s1:
            async with open_thread_store(db) as s2:
                await asyncio.gather(
                    s1.register_thread("t1", "general", {"who": "s1"}),
                    s2.register_thread("t2", "general", {"who": "s2"}),
                )
                cur = await s1.db.execute(
                    "SELECT thread_id, metadata FROM pux_threads ORDER BY thread_id")
                rows = {r[0]: json.loads(r[1]) for r in await cur.fetchall()}
                await cur.close()
        assert set(rows) == {"t1", "t2"}
        assert rows["t1"]["who"] == "s1"
        assert rows["t2"]["who"] == "s2"

    asyncio.run(go())


def test_persistence_survives_close_reopen(tmp_path):
    """The cross-runtime claim: a thread written by one store is visible to a
    fresh store opened later on the same file (pux direct writes, pux show reads)."""

    async def go():
        db = tmp_path / "persist.sqlite"
        async with open_thread_store(db) as s1:
            await s1.register_thread("persist-1", "invest", {"source": "direct"})
        async with open_thread_store(db) as s2:
            cur = await s2.db.execute(
                "SELECT org FROM pux_threads WHERE thread_id = ?", ("persist-1",))
            row = await cur.fetchone()
            await cur.close()
            assert row is not None and row[0] == "invest"

    asyncio.run(go())


def test_list_threads_filters_by_org_and_orders_newest_first(tmp_path):
    """``list_threads`` backs the ACP ``session/list`` surface: returns every row
    (newest-first by created_at), and narrows to one org when asked — so an ACP
    client enumerating general's sessions never sees invest's. Each row carries
    the four index columns as a dict (the shape load_session/list_sessions read)."""

    async def go():
        db = tmp_path / "list.sqlite"
        async with open_thread_store(db) as store:
            await store.register_thread("g1", "general", {"source": "acp"})
            await store.register_thread("i1", "invest", {"source": "acp"})
            await store.register_thread("g2", "general", {"source": "direct"})

            all_rows = await store.list_threads()
            assert {r["thread_id"] for r in all_rows} == {"g1", "i1", "g2"}
            # newest-first: g2 was registered last → it leads
            assert all_rows[0]["thread_id"] == "g2", (
                f"list_threads not newest-first: {[r['thread_id'] for r in all_rows]}"
            )
            # row shape — the keys load/list read
            assert set(all_rows[0]) == {"thread_id", "org", "metadata", "created_at"}

            general = await store.list_threads(org="general")
            assert {r["thread_id"] for r in general} == {"g1", "g2"}
            assert all(r["org"] == "general" for r in general)

            invest = await store.list_threads(org="invest")
            assert {r["thread_id"] for r in invest} == {"i1"}

    asyncio.run(go())


def test_get_thread_returns_row_or_none(tmp_path):
    """``get_thread`` backs the ACP ``session/load`` existence + org check: the
    row for a known id (with its org, so load can reject a cross-org request),
    and None for an unknown id (so load raises RequestError, not a phantom handle)."""

    async def go():
        db = tmp_path / "get.sqlite"
        async with open_thread_store(db) as store:
            await store.register_thread("known", "general", {"source": "acp"})

            hit = await store.get_thread("known")
            assert hit is not None
            assert hit["thread_id"] == "known"
            assert hit["org"] == "general"
            assert json.loads(hit["metadata"]) == {"source": "acp"}
            assert hit["created_at"]

            miss = await store.get_thread("no-such-id")
            assert miss is None

    asyncio.run(go())
