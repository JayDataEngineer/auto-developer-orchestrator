"""``tools:`` refs → LangChain tools, DERIVED from the REGISTRY.

An agent's ``tools:`` list holds REGISTRY slugs — the same vocabulary the
harness's supervisor surface uses (``kind: tool`` capabilities desugar into
them). Only slugs whose factory is buildable with just a sandbox (no vision
model, no org context) resolve here; any other ref raises with the buildable
list — the wrapper never silently drops a declared tool.
"""
from __future__ import annotations

from typing import TYPE_CHECKING

from langchain_core.tools import BaseTool

from sandbox.exec import shared_backend as _shared_backend
from tools.registry import REGISTRY, Category, Requirements

if TYPE_CHECKING:
    from deepagents.backends.sandbox import BaseSandbox

    from tools.registry import ToolSpec


def _buildable_specs() -> dict[str, ToolSpec]:
    """Specialist REGISTRY slugs whose factory takes just ``sandbox=``. This is
    the DERIVED buildable surface — a new ``Requirements()``-only tool joins
    automatically, no second list to maintain."""
    return {
        s.slug: s for s in REGISTRY
        if s.category is Category.SPECIALIST and s.needs == Requirements()
    }


def resolve_tool_ref(ref: str, *, sandbox: BaseSandbox | None = None) -> BaseTool:
    """One ``tools:`` ref → a live tool.

    The tool executes against the process's shared sandbox (the same
    ``PUX_SANDBOX`` mode the harness runs — ``openshell`` by default,
    ``local`` on the host). ``sandbox`` is injectable for dry-run plans that
    must not touch the sandbox gateway.
    """
    spec = _buildable_specs().get(ref)
    if spec is None or spec.factory is None:
        raise ValueError(
            f"unknown tool ref {ref!r}; buildable refs: {sorted(_buildable_specs())}"
        )
    return spec.factory(sandbox=sandbox if sandbox is not None else _shared_backend())
