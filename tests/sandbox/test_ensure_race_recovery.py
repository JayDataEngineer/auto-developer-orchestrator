"""Tests for the ensure() name-conflict race recovery.

When two ``ensure()`` calls in the SAME project race, one wins ``create()``
and the other hits the refuse-to-kill guard (the winner's container is
RUNNING under this project's derived name). ``ensure()`` must auto-recover:
re-check ``_running_for_project()`` (the winner may now be registered) and
reuse it, or retry ``create()`` exactly once. Other failures must propagate
unchanged — the recovery is scoped to this one race, never masking real
errors and (by construction, since the name is project-derived) never
reaching across to a sibling project's container.
"""
from __future__ import annotations

import pytest

from pux_harness.sandbox import container as C
from pux_harness.sandbox.container import SandboxContainer, ContainerError


def _bare_container() -> SandboxContainer:
    """An instance without running __init__ (which would touch Docker)."""
    sb = SandboxContainer.__new__(SandboxContainer)
    sb._name = None
    sb.project_path = "/fake/proj"
    sb.sandbox_id = "ptest0000"
    sb.org = "twitter-agent"
    return sb


@pytest.fixture(autouse=True)
def _no_projects_io(monkeypatch):
    """ensure() calls projects.warn_if_switched + projects.record — stub both
    so no filesystem / registry I/O happens during the race tests. Also stub
    ``_network_healthy``: the race-recovery tests exercise create()'s
    name-conflict retry path, NOT network probing, and the ``_bare_container``
    harness intentionally skips ``__init__`` (so no DockerClient is wired).
    Network-health self-healing has its own dedicated coverage in
    ``test_container.py::test_ensure_force_removes_reused_container_*``."""
    from pux_harness.sandbox import projects
    monkeypatch.setattr(projects, "warn_if_switched", lambda *a, **k: False)
    monkeypatch.setattr(projects, "record", lambda *a, **k: None)
    monkeypatch.setattr(SandboxContainer, "_network_healthy",
                        lambda self, name: True)


# --- _is_running_name_conflict classifier ------------------------------------

def test_classifier_running_conflict():
    msg = ("sandbox name 'orchestrator-sandbox-ptest0000' is held by a RUNNING "
           "container (id abc123).")
    assert C._is_running_name_conflict(ContainerError(msg))


def test_classifier_raw_409_already_in_use():
    assert C._is_running_name_conflict(ContainerError(
        "409 already in use by a RUNNING container xyz"))


def test_classifier_start_failure_is_not_running_conflict():
    """A start failure (not a name conflict) must NOT trigger the retry."""
    assert not C._is_running_name_conflict(ContainerError("start foo: boom"))


def test_classifier_generic_create_error_is_not_running_conflict():
    assert not C._is_running_name_conflict(ContainerError("create foo: disk full"))


# --- ensure() race recovery --------------------------------------------------

def test_ensure_reuses_after_race_winner_registers(monkeypatch):
    """create() refuses (running conflict) → re-check finds the winner → reuse.
    The signature recovery: no second create() call, no escalation."""
    sb = _bare_container()
    create_calls: list[int] = []

    def _boom_then_ok(self):
        create_calls.append(1)
        raise ContainerError(
            "sandbox name 'orchestrator-sandbox-ptest0000' is held by a "
            "RUNNING container (id abc123)."
        )

    checked: list[str] = []

    def _running(self):
        # First call (top of ensure): no winner yet. Second call (post-conflict
        # re-check): the racer finished registering — reuse it.
        checked.append("called")
        return "orchestrator-sandbox-ptest0000" if len(checked) >= 2 else None

    monkeypatch.setattr(SandboxContainer, "create", _boom_then_ok)
    monkeypatch.setattr(SandboxContainer, "_running_for_project", _running)
    monkeypatch.setattr(SandboxContainer, "_validate_reused_container", lambda self, name: None)

    name = sb.ensure()

    assert name == "orchestrator-sandbox-ptest0000"
    assert sb._name == name  # cached
    assert len(create_calls) == 1, "must NOT retry create() when reuse succeeds"


def test_ensure_retries_create_once_when_no_winner(monkeypatch):
    """create() refuses (running conflict) → re-check finds nothing → retry
    create() exactly once. The retry succeeds."""
    sb = _bare_container()
    create_calls: list[int] = []

    def _first_boom_second_ok(self):
        create_calls.append(1)
        if len(create_calls) == 1:
            raise ContainerError(
                "held by a RUNNING container (id abc123).")
        return "orchestrator-sandbox-ptest0000"

    monkeypatch.setattr(SandboxContainer, "create", _first_boom_second_ok)
    monkeypatch.setattr(SandboxContainer, "_running_for_project", lambda self: None)

    name = sb.ensure()

    assert name == "orchestrator-sandbox-ptest0000"
    assert len(create_calls) == 2, "exactly one retry, not a loop"


def test_ensure_does_not_retry_non_running_conflict(monkeypatch):
    """A start failure or other ContainerError must propagate immediately —
    the recovery is scoped to the name-conflict race ONLY."""
    sb = _bare_container()
    create_calls: list[int] = []

    def _start_fail(self):
        create_calls.append(1)
        raise ContainerError("start orchestrator-sandbox-ptest0000: port in use")

    monkeypatch.setattr(SandboxContainer, "create", _start_fail)
    monkeypatch.setattr(SandboxContainer, "_running_for_project", lambda self: None)

    with pytest.raises(ContainerError, match="port in use"):
        sb.ensure()
    assert len(create_calls) == 1, "non-race errors must NOT retry"
