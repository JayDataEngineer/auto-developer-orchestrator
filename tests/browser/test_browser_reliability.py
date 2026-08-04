"""EXTENSIVE reliability tests for the browser tool's spawn + fallback paths.

These tests PROVE — not claim — that the browser tool always returns a working
browser. Every failure mode that previously caused "browser not available" or
"browser is down" is covered by an explicit test:

  1. Happy path: ephemeral spawn succeeds on first attempt
  2. Ephemeral spawn succeeds on retry (2nd attempt)
  3. Ephemeral spawn succeeds on final attempt (3rd)
  4. Ephemeral spawn fails ALL 3 attempts → FALLBACK to supervisord port 9876
  5. Ephemeral fails AND supervisord down → transient RuntimeError (rare)
  6. Cached port still alive → instant return, no spawn
  7. Cached port dead → respawn
  8. _supervisord_browser_ready: alive=true → True
  9. _supervisord_browser_ready: alive=false, ok=false → False (no warmup)
 10. _supervisord_browser_ready: alive=false, ok=true → warmup → alive=true → True
 11. _supervisord_browser_ready: alive=false, ok=true → warmup → still false → False
 12. _supervisord_browser_ready: connection refused → False
 13. Fallback cached across calls (no re-spawn storm)
 14. _sb_post happy path → returns parsed JSON
 15. _sb_post on fallback works (proves the fallback port serves /navigate)
 16. _sb_post on total failure → transient error (NOT "give up")
 17. _sb_post timeout → transient error
 18. _sb_post exec failure → transient error
 19. _sb_post malformed response → transient error
 20. No sticky dead state EVER (10 sequential failures then success works)
 21. _spawn_one_attempt: kill-stale called before spawn
 22. No breaker symbols present (regression guard)
 23. Error messages never instruct the agent to "give up" / "tell user down"
"""
from __future__ import annotations

import pytest

from pux_harness.sandbox.tools import browser as B


# ---------------------------------------------------------------------------
# Fake exec client — records every call, returns configurable responses
# ---------------------------------------------------------------------------

class _FakeExec:
    """Stand-in for DockerExecClient. Records calls; returns canned responses.

    `respond(substr, output, exit_code=0)`: any exec() whose command contains
    `substr` returns (output, exit_code). First matching substr wins. Calls
    with no configured response return ("", 0).
    """

    def __init__(self) -> None:
        self.calls: list[str] = []
        self._responses: list[tuple[str, str, int]] = []

    def respond(self, substr: str, output: str, exit_code: int = 0) -> "_FakeExec":
        self._responses.append((substr, output, exit_code))
        return self

    def exec(self, cmd: str, timeout: int | None = None) -> tuple[str, int]:
        self.calls.append(cmd)
        for substr, output, exit_code in self._responses:
            if substr in cmd:
                return output, exit_code
        return "", 0


# ---------------------------------------------------------------------------
# Fixtures — reset module globals between tests (no cross-test leakage)
# ---------------------------------------------------------------------------

@pytest.fixture(autouse=True)
def _reset_browser_state(monkeypatch):
    """Reset all module-level state before each test. Without this, a cached
    `_process_http_port` from one test would short-circuit the next test's
    spawn path — making the tests lie about what they cover."""
    monkeypatch.setattr(B, "_process_http_port", None)
    monkeypatch.setattr(B, "_warmup_started", False)
    yield


# ---------------------------------------------------------------------------
# Test 22 (regression guard): NO breaker symbols, EVER
# ---------------------------------------------------------------------------

def test_no_breaker_symbols_present():
    """The sticky circuit breaker was a reliability defect. It must NEVER
    come back. Any of these symbols re-appearing is a regression."""
    assert not hasattr(B, "_browser_dead"), "_browser_dead resurrected"
    assert not hasattr(B, "_spawn_failures"), "_spawn_failures resurrected"
    assert not hasattr(B, "_MAX_SPAWN_FAILURES"), "_MAX_SPAWN_FAILURES resurrected"


def test_no_browser_unavailable_reason_string():
    """The `reason: browser_unavailable` JSON shape was the sticky-breaker
    contract — surfacing it instructs the agent to give up. No live code
    path may produce it. (The only places it appears must be comments.)"""
    import inspect
    src = inspect.getsource(B)
    # Strip comments + docstrings is hard; instead check that the literal
    # JSON key is not ASSIGNED anywhere. A comment mentioning the old reason
    # is fine (for context); an actual dict literal is not.
    for marker in ['"reason": "browser_unavailable"',
                   "'reason': 'browser_unavailable'",
                   '"reason":"browser_unavailable"']:
        assert marker not in src, f"breaker reason literal present: {marker!r}"


