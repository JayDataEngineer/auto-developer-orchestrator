"""The graph backend — dcode's own ``LocalShellBackend`` (filesystem + shell).

``create_deep_agent`` defaults to a bare ``StateBackend`` (no filesystem, no
``execute``); the org's agents are built on the native fs/shell tools, so the
wrapper passes the same local backend dcode's CLI uses for ``--backend local``
runs. ``cwd`` roots the backend's filesystem view; ``virtual_mode=False``
means the agent edits the REAL files there (visible in the TUI, committed on
disk) — not a throwaway overlay.
"""
from __future__ import annotations

from pathlib import Path

from deepagents.backends import LocalShellBackend


def local_backend(*, cwd: str | Path | None = None) -> LocalShellBackend:
    """A ``LocalShellBackend`` rooted at ``cwd`` (process cwd when omitted)."""
    return LocalShellBackend(
        root_dir=str(Path(cwd).resolve()) if cwd is not None else None,
        virtual_mode=False,
    )
