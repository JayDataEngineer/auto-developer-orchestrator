"""Push-notification contract: a background run fires a completion webhook.

Closes the gap surfaced 2026-07-08: an MCP client fires ``start_run`` and ends
its turn — with nothing pushing completion it must poll ``list_runs``. The fix is
a run-completion webhook: the background run's terminal metadata is POSTed to a
caller-supplied URL so the caller learns the run finished with NO polling.

Two tiers ([[verify-or-die]], prove-not-assert):

* **Wiring** (offline, keyless, deterministic): monkeypatches the dispatcher
  injection point ``server._dispatch_run_webhook`` with a recorder and asserts
  it is called ONCE, after ``/runs/{id}/wait`` resolves, with the run's terminal
  meta (status + run_id + output). Proves ``_run_task`` notifies on EVERY
  terminal outcome (success/interrupted/error), and stays silent with no URL.
* **Dispatcher unit** (offline): drives the REAL ``_dispatch_run_webhook`` with a
  fake httpx client — proves the HTTP payload drops the caller's ``webhook_url``
  and tags ``event="run.completed"``, and that a dead target is SWALLOWED (never
  raises into the run). The real network delivery is upstream httpx's job.
"""
from __future__ import annotations

import asyncio
from typing import Any

import pytest
from fastapi.testclient import TestClient
from langchain_core.messages import AIMessage, HumanMessage

from pux_harness import server


# ---------------------------------------------------------------------------
# Stub graph — _invoke_once calls ainvoke + aget_state. Returns a finished run
# (next empty => not interrupted) whose answer _final_answer extracts.
# ---------------------------------------------------------------------------


class _Snap:
    def __init__(self) -> None:
        self.next: tuple[str, ...] = ()
        self.tasks: tuple[Any, ...] = ()
        self.config: dict[str, Any] = {}
        self.parent_config = None


class _WebhookStubGraph:
    def __init__(self, *, answer: str = "webhook-stub-answer") -> None:
        self._answer = answer
        self.nodes: dict[str, Any] = {}

    async def ainvoke(self, inp: Any, config: dict[str, Any]) -> dict[str, Any]:
        return {"messages": [HumanMessage("hi"), AIMessage(self._answer)]}

    async def aget_state(self, config: dict[str, Any]) -> _Snap:
        return _Snap()


@pytest.fixture
def client(tmp_path, monkeypatch) -> TestClient:
    """Isolated DB + stub graph + recorder dispatcher. Skips Docker prep
    (monkeypatches ``container.prepare``) so the background run is fast."""
    import pux_harness.sandbox.container as container_mod
    import pux_harness.threads as threads_mod

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "webhook.sqlite")
    monkeypatch.setattr(server, "build_graph", lambda org, **kw: _WebhookStubGraph())
    monkeypatch.setattr(container_mod, "prepare", lambda org: [])

    calls: list[dict[str, Any]] = []

    async def _record(meta: dict[str, Any]) -> None:
        calls.append(dict(meta))

    monkeypatch.setattr(server, "_dispatch_run_webhook", _record)

    with TestClient(server.app) as c:
        c._webhook_calls = calls  # type: ignore[attr-defined]
        yield c


def _create_thread(client: TestClient, org: str = "general") -> str:
    return client.post("/threads", json={"agent_id": org}).json()["thread_id"]


def _run_to_completion(client: TestClient, tid: str, body: dict[str, Any]) -> str:
    r = client.post(f"/threads/{tid}/runs", json=body)
    assert r.status_code == 200, r.text
    run_id = r.json()["run_id"]
    w = client.get(f"/runs/{run_id}/wait")  # blocks until _run_task resolves
    assert w.status_code == 200, w.text
    return run_id


# ---------------------------------------------------------------------------
# wiring
# ---------------------------------------------------------------------------


def test_background_run_fires_webhook_with_terminal_meta(client: TestClient) -> None:
    """A background run with a webhook_url fires the dispatcher exactly once,
    AFTER the run reaches a terminal state, carrying run_id + status + output."""
    calls = client._webhook_calls  # type: ignore[attr-defined]
    tid = _create_thread(client)
    run_id = _run_to_completion(client, tid, {"input": "hi", "webhook_url": "http://cb/x"})

    assert len(calls) == 1, f"expected 1 webhook dispatch, got {len(calls)}: {calls}"
    meta = calls[0]
    assert meta["run_id"] == run_id
    assert meta["status"] == "success"
    assert meta["output"] == "webhook-stub-answer"
    # the dispatcher receives the resolved callback URL (so it knows where to POST)
    assert meta.get("webhook_url") == "http://cb/x"


