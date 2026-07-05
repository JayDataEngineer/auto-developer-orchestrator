"""Grader tool factories (Phase 17.B.2) drive ``exec_client.exec`` with the
right command and surface exit-code + output so the grader can cite evidence.

The grader is a separate sub-agent inside ``RubricMiddleware`` — it does NOT
inherit the main agent's ``FilesystemMiddleware``, so these factories bind to
``exec_client`` directly (mirroring the ``_python_tool`` idiom). We monkeypatch
``exec_client.exec`` to capture the command + return a canned (output, code),
then invoke each constructed StructuredTool's ``_run``.

Docker-free: the factories only CAPTURE ``exec_client`` at build time and call
``exec_client.exec`` at run time — both stubbed here.
"""
from __future__ import annotations

import pytest

from pux_harness.sandbox import tools


class _FakeExec:
    """Stand-in for ``DockerExecClient``; records each command, returns a
    queued (output, exit_code) per call (or a single reply for all calls)."""

    def __init__(self, replies=None):
        self.calls: list[str] = []
        # replies: a list of (output, code) popped in order, OR a single
        # (output, code) returned for every call.
        self._replies = replies
        self._single = isinstance(replies, tuple)

    def exec(self, command, *, timeout=None):  # noqa: A003 — mirror real sig
        self.calls.append(command)
        if self._single:
            return self._replies
        return self._replies.pop(0) if self._replies else ("", 0)


def _grader_tools(exec_client):
    return {t.name: t for t in tools.build_grader_tools(exec_client)}


# --- registration ----------------------------------------------------------

def test_grader_tools_registered_with_prefixed_names():
    """All three grader tools exist, prefixed ``pux_grader_`` (distinct from the
    main agent's native fs/shell tools — no collision risk)."""
    exec_ = _FakeExec()
    names = set(_grader_tools(exec_))
    assert {
        "pux_grader_execute", "pux_grader_read_file", "pux_grader_grep",
    } <= names
    for t in tools.build_grader_tools(exec_):
        assert t.description and t.description.strip()


# --- execute ---------------------------------------------------------------

def test_grader_execute_returns_exit_code_and_output():
    """A command's exit code + output are surfaced verbatim — the grader cites
    them in its verdict (a failing suite is evidence, not a tool error)."""
    exec_ = _FakeExec(replies=("All tests passed.\n", 0))
    res = _grader_tools(exec_)["pux_grader_execute"].invoke({"command": "pytest -q"})
    assert exec_.calls == ["pytest -q"]
    assert '"success": true' in res
    assert '"exit_code": 0' in res
    assert "All tests passed." in res


def test_grader_execute_surfaces_failing_exit_code_as_success():
    """A non-zero exit (failing tests) is still success=true — the grader asked
    to run a command and got an answer; the FAILURE is in exit_code, which the
    grader reads to mark the rubric clause unsatisfied."""
    exec_ = _FakeExec(replies=("1 failed\n", 1))
    res = _grader_tools(exec_)["pux_grader_execute"].invoke({"command": "pytest"})
    assert '"success": true' in res
    assert '"exit_code": 1' in res
    assert "1 failed" in res


def test_grader_execute_requires_command():
    exec_ = _FakeExec()
    res = _grader_tools(exec_)["pux_grader_execute"].invoke({"command": ""})
    assert exec_.calls == []  # never reached exec
    assert "no command provided" in res


# --- read_file -------------------------------------------------------------

def test_grader_read_file_returns_content():
    exec_ = _FakeExec(replies=("file body here\n", 0))
    res = _grader_tools(exec_)["pux_grader_read_file"].invoke({"path": "src/x.py"})
    # Path is shell-quoted in the cat command.
    assert exec_.calls == ["cat src/x.py"]
    assert '"success": true' in res
    assert "file body here" in res


def test_grader_read_file_missing_path_is_error():
    """A missing file (cat non-zero) is a tool error (distinct from a command's
    non-zero exit in execute) — the grader asked for a file that isn't there."""
    exec_ = _FakeExec(replies=("No such file\n", 1))
    res = _grader_tools(exec_)["pux_grader_read_file"].invoke({"path": "ghost.py"})
    assert '"success": false' in res
    assert "cat exited 1" in res


def test_grader_read_file_requires_path():
    res = _grader_tools(_FakeExec())["pux_grader_read_file"].invoke({"path": ""})
    assert "no path provided" in res


# --- grep ------------------------------------------------------------------

def test_grader_grep_default_workspace_recursive():
    exec_ = _FakeExec(replies=("src/x.py:42:TODO\n", 0))
    res = _grader_tools(exec_)["pux_grader_grep"].invoke({"pattern": "TODO"})
    assert exec_.calls == ["grep -rn TODO /sandbox/workspace"]
    assert '"success": true' in res
    assert "src/x.py:42:TODO" in res


def test_grader_grep_no_matches_is_success_empty():
    """grep exit 1 (no matches) is success with empty matches — the grader got
    an answer ('this regression marker is gone'), not a tool error."""
    exec_ = _FakeExec(replies=("", 1))
    res = _grader_tools(exec_)["pux_grader_grep"].invoke(
        {"pattern": "DEPRECATED_API"}
    )
    assert '"success": true' in res
    assert '"matches": ""' in res


def test_grader_grep_bad_pattern_is_error():
    """grep exit 2 (bad regex / missing path) is a real tool error."""
    exec_ = _FakeExec(replies=("grep: Invalid regex\n", 2))
    res = _grader_tools(exec_)["pux_grader_grep"].invoke({"pattern": "*("})
    assert '"success": false' in res
    assert "grep error" in res


def test_grader_grep_path_and_include_filter():
    """A custom path + include glob are threaded into the command."""
    exec_ = _FakeExec(replies=("hit\n", 0))
    _grader_tools(exec_)["pux_grader_grep"].invoke(
        {"pattern": "def ", "path": "src", "include": "*.py"}
    )
    assert exec_.calls == ["grep -rn 'def ' src --include='*.py'"]
