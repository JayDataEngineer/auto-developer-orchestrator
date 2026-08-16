"""The MCP projection — an org's declared ``capabilities:`` → ``.mcp.json``.

The shared catalog at ``profiles/_shared/tool_servers.yaml`` is the registry of
every MCP server the workspace knows; this module projects it (per org, or as
the union the ``sync`` surface needs) into the standard ``mcpServers`` schema
dcode reads natively.

Pure data + filesystem — no pux runtime, no Docker, no tokens.
"""
from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml

from compiler.capabilities import org_mcp_items_from_dict
from profiles.loaders import _org_path


def _mcp_entry(d: dict[str, Any]) -> dict[str, Any]:
    """One catalog entry → one ``mcpServers[name]`` value (the dcode schema:
    stdio = ``command``/``args``/``env``; sse/http = ``type``/``url``/``headers``;
    the catalog allowlist maps to ``allowedTools``)."""
    entry: dict[str, Any] = {}
    if d.get("transport", "stdio") == "stdio":
        entry["command"] = d.get("command", "")
        if d.get("args"):
            entry["args"] = list(d["args"])
        if d.get("env"):
            entry["env"] = dict(d["env"])
    else:
        entry["type"] = d.get("transport", "")
        entry["url"] = d.get("url", "")
        if d.get("headers"):
            entry["headers"] = dict(d["headers"])
    if isinstance(d.get("tools"), list):
        entry["allowedTools"] = list(d["tools"])
    return entry


def _org_mcp_servers(org: str, root: Path) -> dict[str, Any]:
    """The org's ``capabilities:`` mcp entries → a ``mcpServers`` mapping,
    catalog-resolved. ``${VAR}`` placeholders stay raw (dcode interpolates)."""
    manifest = _org_path(org, root) / "org.yaml"
    if not manifest.is_file():
        return {}
    data = yaml.safe_load(manifest.read_text()) or {}
    catalog_path = root / "profiles" / "_shared" / "tool_servers.yaml"
    catalog: dict[str, Any] = (
        yaml.safe_load(catalog_path.read_text()) or {} if catalog_path.is_file() else {}
    )
    servers: dict[str, Any] = {}
    for item in org_mcp_items_from_dict(data, org):
        if isinstance(item, dict) and "name" in item:  # inline spec
            d = dict(item)
            servers[str(d.pop("name"))] = _mcp_entry(d)
            continue
        ref = item if isinstance(item, str) else str(item.get("ref", ""))
        if ref not in catalog:
            msg = f"{org}: capabilities: unknown catalog ref {ref!r}"
            raise ValueError(msg)
        d = dict(catalog[ref])
        if isinstance(item, dict) and item.get("tools") is not None:
            d["tools"] = item["tools"]
        servers[ref] = _mcp_entry(d)
    return servers
