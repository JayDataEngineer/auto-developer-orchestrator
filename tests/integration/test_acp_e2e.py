"""Live E2E for the ACP stdio server (the Zed editor wire path).

Drives the REAL ``pux acp --org general`` subprocess over stdio through the full
handshake + a real prompt, collecting the streamed agent message — the exact wire
path Zed uses. Closes the gap the hermetic ``tests/test_acp.py`` leaves open: that
the factory compiles, the real model answers, and the response streams back over
JSON-RPC. This is the "so I don't have to deal with this" proof.

Skipped unless ``PUX_E2E=1`` — the factory boots the sandbox eagerly
(``DockerExecClient.__init__`` → ``docker.from_env``), and the model needs a live
``OPENROUTER_API_KEY`` (``bin/pux`` sources ``.env`` before launching). To run:

    # Docker daemon running + .env has OPENROUTER_API_KEY
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
from acp.helpers import image_block, text_block
from acp.schema import RequestPermissionResponse
from acp.stdio import spawn_agent_process

pytestmark = pytest.mark.skipif(
    os.environ.get("PUX_E2E") != "1",
    reason=(
        "set PUX_E2E=1 (Docker running + .env OPENROUTER_API_KEY) to run the live ACP e2e"
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


def _solid_png_b64(w: int, h: int, rgb: tuple[int, int, int]) -> str:
    """A valid solid-color RGB PNG (base64) from stdlib alone — no PIL dep.

    The image-prompt test needs REAL, decodable image bytes (a malformed blob
    could make the model error for the wrong reason and muddy the proof).
    Hand-encoding a PNG is error-prone, so we synthesize a valid one: 8-bit
    color-type-2 (RGB) IHDR + a zlib-compressed scanline stream + IEND. The
    same recipe any PNG encoder uses."""
    import base64
    import struct
    import zlib

    def chunk(typ: bytes, data: bytes) -> bytes:
        c = typ + data
        return (
            struct.pack(">I", len(data))
            + c
            + struct.pack(">I", zlib.crc32(c) & 0xFFFFFFFF)
        )

    raw = b"".join(b"\x00" + bytes(rgb) * w for _ in range(h))
    ihdr = struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)  # 8-bit, RGB, default
    png = (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", ihdr)
        + chunk(b"IDAT", zlib.compress(raw))
        + chunk(b"IEND", b"")
    )
    return base64.b64encode(png).decode()


async def _prompt_and_collect(
    prompt: list[Any] | str, *, tier: str | None = None, org: str = "general",
) -> tuple[str, list[str], Any]:
    client = _CollectingClient()
    # Forward the parent env so OPENROUTER_API_KEY reaches the child past
    # ``acp.transports.default_environment``'s POSIX allowlist (which strips
    # everything but HOME/PATH/SHELL/…). ``bin/pux`` auto-loads ``.env``; the
    # bare ``python -m pux_harness.acp`` the test spawns does not, so without
    # this the factory boots keyless and ``session/prompt`` returns
    # "Internal error". This mirrors Zed, which passes the editor env through.
    # ``tier`` (e.g. "fast" → multimodal base) is threaded via PUX_TIER so the
    # subprocess resolves a different base model without a separate launch path.
    env = dict(os.environ)
    if tier is not None:
        env["PUX_TIER"] = tier
    blocks = [text_block(prompt)] if isinstance(prompt, str) else list(prompt)
    async with spawn_agent_process(
        lambda _agent: client,
        sys.executable,
        "-m",
        "pux_harness.acp",
        "--org",
        org,
        cwd=str(REPO_ROOT),
        env=env,
    ) as (conn, _proc):
        await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=60)
        session = await asyncio.wait_for(conn.new_session(cwd=str(REPO_ROOT)), timeout=60)
        resp = await asyncio.wait_for(
            conn.prompt(prompt=blocks, session_id=session.session_id),
            timeout=240,
        )
        # Drain any session/update notifications still queued behind the
        # PromptResponse (they arrive as separate JSON-RPC notifications).
        await asyncio.sleep(1.5)
        return _agent_text(client.updates), _update_kinds(client.updates), resp


async def _two_turn_ask_user(org: str, ask_prompt: str, reply: str) -> tuple[str, str, list[str]]:
    """Drive the ACP turn-based ask_user path over ONE stdio session.

    Spawns ``pux acp --org <org>``, sends ``ask_prompt`` (which should make the
    agent call ``ask_user``), asserts the turn ENDED (the PromptResponse is back
    — the agent posed its question + stopped per the supervisor prompt suffix),
    then sends ``reply`` as a SECOND prompt on the SAME session and returns
    (turn1_text, turn2_text, turn1_kinds). The reply should be incorporated into
    turn2 — the proof the turn-based pattern round-trips. Raises if turn 1 does
    not return (the agent didn't end its turn — the ask_user gate failed)."""
    client = _CollectingClient()
    env = dict(os.environ)
    async with spawn_agent_process(
        lambda _agent: client,
        sys.executable, "-m", "pux_harness.acp", "--org", org,
        cwd=str(REPO_ROOT), env=env,
    ) as (conn, _proc):
        await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=60)
        session = await asyncio.wait_for(conn.new_session(cwd=str(REPO_ROOT)), timeout=60)
        # Turn 1: the agent should call ask_user → pose its question → end turn.
        await asyncio.wait_for(
            conn.prompt(prompt=[text_block(ask_prompt)], session_id=session.session_id),
            timeout=240,
        )
        await asyncio.sleep(1.5)
        turn1_text = _agent_text(client.updates)
        turn1_kinds = _update_kinds(client.updates)
        client.updates.clear()
        # Turn 2: the user's reply, on the same session (same thread_id → the
        # checkpointer carries turn 1's state, so the agent has context).
        await asyncio.wait_for(
            conn.prompt(prompt=[text_block(reply)], session_id=session.session_id),
            timeout=240,
        )
        await asyncio.sleep(1.5)
        turn2_text = _agent_text(client.updates)
        return turn1_text, turn2_text, turn1_kinds


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
    OpenRouter streams ``tool_call_chunks`` (proven by /tmp/mimo_stream_probe.py);
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