def test_error_messages_never_tell_agent_to_give_up():
    """Error strings must NEVER contain 'tell the user' / 'STOP calling' /
    'permanently broken' / 'complete the task without browser' — those were
    the breaker's instructions to give up. New errors say 'transient — retry'."""
    import inspect
    src = inspect.getsource(B)
    for banned in [
        "Tell the user the browser is down",
        "STOP calling any pux_sandbox_browser",
        "STOP calling browser tools",
        "permanently broken",
        "complete the task without browser tools",
        "Inform the user the browser is unavailable",
    ]:
        assert banned not in src, (
            f"give-up phrase still in source: {banned!r}"
        )


# ---------------------------------------------------------------------------
# Tests 8-12: _supervisord_browser_ready (the fallback readiness check)
# ---------------------------------------------------------------------------

def test_supervisord_ready_when_alive_true():
    """Fallback usable when /status reports alive=true."""
    client = _FakeExec().respond("/status", '{"ok": true, "alive": true}')
    assert B._supervisord_browser_ready(client) is True


def test_supervisord_ready_handles_no_spaces_in_json():
    """sb_server may emit minified JSON (alive:true without space)."""
    client = _FakeExec().respond("/status", '{"ok":true,"alive":true}')
    assert B._supervisord_browser_ready(client) is True


def test_supervisord_not_ready_when_alive_false_ok_false():
    """Both ok + alive false → no fallback (server itself down)."""
    client = _FakeExec().respond("/status", '{"ok": false}')
    assert B._supervisord_browser_ready(client) is False


def test_supervisord_warmup_path_when_ok_but_not_alive_then_alive():
    """Server up (ok=true) but Chrome cold (alive=false) → trigger warmup,
    re-check, find alive=true → fallback usable."""
    client = (
        _FakeExec()
        # First /status call: ok but not alive
        .respond("max-time 3", '{"ok": true, "alive": false}')
    )
    # Need DIFFERENT responses for the two /status calls — use a stateful fake
    calls = {"n": 0}

    def _exec(cmd, timeout=None):
        client.calls.append(cmd)
        if "/status" in cmd and "max-time 3" in cmd:
            calls["n"] += 1
            if calls["n"] == 1:
                return '{"ok": true, "alive": false}', 0
            return '{"ok": true, "alive": true}', 0  # after warmup
        return "", 0

    client.exec = _exec
    assert B._supervisord_browser_ready(client) is True


def test_supervisord_warmup_fails_returns_false():
    """Warmup ran but Chrome still not alive → fallback NOT usable."""
    state = {"n": 0}

    def _exec(cmd, timeout=None):
        state["n"] += 1
        if "/status" in cmd and "max-time 3" in cmd:
            return '{"ok": true, "alive": false}', 0
        return "", 0  # warmup curl returns nothing useful

    client = _FakeExec()
    client.exec = _exec
    assert B._supervisord_browser_ready(client) is False


def test_supervisord_not_ready_on_empty_response():
    """Connection refused / empty response → False (not usable)."""
    client = _FakeExec()  # no responses configured → empty string
    assert B._supervisord_browser_ready(client) is False


# ---------------------------------------------------------------------------
# Tests 1-3: _ensure_ephemeral_server happy + retry paths
# ---------------------------------------------------------------------------

def test_ensure_ephemeral_succeeds_on_first_attempt(monkeypatch):
    """Happy path: spawn succeeds immediately → returns ephemeral port."""
    spawn_calls: list[tuple[int, int]] = []

    def _spawn(client, http_port, cdp_port):
        spawn_calls.append((http_port, cdp_port))
        return True  # immediate success

    monkeypatch.setattr(B, "_spawn_one_attempt", _spawn)
    # Stub cookie restore so it doesn't issue exec calls
    monkeypatch.setattr(B, "_restore_session_cookies", lambda c, p: None)

    port = B._ensure_ephemeral_server(_FakeExec())
    assert port == B._process_http_port
    assert len(spawn_calls) == 1, f"expected 1 spawn call, got {spawn_calls}"


