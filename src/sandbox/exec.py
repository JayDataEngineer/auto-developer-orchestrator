"""Thin exec seam — one ``BaseSandbox`` per process.

``shared_backend()`` constructs: ``PUX_SANDBOX=local`` (default) → deepagents'
``LocalShellBackend`` (host filesystem — the dcode-native default, no
container); ``PUX_SANDBOX=openshell`` → ``OpenShellSandbox`` over the NVIDIA
gateway (explicit opt-in; the langchain adapter pins ``deepagents<0.6`` so it
can never ship with the real SDK — it only exists for legacy gateway setups).
Specialist tools take ``BaseSandbox`` directly — the portable langchain
contract.
"""
from __future__ import annotations

import os
import sys
from typing import Any

from deepagents.backends.sandbox import BaseSandbox

# Workspace root inside the sandbox — the project mount + the image WORKDIR.
# Kept as a constant because a handful of specialist tools (declared/dynamic)
# + grader descriptions reference it for in-sandbox path wayfinding.
WORKSPACE_ROOT = "/sandbox/workspace"


# --- project wayfinding (host-side, no docker) -----------------------------

def resolve_project_path() -> str:
    """Absolute project path. ``PUX_PROJECT_PATH`` wins, else the repo root.

    The sandbox workspace is bind-mounted from this path. URL schemes are
    rejected: their colons corrupt path parsing."""
    from profiles._paths import project_root
    p = os.environ.get("PUX_PROJECT_PATH")
    if not p:
        p = str(project_root())
        sys.stderr.write(
            "pux sandbox: WARNING — PUX_PROJECT_PATH unset; binding "
            f"{WORKSPACE_ROOT} to the harness repo fallback ({p}).\n"
        )
    if "://" in p:
        raise ValueError(f"sandboxes require a local path; received a URL: {p!r}")
    return os.path.abspath(p)


# --- process singletons ---------------------------------------------------

_backend: BaseSandbox | None = None
# Hold the entered openshell.Sandbox so it isn't GC'd (its __exit__ tears down
# the sandbox). Lives at module scope for the process lifetime.
_openshell_sb: Any = None


def shared_backend() -> BaseSandbox:
    """One ``BaseSandbox`` for the process (lazy)."""
    global _backend
    if _backend is None:
        _backend = _make_backend()
    return _backend


def _make_backend() -> BaseSandbox:
    mode = os.environ.get("PUX_SANDBOX", "local")
    if mode == "openshell":
        return _make_openshell_backend()
    if mode == "local":
        return _make_local_backend()
    raise RuntimeError(
        f"PUX_SANDBOX={mode!r} unsupported (use 'openshell' or 'local')."
    )


def _make_openshell_backend() -> BaseSandbox:
    global _openshell_sb
    try:
        import openshell
        from langchain_nvidia_openshell import OpenShellSandbox
    except ImportError as exc:
        raise RuntimeError(
            "PUX_SANDBOX=openshell but the OpenShell SDK is not installed. "
            "pip install openshell langchain-nvidia-openshell, then start the "
            "gateway (see ~/.local/share/pux/openshell/README.md)."
        ) from exc
    ws = os.environ.get("PUX_SANDBOX_WORKSPACE", "default")
    _openshell_sb = openshell.Sandbox(
        workspace=ws, name=f"pux-{os.getpid()}", delete_on_exit=False,
    )
    _openshell_sb.__enter__()  # hold open for the process lifetime
    return OpenShellSandbox(sandbox=_openshell_sb)


def _make_local_backend() -> BaseSandbox:
    """Host-shell backend — no container. Runs commands directly on the host.
    Used by tests / ``kit compile`` / anywhere the OpenShell gateway isn't
    available. deepagents' ``LocalShellBackend`` implements the full
    ``BaseSandbox`` surface (``execute``/``ls``/``read``/``upload_files``/…)."""
    from deepagents.backends.local_shell import LocalShellBackend
    return LocalShellBackend()  # type: ignore[return-value]  # implements the sandbox protocol; deepagents' BaseSandbox typing is loose (its CLI passes LocalShellBackend)
