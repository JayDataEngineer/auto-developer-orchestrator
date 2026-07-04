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

Run: ``pux acp`` (or ``pux acp --org invest``). Stdin/stdout are the protocol —
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

from pux_harness.agent.graph import build_graph
from pux_harness.agent.orgs import discover_orgs

DEFAULT_ORG = "general"


def _make_factory(org: str) -> Callable[[AgentSessionContext], CompiledStateGraph]:
    """Build a graph factory bound to ``org``.

    ``context.cwd`` (the editor's project dir) is intentionally ignored: the
    Pux sandbox workspace is the bind-mounted project at ``/sandbox/
    workspace/``, fixed by the container, not the editor's cwd. The factory is
    called once and cached by ``AgentServerACP`` (it keys sessions by
    ``thread_id`` in the checkpointer, not by rebuilding the graph), so the org
    cannot vary per session — it is fixed at server startup.
    """

    def factory(_context: AgentSessionContext) -> CompiledStateGraph:
        # build_graph() pulls the shared DockerExecClient + PuxSandboxBackend
        # (process singletons) and compiles model + 13 specialists + subagents.
        # The checkpointer is caller-supplied; MemorySaver keys sessions by
        # thread_id (= ACP session_id). Persistent AsyncSqliteSaver (like
        # server.py) is a deliberate future option, not a v1 need.
        return build_graph(org, checkpointer=MemorySaver())

    return factory


def main() -> None:
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

    known = discover_orgs()
    if args.org not in known:
        # No fallback to a default here — an *explicitly named* unknown org is
        # an operator error, surface it. (Defaulting to `general` when nothing
        # was specified is a default, not a fallback, and happens above.)
        sys.stderr.write(f"pux acp: unknown org {args.org!r}; discovered: {known}\n")
        raise SystemExit(2)

    acp_agent = AgentServerACP(agent=_make_factory(args.org))
    asyncio.run(run_acp_agent(acp_agent))


if __name__ == "__main__":
    main()