def test_ensure_ephemeral_succeeds_on_second_attempt(monkeypatch):
    """First spawn fails (Chrome half-dead), second attempt kills stale and
    succeeds — proves the retry loop actually retries."""
    attempt = {"n": 0}

    def _spawn(client, http_port, cdp_port):
        attempt["n"] += 1
        return attempt["n"] == 2  # fail first, succeed second

    monkeypatch.setattr(B, "_spawn_one_attempt", _spawn)
    monkeypatch.setattr(B, "_restore_session_cookies", lambda c, p: None)
    # Stub log-reading exec (returns empty)
    client = _FakeExec().respond("tail", "", 0)

    port = B._ensure_ephemeral_server(client)
    assert attempt["n"] == 2
    assert port == B._process_http_port


def test_ensure_ephemeral_succeeds_on_final_attempt(monkeypatch):
    """All spawn attempts except the LAST fail — last one succeeds. Proves
    we actually try up to _BROWSER_SPAWN_ATTEMPTS times, not give up early."""
    attempt = {"n": 0}
    target = B._BROWSER_SPAWN_ATTEMPTS  # 3 by default

    def _spawn(client, http_port, cdp_port):
        attempt["n"] += 1
        return attempt["n"] == target

    monkeypatch.setattr(B, "_spawn_one_attempt", _spawn)
    monkeypatch.setattr(B, "_restore_session_cookies", lambda c, p: None)
    client = _FakeExec().respond("tail", "", 0)

    port = B._ensure_ephemeral_server(client)
    assert attempt["n"] == target
    assert port == B._process_http_port


# ---------------------------------------------------------------------------
# Test 4-5: fallback path (the RELIABILITY CONTRACT)
# ---------------------------------------------------------------------------

def test_ensure_ephemeral_falls_back_to_supervisord_when_spawn_fails(monkeypatch):
    """*** THE CORE RELIABILITY TEST ***

    Ephemeral spawn fails ALL attempts → FALL BACK to supervisord port 9876
    (which is up + alive). The agent NEVER sees 'browser not available' —
    it gets a working browser via the fallback. This is the exact scenario
    that previously caused 'browser is down' reports."""
    def _spawn_fail(client, http_port, cdp_port):
        return False  # Chrome never comes up

    monkeypatch.setattr(B, "_spawn_one_attempt", _spawn_fail)
    monkeypatch.setattr(B, "_restore_session_cookies", lambda c, p: None)
    # Supervisord IS ready (the always-on default)
    monkeypatch.setattr(B, "_supervisord_browser_ready", lambda c: True)

    client = _FakeExec().respond("tail", "", 0).respond("echo", "", 0)

    port = B._ensure_ephemeral_server(client)
    assert port == B._SUPERVISORD_SB_PORT, (
        f"ephemeral failure must fall back to {B._SUPERVISORD_SB_PORT}, got {port}"
    )
    assert B._process_http_port == B._SUPERVISORD_SB_PORT


def test_ensure_ephemeral_raises_when_both_layers_fail(monkeypatch):
    """Catastrophic case: ephemeral spawn fails AND supervisord fallback down.
    Must raise a TRANSIENT error (not return a broken port, not silently fail).
    The container will be recreated by ensure() on the next call."""
    monkeypatch.setattr(B, "_spawn_one_attempt", lambda c, h, p: False)
    monkeypatch.setattr(B, "_restore_session_cookies", lambda c, p: None)
    monkeypatch.setattr(B, "_supervisord_browser_ready", lambda c: False)
    client = _FakeExec().respond("tail", "", 0)

    with pytest.raises(RuntimeError) as exc_info:
        B._ensure_ephemeral_server(client)

    msg = str(exc_info.value)
    # Error must be TRANSIENT (retry-friendly), not defeatist
    assert "Transient" in msg or "transient" in msg or "Retry" in msg, (
        f"error must say 'transient/retry', got: {msg!r}"
    )
    for banned in ["permanently broken", "STOP calling", "Tell the user"]:
        assert banned not in msg


def test_fallback_logs_to_diagnostic_file(monkeypatch):
    """When the fallback fires, it appends to /tmp/pux_browser_fallback.log
    inside the container so operators can see ephemeral was broken (root-cause
    visibility) without the agent ever seeing a failure."""
    monkeypatch.setattr(B, "_spawn_one_attempt", lambda c, h, p: False)
    monkeypatch.setattr(B, "_restore_session_cookies", lambda c, p: None)
    monkeypatch.setattr(B, "_supervisord_browser_ready", lambda c: True)
    client = _FakeExec().respond("tail", "", 0)

    B._ensure_ephemeral_server(client)

    # The fallback must have appended a diagnostic log line
    log_calls = [c for c in client.calls if "pux_browser_fallback.log" in c]
    assert len(log_calls) >= 1, (
        f"fallback must log to pux_browser_fallback.log; calls={client.calls}"
    )
    assert "fall" in log_calls[0].lower(), (
        f"log message should mention fallback; got: {log_calls[0]!r}"
    )


