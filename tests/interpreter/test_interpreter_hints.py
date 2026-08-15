"""Unit tests for ``InterpreterHintsMiddleware``.

Verifies that:
  - eval errors emit a Command([text_tm, human_hint]) with the right
    classification + fallback for every error_type
  - eval successes pass through untouched (zero happy-path cost)
  - non-eval tools short-circuit before any scan
  - the hint names the fallback tools (glob/grep/ls/read_file/task)
"""
from __future__ import annotations

import json
from types import SimpleNamespace

import pytest
from langchain_core.messages import HumanMessage, ToolMessage
from langgraph.types import Command

from deepagents_context.interpreter_hints import (
    InterpreterHintsMiddleware,
    _classify,
    _build_hint,
    _EVAL_TOOL_NAME,
)


# --------------------------------------------------------------------------- #
# helpers
# --------------------------------------------------------------------------- #

def _req(name: str):
    """Build a minimal ToolCallRequest stand-in."""
    return SimpleNamespace(tool_call={"name": name, "args": {}, "id": "tc_1"})


def _text_tm(content: str, *, name: str = _EVAL_TOOL_NAME,
             tool_call_id: str = "tc_1") -> ToolMessage:
    return ToolMessage(content=content, name=name, tool_call_id=tool_call_id)


def _handler_returning(value):
    def _h(_request):
        return value
    return _h


def _messages(cmd):
    """Extract messages from a Command(update={...}) or return as-is."""
    if isinstance(cmd, Command):
        return cmd.update.get("messages", [])
    return [cmd]


def _eval_error_xml(error_type: str, message: str = "boom",
                    stack: str = "at foo:1:2") -> str:
    return f'<error type="{error_type}">{message}\n{stack}</error>'


def _eval_success_xml(value: str = "42", kind: str = "number") -> str:
    return f'<result kind="{kind}">{value}</result>'


# --------------------------------------------------------------------------- #
# classification table
# --------------------------------------------------------------------------- #

class TestClassify:
    """Every known error_type yields a non-empty (classification, hint)."""

    @pytest.mark.parametrize("etype", [
        "SyntaxError", "Timeout", "OutOfMemory",
        "PTCCallBudgetExceeded", "Deadlock", "ConcurrentEval",
        "MarshalError",
    ])
    def test_known_types_classify(self, etype):
        cls, hint = _classify(etype)
        assert cls, f"{etype}: classification empty"
        assert hint, f"{etype}: hint empty"
        assert etype not in cls or etype == "MarshalError"  # type appears or is paraphrased

    @pytest.mark.parametrize("runtime_type", [
        "TypeError", "ReferenceError", "RangeError",
        "URIError", "EvalError", "SomeCustomError",
    ])
    def test_runtime_types_fallthrough(self, runtime_type):
        """Runtime JS exceptions carry the JS error name as the type."""
        cls, hint = _classify(runtime_type)
        assert runtime_type in cls, f"{runtime_type} should appear in classification"
        assert "runtime exception" in cls.lower()

    def test_syntax_error_mentions_parens_gotcha(self):
        _, hint = _classify("SyntaxError")
        assert "return ({a: 1})" in hint or "parens" in hint.lower() or "return (" in hint

    def test_timeout_mentions_budget(self):
        _, hint = _classify("Timeout")
        assert "budget" in hint.lower() or "timeout" in hint.lower() or "loop" in hint.lower()

    def test_ptc_budget_mentions_promise_all(self):
        _, hint = _classify("PTCCallBudgetExceeded")
        assert "Promise.all" in hint or "budget" in hint.lower()

    def test_hint_includes_fallback(self):
        hint = _build_hint("Timeout", ["glob", "grep", "ls", "read_file", "task"])
        assert "glob" in hint
        assert "grep" in hint
        assert "task" in hint
        assert "ErrorType: Timeout" in hint


# --------------------------------------------------------------------------- #
# wrap_tool_call: happy path (no overhead)
# --------------------------------------------------------------------------- #

