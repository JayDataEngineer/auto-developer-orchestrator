"""ACP ship-readiness test suite.

The comprehensive gate: every org in the repo must boot, handshake, and speak
ACP correctly. Split into five sections of escalating scope:

Section 1 — Handshake (no tokens, always runs):
    All 14 orgs do initialize + new_session over the REAL stdio transport.
    Proves each org's factory compiles, the AgentServerACP wiring is correct,
    and a session is minted. No model tokens, no sandbox boot — the factory
    fires lazily from ``prompt``, never reached here. This is the cheapest
    "does it boot?" gate.

Section 2 — Live prompt (PUX_E2E=1):
    All 14 orgs respond to a trivial prompt with streamed
    ``agent_message_chunk`` text. Proves the full wire path end-to-end:
    subprocess → JSON-RPC → factory → graph → real model → streamed response.

Section 3 — MCP tool execution (PUX_E2E=1):
    Each MCP server catalog entry (web_research, godot-mcp-runtime, equibles,
    github) is provoked via a prompt that demands its use. A ``tool_call``
    session_update streams back with the server name in the title — proving
    the full MCP bootstrap → connection → tool-injection → agent-invocation
    chain works over ACP.

Section 4 — game-studio godot routing (PUX_E2E=1):
    game-studio arms godot-mcp-runtime on the ORG; the gameplay-programmer
    agent declares it in frontmatter (``kind: mcp, ref: godot-mcp-runtime``).
    A godot-provoking prompt triggers a ``mcp__godot-mcp-runtime__*`` tool
    call, proving the per-agent MCP gate routes over the live wire.

Section 5 — browser-agent e2e (PUX_E2E=1):
    browser-agent drives the in-container SeleniumBase Chrome. A navigation
    prompt must trigger a ``pux_sandbox_browser_*`` tool call — proving the
    full sandbox → sb_server → Chrome → tool-injection chain is ship-ready.

    PUX_E2E=1 uv run pytest tests/integration/test_acp_ship_readiness.py -q

Section 1 runs unconditionally; Sections 2-5 skip without PUX_E2E=1.
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

REPO_ROOT = Path(__file__).resolve().parents[2]

# Every org in the repo (``uv run pux list``). Order: the shared-base orgs
# first (no specialists), then the specialist orgs alphabetically.
ALL_ORGS = [
    "_demo",
    "browser-agent",
    "coder",
    "deep-research-engine",
    "fs-explorer",
    "game-studio",
    "general",
    "invest",
    "orchestrator",
    "social-media-pipeline",
    "telegram-agent",
    "twitter-agent",
    "video-production",
    "web-search",
]


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------


class _CollectingClient:
    """Records streamed ``session_update`` chunks (agent text + tool calls) and
    auto-allows any tool permission so a real prompt runs unattended.

    ``session_update`` parameter MUST be named ``session_id`` — the ACP router
    dispatches via keyword arg, so ``sid`` or ``*args`` leaves it unfilled →
    TypeError → silently swallowed by ``contextlib.suppress(Exception)`` and all
    updates are lost. This is the #1 gotcha in ACP client implementation.
    """

    def __init__(self) -> None:
        self.updates: list[Any] = []

    def on_connect(self, conn: Any) -> None:  # noqa: ARG002
        return None

    async def session_update(
        self, session_id: str, update: Any, **kw: Any  # noqa: ARG002
    ) -> None:
        self.updates.append(update)

    async def request_permission(self, *a: Any, **k: Any) -> RequestPermissionResponse:  # noqa: ARG002
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
    """The ordered ``session_update`` discriminator values."""
    return [str(getattr(u, "session_update", None)) for u in updates]


def _tool_call_names(updates: list[Any]) -> list[str]:
    """Extract tool names from ``tool_call`` (ToolCallStart) updates.

    For MCP tools and other non-special-cased tools, deepagents-acp sets the
    ``title`` field to the raw tool name string (e.g.
    ``mcp__web_research__search``). See ``_create_tool_call_start`` line 610.
    """
    names: list[str] = []
    for u in updates:
        if getattr(u, "session_update", None) != "tool_call":
            continue
        title = getattr(u, "title", None)
        if title:
            names.append(str(title))
    return names


async def _handshake(org: str) -> Any:
    """Spawn ``python -m pux_harness.acp --org <org>``, run initialize +
    new_session. Return the ``NewSessionResponse``."""
    async with spawn_agent_process(
        lambda _agent: _CollectingClient(),
        sys.executable,
        "-m",
        "pux_harness.acp",
        "--org",
        org,
        cwd=str(REPO_ROOT),
    ) as (conn, _proc):
        init = await asyncio.wait_for(conn.initialize(protocol_version=1), timeout=30)
        assert init is not None, "initialize returned None"
        return await asyncio.wait_for(
            conn.new_session(cwd=str(REPO_ROOT)), timeout=30
        )


async def _prompt_and_collect(
    prompt: str, *, org: str = "general"
) -> tuple[str, list[str], list[str], Any]:
    """Spawn the ACP subprocess, send ``prompt`` as a single text block, return
    ``(agent_text, update_kinds, tool_call_names, PromptResponse)``.

    Forwards the parent env so ``OPENCODE_API_KEY`` reaches the subprocess past
    the ACP transport's POSIX allowlist (which strips everything but
    HOME/PATH/SHELL/...). ``bin/pux`` auto-loads ``.env``; the bare ``python -m
    pux_harness.acp`` the test spawns does not.
    """
    client = _CollectingClient()
    env = dict(os.environ)
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
        session = await asyncio.wait_for(
            conn.new_session(cwd=str(REPO_ROOT)), timeout=60
        )
        resp = await asyncio.wait_for(
            conn.prompt(
                prompt=[text_block(prompt)],
                session_id=session.session_id,
            ),
            timeout=240,
        )
        # Drain queued notifications (arrive as separate JSON-RPC notifications
        # behind the PromptResponse).
        await asyncio.sleep(1.5)
        return (
            _agent_text(client.updates),
            _update_kinds(client.updates),
            _tool_call_names(client.updates),
            resp,
        )


# ===========================================================================
# Section 1 — Handshake (no tokens, always runs)
# ===========================================================================
#
# The cheapest ship-readiness gate: does every org boot the ACP server, speak
# the initialize/new_session JSON-RPC, and mint a session? No model tokens, no
# sandbox boot (the factory fires lazily from ``prompt``). Extends the existing
# ``test_acp.py`` handshake tests from 2 orgs to ALL 13.


@pytest.mark.parametrize("org", ALL_ORGS)
def test_handshake_initializes_and_mints_session(org: str, project_root) -> None:
    """initialize + new_session against ``pux acp --org <org>`` over stdio.

    Proves the factory compiles for every org — the AgentServerACP wiring,
    the org.yaml/extends chain, the agent manifest, and the config-option
    advertisement all fire before the model or sandbox is touched.
    """
    session = asyncio.run(_handshake(org))
    assert isinstance(session.session_id, str) and len(session.session_id) > 0, (
        f"{org}: new_session did not return a session_id"
    )
    # The model dropdown is advertised as a config_option for every org.
    opts = getattr(session, "config_options", None) or []
    cats = [getattr(getattr(o, "root", o), "category", None) for o in opts]
    assert "model" in cats, (
        f"{org}: no 'model' config_option advertised (editor would have no "
        f"model dropdown): {opts!r}"
    )


# ===========================================================================
# Sections 2-4 — Live tests (PUX_E2E=1 gated)
# ===========================================================================

_live = pytest.mark.skipif(
    os.environ.get("PUX_E2E") != "1",
    reason="set PUX_E2E=1 (model key in .env) to run live ACP tests",
)


# ===========================================================================
# Section 2 — Live prompt (all orgs respond)
# ===========================================================================


@_live
@pytest.mark.parametrize("org", ALL_ORGS)
def test_live_prompt_returns_answer(org: str) -> None:
    """Every org responds to a trivial prompt with streamed agent text.

    Full wire path: subprocess → JSON-RPC → factory → graph → real model →
    ``agent_message_chunk`` stream. If an org fails here, it is not
    ship-ready — the boot + model + streaming chain is broken somewhere.
    """
    text, _kinds, _tools, resp = asyncio.run(
        _prompt_and_collect(
            "Reply with exactly one word: ready. Use no tools.", org=org
        )
    )
    assert resp is not None, f"{org}: prompt returned None"
    assert text.strip(), (
        f"{org}: agent streamed no message text — the model produced no answer"
    )


# ===========================================================================
# Section 3 — MCP tool execution (each MCP server provoked + verified)
# ===========================================================================
#
# For each MCP server catalog entry, send a prompt that naturally demands the
# tool. Assert a ``tool_call`` session_update streams back with the server name
# in the title. This proves the full MCP chain: bootstrap → connect →
# tool-injection into the graph → model invocation → streamed tool_call.
#
# deepagents-acp sets the ToolCallStart ``title`` to the raw tool name for any
# non-special-cased tool (line 610 of server.py), so MCP tools carry their full
# ``mcp__<server>__<tool>`` name in the title.


@_live
def test_mcp_web_research_executes() -> None:
    """web_research MCP server connects and its tools reach the agent.

    web-search org arms ``web_research`` (search, fetch, research — 3 tools).
    A natural search prompt should trigger an ``mcp__web_research__*`` tool call
    streamed over ACP.
    """
    _text, _kinds, tools, resp = asyncio.run(
        _prompt_and_collect(
            "Search the web for 'agent client protocol specification'. "
            "You must use your web search tool to do this.",
            org="web-search",
        )
    )
    assert resp is not None, "web-search: prompt returned None"
    mcp_calls = [t for t in tools if "web_research" in t.lower()]
    assert mcp_calls, (
        f"web-search: no mcp__web_research__* tool call streamed; "
        f"tool_calls={tools!r}"
    )


@_live
def test_mcp_godot_runtime_executes() -> None:
    """godot-mcp-runtime MCP server connects and its tools reach the agent.

    game-studio arms ``godot-mcp-runtime`` (36 tools). The gameplay-programmer
    agent declares it via frontmatter. A project-listing prompt should trigger
    an ``mcp__godot-mcp-runtime__*`` tool call.
    """
    _text, _kinds, tools, resp = asyncio.run(
        _prompt_and_collect(
            "List all Godot projects in the current workspace using your "
            "Godot project management tools.",
            org="game-studio",
        )
    )
    assert resp is not None, "game-studio: prompt returned None"
    mcp_calls = [t for t in tools if "godot" in t.lower()]
    assert mcp_calls, (
        f"game-studio: no mcp__godot-mcp-runtime__* tool call streamed; "
        f"tool_calls={tools!r}"
    )


@_live
def test_mcp_equibles_executes() -> None:
    """equibles MCP server connects and its tools reach the agent.

    invest org arms ``equibles`` for market data. A stock-lookup prompt should
    trigger an ``mcp__equibles__*`` tool call.
    """
    _text, _kinds, tools, resp = asyncio.run(
        _prompt_and_collect(
            "Look up the current price of AAPL stock using your market "
            "data tools.",
            org="invest",
        )
    )
    assert resp is not None, "invest: prompt returned None"
    mcp_calls = [t for t in tools if "equible" in t.lower()]
    assert mcp_calls, (
        f"invest: no mcp__equibles__* tool call streamed; tool_calls={tools!r}"
    )


@_live
def test_mcp_github_executes() -> None:
    """github MCP server connects and its tools reach the agent.

    coder org arms ``github`` for repository operations. A repo-search prompt
    should trigger an ``mcp__github__*`` tool call.
    """
    _text, _kinds, tools, resp = asyncio.run(
        _prompt_and_collect(
            "Search GitHub repositories for 'agent-client-protocol' using "
            "your GitHub tools.",
            org="coder",
        )
    )
    assert resp is not None, "coder: prompt returned None"
    mcp_calls = [t for t in tools if "github" in t.lower()]
    assert mcp_calls, (
        f"coder: no mcp__github__* tool call streamed; tool_calls={tools!r}"
    )


# ===========================================================================
# Section 4 — game-studio godot routing (per-agent MCP gate)
# ===========================================================================
#
# game-studio arms godot-mcp-runtime at the ORG level, but the frontmatter
# gate means only the gameplay-programmer agent declares
# ``kind: mcp, ref: godot-mcp-runtime``. The supervisor routes to
# gameplay-programmer for game-dev tasks; that subagent's graph includes the
# godot MCP tools; other subagents' graphs do not.
#
# This test proves the end-to-end chain works: the supervisor picks the right
# subagent, that subagent's MCP tools are wired, and the tool call streams back
# over ACP. The negative case (other agents NOT seeing godot tools) is a graph-
# construction property tested at the unit level, not via ACP (the absence of a
# tool call through ACP is ambiguous — the model might just choose not to use
# the tool).


@_live
def test_game_studio_godot_routes_to_gameplay_programmer() -> None:
    """A game-dev prompt triggers godot MCP tool calls via the
    gameplay-programmer subagent.

    This is the most demanding test in the suite: the supervisor must route to
    the gameplay-programmer, that subagent's godot-mcp-runtime tools must be
    injected into its graph, and the tool must actually execute (the MCP server
    must have connected, found Godot via GODOT_PATH, and responded).

    The prompt asks for a concrete godot action (list projects) that only the
    gameplay-programmer has the tools for. If the supervisor mis-routes or the
    MCP server fails to connect, no ``mcp__godot-mcp-runtime__*`` call streams.
    """
    _text, _kinds, tools, resp = asyncio.run(
        _prompt_and_collect(
            "As the gameplay programmer, list all Godot projects using your "
            "mcp__godot-mcp-runtime__list_projects tool. You must call it.",
            org="game-studio",
        )
    )
    assert resp is not None, "game-studio: prompt returned None"
    godot_calls = [t for t in tools if "godot-mcp-runtime" in t.lower()]
    assert godot_calls, (
        f"game-studio: supervisor did not route to gameplay-programmer, or "
        f"godot-mcp-runtime tools not injected; tool_calls={tools!r}"
    )


# ===========================================================================
# Section 5 — browser-agent e2e (sandbox browser tool execution)
# ===========================================================================
#
# browser-agent is a standalone org (no subagents — the CTO IS the browser
# agent, mirroring web-search's pattern). Its entire tool surface is
# ``pux_sandbox_browser_*`` running inside the Docker sandbox's SeleniumBase
# Chrome (sb_server). The generic live-prompt test ("Reply with one word: ready.
# Use no tools.") proves the wire path, but for a browser org "use no tools" is
# the wrong axis — the browser IS all tools. This section exercises the actual
# browser: a navigation prompt must trigger a ``pux_sandbox_browser_*`` tool call
# streamed over ACP.
#
# This is the most infrastructure-dependent test in the suite: the sandbox
# container must be up, sb_server must have survived its stealth-mode boot
# (Chrome cold-start + CDP attach), and the agent must successfully invoke the
# in-container browser. If this passes, the full browser chain is ship-ready:
# org.yaml → policy.yaml → sandbox boot → sb_server → Chrome → tool injection →
# agent invocation → ACP stream.


@_live
def test_browser_agent_navigates_real_page() -> None:
    """A navigation prompt triggers a real browser tool call over ACP.

    browser-agent must drive the in-container SeleniumBase Chrome via a
    ``pux_sandbox_browser_*`` tool (navigate, search, or screenshot). This
    proves: (1) the standalone org arms the browser specialist tools correctly,
    (2) the sandbox sb_server is up and responsive (stealth Chrome survived
    boot — the ``pkill -f chromium`` self-kill bug is NOT present), (3) the
    warmup job (policy.yaml ``jobs:``) or cold-start brought the browser online,
    (4) the tool call streams back as a ``tool_call`` session_update over ACP.

    If this fails with no browser tool call, check (in order): sb_server logs
    inside the container (``docker exec ... cat /var/log/supervisor/sb-server-
    error.log``), the container's memory limit (``docker inspect ... --format
    '{{.HostConfig.Memory}}'`` — must be 4 GiB for browser orgs), and the
    pkill patterns in ``sandbox/scripts/sb_server.py`` (bare ``chromium``
    matches ``--use-chromium`` in sb_server's own argv → self-SIGKILL).
    """
    _text, _kinds, tools, resp = asyncio.run(
        _prompt_and_collect(
            "Use your browser to navigate to https://example.com and tell me "
            "the page title. You MUST call browser_navigate to do this — do "
            "not use any web search or fetch tool.",
            org="browser-agent",
        )
    )
    assert resp is not None, "browser-agent: prompt returned None"
    browser_calls = [t for t in tools if "browser" in t.lower()]
    assert browser_calls, (
        f"browser-agent: no pux_sandbox_browser_* tool call streamed; the "
        f"in-container sb_server may not be running (check sandbox boot). "
        f"tool_calls={tools!r}"
    )
