"""Per-org deepagents graph builder, shared by the in-process runner
(``main.py``) and the Agent Protocol server (``server.py``).

One MCP client + one ``PuxSandboxBackend`` serve the whole process (the bridge
is a single JSON-RPC session; the backend is stateless apart from an
observation log). Per-org compiled graphs are built lazily and cached by the
caller — building is expensive (model init, MCP initialize, tools/list,
subagent assembly) and the only per-org variation is system_prompt +
subagents + the specialist-tool whitelist.
"""
from __future__ import annotations

from typing import Any

from deepagents import create_deep_agent
from langgraph.graph.state import CompiledStateGraph

from pux_harness.bridge import PuxMCPClient, get_pux_client, get_pux_tools
from pux_harness.context_offload import ContextOffloadMiddleware, build_ctx_tools
from pux_harness.model import get_model
from pux_harness.orgs import build_system_prompt, load_subagents
from pux_harness.sandbox import PuxSandboxBackend

_client: PuxMCPClient | None = None
_backend: PuxSandboxBackend | None = None


def shared_client() -> PuxMCPClient:
    """One MCP session for the process. Created on first use so importing this
    module never touches the network (lets tests + `--help` stay cheap)."""
    global _client
    if _client is None:
        _client = get_pux_client()
    return _client


def shared_backend() -> PuxSandboxBackend:
    """One sandbox backend over the shared client."""
    global _backend
    if _backend is None:
        _backend = PuxSandboxBackend(shared_client())
    return _backend


def build_graph(org: str, *, checkpointer: Any) -> CompiledStateGraph:
    """Compile the deepagents graph for ``org`` against ``checkpointer``.

    Specialist ``pux_sandbox_*`` tools come from ``tools=``; native fs/shell
    tools come from ``FilesystemMiddleware`` via the shared backend
    (auto-injected into the main agent + every subagent by ``create_deep_agent``).
    The checkpointer is caller-supplied so the runner can use an ephemeral
    ``MemorySaver`` while the server uses a persistent ``AsyncSqliteSaver``.
    """
    model = get_model()
    tools = get_pux_tools(client=shared_client())
    # Phase 7: ctx_recall/ctx_search ride on the MAIN agent only (they're not in
    # any subagent ``tools:`` whitelist, so excluding them from the subagent-
    # resolution ``tools`` keeps specialist whitelists clean). The offload
    # middleware shares the process-wide store with these tools via shared_store().
    # Main-agent-only: deepagents' SubAgentMiddleware doesn't forward a raw
    # spec's `middleware` key (verified in the Phase 7 E2E), so attaching it to
    # specialists is a silent no-op — see context_offload.py module docstring.
    ctx_tools = build_ctx_tools()
    return create_deep_agent(
        model=model,
        system_prompt=build_system_prompt(org),
        tools=[*tools, *ctx_tools],
        subagents=load_subagents(org, tools),
        middleware=[ContextOffloadMiddleware()],
        backend=shared_backend(),
        checkpointer=checkpointer,
    )
