"""P3 contract: pin the server.py ``/runs/stream`` SSE wire format.

This is the mandated-first artifact for the upstream-protocol pivot's P3
("retire the hand-rolled server.py REST") — ``docs/prod-come-back-to.md``:
"MUST land a contract test first that pins the exact HTTP surface (/threads,
/runs/stream, /store/items, SSE event shapes) so the cutover is verifiable."

It pins what ``server._stream_run`` emits TODAY — ``event: metadata`` then
``messages`` / ``updates`` / ``values`` frames, an ``error`` frame on exception
— so the cutover to the UPSTREAM ``langgraph-api`` runtime (``langgraph dev`` /
``langgraph build``, the ASGI app that owns the same ``/runs/stream`` surface)
is verifiable byte-for-byte. ``rely-on-upstream``: after the cutover the
upstream ASGI app takes over; this contract is what makes "the new form is
equivalent" a checkable claim, not an assertion (``verify-or-die``).

The gold standard is **consume-with-the-real-decoder**: every frame is parsed
by ``langgraph_sdk.sse.SSEDecoder`` — the SAME decoder a ``RunClient`` uses to
read our stream — proving an upstream SDK client consumes our SSE with no
adapter. This is the load-bearing parity proof between our hand-rolled encoder
and the upstream one.

No live model, no key, no sandbox: a deterministic stub graph implements
``astream`` directly, yielding the ``(mode, chunk)`` tuples ``_stream_run``
consumes. Offline + fast (runs in keyless CI). The full live run is proven by
``tests/integration/test_acp_e2e.py`` + the prod phone→Hermes→dev-bot E2E.
"""
from __future__ import annotations

import json
import uuid
from typing import Any

import pytest
from fastapi.testclient import TestClient

from pux_harness import server


# ---------------------------------------------------------------------------
# Streaming stub — a deterministic stand-in for a compiled org graph. Only
# ``astream`` is exercised by ``_stream_run``; the rest satisfy the
# compiled-graph contract cheaply.
# ---------------------------------------------------------------------------


class _StreamingStubGraph:
    """Yields a fixed ``messages``/``updates``/``values`` sequence so the SSE
    frame set is deterministic. Records the input/config it was fed so the
    contract can assert the server threaded the right thread_id through."""

    def __init__(self, *, chunks: list[tuple[str, Any]] | None = None) -> None:
        self._chunks = chunks if chunks is not None else _DEFAULT_CHUNKS
        self.invoked: list[tuple[Any, dict[str, Any]]] = []
        self.nodes: dict[str, Any] = {}

    async def astream(self, inp: Any, config: dict[str, Any] | None = None, **kw: Any):
        self.invoked.append((inp, config or {}))
        for mode, chunk in self._chunks:
            yield mode, chunk


#: The langgraph stream modes ``_stream_run`` requests + the chunk shapes it
#: serializes. ``messages`` is a ``(msg, meta)`` pair → ``[jsonable(msg),
#: jsonable(meta)]``; ``updates``/``values`` pass through as-is.
_DEFAULT_CHUNKS: list[tuple[str, Any]] = [
    ("messages", ({"type": "ai", "content": "hel"}, {"langgraph_node": "agent", "langgraph_step": 1})),
    ("messages", ({"type": "ai", "content": "lo"}, {"langgraph_node": "agent", "langgraph_step": 1})),
    ("updates", {"agent": {"messages": [{"role": "ai", "content": "hello"}]}}),
    ("values", {"messages": [{"role": "user", "content": "hi"}, {"role": "ai", "content": "hello"}]}),
]


class _RaisingStubGraph(_StreamingStubGraph):
    """Raises mid-stream so the ``error`` frame contract is exercisable."""

    async def astream(self, inp: Any, config: dict[str, Any] | None = None, **kw: Any):
        self.invoked.append((inp, config or {}))
        yield "values", {"messages": []}
        raise RuntimeError("boom: simulated mid-stream failure")


