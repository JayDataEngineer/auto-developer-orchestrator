"""409 "removal already in progress" race tolerance for ``SandboxContainer``.

The bug (locked here): two ``pux acp`` sessions share the deterministic
container name ``orchestrator-sandbox-<id>``; when one tears down while another
recreates, BOTH call ``remove(force=True)`` and the second surfaces Docker's
``409 Conflict ("removal of container X is already in progress")`` as a hard
error in the user's face — the "manual yegsting" failure. ``_safe_remove``
treats that 409 as "the other caller is doing my job", polls ``get()`` until
``NotFound``, and returns cleanly. Other ``APIError``s still surface.

These tests pin the behavior with fakes — no Docker daemon required.
"""
from __future__ import annotations

import pytest
from docker.errors import APIError, NotFound

from pux_harness.sandbox.container import SandboxContainer


class _FakeContainer:
    """docker container stand-in whose ``remove()`` is scriptable."""

    def __init__(self, remove_exc: Exception | None = None) -> None:
        self.name = "orchestrator-sandbox-test"
        self._remove_exc = remove_exc
        self.remove_calls = 0

    def remove(self, force: bool = False) -> None:  # noqa: ARG002
        self.remove_calls += 1
        if self._remove_exc is not None:
            exc, self._remove_exc = self._remove_exc, None  # raise once
            raise exc


class _FakeClient:
    """docker client whose ``containers.get()`` returns the container for the
    first ``present_polls`` calls, then raises ``NotFound``."""

    def __init__(self, *, present_polls: int = 0) -> None:
        self._present = present_polls
        self.get_calls = 0

    @property
    def containers(self):  # noqa: ANN201
        outer = self

        class _C:
            def get(self, name: str):  # noqa: ANN202, ARG002
                outer.get_calls += 1
                if outer.get_calls <= outer._present:
                    return _FakeContainer()  # still mid-removal
                raise NotFound(f"no such container: {name}")

        return _C()


def _sb(client: _FakeClient) -> SandboxContainer:
    return SandboxContainer(
        project_path="/fake/project",
        sandbox_id="test",
        client=client,  # type: ignore[arg-type]
    )


@pytest.fixture(autouse=True)
def _no_sleep(monkeypatch):
    """The poll loop's ``time.sleep`` is a no-op so tests are instant; the loop
    still terminates because get() raises NotFound within a couple of polls."""
    monkeypatch.setattr("pux_harness.sandbox.container.time.sleep", lambda _s: None)


def test_safe_remove_swallows_409_already_in_progress():
    """THE race fix: remove() raises 409 'already in progress' because a
    concurrent teardown is mid-flight. ``_safe_remove`` polls until NotFound and
    returns cleanly — the user never sees the 409."""
    c = _FakeContainer(
        remove_exc=APIError(
            '409 Client Error for http+docker://localhost: Conflict '
            '("removal of container abc123 is already in progress")'
        )
    )
    client = _FakeClient(present_polls=0)  # concurrent removal already finished
    _sb(client)._safe_remove(c, name="orchestrator-sandbox-test")  # must NOT raise
    assert c.remove_calls == 1


def test_safe_remove_polls_then_succeeds(monkeypatch):
    """The concurrent removal is still in flight for a couple of polls, then
    lands (NotFound). The loop tolerates the interim 'still present' reads
    without hitting the 30s deadline."""
    clock = [0.0]

    def _monotonic() -> float:
        clock[0] += 0.4  # 0.4s per call; deadline is 30s → plenty of headroom
        return clock[0]

    monkeypatch.setattr("pux_harness.sandbox.container.time.monotonic", _monotonic)

    c = _FakeContainer(
        remove_exc=APIError(
            '409 Client Error: Conflict ("removal already in progress")'
        )
    )
    client = _FakeClient(present_polls=2)  # present twice, then NotFound
    _sb(client)._safe_remove(c, name="orchestrator-sandbox-test")  # must NOT raise
    assert client.get_calls == 3  # 2 present reads + 1 NotFound


def test_safe_remove_reraises_non_race_apierror():
    """A non-'already in progress' APIError (e.g. 500 daemon down) is a REAL
    failure — it must surface, not be swallowed by the race-tolerance path."""
    c = _FakeContainer(
        remove_exc=APIError("500 Internal Server Error: daemon is down")
    )
    with pytest.raises(APIError):
        _sb(_FakeClient())._safe_remove(c, name="orchestrator-sandbox-test")


def test_safe_remove_returns_cleanly_when_already_gone():
    """remove() raising NotFound (container vanished) is the desired end state —
    return, don't error."""
    c = _FakeContainer(remove_exc=NotFound("no such container"))
    _sb(_FakeClient())._safe_remove(c, name="orchestrator-sandbox-test")
    assert c.remove_calls == 1
