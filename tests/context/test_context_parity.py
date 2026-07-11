"""Context-mode parity — tests for the gap-closing surface.

Covers the NEW tools + EventStore methods added to close the gap vs the
context-mode MCP plugin (see GAPS-context-mode-parity.md):

- ctx_index / EventStore.stash_blob round-trip (gap 3)
- ctx_stats / EventStore.stats (gap 6)
- ctx_doctor (gap 6)
- ctx_purge / EventStore.purge (gap 6)

All tests use tmp_path EventStores — no Docker, no model, no network.
"""
from __future__ import annotations

import json

from langchain_core.tools import StructuredTool

from pux_harness.context.events import EventStore
from pux_harness.context.tools import build_context_tools


# -- helpers ------------------------------------------------------------------

def _names(tools: list) -> set[str]:
    return {t.name for t in tools}


def _invoke(tool: StructuredTool, **kwargs) -> str:
    """Invoke a StructuredTool by name via its kwargs (works for both function
    and from_function constructions)."""
    return tool.invoke(kwargs)


# -- build_context_tools surface ---------------------------------------------

def test_build_context_tools_ships_all_six_names():
    """The tool surface grew from 2 to 6; the registry must list them all."""
    tools = build_context_tools(EventStore(":memory:"))
    assert _names(tools) == {
        "ctx_recall", "ctx_search", "ctx_index",
        "ctx_stats", "ctx_doctor", "ctx_purge",
    }


# -- gap 3: ctx_index ---------------------------------------------------------

def test_ctx_index_stashes_and_returns_handle(tmp_path):
    """ctx_index parks content as a blob and returns a ctx:<id> handle."""
    store = EventStore(tmp_path / "e.db")
    tools = {t.name: t for t in build_context_tools(store)}
    out = _invoke(tools["ctx_index"], content="hello world", source="probe")
    assert "ctx:" in out
    # The handle round-trips through ctx_recall.
    handle = out.split("handle ")[1].split(".")[0].strip()
    assert _invoke(tools["ctx_recall"], handle=handle) == "hello world"


def test_ctx_index_empty_content_noops(tmp_path):
    store = EventStore(tmp_path / "e.db")
    tools = {t.name: t for t in build_context_tools(store)}
    out = _invoke(tools["ctx_index"], content="", source="")
    assert "nothing indexed" in out


def test_ctx_indexed_content_is_searchable(tmp_path):
    """ctx_index + ctx_search round-trip: the indexed content surfaces in a
    later search by a distinctive phrase."""
    store = EventStore(tmp_path / "e.db")
    tools = {t.name: t for t in build_context_tools(store)}
    _invoke(
        tools["ctx_index"],
        content="The cleanup pattern: return a teardown function from useEffect.",
        source="react-docs",
    )
    hits = _invoke(tools["ctx_search"], query="cleanup useEffect teardown")
    assert "cleanup" in hits.lower()


# -- gap 6: ctx_stats ---------------------------------------------------------

def test_ctx_stats_reflects_capture_and_offload(tmp_path):
    """stats() reports event + blob counts that match what we wrote."""
    store = EventStore(tmp_path / "e.db")
    store.capture("tool_call", {"tool": "execute", "args": "ls"}, thread_id="t1")
    store.capture("error", {"error": "boom"}, thread_id="t1")
    store.stash_blob("a" * 100, tool="big_op", thread_id="t1")
    store.flush()
    stats = store.stats()
    assert stats["events"] == 2
    assert stats["blobs"] == 1
    assert stats["blob_chars"] == 100
    assert stats["events_by_type"].get("tool_call") == 1
    assert stats["events_by_type"].get("error") == 1
    assert "t1" in stats["threads"]
    assert stats["db_bytes"] > 0
    assert isinstance(stats["fts5"], bool)


def test_ctx_stats_thread_scoped(tmp_path):
    """stats(thread_id=...) only counts that thread's rows."""
    store = EventStore(tmp_path / "e.db")
    store.capture("tool_call", {"tool": "x"}, thread_id="t1")
    store.capture("tool_call", {"tool": "y"}, thread_id="t2")
    store.flush()
    assert store.stats(thread_id="t1")["events"] == 1
    assert store.stats(thread_id="t2")["events"] == 1
    assert store.stats()["events"] == 2


