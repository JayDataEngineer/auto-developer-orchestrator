"""ACP stdio server routing test.

Drives ``pux_harness.acp`` as a real subprocess over stdio using the ``acp``
client SDK — initialize + new_session handshake. This proves the transport
layer (subprocess spawn, JSON-RPC framing, the ``AgentServerACP(agent=factory)``
wiring) end-to-end. It spends **no model tokens and boots no sandbox**:
``new_session`` only mints a ``session_id``; the factory (which builds the
graph → model init → lazy sandbox boot) is invoked lazily from ``prompt``, not
reached here. The full prompt path is proven separately in the verify
log with a real ``pux acp`` run.

Sync test driving an async handshake via ``asyncio.run`` — the harness test
suite is all-sync and has no pytest-asyncio dep; no new infra for one test.
Mirrors ``test_server.py``'s no-token routing-test style.
"""
from __future__ import annotations

import asyncio
import sys
from pathlib import Path
from typing import Any

from acp.exceptions import RequestError
from acp.stdio import spawn_agent_process


class _NoopClient:
    """Minimal Client for the handshake — the agent never calls back during
    initialize/new_session, so every method is a no-op/empty default."""

    def on_connect(self, conn: Any) -> None:  # noqa: ARG002
        return None

    async def request_permission(self, *a: Any, **k: Any) -> Any:  # noqa: ARG002
        raise RuntimeError("permission request not handled in handshake test")

    async def session_update(self, *a: Any, **k: Any) -> None:  # noqa: ARG002
        return None

    async def write_text_file(self, *a: Any, **k: Any) -> None:  # noqa: ARG002
        return None

    async def read_text_file(self, *a: Any, **k: Any) -> Any:  # noqa: ARG002
        return None

    async def create_terminal(self, *a: Any, **k: Any) -> Any:  # noqa: ARG002
        return None

    async def terminal_output(self, *a: Any, **k: Any) -> Any:  # noqa: ARG002
        return None

    async def release_terminal(self, *a: Any, **k: Any) -> None:  # noqa: ARG002
        return None

    async def wait_for_terminal_exit(self, *a: Any, **k: Any) -> Any:  # noqa: ARG002
        return None

    async def kill_terminal(self, *a: Any, **k: Any) -> None:  # noqa: ARG002
        return None

    async def ext_method(self, *a: Any, **k: Any) -> dict[str, Any]:  # noqa: ARG002
        return {}

    async def ext_notification(self, *a: Any, **k: Any) -> None:  # noqa: ARG002
        return None


async def _handshake(org: str, harness_root: Path) -> Any:
    """Spawn ``pux acp --org <org>``, run initialize + new_session, return the
    ``NewSessionResponse`` (carries ``session_id`` + the advertised
    ``config_options`` the editor sees)."""
    async with spawn_agent_process(
        lambda _agent: _NoopClient(),
        sys.executable,
        "-m",
        "pux_harness.acp",
        "--org",
        org,
        cwd=str(harness_root),
    ) as (conn, _proc):
        init = await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=30)
        assert init is not None, "initialize returned None"
        session = await asyncio.wait_for(
            conn.new_session(cwd=str(harness_root)), timeout=30
        )
        return session


async def _handshake_with_db(db: Path, harness_root: Path) -> Any:
    """Spawn ``pux acp --org general`` with ``PUX_API_DB`` redirected to ``db``,
    run initialize + new_session, return the ``NewSessionResponse``.

    ``PUX_API_DB`` and ``PUX_PROJECT_ROOT`` are passed explicitly via
    ``spawn_agent_process``'s ``env=`` (the ACP transport ships a TRIMMED env
    allowlist, so ``monkeypatch.setenv`` on the parent does NOT reach the
    subprocess). No model tokens, no sandbox boot: the factory fires lazily from
    ``prompt``, never reached here.
    """
    async with spawn_agent_process(
        lambda _agent: _NoopClient(),
        sys.executable,
        "-m",
        "pux_harness.acp",
        "--org",
        "general",
        cwd=str(harness_root),
        env={"PUX_API_DB": str(db), "PUX_PROJECT_ROOT": str(harness_root)},
    ) as (conn, _proc):
        init = await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=30)
        assert init is not None, "initialize returned None"
        return await asyncio.wait_for(
            conn.new_session(cwd=str(harness_root)), timeout=30
        )


