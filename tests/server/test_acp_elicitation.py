"""ask_user over ACP — the ONE end-turn-and-resume mechanic.

``deepagents-acp`` only emits ``session/request_permission`` (a tool-gate, no
free-text answer) and hard-errors on free-form ``interrupt()``. We override that:
the ``ask_user`` tool raises ``interrupt({"ask_user": ...})``; the server's
``_handle_interrupts`` presents the question, ENDS the turn (raises
``_AskUserPause`` → ``PromptResponse(stop_reason="end_turn")``); the user's NEXT
freeform message is the resume signal — ``prompt`` detects the pending interrupt
at entry and ``_resume_ask_user`` feeds ``Command(resume=...)`` FIRST (the stock
loop's ``{"messages":[reply]}``-first shape CANCELS the paused tool call — proven
poison). No request_permission / elicitation split, no prompt-suffix.

These tests prove:
  * the helpers (``_content_to_text`` / ``_extract_text`` / partition predicate);
  * ``_handle_interrupts`` presents + raises ``_AskUserPause`` for ask_user and
    delegates tool-gates to the base;
  * ``prompt`` routes resume-vs-normal and catches ``_AskUserPause``;
  * the FULL end-to-end mechanic against the REAL deepagents graph + REAL server
    (offline + deterministic via a scripted model): turn 1 ends on the interrupt,
    turn 2 = the user's freeform reply → the agent continues with that answer.
"""

from __future__ import annotations

import asyncio
from typing import Any
from unittest.mock import AsyncMock, MagicMock

import pytest
from acp.schema import (
    ClientCapabilities,
    ElicitationCapabilities,
    ElicitationFormCapabilities,
    PromptResponse,
    TextContentBlock,
)
from deepagents_acp.server import AgentServerACP

from pux_harness.acp import (
    _AskUserPause,
    _content_to_text,
    _extract_text,
    _RegisteringAgentServerACP,
)


# --- shared fixtures ---------------------------------------------------------


def _make_server(agent: Any = None) -> _RegisteringAgentServerACP:
    """Build the real server subclass with a fake client connection.

    ``self._conn`` mirrors the runtime shape so base methods (``_log_text`` etc.)
    that reach the connection don't blow up. The integration test passes a REAL
    graph factory via ``agent``."""
    server = _RegisteringAgentServerACP(
        agent=agent if agent is not None else (lambda ctx: None),
        store=MagicMock(),
        org="general",
    )
    client = MagicMock()
    client.session_update = AsyncMock()
    server._conn = client
    return server


class _Interrupt:
    """Minimal stand-in for a langgraph ``Interrupt`` (only ``.value`` is read)."""

    def __init__(self, value: Any) -> None:
        self.value = value
        self.id = "it-1"


def _state(interrupts):
    state = MagicMock()
    state.interrupts = list(interrupts)
    return state


async def _capture_log(server) -> list[str]:
    """Replace ``_log_text`` with a capturing async stub; return the buffer."""
    logged: list[str] = []

    async def fake_log(session_id, text, **_kw):
        logged.append(text)

    server._log_text = fake_log  # type: ignore[method-assign]
    return logged


# --- module helpers ----------------------------------------------------------


def test_content_to_text_flattens_text_blocks() -> None:
    blocks = [
        TextContentBlock(type="text", text="blue"),
        TextContentBlock(type="text", text="now"),
    ]
    assert _content_to_text(blocks) == "blue\nnow"


def test_content_to_text_ignores_non_text_and_empty() -> None:
    assert _content_to_text(None) == ""
    assert _content_to_text([]) == ""
    # A non-text block (no ``.text``) is dropped, not crashed on.
    assert _content_to_text([MagicMock(spec=[])]) == ""


def test_extract_text() -> None:
    assert _extract_text("hi") == "hi"
    assert _extract_text([{"type": "text", "text": "a"}, "b"]) == "ab"
    assert _extract_text([{"type": "image", "url": "x"}]) == ""
    assert _extract_text(42) == "42"


