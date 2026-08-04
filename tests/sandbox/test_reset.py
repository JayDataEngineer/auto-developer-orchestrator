"""reset() + exec re-ensure: the stuck-sandbox recovery path.

``SandboxContainer.reset()`` force-removes the container WITHOUT saving state
(fast — won't hang on a wedged container the way ``destroy()`` can). The agent
process's exec client is a module singleton that caches the container name, so
after a reset it holds a STALE name; ``_do_exec`` MUST clear that cache and
re-ensure on ``NotFound`` so the next tool call auto-recovers instead of dying
with 'vanished mid-run'. Together these back the ``reset_session`` MCP primitive.
Fakes only — no Docker daemon required.
"""
from __future__ import annotations

import pytest
from docker.errors import NotFound

from pux_harness.sandbox.container import SandboxContainer


class _FakeContainer:
    def __init__(self) -> None:
        self.name = "orchestrator-sandbox-test"
        self.remove_calls = 0

    def remove(self, force: bool = False) -> None:  # noqa: ARG002
        self.remove_calls += 1


class _FakeClient:
    def __init__(self, *, container=None, not_found: bool = False) -> None:
        self._c = container
        self._nf = not_found

    @property
    def containers(self):  # noqa: ANN201
        outer = self

        class _C:
            def get(self, name: str):  # noqa: ANN202, ARG002
                if outer._nf:
                    raise NotFound("no such container")
                return outer._c

        return _C()


def _sb(client) -> SandboxContainer:
    return SandboxContainer(
        project_path="/fake", sandbox_id="test", client=client
    )


def test_reset_force_removes_container():
    """reset() force-removes a running container (no save) + clears the cache."""
    c = _FakeContainer()
    sb = _sb(_FakeClient(container=c))
    sb.reset()
    assert c.remove_calls == 1
    assert sb._name is None


def test_reset_noop_when_already_gone():
    """reset() on an already-absent container is a clean no-op (idempotent)."""
    sb = _sb(_FakeClient(not_found=True))
    sb.reset()  # must not raise
    assert sb._name is None


def test_do_exec_re_ensures_on_not_found(monkeypatch):
    """THE recovery link: reset_session force-removes the container, but the
    agent's exec singleton still caches the STALE name. ``_do_exec`` MUST clear
    the cache and retry via the ``container`` property (which re-ensures) on
    NotFound — otherwise every post-reset tool call dies with 'vanished mid-run'
    until the agent process is manually restarted."""
    from pux_harness.sandbox.docker_exec import DockerExecClient

    ec = DockerExecClient.__new__(DockerExecClient)  # bypass __init__ (no Docker)
    ec._container = "stale"
    ec._boot = True

    class _Fresh:
        def exec_run(self, cmd, **kw):  # noqa: ANN202, ARG002
            return ("ok", 0)

    fresh = _Fresh()
    gets: list[str] = []

    class _C:
        def get(self, name: str):  # noqa: ANN202
            gets.append(name)
            if name == "stale":
                raise NotFound("gone")
            return fresh

    class _FakeClient:
        @property
        def containers(self):  # noqa: ANN201
            return _C()

    ec._client = _FakeClient()

    # Mirror the real ``container`` property's cache+re-ensure semantics without
    # touching Docker: returns the cached name while set; "fresh" after clear.
    def _container_prop(self):  # noqa: ANN202
        if self._container is None:
            self._container = "fresh"
        return self._container

    monkeypatch.setattr(DockerExecClient, "container", property(_container_prop))

    out, code = ec._do_exec("ls")

    assert code == 0
    assert gets == ["stale", "fresh"], gets
    assert ec._container == "fresh"
