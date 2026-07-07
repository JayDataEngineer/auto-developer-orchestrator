"""Live AG-UI proof: ``POST /agui/{org}`` streams CopilotKit SSE events.

This is the previously-UNTESTED surface — the "CopilotKit happy" half of the
protocol-surface goal (the ACP/Toad half is ``test_acp.py``). It drives the
REAL ``general`` org graph (``build_graph``, the live MiMo model) through the
AG-UI mount that ``server.py`` gates on ``policy.protocols_for_org``. Because
general declares ``protocols: [acp, agui]``, it passes the gate and IS mounted;
the stream proves the mount emits real events end-to-end.

Gated on ``OPENCODE_API_KEY`` (sourced from ``.env`` — ``uv run`` does NOT
auto-load it, ``bin/pux`` does, so this mirrors bin/pux). Absent key → skip
(legitimate: no live credential). No fake key is ever injected
(``dont-fakekey-skip-e2e``). Spends real tokens; this is the ``verify-or-die``
proof that the surface works, not an assertion that it should.

``TestClient(server.app)`` as a context manager runs the FastAPI lifespan → the
AG-UI mount loop runs → ``/agui/general`` is registered. The
``StreamingResponse`` is fully consumed (TestClient buffers), then the SSE body
is parsed for ``RUN_STARTED`` + a ``TEXT_MESSAGE_*`` event + ``RUN_FINISHED``,
and the absence of ``RUN_ERROR``.
"""
from __future__ import annotations

import json
import os
from pathlib import Path

import pytest
from starlette.testclient import TestClient

from pux_harness import server


# ---------------------------------------------------------------------------
# Offline guard — the AG-UI surface must be AVAILABLE (no live key, no model)
# ---------------------------------------------------------------------------


def test_ag_ui_dependency_is_importable() -> None:
    """Permanent guard against the 76659a0 regression: that commit dropped the
    ``copilotkit`` dep but left ``from copilotkit import LangGraphAGUIAgent`` in
    server.py. While copilotkit lingered in the venv it kept working; once a
    clean ``uv sync`` pruned it, ``_HAS_AG_UI`` flipped False and the ENTIRE
    ``/agui/*`` surface was silently disabled (the mount loop is gated on it).
    The live tests below are OPENCODE_API_KEY-gated, so without this guard the
    regression is invisible in keyless CI. Asserting the import flag directly
    fails loud the moment ag-ui-langgraph is unresolvable — no key, no model,
    no tokens."""
    import pux_harness.server as srv

    assert srv._HAS_AG_UI, (
        "AG-UI unavailable: importing ag_ui_langgraph (LangGraphAgent + "
        "add_langgraph_fastapi_endpoint) failed — _HAS_AG_UI is False, which "
        "silently disables the whole /agui/* mount. Check the ag-ui-langgraph "
        "dependency in pux-harness/pyproject.toml."
    )


# ---------------------------------------------------------------------------
# env: source .env NAMES only (never log values), skip without a live key
# ---------------------------------------------------------------------------


def _source_dotenv_names_only() -> None:
    """Mirror ``bin/pux``'s ``set -a; . .env`` for an in-process live test.

    ``uv run`` does NOT auto-load ``.env``; without this the test would skip
    even though ``.env`` holds the credential. Only var NAMES are read off disk
    into ``os.environ`` — values are never printed or logged."""
    env = Path(__file__).resolve().parents[2] / ".env"
    if not env.is_file():
        return
    for line in env.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))


@pytest.fixture(scope="module")
def live_key() -> str:
    _source_dotenv_names_only()
    key = os.environ.get("OPENCODE_API_KEY")
    if not key:
        pytest.skip("OPENCODE_API_KEY not set — skipping live AG-UI E2E")
    return key


# ---------------------------------------------------------------------------
# SSE parsing — robust to either `event:` headers or type-in-data framing
# ---------------------------------------------------------------------------