def test_has_ask_user_interrupt() -> None:
    assert _RegisteringAgentServerACP._has_ask_user_interrupt(
        _state([_Interrupt({"ask_user": {"question": "q?"}})])
    ) is True
    assert _RegisteringAgentServerACP._has_ask_user_interrupt(
        _state([_Interrupt({"action_requests": []})])
    ) is False
    assert _RegisteringAgentServerACP._has_ask_user_interrupt(_state([])) is False


# --- _handle_interrupts: present + raise vs delegate -------------------------


def test_handle_interrupts_ask_user_presents_and_raises() -> None:
    async def _run() -> None:
        srv = _make_server()
        logged = await _capture_log(srv)
        with pytest.raises(_AskUserPause):
            await srv._handle_interrupts(
                current_state=_state([
                    _Interrupt({"ask_user": {
                        "question": "Ship?", "options": ["ship", "wait"], "default": "wait",
                    }}),
                ]),
                session_id="s1",
            )
        # The question + BOTH options were presented so the user can answer.
        joined = "\n".join(logged)
        assert "Ship?" in joined
        assert "ship / wait" in joined

    asyncio.run(_run())


def test_handle_interrupts_free_text_ask_user_presented() -> None:
    """Free-text ask_user (no options) is presented too — the unified path."""
    async def _run() -> None:
        srv = _make_server()
        logged = await _capture_log(srv)
        with pytest.raises(_AskUserPause):
            await srv._handle_interrupts(
                current_state=_state([
                    _Interrupt({"ask_user": {"question": "Name?", "options": [], "default": None}}),
                ]),
                session_id="s1",
            )
        assert "Name?" in "\n".join(logged)

    asyncio.run(_run())


def test_handle_interrupts_delegates_tool_gates(monkeypatch) -> None:
    async def _run() -> None:
        srv = _make_server()
        parent = AsyncMock(return_value=[{"type": "approve", "action": "approve"}])
        monkeypatch.setattr(AgentServerACP, "_handle_interrupts", parent)
        out = await srv._handle_interrupts(
            current_state=_state([
                _Interrupt({"action_requests": [{"name": "edit_file", "args": {}}]}),
            ]),
            session_id="s1",
        )
        parent.assert_awaited_once()
        # The tool-gate interrupts were forwarded to the base unchanged.
        assert parent.call_args.kwargs["pending_interrupts"]
        assert out == [{"type": "approve", "action": "approve"}]

    asyncio.run(_run())


def test_handle_interrupts_empty_returns_empty_list() -> None:
    async def _run() -> None:
        srv = _make_server()
        out = await srv._handle_interrupts(current_state=_state([]), session_id="s1")
        assert out == []

    asyncio.run(_run())


# --- prompt: routing + _AskUserPause handling --------------------------------


def _msg(text: str) -> list[TextContentBlock]:
    return [TextContentBlock(type="text", text=text)]


def test_prompt_normal_delegates_to_base_when_no_pending_ask_user(monkeypatch) -> None:
    async def _run() -> None:
        srv = _make_server()
        srv._agent = MagicMock()
        srv._agent.aget_state = AsyncMock(return_value=_state([]))  # no pending interrupt
        srv._resume_ask_user = AsyncMock(  # type: ignore[method-assign]
            return_value=PromptResponse(stop_reason="end_turn")
        )
        base_prompt = AsyncMock(return_value=PromptResponse(stop_reason="end_turn"))
        monkeypatch.setattr(AgentServerACP, "prompt", base_prompt)
        await srv.prompt(prompt=_msg("hi"), session_id="s1")
        base_prompt.assert_awaited_once()
        srv._resume_ask_user.assert_not_awaited()

    asyncio.run(_run())


def test_prompt_resume_routes_to_resume_ask_user(monkeypatch) -> None:
    async def _run() -> None:
        srv = _make_server()
        srv._agent = MagicMock()
        srv._agent.aget_state = AsyncMock(
            return_value=_state([_Interrupt({"ask_user": {"question": "q?"}})])  # pending!
        )
        srv._resume_ask_user = AsyncMock(  # type: ignore[method-assign]
            return_value=PromptResponse(stop_reason="end_turn")
        )
        base_prompt = AsyncMock(return_value=PromptResponse(stop_reason="end_turn"))
        monkeypatch.setattr(AgentServerACP, "prompt", base_prompt)
        await srv.prompt(prompt=_msg("blue"), session_id="s1")
        # The user's freeform reply was handed to the resume path verbatim.
        srv._resume_ask_user.assert_awaited_once()
        args = srv._resume_ask_user.call_args
        assert args.args[0] == "s1"
        assert _content_to_text(args.args[1]) == "blue"
        base_prompt.assert_not_awaited()

    asyncio.run(_run())


