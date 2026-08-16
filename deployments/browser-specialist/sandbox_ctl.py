#!/usr/bin/env python3
"""Ops CLI for the browser workload sandbox (see browser_specialist.sandbox).

Usage (from the deployment dir):
    uv run python sandbox_ctl.py status   # id, state, MCP endpoint, health
    uv run python sandbox_ctl.py kill     # teardown (Chrome state dies with it)
"""
from __future__ import annotations

import asyncio
import json
import sys


async def _status() -> int:
    from browser_specialist import sandbox as S

    sid = S._read_id()
    if not sid:
        print(json.dumps({"running": False,
                          "reason": "not created yet (spawns on first browser task)"}))
        return 0
    from opensandbox import Sandbox

    try:
        sbx = await Sandbox.connect(sid, connection_config=S._connection_config())
        info = await sbx.get_info()
        base = await S._endpoint_base(sbx)
        healthy = False
        try:
            with __import__("urllib.request", fromlist=["x"]).urlopen(
                f"{base}/health", timeout=3
            ) as r:
                healthy = r.status == 200
        except Exception:
            pass
        print(json.dumps({
            "running": True,
            "sandbox_id": sid,
            "image": S.SANDBOX_IMAGE,
            "state": str(getattr(info, "status", "")),
            "mcp_url": f"{base}/mcp",
            "mcp_healthy": healthy,
        }))
        return 0 if healthy else 1
    except Exception as exc:  # noqa: BLE001
        print(json.dumps({"running": False, "reason": str(exc)}))
        return 1


async def _kill() -> int:
    from browser_specialist import sandbox as S

    killed = await S.kill_browser_sandbox()
    print("browser sandbox killed" if killed else "no browser sandbox to kill")
    return 0


async def main() -> int:
    cmd = sys.argv[1] if len(sys.argv) > 1 else "status"
    if cmd == "status":
        return await _status()
    if cmd == "kill":
        return await _kill()
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