def test_ctx_stats_tool_returns_json(tmp_path):
    """The agent-facing tool returns a JSON string the model can parse."""
    store = EventStore(tmp_path / "e.db")
    tools = {t.name: t for t in build_context_tools(store)}
    payload = _invoke(tools["ctx_stats"])
    parsed = json.loads(payload)
    assert parsed["events"] == 0
    assert parsed["blobs"] == 0


# -- gap 6: ctx_purge ---------------------------------------------------------

def test_ctx_purge_all_clears_events_and_blobs(tmp_path):
    store = EventStore(tmp_path / "e.db")
    store.capture("tool_call", {"tool": "x"}, thread_id="t1")
    store.capture("tool_call", {"tool": "y"}, thread_id="t2")
    store.stash_blob("payload", tool="op", thread_id="t1")
    store.flush()

    out = store.purge()
    assert out == {"events_deleted": 2, "blobs_deleted": 1}
    assert store.stats()["events"] == 0
    assert store.stats()["blobs"] == 0


def test_ctx_purge_thread_scoped_leaves_others(tmp_path):
    store = EventStore(tmp_path / "e.db")
    store.capture("tool_call", {"tool": "x"}, thread_id="t1")
    store.capture("tool_call", {"tool": "y"}, thread_id="t2")
    store.stash_blob("a", tool="op", thread_id="t1")
    store.stash_blob("b", tool="op", thread_id="t2")
    store.flush()

    out = store.purge(thread_id="t1")
    assert out == {"events_deleted": 1, "blobs_deleted": 1}
    # t2 is untouched.
    assert store.stats(thread_id="t2")["events"] == 1
    assert store.stats(thread_id="t2")["blobs"] == 1
    # And the FTS index for t2 is still searchable (blobs_fts is standalone —
    # the purge must clear only matching rows, not nuke the whole table).
    hits = store.search_context("b", thread_id="t2")
    assert hits, "thread-scoped purge must not orphan other threads' FTS rows"


def test_ctx_purge_leaves_session_resume(tmp_path):
    """Compaction snapshots are recovery state — purge must NOT drop them."""
    store = EventStore(tmp_path / "e.db")
    store.capture("tool_call", {"tool": "x"}, thread_id="t1")
    store.upsert_resume("t1", "<snapshot/>", 1)
    store.flush()

    store.purge()
    resume = store.get_resume("t1")
    assert resume is not None, "session_resume snapshots survive ctx_purge"


def test_ctx_purge_tool_runs_end_to_end(tmp_path):
    """The agent-facing tool wires store.purge to a destruction ack."""
    store = EventStore(tmp_path / "e.db")
    store.capture("tool_call", {"tool": "x"}, thread_id="t1")
    store.flush()
    tools = {t.name: t for t in build_context_tools(store)}
    out = _invoke(tools["ctx_purge"], thread_id="")
    assert "Purged 1 events" in out
    assert "0 blobs" in out


# -- gap 6: ctx_doctor --------------------------------------------------------

def test_ctx_doctor_reports_ok_on_healthy_store(tmp_path):
    store = EventStore(tmp_path / "e.db")
    store.capture("tool_call", {"tool": "x"}, thread_id="t1")  # force schema init
    store.flush()
    tools = {t.name: t for t in build_context_tools(store)}
    report = _invoke(tools["ctx_doctor"])
    lines = report.splitlines()
    # At least the four checks: store_path, db_writable, fts5, schema.
    names = [ln.split(":")[0].strip() for ln in lines]
    assert any("store_path" in n for n in names)
    assert any("db_writable" in n for n in names)
    assert any("fts5" in n for n in names)
    assert any("schema" in n for n in names)
    # On a healthy store, every check is [OK].
    assert all("[OK]" in ln for ln in lines), report


