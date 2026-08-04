"""Tests for the Pux MCP server (``pux_harness.mcp_server``).

The MCP server delegates to per-org ACP subprocesses (one ``pux`` process per
org, managed by ``OrgConnection``). These tests mock the pool layer
(``_get_org`` / ``_find_org_for_session``) so no real subprocess is spawned,
and drive the REAL FastMCP tool dispatch via the in-memory ``Client(MCP)``.

This proves each tool's input → OrgConnection method mapping + return text
shaping — the translation layer between the MCP tool call and the ACP
subagent lifecycle.
"""
from __future__ import annotations

import asyncio
from typing import Any

import pytest
from fastmcp import Client

from pux_harness import mcp_server


# ---------------------------------------------------------------------------
# Mock OrgConnection — records calls, returns canned responses
# ---------------------------------------------------------------------------

class MockOrgConnection:
    """Stand-in for OrgConnection that records every call and returns
    deterministic responses. No subprocess, no network."""

    def __init__(self, org: str = "general") -> None:
        self.org = org
        self.alive = True
        self.calls: list[tuple] = []
        self._next_session_id = "sess-abc123"
        self._prompt_result: tuple[str, str, str, list] = (
            "hello from agent", "thinking...", "end_turn", [],
        )
        self._sessions: list[dict] = []
        self._load_result = True

    async def new_session(self, model: str | None = None,
                           cwd: str | None = None) -> str:
        self.calls.append(("new_session", model, cwd))
        return self._next_session_id

    async def prompt(self, session_id: str,
                     message: str,
                     images: list | None = None) -> tuple[str, str, str, list]:
        self.calls.append(("prompt", session_id, message))
        return self._prompt_result

    async def list_sessions_raw(self) -> list[dict]:
        self.calls.append(("list_sessions_raw",))
        return self._sessions

    async def cancel(self, session_id: str) -> None:
        self.calls.append(("cancel", session_id))

    async def load(self, session_id: str) -> bool:
        self.calls.append(("load", session_id))
        return self._load_result

    async def set_model_raw(self, session_id: str, model: str) -> None:
        self.calls.append(("set_model_raw", session_id, model))

    async def stop(self) -> None:
        """Stand-in for OrgConnection.stop() — kills the cached ACP subprocess.
        reload_profiles() calls this to force a fresh process (that re-reads
        profile yaml) on the next new_session()."""
        self.calls.append(("stop",))
        self.alive = False


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def mock_conn():
    """A fresh MockOrgConnection per test (no cross-test state)."""
    return MockOrgConnection()


@pytest.fixture
def mock_pool(monkeypatch, mock_conn):
    """Patch the pool layer so no real subprocess is spawned.

    ``_get_org`` is async; ``_find_org_for_session`` is sync. Both are
    patched to return the same MockOrgConnection. Also clears the
    module-level ``_session_org`` / ``_pool`` dicts so tests are hermetic."""
    async def _mock_get_org(org: str) -> MockOrgConnection:
        mock_conn.org = org
        return mock_conn

    monkeypatch.setattr(mcp_server, "_get_org", _mock_get_org)
    monkeypatch.setattr(
        mcp_server, "_find_org_for_session",
        lambda sid: mock_conn,
    )
    mcp_server._session_org.clear()
    mcp_server._pool.clear()
    yield mock_conn
    mcp_server._session_org.clear()
    mcp_server._pool.clear()


@pytest.fixture
def no_connection(monkeypatch):
    """Patch _find_org_for_session to return None — simulates no active
    connection for a session_id (e.g. server restarted, session expired)."""
    monkeypatch.setattr(
        mcp_server, "_find_org_for_session", lambda sid: None,
    )


@pytest.fixture
def temp_staged(monkeypatch, tmp_path):
    """Redirect _STAGED_HOST_DIR and _DATA_HOST_DIR to a temp directory so
    tests don't pollute the real shared filesystem (data/staged/ is visible
    to Hermes via the workspace bind mount — test artifacts there show up as
    phantom images in production)."""
    fake_data = tmp_path / "data"
    fake_staged = fake_data / "staged"
    fake_staged.mkdir(parents=True)
    monkeypatch.setattr(mcp_server, "_STAGED_HOST_DIR", fake_staged)
    monkeypatch.setattr(mcp_server, "_DATA_HOST_DIR", fake_data)
    monkeypatch.setattr(mcp_server, "_STAGED_CONTAINER_DIR", "/tmp/fake_staged")
    return fake_data


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

async def _call(name: str, **args: Any):
    async with Client(mcp_server.MCP) as c:
        return await c.call_tool(name, args)


def run(coro):
    return asyncio.run(coro)


# ---------------------------------------------------------------------------
# new_session
# ---------------------------------------------------------------------------

def test_new_session_happy_path(mock_pool):
    r = run(_call("new_session", org="general"))
    assert r.is_error is False
    assert "sess-abc123" in r.content[0].text
    assert "general" in r.content[0].text
    # The OrgConnection.new_session was called
    assert mock_pool.calls[0][0] == "new_session"


