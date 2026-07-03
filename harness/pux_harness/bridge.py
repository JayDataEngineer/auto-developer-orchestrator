"""Bridge to the existing Go MCP sandbox server (localhost:9987).

The Go server speaks plain JSON-RPC 2.0 over HTTP POST — strictly
request/response, no SSE, GET returns 405 (backend/internal/mcpserver/
transport.go:14-20). That is NOT the standard MCP "streamable HTTP"
transport, so langchain-mcp-adapters' streamable_http client can reject it.
This module talks the proven wire protocol directly — initialize → capture
Mcp-Session-Id → tools/list → tools/call — and exposes each tool as a
LangChain StructuredTool, name-prefixed `pux_sandbox_`. Phase 3 narrowed the
model-visible surface to the SPECIALIST tools (browser/desktop/vision/skills/
python); the fs/shell surface is deepagents' native tools (ls/read_file/
write_file/edit_file/glob/grep/execute) backed by ``PuxSandboxBackend``, which
reaches this server's ``bash`` internally — the Go ``bash``/``file_*`` tools are
no longer bound to the model.

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


# Phase 3: the model-visible fs/shell surface moved to deepagents native tools
# (ls/read_file/write_file/edit_file/glob/grep/execute) via PuxSandboxBackend.
# This bridge exposes ONLY specialists — the Go server's bash + file_* tools
# still exist for the backend's internal use but are not bound to the model.
_SPECIALIST_TOOLS = frozenset({
    "browser_navigate", "browser_click", "browser_type", "browser_screenshot", "browser_evaluate",
    "desktop_screenshot", "desktop_click", "desktop_type", "desktop_key",
    "describe_image", "list_skills", "load_skill", "python",
})


def get_pux_client(url: str = PUX_MCP_URL) -> PuxMCPClient:
    """Connect + initialize once. Shared by the backend and the tool surface so
    a subagent tree rides one MCP session."""
    client = PuxMCPClient(url)
    client.initialize()
    return client


def get_pux_tools(
    url: str = PUX_MCP_URL,
    only: set[str] | frozenset[str] | None = _SPECIALIST_TOOLS,
    client: PuxMCPClient | None = None,
) -> list[StructuredTool]:
    """Return LangChain StructuredTools backed by the Go sandbox.

    ``only`` filters by the UN-prefixed MCP tool name; it defaults to the
    specialist set (fs/shell is native via ``PuxSandboxBackend``). Pass
    ``only=None`` for all tools, or a custom set. ``client`` lets the backend +
    tool surface share one initialized session.
    """
    client = client or get_pux_client(url)
    specs = client.list_tools()
    return [
        _make_tool(client, s)
        for s in specs
        if only is None or s["name"] in only
    ]


if __name__ == "__main__":
    print(f"connecting to {PUX_MCP_URL} ...")
    tools = get_pux_tools()
    print(f"{len(tools)} specialist tools: "
          f"{sorted(t.name[len(PUX_PREFIX):] for t in tools)}")