async def _handshake_via_wrapper(org: str, project_root: Path) -> Any:
    """Spawn through the REAL ``pux`` launcher (bin/pux → ``uv run`` →
    ``python -m pux_harness.acp``) — the EXACT spawn command editors/dispatchers
    (Zed, Toad via ``toad acp "pux acp --org …"``, OpenClaw, Hermes) use.

    The other helpers spawn ``python -m pux_harness.acp`` directly, bypassing
    the ``uv run`` stdout-cleanliness + bin/pux ``.env`` re-source the wrapper
    adds. If ``uv run`` ever prints to stdout, ACP's JSON-RPC framing breaks and
    this handshake fails — the Toad/Zed-blocking regression this locks. No model
    tokens, no sandbox boot (factory fires lazily from ``prompt``)."""
    pux = project_root / "bin" / "pux"
    async with spawn_agent_process(
        lambda _agent: _NoopClient(),
        str(pux),
        "acp",
        "--org",
        org,
        cwd=str(project_root),
    ) as (conn, _proc):
        init = await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=60)
        assert init is not None, "initialize returned None"
        return await asyncio.wait_for(
            conn.new_session(cwd=str(project_root)), timeout=60
        )


def test_acp_handshake_via_pux_wrapper(project_root) -> None:
    """The ``pux acp --org general`` WRAPPER (bin/pux + uv run) speaks clean ACP
    on stdio — the spawn path Toad/Zed/OpenClaw hit. Catches uv-run stdout
    pollution that the direct-``python -m`` handshake tests cannot see."""
    session = asyncio.run(_handshake_via_wrapper("general", project_root))
    assert isinstance(session.session_id, str) and len(session.session_id) > 0
    opts = getattr(session, "config_options", None) or []
    assert any(
        getattr(getattr(o, "root", o), "category", None) == "model" for o in opts
    ), f"wrapper path did not advertise the model dropdown: {opts!r}"


def test_acp_handshake_general(project_root) -> None:
    """initialize + new_session against ``pux acp --org general`` over stdio."""
    session = asyncio.run(_handshake("general", project_root))
    assert isinstance(session.session_id, str) and len(session.session_id) > 0


def test_acp_handshake_invest(project_root) -> None:
    """Second org wires the same way (the factory is org-bound at startup)."""
    session = asyncio.run(_handshake("invest", project_root))
    assert isinstance(session.session_id, str) and len(session.session_id) > 0


def test_acp_main_opens_persistent_store_over_stdio(tmp_path, project_root) -> None:
    """``_acp_main`` opens the shared thread store through the REAL
    stdio transport, so the sqlite file gains the langgraph ``checkpoints``
    table — the proof ACP no longer dies with an ephemeral ``MemorySaver``.

    Fresh sqlite connection on the same file the subprocess wrote proves
    cross-process visibility — exactly the property ``pux show``/``pux resume``
    will rely on. No model tokens, no sandbox boot (the factory fires lazily
    from ``prompt``, never reached here)."""
    import sqlite3

    db = tmp_path / "acp.sqlite"
    session = asyncio.run(_handshake_with_db(db, project_root))
    assert session.session_id, "handshake did not complete; store may not have opened"

    conn = sqlite3.connect(str(db))
    try:
        rows = {
            r[0] for r in conn.execute(
                "SELECT name FROM sqlite_master WHERE type='table'"
            )
        }
    finally:
        conn.close()
    assert "checkpoints" in rows, (
        f"checkpoints table missing — ACP did not open the persistent store; "
        f"got tables {rows!r}"
    )
    assert "pux_threads" in rows, (
        f"pux_threads index missing — open_thread_store did not run; got {rows!r}"
    )


