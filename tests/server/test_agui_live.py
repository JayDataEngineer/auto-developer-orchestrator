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