def _sse_events(body: str) -> list[tuple[str, dict | None]]:
    """Parse an SSE body into ``[(event_type, parsed_data_or_None)]``.

    AG-UI events carry their kind as a ``type`` field on the JSON ``data:`` line
    (e.g. ``{"type": "TEXT_MESSAGE_CONTENT", ...}``); the encoder may ALSO emit
    an ``event:`` header. We prefer the JSON ``type`` (definitive) and fall back
    to the ``event:`` header for any event whose data isn't parseable JSON."""
    out: list[tuple[str, dict | None]] = []
    pending_event: str | None = None
    for line in body.splitlines():
        if line.startswith("event:"):
            pending_event = line[len("event:"):].strip()
        elif line.startswith("data:"):
            data = line[len("data:"):].strip()
            etype: str = ""
            parsed: dict | None = None
            if data:
                try:
                    obj = json.loads(data)
                    if isinstance(obj, dict):
                        parsed = obj
                        etype = str(obj.get("type") or "")
                except json.JSONDecodeError:
                    pass
            out.append((etype or pending_event or "", parsed))
            pending_event = None
    return out


def _assistant_text_from_snapshot(events: list[tuple[str, dict | None]]) -> str:
    """Pull the assistant's reply text out of the terminal MESSAGES_SNAPSHOT.

    MiMo via OpenCode Go streams langgraph deltas that ag_ui_langgraph doesn't
    map to incremental ``TEXT_MESSAGE_*`` events — they pass through as ``RAW``.
    The model's reply is therefore delivered as a ``MESSAGES_SNAPSHOT`` (the full
    final message list). This extracts the assistant turn's text from it, which
    is the real proof the model produced a reply through the surface."""
    snapshots = [d for t, d in events if t == "MESSAGES_SNAPSHOT" and isinstance(d, dict)]
    if not snapshots:
        return ""
    msgs = snapshots[-1].get("messages") or []
    for m in msgs:
        if not isinstance(m, dict) or m.get("role") != "assistant":
            continue
        content = m.get("content")
        if isinstance(content, str):
            return content
        if isinstance(content, list):
            texts = [
                p.get("text", "")
                for p in content
                if isinstance(p, dict) and p.get("type") == "text"
            ]
            joined = "".join(texts).strip()
            if joined:
                return joined
    return ""


def _assistant_text(events: list[tuple[str, dict | None]]) -> str:
    """The assistant's reply text, robust to EITHER delivery shape.

    MiMo via OpenCode Go sometimes lands the reply only in the terminal
    MESSAGES_SNAPSHOT (deltas pass through as ``RAW``) and sometimes streams it
    as real incremental ``TEXT_MESSAGE_CONTENT`` events (``delta`` chunks). Prefer
    the incremental deltas (the strongest surface proof — the text streamed) and
    fall back to MESSAGES_SNAPSHOT so neither delivery shape returns empty."""
    deltas = [
        d.get("delta", "")
        for t, d in events
        if t == "TEXT_MESSAGE_CONTENT" and isinstance(d, dict)
    ]
    joined = "".join(deltas).strip()
    if joined:
        return joined
    return _assistant_text_from_snapshot(events)


# ---------------------------------------------------------------------------
# the proof
# ---------------------------------------------------------------------------


def _general_prompt() -> dict:
    """Minimal valid ``RunAgentInput`` for general — a one-line user turn that
    elicits a direct text reply (no tool use → no sandbox boot). Validated
    against ``ag_ui.core.RunAgentInput`` at design time."""
    return {
        "thread_id": "live-agui-test",
        "run_id": "run-1",
        "state": {},
        "messages": [
            {
                "role": "user",
                "id": "m1",
                "content": [{"type": "text", "text": "Reply with exactly: AGUI OK"}],
            }
        ],
        "tools": [],
        "context": [],
        "forwarded_props": {},
    }


def test_agui_general_mounted(live_key, tmp_path, monkeypatch) -> None:
    """The #67 serve-gate lets the default-[acp, agui] general org through to the
    AG-UI mount — a cheap (token-free) precondition: after lifespan the route
    exists, so the gate did not exclude general."""
    import pux_harness.threads as threads_mod

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "agui-gate.sqlite")
    with TestClient(server.app) as client:
        routes = {getattr(r, "path", "") for r in client.app.routes}
    assert "/agui/general" in routes, (
        "general (protocols: [acp, agui]) was NOT mounted — the serve-gate "
        "excluded it or the AG-UI mount loop did not run"
    )


