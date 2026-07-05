"""Memory paths and namespace configuration.

Memory lives in ``.pux/memory/`` (gitignored) — agent-managed, not tracked.
The namespace ``(org,)`` scopes memory per-org so each CTO builds its own
accumulated knowledge across conversations.
"""
from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from langgraph.runtime import Runtime

MEMORY_SOURCES: list[str] = ["/memories/AGENTS.md"]
"""Memory file paths loaded at conversation start. The agent can create
additional files under ``/memories/`` during conversations, but only these
are injected into the system prompt at startup."""


def memory_namespace(org: str) -> callable:
    """Return a namespace factory that scopes memory to ``org``.

    The factory signature matches ``StoreBackend``'s ``NamespaceFactory``
    protocol: ``(Runtime) -> tuple[str, ...]``.

    Using a closure over ``org`` keeps the namespace deterministic —
    the same org always reads/writes the same memory shard, regardless
    of which user or thread is active.
    """

    def _namespace(rt: Runtime) -> tuple[str, ...]:
        return (org,)

    return _namespace