def test_new_session_with_model_and_cwd(mock_pool):
    r = run(_call("new_session", org="general", model="glm-5.2",
                  cwd="/some/path"))
    assert r.is_error is False
    assert "model: glm-5.2" in r.content[0].text
    assert "cwd: /some/path" in r.content[0].text
    method, model, cwd = mock_pool.calls[0]
    assert model == "glm-5.2"
    assert cwd == "/some/path"


def test_new_session_unknown_org(mock_pool):
    r = run(_call("new_session", org="no-such-org"))
    assert "Error" in r.content[0].text
    assert "unknown org" in r.content[0].text


def test_new_session_registers_session_org(mock_pool):
    run(_call("new_session", org="general"))
    assert "sess-abc123" in mcp_server._session_org
    assert mcp_server._session_org["sess-abc123"] == "general"


# ---------------------------------------------------------------------------
# prompt
# ---------------------------------------------------------------------------

def test_prompt_happy_path(mock_pool):
    # Register the session first so _find_org_for_session can find it.
    mcp_server._session_org["sess-1"] = "general"
    r = run(_call("prompt", session_id="sess-1", message="hello"))
    assert r.is_error is False
    assert "hello from agent" in r.content[0].text
    assert "end_turn" in r.content[0].text
    method, sid, msg = mock_pool.calls[0]
    assert sid == "sess-1"
    assert msg == "hello"


def test_prompt_no_connection(no_connection):
    r = run(_call("prompt", session_id="orphan", message="hi"))
    assert "Error" in r.content[0].text
    assert "no active connection" in r.content[0].text


def test_prompt_cancelled_stop_reason(mock_pool):
    mock_pool._prompt_result = ("partial", "", "cancelled", [])
    mcp_server._session_org["sess-1"] = "general"
    r = run(_call("prompt", session_id="sess-1", message="go"))
    assert "cancelled" in r.content[0].text
    assert "Task cancelled" in r.content[0].text


def test_prompt_no_response_text(mock_pool):
    mock_pool._prompt_result = ("", "", "end_turn", [])
    mcp_server._session_org["sess-1"] = "general"
    r = run(_call("prompt", session_id="sess-1", message="go"))
    assert "(no response)" in r.content[0].text


def test_prompt_returns_agent_images_inline(mock_pool, temp_staged):
    """When the agent emits image content blocks, the MCP tool returns them
    as native ImageContent alongside the text — Hermes sees them inline."""
    # 1x1 red PNG
    png_b64 = (
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mP8z8BQDwAE"
        "hQGAhKmMIQAAAABJRU5ErkJggg=="
    )
    mock_pool._prompt_result = (
        "Here's the screenshot", "", "end_turn",
        [{"data": png_b64, "mime_type": "image/png"}],
    )
    mcp_server._session_org["sess-1"] = "general"
    r = run(_call("prompt", session_id="sess-1", message="take screenshot"))
    assert r.is_error is False
    # First content block is text
    assert "Here's the screenshot" in r.content[0].text
    assert "end_turn" in r.content[0].text
    assert "📸" in r.content[0].text  # image saved notification
    # Second content block is the image
    assert len(r.content) >= 2
    assert r.content[1].type == "image"
    assert r.content[1].data == png_b64
    assert r.content[1].mimeType == "image/png"


# ---------------------------------------------------------------------------
# list_sessions
# ---------------------------------------------------------------------------

# list_sessions behavior (store-backed, not pool-walking) is covered below by:
#   test_list_sessions_reads_store_not_pool
#   test_list_sessions_org_filter
#   test_list_sessions_empty_store_says_so_honestly
# The legacy versions of these walked the in-memory _pool, which lied ("No
# subagent sessions") on any fresh server despite 1000+ threads on disk.


# ---------------------------------------------------------------------------
# load_session
# ---------------------------------------------------------------------------

def test_load_session_found(mock_pool):
    r = run(_call("load_session", session_id="old-sess", org="general"))
    assert "resumed" in r.content[0].text
    assert "old-sess" in r.content[0].text
    method, sid = mock_pool.calls[0]
    assert sid == "old-sess"


def test_load_session_not_found(mock_pool):
    mock_pool._load_result = False
    r = run(_call("load_session", session_id="ghost", org="general"))
    assert "not found" in r.content[0].text


# ---------------------------------------------------------------------------
# cancel_session
# ---------------------------------------------------------------------------

def test_cancel_session_happy_path(mock_pool):
    mcp_server._session_org["sess-1"] = "general"
    r = run(_call("cancel_session", session_id="sess-1"))
    assert "Cancelled" in r.content[0].text
    assert "sess-1" in r.content[0].text
    method, sid = mock_pool.calls[0]
    assert sid == "sess-1"


def test_cancel_session_no_connection(no_connection):
    r = run(_call("cancel_session", session_id="orphan"))
    assert "Error" in r.content[0].text
    assert "no active connection" in r.content[0].text