def test_ctx_doctor_flags_missing_fts5(tmp_path, monkeypatch):
    """If the SQLite build lacks FTS5, ctx_doctor must surface [FAIL] fts5."""
    store = EventStore(tmp_path / "e.db")
    # Force stats() to report fts5=False (simulating a no-FTS5 build) without
    # depending on the interpreter actually lacking FTS5.
    monkeypatch.setattr(store, "stats", lambda *, thread_id="": {
        "events": 0, "blobs": 0, "blob_chars": 0, "events_by_type": {},
        "threads": [], "db_bytes": 0, "db_path": str(store.db_path),
        "fts5": False,
    })
    tools = {t.name: t for t in build_context_tools(store)}
    report = _invoke(tools["ctx_doctor"])
    fts5_line = next(ln for ln in report.splitlines() if "fts5" in ln)
    assert "[FAIL]" in fts5_line, fts5_line
    assert "LIKE" in fts5_line


# -- gaps 4 + 5: PromptCaptureMiddleware --------------------------------------

from langchain_core.messages import HumanMessage, AIMessage, SystemMessage, ToolMessage
from pux_harness.context.prompt_capture import PromptCaptureMiddleware


def _state(*msgs, thread_id: str = "t1"):
    return {"messages": list(msgs), "configurable": {"thread_id": thread_id}}


def test_prompt_capture_records_user_message(tmp_path):
    """before_model captures a new HumanMessage as a user_message event."""
    store = EventStore(tmp_path / "e.db")
    m = PromptCaptureMiddleware(store)
    state = _state(SystemMessage(content="sys"), HumanMessage(content="fix the bug"))
    m.before_model(state, runtime=None)
    store.flush()
    events = store.recent(thread_id="t1", limit=50, min_priority=10)
    types = [e.type for e in events]
    assert "user_message" in types
    um = next(e for e in events if e.type == "user_message")
    assert "fix the bug" in um.data.get("content", "")


def test_prompt_capture_skips_already_seen_messages(tmp_path):
    """A second before_model call (next loop iteration) must NOT re-capture the
    same HumanMessage — the watermark advances past it."""
    store = EventStore(tmp_path / "e.db")
    m = PromptCaptureMiddleware(store)
    state = _state(HumanMessage(content="prompt-A"))
    m.before_model(state, runtime=None)
    # Simulate the next model iteration: same messages, no new human.
    m.before_model(state, runtime=None)
    store.flush()
    um_count = sum(1 for e in store.recent(thread_id="t1", limit=50, min_priority=10)
                   if e.type == "user_message")
    assert um_count == 1, "watermark must prevent duplicate capture"


def test_prompt_capture_thread_isolation(tmp_path):
    """Two threads in one process get independent watermarks — t2's messages
    don't leak into t1's capture and vice versa."""
    store = EventStore(tmp_path / "e.db")
    m = PromptCaptureMiddleware(store)
    m.before_model(_state(HumanMessage(content="t1-msg"), thread_id="t1"), runtime=None)
    m.before_model(_state(HumanMessage(content="t2-msg"), thread_id="t2"), runtime=None)
    store.flush()
    t1 = [e for e in store.recent(thread_id="t1", limit=50, min_priority=10)
          if e.type == "user_message"]
    t2 = [e for e in store.recent(thread_id="t2", limit=50, min_priority=10)
          if e.type == "user_message"]
    assert len(t1) == 1 and "t1-msg" in t1[0].data["content"]
    assert len(t2) == 1 and "t2-msg" in t2[0].data["content"]


def test_prompt_capture_disabled_noops(tmp_path):
    store = EventStore(tmp_path / "e.db")
    m = PromptCaptureMiddleware(store, enabled=False)
    m.before_model(_state(HumanMessage(content="hi")), runtime=None)
    m.after_agent(_state(AIMessage(content="bye")), runtime=None)
    store.flush()
    assert store.stats()["events"] == 0


def test_prompt_capture_no_thread_id_drops(tmp_path):
    """Parity with session_guide: no thread_id → can't scope → drop."""
    store = EventStore(tmp_path / "e.db")
    m = PromptCaptureMiddleware(store)
    m.before_model({"messages": [HumanMessage(content="x")]}, runtime=None)
    store.flush()
    assert store.stats()["events"] == 0


