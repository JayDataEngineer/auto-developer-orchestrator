"""Org roster → native dcode ``SubAgent`` profiles (the ``src/profiles/`` layer).

Every rostered agent's merged spec (``extends:`` chains resolved by the kit
loaders) is projected onto the native ``SubAgent`` TypedDict — name,
description, system_prompt, tools, model, middleware — the per-agent
segmentation dcode's middleware threads to the model. ``tools:`` refs resolve
through the tool registry; ``mcp:`` refs must name a server the org has
actually loaded, else ValueError. ``middleware:`` maps onto the native
``RubricMiddleware``.
"""
from __future__ import annotations

from pathlib import Path
from typing import TYPE_CHECKING, Any

from deepagents.middleware.subagents import SubAgent

from profiles.loaders import _load_agent_spec, org_agent_slugs

from middlewares.rubric import agent_middlewares
from tools.resolve import resolve_tool_ref

if TYPE_CHECKING:
    from deepagents.backends.sandbox import BaseSandbox
    from langchain_core.tools import BaseTool


def org_subagent_specs(
    org: str, *,
    project_root: Path,
    mcp_tools_by_server: dict[str, list["BaseTool"]],
    model: Any,
    sandbox: "BaseSandbox | None" = None,
) -> list[SubAgent]:
    """Every rostered agent of ``org`` as a native ``SubAgent`` dict.

    A ``tools:`` ref that no registry entry can build, an ``mcp:`` ref that
    names no loaded server, or an unknown ``middleware:`` ref all raise — the
    org's declared per-agent surface can never silently shrink.
    """
    specs: list[SubAgent] = []
    for slug in org_agent_slugs(org, project_root):
        spec = _load_agent_spec(slug, org, project_root)
        if spec is None:
            raise FileNotFoundError(f"no agent {slug!r} for org {org!r}")

        tools: list["BaseTool"] = [
            resolve_tool_ref(t, sandbox=sandbox) for t in (spec.get("tools") or [])
        ]
        for item in spec.get("mcp") or []:
            ref = item.get("ref") if isinstance(item, dict) else item
            if ref not in mcp_tools_by_server:
                raise ValueError(
                    f"agent {slug!r}: mcp ref {ref!r} is not among the org's "
                    f"loaded servers ({sorted(mcp_tools_by_server)})"
                )
            tools.extend(mcp_tools_by_server[ref])

        subspec: SubAgent = {
            "name": str(spec.get("name") or slug),
            "description": str(spec.get("description") or ""),
            "system_prompt": str(spec.get("system_prompt") or ""),
        }
        if tools:
            subspec["tools"] = tools
        model_name = spec.get("model")
        if isinstance(model_name, str) and model_name.strip():
            subspec["model"] = model_name.strip()
        middleware = agent_middlewares(spec, model=model)
        if middleware:
            # deepagents' SubAgent.typeddict wants AgentMiddleware[AgentState, ...]
            # but RubricMiddleware is AgentMiddleware[RubricState, ...] — the SDK's
            # own typing doesn't compose; runtime is exactly what dcode threads.
            subspec["middleware"] = middleware  # type: ignore[typeddict-item]
        specs.append(subspec)
    return specs