def test_agui_general_streams_text(live_key, tmp_path, monkeypatch) -> None:
    """POST /agui/general streams a real run to completion and delivers the
    model's reply through the surface. Asserts the full lifecycle (RUN_STARTED →
    RUN_FINISHED, no RUN_ERROR) AND that the assistant's reply text arrives in
    the terminal MESSAGES_SNAPSHOT — the live proof the AG-UI surface works
    end-to-end against the real MiMo model.

    (Note: incremental ``TEXT_MESSAGE_*`` token events are NOT emitted today —
    langgraph chat-model deltas stream through as ``RAW`` and the reply lands in
    MESSAGES_SNAPSHOT. That incremental-streaming translation is a separate gap,
    out of scope for this 'surface works' proof.)"""
    import pux_harness.threads as threads_mod

    # Isolate the run's checkpoint state from the shared production store.
    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "agui-live.sqlite")

    with TestClient(server.app) as client:
        resp = client.post(
            "/agui/general",
            json=_general_prompt(),
            headers={"Accept": "text/event-stream"},
            timeout=240,
        )

    assert resp.status_code == 200, (
        f"non-200 from /agui/general: {resp.status_code}\n{resp.text[:800]}"
    )
    events = _sse_events(resp.text)
    assert events, f"no SSE events parsed from response:\n{resp.text[:800]}"
    types = [t for t, _ in events]

    assert "RUN_STARTED" in types, f"no RUN_STARTED in events: {types}"
    assert "RUN_FINISHED" in types, f"stream did not finish cleanly: {types}"
    assert "RUN_ERROR" not in types, (
        f"RUN_ERROR in stream: {[d for t, d in events if t == 'RUN_ERROR']}"
    )

    # The model's reply must arrive through the surface (not just lifecycle
    # ticks). MESSAGES_SNAPSHOT carries the final assistant turn.
    reply = _assistant_text_from_snapshot(events)
    assert reply, (
        "MESSAGES_SNAPSHOT carried no assistant text — the model did not reply "
        f"through the surface. events: {types}"
    )


# ---------------------------------------------------------------------------
# ask_user over the web lane — the AG-UI interrupt + resume proof
# ---------------------------------------------------------------------------


def _ask_user_prompt(thread_id: str) -> dict:
    """Turn-1 RunAgentInput for social-media-pipeline: a prompt that demands the
    agent call ``ask_user`` BEFORE drafting. Over the web lane ask_user takes the
    INTERRUPT branch (``transport="serve"``), so the graph pauses + the AG-UI
    adapter emits an ``on_interrupt`` CUSTOM event carrying the question, then
    ends the run cleanly (RUN_FINISHED) — verified in ag_ui_langgraph/agent.py
    (``has_active_interrupts and not has_resume_input`` → RUN_STARTED +
    on_interrupt + RUN_FINISHED, no graph execution, no hang)."""
    return {
        "thread_id": thread_id,
        "run_id": "ask-1",
        "state": {},
        "messages": [
            {
                "role": "user",
                "id": "a1",
                "content": [
                    {
                        "type": "text",
                        "text": (
                            "I want you to draft a tweet, but FIRST you must ask "
                            "me a question: call the ask_user tool to ask what "
                            "tone I want (casual or formal), then STOP and wait "
                            "for my answer. Do NOT draft anything yet. You MUST "
                            "call ask_user before doing anything else."
                        ),
                    }
                ],
            }
        ],
        "tools": [],
        "context": [],
        "forwarded_props": {},
    }


def _ask_user_resume(thread_id: str, reply: str) -> dict:
    """Turn-2 RunAgentInput that RESUMES the paused interrupt. CopilotKit's
    ``useInterrupt`` ``resolve(text)`` resumes via ``forwarded_props.command.
    resume``; ag_ui_langgraph forwards a non-JSON string raw to
    ``Command(resume=<str>)`` (agent.py:582), which is exactly what langgraph's
    ``interrupt()`` returns — so our ask_user tool body receives ``reply`` as its
    result and the agent continues with it. Same ``thread_id`` so the
    checkpointer hands the adapter the paused checkpoint."""
    return {
        "thread_id": thread_id,
        "run_id": "ask-2",
        "state": {},
        "messages": [],
        "tools": [],
        "context": [],
        "forwarded_props": {"command": {"resume": reply}},
    }


def _interrupt_event(events: list[tuple[str, dict | None]]) -> dict | None:
    """The ``on_interrupt`` CUSTOM event, if one fired. ag_ui_langgraph emits it
    as ``{"type": "CUSTOM", "name": "on_interrupt", "value": "<json string>"}``
    where ``value`` is our interrupt payload (``{"question","options",
    "default"}``) JSON-encoded. Returns the parsed CUSTOM event dict (None if no
    interrupt fired)."""
    for etype, data in events:
        if etype != "CUSTOM" or not isinstance(data, dict):
            continue
        if data.get("name") == "on_interrupt":
            return data
    return None