def test_turn_end_captures_final_ai_response(tmp_path):
    """after_agent captures the last AIMessage (no pending tool_calls) as a
    turn_end event."""
    store = EventStore(tmp_path / "e.db")
    m = PromptCaptureMiddleware(store)
    state = _state(
        HumanMessage(content="hello"),
        AIMessage(content="", tool_calls=[{"name": "x", "args": {}, "id": "1"}]),
        ToolMessage(content="result", tool_call_id="1"),
        AIMessage(content="Here is the final answer."),
    )
    m.after_agent(state, runtime=None)
    store.flush()
    te = [e for e in store.recent(thread_id="t1", limit=50, min_priority=10)
          if e.type == "turn_end"]
    assert len(te) == 1
    assert "final answer" in te[0].data["content"]


def test_turn_end_skips_intermediate_toolcall_ai(tmp_path):
    """An AIMessage WITH pending tool_calls is an intermediate decision, not a
    turn-end. after_agent must scan back past it to the real final response."""
    store = EventStore(tmp_path / "e.db")
    m = PromptCaptureMiddleware(store)
    intermediate = AIMessage(content="thinking", tool_calls=[{"name": "x", "args": {}, "id": "1"}])
    final = AIMessage(content="done")
    state = _state(HumanMessage(content="q"), intermediate, final)
    m.after_agent(state, runtime=None)
    store.flush()
    te = [e for e in store.recent(thread_id="t1", limit=50, min_priority=10)
          if e.type == "turn_end"]
    assert len(te) == 1
    assert te[0].data["content"] == "done"


def test_turn_end_none_when_only_toolcall_ai(tmp_path):
    """If the agent ended mid-tool-call (no final response), nothing to capture."""
    store = EventStore(tmp_path / "e.db")
    m = PromptCaptureMiddleware(store)
    state = _state(
        HumanMessage(content="q"),
        AIMessage(content="", tool_calls=[{"name": "x", "args": {}, "id": "1"}]),
    )
    m.after_agent(state, runtime=None)
    store.flush()
    assert all(e.type != "turn_end" for e in store.recent(thread_id="t1", limit=50, min_priority=10))


def test_captured_user_message_is_searchable(tmp_path):
    """The point of the capture: a user prompt survives in the store and is
    recoverable via ctx_search after the working window has moved on."""
    store = EventStore(tmp_path / "e.db")
    tools = {t.name: t for t in build_context_tools(store)}
    m = PromptCaptureMiddleware(store)
    m.before_model(_state(HumanMessage(content="please refactor the auth module")), runtime=None)
    # after_agent bumps the watermark too; not needed here but realistic.
    hits = _invoke(tools["ctx_search"], query="refactor auth")
    assert "refactor" in hits.lower()


# -- gap 4+5 wiring: PromptCaptureMiddleware is a default-on supervisor spec ---

def test_prompt_capture_is_in_registry_defaults_and_correct_position():
    """The middleware is wired into the stack: registered, default-on for
    supervisors, supervisor-only (subagents don't receive user prompts), and
    mounted AFTER session_guide so a compaction snapshot sees the latest
    user_message + turn_end events."""
    from pux_harness.agent import stack
    names = stack.middleware_names()
    assert "prompt_capture" in names
    assert "prompt_capture" in stack.DEFAULT_SUPERVISOR
    by_name = {s.name: s for s in stack.MIDDLEWARE_REGISTRY}
    assert by_name["prompt_capture"].scope == {stack.Scope.SUPERVISOR}
    # Mount-order check: registry order is canonical.
    ordered = [s.name for s in stack.MIDDLEWARE_REGISTRY]
    assert ordered.index("prompt_capture") > ordered.index("session_guide")
    assert ordered.index("prompt_capture") < ordered.index("rubric")


