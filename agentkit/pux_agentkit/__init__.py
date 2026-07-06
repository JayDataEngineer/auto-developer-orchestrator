"""pux_agentkit — a standalone org + skill compiler.

Turn a folder of org + skills into a running deepagents agent with NO Docker
harness dependency. The typical consumer is a different, standalone project
(e.g. a Wan2GP + CopilotKit app) that authors its own org + a skill and wires
the compiled graph to its UI.

Quick start::

    from pux_agentkit import compile_org
    from langgraph.checkpoint.memory import MemorySaver

    graph = compile_org(
        "my_org",
        model=my_chat_model,
        tools=[my_wan2gp_tool],
        project_root="./my_app",
        checkpointer=MemorySaver(),
    )
    graph.invoke({"messages": [{"role": "user", "content": "..."}]})

See ``agentkit/README.md`` for the org + skill format and a full walk-through.
"""
from __future__ import annotations

from pux_agentkit.compile import compile_org, load_subagents
from pux_agentkit.loaders import (
    build_system_prompt,
    discover_orgs,
    load_org_prompt,
    load_root_prompt,
    org_agent_slugs,
)

__all__ = [
    "compile_org",
    "load_subagents",
    "discover_orgs",
    "org_agent_slugs",
    "load_root_prompt",
    "load_org_prompt",
    "build_system_prompt",
]

__version__ = "0.1.0"
