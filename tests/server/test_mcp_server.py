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
        self._prompt_result: tuple[str, str, str] = (
            "hello from agent", "thinking...", "end_turn",
        )
        self._sessions: list[dict] = []
        self._load_result = True

    async def new_session(self, model: str | None = None,
                           cwd: str | None = None) -> str:
        self.calls.append(("new_session", model, cwd))
        return self._next_session_id

    async def prompt(self, session_id: str,
                     message: str) -> tuple[str, str, str]:
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
    mock_pool._prompt_result = ("partial", "", "cancelled")
    mcp_server._session_org["sess-1"] = "general"
    r = run(_call("prompt", session_id="sess-1", message="go"))
    assert "cancelled" in r.content[0].text
    assert "Task cancelled" in r.content[0].text


def test_prompt_no_response_text(mock_pool):
    mock_pool._prompt_result = ("", "", "end_turn")
    mcp_server._session_org["sess-1"] = "general"
    r = run(_call("prompt", session_id="sess-1", message="go"))
    assert "(no response)" in r.content[0].text


# ---------------------------------------------------------------------------
# list_sessions
# ---------------------------------------------------------------------------

def test_list_sessions_empty_pool(mock_pool):
    r = run(_call("list_sessions"))
    assert "No subagent sessions" in r.content[0].text


def test_list_sessions_with_data(mock_pool):
    mock_pool._sessions = [
        {"session_id": "s1", "org": "general", "title": "test",
         "updated_at": "2026-07-11T12:00:00Z"},
    ]
    # list_sessions iterates _pool; inject a fake entry.
    mcp_server._pool["general"] = mock_pool
    r = run(_call("list_sessions"))
    assert "1 session" in r.content[0].text
    assert "s1" in r.content[0].text
    assert "general" in r.content[0].text


def test_list_sessions_org_filter(mock_pool):
    mock_pool._sessions = [
        {"session_id": "s1", "org": "general", "title": None,
         "updated_at": None},
    ]
    mcp_server._pool["general"] = mock_pool
    r = run(_call("list_sessions", org="general"))
    assert "s1" in r.content[0].text
    # Filtering by a different org returns empty.
    r2 = run(_call("list_sessions", org="invest"))
    assert "No active" in r2.content[0].text


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