# -- gap 7: UNIVERSAL forcing surface (deny fires on EVERY tool call) ---------
#
# The routing middleware's deny path is now UNIVERSAL — every tool call's
# string args are scanned, not just execute/bash/pux_sandbox_execute/pux_
# sandbox_python. This is the parity surface with context-mode's PreToolUse
# hook, which fires on all tools. A declared dynamic tool (pux_sandbox_*), an
# MCP tool, or any future tool whose args carry a curl/requests.get pattern
# gets denied the same as execute("curl ...") would. The intercept_tools
# gate is GONE for deny; it survives only for the declared-redirect path
# (redirects are meaningless for non-exec tools).
#
# The shell patterns are tightened with (?=\s+\S) so the BARE word "curl" in a
# non-exec arg (grep("curl"), documentation text) does NOT false-positive.

from types import SimpleNamespace

import pytest

from pux_harness.context.sandbox_routing import (
    RoutingMiddleware,
    _DENY_MSG,
    _PY_DENY_MSG,
    _INTERCEPT_TOOLS,
)


def _py_req(code: str, tcid: str = "call_py") -> SimpleNamespace:
    """A pux_sandbox_python ToolCallRequest stand-in carrying a ``code`` arg."""
    return SimpleNamespace(
        tool_call={"name": "pux_sandbox_python", "args": {"code": code}, "id": tcid},
        state={},
    )


def _shell_req(command: str, name: str = "execute", tcid: str = "call_sh") -> SimpleNamespace:
    return SimpleNamespace(
        tool_call={"name": name, "args": {"command": command}, "id": tcid},
        state={},
    )


def _ran() -> tuple:
    """A handler that records it ran (deny/redirect paths never call it)."""
    box: list = []
    return box, (lambda _r: box.append(True) or ToolMessage(content="ran", tool_call_id="x", name="x"))


# --- UNIVERSAL deny: fires on ANY tool, not just the hardcoded set -----------

def test_intercept_tools_no_longer_gates_deny():
    """The deny path is universal — intercept_tools now scopes ONLY the
    declared-redirect path (exec tools). pux_sandbox_python is NOT in the set
    (it was in v1 of gap 7; the allowlist approach was wrong). Deny fires on
    it anyway because deny scans ALL tools."""
    assert "pux_sandbox_python" not in _INTERCEPT_TOOLS
    # But deny still fires on it (proven by test_python_network_calls_are_denied
    # below). The allowlist is dead for deny.


def test_extract_scan_pulls_all_string_args():
    """_extract_scan concatenates ALL string-valued args — not just
    command/cmd/code. A declared tool with a ``url`` or ``query`` arg is
    scanned the same as execute(command=...)."""
    mw = RoutingMiddleware()
    req = SimpleNamespace(
        tool_call={"name": "pux_sandbox_custom", "args": {
            "url": "https://example.com",
            "method": "GET",
            "count": 42,  # non-string, must be skipped
            "nested": {"a": 1},  # non-string, skipped
        }, "id": "c1"},
        state={},
    )
    scan = mw._extract_scan(req)
    assert "https://example.com" in scan
    assert "GET" in scan
    assert "42" not in scan  # int arg skipped


def test_deny_fires_on_unknown_tool_not_in_any_hardcoded_set():
    """PARITY: an arbitrary tool the middleware has never heard of (a declared
    dynamic tool, an MCP tool, a future tool) is denied when its args carry a
    network-fetch pattern. This is the gap that the allowlist approach left
    open — the whole point of universal deny."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    # A declared dynamic tool that the org exposed — NOT in _INTERCEPT_TOOLS.
    req = SimpleNamespace(
        tool_call={"name": "pux_sandbox_fetch_signals",
                   "args": {"ticker": "AAPL", "script": "requests.get('https://api.x.com/data')"},
                   "id": "c1"},
        state={},
    )
    out = mw.wrap_tool_call(req, handler=handler)
    assert box == [], "deny must fire on tools outside the hardcoded set"
    assert out.content == _PY_DENY_MSG


def test_deny_fires_on_mcp_shaped_tool():
    """PARITY: an MCP tool (prefixed mcp__server__tool) is denied the same as a
    native tool when its args carry curl/network egress."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    req = SimpleNamespace(
        tool_call={"name": "mcp__github__run_action",
                   "args": {"cmd": "curl https://exfil.example/data"},
                   "id": "c1"},
        state={},
    )
    out = mw.wrap_tool_call(req, handler=handler)
    assert box == []
    assert out.content == _DENY_MSG  # shell family (curl)