def test_acp_session_registered_in_pux_threads_over_stdio(tmp_path, project_root) -> None:
    """Each ACP session is indexed in ``pux_threads`` so it
    is visible to ``pux resume`` / ``pux show`` (the deferred session-hook gap,
    now closed by ``_RegisteringAgentServerACP.new_session``).

    Drives the REAL stdio transport — initialize + new_session against a tmp
    ``PUX_API_DB`` — then a fresh sqlite read asserts the CLIENT-returned
    ``session_id`` is in ``pux_threads`` with ``org='general'`` +
    ``metadata.source == 'acp'``. That the session_id minted in the subprocess
    is found via a SEPARATE connection is the cross-process visibility proof;
    that its metadata carries ``source: acp`` is the proof the registration
    override (not ``direct``/``serve``) produced it. No model tokens, no
    sandbox boot (factory fires lazily from ``prompt``)."""
    import json
    import sqlite3

    db = tmp_path / "acp-session.sqlite"
    session = asyncio.run(_handshake_with_db(db, project_root))
    sid = session.session_id
    assert sid, "handshake did not complete"

    conn = sqlite3.connect(str(db))
    try:
        row = conn.execute(
            "SELECT org, metadata FROM pux_threads WHERE thread_id = ?", (sid,)
        ).fetchone()
    finally:
        conn.close()
    assert row is not None, (
        f"session {sid!r} not in pux_threads — new_session did not register it"
    )
    org, metadata_json = row
    assert org == "general", f"registered org {org!r} != 'general'"
    assert json.loads(metadata_json) == {"source": "acp"}, (
        f"metadata {metadata_json!r} != {{'source': 'acp'}}"
    )


def test_acp_advertises_mimo_not_openai(project_root) -> None:
    """The server advertises the org's base-role model (MiMo via OpenCode Go) as a
    session ``config_option`` — NOT OpenAI/ChatGPT. Without ``models=`` at
    construction, Zed's model dropdown falls back to its built-in ChatGPT list
    (the "asks for OpenAI models" bug); this asserts the fix holds.

    Hermetic: ``new_session`` never fires the factory, so no Docker, no key, no
    tokens. The expected id is computed the SAME way the server computes it
    (``resolve_model_id``), so parent + subprocess agree regardless of env.
    """
    from pux_harness.agent.model import resolve_model_id

    session = asyncio.run(_handshake("general", project_root))
    expected = resolve_model_id(role="base", org="general")

    # Each option is a RootModel wrapping a SessionConfigOptionSelect (`.root`).
    opts = getattr(session, "config_options", None) or []
    model_selects = [
        getattr(o, "root", o)
        for o in opts
        if getattr(getattr(o, "root", o), "category", None) == "model"
    ]
    assert model_selects, (
        f"no `model` config_option advertised (Zed would fall back to OpenAI): {opts!r}"
    )
    sel = model_selects[0]
    assert sel.current_value == expected, (
        f"advertised model {sel.current_value!r} != resolved base-role id {expected!r}"
    )
    # Regression guard: nothing OpenAI/ChatGPT leaks into the advertised surface.
    surface = [sel.current_value]
    surface += [getattr(o, "value", "") for o in sel.options]
    surface += [getattr(o, "name", "") for o in sel.options]
    leaked = [
        s for s in surface if "openai" in str(s).lower() or "gpt" in str(s).lower()
    ]
    assert not leaked, f"OpenAI/ChatGPT model leaked into advertisement: {leaked!r}"