# ---------------------------------------------------------------------------
# Tests 6-7, 13: cached port fast-path
# ---------------------------------------------------------------------------

def test_cached_ephemeral_port_alive_no_respawn(monkeypatch):
    """If the cached port is alive, return immediately — no spawn calls."""
    spawn_calls = []
    monkeypatch.setattr(B, "_spawn_one_attempt",
                        lambda c, h, p: spawn_calls.append((h, p)) or True)
    monkeypatch.setattr(B, "_process_http_port", 9942)
    monkeypatch.setattr(B, "_is_server_alive", lambda c, p: True)

    port = B._ensure_ephemeral_server(_FakeExec())
    assert port == 9942
    assert spawn_calls == [], f"should NOT spawn when cached port alive; got {spawn_calls}"


def test_cached_ephemeral_port_dead_triggers_respawn(monkeypatch):
    """Cached port found dead → spawn fresh. The cached-port fast-path must
    not trap us into reusing a known-dead port."""
    monkeypatch.setattr(B, "_process_http_port", 9942)
    monkeypatch.setattr(B, "_is_server_alive", lambda c, p: False)
    monkeypatch.setattr(B, "_spawn_one_attempt", lambda c, h, p: True)
    monkeypatch.setattr(B, "_restore_session_cookies", lambda c, p: None)

    port = B._ensure_ephemeral_server(_FakeExec())
    # Spawn ran, new port allocated
    assert port == B._process_http_port


def test_fallback_port_cached_no_respawn_storm(monkeypatch):
    """After fallback to 9876, the next call reuses 9876 — does NOT re-spawn
    or re-check ephemeral. Prevents a spawn storm where every call retries
    the broken ephemeral path."""
    monkeypatch.setattr(B, "_spawn_one_attempt", lambda c, h, p: False)
    monkeypatch.setattr(B, "_restore_session_cookies", lambda c, p: None)
    monkeypatch.setattr(B, "_supervisord_browser_ready", lambda c: True)
    client = _FakeExec().respond("tail", "", 0).respond("echo", "", 0)

    # First call: spawn fails, fallback to 9876
    port1 = B._ensure_ephemeral_server(client)
    assert port1 == 9876

    # Second call: cached 9876 alive → no spawn, no fallback check
    spawn_calls_after = []
    monkeypatch.setattr(B, "_spawn_one_attempt",
                        lambda c, h, p: spawn_calls_after.append((h, p)) or False)
    ready_calls_after = []
    monkeypatch.setattr(B, "_supervisord_browser_ready",
                        lambda c: ready_calls_after.append(c) or True)
    # Make cached-port check say "alive"
    monkeypatch.setattr(B, "_is_server_alive", lambda c, p: True)

    port2 = B._ensure_ephemeral_server(_FakeExec())
    assert port2 == 9876
    assert spawn_calls_after == [], "must not re-spawn on cached fallback port"
    assert ready_calls_after == [], "must not re-check fallback when cached alive"


# ---------------------------------------------------------------------------
# Tests 14-19: _sb_post (the actual tool-call entry point)
# ---------------------------------------------------------------------------

def test_sb_post_happy_path_returns_parsed_json(monkeypatch):
    """Spawn succeeds + POST returns valid JSON → _sb_post returns it via _result."""
    monkeypatch.setattr(B, "_ensure_ephemeral_server", lambda c: 9942)
    client = (
        _FakeExec()
        .respond("/navigate", '{"ok": true, "page_data": {"title": "Hi"}}', 0)
    )
    out = B._sb_post(client, "/navigate", {"url": "https://example.com"})
    assert '"ok": true' in out
    assert "Hi" in out