def test_deny_fires_on_tool_with_url_arg_containing_python_pattern():
    """PARITY: a tool whose ``url`` arg contains a Python network pattern is
    denied — the scan reads ALL string args, not just command/code."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    req = SimpleNamespace(
        tool_call={"name": "pux_sandbox_process",
                   "args": {"payload": "import httpx; httpx.get('https://x.example')"},
                   "id": "c1"},
        state={},
    )
    out = mw.wrap_tool_call(req, handler=handler)
    assert box == []
    assert out.content == _PY_DENY_MSG


# --- no false positive on bare "curl" word (tightened shell patterns) --------

def test_grep_for_curl_word_does_not_false_positive():
    """The shell curl pattern requires ``curl`` + space + content (a URL or
    flag). ``grep("curl")`` — bare word, no trailing arg — must NOT be denied.
    This is what makes universal scanning viable: the pattern is specific
    enough to not trip on documentation/search args."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    # grep with pattern="curl" — the scan string is just "curl", no URL/flag.
    req = SimpleNamespace(
        tool_call={"name": "grep", "args": {"pattern": "curl"}, "id": "c1"},
        state={},
    )
    out = mw.wrap_tool_call(req, handler=handler)
    assert box == [True], "bare word 'curl' must not false-positive"
    assert out.content == "ran"


def test_grep_for_curl_in_command_arg_does_not_false_positive():
    """``execute("grep -r 'curl' src/")`` — the word ``curl`` appears inside
    quotes (grep pattern), NOT followed by space+URL. Tightened pattern doesn't
    match. Shell deny must NOT fire."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    out = mw.wrap_tool_call(
        _shell_req("grep -r 'curl' src/"), handler=handler)
    assert box == [True]
    assert out.content == "ran"


# --- Python network egress is denied (universal scan catches it) -------------

@pytest.mark.parametrize("code,needle", [
    ("import requests\nrequests.get('https://example.com')", "requests.get"),
    ("import requests\nr = requests.post('https://example.com', data={})", "requests.post"),
    ("import urllib.request\nurllib.request.urlopen('https://example.com')", "urlopen"),
    ("from urllib.request import urlopen\nurlopen('https://example.com')", "urlopen"),
    ("import urllib.request\nurllib.request.urlretrieve('https://example.com', '/tmp/o')", "urlretrieve"),
    ("import httpx\nhttpx.get('https://example.com')", "httpx.get"),
    ("import httpx\nhttpx.Client().get('https://example.com')", "httpx.Client"),
    ("import subprocess\nsubprocess.run(['curl', 'https://example.com'])", "curl"),
    ("import subprocess\nsubprocess.run('wget https://example.com', shell=True)", "wget"),
    ("import os\nos.system('curl -s https://example.com')", "curl"),
    ("import os\nos.popen('wget https://example.com')", "wget"),
])
def test_python_network_calls_are_denied(code, needle):
    """Each of these dumps a response body into context if allowed to run; the
    universal scan catches them via the Python deny list and emits _PY_DENY_MSG.
    These fire on pux_sandbox_python even though it's NOT in intercept_tools —
    deny is universal."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    out = mw.wrap_tool_call(_py_req(code), handler=handler)
    assert box == [], f"handler ran for code mentioning {needle!r} — deny failed"
    assert out.content == _PY_DENY_MSG


def test_python_deny_is_async_too():
    """The async wrap (used by the server/runner ainvoke path) denies the same
    Python network calls — no async-only bypass."""
    import asyncio
    mw = RoutingMiddleware()
    box = []

    async def handler(_r):
        box.append(True)
        return ToolMessage(content="ran", tool_call_id="x", name="x")

    out = asyncio.run(mw.awrap_tool_call(
        _py_req("requests.get('https://example.com')"), handler=handler))
    assert box == []
    assert out.content == _PY_DENY_MSG