class TestHappyPath:
    """Success results and non-eval tools pass through unchanged."""

    def test_eval_success_passes_through(self):
        mw = InterpreterHintsMiddleware()
        tm = _text_tm(_eval_success_xml())
        out = mw.wrap_tool_call(_req("eval"), _handler_returning(tm))
        assert out is tm, "success result must pass through untouched (identity)"

    def test_non_eval_tool_passes_through(self):
        mw = InterpreterHintsMiddleware()
        tm = _text_tm("some result", name="glob")
        out = mw.wrap_tool_call(_req("glob"), _handler_returning(tm))
        assert out is tm

    def test_browser_tool_passes_through(self):
        mw = InterpreterHintsMiddleware()
        tm = _text_tm('{"ok": true}', name="pux_sandbox_browser_navigate")
        out = mw.wrap_tool_call(
            _req("pux_sandbox_browser_navigate"), _handler_returning(tm)
        )
        assert out is tm

    def test_non_toolmessage_passes_through(self):
        mw = InterpreterHintsMiddleware()
        out = mw.wrap_tool_call(_req("eval"), _handler_returning("just a string"))
        assert out == "just a string"

    def test_empty_content_passes_through(self):
        mw = InterpreterHintsMiddleware()
        tm = _text_tm("")
        out = mw.wrap_tool_call(_req("eval"), _handler_returning(tm))
        assert out is tm


# --------------------------------------------------------------------------- #
# wrap_tool_call: error path emits Command with hint
# --------------------------------------------------------------------------- #

class TestErrorEnrichment:
    """eval errors emit Command([text_tm, human_hint])."""

    @pytest.mark.parametrize("etype", [
        "SyntaxError", "Timeout", "OutOfMemory",
        "PTCCallBudgetExceeded", "Deadlock", "ConcurrentEval",
        "TypeError", "ReferenceError",
    ])
    def test_error_emits_command_with_hint(self, etype):
        mw = InterpreterHintsMiddleware()
        tm = _text_tm(_eval_error_xml(etype))
        out = mw.wrap_tool_call(_req("eval"), _handler_returning(tm))

        msgs = _messages(out)
        assert len(msgs) == 2, f"{etype}: expected [ToolMessage, HumanMessage]"

        # text ToolMessage preserved with identity + tool_call_id
        text_tm = msgs[0]
        assert isinstance(text_tm, ToolMessage)
        assert text_tm is tm, f"{etype}: original ToolMessage must be preserved (identity)"
        assert text_tm.tool_call_id == "tc_1"
        assert text_tm.name == "eval"

        # companion HumanMessage carries the hint
        human = msgs[1]
        assert isinstance(human, HumanMessage)
        assert isinstance(human.content, list)
        assert human.content[0]["type"] == "text"
        hint_text = human.content[0]["text"]
        assert "[eval failed" in hint_text
        assert f"ErrorType: {etype}" in hint_text
        assert "glob" in hint_text  # fallback tools listed
        assert "task" in hint_text

    def test_command_is_a_command_instance(self):
        mw = InterpreterHintsMiddleware()
        tm = _text_tm(_eval_error_xml("Timeout"))
        out = mw.wrap_tool_call(_req("eval"), _handler_returning(tm))
        assert isinstance(out, Command)

    def test_hint_text_contains_classification_and_hint(self):
        mw = InterpreterHintsMiddleware()
        tm = _text_tm(_eval_error_xml("SyntaxError"))
        out = mw.wrap_tool_call(_req("eval"), _handler_returning(tm))
        human = _messages(out)[1]
        text = human.content[0]["text"]
        assert "syntax error" in text.lower()
        assert "Why:" in text
        assert "Fix the script" in text
        assert "skip eval" in text

    def test_custom_fallback_tools(self):
        mw = InterpreterHintsMiddleware(fallback_tools=["my_tool", "other"])
        tm = _text_tm(_eval_error_xml("Timeout"))
        out = mw.wrap_tool_call(_req("eval"), _handler_returning(tm))
        human = _messages(out)[1]
        text = human.content[0]["text"]
        assert "my_tool" in text
        assert "other" in text
        # default tools NOT present when custom list given
        assert "glob" not in text


# --------------------------------------------------------------------------- #
# async
# --------------------------------------------------------------------------- #

class TestAsync:
    def test_awrap_tool_call_error_emits_hint(self):
        import asyncio

        mw = InterpreterHintsMiddleware()
        tm = _text_tm(_eval_error_xml("Timeout"))

        async def _handler(_req):
            return tm

        out = asyncio.run(mw.awrap_tool_call(_req("eval"), _handler))
        msgs = _messages(out)
        assert len(msgs) == 2
        assert isinstance(msgs[1], HumanMessage)

    def test_awrap_tool_call_success_passes_through(self):
        import asyncio

        mw = InterpreterHintsMiddleware()
        tm = _text_tm(_eval_success_xml())

        async def _handler(_req):
            return tm

        out = asyncio.run(mw.awrap_tool_call(_req("eval"), _handler))
        assert out is tm