# ---------------------------------------------------------------------------
# session/load + session/list (#68) — survive-restart resume primitive
# ---------------------------------------------------------------------------
#
# Hermes (daemon-backed sessions that survive editor close) + acpx (parallel
# workstreams / k8s-batch session isolation) BOTH depend on session/load +
# session/list: pick a prior thread back up after pux exits, and enumerate an
# org's threads. deepagents-acp 0.0.8 advertises neither (the latest PyPI
# release, so no upgrade path) — pux hand-rolls both against the shared
# pux_threads index + persistent checkpointer. These drive the REAL stdio
# transport. No model tokens, no sandbox boot: the factory fires lazily from
# ``prompt``, which none of these exercises.


async def _session_load_list(db: Path, harness_root: Path) -> Any:
    """initialize + new_session + list_sessions + load_session(good) +
    load_session(bogus) over the REAL stdio transport against a tmp PUX_API_DB.

    Returns ``(init, new, listed, loaded, bogus_err)``. The bogus load is
    EXPECTED to raise ``RequestError`` (the existence guard); ``bogus_err`` is
    ``None`` if it did not (a bug). One ``spawn_agent_process`` context serves
    all five calls — list/load must run on the SAME connection that minted the
    session. Mirrors ``_handshake_with_db``'s explicit ``env=`` (the ACP
    transport ships a TRIMMED env allowlist; ``monkeypatch.setenv`` on the
    parent does not reach the subprocess)."""
    async with spawn_agent_process(
        lambda _agent: _NoopClient(),
        sys.executable,
        "-m",
        "pux_harness.acp",
        "--org",
        "general",
        cwd=str(harness_root),
        env={"PUX_API_DB": str(db), "PUX_PROJECT_ROOT": str(harness_root)},
    ) as (conn, _proc):
        init = await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=30)
        assert init is not None, "initialize returned None"
        new = await asyncio.wait_for(conn.new_session(cwd=str(harness_root)), timeout=30)
        listed = await asyncio.wait_for(conn.list_sessions(), timeout=30)
        loaded = await asyncio.wait_for(
            conn.load_session(cwd=str(harness_root), session_id=new.session_id),
            timeout=30,
        )
        bogus_err: RequestError | None = None
        try:
            await asyncio.wait_for(
                conn.load_session(
                    cwd=str(harness_root), session_id="no-such-session-zzz"
                ),
                timeout=30,
            )
        except RequestError as exc:
            bogus_err = exc
        return init, new, listed, loaded, bogus_err


