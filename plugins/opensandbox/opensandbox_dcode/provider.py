"""OpenSandbox sandbox provider for Deep Agents Code.

Bridges dcode's `--sandbox` seam to the upstream OpenSandbox platform
(https://github.com/opensandbox-group/OpenSandbox): the dcode process stays
local while every tool call (execute / read / write / glob / grep) runs in an
OpenSandbox-managed container (Docker or Kubernetes runtime).

Published under the `deepagents_code.sandbox_providers` entry point, so after
installing this package `dcode --sandbox opensandbox` works with no config.

Environment:
    OPENSANDBOX_DOMAIN    server address (default localhost:8080)
    OPENSANDBOX_PROTOCOL  http | https (default http)
    OPENSANDBOX_API_KEY   API key when the server enforces auth
    OPENSANDBOX_IMAGE     sandbox image (default python:3.12)
    OPENSANDBOX_TIMEOUT_HOURS  sandbox lifetime (default 8h)
"""

from __future__ import annotations

import asyncio
import os
import threading
from datetime import timedelta
from typing import Any

from deepagents.backends.protocol import (
    ExecuteResponse,
    FileDownloadResponse,
    FileUploadResponse,
)
from deepagents.backends.sandbox import BaseSandbox
from deepagents_code.integrations.sandbox_provider import (
    SandboxInstallHint,
    SandboxProvider,
    SandboxProviderMetadata,
)

from opensandbox import Sandbox
from opensandbox.config.connection import ConnectionConfig
from opensandbox.services.command import RunCommandOpts
from opensandbox.services.sandbox import SandboxImageSpec


# ── async plumbing ───────────────────────────────────────────────────────────
# The OpenSandbox SDK is async-first; dcode's provider/backend calls are sync.
# One dedicated event loop on a daemon thread serves every coroutine.

_loop: asyncio.AbstractEventLoop | None = None
_loop_lock = threading.Lock()


def _run(coro: Any, timeout_s: float | None = None) -> Any:
    global _loop
    with _loop_lock:
        if _loop is None or _loop.is_closed():
            _loop = asyncio.new_event_loop()
            threading.Thread(target=_loop.run_forever, daemon=True).start()
    fut = asyncio.run_coroutine_threadsafe(coro, _loop)
    return fut.result(timeout_s)


def _connection_config() -> ConnectionConfig:
    kwargs: dict[str, Any] = {
        "domain": os.environ.get("OPENSANDBOX_DOMAIN", "localhost:8080"),
        "protocol": os.environ.get("OPENSANDBOX_PROTOCOL", "http"),
    }
    api_key = os.environ.get("OPENSANDBOX_API_KEY")
    if api_key:
        kwargs["api_key"] = api_key
    return ConnectionConfig(**kwargs)


# ── backend ──────────────────────────────────────────────────────────────────


class OpenSandboxBackend(BaseSandbox):
    """dcode backend over a live OpenSandbox sandbox.

    `BaseSandbox` derives every file operation (ls/read/write/edit/glob/grep/
    delete) from `execute()`, so the whole tool surface rides on
    `sandbox.commands.run`.
    """

    def __init__(self, sandbox: Sandbox) -> None:
        self._sandbox = sandbox

    @property
    def id(self) -> str:  # noqa: A003 — protocol-mandated name
        return self._sandbox.id

    @property
    def sandbox(self) -> Sandbox:
        """The underlying OpenSandbox SDK handle (endpoints, metrics, egress)."""
        return self._sandbox

    def upload_files(self, files: list[tuple[str, bytes]]) -> list[FileUploadResponse]:
        out: list[FileUploadResponse] = []
        for path, content in files:
            try:
                _run(self._sandbox.files.write_file(path, content))
                out.append(FileUploadResponse(path=path, error=None))
            except Exception as exc:
                out.append(FileUploadResponse(path=path, error=str(exc)))
        return out

    def download_files(self, paths: list[str]) -> list[FileDownloadResponse]:
        out: list[FileDownloadResponse] = []
        for path in paths:
            try:
                content = _run(self._sandbox.files.read_bytes(path))
                out.append(FileDownloadResponse(path=path, content=content, error=None))
            except Exception as exc:
                out.append(FileDownloadResponse(path=path, error=str(exc)))
        return out

    def execute(self, command: str, *, timeout: int | None = None) -> ExecuteResponse:
        opts = RunCommandOpts(background=False)
        coro = self._sandbox.commands.run(command, opts=opts)
        try:
            execution = _run(coro, timeout_s=float(timeout) if timeout else None)
        except TimeoutError:
            return ExecuteResponse(
                output=f"[opensandbox] command timed out after {timeout}s",
                exit_code=124,
                truncated=False,
            )
        except Exception as exc:  # transport/API failure — surface, don't crash dcode
            return ExecuteResponse(output=f"[opensandbox] {exc}", exit_code=1)
        return ExecuteResponse(
            output=execution.text or "",
            exit_code=execution.exit_code,
            truncated=False,
        )


# ── provider ─────────────────────────────────────────────────────────────────


class OpenSandboxProvider(SandboxProvider):
    """Entry-point provider: `dcode --sandbox opensandbox`."""

    @property
    def metadata(self) -> SandboxProviderMetadata:
        return SandboxProviderMetadata(
            name="opensandbox",
            working_dir="/workspace",
            install=SandboxInstallHint(kind="package", name="opensandbox-dcode"),
            supports_sandbox_id=True,
            supports_snapshot_name=False,
        )

    def get_or_create(self, *, sandbox_id: str | None = None, **kwargs: Any) -> OpenSandboxBackend:
        cc = _connection_config()
        if sandbox_id:
            sandbox = _run(Sandbox.connect(sandbox_id, connection_config=cc), timeout_s=60.0)
        else:
            hours = float(os.environ.get("OPENSANDBOX_TIMEOUT_HOURS", "8"))
            image = os.environ.get("OPENSANDBOX_IMAGE", "python:3.12")
            sandbox = _run(
                Sandbox.create(
                    SandboxImageSpec(image=image),
                    timeout=timedelta(hours=hours),
                    connection_config=cc,
                ),
                timeout_s=180.0,
            )
        return OpenSandboxBackend(sandbox)

    def delete(self, *, sandbox_id: str, **kwargs: Any) -> None:
        cc = _connection_config()
        sandbox = _run(Sandbox.connect(sandbox_id, connection_config=cc), timeout_s=60.0)
        _run(sandbox.kill(), timeout_s=60.0)
