"""Tests for ``pux_harness.memory.backend.build_memory_backend`` — the
CompositeBackend instance that routes ``/memories/`` to a ``StoreBackend``.

The crash these guard against (caught by the live E2E for
``pux direct``): ``MemoryMiddleware.before_agent`` downloads memory files at
graph startup via ``backend.adownload_files`` → ``StoreBackend.download_files``
→ ``store.get``. If the ``StoreBackend`` is built with ``store=None`` that call
dies with ``AttributeError: 'NoneType' object has no attribute 'get'`` —
blocking EVERY ``pux direct`` run before the model is even called.

``test_graph.py`` MOCKS ``build_memory_backend`` (returns ``(MagicMock(),
None)``), so it never exercises the real store plumbing — green unit tests
alongside a totally broken live runner ([[feedback_prepare_wiring_e2e_gap]]:
a wiring seam proven only by a mocked unit test is UNPROVEN). These tests drive
the REAL factory.
"""
from __future__ import annotations

from unittest.mock import MagicMock

from langgraph.store.memory import InMemoryStore

from pux_harness.memory.backend import build_memory_backend


def _default_backend():
    # PuxSandboxBackend is only the "everything else" route target — its
    # internals aren't exercised here, so a stand-in is enough.
    return MagicMock(name="default_backend")


def test_store_none_supplies_in_memory_store_not_none():
    # THE CRASH GUARD. A None store MUST NOT reach StoreBackend — it has no
    # in-graph fallback and dies on the first download_files. The builder
    # supplies an InMemoryStore so the ephemeral runner path works without the
    # caller having to know this.
    backend, store = build_memory_backend(
        org="general", default_backend=_default_backend(), store=None,
    )
    assert store is not None
    assert isinstance(store, InMemoryStore)


def test_store_backend_uses_non_none_store():
    # The StoreBackend the builder constructs must resolve to a NON-None store
    # at call time — this is the exact value the crash dereferenced
    # (download_files → store.get on None). Drives the REAL builder (not a mock)
    # with store=None, then asks the backend for the store it would actually
    # use (``_get_store`` is StoreBackend's own resolution method — init store
    # wins, the get_store() fallback is never reached).
    backend, store = build_memory_backend(
        org="general", default_backend=_default_backend(), store=None,
    )
    # CompositeBackend.routes maps the prefix → the StoreBackend instance.
    store_backends = [v for v in backend.routes.values()]
    assert store_backends, "expected a /memories/ route to a StoreBackend"
    sb = store_backends[0]
    assert sb._get_store() is not None
    assert sb._get_store() is store  # same object the graph gets via create_deep_agent


def test_caller_supplied_store_is_preserved_not_replaced():
    # A caller that wants cross-restart survival (the server) passes its own
    # store; the builder must use THAT, not swap in an InMemoryStore.
    caller_store = InMemoryStore()
    backend, store = build_memory_backend(
        org="dev-bot", default_backend=_default_backend(), store=caller_store,
    )
    assert store is caller_store
    sb = list(backend.routes.values())[0]
    assert sb._get_store() is caller_store


def test_namespace_is_project_scoped_not_org_scoped():
    # memory_namespace ignores ``org`` by design — memory is shared across all
    # orgs in the same working directory (Claude's per-project model). Two orgs
    # therefore resolve to the SAME namespace. Locks that contract so the org
    # arg isn't mistakenly wired in later.
    from pux_harness.memory.config import memory_namespace

    ns_a = memory_namespace("general")(rt=MagicMock())
    ns_b = memory_namespace("dev-bot")(rt=MagicMock())
    assert ns_a == ns_b