# ---------------------------------------------------------------------------
# reset_session
# ---------------------------------------------------------------------------

def test_reset_session_resets_sandbox(mock_pool, monkeypatch):
    """Happy path: cancel the in-flight task, then force-reset the org's
    sandbox container. Patches SandboxContainer so no Docker is touched."""
    mock_pool.org = "coder"
    import pux_harness.sandbox.container as container_mod

    reset_called = {"v": False}

    class _FakeSB:
        def __init__(self, **kw):  # noqa: ANN204
            self.kw = kw

        def reset(self):  # noqa: ANN201
            reset_called["v"] = True

    monkeypatch.setattr(container_mod, "SandboxContainer", _FakeSB)

    r = run(_call("reset_session", session_id="sess-1"))

    assert r.is_error is False
    assert "reset" in r.content[0].text.lower()
    assert "coder" in r.content[0].text
    assert reset_called["v"] is True
    # cancel was issued first (hygiene before the force-remove)
    assert ("cancel", "sess-1") in mock_pool.calls


def test_reset_session_no_connection(no_connection):
    r = run(_call("reset_session", session_id="orphan"))
    assert "Error" in r.content[0].text
    assert "no active connection" in r.content[0].text


# ---------------------------------------------------------------------------
# set_model
# ---------------------------------------------------------------------------

def test_set_model_happy_path(mock_pool):
    mcp_server._session_org["sess-1"] = "general"
    r = run(_call("set_model", session_id="sess-1", model="glm-5.2"))
    assert "Model set to" in r.content[0].text
    assert "glm-5.2" in r.content[0].text
    method, sid, model = mock_pool.calls[0]
    assert model == "glm-5.2"


def test_set_model_no_connection(no_connection):
    r = run(_call("set_model", session_id="orphan", model="x"))
    assert "Error" in r.content[0].text
    assert "no active connection" in r.content[0].text


# ---------------------------------------------------------------------------
# stage_file + read_file (file round-trip — no org connection needed)
# ---------------------------------------------------------------------------

# A 1x1 red PNG.
_PNG_B64 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mP8z8BQDwAE"
    "hQGAhKmMIQAAAABJRU5ErkJggg=="
)


def test_stage_file_then_read_file_roundtrip(temp_staged):
    """stage_file writes bytes → read_file recovers them → base64 matches."""
    r = run(_call("stage_file", filename="rt.png", content_b64=_PNG_B64))
    assert r.is_error is False
    assert "Staged" in r.content[0].text
    assert "/tmp/fake_staged/rt.png" in r.content[0].text

    r2 = run(_call("read_file", path="staged/rt.png"))
    assert r2.is_error is False
    text = r2.content[0].text
    assert "Read" in text
    assert "image/png" in text
    assert _PNG_B64 in text  # the base64 payload is present


def test_stage_file_bad_filename(temp_staged):
    """Filenames with path separators or .. are rejected."""
    r = run(_call("stage_file", filename="../escape.png", content_b64=_PNG_B64))
    assert "Error" in r.content[0].text
    assert "unsafe filename" in r.content[0].text


def test_stage_file_bad_base64(temp_staged):
    r = run(_call("stage_file", filename="ok.png", content_b64="!!!not-b64!!!"))
    assert "Error" in r.content[0].text
    assert "invalid base64" in r.content[0].text


def test_read_file_path_traversal_blocked(temp_staged):
    r = run(_call("read_file", path="../../../etc/passwd"))
    assert "Error" in r.content[0].text
    assert "unsafe path" in r.content[0].text


def test_read_file_absolute_path_blocked(temp_staged):
    r = run(_call("read_file", path="/etc/passwd"))
    assert "Error" in r.content[0].text
    assert "unsafe path" in r.content[0].text


def test_read_file_nonexistent(temp_staged):
    r = run(_call("read_file", path="images/does-not-exist.jpg"))
    assert "Error" in r.content[0].text
    assert "not found" in r.content[0].text


# ---------------------------------------------------------------------------
# reload_profiles
# ---------------------------------------------------------------------------

def test_reload_profiles_single_org_bounces_only_that_subprocess(mock_pool):
    """reload_profiles(org=X) stops X's cached ACP subprocess so the next
    new_session re-reads profile.yaml/profile.local.yaml, and leaves other
    orgs' subprocesses untouched (isolation)."""
    coder_conn = MockOrgConnection("coder")
    twitter_conn = MockOrgConnection("twitter-agent")
    mcp_server._pool["coder"] = coder_conn
    mcp_server._pool["twitter-agent"] = twitter_conn

    r = run(_call("reload_profiles", org="coder"))

    assert r.is_error is False
    txt = r.content[0].text
    assert "coder" in txt
    assert "Reloaded profiles" in txt
    # coder bounced
    assert ("stop",) in coder_conn.calls
    assert coder_conn.alive is False
    # twitter-agent left alone
    assert ("stop",) not in twitter_conn.calls
    assert twitter_conn.alive is True