@pytest.fixture
def client(tmp_path, monkeypatch) -> TestClient:
    """Isolate the DB + swap in the streaming stub. Mirrors the
    ``tests/server/test_server.py`` ``client`` fixture (patch the module
    attribute, not the env var)."""
    import pux_harness.threads as threads_mod

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "stream-contract.sqlite")
    monkeypatch.setattr(server, "build_graph", lambda org, **kw: _StreamingStubGraph())
    with TestClient(server.app) as c:
        yield c


# ---------------------------------------------------------------------------
# Gold-standard SSE parse: drive the SAME decoder a langgraph_sdk RunClient
# uses. If our frames don't parse here, an upstream consumer can't read them.
# ---------------------------------------------------------------------------


def _parse_with_sdk_decoder(body: str) -> list[tuple[str, Any]]:
    """Parse an SSE body via ``langgraph_sdk.sse.SSEDecoder`` (the real wire
    consumer) → ``[(event_name, data_or_None)]``. Proves the bytes are
    SDK-consumable, not just textually well-formed."""
    from langgraph_sdk.sse import SSEDecoder

    dec = SSEDecoder()
    out: list[tuple[str, Any]] = []
    for raw in body.split("\n"):
        part = dec.decode(raw.encode())
        if part is not None:
            out.append((part.event, part.data))
    return out


# ---------------------------------------------------------------------------
# the contract
# ---------------------------------------------------------------------------


def test_runs_stream_consumable_by_upstream_sdk_decoder(client: TestClient) -> None:
    """Every frame parses through the upstream ``langgraph_sdk`` SSEDecoder,
    the leading frame is ``metadata`` carrying a UUID ``run_id``, and the
    ``messages`` / ``updates`` / ``values`` frames carry the stub's content
    through ``_jsonable`` serialization. This is the parity proof: a real
    ``RunClient`` reads our hand-rolled stream with no adapter."""
    resp = client.post("/runs/stream", json={"agent_id": "general", "input": "hi"})
    assert resp.status_code == 200, f"non-200: {resp.status_code}\n{resp.text[:400]}"

    parts = _parse_with_sdk_decoder(resp.text)
    assert parts, f"SDK decoder parsed no frames from:\n{resp.text[:400]}"
    events = [name for name, _ in parts]

    # Leading metadata frame carries the run_id the route minted. The run_id is
    # the ONLY way a client learns the id (the route returns nothing else), so
    # this frame + its UUID shape are load-bearing.
    assert events[0] == "metadata", f"first frame must be metadata, got: {events}"
    meta = parts[0][1]
    assert isinstance(meta, dict) and "run_id" in meta, f"metadata frame: {meta}"
    # valid UUID (the route does str(uuid.uuid4()))
    uuid.UUID(str(meta["run_id"]))

    # All three stream modes the route requests are present + carry content.
    assert "messages" in events, f"no messages frame: {events}"
    assert "updates" in events, f"no updates frame: {events}"
    assert "values" in events, f"no values frame: {events}"

    # A messages frame is [msg, meta] (server does _jsonable on both halves).
    msg_frames = [d for n, d in parts if n == "messages" and isinstance(d, list)]
    assert msg_frames, "messages frame did not decode to a [msg, meta] list"
    first_msg = msg_frames[0]
    assert len(first_msg) == 2, f"messages frame shape: {first_msg}"
    assert first_msg[0].get("content") == "hel", (
        f"stub message content did not survive _jsonable: {first_msg}"
    )

    # An updates frame carries the stub's node-delta dict.
    upd = next(d for n, d in parts if n == "updates")
    assert isinstance(upd, dict) and "agent" in upd, f"updates frame: {upd}"


