"""Push-notification contract: run completion also fans to the event stream.

Companion to ``test_run_webhook.py``. The outbound webhook (POST to a caller
URL) only helps a client that HOSTS a receiver. Hermes (a Telegram bot behind
NAT — "Hermes can't make webhooks on the sandbox") hosts none, so for background
runs (``start_run``) it would be stuck polling ``list_runs``. The fix is a
receiver-of-last-resort that lives ON the pux side: ``server._run_task`` publishes
the SAME terminal payload to an in-process ``EventBus`` (``run_events.py``),
exposed as ``GET /events`` (poll/catch-up) + ``GET /events/stream`` (SSE) +
``GET /events/health``. Hermes subscribes once and gets every run's completion.

Two tiers ([[verify-or-die]], prove-not-assert):

* **Wiring** (offline, keyless): a stub graph + monkeypatched Docker prep; fire a
  background run, then ``GET /events`` returns the run's completion event with
  ``event="run.completed"`` and NO ``webhook_url``. Proves ``_run_task`` publishes
  on every terminal outcome (success + error both exercised).
* **Bus unit** (offline): the real ``EventBus`` — fan-out to a live subscriber,
  ``recent(since=)`` catch-up, jsonl persistence, bounded ring, health counts.
"""
from __future__ import annotations

import asyncio
import json
from pathlib import Path
from typing import Any

import pytest
from fastapi.testclient import TestClient
from langchain_core.messages import AIMessage, HumanMessage

from pux_harness import server
from pux_harness.run_events import EventBus


# ---------------------------------------------------------------------------
# Stub graph (mirrors test_run_webhook.py)
# ---------------------------------------------------------------------------


class _Snap:
    def __init__(self) -> None:
        self.next: tuple[str, ...] = ()
        self.tasks: tuple[Any, ...] = ()
        self.config: dict[str, Any] = {}
        self.parent_config = None


class _StubGraph:
    def __init__(self, *, answer: str = "events-stub-answer") -> None:
        self._answer = answer
        self.nodes: dict[str, Any] = {}

    async def ainvoke(self, inp: Any, config: dict[str, Any]) -> dict[str, Any]:
        return {"messages": [HumanMessage("hi"), AIMessage(self._answer)]}

    async def aget_state(self, config: dict[str, Any]) -> _Snap:
        return _Snap()


@pytest.fixture
def client(tmp_path, monkeypatch) -> TestClient:
    """Isolated DB + stub graph; Docker prep skipped. The REAL EventBus is used
    (it is pure in-process), so /events reflects what _run_task publishes."""
    import pux_harness.sandbox.container as container_mod
    import pux_harness.threads as threads_mod

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "events.sqlite")
    monkeypatch.setattr(server, "build_graph", lambda org, **kw: _StubGraph())
    monkeypatch.setattr(container_mod, "prepare", lambda org: [])

    with TestClient(server.app) as c:
        yield c


def _create_thread(client: TestClient, org: str = "general") -> str:
    return client.post("/threads", json={"agent_id": org}).json()["thread_id"]


def _run_to_completion(client: TestClient, tid: str, body: dict[str, Any]) -> str:
    run_id = client.post(f"/threads/{tid}/runs", json=body).json()["run_id"]
    client.get(f"/runs/{run_id}/wait")  # blocks until _run_task resolves
    return run_id


# ---------------------------------------------------------------------------
# wiring: _run_task publishes to the bus on every terminal outcome
# ---------------------------------------------------------------------------


def test_background_run_publishes_completion_to_events(client: TestClient) -> None:
    """A background run publishes its completion to the event bus — a webhook-less
    client learns it via GET /events with no polling of list_runs."""
    tid = _create_thread(client)
    run_id = _run_to_completion(client, tid, {"input": "hi", "webhook_url": "http://cb/x"})

    r = client.get("/events")
    assert r.status_code == 200, r.text
    events = r.json()["events"]
    matching = [e for e in events if e.get("run_id") == run_id]
    assert len(matching) == 1, f"expected the run in /events, got: {events}"
    ev = matching[0]
    assert ev["event"] == "run.completed"
    assert ev["status"] == "success"
    assert ev["output"] == "events-stub-answer"
    assert "webhook_url" not in ev, "callback URL must not appear in the event payload"
    assert ev.get("ts"), "server-side ts stamped for catch-up ordering"