def test_acp_advertises_session_load_and_list(tmp_path, project_root) -> None:
    """initialize advertises ONLY the surfaces we ACTUALLY back:
    ``load_session=True`` + ``session_capabilities.list`` set, with
    ``fork``/``resume``/``close`` left UNSET (UNSTABLE in the spec + unbacked),
    ``prompt_capabilities.image=False`` (text-only base), and
    ``mcp_capabilities.{http,sse}=False`` (client-MCP not honored).

    Truthful-capability surface — a client (Hermes/acpx) only offers resume/list
    paths that exist, and never sends image/MCP payloads we'd silently drop.
    This is the audit-row close from [[protocol-surface-map]] (#68 load/list,
    #69 image, #71 mcp). Drives the REAL stdio transport; no tokens, no
    sandbox."""
    init, *_ = asyncio.run(_session_load_list(tmp_path / "cap.sqlite", project_root))
    caps = init.agent_capabilities
    assert caps.load_session is True, (
        f"load_session not advertised (got {caps.load_session!r})"
    )
    assert caps.session_capabilities is not None, "session_capabilities missing"
    sc = caps.session_capabilities
    assert sc.list is not None, "session_capabilities.list not advertised"
    assert sc.fork is None and sc.resume is None and sc.close is None, (
        f"fork/resume/close advertised but unbacked — untruthful: "
        f"fork={sc.fork!r} resume={sc.resume!r} close={sc.close!r}"
    )
    # Truthful image cap (#69): default-tier general's base model is glm-5.2
    # (text-only) → image must be False over the wire, not the base class's
    # hardcoded True. (The True branch is covered in-process below.)
    assert init.agent_capabilities.prompt_capabilities.image is False, (
        "default-tier general advertised image=True — a text-only base must not "
        "offer image attach to the editor"
    )
    # Truthful MCP cap (#71): we do NOT back client-passed ``mcp_servers`` yet
    # — deepagents-acp 0.0.8 drops them (``new_session`` accepts the param then
    # never stores it; ``AgentSessionContext`` is a frozen dataclass with only
    # cwd/mode/model, so the factory can't receive them either). Per-session
    # honoring needs a per-session graph rebuild + ``McpSessionManager``
    # lifecycle; deferred until a dispatcher (Zed/Toad/acpx/OpenClaw/Hermes)
    # actually requires it. So we MUST NOT advertise ``mcp_capabilities`` True —
    # a client seeing True would send MCP servers we silently ignore. This
    # locks the schema default into a deliberate contract: flip it True only
    # alongside the backing work, never before.
    mcp = caps.mcp_capabilities
    assert mcp is not None, "mcp_capabilities missing"
    assert mcp.http is False, (
        f"mcp_capabilities.http advertised True but client-MCP is unbacked: {mcp!r}"
    )
    assert mcp.sse is False, (
        f"mcp_capabilities.sse advertised True but client-MCP is unbacked: {mcp!r}"
    )
    # acp 0.11 drift guard: the bump added ``McpCapabilities.acp`` plus
    # ``PromptCapabilities.audio`` / ``embedded_context``. We back NONE of them,
    # so each must stay unadvertised (falsy) over the wire. A future bump that
    # flips a schema default surfaces here rather than silently leaking a
    # capability a client (Hermes/acpx) would then act on. Assertions are
    # field-level (not whole-dict) so further upstream capability fields can't
    # drift silently either.
    assert not mcp.acp, (
        f"mcp_capabilities.acp advertised but ACP-as-MCP-transport is unbacked: {mcp!r}"
    )
    pc = init.agent_capabilities.prompt_capabilities
    assert pc is not None, "prompt_capabilities missing"
    assert not pc.audio, (
        f"prompt_capabilities.audio advertised but audio input is unbacked: {pc!r}"
    )
    assert not pc.embedded_context, (
        f"prompt_capabilities.embedded_context advertised but unbacked: {pc!r}"
    )


def test_acp_session_load_and_list_roundtrip(tmp_path, project_root) -> None:
    """A session created via new_session is LISTABLE + LOADABLE over the REAL
    stdio transport, and load_session on a bogus id raises RequestError.

    list_sessions returns the just-created session (with a cwd + updated_at);
    load_session on the real id succeeds and re-renders the model config_option
    (mirrors new_session); load_session on a bogus id is rejected (the
    existence + org-isolation guard Hermes/acpx rely on to never hand out a
    handle to a foreign or stale session). No tokens, no sandbox."""
    _init, new, listed, loaded, bogus_err = asyncio.run(
        _session_load_list(tmp_path / "load-list.sqlite", project_root)
    )
    sid = new.session_id
    assert sid, "new_session produced no session_id"

    # list_sessions shows the just-created session, with a cwd + updated_at
    ids = [s.session_id for s in listed.sessions]
    assert sid in ids, f"new session {sid!r} absent from list_sessions: {ids!r}"
    row = next(s for s in listed.sessions if s.session_id == sid)
    assert row.cwd, "listed session carries no cwd"
    assert row.updated_at, "listed session carries no updated_at (created_at)"

    # load_session on the real id re-renders the model config_option (the
    # same dropdown new_session returns) — proves load mirrors new's per-session
    # state hydration, which a subsequent prompt needs.
    opts = getattr(loaded, "config_options", None) or []
    cats = [getattr(getattr(o, "root", o), "category", None) for o in opts]
    assert "model" in cats, (
        f"load_session did not re-render the model config_option: {opts!r}"
    )

    # load_session on a bogus id is rejected — the survive-restart existence
    # guard. Without it a client could load a phantom session and get an empty
    # resume.
    assert bogus_err is not None, (
        "load_session(bogus id) did NOT raise — the existence guard is missing"
    )