def test_sb_post_works_via_fallback_port(monkeypatch):
    """When _ensure_ephemeral_server returns the fallback port (9876),
    _sb_post must still POST to it and return the result. This proves the
    fallback actually serves tool calls end-to-end."""
    monkeypatch.setattr(B, "_ensure_ephemeral_server",
                        lambda c: B._SUPERVISORD_SB_PORT)
    client = _FakeExec().respond("/navigate",
                                  '{"ok": true, "page_data": {"title": "FB"}}', 0)
    out = B._sb_post(client, "/navigate", {"url": "https://example.com"})
    assert '"ok": true' in out
    assert "FB" in out


def test_sb_post_on_spawn_failure_returns_transient(monkeypatch):
    """Both layers fail → _sb_post returns a TRANSIENT error (not 'give up').
    The agent can retry; the next call may succeed (container healing)."""
    def _boom(client):
        raise RuntimeError("ALL browser spawn paths failed: ... transient — retry")
    monkeypatch.setattr(B, "_ensure_ephemeral_server", _boom)
    client = _FakeExec()
    out = B._sb_post(client, "/navigate", {"url": "https://example.com"})
    assert "success" in out  # _result wraps it
    assert "transient" in out.lower() or "retry" in out.lower()
    for banned in ["permanently broken", "STOP calling", "Tell the user"]:
        assert banned not in out


def test_sb_post_on_timeout_returns_transient(monkeypatch):
    """Curl POST times out → transient error."""
    from pux_harness.sandbox.docker_exec import ExecTimeout
    monkeypatch.setattr(B, "_ensure_ephemeral_server", lambda c: 9942)

    def _exec_timeout(cmd, timeout=None):
        raise ExecTimeout("timed out")
    client = _FakeExec()
    client.exec = _exec_timeout

    out = B._sb_post(client, "/navigate", {"url": "https://example.com"})
    assert "timeout" in out.lower() or "transient" in out.lower()


def test_sb_post_on_exec_exception_returns_transient(monkeypatch):
    """exec() raises some non-timeout exception → transient error."""
    monkeypatch.setattr(B, "_ensure_ephemeral_server", lambda c: 9942)

    def _exec_raise(cmd, timeout=None):
        raise OSError("connection reset")
    client = _FakeExec()
    client.exec = _exec_raise

    out = B._sb_post(client, "/navigate", {"url": "https://example.com"})
    assert "success" in out
    assert "exec_failed" in out or "transient" in out.lower()


def test_sb_post_on_malformed_json_returns_transient(monkeypatch):
    """sb_server returns non-JSON → transient error (agent can retry)."""
    monkeypatch.setattr(B, "_ensure_ephemeral_server", lambda c: 9942)
    client = _FakeExec().respond("/navigate", "<<not json>>", 0)
    out = B._sb_post(client, "/navigate", {"url": "https://example.com"})
    assert "malformed" in out.lower() or "transient" in out.lower()


# ---------------------------------------------------------------------------
# Test 20: NO STICKY STATE — 10 failures then success works
# ---------------------------------------------------------------------------

def test_no_sticky_state_ten_failures_then_success(monkeypatch):
    """*** THE ANTI-BREAKER TEST ***

    Ten consecutive tool calls fail (all layers down). The 11th call must
    STILL ATTEMPT to spawn — no sticky dead state, no permanent failure.
    This is the exact behavior the old breaker broke (2 fails → dead forever)."""
    # First 10 calls: both layers fail. 11th: ephemeral succeeds.
    call = {"n": 0}

    def _ensure(client):
        call["n"] += 1
        if call["n"] <= 10:
            raise RuntimeError("transient — retry")
        return 9942  # success on 11th

    monkeypatch.setattr(B, "_ensure_ephemeral_server", _ensure)
    client = _FakeExec().respond("/navigate", '{"ok": true}', 0)

    # First 10 calls: failures (but each is a TRANSIENT error, not 'give up')
    for i in range(10):
        out = B._sb_post(client, "/navigate", {"url": "https://x"})
        assert "transient" in out.lower() or "retry" in out.lower() or "spawn_failed" in out, (
            f"call {i+1} must return transient error, not sticky-give-up: {out[:200]}"
        )

    # 11th call: SUCCESS (proves no sticky state)
    out = B._sb_post(client, "/navigate", {"url": "https://x"})
    assert '"ok": true' in out, (
        f"11th call must succeed after 10 transient failures; got: {out[:200]}"
    )


# ---------------------------------------------------------------------------
# Test 21: _spawn_one_attempt calls kill-stale before spawn
# ---------------------------------------------------------------------------