def test_reload_profiles_all_active_orgs_when_no_org_arg(mock_pool):
    """reload_profiles() with no arg bounces EVERY active org in the pool —
    the equivalent of a full server restart, scoped to the profile layer."""
    a = MockOrgConnection("coder")
    b = MockOrgConnection("twitter-agent")
    mcp_server._pool["coder"] = a
    mcp_server._pool["twitter-agent"] = b

    r = run(_call("reload_profiles"))

    assert r.is_error is False
    txt = r.content[0].text
    assert "coder" in txt
    assert "twitter-agent" in txt
    assert ("stop",) in a.calls
    assert ("stop",) in b.calls


def test_reload_profiles_unknown_org_reported_not_raised(mock_pool):
    """An org not in the pool is reported as skipped, not a hard error —
    operators can call reload_profiles speculatively without crashing."""
    mcp_server._pool["coder"] = MockOrgConnection("coder")

    r = run(_call("reload_profiles", org="nonexistent"))

    assert r.is_error is False
    txt = r.content[0].text.lower()
    assert "nonexistent" in txt or "not active" in txt


def test_reload_profiles_empty_pool_is_noop(mock_pool):
    """No active orgs → helpful 'No active orgs' message, no crash."""
    r = run(_call("reload_profiles"))
    assert r.is_error is False
    assert "No active orgs" in r.content[0].text


# ---------------------------------------------------------------------------
# deploy_browser_agent + browser_status (non-blocking fire + poll)
# ---------------------------------------------------------------------------

_PNG_1X1 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ"
    "xIsAAAAASUVORK5CYII="
)


@pytest.fixture(autouse=True)
def _clear_browser_tasks():
    """Hermeticity: clear the in-memory browser-task tracker before AND after
    each test so progress dicts from one test can't leak into another."""
    mcp_server._BROWSER_TASKS.clear()
    yield
    mcp_server._BROWSER_TASKS.clear()


def _extract_sid(text: str) -> str | None:
    """Pull the session_id out of a deploy_browser_agent response."""
    import re
    m = re.search(r"session_id: `([^`]+)`", text)
    return m.group(1) if m else None


async def _deploy_and_status(task: str) -> tuple:
    """Call deploy_browser_agent, drain the background task, then poll
    browser_status. Returns (deploy_result, sid, status_result)."""
    async with Client(mcp_server.MCP) as c:
        deploy_r = await c.call_tool("deploy_browser_agent", {"task": task})
        sid = _extract_sid(deploy_r.content[0].text)
        # Drain background tasks so the (synchronous mock) prompt completes
        # before we poll status. In production this is where the 30-90s of
        # real agent work would happen, pollable via browser_status.
        if mcp_server._BG_TASKS:
            await asyncio.gather(*mcp_server._BG_TASKS)
        status_r = await c.call_tool("browser_status", {"session_id": sid})
    return deploy_r, sid, status_r


def test_deploy_returns_immediately_with_session_id(mock_pool):
    """deploy_browser_agent is NON-BLOCKING — returns immediately with a
    session_id. The agent text is NOT in this response; it's in
    browser_status once state=done. This is the durable fix for the
    'browser agent isn't producing traffic' symptom: the outer agent
    gets a real handle to poll, not a 90s black-box wait."""
    mock_pool.org = "browser-agent"
    mock_pool._prompt_result = ("Page says: Welcome", "", "end_turn", [])

    r = run(_call("deploy_browser_agent", task="Go to https://example.com"))

    assert r.is_error is False
    txt = r.content[0].text
    assert "Browser task started" in txt
    sid = _extract_sid(txt)
    assert sid is not None, f"session_id missing from deploy response: {txt!r}"
    # The deploy response tells the caller exactly how to get the result.
    assert "browser_status" in txt
    # The agent text is NOT here — must poll browser_status.
    assert "Welcome" not in txt
    # new_session was called; prompt has not YET been called at this instant
    # (it runs in the background task).
    assert mock_pool.calls[0][0] == "new_session"


def test_browser_status_returns_result_when_done(mock_pool):
    """After the background task completes, browser_status returns the
    agent's text + stop_reason — the original synchronous payload, just
    deferred."""
    mock_pool.org = "browser-agent"
    mock_pool._prompt_result = ("Page says: Welcome to Example", "", "end_turn", [])

    deploy_r, sid, status_r = run(_deploy_and_status(
        "Go to https://example.com and summarize"
    ))

    assert status_r.is_error is False
    txt = status_r.content[0].text
    assert "done" in txt
    assert "Welcome to Example" in txt
    assert "[end_turn]" in txt
    # new_session + prompt were both called on the browser-agent org
    assert mock_pool.calls[0][0] == "new_session"
    assert mock_pool.calls[1][0] == "prompt"
    assert mock_pool.calls[1][1] == sid


