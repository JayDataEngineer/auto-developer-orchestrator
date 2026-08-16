"""The browser workload sandbox — lifecycle + MCP endpoint discovery.

The browser tier's isolation is a REAL boundary, not guard-rails inside the
tool server: mc_browser and its Chromium run inside an OpenSandbox container
(``browser-specialist-sandbox:latest``, see ``sandbox/Dockerfile``). The
container carries no credentials — the model token lives in the trusted
Aegra tier — and has no reach onto the host filesystem or host processes.

This module owns the sandbox lifecycle:
- ``ensure_browser_sandbox()`` — connect to the persisted sandbox or create
  it (long-lived; id persisted next to the deployment), renew its lease,
  and wait for the MCP HTTP server inside to answer /health;
- ``mcp_url()`` — the host-side URL langchain-mcp-adapters connects to;
- ``kill_browser_sandbox()`` — teardown (Chrome state dies with it).

Env: ``BROWSER_SANDBOX_IMAGE`` (image tag), ``BROWSER_SANDBOX_PORT``
(in-container MCP port), ``BROWSER_SANDBOX_ID_FILE`` (persisted id),
``OPENSANDBOX_DOMAIN`` / ``OPENSANDBOX_API_KEY`` (the sandbox server).
"""

from __future__ import annotations

import asyncio
import os
import time
import urllib.request
from datetime import timedelta
from pathlib import Path

SANDBOX_IMAGE = os.environ.get(
    "BROWSER_SANDBOX_IMAGE", "browser-specialist-sandbox:latest"
)
SANDBOX_MCP_PORT = int(os.environ.get("BROWSER_SANDBOX_PORT", "8765"))
# The local server caps sandbox leases at 86400s (24h) — stay under it and
# renew periodically (see _start_renewal_loop) so a long-lived Aegra process
# never outlives the sandbox.
SANDBOX_LIFETIME = timedelta(hours=23)
_RENEW_INTERVAL = timedelta(hours=12)
ID_FILE = Path(
    os.environ.get(
        "BROWSER_SANDBOX_ID_FILE",
        str(Path(__file__).resolve().parents[2] / ".sandbox_id"),
    )
)


def _connection_config():
    from opensandbox.config.connection import ConnectionConfig

    return ConnectionConfig(
        domain=os.environ.get("OPENSANDBOX_DOMAIN", "localhost:8080"),
        protocol=os.environ.get("OPENSANDBOX_PROTOCOL", "http"),
        api_key=os.environ.get("OPENSANDBOX_API_KEY") or None,
    )


def _read_id() -> str | None:
    try:
        return ID_FILE.read_text().strip() or None
    except OSError:
        return None


async def _endpoint_base(sbx) -> str:
    """Scheme-normalized base URL of the sandbox's MCP port.

    The local server returns proxy-form endpoints without a scheme
    (``127.0.0.1:<host>/proxy/<port>``); urlopen and HTTP clients need one.
    """
    ep = await sbx.get_endpoint(SANDBOX_MCP_PORT)
    base = ep.endpoint.rstrip("/")
    if "://" not in base:
        base = f"http://{base}"
    return base


async def _wait_healthy(sbx, timeout_s: float = 120.0) -> None:
    """Wait until the in-sandbox MCP server answers /health."""
    base = await _endpoint_base(sbx)
    deadline = time.monotonic() + timeout_s
    last: Exception | None = None
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(f"{base}/health", timeout=3) as resp:
                if resp.status == 200:
                    return
        except Exception as exc:  # noqa: BLE001 — retry until deadline
            last = exc
        await asyncio.sleep(2)
    raise RuntimeError(
        f"browser sandbox MCP not healthy after {timeout_s}s ({base}): {last}"
    )


async def ensure_browser_sandbox():
    """The one long-lived browser sandbox: connect or create, then renew."""
    from opensandbox import Sandbox

    conn = _connection_config()
    sid = _read_id()
    if sid:
        try:
            sbx = await Sandbox.connect(sid, connection_config=conn)
            await sbx.get_info()  # raises when the id is stale
            await _wait_healthy(sbx)
            try:
                await sbx.renew(SANDBOX_LIFETIME)
            except Exception:  # best-effort; the renewal loop also retries
                pass
            start_renewal_loop(sbx)
            return sbx
        except Exception:
            pass  # stale id (sandbox expired/killed) — create a fresh one
    sbx = await Sandbox.create(
        SANDBOX_IMAGE,
        timeout=SANDBOX_LIFETIME,
        # The platform replaces the image ENTRYPOINT with its execd bootstrap,
        # so the workload is named explicitly (same pattern as upstream's
        # code-interpreter example).
        entrypoint=["python", "/opt/mc_browser.py"],
        env={"MC_BROWSER_HEADLESS": os.environ.get("MC_BROWSER_HEADLESS", "1")},
        metadata={
            "role": "browser-specialist",
            "workspace": "auto-developer-orchestrator",
        },
        connection_config=conn,
        ready_timeout=timedelta(seconds=120),
    )
    ID_FILE.parent.mkdir(parents=True, exist_ok=True)
    ID_FILE.write_text(sbx.id)
    await _wait_healthy(sbx)
    start_renewal_loop(sbx)
    return sbx


_RENEW_TASK: asyncio.Task | None = None


def start_renewal_loop(sbx) -> None:
    """Keep the sandbox lease alive for the life of this process."""
    global _RENEW_TASK

    async def _loop():
        while True:
            await asyncio.sleep(_RENEW_INTERVAL.total_seconds())
            try:
                await sbx.renew(SANDBOX_LIFETIME)
            except Exception:  # best-effort; ensure_browser_sandbox also renews
                pass

    if _RENEW_TASK is None or _RENEW_TASK.done():
        _RENEW_TASK = asyncio.create_task(_loop())


async def mcp_connection(sbx) -> dict:
    """The langchain-mcp-adapters connection dict for the in-sandbox server."""
    ep = await sbx.get_endpoint(SANDBOX_MCP_PORT)
    base = ep.endpoint.rstrip("/")
    if "://" not in base:
        base = f"http://{base}"
    cfg: dict = {
        "transport": "streamable_http",
        "url": f"{base}/mcp",
    }
    if ep.headers:
        cfg["headers"] = dict(ep.headers)
    return cfg


async def kill_browser_sandbox() -> bool:
    sid = _read_id()
    if not sid:
        return False
    from opensandbox import Sandbox

    try:
        sbx = await Sandbox.connect(sid, connection_config=_connection_config())
        await sbx.kill()
    finally:
        ID_FILE.unlink(missing_ok=True)
    return True