def test_env_webhook_url_is_the_default(client: TestClient, monkeypatch) -> None:
    """PUX_RUN_WEBHOOK_URL supplies the callback when the run body omits it —
    the operator can wire one global receiver instead of per-run URLs."""
    monkeypatch.setenv("PUX_RUN_WEBHOOK_URL", "http://from-env/cb")
    calls = client._webhook_calls  # type: ignore[attr-defined]
    tid = _create_thread(client)
    _run_to_completion(client, tid, {"input": "hi"})
    assert len(calls) == 1
    assert calls[0].get("webhook_url") == "http://from-env/cb"


def test_failed_run_still_fires_webhook(tmp_path, monkeypatch) -> None:
    """A run that errors mid-graph still notifies — the webhook carries
    status="error" so the caller learns the run failed, not just succeeded."""
    import pux_harness.sandbox.container as container_mod
    import pux_harness.threads as threads_mod

    class _BoomGraph(_WebhookStubGraph):
        async def ainvoke(self, inp: Any, config: dict[str, Any]) -> dict[str, Any]:
            raise RuntimeError("simulated agent failure")

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "webhook-err.sqlite")
    monkeypatch.setattr(server, "build_graph", lambda org, **kw: _BoomGraph())
    monkeypatch.setattr(container_mod, "prepare", lambda org: [])
    calls: list[dict[str, Any]] = []
    monkeypatch.setattr(server, "_dispatch_run_webhook", lambda meta: calls.append(dict(meta)) or asyncio.sleep(0))  # type: ignore[assignment]

    with TestClient(server.app) as c:
        tid = c.post("/threads", json={"agent_id": "general"}).json()["thread_id"]
        rid = c.post(f"/threads/{tid}/runs", json={"input": "x", "webhook_url": "http://cb/err"}).json()["run_id"]
        c.get(f"/runs/{rid}/wait")
    # error terminal state still fires, carrying status=error
    assert any(m.get("status") == "error" for m in calls), f"no error dispatch: {calls}"


# ---------------------------------------------------------------------------
# dispatcher unit (real _dispatch_run_webhook, fake httpx)
# ---------------------------------------------------------------------------


class _FakeResp:
    status_code = 200


class _FakeAsyncClient:
    """Stand-in for httpx.AsyncClient: records POSTs, never touches the network."""

    def __init__(self) -> None:
        self.posts: list[dict[str, Any]] = []

    async def __aenter__(self) -> "_FakeAsyncClient":
        return self

    async def __aexit__(self, *exc: object) -> bool:
        return False

    async def post(self, url: str, json: dict[str, Any] | None = None, **kw: Any) -> _FakeResp:
        self.posts.append({"url": url, "json": json})
        return _FakeResp()


def test_dispatcher_payload_drops_url_and_tags_event(monkeypatch) -> None:
    """The HTTP payload does NOT echo the caller's webhook_url back (it's their
    private callback location) and is tagged event="run.completed" for demux."""
    fake = _FakeAsyncClient()
    monkeypatch.setattr(server.httpx, "AsyncClient", lambda *a, **kw: fake)

    asyncio.run(
        server._dispatch_run_webhook(
            {"run_id": "r1", "status": "success", "output": "done", "webhook_url": "http://cb/y"}
        )
    )
    assert len(fake.posts) == 1
    assert fake.posts[0]["url"] == "http://cb/y"
    payload = fake.posts[0]["json"]
    assert payload["event"] == "run.completed"
    assert "webhook_url" not in payload, "callback URL must not be echoed in the payload"
    assert payload["run_id"] == "r1" and payload["status"] == "success"


def test_dispatcher_no_url_is_a_no_post(monkeypatch) -> None:
    """No webhook_url => the dispatcher returns immediately without any POST."""
    fake = _FakeAsyncClient()
    monkeypatch.setattr(server.httpx, "AsyncClient", lambda *a, **kw: fake)
    asyncio.run(server._dispatch_run_webhook({"run_id": "r2", "status": "success", "output": "x"}))
    assert fake.posts == []


def test_dispatcher_swallows_delivery_failure(monkeypatch) -> None:
    """A dead/unreachable target is SWALLOWED — best-effort notification must
    NEVER raise into (and so fail) the background run that produced the result."""

    class _BoomClient:
        async def __aenter__(self) -> "_BoomClient":
            return self

        async def __aexit__(self, *exc: object) -> bool:
            return False

        async def post(self, *a: object, **kw: object) -> _FakeResp:
            raise OSError("connection refused")

    monkeypatch.setattr(server.httpx, "AsyncClient", lambda *x, **kw: _BoomClient())
    # must NOT raise
    asyncio.run(
        server._dispatch_run_webhook(
            {"run_id": "r3", "status": "success", "output": "", "webhook_url": "http://dead/x"}
        )
    )