def test_acp_image_capability_gates_on_base_multimodal(tmp_path, monkeypatch) -> None:
    """``prompt_capabilities.image`` is truthful: True only when the org's BASE
    (supervisor) model is multimodal.

    The base class hardcodes ``image=True`` for every org; we gate it on
    ``driver_multimodal(role="base", org=...)`` — the SAME seam
    ``BrowserVisionMiddleware`` uses. So a text-only base (glm-5.2, default
    tier) does NOT advertise image-attach to the editor, while a multimodal base
    (mimo-v2.5, fast tier) does. Backing for the multimodal case is LIVE-PROVEN
    by the browser-vision work; the point here is the TRUTHFUL gate, not
    re-proving image ingestion. In-process (no subprocess, no tokens). #69."""
    import pux_harness.threads as threads_mod
    from pux_harness.acp import _RegisteringAgentServerACP
    from pux_harness.threads import open_thread_store

    monkeypatch.setattr(threads_mod, "PUX_API_DB", str(tmp_path / "img.sqlite"))

    async def image_flag() -> bool:
        async with open_thread_store() as store:
            srv = _RegisteringAgentServerACP(
                agent=lambda _ctx: None,
                store=store,
                org="general",
                models=[{"value": "x", "name": "x", "description": "x"}],
            )
            resp = await srv.initialize(protocol_version=1)
            return resp.agent_capabilities.prompt_capabilities.image

    # default tier → base glm-5.2 (text-only) → image must be False
    monkeypatch.delenv("PUX_TIER", raising=False)
    assert asyncio.run(image_flag()) is False, (
        "default-tier general (glm-5.2) advertised image=True — a text-only "
        "base must not offer image attach"
    )
    # fast tier → base mimo-v2.5 (multimodal) → image must be True
    monkeypatch.setenv("PUX_TIER", "fast")
    assert asyncio.run(image_flag()) is True, (
        "fast-tier general (mimo-v2.5) advertised image=False — a multimodal "
        "base must offer image attach"
    )


def test_acp_cancel_flips_cancellation_flag(tmp_path, monkeypatch) -> None:
    """Cancellation is UPSTREAM-owned: ``AgentServerACP.cancel`` sets
    ``self._cancelled``; the ``prompt`` loop checks it (loop-top + mid-stream)
    and returns ``PromptResponse(stop_reason="cancelled")`` — NOT an error.
    ``_RegisteringAgentServerACP`` overrides NEITHER, so the surface must remain
    intact on our subclass.

    This locks that invariant in-process (no model, no conn, no flaky mid-stream
    race): ``_cancelled`` starts False and ``cancel()`` flips it True. If a
    future override breaks cancellation, this fails before the live wire path
    does. The full mid-stream ``stop_reason="cancelled"`` wire proof is deferred
    — it exercises upstream's unmodified ``prompt`` loop, not our code. Audit
    item (5) of [[protocol-surface-map]]."""
    import pux_harness.threads as threads_mod
    from pux_harness.acp import _RegisteringAgentServerACP
    from pux_harness.threads import open_thread_store

    monkeypatch.setattr(threads_mod, "PUX_API_DB", str(tmp_path / "cancel.sqlite"))

    async def go() -> bool:
        async with open_thread_store() as store:
            srv = _RegisteringAgentServerACP(
                agent=lambda _ctx: None,
                store=store,
                org="general",
                models=[{"value": "x", "name": "x", "description": "x"}],
            )
            assert srv._cancelled is False, "flag should start False"
            await srv.cancel(session_id="any")
            return srv._cancelled

    assert asyncio.run(go()) is True, "cancel() did not flip _cancelled"