def test_failed_run_publishes_error_terminal(client: TestClient, monkeypatch, tmp_path) -> None:
    """An errored run still publishes — status=error — so a webhook-less client
    learns the run FAILED, not just succeeded."""
    import pux_harness.sandbox.container as container_mod
    import pux_harness.threads as threads_mod

    class _BoomGraph(_StubGraph):
        async def ainvoke(self, inp: Any, config: dict[str, Any]) -> dict[str, Any]:
            raise RuntimeError("simulated agent failure")

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "events-err.sqlite")
    monkeypatch.setattr(server, "build_graph", lambda org, **kw: _BoomGraph())
    monkeypatch.setattr(container_mod, "prepare", lambda org: [])

    with TestClient(server.app) as c:
        tid = c.post("/threads", json={"agent_id": "general"}).json()["thread_id"]
        rid = c.post(f"/threads/{tid}/runs", json={"input": "x"}).json()["run_id"]
        c.get(f"/runs/{rid}/wait")
        events = c.get("/events").json()["events"]
    assert any(e.get("run_id") == rid and e.get("status") == "error" for e in events), events


def test_events_health_reports_subscribers_and_count(client: TestClient) -> None:
    r = client.get("/events/health")
    assert r.status_code == 200
    h = r.json()
    assert h["ok"] is True
    assert "subscribers" in h and "events" in h


def test_events_since_filters_by_ts(client: TestClient) -> None:
    """?since=<ts> returns only events newer than ts — the catch-up path on reconnect."""
    tid = _create_thread(client)
    _run_to_completion(client, tid, {"input": "hi"})
    all_events = client.get("/events").json()["events"]
    assert all_events, "precondition: at least one event published"
    cutoff = all_events[0]["ts"]
    # filter to strictly-after the first event's ts
    newer = client.get("/events", params={"since": cutoff}).json()["events"]
    assert all(e["ts"] > cutoff for e in newer)
    # far-future cutoff returns nothing
    assert client.get("/events", params={"since": "9999-01-01T00:00:00Z"}).json()["events"] == []


# ---------------------------------------------------------------------------
# bus unit (real EventBus, no server)
# ---------------------------------------------------------------------------


def test_bus_fans_out_to_live_subscriber(tmp_path: Path) -> None:
    """publish() delivers to a subscribed queue — the mechanism /events/stream drains."""
    bus = EventBus(log_path=tmp_path / "ev.jsonl")

    async def main() -> None:
        q = bus.subscribe()
        await bus.publish({"run_id": "r1", "status": "success", "event": "run.completed"})
        ev = await asyncio.wait_for(q.get(), timeout=2)
        assert ev["run_id"] == "r1"
        assert ev["ts"]  # server-side stamp added

    asyncio.run(main())
    assert bus.health()["subscribers"] == 1


def test_bus_unsubscribe_stops_delivery(tmp_path: Path) -> None:
    bus = EventBus()
    q = bus.subscribe()
    bus.unsubscribe(q)
    assert bus.health()["subscribers"] == 0


def test_bus_recent_and_since(tmp_path: Path) -> None:
    bus = EventBus()

    async def main() -> None:
        for i in range(5):
            await bus.publish({"run_id": f"r{i}", "event": "run.completed"})
            await asyncio.sleep(0.002)  # real completions are never sub-ms apart

    asyncio.run(main())
    recent = bus.recent(limit=3)
    assert [e["run_id"] for e in recent] == ["r2", "r3", "r4"]
    # monotonic seq stamped on every event (deterministic ordering + client dedup)
    assert [e["seq"] for e in bus.recent(limit=100)] == [0, 1, 2, 3, 4]
    # since the 2nd event's ts → only r2..r4 (strictly after r1)
    since = bus.recent()[1]["ts"]
    assert [e["run_id"] for e in bus.recent(since=since)] == ["r2", "r3", "r4"]


def test_bus_persists_to_jsonl(tmp_path: Path) -> None:
    """Events append to .pux/run_events.jsonl so catch-up survives a restart."""
    log = tmp_path / "ev.jsonl"
    bus = EventBus(log_path=log)

    async def main() -> None:
        await bus.publish({"run_id": "r1", "status": "success", "event": "run.completed"})

    asyncio.run(main())
    lines = log.read_text().splitlines()
    assert len(lines) == 1
    rec = json.loads(lines[0])
    assert rec["run_id"] == "r1" and rec["event"] == "run.completed" and rec["ts"]
    assert rec["seq"] == 0


def test_bus_ring_is_bounded(tmp_path: Path) -> None:
    """The in-memory ring is bounded — old events age out (the log keeps them)."""
    bus = EventBus(cap=3)

    async def main() -> None:
        for i in range(10):
            await bus.publish({"run_id": f"r{i}", "event": "run.completed"})

    asyncio.run(main())
    ids = [e["run_id"] for e in bus.recent(limit=100)]
    assert ids == ["r7", "r8", "r9"], f"ring should keep only the last cap=3, got {ids}"