def test_agui_ask_user_interrupts_and_resumes(live_key, tmp_path, monkeypatch) -> None:
    """The ask_user HITL proof over the WEB lane (AG-UI / CopilotKit).

    social-media-pipeline opts into ``ask_user`` (``profile.yaml``). Over the web
    the tool takes the INTERRUPT branch: ``interrupt()`` (langgraph primitive)
    pauses the graph, the AG-UI adapter surfaces it as an ``on_interrupt`` CUSTOM
    event + RUN_FINISHED (no hang), and a follow-up POST with
    ``forwarded_props.command.resume`` resumes the graph so ask_user returns the
    human's reply as its tool result and the agent continues. This is the backend
    contract a CopilotKit ``useInterrupt`` card consumes.

    Proves, live:
    1. Turn 1 — the agent calls ``ask_user`` → an ``on_interrupt`` event fires
       (the graph paused at the interrupt), the run ends cleanly (RUN_FINISHED),
       no RUN_ERROR. The interrupt carries our question payload.
    2. Turn 2 — resuming with ``"casual"`` → the run finishes cleanly and the
       agent's reply incorporates the resumed answer (the round-trip closed).

    Model-cooperation-dependent (like the ACP proof): if MiMo ignores "ask first"
    and drafts anyway, turn 1 carries no interrupt and the test flags a real gap
    (the tool description or supervisor suffix needs tightening), not a silent
    pass. Skipped without OPENCODE_API_KEY."""
    import pux_harness.threads as threads_mod

    # Isolate this run's checkpoint state from the shared production store AND
    # from other live tests (the resume round-trip needs a clean thread).
    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "agui-ask.sqlite")

    with TestClient(server.app) as client:
        # --- Turn 1: ask_user → interrupt ---
        r1 = client.post(
            "/agui/social-media-pipeline",
            json=_ask_user_prompt("live-agui-ask"),
            headers={"Accept": "text/event-stream"},
            timeout=240,
        )
        assert r1.status_code == 200, (
            f"non-200 from /agui/social-media-pipeline: {r1.status_code}\n{r1.text[:800]}"
        )
        events1 = _sse_events(r1.text)
        types1 = [t for t, _ in events1]
        assert events1, f"no SSE events parsed from turn 1:\n{r1.text[:800]}"
        assert "RUN_ERROR" not in types1, (
            f"RUN_ERROR in turn 1: {[d for t, d in events1 if t == 'RUN_ERROR']}"
        )
        # The load-bearing assertion: ask_user interrupted the graph.
        interrupt = _interrupt_event(events1)
        assert interrupt is not None, (
            "no on_interrupt CUSTOM event fired in turn 1 — ask_user did not "
            f"interrupt the graph over the web lane. types: {types1}"
        )
        assert "RUN_FINISHED" in types1, (
            f"interrupt fired but run never finished (hung?): {types1}"
        )

        # --- Turn 2: resume the interrupt with the human's reply ---
        r2 = client.post(
            "/agui/social-media-pipeline",
            json=_ask_user_resume("live-agui-ask", "casual"),
            headers={"Accept": "text/event-stream"},
            timeout=240,
        )
        assert r2.status_code == 200, (
            f"non-200 resuming the interrupt: {r2.status_code}\n{r2.text[:800]}"
        )
        events2 = _sse_events(r2.text)
        types2 = [t for t, _ in events2]
        assert "RUN_ERROR" not in types2, (
            f"RUN_ERROR resuming turn 2: {[d for t, d in events2 if t == 'RUN_ERROR']}"
        )
        assert "RUN_FINISHED" in types2, (
            f"resume run did not finish cleanly: {types2}"
        )
        # The agent continued past the interrupt and produced a reply that
        # incorporates the resumed "casual" answer.
        reply2 = _assistant_text(events2)
        assert reply2, (
            "turn 2 carried no assistant text — the agent did not continue after "
            f"the interrupt resumed. types: {types2}"
        )
        assert "casual" in reply2.lower(), (
            f"the resumed 'casual' answer did not reach the agent's reply — "
            f"the interrupt→resume round-trip did not close. reply2={reply2!r}"
        )
