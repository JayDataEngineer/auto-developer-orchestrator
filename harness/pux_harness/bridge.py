"""Bridge to the existing Go MCP sandbox server (localhost:9987).

The Go server speaks plain JSON-RPC 2.0 over HTTP POST — strictly
request/response, no SSE, GET returns 405 (backend/internal/mcpserver/
transport.go:14-20). That is NOT the standard MCP "streamable HTTP"
transport, so langchain-mcp-adapters' streamable_http client can reject it.
This module talks the proven wire protocol directly — initialize → capture
Mcp-Session-Id → tools/list → tools/call — and exposes each tool as a
LangChain StructuredTool, name-prefixed `pux_sandbox_` so the org AGENTS.md
overlays port verbatim (they already say "use pux_sandbox_bash", etc.).

The curl probe (2026-07-03) confirmed this exact request/response shape and
that the server returns Mcp-Session-Id on initialize.
"""
from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from typing import Any

from langchain_core.tools import StructuredTool
from pydantic import create_model

PUX_MCP_URL = os.environ.get("PUX_MCP_URL", "http://127.0.0.1:9987/")
PUX_PREFIX = "pux_sandbox_"
_TIMEOUT = 300

# JSON-Schema type → Python type. Unknown types fall back to object (Any-ish).
_JSON_TYPE = {
    "string": str,
    "integer": int,
    "number": float,
    "boolean": bool,
    "array": list,
    "object": dict,
}


class PuxMCPClient:
    """Minimal MCP-over-HTTP client for the pux Go server's request/response wire."""

    def __init__(self, url: str = PUX_MCP_URL, timeout: int = _TIMEOUT):
        self.url = url
        self.timeout = timeout
        self.session_id: str | None = None
        self._id = 0

    def _next_id(self) -> int:
        self._id += 1
        return self._id

    def _post(self, payload: dict, parse: bool = True) -> Any:
        data = json.dumps(payload).encode()
        req = urllib.request.Request(
            self.url,
            data=data,
            method="POST",
            headers={
                "Content-Type": "application/json",
                "Accept": "application/json, text/event-stream",
            },
        )
        if self.session_id:
            req.add_header("Mcp-Session-Id", self.session_id)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                if not self.session_id:
                    self.session_id = resp.headers.get("Mcp-Session-Id")
                body = resp.read()
        except urllib.error.HTTPError as exc:  # surface server errors verbosely
            detail = exc.read().decode(errors="replace")[:800]
            raise RuntimeError(f"MCP HTTP {exc.code} from {self.url}: {detail}") from exc
        if not parse or not body:
            return None
        return json.loads(body.decode())

    def initialize(self) -> dict:
        resp = self._post(
            {
                "jsonrpc": "2.0",
                "id": self._next_id(),
                "method": "initialize",
                "params": {
                    "protocolVersion": "2025-03-26",
                    "capabilities": {},
                    "clientInfo": {"name": "pux-harness", "version": "0.1"},
                },
            }
        )
        # notifications/initialized is fire-and-forget (202, no JSON body).
        self._post({"jsonrpc": "2.0", "method": "notifications/initialized"}, parse=False)
        if isinstance(resp, dict) and resp.get("error"):
            raise RuntimeError(f"initialize error: {resp['error']}")
        return resp or {}

    def list_tools(self) -> list[dict]:
        resp = self._post(
            {"jsonrpc": "2.0", "id": self._next_id(), "method": "tools/list", "params": {}}
        )
        if isinstance(resp, dict) and resp.get("error"):
            raise RuntimeError(f"tools/list error: {resp['error']}")
        return (resp or {}).get("result", {}).get("tools", [])

    def call_tool(self, name: str, arguments: dict) -> dict:
        resp = self._post(
            {
                "jsonrpc": "2.0",
                "id": self._next_id(),
                "method": "tools/call",
                "params": {"name": name, "arguments": arguments},
            }
        )
        if isinstance(resp, dict) and resp.get("error"):
            raise RuntimeError(f"tools/call {name} error: {resp['error']}")
        return (resp or {}).get("result", {})


def _schema_to_model(tool_name: str, schema: dict | None):
    """Build a Pydantic args model from a tool's JSON inputSchema (flat only)."""
    schema = schema or {}
    props = schema.get("properties", {}) or {}
    required = set(schema.get("required", []) or [])
    fields: dict[str, Any] = {}
    for fname, fdef in props.items():
        py = _JSON_TYPE.get((fdef or {}).get("type"), object)
        fields[fname] = (py, ... if fname in required else None)
    return create_model(f"{tool_name}_args", **fields)


def _extract_text(result: dict) -> str:
    """Flatten an MCP tool-call result into a string for the agent."""
    if not isinstance(result, dict):
        return str(result)
    parts: list[str] = []
    for item in result.get("content", []) or []:
        if isinstance(item, dict):
            if item.get("type") == "text":
                parts.append(item.get("text", ""))
            else:
                parts.append(json.dumps(item))
        else:
            parts.append(str(item))
    text = "\n".join(p for p in parts if p)
    if result.get("isError"):
        return f"ERROR: {text}"
    return text


def _make_tool(client: PuxMCPClient, spec: dict) -> StructuredTool:
    name = spec["name"]
    description = spec.get("description", "") or name
    args_model = _schema_to_model(name, spec.get("inputSchema"))

    def _run(**kwargs: Any) -> str:
        return _extract_text(client.call_tool(name, kwargs))

    async def _arun(**kwargs: Any) -> str:
        return _extract_text(client.call_tool(name, kwargs))

    return StructuredTool(
        name=PUX_PREFIX + name,
        description=description,
        args_schema=args_model,
        func=_run,
        coroutine=_arun,
    )


def get_pux_tools(url: str = PUX_MCP_URL, only: set[str] | None = None) -> list[StructuredTool]:
    """Connect + initialize, return LangChain tools backed by the Go sandbox.

    `only` filters by the UN-prefixed MCP tool name, e.g. {"bash", "file_read"}.
    """
    client = PuxMCPClient(url)
    client.initialize()
    specs = client.list_tools()
    return [
        _make_tool(client, s)
        for s in specs
        if not only or s["name"] in only
    ]


if __name__ == "__main__":
    print(f"connecting to {PUX_MCP_URL} ...")
    tools = get_pux_tools()
    print(f"{len(tools)} tools: {[t.name for t in tools]}")
    bash = next((t for t in tools if t.name == PUX_PREFIX + "bash"), None)
    if bash:
        print("--- smoke: bash `ls /sandbox/workspace | head` ---")
        print(bash.invoke({"command": "ls /sandbox/workspace 2>/dev/null | head -20"}))
