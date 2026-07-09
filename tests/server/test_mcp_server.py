"""Tests for the Pux MCP server (`pux_harness.mcp_server`).

Drives the real FastMCP tool dispatch via the in-memory `Client(mcp)`, with the
HTTP backend faked by an `httpx.MockTransport` injected into `mcp_server._client`.
This proves tool → Agent-Protocol-REST translation (method/path/body) and that
every tool's return text carries the right fields — without a live Agent Protocol
server (prod: `aegra serve`; dev: `langgraph dev` / `aegra dev`).
"""
from __future__ import annotations

import asyncio
import json
from typing import Any

import httpx
import pytest
from fastmcp import Client

from pux_harness import mcp_server


# --- mock Agent Protocol backend ---------------------------------------------

class MockAPI:
    """Records every request; returns canned responses keyed by (method, path)."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, str, Any]] = []
        self.routes: dict[tuple[str, str], tuple[int, Any]] = {}

    def route(self, method: str, path: str, *, status: int = 200, payload: Any = None) -> None:
        self.routes[(method, path)] = (status, payload)

    def find(self, method: str, path: str) -> Any:
        """Return the parsed JSON body of the first matching request, or None."""
        for m, p, body in self.calls:
            if m == method and p == path:
                return body
        return None

    def handler(self, request: httpx.Request) -> httpx.Response:
        body: Any = None
        if request.content:
            try:
                body = json.loads(request.content)
            except Exception:  # noqa: BLE001
                body = None
        self.calls.append((request.method, request.url.path, body))
        key = (request.method, request.url.path)
        if key in self.routes:
            status, payload = self.routes[key]
            if status >= 400:
                return httpx.Response(status, json={"detail": payload})
            return httpx.Response(status, json=payload)
        return httpx.Response(404, json={"detail": f"unmocked {request.method} {request.url.path}"})


@pytest.fixture
def api(monkeypatch):
    api = MockAPI()
    client = httpx.AsyncClient(transport=httpx.MockTransport(api.handler), base_url="http://test")
    monkeypatch.setattr(mcp_server, "_client", client)
    yield api


@pytest.fixture
def down_api(monkeypatch):
    """A backend that refuses every connection — simulates the Agent Protocol
    server being down (no `aegra serve` / `langgraph dev` running)."""
    def raise_handler(_request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("[Errno 111] Connection refused")
    client = httpx.AsyncClient(transport=httpx.MockTransport(raise_handler), base_url="http://test")
    monkeypatch.setattr(mcp_server, "_client", client)


# --- helpers -----------------------------------------------------------------

async def _call(name: str, **args: Any):
    async with Client(mcp_server.mcp) as c:
        return await c.call_tool(name, args)


def run(coro):
    return asyncio.run(coro)


# --- discovery ---------------------------------------------------------------

def test_health_ok(api):
    api.route("GET", "/events/health", payload={"ok": True})
    api.route("POST", "/assistants/search",
              payload=[{"graph_id": "general"}, {"graph_id": "invest"}])
    r = run(_call("health"))
    assert r.is_error is False
    assert r.data == "ok — backend up; 2 org(s): general, invest"


def test_health_backend_down(down_api):
    r = run(_call("health"))
    # Returned as in-band text (not a raised error), so is_error stays False.
    assert r.is_error is False
    assert r.data.startswith("ERROR:")
    assert "Agent Protocol server" in r.data  # was "pux serve" (retired in phase D)


def test_list_orgs_enriched(api):
    api.route(
        "POST", "/agents/search",
        payload=[  # real /agents/search returns a BARE list, not {"agents":[...]}
            {"agent_id": "invest", "name": "invest",
             "description": "Pux org 'invest'; specialists: analyst, trader",
             "metadata": {"specialists": ["analyst", "trainer"]}},
            {"agent_id": "general", "name": "general",
             "description": "Pux org 'general'; specialists: (none)",
             "metadata": {"specialists": []}},
        ],
    )
    r = run(_call("list_orgs"))
    assert "**invest**" in r.data
    assert "analyst" in r.data
    assert "(no subagents)" in r.data  # general's empty specialist list
    # correct REST body
    assert api.find("POST", "/agents/search") == {"metadata": {}, "page": 1}


def test_get_org(api):
    api.route(
        "GET", "/agents/invest",
        payload={"agent_id": "invest", "name": "invest",
                 "description": "Pux org 'invest'; specialists: analyst, trader",
                 "metadata": {"specialists": ["analyst", "trader"]}},
    )
    r = run(_call("get_org", org="invest"))
    assert "invest" in r.data
    assert "analyst, trader" in r.data


# --- execution: blocking -----------------------------------------------------

def test_run_agent_happy_path(api):
    # real /runs/wait returns a run-meta dict (status/output/error), not {messages:[...]}
    api.route(
        "POST", "/runs/wait",
        payload={"thread_id": "T", "agent_id": "invest",
                 "status": "success", "output": "the answer is 42"},
    )
    r = run(_call("run_agent", task="what is the answer", org="invest"))
    assert r.data == "the answer is 42"  # bare answer text, not a JSON status blob
    body = api.find("POST", "/runs/wait")
    assert body["agent_id"] == "invest"
    assert body["input"] == "what is the answer"
    assert body["recursion_limit"] == 60
    assert "thread_id" not in body  # not added when omitted


def test_run_agent_carries_thread_and_recursion(api):
    api.route("POST", "/runs/wait",
              payload={"thread_id": "T1", "agent_id": "general",
                       "status": "success", "output": "ok"})
    run(_call("run_agent", task="more", org="general", thread_id="T1", recursion_limit=25))
    body = api.find("POST", "/runs/wait")
    assert body["thread_id"] == "T1"
    assert body["recursion_limit"] == 25


def test_run_agent_error_status_surfaces_ERROR(api):
    # /runs/wait catches model failures into status=error
    api.route("POST", "/runs/wait",
              payload={"thread_id": "T", "agent_id": "general",
                       "status": "error", "output": "", "error": "model 401"})
    r = run(_call("run_agent", task="x", org="general"))
    assert r.data.startswith("ERROR:")
    assert "model 401" in r.data


def test_run_agent_unknown_org_error_text(api):
    api.route("POST", "/runs/wait", status=404, payload="unknown agent 'foo'")
    r = run(_call("run_agent", task="x", org="foo"))
    assert r.data.startswith("ERROR:")
    assert "unknown agent" in r.data


# --- execution: async lifecycle ----------------------------------------------

def test_start_run_body_and_pending(api):
    api.route(
        "POST", "/threads/T1/runs",
        payload={"run_id": "R1", "thread_id": "T1", "agent_id": "general",
                 "status": "pending", "started_at": "2026-07-05T00:00:00Z",
                 "output": "", "error": None},
    )
    r = run(_call("start_run", thread_id="T1", task="do a long thing"))
    assert "R1" in r.data and "pending" in r.data
    body = api.find("POST", "/threads/T1/runs")
    assert body == {"input": "do a long thing", "recursion_limit": 60}


def test_wait_for_run_surfaces_output_error_warnings(api):
    api.route(
        "GET", "/runs/R1/wait",
        payload={"run_id": "R1", "status": "success", "output": "done",
                 "error": None, "warnings": ["job fetch: failed"]},
    )
    r = run(_call("wait_for_run", run_id="R1"))
    assert "R1" in r.data
    assert "success" in r.data
    assert "output: done" in r.data
    assert "warnings:" in r.data and "job fetch: failed" in r.data  # not swallowed


def test_wait_for_run_error_status(api):
    api.route(
        "GET", "/runs/R2/wait",
        payload={"run_id": "R2", "status": "error", "output": "", "error": "boom"},
    )
    r = run(_call("wait_for_run", run_id="R2"))
    assert "error" in r.data and "boom" in r.data


def test_list_runs_non_blocking(api):
    api.route(
        "GET", "/threads/T1/runs",
        payload=[
            {"run_id": "R1", "status": "success", "output": "a", "error": None},
            {"run_id": "R2", "status": "running", "output": "", "error": None},
        ],
    )
    r = run(_call("list_runs", thread_id="T1"))
    assert "R1" in r.data and "R2" in r.data
    assert "success" in r.data and "running" in r.data


def test_cancel_run(api):
    api.route(
        "POST", "/runs/R1/cancel",
        payload={"run_id": "R1", "status": "cancelled", "output": "", "error": None},
    )
    r = run(_call("cancel_run", run_id="R1"))
    assert "R1" in r.data and "cancelled" in r.data


async def _compose(api):
    async with Client(mcp_server.mcp) as c:
        api.route("POST", "/threads", payload={"thread_id": "T9", "agent_id": "general", "metadata": {}})
        t = await c.call_tool("create_thread", {"org": "general"})
        api.route("POST", "/threads/T9/runs",
                  payload={"run_id": "R9", "thread_id": "T9", "status": "pending",
                           "output": "", "error": None})
        s = await c.call_tool("start_run", {"thread_id": "T9", "task": "go"})
        api.route("GET", "/threads/T9/runs",
                  payload=[{"run_id": "R9", "status": "success", "output": "done", "error": None}])
        lst = await c.call_tool("list_runs", {"thread_id": "T9"})
        return t, s, lst


def test_create_thread_start_run_list_runs_composition(api):
    t, s, lst = run(_compose(api))
    assert t.data == "T9"
    assert "R9" in s.data and "pending" in s.data
    assert "success" in lst.data and "done" in lst.data
    # bodies prove the wiring
    assert api.find("POST", "/threads") == {"agent_id": "general", "metadata": {}}
    assert api.find("POST", "/threads/T9/runs") == {"input": "go", "recursion_limit": 60}


# --- threads (read) ----------------------------------------------------------

def test_get_thread_renders_messages(api):
    api.route(
        "GET", "/threads/T1",
        payload={"values": {"messages": [
            {"role": "user", "content": "hi"},
            {"role": "assistant", "content": [{"type": "text", "text": "hello there"}]},
        ]}},
    )
    r = run(_call("get_thread", thread_id="T1"))
    assert "**user**: hi" in r.data
    assert "**assistant**: hello there" in r.data  # multimodal block flattened


def test_get_thread_empty(api):
    api.route("GET", "/threads/T1", payload={"values": {}})
    r = run(_call("get_thread", thread_id="T1"))
    assert r.data == "Thread is empty."


def test_list_threads(api):
    api.route(
        "POST", "/threads/search",
        payload=[  # real /threads/search returns a BARE list, not {"threads":[...]}
            {"thread_id": "T1", "agent_id": "invest", "metadata": {}, "created_at": "2026-07-05T01:02:03Z"},
        ],
    )
    r = run(_call("list_threads"))
    assert "T1" in r.data and "invest" in r.data
    # agent_id is read from the top level, not metadata
    assert "2026-07-05T01:02:03" in r.data


def test_delete_thread(api):
    api.route("DELETE", "/threads/T1", payload={})
    r = run(_call("delete_thread", thread_id="T1"))
    assert "T1" in r.data and "deleted" in r.data
    assert any(m == "DELETE" and p == "/threads/T1" for m, p, _ in api.calls)


# --- jobs --------------------------------------------------------------------

def test_run_jobs(api):
    api.route(
        "POST", "/jobs/invest/run",
        payload={"org": "invest", "jobs": [
            {"name": "fetch", "status": "ok", "error": None, "duration": 1.2},
            {"name": "embed", "status": "error", "error": "oom", "duration": 0.5},
        ]},
    )
    r = run(_call("run_jobs", org="invest"))
    assert "fetch: ok" in r.data and "[1.2s]" in r.data
    assert "embed: error (oom)" in r.data
    assert api.find("POST", "/jobs/invest/run") == {}


def test_run_jobs_single_job_filter(api):
    api.route("POST", "/jobs/invest/run",
              payload={"org": "invest", "jobs": [{"name": "fetch", "status": "ok", "error": None, "duration": 1.0}]})
    run(_call("run_jobs", org="invest", job="fetch"))
    assert api.find("POST", "/jobs/invest/run") == {"job": "fetch"}


def test_get_jobs_declared(api):
    api.route(
        "GET", "/jobs/invest/status",
        payload={"org": "invest", "jobs": [
            {"name": "fetch", "script": "fetch.py", "args": [], "timeout": 120, "description": "pull corpus"},
        ]},
    )
    r = run(_call("get_jobs", org="invest"))
    assert "fetch" in r.data and "pull corpus" in r.data


def test_get_jobs_no_policy(api):
    api.route("GET", "/jobs/general/status", payload={"org": "general", "jobs": [], "message": "no policy.yaml"})
    r = run(_call("get_jobs", org="general"))
    assert "no policy.yaml" in r.data


# --- transport: singleton reuse + connect error ------------------------------

async def _two_calls():
    async with Client(mcp_server.mcp) as c:
        a = await c.call_tool("health", {})
        b = await c.call_tool("health", {})
        return a, b


def test_singleton_client_reused(api):
    api.route("GET", "/events/health", payload={"ok": True})
    api.route("POST", "/assistants/search", payload=[{"graph_id": "general"}])
    client_before = mcp_server._get_client()
    a, b = run(_two_calls())
    # Same AsyncClient instance served both calls — no per-call client churn.
    assert mcp_server._get_client() is client_before
    assert a.data == b.data == "ok — backend up; 1 org(s): general"
    # 2 tool calls, each doing GET /events/health + POST /assistants/search = 2 of
    # EACH (4 requests total), all over the SAME client — proves reuse, no churn.
    assert len([c for c in api.calls if c[0] == "GET" and c[1] == "/events/health"]) == 2
    assert len([c for c in api.calls if c[0] == "POST" and c[1] == "/assistants/search"]) == 2


def test_unknown_run_id_404_error_text(api):
    api.route("GET", "/runs/missing/wait", status=404, payload="unknown run 'missing'")
    r = run(_call("wait_for_run", run_id="missing"))
    assert r.data.startswith("ERROR:")
    assert "unknown run" in r.data