def test_browser_status_returns_images_inline_when_done(mock_pool, temp_staged):
    """Screenshots flow back as native MCP image content blocks via
    browser_status (not the deploy call) — the original asset passthrough,
    preserved across the fire-and-status refactor."""
    mock_pool.org = "browser-agent"
    mock_pool._prompt_result = (
        "Here is the screenshot.", "", "end_turn",
        [{"data": _PNG_1X1, "mime_type": "image/png"}],
    )

    deploy_r, sid, status_r = run(_deploy_and_status(
        "screenshot https://example.com"
    ))

    # The deploy response has NO images — they come via status.
    assert all(b.type == "text" for b in deploy_r.content)

    # status_r delivers text + image when state=done.
    types = [b.type for b in status_r.content]
    assert "text" in types and "image" in types
    img_block = next(b for b in status_r.content if b.type == "image")
    assert img_block.data == _PNG_1X1
    assert img_block.mimeType == "image/png"
    # persisted to staged/ (temp_staged is the data/ dir; files land in staged/)
    staged = list((temp_staged / "staged").glob("browser_agent_*.png"))
    assert len(staged) == 1


def test_browser_status_reports_error_with_resume_hint(mock_pool):
    """A mid-turn crash sets state=error; browser_status returns the
    exception + a resume hint. The session_id is still durable — the
    conversation persists in the agent-protocol store."""
    mock_pool.org = "browser-agent"

    async def _boom_prompt(sid, message, images=None):
        raise RuntimeError("sandbox crashed mid-turn")
    mock_pool.prompt = _boom_prompt

    deploy_r, sid, status_r = run(_deploy_and_status("anything"))

    txt = status_r.content[0].text
    assert "error" in txt.lower()
    assert "sandbox crashed mid-turn" in txt
    # Resume recipe included.
    assert "load_session" in txt
    assert sid in txt


def test_deploy_infra_error_before_session_creation(mock_pool):
    """If _get_org or new_session fails BEFORE the session exists, deploy
    returns an error string (no sid, no background task). This is the only
    path that returns an error from deploy itself — mid-turn failures land
    in browser_status instead."""
    async def _boom(org):
        raise RuntimeError("sandbox refused to start")
    import pux_harness.mcp_server as ms
    ms._get_org = _boom  # noqa: SLF001

    r = run(_call("deploy_browser_agent", task="anything"))

    assert r.is_error is False
    txt = r.content[0].text
    assert "Error deploying browser-agent" in txt
    assert "no session was created" in txt
    assert "sandbox refused to start" in txt
    # No background task was registered.
    assert mcp_server._BROWSER_TASKS == {}


def test_browser_status_unknown_session_lists_active_tasks(mock_pool, monkeypatch):
    """When the sid isn't in the tracker (server restart, typo, or created
    via new_session), browser_status explains the mismatch and lists
    currently-running tasks so the caller can recover."""
    mock_pool.org = "browser-agent"

    # Slow mock prompt — keeps the bg task in 'running' state while we poll.
    async def _slow_prompt(session_id, message, images=None):
        await asyncio.sleep(0.05)
        return ("ok", "", "end_turn", [])
    monkeypatch.setattr(mock_pool, "prompt", _slow_prompt)

    async def _deploy_then_ask_wrong_sid():
        async with Client(mcp_server.MCP) as c:
            deploy_r = await c.call_tool(
                "deploy_browser_agent", {"task": "real task"}
            )
            sid = _extract_sid(deploy_r.content[0].text)
            # Poll the WRONG sid while the real task is still running.
            status_r = await c.call_tool(
                "browser_status", {"session_id": "no-such-session-zzz"}
            )
            return deploy_r, sid, status_r

    deploy_r, sid, status_r = run(_deploy_then_ask_wrong_sid())

    txt = status_r.content[0].text
    assert "No deploy_browser_agent task" in txt
    assert "no-such-session-zzz" in txt
    # The active-task list is included with our still-running task.
    assert "Active browser tasks:" in txt
    assert sid in txt


def test_deploy_always_targets_browser_agent_org(mock_pool):
    """The tool is scoped to browser-agent — the caller cannot redirect it."""
    mock_pool.org = "browser-agent"
    mock_pool._prompt_result = ("ok", "", "end_turn", [])

    run(_deploy_and_status("do something"))

    # _get_org is called with exactly "browser-agent" (captured by the mock
    # fixture, which sets mock_pool.org from the arg).
    assert mock_pool.org == "browser-agent"


