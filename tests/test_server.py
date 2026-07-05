"""Tests for the Agent Protocol server (Phase 4).

Two tiers, mirroring ``test_org_contract.py``:

* **Pure helpers** (no server, no tokens, no Docker): ``_normalize_input``,
  ``_final_answer``, ``_status_from_snapshot``, ``_jsonable`` — the input
  coercion, answer extraction, and snapshot→status logic the endpoints rely on.
* **HTTP routing** (FastAPI ``TestClient``, stub graph — no tokens, no Docker):
  proves the REST envelope + thread/run CRUD plumbing is correct by
  monkeypatching ``build_graph`` with a deterministic stub. The real LLM-driven
  run is proven end-to-end in the Phase 4 verify log (``/runs/wait`` general →
  9 Go files); these tests lock the contract that run exercised.
"""
from __future__ import annotations

from types import SimpleNamespace
from typing import Any

import pytest
from fastapi.testclient import TestClient
from langchain_core.messages import AIMessage, HumanMessage

from pux_harness import server


# --- pure helpers -------------------------------------------------------------


def test_normalize_input_string_to_messages() -> None:
    out = server._normalize_input("hello")
    assert out == {"messages": [{"role": "user", "content": "hello"}]}


def test_normalize_input_passthrough_messages_dict() -> None:
    payload = {"messages": [{"role": "user", "content": "hi"}]}
    assert server._normalize_input(payload) is payload


def test_normalize_input_none_rejected() -> None:
    from fastapi import HTTPException

    with pytest.raises(HTTPException) as exc:
        server._normalize_input(None)
    assert exc.value.status_code == 422


def test_normalize_input_arbitrary_dict_jsonified() -> None:
    out = server._normalize_input({"foo": 1})
    assert out["messages"][0]["role"] == "user"
    assert "foo" in out["messages"][0]["content"]


def test_final_answer_extracts_last_message_content() -> None:
    result = {"messages": [HumanMessage("q"), AIMessage("the answer")]}
    assert server._final_answer(result) == "the answer"


def test_final_answer_empty() -> None:
    assert server._final_answer({}) == ""
    assert server._final_answer({"messages": []}) == ""


def test_final_answer_multimodal_blocks_joined() -> None:
    result = {"messages": [AIMessage(content=[{"type": "text", "text": "a"}, "b"])]}
    assert server._final_answer(result) == "a\nb"


def test_status_from_snapshot_states() -> None:
    # finished: messages, no pending tool calls, no next
    snap = SimpleNamespace(
        next=(), values={"messages": [AIMessage("done")]}
    )
    assert server._status_from_snapshot(snap) == "finished"
    # interrupted: next is non-empty
    assert server._status_from_snapshot(SimpleNamespace(next=("x",), values={})) == "interrupted"
    # interrupted: last message still has tool calls
    snap_tc = SimpleNamespace(next=(), values={"messages": [SimpleNamespace(tool_calls=[1])]})
    assert server._status_from_snapshot(snap_tc) == "interrupted"
    # idle: no messages at all
    assert server._status_from_snapshot(SimpleNamespace(next=(), values={})) == "idle"


def test_jsonable_handles_scalars_lists_dicts() -> None:
    assert server._jsonable(None) is None
    assert server._jsonable(3) == 3
    assert server._jsonable([1, "x"]) == [1, "x"]
    assert server._jsonable({"a": 1}) == {"a": 1}


def test_jsonable_coerces_langchain_message() -> None:
    out = server._jsonable(AIMessage("hi"))
    assert out["role"] == "ai"
    assert out["content"] == "hi"
    assert out["tool_calls"] is None


# --- HTTP routing with a stub graph -------------------------------------------


class _StubSnapshot:
    def __init__(self, values: dict[str, Any], nxt: tuple[str, ...] = ()) -> None:
        self.values = values
        self.next = nxt
        self.config = {"configurable": {"checkpoint_id": "cp-1"}}
        self.parent_config = None


class _StubGraph:
    """Deterministic stand-in for a compiled org graph. Records invocations so
    routing tests can assert the server fed it the right thread_id/input."""

    def __init__(self) -> None:
        self.invoked: list[tuple[dict[str, Any], dict[str, Any]]] = []
        self._state = _StubSnapshot({})
        # copilotkit's LangGraphAGUIAgent walks ``graph.nodes.items()`` at
        # construction to find declared subgraphs; expose an empty node map so
        # the stub satisfies the compiled-graph contract without subgraphs.
        self.nodes: dict[str, Any] = {}

    async def ainvoke(self, inp: dict[str, Any], config: dict[str, Any]) -> dict[str, Any]:
        self.invoked.append((inp, config))
        # echo a finished conversation
        return {"messages": [HumanMessage(inp["messages"][0]["content"]), AIMessage("stub-answer")]}

    async def aget_state(self, config: dict[str, Any]) -> _StubSnapshot:
        self._state = _StubSnapshot(
            {"messages": [AIMessage("stub-state")]}, nxt=()
        )
        return self._state

    async def aget_state_history(self, config: dict[str, Any]):  # async generator
        for cp in ("cp-1", "cp-0"):
            snap = _StubSnapshot({"messages": [AIMessage("rev")]})
            snap.config = {"configurable": {"checkpoint_id": cp}}
            yield snap


