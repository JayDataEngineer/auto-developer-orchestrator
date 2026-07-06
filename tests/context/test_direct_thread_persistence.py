"""Prove ``pux direct`` persists threads to the SHARED store.

This is the cross-runtime-wiring proof, not a unit test of the helper
([[feedback_prepare_wiring_e2e_gap]]): it drives the REAL ``_run`` entry point
(the one ``pux direct`` calls) with the Docker/model touch-points stubbed, then
opens a FRESH ``open_thread_store()`` on the same sqlite file and asserts the
thread landed in ``pux_threads`` with ``metadata.source == "direct"``. That is
exactly the wiring ``pux show <id>`` / ``pux resume`` will rely on — a thread
created by ``direct`` visible to the server-side read path."""
from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

from pux_harness import main
from pux_harness.threads import open_thread_store


def _fake_agent():
    async def _ainvoke(state, config=None):
        return {"messages": [SimpleNamespace(
            type="ai", content="ok",
            tool_calls=[],
            usage_metadata={"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
        )]}

    return SimpleNamespace(ainvoke=_ainvoke)


def test_run_registers_thread_in_shared_store(monkeypatch, tmp_path):
    """``_run`` writes the thread into ``pux_threads`` via the shared store, so a
    fresh reader on the same DB sees it — the literal unification claim."""
    import pux_harness.threads as threads_mod

    db = tmp_path / "direct.sqlite"
    monkeypatch.setattr(threads_mod, "PUX_API_DB", db)

    # Stub the three touch-points _run reaches outside the store:
    #   _build_agent    -> (fake agent, fake backend) so no Docker/model graph build
    #   shared_exec     -> sentinel exec client handed to prepare()
    #   resolve_tool_servers -> [] so the MCP branch is skipped (hermetic)
    #   prepare         -> [] so no prep jobs run
    fake_backend = SimpleNamespace(execute_log=[])
    monkeypatch.setattr(
        main, "_build_agent",
        lambda org, saver=None, mcp_tools=None: (_fake_agent(), fake_backend),
    )
    monkeypatch.setattr(main, "shared_exec", lambda: object())
    monkeypatch.setattr(main, "resolve_tool_servers", lambda org: [])
    monkeypatch.setattr("pux_harness.sandbox.container.prepare",
                        lambda org, *, exec_client=None, **kw: [])

    # Pass an explicit thread id so we know what to look for afterwards.
    thread_id = "general-deadbeef"
    asyncio.run(main._run("general", "remember the number 42",
                          recursion_limit=5, thread=thread_id))

    # A FRESH store on the same file — simulates a separate `pux show` process.
    async def _read():
        async with open_thread_store() as store:
            cur = await store.db.execute(
                "SELECT org, metadata FROM pux_threads WHERE thread_id = ?",
                (thread_id,))
            return await cur.fetchone()

    row = asyncio.run(_read())
    assert row is not None, "thread not registered in pux_threads"
    org, metadata_json = row
    assert org == "general"
    metadata = json.loads(metadata_json)
    assert metadata["source"] == "direct"
    assert metadata["task"] == "remember the number 42"


def test_run_uses_existing_thread_when_passed(monkeypatch, tmp_path):
    """``--thread <id>`` must NOT mint a fresh id; it resumes the given one.
    Proves the resume path reuses the id (so checkpoints land on the same row)."""
    import pux_harness.threads as threads_mod

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "resume.sqlite")
    fake_backend = SimpleNamespace(execute_log=[])
    monkeypatch.setattr(
        main, "_build_agent",
        lambda org, saver=None, mcp_tools=None: (_fake_agent(), fake_backend),
    )
    monkeypatch.setattr(main, "shared_exec", lambda: object())
    monkeypatch.setattr(main, "resolve_tool_servers", lambda org: [])
    monkeypatch.setattr("pux_harness.sandbox.container.prepare",
                        lambda org, *, exec_client=None, **kw: [])

    seen_thread: list[str] = []
    captured = SimpleNamespace(ainvoke=None)

    async def _capture_ainvoke(state, config=None):
        seen_thread.append(config["configurable"]["thread_id"])
        return {"messages": [SimpleNamespace(
            type="ai", content="ok", tool_calls=[],
            usage_metadata={"input_tokens": 0, "output_tokens": 0, "total_tokens": 0},
        )]}

    captured.ainvoke = _capture_ainvoke
    monkeypatch.setattr(
        main, "_build_agent",
        lambda org, saver=None, mcp_tools=None: (captured, fake_backend),
    )

    asyncio.run(main._run("general", "continue", recursion_limit=5,
                          thread="general-existing-thread"))

    assert seen_thread == ["general-existing-thread"]