def test_browser_status_running_state_before_completion(mock_pool, monkeypatch):
    """If the background task hasn't completed yet, browser_status returns
    state=running with elapsed time. The mock prompt is normally instant;
    we slow it down so we can observe the in-flight state."""
    mock_pool.org = "browser-agent"

    # Slow mock prompt — the bg task is still running when we poll.
    async def _slow_prompt(session_id, message, images=None):
        await asyncio.sleep(0.05)
        return ("ok", "", "end_turn", [])
    monkeypatch.setattr(mock_pool, "prompt", _slow_prompt)

    async def _deploy_then_poll_midflight():
        async with Client(mcp_server.MCP) as c:
            deploy_r = await c.call_tool(
                "deploy_browser_agent", {"task": "long task"}
            )
            sid = _extract_sid(deploy_r.content[0].text)
            # Poll BEFORE the slow prompt finishes — the bg task is still
            # pending.
            status_r = await c.call_tool("browser_status", {"session_id": sid})
            return deploy_r, sid, status_r

    deploy_r, sid, status_r = run(_deploy_then_poll_midflight())

    txt = status_r.content[0].text
    assert "running" in txt
    assert "elapsed" in txt
    # The task preview is in the status so the caller remembers what's running.
    assert "long task" in txt



# ---------------------------------------------------------------------------
# web-research proxy (web_search / web_fetch / web_research)
# ---------------------------------------------------------------------------

import shutil as _shutil
import socket as _socket
from mcp.types import TextContent as _MText, ImageContent as _MImg


class _FakeResult:
    """Stand-in for an MCP CallToolResult — content list + is_error flag."""
    def __init__(self, content, is_error=False):
        self.content = content
        self.is_error = is_error


def _patch_forward(monkeypatch, captor, content=None, is_error=False, exc=None):
    """Monkeypatch the single forward seam _forward_to_research_mcp."""
    import pux_harness.mcp_server as ms
    async def _fake(url, tool, args):
        captor["tool"] = tool
        captor["args"] = dict(args)
        captor["url"] = url
        if exc is not None:
            raise exc
        return _FakeResult(content or [_MText(type="text", text="ok")], is_error=is_error)
    monkeypatch.setattr(ms, "_forward_to_research_mcp", _fake)


def test_web_search_forwards_query_and_optionals(mock_pool, monkeypatch):
    cap = {}
    _patch_forward(monkeypatch, cap, [_MText(type="text", text="result: langgraph is a graph framework")])
    r = run(_call("web_search", query="langgraph", top_k=5))
    assert cap["tool"] == "search"
    assert cap["args"] == {"query": "langgraph", "top_k": 5}
    assert "langgraph is" in r.content[0].text


def test_web_search_omits_none_optionals(mock_pool, monkeypatch):
    cap = {}
    _patch_forward(monkeypatch, cap)
    run(_call("web_search", query="just a query"))
    assert cap["args"] == {"query": "just a query"}, "None optionals must NOT be forwarded"


def test_web_fetch_passes_through_images_inline(mock_pool, monkeypatch):
    cap = {}
    _patch_forward(monkeypatch, cap, [
        _MText(type="text", text="# Page\nhello"),
        _MImg(type="image", data="ABC==", mimeType="image/png"),
    ])
    r = run(_call("web_fetch", url="https://example.com", text_only=True))
    assert cap["tool"] == "fetch"
    assert cap["args"]["url"] == "https://example.com"
    assert cap["args"]["text_only"] is True
    types = [b.type for b in r.content]
    assert "text" in types and "image" in types, "fetch image must flow through inline"
    img = next(b for b in r.content if b.type == "image")
    assert img.data == "ABC=="


def test_web_research_defaults_forwarded(mock_pool, monkeypatch):
    cap = {}
    _patch_forward(monkeypatch, cap)
    run(_call("web_research", query="model context protocol"))
    assert cap["tool"] == "research"
    assert cap["args"] == {"query": "model context protocol", "max_results": 3, "depth": "quick"}


def test_web_proxy_unreachable_returns_error_string(mock_pool, monkeypatch):
    cap = {}
    _patch_forward(monkeypatch, cap, exc=ConnectionRefusedError("connection refused"))
    r = run(_call("web_search", query="anything"))
    txt = r.content[0].text
    assert "Error" in txt
    assert "unreachable" in txt
    assert "connection refused" in txt


def test_web_proxy_upstream_is_error_surfaced(mock_pool, monkeypatch):
    cap = {}
    _patch_forward(monkeypatch, cap,
                   [_MText(type="text", text="boom: invalid query syntax")], is_error=True)
    r = run(_call("web_search", query="x"))
    txt = r.content[0].text
    assert "Error from web-research-mcp search" in txt
    assert "boom: invalid query syntax" in txt


def _research_mcp_up() -> bool:
    s = _socket.socket(); s.settimeout(1.0)
    try:
        s.connect(("127.0.0.1", 41827))
        return True
    except OSError:
        return False
    finally:
        s.close()


@pytest.mark.skipif(not _research_mcp_up(), reason="web-research-mcp not running on :41827")
def test_web_search_live_integration(mock_pool):
    """Live: the real web-research-mcp. Proves the proxy wiring end-to-end
    (connect → initialize → call_tool → coerce → return). Skipped if the
    server is down so the suite stays hermetic in CI."""
    r = run(_call("web_search", query="model context protocol anthropic", top_k=3))
    # Don't assert on result content (the web is non-deterministic); only that
    # we got a real response back, not an infrastructure error.
    assert r.is_error is False
    txt = r.content[0].text if r.content else ""
    assert "unreachable" not in txt, f"proxy should have reached the live server: {txt[:120]}"