def test_prompt_catches_ask_user_pause_and_ends_turn(monkeypatch) -> None:
    """When the base loop raises ``_AskUserPause`` (agent called ask_user
    mid-turn), ``prompt`` swallows it and returns ``end_turn`` — the interrupt
    persists for the user's next message to resume."""
    async def _run() -> None:
        srv = _make_server()
        srv._agent = MagicMock()
        srv._agent.aget_state = AsyncMock(return_value=_state([]))

        async def raising_prompt(self, prompt, session_id, message_id=None, **kw):
            raise _AskUserPause

        monkeypatch.setattr(AgentServerACP, "prompt", raising_prompt)
        resp = await srv.prompt(prompt=_msg("go"), session_id="s1")
        assert resp.stop_reason == "end_turn"

    asyncio.run(_run())


# --- the proof: full end-to-end resume against the real deepagents graph -----


def _scripted_model(responses):
    """Deterministic stand-in for the chat model.

    deepagents binds tools + structured output at runtime; the stock fakes raise
    ``NotImplementedError`` there, so we stub those to return ``self`` and feed a
    scripted list of ``AIMessage``s (one per agent step). Must be a DIRECT
    ``FakeMessagesListChatModel`` subclass (not a wrapper): ``BaseChatModel`` is
    pydantic-backed and a ``__getattr__``-forwarding wrapper breaks its attribute
    protocol. Turn 1 emits the ``ask_user`` tool call; after resume, turn 2 emits
    the final reply."""
    from langchain_core.language_models import FakeMessagesListChatModel

    class _Inner(FakeMessagesListChatModel):
        def bind_tools(self, tools, *, tool_choice=None, **_kw):  # noqa: ARG002
            return self

        def with_structured_output(self, schema, **_kw):  # noqa: ARG002
            return self

    return _Inner(responses=responses)


def test_end_to_end_resume_through_real_server() -> None:
    async def _run() -> None:
        from langchain_core.messages import AIMessage
        from langgraph.checkpoint.memory import MemorySaver

        from deepagents import create_deep_agent

        from pux_harness.agent.hitl import make_ask_user_tool

        ask = make_ask_user_tool("acp")
        graph = create_deep_agent(
            model=_scripted_model([
                AIMessage(content="", tool_calls=[{
                    "name": "ask_user",
                    "args": {"question": "Pick a color", "options": ["red", "blue"],
                             "default": "red"},
                    "id": "tc1", "type": "tool_call",
                }]),
                AIMessage(content="You chose **blue**."),
            ]),
            tools=[ask],
            checkpointer=MemorySaver(),
        )
        srv = _make_server(agent=lambda _ctx: graph)
        logged = await _capture_log(srv)
        cfg = {"configurable": {"thread_id": "s1"}}

        # --- Turn 1: agent calls ask_user → turn ENDS on the interrupt. -------
        resp1 = await srv.prompt(
            prompt=_msg("Ask me my favorite color with ask_user, then stop."),
            session_id="s1",
        )
        assert resp1.stop_reason == "end_turn"  # mechanically a normal turn end
        assert any("Pick a color" in t for t in logged)
        snap = await graph.aget_state(cfg)
        assert snap.interrupts, "the ask_user interrupt must persist in the checkpoint"

        # --- Turn 2: the user's FREEFORM reply resumes the thread. ------------
        logged.clear()
        resp2 = await srv.prompt(prompt=_msg("blue"), session_id="s1")
        assert resp2.stop_reason == "end_turn"
        # The agent continued with the resumed answer.
        assert any("blue" in t.lower() for t in logged)
        snap2 = await graph.aget_state(cfg)
        assert not snap2.interrupts, "no interrupt pending after the resume completes"

    asyncio.run(_run())