def test_python_deny_logs_routing_denied_event(tmp_path, monkeypatch):
    """A Python deny is observability-equivalent to a shell deny: a
    routing_denied event lands in the store tagged with deny_family=python."""
    from pux_harness.context import sandbox_routing as sr
    store = EventStore(tmp_path / "e.db")
    monkeypatch.setattr(sr, "shared_event_store", lambda: store)
    mw = sr.RoutingMiddleware()
    mw.wrap_tool_call(
        _py_req("requests.get('https://example.com')"),
        handler=lambda _r: ToolMessage(content="x", tool_call_id="x", name="x"))
    store.flush()
    denied = store.recent(event_type="routing_denied")
    assert len(denied) == 1
    assert denied[0].data["deny_family"] == "python"
    assert denied[0].data["tool"] == "pux_sandbox_python"


# --- documented false-positive tradeoff (parity with context-mode) -----------

def test_universal_scan_false_positive_on_grep_for_python_pattern():
    """PARITY TRADEOFF: universal scanning means ``execute("grep -r
    'requests.get(' src/")`` IS denied — the scan string contains
    ``requests.get(`` which matches the Python deny pattern. This is a false
    positive (the agent is searching code, not fetching a URL). context-mode's
    PreToolUse has the same tradeoff — it pattern-matches all tools too. The
    agent workaround: grep for a substring that doesn't look like a fetch
    (e.g. ``grep -rF 'requests.get'`` without the paren, or search for a
    distinctive local string)."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    out = mw.wrap_tool_call(
        _shell_req("grep -r 'requests.get(' src/"), handler=handler)
    # This IS denied — the false positive is the cost of universal scanning.
    assert box == [], "universal scan catches grep-for-code-pattern (tradeoff)"
    assert out.content == _PY_DENY_MSG  # tagged python (requests.get pattern)


# --- innocuous code passes through ------------------------------------------

def test_innocuous_python_code_passes_through():
    """Non-network Python (math, parsing, file IO) is not denied — the deny
    list targets network egress, not arbitrary code execution."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    out = mw.wrap_tool_call(
        _py_req("import json\nprint(json.dumps({'a': 1}))"),
        handler=handler)
    assert box == [True]
    assert out.content == "ran"


def test_python_deny_can_be_disabled_per_org():
    """An org can ship its own py_deny_patterns (incl. empty) via profile.yaml
    routing config — e.g. an org that wants to allow requests.get inside the
    sandbox. The default is the broad deny, but it is overrideable."""
    mw = RoutingMiddleware(py_deny_patterns=[])
    box, handler = _ran()
    out = mw.wrap_tool_call(
        _py_req("requests.get('https://example.com')"),
        handler=handler)
    assert box == [True], "empty py_deny_patterns disables the Python deny"


# --- shell deny regression (unchanged by universal scanning) -----------------

def test_shell_curl_deny_still_fires_and_uses_shell_message():
    """The shell curl deny is byte-identical to before: same _DENY_MSG, same
    routing_denied event, handler never runs. Universal scanning did not
    narrow the shell side."""
    mw = RoutingMiddleware()
    box, handler = _ran()
    out = mw.wrap_tool_call(
        _shell_req("curl https://example.com"), handler=handler)
    assert box == []
    assert out.content == _DENY_MSG  # the SHELL message, not _PY_DENY_MSG


def test_shell_curl_deny_logs_shell_family(tmp_path, monkeypatch):
    """Shell deny events are tagged deny_family=shell (Python deny events are
    deny_family=python) — lets a dashboard split the two deny surfaces."""
    from pux_harness.context import sandbox_routing as sr
    store = EventStore(tmp_path / "e.db")
    monkeypatch.setattr(sr, "shared_event_store", lambda: store)
    mw = sr.RoutingMiddleware()
    mw.wrap_tool_call(
        _shell_req("curl https://example.com"),
        handler=lambda _r: ToolMessage(content="x", tool_call_id="x", name="x"))
    store.flush()
    denied = store.recent(event_type="routing_denied")
    assert len(denied) == 1
    assert denied[0].data["deny_family"] == "shell"
    assert denied[0].data["tool"] == "execute"