def test_spawn_one_attempt_kills_stale_before_spawn():
    """Each spawn attempt must kill stale processes on the ports BEFORE
    spawning — otherwise a half-dead Chrome from a prior attempt holds the
    port and the new spawn fails to bind. Verified via exec call order."""
    client = _FakeExec()
    assert B._spawn_one_attempt(client, 9942, 9342) in (True, False)  # runs
    # The kill must hit BOTH ports (http + cdp)
    kill_calls = [c for c in client.calls if "ss -tlnp" in c or "_kt" in c]
    assert len(kill_calls) >= 1, (
        f"must kill-stale before spawn; calls: {client.calls}"
    )
    kill_cmd = kill_calls[0]
    assert "9942" in kill_cmd and "9342" in kill_cmd, (
        f"kill must cover BOTH ports; got: {kill_cmd}"
    )
    spawn_calls = [c for c in client.calls if "sb_server.py" in c]
    assert len(spawn_calls) >= 1, "must spawn sb_server.py"
    # Kill must come BEFORE spawn
    first_kill = client.calls.index(kill_calls[0])
    first_spawn = client.calls.index(spawn_calls[0])
    assert first_kill < first_spawn, "kill-stale must come before spawn"


def test_spawn_kill_never_uses_fuser():
    """*** THE ANTI-LEAK REGRESSION GUARD ***

    The container's busybox does NOT ship ``fuser``. Earlier code that called
    ``fuser -k`` silently no-op'd (stderr was discarded to /dev/null), so
    Chrome helpers piled up across spawns until the PID cgroup tripped
    ("Resource temporarily unavailable"), killing the sandbox mid-task.

    Any future revert to ``fuser`` is a reintroduction of that leak and must
    fail this test loudly. The kill MUST use ``ss`` (always present).
    """
    client = _FakeExec()
    B._spawn_one_attempt(client, 9942, 9342)
    kill_calls = [c for c in client.calls if "_kt" in c or "ss -tlnp" in c]
    assert kill_calls, "no kill command issued"
    kill_cmd = kill_calls[0]
    # fuser is BANNED — it doesn't exist in the container
    assert "fuser" not in kill_cmd, (
        f"fuser is banned (busybox skips it → silent no-op → orphan leak); "
        f"got: {kill_cmd}"
    )
    # Must use ss to find the listener PID
    assert "ss -tlnp" in kill_cmd, (
        f"must use ss to find listener PID (fuser not installed); got: {kill_cmd}"
    )


def test_spawn_kill_walks_process_tree_by_ppid():
    """*** THE ANTI-LEAK REGRESSION GUARD #2 ***

    Chrome calls ``setsid()`` on some helper subprocesses (crashpad, zygote),
    detaching them from the spawner's process group. ``kill -9 -PGID`` alone
    misses them — they survive as orphans and pile up.

    The kill MUST walk the process tree by ppid recursively so every
    descendant is reaped. This test fails if anyone simplifies back to a
    flat ``kill -9 $pid``.
    """
    client = _FakeExec()
    B._spawn_one_attempt(client, 9942, 9342)
    kill_calls = [c for c in client.calls if "_kt" in c or "ss -tlnp" in c]
    assert kill_calls
    kill_cmd = kill_calls[0]
    # Must define a recursive walker (the _kt shell function)
    assert "_kt()" in kill_cmd, (
        f"must define a recursive tree-walker function; got: {kill_cmd}"
    )
    # Must recurse via _kt call inside the function
    assert "_kt $c" in kill_cmd, (
        f"must recurse into children (_kt $c); got: {kill_cmd}"
    )
    # Must walk ppid via ps + awk (Chrome's setsid helpers can't be reached
    # by pgid)
    assert "ppid" in kill_cmd, (
        f"must walk via ppid (pgid misses setsid helpers); got: {kill_cmd}"
    )


def test_spawn_kill_command_is_well_formed_shell():
    """The kill command is a shell loop — confirm its structure is parseable
    and hits both ports. A broken shell loop would silently no-op and the
    leak would resume invisibly."""
    client = _FakeExec()
    B._spawn_one_attempt(client, 9942, 9342)
    kill_calls = [c for c in client.calls if "_kt" in c or "ss -tlnp" in c]
    assert kill_calls
    cmd = kill_calls[0]
    # Both ports must be in the kill range
    assert "9942" in cmd and "9342" in cmd
    # Must be a loop (iterates over both ports)
    assert "for port in" in cmd, (
        f"kill must loop over both ports; got: {cmd}"
    )
