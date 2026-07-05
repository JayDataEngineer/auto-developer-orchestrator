"""ACP (Agent Client Protocol) stdio server — exposes ``build_graph(org)`` to
editors (Zed / VS Code via vscode-acp / Neovim). The editor IS the TUI; this
module owns no UI code.

ACP (``agentclientprotocol.com``) is a stdio JSON-RPC protocol between coding
agents and editors. ``deepagents-acp``'s ``AgentServerACP`` wraps a deepagents
graph as such a server. We hand it a **factory** that returns the compiled
per-org graph; the server caches the first build (one graph instance serves all
sessions, keyed by ``thread_id=session_id`` in the checkpointer) — so the org
is fixed at startup, not per-session.

Org resolution (first wins): ``--org`` flag → ``$PUX_ORG`` → ``general``. An
unknown org fails loud. The sandbox self-boots lazily on first tool use
(``build_graph`` → ``shared_backend()`` → ``shared_exec()`` → ``ensure()``,
Phase 8g), so — like ``pux direct`` — ``pux acp`` needs no prior
``pux sandbox start``.

Run: ``pux acp --org invest``. Stdin/stdout are the protocol —
this process must not print to stdout. Errors go to stderr.
"""
from __future__ import annotations

import argparse
import asyncio
import os
import sys
from collections.abc import Callable

from acp import run_agent as run_acp_agent
from deepagents_acp.server import AgentServerACP, AgentSessionContext
from langgraph.checkpoint.memory import MemorySaver
from langgraph.graph.state import CompiledStateGraph

from langchain_core.tools import BaseTool

from pux_harness.agent.graph import build_graph
from pux_harness.agent.model import resolve_model_id
from pux_harness.agent.orgs import discover_orgs
from pux_harness.agent.tool_servers import resolve_tool_servers

DEFAULT_ORG = "general"


def _make_factory(
    org: str, mcp_tools: list[BaseTool] | None = None,
) -> Callable[[AgentSessionContext], CompiledStateGraph]:
    """Build a graph factory bound to ``org``.

    ``context.cwd`` (the editor's project dir) is intentionally ignored: the
    Pux sandbox workspace is the bind-mounted project at ``/sandbox/
    workspace/``, fixed by the container, not the editor's cwd. The factory is
    called once and cached by ``AgentServerACP`` (it keys sessions by
    ``thread_id`` in the checkpointer, not by rebuilding the graph), so the org
    cannot vary per session — it is fixed at server startup.
    """

    _tools = list(mcp_tools) if mcp_tools else []

    def factory(_context: AgentSessionContext) -> CompiledStateGraph:
        return build_graph(org, checkpointer=MemorySaver(), mcp_tools=_tools)

    return factory


def _advertised_models(org: str) -> list[dict[str, str]]:
    """The model selector the editor (Zed) sees for this org's agent.

    ``AgentServerACP`` only populates ``new_session.config_options`` — the model
    dropdown the editor renders — when ``models=[...]`` is passed at construction.
    Pass nothing and the editor falls back to its own built-in model list: Zed
    shows ChatGPT/OpenAI models even though this agent runs MiMo via OpenCode Go
    (the "asks for OpenAI models" bug).

    The advertised id is the main agent's base-role model — the SAME id
    ``build_graph`` compiles — resolved via ``resolve_model_id`` (id only: no
    ``ChatOpenAI``, no key, no network). So what the editor shows == what runs.

    The factory ignores ``context.model``, so the single advertised option is
    authoritative. Honoring Zed-side model switching (threading ``context.model``
    into ``build_graph``) is a deliberate future option, not v1.
    """
    mid = resolve_model_id(role="base", org=org)
    return [{"value": mid, "name": mid, "description": "OpenCode Go (MiMo)"}]


# --- Public API (called from the unified CLI) ---------------------------------


async def _acp_main(org: str) -> None:
    """Async wrapper that opens MCP sessions, builds the ACP server, then runs."""
    from pux_harness.agent.mcp_client import McpSessionManager  # noqa: PLC0415
    mcp_tools: list[BaseTool] = []
    _mcp_mgr = None
    try:
        specs = resolve_tool_servers(org)
        if specs:
            _mcp_mgr = McpSessionManager(org, specs)
            await _mcp_mgr.open()
            mcp_tools = _mcp_mgr.tools
    except Exception as exc:
        sys.stderr.write(f"pux acp: tool_servers resolution failed: {exc}\n")
    acp_agent = AgentServerACP(
        agent=_make_factory(org, mcp_tools=mcp_tools),
        models=_advertised_models(org),
    )
    await run_acp_agent(acp_agent)
    if _mcp_mgr is not None:
        await _mcp_mgr.close()


def run_acp(org: str = DEFAULT_ORG) -> None:
    """Run the deepagents org graph as an ACP stdio server (editor = TUI)."""
    known = discover_orgs()
    if org not in known:
        sys.stderr.write(f"pux acp: unknown org {org!r}; discovered: {known}\n")
        raise SystemExit(2)

    asyncio.run(_acp_main(org))


def main() -> None:
    """Legacy CLI entry point (argparse). Replaced by ``pux_harness.cli.main``."""
    ap = argparse.ArgumentParser(
        prog="pux acp",
        description="Run the deepagents org graph as an ACP stdio server (editor = TUI).",
    )
    ap.add_argument(
        "--org",
        default=os.environ.get("PUX_ORG", DEFAULT_ORG),
        help=f"org to serve (default: $PUX_ORG or {DEFAULT_ORG!r})",
    )
    args = ap.parse_args()
    run_acp(args.org)


if __name__ == "__main__":
    main()