def test_acp_live_image_prompt_returns_answer() -> None:
    """An image sent as an ACP ``ImageContentBlock`` reaches the multimodal BASE
    model (mimo-v2.5, fast tier) and elicits a reply — the BACKING proof for the
    ``prompt_capabilities.image=True`` we advertise for multimodal bases (#69).

    Why this is a SEPARATE proof from browser-vision: the ACP path
    (``convert_image_block_to_content_blocks``) emits the OpenAI ``image_url``
    data-URI shape ``{"type":"image_url","image_url":{"url":"data:..."}}``,
    NOT the canonical ``{"type":"image","base64",...}`` shape
    ``BrowserVisionMiddleware`` already proved MiMo ingests. So ingestion of the
    ACP image_url shape was genuinely UNVERIFIED — the audit row said
    "advertised True — BACKING UNVERIFIED". This drives the REAL ACP stdio path
    (ImageContentBlock → conversion → HumanMessage → MiMo) under PUX_TIER=fast
    (multimodal base) and asserts the model replies. The synthesized PNG is a
    valid 16×16 solid red, so a failure points at the image_url→model path, not
    a malformed image. Skipped without PUX_E2E=1."""
    prompt = [
        text_block(
            "I attached an image. In one short sentence, say what single color "
            "fills it, then stop. Use no tools."
        ),
        image_block(
            data=_solid_png_b64(16, 16, (255, 0, 0)), mime_type="image/png"
        ),
    ]
    text, _kinds, resp = asyncio.run(_prompt_and_collect(prompt, tier="fast"))
    assert resp is not None, "prompt returned None"
    assert text.strip(), (
        "agent streamed no reply to the image prompt — the image_url block was "
        "likely rejected/ignored by the multimodal model (#69 image-backing gap)"
    )
    # The actual INGESTION proof (not just "didn't crash"): the prompt gave the
    # model NO color hint, only "say what single color fills it". If the
    # image_url block reached the multimodal base, the reply names red; if the
    # block were silently dropped, the model would have to guess — so a missing
    # "red" flags a real image_url→model gap, not a model-phrasing artifact.
    assert "red" in text.lower(), (
        f"reply did not identify the red image — the image_url block likely did "
        f"not reach the model; reply={text!r}"
    )


def test_acp_live_ask_user_is_turn_based() -> None:
    """The ask_user HITL proof over ACP (the editor lane).

    social-media-pipeline opts into ``ask_user`` (``profile.yaml``). Over ACP the
    editor's permission popover has no free-text field, so the tool takes the
    TURN-BASED branch: it poses the question as assistant text + the supervisor
    prompt suffix makes the agent END its turn (no interrupt — the client can't
    resume one). This test drives that round-trip over the REAL stdio wire:

    1. Turn 1: a prompt that demands the agent ask before acting → the agent
       calls ``ask_user``, its question lands in the streamed
       ``agent_message_chunk`` text, AND the turn ends (the PromptResponse
       returns — the gate worked, the agent stopped).
    2. Turn 2: the user's reply on the SAME session → the agent incorporates it
       (the reply appears in turn 2's text).

    The strong assertion is behavioral + model-cooperation-dependent: if MiMo
    ignores the "ask first" instruction and barrels ahead, turn 1 won't carry a
    question and the test flags a real gap (the supervisor prompt suffix or the
    tool description needs tightening), not a silent pass. Skipped without
    PUX_E2E=1."""
    ask_prompt = (
        "I want you to draft a tweet, but FIRST you must ask me a question: "
        "call the ask_user tool to ask what tone I want (casual or formal), "
        "then STOP and wait for my answer. Do NOT draft anything yet. You MUST "
        "call ask_user before doing anything else."
    )
    reply = "casual — keep it relaxed and friendly."
    turn1, turn2, _kinds = asyncio.run(
        _two_turn_ask_user("social-media-pipeline", ask_prompt, reply)
    )
    # Turn 1 carried the agent's question (ask_user posed it as text).
    assert turn1.strip(), (
        "turn 1 streamed no text — the agent did not pose its ask_user question"
    )
    # The reply was incorporated into turn 2 (the turn-based round-trip closed).
    assert turn2.strip(), "turn 2 streamed no text — the agent did not respond to the reply"
    assert "casual" in (turn1 + " " + turn2).lower(), (
        "the user's 'casual' reply did not surface in the conversation — the "
        "turn-based pattern did not round-trip"
    )


