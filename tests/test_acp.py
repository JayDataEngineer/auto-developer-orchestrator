"""ACP stdio server routing test.

Drives ``pux_harness.acp`` as a real subprocess over stdio using the ``acp``
client SDK — initialize + new_session handshake. This proves the transport
layer (subprocess spawn, JSON-RPC framing, the ``AgentServerACP(agent=factory)``
wiring) end-to-end. It spends **no model tokens and boots no sandbox**:
``new_session`` only mints a ``session_id``; the factory (which builds the
graph → model init → lazy sandbox boot) is invoked lazily from ``prompt``, not
reached here. The full prompt path is proven separately in the Phase 9 verify
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

from acp.stdio import spawn_agent_process

HARNESS_DIR = Path(__file__).resolve().parent.parent


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


async def _handshake(org: str) -> str:
    """Spawn ``pux acp --org <org>``, run initialize + new_session, return session_id."""
    async with spawn_agent_process(
        lambda _agent: _NoopClient(),
        sys.executable,
        "-m",
        "pux_harness.acp",
        "--org",
        org,
        cwd=str(HARNESS_DIR),
    ) as (conn, _proc):
        init = await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=30)
        assert init is not None, "initialize returned None"
        session = await asyncio.wait_for(
            conn.new_session(cwd=str(HARNESS_DIR)), timeout=30
        )
        return session.session_id


def test_acp_handshake_general() -> None:
    """initialize + new_session against ``pux acp --org general`` over stdio."""
    session_id = asyncio.run(_handshake("general"))
    assert isinstance(session_id, str) and len(session_id) > 0


def test_acp_handshake_invest() -> None:
    """Second org wires the same way (the factory is org-bound at startup)."""
    session_id = asyncio.run(_handshake("invest"))
    assert isinstance(session_id, str) and len(session_id) > 0