# ---------------------------------------------------------------------------
# Regression: OrgConnection.prompt must pass args in acp.py's order —
#   acp.py:prompt(self, prompt, session_id, ...)  →  prompt FIRST, session_id SECOND.
# A prior build transposed these in the internal conn.prompt(...) call, which
# raised a deterministic 2-error PromptRequest ValidationError on EVERY prompt
# (prompt got the session_id string; session_id got the content-block list).
# The MCP-layer tests above masked it because they swap in MockOrgConnection at
# the wrapper boundary and never exercise the real OrgConnection.prompt body.
# ---------------------------------------------------------------------------
def test_orgconnection_prompt_passes_args_in_acp_order(monkeypatch):
    from pux_harness.mcp_server import OrgConnection

    recorded: dict[str, Any] = {}

    class _Resp:
        stop_reason = "end_turn"

    class FakeConn:
        async def prompt(self, prompt, session_id, **kw):
            # acp.py signature: prompt is the content-block list, session_id is the str.
            recorded["prompt_arg"] = prompt
            recorded["session_id_arg"] = session_id
            return _Resp()

    class FakeClient:
        def reset(self, sid): pass
        def messages(self, sid): return []
        def thoughts(self, sid): return []
        def images(self, sid): return []

    oc = OrgConnection(org="general")
    oc.conn = FakeConn()
    oc.client = FakeClient()

    async def _noop_ensure():
        return oc
    oc.ensure = _noop_ensure  # type: ignore[assignment]

    text, thoughts, stop, agent_images = run(oc.prompt("SID-123", "hello"))

    # The content-block list must be the FIRST positional arg passed to
    # conn.prompt, and the session_id string the SECOND. If these are
    # transposed, acp.py builds PromptRequest(prompt=<sid str>,
    # session_id=<block list>) → pydantic ValidationError on every call.
    assert recorded["session_id_arg"] == "SID-123", (
        "session_id must be the 2nd arg to conn.prompt (acp.py order)"
    )
    assert isinstance(recorded["prompt_arg"], list), (
        "prompt (content blocks) must be the 1st arg to conn.prompt (acp.py order)"
    )
    assert recorded["prompt_arg"][0].text == "hello"
    assert stop == "end_turn"


# ---------------------------------------------------------------------------
# Resilience: transient errors retried, deterministic errors surfaced, and
# deploy_browser_agent never swallows the session id on failure (the fix for
# "it breaks the subagent, then I lose the entire conversation").
# ---------------------------------------------------------------------------
def test_is_transient_provider_error_classification():
    from pux_harness.mcp_server import _is_transient_provider_error

    class APIConnectionError(Exception): pass
    class InternalServerError(Exception): pass
    class RateLimitError(Exception): pass
    class BadRequestError(Exception): pass
    class ValidationError(Exception): pass

    # transient — retried
    assert _is_transient_provider_error(ConnectionError("model stream stalled"))
    assert _is_transient_provider_error(TimeoutError("operation timed out"))
    assert _is_transient_provider_error(APIConnectionError("conn refused"))
    assert _is_transient_provider_error(InternalServerError("service 503"))
    assert _is_transient_provider_error(RateLimitError("rate limit hit"))
    # BadRequestError retried ONLY when the message implies stream/timeout
    assert _is_transient_provider_error(BadRequestError("model stream stalled"))
    assert _is_transient_provider_error(BadRequestError("upstream timeout"))
    assert not _is_transient_provider_error(BadRequestError("invalid model id"))
    # deterministic — NOT retried
    assert not _is_transient_provider_error(ValueError("invalid input"))
    assert not _is_transient_provider_error(KeyError("missing"))
    assert not _is_transient_provider_error(ValidationError("2 validation errors"))
    assert not _is_transient_provider_error(AttributeError("no attr"))


def test_orgconnection_prompt_retries_transient_then_succeeds(monkeypatch):
    """A transient provider error is retried; second attempt succeeds and the
    caller never sees the hiccup."""
    from pux_harness.mcp_server import OrgConnection

    calls = {"n": 0}
    class _Resp: stop_reason = "end_turn"
    class FakeConn:
        async def prompt(self, prompt, session_id, **kw):
            calls["n"] += 1
            if calls["n"] == 1:
                raise ConnectionError("model stream stalled: connection reset by peer")
            return _Resp()
    class FakeClient:
        def reset(self, sid): pass
        def messages(self, sid): return ["ok-after-retry"]
        def thoughts(self, sid): return []
        def images(self, sid): return []

    oc = OrgConnection(org="general")
    oc.conn = FakeConn()
    oc.client = FakeClient()
    async def _noop(): return oc
    oc.ensure = _noop  # type: ignore[assignment]
    async def _fast_sleep(_d): return None
    monkeypatch.setattr("asyncio.sleep", _fast_sleep)

    text, _thoughts, stop, _imgs = run(oc.prompt("SID", "hi"))
    assert calls["n"] == 2, f"should retry once after transient: {calls}"
    assert stop == "end_turn"
    assert text == "ok-after-retry"