@pytest.fixture
def client(tmp_path, monkeypatch) -> TestClient:
    # isolate the DB so tests don't touch the operator's .pux/ store.
    # NB: patch the module attribute, not the env var — PUX_API_DB is read at
    # import time, so setenv here would be too late.
    monkeypatch.setattr(server, "PUX_API_DB", tmp_path / "test.sqlite")
    monkeypatch.setattr(server, "build_graph", lambda org, **kw: _StubGraph())
    # TestClient as a context manager runs the lifespan (opens saver + db)
    with TestClient(server.app) as c:
        yield c


def test_health_lists_orgs(client: TestClient) -> None:
    r = client.get("/ok")
    assert r.status_code == 200
    body = r.json()
    assert body["ok"] is True
    assert "general" in body["orgs"]


def test_agents_search_returns_descriptors(client: TestClient) -> None:
    agents = client.post("/agents/search").json()
    ids = {a["agent_id"] for a in agents}
    assert "general" in ids and "_demo" in ids
    assert all("specialists" in a["metadata"] for a in agents)


def test_agent_get_unknown_404(client: TestClient) -> None:
    assert client.get("/agents/nope").status_code == 404


def test_thread_create_get_delete(client: TestClient) -> None:
    # create
    t = client.post("/threads", json={"agent_id": "general"}).json()
    tid = t["thread_id"]
    assert t["status"] == "idle"
    # get state
    st = client.get(f"/threads/{tid}").json()
    assert st["agent_id"] == "general"
    # delete + confirm gone
    assert client.delete(f"/threads/{tid}").json()["deleted"] is True
    assert client.get(f"/threads/{tid}").status_code == 404


def test_thread_create_unknown_agent_404(client: TestClient) -> None:
    assert client.post("/threads", json={"agent_id": "nope"}).status_code == 404


def test_thread_history_returns_revisions(client: TestClient) -> None:
    tid = client.post("/threads", json={"agent_id": "general"}).json()["thread_id"]
    hist = client.get(f"/threads/{tid}/history").json()
    assert len(hist) == 2
    assert all("checkpoint_id" in h for h in hist)


def test_threads_search_filters_by_agent(client: TestClient) -> None:
    g = client.post("/threads", json={"agent_id": "general"}).json()["thread_id"]
    d = client.post("/threads", json={"agent_id": "_demo"}).json()["thread_id"]
    only_general = client.post("/threads/search", json={"agent_id": "general"}).json()
    assert {t["thread_id"] for t in only_general} == {g}
    assert d not in {t["thread_id"] for t in only_general}


def test_run_ephemeral_executes_stub_and_returns_answer(client: TestClient) -> None:
    res = client.post(
        "/runs/wait", json={"agent_id": "general", "input": "do the thing"}
    ).json()
    assert res["status"] == "success"
    assert res["output"] == "stub-answer"
    assert res["thread_id"]
    # the stub recorded the invocation with the right thread_id + normalized input
    # (graph was built once + cached; pull it from app state)
    graph = server.app.state.graphs["general"]
    assert graph.invoked, "stub graph was not invoked"
    inp, cfg = graph.invoked[-1]
    assert inp == {"messages": [{"role": "user", "content": "do the thing"}]}
    assert cfg["configurable"]["thread_id"] == res["thread_id"]


def test_run_ephemeral_unknown_agent_404(client: TestClient) -> None:
    assert client.post(
        "/runs/wait", json={"agent_id": "nope", "input": "x"}
    ).status_code == 404


def test_thread_background_run_wait_cancel(client: TestClient) -> None:
    tid = client.post("/threads", json={"agent_id": "general"}).json()["thread_id"]
    run = client.post(f"/threads/{tid}/runs", json={"input": "go"}).json()
    rid = run["run_id"]
    # wait for it (stub resolves immediately)
    done = client.get(f"/runs/{rid}/wait").json()
    assert done["status"] == "success"
    assert done["output"] == "stub-answer"
    # listed under the thread
    runs = client.get(f"/threads/{tid}/runs").json()
    assert len(runs) == 1 and runs[0]["run_id"] == rid


def test_run_wait_unknown_run_404(client: TestClient) -> None:
    assert client.get("/runs/nope/wait").status_code == 404


def test_require_thread_unknown_returns_404(client: TestClient) -> None:
    # operations on a thread that was never registered
    assert client.get("/threads/never-made").status_code == 404
    assert client.get("/threads/never-made/history").status_code == 404
    assert client.post("/threads/never-made/runs", json={"input": "x"}).status_code == 404