# --- capability-independence: the retired ``acp_capability_probe``, as a test --
#
# That probe (scripts/acp_capability_probe.py) ran a LIVE ACP client, read the
# ``elicitation.form`` capability from its ``initialize`` request, and reported
# which ask_user "path" would fire (native ``elicitation/create`` vs
# ``session/request_permission`` vs turn-based chat). That capability-driven
# branching is GONE — the refactor collapsed ask_user into ONE
# end-turn-and-resume mechanic that never reads client capabilities. This is the
# faithful test form of the probe: drive the REAL server through ``initialize``
# with two client capability shapes — one advertising ``elicitation.form`` (the
# path the probe used to select) and one advertising nothing — and assert the
# ask_user mechanic is IDENTICAL in both. Re-introducing capability-driven
# branching breaks the equality.


def _ask_user_outcome(caps: ClientCapabilities | None) -> dict[str, object]:
    """Run the full 2-turn ask_user flow against a fresh real server/graph whose
    client connected advertising ``caps``; return a canonical outcome dict."""
    async def _run() -> dict[str, object]:
        from langchain_core.messages import AIMessage
        from langgraph.checkpoint.memory import MemorySaver

        from deepagents import create_deep_agent

        from pux_harness.agent.hitl import make_ask_user_tool

        ask = make_ask_user_tool("acp")
        graph = create_deep_agent(
            model=_scripted_model([
                AIMessage(content="", tool_calls=[{
                    "name": "ask_user",
                    "args": {"question": "Pick a color", "options": ["red", "blue"],
                             "default": "red"},
                    "id": "tc1", "type": "tool_call",
                }]),
                AIMessage(content="You chose **blue**."),
            ]),
            tools=[ask],
            checkpointer=MemorySaver(),
        )
        srv = _make_server(agent=lambda _ctx: graph)
        logged = await _capture_log(srv)
        init = await srv.initialize(protocol_version=1, client_capabilities=caps)
        advertised_image = init.agent_capabilities.prompt_capabilities.image

        cfg = {"configurable": {"thread_id": "s1"}}
        resp1 = await srv.prompt(
            prompt=_msg("Ask me my favorite color with ask_user, then stop."),
            session_id="s1",
        )
        question_presented = any("Pick a color" in t for t in logged)
        persisted = bool((await graph.aget_state(cfg)).interrupts)
        logged.clear()
        resp2 = await srv.prompt(prompt=_msg("blue"), session_id="s1")
        no_pending_after = not (await graph.aget_state(cfg)).interrupts

        return {
            "advertised_image": advertised_image,
            "stop1": resp1.stop_reason,
            "question_presented": question_presented,
            "persisted": persisted,
            "stop2": resp2.stop_reason,
            "no_pending_after": no_pending_after,
        }

    return asyncio.run(_run())


def test_ask_user_path_is_independent_of_client_elicitation_capability(monkeypatch) -> None:
    # The advertised-image flag is derived from model config (``driver_multimodal``),
    # NOT from client capabilities — pin it so the comparison isolates the mechanic.
    monkeypatch.setattr("pux_harness.acp.driver_multimodal", lambda **_kw: False)

    shapes = {
        "elicitation_form_advertised": ClientCapabilities(
            elicitation=ElicitationCapabilities(form=ElicitationFormCapabilities())
        ),
        "no_elicitation": ClientCapabilities(),
    }
    outcomes = {label: _ask_user_outcome(caps) for label, caps in shapes.items()}

    # The mechanic is the SAME regardless of what the client advertised — i.e. a
    # client offering native ``elicitation/create`` gets the identical
    # end-turn-and-resume path as one offering nothing.
    assert outcomes["elicitation_form_advertised"] == outcomes["no_elicitation"]

    # ...and that shared outcome IS end-turn-and-resume, not an elicitation path.
    assert outcomes["elicitation_form_advertised"] == {
        "advertised_image": False,
        "stop1": "end_turn",
        "question_presented": True,
        "persisted": True,
        "stop2": "end_turn",
        "no_pending_after": True,
    }