def test_orgconnection_prompt_does_not_retry_deterministic_errors(monkeypatch):
    """A real code bug surfaces immediately — no wasted retries."""
    from pux_harness.mcp_server import OrgConnection

    calls = {"n": 0}
    class FakeConn:
        async def prompt(self, prompt, session_id, **kw):
            calls["n"] += 1
            raise ValueError("invalid input")
    class FakeClient:
        def reset(self, sid): pass
        def messages(self, sid): return []
        def thoughts(self, sid): return []
        def images(self, sid): return []

    oc = OrgConnection(org="general")
    oc.conn = FakeConn()
    oc.client = FakeClient()
    async def _noop(): return oc
    oc.ensure = _noop  # type: ignore[assignment]
    async def _fast_sleep(_d): return None
    monkeypatch.setattr("asyncio.sleep", _fast_sleep)

    with pytest.raises(ValueError):
        run(oc.prompt("SID", "hi"))
    assert calls["n"] == 1, f"deterministic error must not retry: {calls}"


def test_deploy_browser_agent_no_session_created_path(mock_pool, monkeypatch):
    """If new_session itself fails (before a sid exists), the error says so
    honestly — no fake sid, no resume promise."""
    async def _raising_new_session(model=None, cwd=None):
        raise RuntimeError("sandbox refused to start")
    monkeypatch.setattr(mock_pool, "new_session", _raising_new_session)

    r = run(_call("deploy_browser_agent", task="go to example.com"))
    txt = r.content[0].text if r.content else ""
    assert "no session was created" in txt
    assert "sandbox refused to start" in txt


# ---------------------------------------------------------------------------
# list_sessions reads the persistent thread store (not the in-memory _pool).
# The fix for the silent "No subagent sessions" lie that made persisted work
# look lost — a fresh server has an empty _pool but the store has every thread.
# ---------------------------------------------------------------------------
def test_list_sessions_reads_store_not_pool(monkeypatch):
    from contextlib import asynccontextmanager
    from pux_harness import threads as threads_mod

    class _FakeStore:
        async def list_threads(self, org=None):
            rows = [
                {"thread_id": "abc123", "org": "browser-agent",
                 "metadata": '{"title": "evaluate pose studio"}',
                 "created_at": "2026-07-20T17:56:54+00:00"},
                {"thread_id": "def456", "org": "coder",
                 "metadata": "{}", "created_at": "2026-07-20T08:34:25+00:00"},
            ]
            return [r for r in rows if org is None or r["org"] == org]

    @asynccontextmanager
    async def _fake_open():
        yield _FakeStore()

    monkeypatch.setattr(threads_mod, "open_thread_store", _fake_open)
    mcp_server._pool.clear()  # fresh server — empty pool

    r = run(_call("list_sessions"))
    txt = r.content[0].text if r.content else ""
    assert "abc123" in txt, f"thread must appear: {txt[:300]}"
    assert "def456" in txt
    assert "browser-agent" in txt and "coder" in txt
    assert "2 of 2 session(s)" in txt
    assert "load_session" in txt  # resume recipe surfaced
    assert "No subagent sessions" not in txt
    # title parsed out of json metadata
    assert "evaluate pose studio" in txt


def test_list_sessions_org_filter(monkeypatch):
    from contextlib import asynccontextmanager
    from pux_harness import threads as threads_mod

    class _FakeStore:
        async def list_threads(self, org=None):
            rows = [
                {"thread_id": "abc", "org": "browser-agent", "metadata": "{}", "created_at": "t1"},
                {"thread_id": "def", "org": "coder", "metadata": "{}", "created_at": "t2"},
            ]
            return [r for r in rows if org is None or r["org"] == org]

    @asynccontextmanager
    async def _fake_open():
        yield _FakeStore()

    monkeypatch.setattr(threads_mod, "open_thread_store", _fake_open)
    mcp_server._pool.clear()
    r = run(_call("list_sessions", org="browser-agent"))
    txt = r.content[0].text if r.content else ""
    assert "abc" in txt and "def" not in txt
    assert "org=browser-agent" in txt


def test_list_sessions_empty_store_says_so_honestly(monkeypatch):
    from contextlib import asynccontextmanager
    from pux_harness import threads as threads_mod

    class _FakeStore:
        async def list_threads(self, org=None): return []

    @asynccontextmanager
    async def _fake_open():
        yield _FakeStore()

    monkeypatch.setattr(threads_mod, "open_thread_store", _fake_open)
    mcp_server._pool.clear()
    r = run(_call("list_sessions"))
    txt = r.content[0].text if r.content else ""
    assert "No subagent sessions" in txt  # honest only when the STORE is empty
