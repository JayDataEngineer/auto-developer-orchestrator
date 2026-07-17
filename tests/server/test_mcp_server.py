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
# deploy_browser_agent
# ---------------------------------------------------------------------------

_PNG_1X1 = (
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ"
    "xIsAAAAASUVORK5CYII="
)


def test_deploy_browser_agent_happy_path(mock_pool):
    """One-shot: new_session on browser-agent + prompt with the task, returns
    the agent text + stop reason. No session-id leaks to the caller."""
    mock_pool.org = "browser-agent"
    mock_pool._prompt_result = ("Page says: Welcome to Example", "", "end_turn", [])

    r = run(_call("deploy_browser_agent", task="Go to https://example.com and summarize"))

    assert r.is_error is False
    txt = r.content[0].text
    assert "Welcome to Example" in txt
    assert "[end_turn]" in txt
    # new_session + prompt were both called on the browser-agent org
    assert mock_pool.calls[0][0] == "new_session"
    assert mock_pool.calls[1][0] == "prompt"
    assert mock_pool.calls[1][1] is not None  # the generated session id


def test_deploy_browser_agent_returns_images_inline(mock_pool, temp_staged):
    """When the agent produces screenshots, they come back as native MCP
    ImageContent blocks (so the client SEES them) AND persist to staged/."""
    mock_pool.org = "browser-agent"
    mock_pool._prompt_result = (
        "Here is the screenshot.", "", "end_turn",
        [{"data": _PNG_1X1, "mime_type": "image/png"}],
    )

    r = run(_call("deploy_browser_agent", task="screenshot https://example.com"))

    assert r.is_error is False
    # text block + image block
    types = [b.type for b in r.content]
    assert "text" in types and "image" in types
    img_block = next(b for b in r.content if b.type == "image")
    assert img_block.data == _PNG_1X1
    assert img_block.mimeType == "image/png"
    # persisted to staged/ (temp_staged is the data/ dir; files land in staged/)
    staged = list((temp_staged / "staged").glob("browser_agent_*.png"))
    assert len(staged) == 1


def test_deploy_browser_agent_infra_error_is_surfaced(mock_pool):
    """If the browser-agent org fails to boot, the error is returned as text
    (not raised) so the MCP client gets a usable message."""
    async def _boom(org):
        raise RuntimeError("sandbox refused to start")
    # mock_pool patches _get_org; override it to raise
    import pux_harness.mcp_server as ms
    ms._get_org = _boom  # noqa: SLF001

    r = run(_call("deploy_browser_agent", task="anything"))

    assert r.is_error is False
    assert "Error deploying browser-agent" in r.content[0].text
    assert "sandbox refused to start" in r.content[0].text


def test_deploy_browser_agent_uses_browser_agent_org(mock_pool):
    """The tool always targets the browser-agent org — the caller cannot
    redirect it to another org (scoped by design)."""
    mock_pool.org = "browser-agent"
    mock_pool._prompt_result = ("ok", "", "end_turn", [])

    run(_call("deploy_browser_agent", task="do something"))

    # _get_org is called with exactly "browser-agent" (captured by the mock
    # fixture, which sets mock_pool.org from the arg).
    assert mock_pool.org == "browser-agent"
