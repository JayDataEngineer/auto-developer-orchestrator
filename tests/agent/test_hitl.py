"""``ask_user`` HITL tool — the transport-aware human-in-the-loop surface.

Proves (offline, no Docker/model):

- the tool body BRANCHES on transport: web (``serve`` / ``agui``) → native
  langgraph ``interrupt()`` (the client's resume value becomes the tool result);
  editor (``acp`` / ``direct`` / ``tui``) → poses the question + ends the turn
  (``interrupt`` must NOT be called — the editor permission popover has no
  free-text field, so an interrupt the client can't resume would dead-end);
- ``ask_user_turn_based`` is the transport predicate the supervisor-prompt
  suffix keys on;
- ``load_ask_user_enabled`` reads the ``ask_user:`` profile flag (true / false /
  malformed → loud failure).

The CONSTRUCTION gate (opt-in AND not mcp/autonomous) + the prompt-suffix append
are proven in ``test_stack.py`` against the real ``build_stack`` factory. The
interrupt-pauses-and-resumes mechanism under deepagents is proven by the Phase A
spike (``/tmp/spike_interrupt_in_tool.py``) + the live web proof (Phase D).
"""
from __future__ import annotations

from pathlib import Path

import pytest

from pux_harness.agent import hitl, orgs, profile


# --- the tool body: web branch interrupts, editor branch poses ---------------

def test_web_branch_calls_interrupt_and_returns_reply(monkeypatch):
    """Over the web the tool calls ``interrupt()``; the value the client resumes
    with becomes the tool result. Mock ``interrupt`` so the tool runs without a
    real graph + capture the payload the CopilotKit ``useInterrupt`` card would
    receive (question + options + default, verbatim)."""
    captured: dict = {}

    def fake_interrupt(payload):
        captured["payload"] = payload
        return "blue"

    monkeypatch.setattr(hitl, "interrupt", fake_interrupt)
    tool = hitl.make_ask_user_tool("serve")
    result = tool.invoke({
        "question": "Pick a color",
        "options": ["red", "blue"],
        "default": "red",
    })
    assert result == "blue"
    assert captured["payload"] == {
        "question": "Pick a color",
        "options": ["red", "blue"],
        "default": "red",
    }


def test_agui_is_also_an_interrupt_transport(monkeypatch):
    """``agui`` (the CopilotKit SSE mount) is a web surface too — same interrupt
    branch as ``serve`` (both run in the long-lived server process)."""
    monkeypatch.setattr(hitl, "interrupt", lambda payload: "yes")
    tool = hitl.make_ask_user_tool("agui")
    assert tool.invoke({"question": "ok?"}) == "yes"


def test_editor_branch_poses_question_without_interrupt(monkeypatch):
    """Over an editor there is no resumable free-text interrupt. The tool poses
    the question as its result + a turn-end marker; the supervisor prompt suffix
    (proven in test_stack.py) makes the agent stop. ``interrupt`` must NOT fire."""
    called = {"n": 0}

    def boom(_payload):  # would fail the test if the editor branch hit it
        called["n"] += 1
        raise AssertionError("editor branch must not interrupt()")

    monkeypatch.setattr(hitl, "interrupt", boom)
    tool = hitl.make_ask_user_tool("acp")
    result = tool.invoke({
        "question": "Ship now or wait?",
        "options": ["ship", "wait"],
        "default": "wait",
    })
    assert called["n"] == 0
    assert "Ship now or wait?" in result
    assert "ship" in result and "wait" in result
    assert "wait for the user's reply" in result.lower()


def test_editor_branch_without_options():
    """No ``options`` → the tool still poses the question with an open-answer
    marker (the agent isn't forced into a bounded choice)."""
    tool = hitl.make_ask_user_tool("direct")
    result = tool.invoke({"question": "What should I name it?"})
    assert "What should I name it?" in result
    assert "any answer" in result


def test_tool_name_is_ask_user():
    """The construction gate + supervisor prompt reference the tool by this name;
    pin it so a rename doesn't silently desync the two."""
    assert hitl.ASK_USER_NAME == "ask_user"
    assert hitl.make_ask_user_tool("serve").name == "ask_user"


# --- the transport predicate ------------------------------------------------

@pytest.mark.parametrize("transport,turn_based", [
    ("serve", False),
    ("agui", False),
    ("acp", False),  # ACP uses interrupt() resume — not turn-based
    ("direct", True),
    ("tui", True),
    ("mcp", True),
])
def test_ask_user_turn_based_predicate(transport, turn_based):
    """Web surfaces (serve/agui) interrupt; everything else is turn-based. The
    supervisor prompt suffix is appended ONLY when this is ``True`` — over the
    web the interrupt pause already gates the reply."""
    assert hitl.ask_user_turn_based(transport) is turn_based


# --- load_ask_user_enabled: the ORG opt-in half of the gate -----------------

@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """Minimal scratch orgs/ tree so ``load_ask_user_enabled`` reads a real
    ``profile.yaml``. Only the ``_orgs_dir`` patch + org ``p`` dir are needed —
    the loader reads no agents/skills."""
    (tmp_path / "orgs").mkdir()
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    (tmp_path / "orgs" / "p").mkdir(parents=True)
    return tmp_path


def _write_profile(fake_tree: Path, body: str) -> None:
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(body)


def test_ask_user_flag_absent_means_disabled(fake_tree):
    """No ``ask_user:`` flag (and no profile.yaml at all) → False. The
    byte-identical default: no ask_user tool anywhere in the stack."""
    assert profile.load_ask_user_enabled("p") is False


def test_ask_user_flag_true_opts_in(fake_tree):
    _write_profile(fake_tree, "ask_user: true\n")
    assert profile.load_ask_user_enabled("p") is True


def test_ask_user_flag_false_is_disabled(fake_tree):
    _write_profile(fake_tree, "ask_user: false\n")
    assert profile.load_ask_user_enabled("p") is False


def test_ask_user_flag_non_bool_raises(fake_tree):
    """A malformed flag fails loud (no silent skip) — and surfaces through
    ``validate_profile`` so it breaks ``--check-contract`` too."""
    _write_profile(fake_tree, "ask_user: maybe\n")
    with pytest.raises(TypeError):
        profile.load_ask_user_enabled("p")
