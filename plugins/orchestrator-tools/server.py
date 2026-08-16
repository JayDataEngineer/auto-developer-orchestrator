#!/usr/bin/env python3
"""orchestrator-tools MCP server — the pux action tools, restored host-side.

Restored from the harness (git 3f37944c:src/tools/{python,describe_image}.py)
with the sandbox indirection removed: execution is a host subprocess. For
isolated execution use dcode's upstream sandbox providers (dcode --sandbox);
this server is the tool surface, not an isolation layer.

Tools:
  python          Execute Python code (python3 -c); print output captured.
  describe_image  Describe an image via an OpenAI-compatible vision endpoint
                  (env-configured; returns a clear error when unconfigured).

Env:
  VISION_API_URL   Chat-completions endpoint, e.g.
                   https://api.z.ai/api/coding/paas/v4/chat/completions
  VISION_MODEL     Vision-capable model id
  VISION_API_KEY   Bearer key for the endpoint

Stdio transport (FastMCP from the official `mcp` SDK).
"""
from __future__ import annotations

import base64
import json
import mimetypes
import os
import shlex
import subprocess
import sys
import urllib.request

from mcp.server.fastmcp import FastMCP

mcp = FastMCP("orchestrator-tools")

_PY_TIMEOUT = int(os.environ.get("ORCH_PYTHON_TIMEOUT", "120"))
_VISION_TIMEOUT = int(os.environ.get("ORCH_VISION_TIMEOUT", "60"))


def _result(payload: dict) -> str:
    return json.dumps(payload)


@mcp.tool()
def python(code: str) -> str:
    """Execute Python code. Print output is captured. Runs host-side via
    python3 -c with whatever this interpreter's environment ships. Use for
    computation and glue; the caller owns safety (prefer dcode --sandbox for
    untrusted code)."""
    if not code:
        return _result({"success": False, "error": "no code provided"})
    try:
        proc = subprocess.run(
            [sys.executable, "-c", code],
            capture_output=True, text=True, timeout=_PY_TIMEOUT,
        )
    except subprocess.TimeoutExpired:
        return _result({"success": False, "error": f"timed out after {_PY_TIMEOUT}s"})
    out = proc.stdout + proc.stderr
    if proc.returncode != 0:
        return _result({"success": False, "error": f"python exited {proc.returncode}", "output": out})
    return _result({"success": True, "output": out})


def _vision_call(image_b64: str, mime: str, prompt: str) -> str:
    url = os.environ.get("VISION_API_URL", "")
    model = os.environ.get("VISION_MODEL", "")
    key = os.environ.get("VISION_API_KEY", "")
    if not (url and model and key):
        return _result({
            "success": False,
            "error": "vision endpoint not configured — set VISION_API_URL, "
                     "VISION_MODEL, VISION_API_KEY (see .env.example)",
        })
    data_url = f"data:{mime};base64,{image_b64}"
    body = json.dumps({
        "model": model,
        "messages": [{
            "role": "user",
            "content": [
                {"type": "text", "text": prompt},
                {"type": "image_url", "image_url": {"url": data_url}},
            ],
        }],
    }).encode()
    req = urllib.request.Request(url, data=body, headers={
        "Content-Type": "application/json",
        "Authorization": f"Bearer {key}",
    })
    try:
        with urllib.request.urlopen(req, timeout=_VISION_TIMEOUT) as resp:
            payload = json.loads(resp.read().decode())
    except Exception as exc:  # noqa: BLE001 — surface any transport/API error
        return _result({"success": False, "error": f"vision call failed: {exc}"})
    try:
        text = payload["choices"][0]["message"]["content"]
    except (KeyError, IndexError, TypeError):
        return _result({"success": False, "error": "unexpected API response", "raw": payload})
    return _result({"success": True, "description": text, "source": "primary"})


@mcp.tool()
def describe_image(image_path: str | None = None, prompt: str | None = None) -> str:
    """Describe an image file. Pass a host path and an optional instruction
    (default: generic description). The vision endpoint is env-configured
    (VISION_API_URL / VISION_MODEL / VISION_API_KEY)."""
    if not image_path:
        return _result({"success": False, "error": "image_path is required"})
    p = os.path.expanduser(image_path)
    if not os.path.isfile(p):
        return _result({"success": False, "error": f"no such file: {p}"})
    mime = mimetypes.guess_type(p)[0] or "image/png"
    with open(p, "rb") as f:
        b64 = base64.b64encode(f.read()).decode()
    return _vision_call(b64, mime, prompt or "Describe this image concisely and factually.")


if __name__ == "__main__":
    mcp.run()
