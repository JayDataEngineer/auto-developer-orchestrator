"""Generic helpers for sandbox-executing LangChain tools — ZERO pux imports.

Portable: any project with a ``BaseSandbox`` can import from here. No
``src`` coupling beyond ``_pux.py`` + ``registry.py`` — the harness-adjacent parts stay there.
"""

from __future__ import annotations

import json
import logging
import subprocess
from typing import TYPE_CHECKING

from pydantic import BaseModel

if TYPE_CHECKING:
    from deepagents.backends.sandbox import BaseSandbox

log = logging.getLogger(__name__)


def _exec(
    sandbox: "BaseSandbox", command: str, timeout: int | None = None,
) -> tuple[str, int]:
    """Run ``command`` in ``sandbox``, return ``(output, exit_code)``.

    Bridges deepagents ``BaseSandbox.execute()`` (returns ``ExecuteResponse``)
    to the ``(output, exit_code)`` tuple the specialist tools unpack. Timeouts
    (``TimeoutError`` / ``subprocess.TimeoutExpired``) are caught and surfaced
    as ``exit_code=124``.
    """
    try:
        r = (sandbox.execute(command) if timeout is None
             else sandbox.execute(command, timeout=timeout))
        return r.output, r.exit_code  # type: ignore[return-value]  # execute()'s Optional is protocol looseness; a completed command always carries an int
    except (TimeoutError, subprocess.TimeoutExpired):
        return f"timeout after {timeout}s", 124


def _result(obj: dict) -> str:
    """Serialize a tool-result dict to the exact JSON the Go bridge surfaced
    (2-space indent + sorted map keys at every level)."""
    return json.dumps(obj, indent=2, sort_keys=True)


def _tail(text: str, n: int = 800) -> str:
    """Last ``n`` chars of ``text`` — keeps stderr tails out of result
    envelopes without leaking megabytes. Mirrors the Go ``tailOutput`` helper."""
    return text if len(text) <= n else "..." + text[len(text) - n:]


class _NoArgs(BaseModel):
    """Schema for argument-less tools (list_skills, browser_tabs, etc.)."""