def test_runs_stream_event_name_surface_is_pinned(client: TestClient) -> None:
    """The EXACT success-path event-name set, pinned. When the P3 cutover to
    upstream ``langgraph_api`` lands, this set changes in a KNOWN way: the
    upstream runtime emits a terminal ``event: end`` (data: null) — the SDK's
    end-of-stream sentinel (``langgraph_sdk._shared.utilities._sse_to_v2_dict``
    returns None for ``event == "end"``). server.py omits it today (a client
    terminates on connection-close instead), so the pinned set does NOT yet
    include ``end``. The cutover ADDS it; this test then fails loud → update
    the pinned set to the canonical upstream surface. That flip is the
    no-legacy-left-behind signal that the cutover changed the wire shape."""
    resp = client.post("/runs/stream", json={"agent_id": "general", "input": "hi"})
    assert resp.status_code == 200
    parts = _parse_with_sdk_decoder(resp.text)
    event_set = {name for name, _ in parts}

    assert event_set == {"metadata", "messages", "updates", "values"}, (
        "The /runs/stream success-path event set drifted from the pinned "
        f"surface. Current: {sorted(event_set)}. If 'end' appeared, the "
        "langgraph-api cutover landed — flip this contract to the canonical "
        "upstream set (add 'end') and update the parity note. If something "
        "else changed, that is an unintended wire regression."
    )


def test_runs_stream_error_frame_on_exception(tmp_path, monkeypatch) -> None:
    """A graph that raises mid-stream yields an ``error`` frame (the run does
    NOT crash the connection — the StreamingResponse already started, so the
    error is delivered in-band as the protocol promises). The frame carries a
    human-readable ``message``."""
    import pux_harness.threads as threads_mod

    monkeypatch.setattr(threads_mod, "PUX_API_DB", tmp_path / "stream-err.sqlite")
    monkeypatch.setattr(server, "build_graph", lambda org, **kw: _RaisingStubGraph())
    with TestClient(server.app) as c:
        resp = c.post("/runs/stream", json={"agent_id": "general", "input": "x"})
    assert resp.status_code == 200, "stream must have started (200) before the error frame"
    parts = _parse_with_sdk_decoder(resp.text)
    events = [n for n, _ in parts]
    assert "error" in events, f"no error frame for a raising graph: {events}"
    err = next(d for n, d in parts if n == "error")
    assert isinstance(err, dict) and "message" in err, f"error frame shape: {err}"
    assert "boom" in str(err["message"]), f"error message: {err['message']}"


def test_runs_stream_response_headers(client: TestClient) -> None:
    """The SSE response advertises the right media type + the no-cache /
    no-proxy-buffering headers a streaming endpoint needs (else an
    intermediary buffers the whole stream and streaming is lost)."""
    resp = client.post("/runs/stream", json={"agent_id": "general", "input": "hi"})
    assert resp.status_code == 200
    ctype = resp.headers.get("content-type", "")
    assert "text/event-stream" in ctype, f"content-type: {ctype}"
    assert resp.headers.get("cache-control") == "no-cache", (
        f"cache-control: {resp.headers.get('cache-control')}"
    )
    assert resp.headers.get("x-accel-buffering") == "no", (
        f"x-accel-buffering: {resp.headers.get('x-accel-buffering')} "
        "(nginx buffers SSE without this)"
    )


def test_thread_runs_stream_streams_on_existing_thread(client: TestClient) -> None:
    """The threaded route (``/threads/{tid}/runs/stream``) streams a run on an
    existing thread — the resume/interrupt path's carrier. Pins that this
    route exists, accepts a ``RunCreate`` body, and emits the same SSE
    surface as the ephemeral route."""
    tid = client.post("/threads", json={"agent_id": "general"}).json()["thread_id"]
    resp = client.post(f"/threads/{tid}/runs/stream", json={"input": "hi"})
    assert resp.status_code == 200, f"non-200: {resp.status_code}\n{resp.text[:400]}"
    parts = _parse_with_sdk_decoder(resp.text)
    events = [n for n, _ in parts]
    assert events and events[0] == "metadata", f"threaded stream metadata: {events}"
    assert {"messages", "updates", "values"} <= {n for n in events}, (
        f"threaded stream missing modes: {events}"
    )
