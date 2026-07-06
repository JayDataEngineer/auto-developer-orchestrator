"""Live E2E for the ACP stdio server (the Zed editor wire path).

Drives the REAL ``pux acp --org general`` subprocess over stdio through the full
handshake + a real prompt, collecting the streamed agent message — the exact wire
path Zed uses. Closes the gap the hermetic ``tests/test_acp.py`` leaves open: that
the factory compiles, the real model answers, and the response streams back over
JSON-RPC. This is the "so I don't have to deal with this" proof.

Skipped unless ``PUX_E2E=1`` — the factory boots the sandbox eagerly
(``DockerExecClient.__init__`` → ``docker.from_env``), and the model needs a live
``OPENCODE_API_KEY`` (the ``acp-zed.sh`` wrapper sources ``.env``; ``bin/pux`` does
the same). To run:

    # Docker daemon running + .env has OPENCODE_API_KEY
    PUX_E2E=1 uv run pytest tests/integration/test_acp_e2e.py -q

Mirrors the gate + ``asyncio.run`` helper style of ``test_mcp_server_e2e.py``.
"""
from __future__ import annotations

import asyncio
import os
import sys
from pathlib import Path
from typing import Any

import pytest
from acp.helpers import text_block
from acp.schema import RequestPermissionResponse
from acp.stdio import spawn_agent_process

pytestmark = pytest.mark.skipif(
    os.environ.get("PUX_E2E") != "1",
    reason=(
        "set PUX_E2E=1 (Docker running + .env OPENCODE_API_KEY) to run the live ACP e2e"
    ),
)

REPO_ROOT = Path(__file__).resolve().parents[2]


class _CollectingClient:
    """Records streamed ``session_update`` chunks (the agent's message) and
    auto-allows any tool permission so a real prompt can run unattended. The
    other Client methods are unused no-ops — the trivial prompt never reaches
    them."""

    def __init__(self) -> None:
        self.updates: list[Any] = []

    def on_connect(self, conn: Any) -> None:  # noqa: ARG002
        return None

    async def session_update(self, session_id: str, update: Any, **kw: Any) -> None:  # noqa: ARG002
        self.updates.append(update)

    async def request_permission(self, *a: Any, **k: Any) -> RequestPermissionResponse:  # noqa: ARG002
        # Empty == default; a tool-free prompt won't reach this anyway.
        return RequestPermissionResponse()

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


def _agent_text(updates: list[Any]) -> str:
    """Join the streamed ``agent_message_chunk`` contents into one string."""
    parts: list[str] = []
    for u in updates:
        if getattr(u, "session_update", None) != "agent_message_chunk":
            continue
        content = getattr(u, "content", None)
        if content is None:
            continue
        parts.append(getattr(content, "text", None) or str(content))
    return "".join(parts)


def _update_kinds(updates: list[Any]) -> list[str]:
    """The ordered ``session_update`` discriminator values (agent_message_chunk,
    tool_call, plan, …) — used to prove tool calls stream over the wire."""
    return [str(getattr(u, "session_update", None)) for u in updates]


async def _prompt_and_collect(prompt_text: str) -> tuple[str, list[str], Any]:
    client = _CollectingClient()
    # Forward the parent env so OPENCODE_API_KEY reaches the child past
    # ``acp.transports.default_environment``'s POSIX allowlist (which strips
    # everything but HOME/PATH/SHELL/…). ``bin/pux`` auto-loads ``.env``; the
    # bare ``python -m pux_harness.acp`` the test spawns does not, so without
    # this the factory boots keyless and ``session/prompt`` returns
    # "Internal error". This mirrors Zed, which passes the editor env through.
    async with spawn_agent_process(
        lambda _agent: client,
        sys.executable,
        "-m",
        "pux_harness.acp",
        "--org",
        "general",
        cwd=str(REPO_ROOT),
        env=os.environ,
    ) as (conn, _proc):
        await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=60)
        session = await asyncio.wait_for(conn.new_session(cwd=str(REPO_ROOT)), timeout=60)
        resp = await asyncio.wait_for(
            conn.prompt(
                prompt=[text_block(prompt_text)],
                session_id=session.session_id,
            ),
            timeout=240,
        )
        # Drain any session/update notifications still queued behind the
        # PromptResponse (they arrive as separate JSON-RPC notifications).
        await asyncio.sleep(1.5)
        return _agent_text(client.updates), _update_kinds(client.updates), resp


def test_acp_live_prompt_returns_answer() -> None:
    """Full Zed wire path: subprocess → JSON-RPC → factory → graph → real MiMo →
    streamed ``agent_message_chunk``. Asserts the agent produced a non-empty
    answer — the regression we care about is the connection working end to end,
    not the exact word, so this stays robust against model phrasing variance."""
    text, _kinds, resp = asyncio.run(
        _prompt_and_collect("Reply with exactly one word: ready. Use no tools.")
    )
    assert resp is not None, "prompt returned None"
    assert text.strip(), "agent streamed no message text; prompt produced no answer"


def test_acp_live_streams_tool_call() -> None:
    """Settles the 'no detailed tool calls in Zed' report end to end. MiMo-via-
    OpenCode-Go streams ``tool_call_chunks`` (proven by /tmp/mimo_stream_probe.py);
    this test proves they reach the ACP wire as ``tool_call`` session_updates when
    the model invokes a native tool through the real graph. If this passes, tool
    calls ARE streamed and the Zed symptom is editor-rendering, not our harness.
    If it fails (no tool_call kind), the gap is real and lives in the server's
    streaming translation — the diagnostic value is the same either way."""
    _text, kinds, _resp = asyncio.run(
        _prompt_and_collect(
            "Use the ls tool to list the files in the current directory, "
            "then tell me the count. You MUST call the ls tool."
        )
    )
    assert "tool_call" in kinds or any(
        k and "tool" in k for k in kinds
    ), f"no tool_call session_update streamed; kinds={kinds!r}"

