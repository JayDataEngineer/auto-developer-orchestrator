"""Live E2E for the Pux MCP server.

Drives the REAL MCP server (`pux mcp`, SSE :9987) talking to the REAL Agent
Protocol server (`pux serve`, :9988) over the network — the same transport
Hermes/OpenClaw use. Closes the [[prepare-wiring-e2e-gap]]: the MockTransport
unit tests prove tool→REST translation; this proves the live wiring (real SSE
transport, real FastAPI routes, real sqlite persistence).

Skipped unless ``PUX_E2E=1`` — start both servers first:

    pux serve &            # FastAPI on :9988
    pux mcp &              # FastMCP SSE on :9987
    PUX_E2E=1 uv run pytest tests/integration/test_mcp_server_e2e.py -q

Covers discovery, threads, jobs-status, and the async-lifecycle plumbing, PLUS
``run_agent`` returning REAL model output when ``OPENCODE_API_KEY`` is present
(the server loads it from ``.env`` via ``bin/pux``). The model-less tests
tolerate a run ending in ``error``; the model-path test asserts a real answer.
"""
from __future__ import annotations

import asyncio
import os

import pytest
from fastmcp import Client

PUX_MCP_URL = os.environ.get("PUX_MCP_URL", "http://127.0.0.1:9987/sse")
KNOWN_STATUSES = {"pending", "running", "success", "error", "cancelled"}

pytestmark = pytest.mark.skipif(
    os.environ.get("PUX_E2E") != "1",
    reason="set PUX_E2E=1 and start `pux serve` + `pux mcp` to run live E2E",
)


def run(coro):
    return asyncio.run(coro)


@pytest.fixture(scope="module")
def client():
    return Client(PUX_MCP_URL)


@pytest.fixture(scope="module")
def org_names(client):
    async def _health():
        async with client as c:
            return await c.call_tool("health", {})
    data = run(_health())
    # "ok — backend up; N org(s): a, b, c"
    return data.data.split("org(s): ", 1)[1].split(", ")


def test_health_and_list_orgs_agree(client, org_names):
    async def _go():
        async with client as c:
            h = await c.call_tool("health", {})
            lo = await c.call_tool("list_orgs", {})
            return h, lo
    h, lo = run(_go())
    assert h.data.startswith("ok — backend up;")
    assert len(org_names) >= 1
    for n in org_names:
        assert f"**{n}**" in lo.data, f"org {n} missing from list_orgs output"


def test_get_org_returns_real_specialists(client, org_names):
    async def _go():
        async with client as c:
            return await c.call_tool("get_org", {"org": org_names[0]})
    r = run(_go())
    assert org_names[0] in r.data
    assert "specialists:" in r.data
    assert "description:" in r.data


def test_thread_lifecycle_over_sse(client):
    async def _go():
        async with client as c:
            created = await c.call_tool("create_thread", {"org": "general"})
            tid = created.data
            gt = await c.call_tool("get_thread", {"thread_id": tid})
            listed = await c.call_tool("list_threads", {"org": "general"})
            hist = await c.call_tool("get_thread_history", {"thread_id": tid})
            deleted = await c.call_tool("delete_thread", {"thread_id": tid})
            return tid, gt, listed, hist, deleted
    tid, gt, listed, hist, deleted = run(_go())
    assert tid
    assert gt.data == "Thread is empty."           # fresh thread
    assert tid in listed.data                       # persisted + searchable
    assert hist.data == "No history."               # no runs yet
    assert "deleted" in deleted.data.lower()


def test_get_jobs_real_policy(client, org_names):
    async def _go():
        async with client as c:
            return await c.call_tool("get_jobs", {"org": org_names[0]})
    r = run(_go())
    assert r.data.startswith(f"org `{org_names[0]}`")   # declared jobs OR a clear message


def test_run_agent_real_model_output(client):
    """Proves the full model path: run_agent → /runs/wait → real MiMo-V2.5 →
    bare answer text (NOT a JSON status blob — the bug _extract_run_output fixes).
    Requires the running `pux serve` to have a live OPENCODE_API_KEY (bin/pux
    auto-loads .env)."""
    async def _go():
        async with client as c:
            return await c.call_tool("run_agent", {
                "task": "Reply with exactly one word: pong",
                "org": "general",
            })
    r = run(_go())
    assert r.is_error is False
    # Bare answer text, not a dumped run-meta dict (regression guard).
    assert not r.data.lstrip().startswith("{"), f"got status blob, not answer: {r.data!r}"
    assert "pong" in r.data.lower(), f"expected 'pong' in real model output: {r.data!r}"


def test_async_lifecycle_success_path(client):
    """create_thread → start_run → wait_for_run completes with a REAL success
    status and non-empty model output (not just the plumbing)."""
    async def _go():
        async with client as c:
            created = await c.call_tool("create_thread", {"org": "general"})
            tid = created.data
            started = await c.call_tool(
                "start_run", {"thread_id": tid, "task": "Reply with exactly one word: ack"})
            run_id = started.data.split("run `", 1)[1].split("`", 1)[0]
            waited = await c.call_tool("wait_for_run", {"run_id": run_id})
            await c.call_tool("delete_thread", {"thread_id": tid})
            return started, waited
    started, waited = run(_go())
    assert "success" in waited.data, f"wait_for_run not success: {waited.data!r}"
    # wait_for_run output line carries the real model answer
    assert "output:" in waited.data
    out = [ln for ln in waited.data.splitlines() if ln.startswith("output:")]
    assert out and len(out[0]) > len("output: "), f"empty output: {waited.data!r}"


def test_cancel_run_on_live_run(client):
    """cancel_run against a freshly-started run lands on a terminal status
    (cancelled OR success if it already finished — both prove the route works
    against a real run, not an unknown-id 404)."""
    async def _go():
        async with client as c:
            created = await c.call_tool("create_thread", {"org": "general"})
            tid = created.data
            # Heavy task → window to cancel before completion.
            started = await c.call_tool(
                "start_run", {"thread_id": tid,
                              "task": "Write a 500-word essay about the history of computing."})
            run_id = started.data.split("run `", 1)[1].split("`", 1)[0]
            cancelled = await c.call_tool("cancel_run", {"run_id": run_id})
            await c.call_tool("delete_thread", {"thread_id": tid})
            return cancelled
    cancelled = run(_go())
    status = cancelled.data.splitlines()[0].split(" — ", 1)[1].split()[0]
    assert status in {"cancelled", "success"}, f"unexpected cancel status: {status!r} ({cancelled.data!r})"


def test_async_lifecycle_plumbing(client):
    """start_run returns a run_id immediately; list_runs observes a real status.
    The run itself will likely end in `error` (no model configured here) — that's
    a legitimate real-world outcome and still proves the wiring through _run_task
    → run_meta."""
    async def _go():
        async with client as c:
            created = await c.call_tool("create_thread", {"org": "general"})
            tid = created.data
            started = await c.call_tool("start_run", {"thread_id": tid, "task": "e2e smoke"})
            runs = await c.call_tool("list_runs", {"thread_id": tid})
            await c.call_tool("delete_thread", {"thread_id": tid})
            return started, runs
    started, runs = run(_go())
    assert "run `" in started.data
    status = started.data.splitlines()[0].split(" — ", 1)[1].split()[0]
    assert status in KNOWN_STATUSES, f"unexpected start_run status: {status!r}"
    assert "run `" in runs.data
