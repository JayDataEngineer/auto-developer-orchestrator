"""CompositeBackend factory for memory routing.

Routes ``/memories/*`` to a ``StoreBackend`` (persistent, agent-managed) and
everything else to the existing ``PuxSandboxBackend`` (sandbox fs/shell).

The ``CompositeBackend`` is constructed as a *factory* (``lambda rt: ...``) so
that ``StoreBackend`` receives the runtime context needed to resolve the
namespace. This matches the deepagents pattern where ``backend=`` can be a
callable that takes ``Runtime`` and returns a ``BackendProtocol``.
"""
from __future__ import annotations

from typing import TYPE_CHECKING

from deepagents.backends.composite import CompositeBackend
from deepagents.backends.state import StateBackend

if TYPE_CHECKING:
    from langgraph.store.base import BaseStore

    from pux_harness.sandbox.backend import PuxSandboxBackend

from pux_harness.memory.config import memory_namespace


def build_memory_backend(
    org: str,
    default_backend: PuxSandboxBackend,
    store: BaseStore | None = None,
) -> tuple[callable, BaseStore | None]:
    """Build the composite backend and store for memory.

    Returns:
        ``(backend_factory, store)`` — the factory is passed as ``backend=``
        to ``create_deep_agent()``; the store is passed as ``store=``.

        When ``store`` is ``None``, ``StoreBackend`` falls back to the
        in-graph store (``get_store()``). Passing an explicit store gives
        the caller control over persistence (``InMemoryStore`` for the
        runner, ``AsyncSqliteSaver``-backed for the server).
    """
    namespace_fn = memory_namespace(org)

    def _backend_factory(rt):
        """Resolve composite backend at graph execution time.

        ``StoreBackend`` needs the runtime to resolve the namespace factory.
        ``StateBackend`` is used as the memory-side default when no explicit
        store is provided (ephemeral, thread-scoped).
        """
        from deepagents.backends.store import StoreBackend

        memory_store = StoreBackend(
            store=store,
            namespace=namespace_fn,
        )
        return CompositeBackend(
            default=default_backend,
            routes={"/memories/": memory_store},
        )

    return _backend_factory, store
