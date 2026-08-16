#!/usr/bin/env python3
"""Regenerate the exclusion YAML from the live workspace surface.

The deny-list fails open, so it must be re-captured whenever a server adds
tools, an allowedTools trim changes, or a new server joins any .mcp.json.
Sources, in order of authority:

  1. live dcode resolution (godot, surreal, opensandbox, web_research, ...)
  2. the docker stdio handshake for github (binary not installed locally)
  3. in-repo source for nitter (infra/nitter/src/server.py)
  4. the allowedTools trims declared in .mcp.json for down servers (ray)

Run from the repo root with the dcode tool venv, then tripwire it:

    $(uv tool dir)/deepagents-code/bin/python plugins/tool-scoping/regenerate.py
    git diff plugins/tool-scoping/tool_scoping_dcode/profiles/
    uv pip install --python "$(uv tool dir)/deepagents-code/bin/python" ./plugins/tool-scoping
    make scoping-check
"""
from __future__ import annotations

import asyncio
import json
import os
import re
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
PLUGIN = Path(__file__).resolve().parent
PROFILE_KEY = "openai:glm-5-turbo"


def _load_env() -> None:
    for line in (REPO / ".env").read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, _, v = line.partition("=")
            os.environ.setdefault(k.strip(), v.strip().strip('"').strip("'"))


async def _live_servers() -> dict[str, list[str]]:
    """Resolve every distinct server across all profiles (dcode's own pipeline)."""
    from deepagents_code.mcp_tools import resolve_and_load_mcp_tools
    from deepagents_code.project_utils import ProjectContext

    out: dict[str, set[str]] = {}
    for prof_dir in sorted((REPO / "profiles").iterdir()):
        if not (prof_dir / ".mcp.json").is_file():
            continue
        ctx = ProjectContext(user_cwd=REPO, project_root=prof_dir)
        _tools, sm, infos = await resolve_and_load_mcp_tools(
            project_context=ctx, no_mcp=False, trust_project_mcp=True)
        for info in infos:
            out.setdefault(info.name, set()).update(
                getattr(t, "name", str(t)) for t in info.tools)
        try:
            await sm.aclose()
        except Exception:
            pass
    return {k: sorted(v) for k, v in out.items() if v}


def _github_names() -> list[str]:
    """tools/list over stdio against the official docker image."""
    handshake = (
        '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":'
        '"2025-03-26","capabilities":{},"clientInfo":{"name":"enum","version":"0"}}}\n'
        '{"jsonrpc":"2.0","method":"notifications/initialized"}\n'
        '{"jsonrpc":"2.0","id":2,"method":"tools/list"}\n'
    )
    proc = subprocess.run(
        ["docker", "run", "-i", "--rm",
         "-e", "GITHUB_PERSONAL_ACCESS_TOKEN=enumerate-only",
         "ghcr.io/github/github-mcp-server:latest", "stdio"],
        input=handshake, capture_output=True, text=True, timeout=120)
    for line in proc.stdout.splitlines():
        try:
            msg = json.loads(line)
        except json.JSONDecodeError:
            continue
        if msg.get("id") == 2:
            return sorted(f"github_{t['name']}" for t in msg["result"]["tools"])
    raise RuntimeError(f"github tools/list handshake failed: {proc.stderr[-300:]}")


def _config_trimmed(prefix: str, server: str) -> list[str]:
    """allowedTools from any profile .mcp.json declaring `server` (down servers)."""
    for prof_dir in sorted((REPO / "profiles").iterdir()):
        f = prof_dir / ".mcp.json"
        if not f.is_file():
            continue
        cfg = json.loads(f.read_text()).get("mcpServers", {})
        if server in cfg and cfg[server].get("allowedTools"):
            return sorted(f"{prefix}_{n}" for n in cfg[server]["allowedTools"])
    return []


def _nitter_names() -> list[str]:
    src = (REPO / "infra" / "nitter" / "src" / "server.py").read_text()
    return sorted(f"nitter_{n}" for n in re.findall(r"@mcp\.tool\s*\nasync def (\w+)\(", src))


async def main() -> int:
    _load_env()
    live = await _live_servers()

    sections: list[tuple[str, list[str], str]] = []
    # Live captures first (dcode-prefixed names as served).
    for name in sorted(live):
        sections.append((name, live[name], "live dcode resolve"))
    # github: docker handshake (local binary absent).
    sections.append(("github", _github_names(),
                     "docker stdio handshake ghcr.io/github/github-mcp-server"))
    # nitter: in-repo source (container flaky).
    if "nitter" not in live:
        sections.append(("nitter", _nitter_names(), "tool defs in infra/nitter/src/server.py"))
    # ray: down at capture; the config trim is the exhaustive surface.
    if "ray_inference" not in live:
        sections.append(("ray_inference", _config_trimmed("ray_inference", "ray_inference"),
                         "allowedTools trim declared in .mcp.json"))
    # equibles: no enumeration source — surfaced by make scoping-check when up.

    lines = [
        "# The no-MCP subagent tier.",
        "#",
        "# Registered under the model key below; scoped agents opt in via",
        "# frontmatter `model: openai:glm-5-turbo` (a real model on the same",
        "# z.ai gateway, one no other session uses, so the key can never hit",
        "# the main agent). excluded_tools is a DENY list of every MCP tool",
        "# name this workspace can serve, so scoped agents keep the built-ins",
        "# (execute/read/write/task) and lose every MCP tool.",
        "#",
        "# This list FAILS OPEN: a server that adds a tool after capture leaks",
        "# into scoped agents. `make scoping-check` is the tripwire; regenerate",
        "# with plugins/tool-scoping/regenerate.py after any server change.",
        "#",
        f"# Regenerated: {__import__('datetime').date.today():%Y-%m-%d}.",
        "#",
        "# Known open hole: `equibles` (down at capture time, external, no",
        "# in-repo source to enumerate) — the check flags it if it ever serves",
        "# tools into a scoped session. NOT excluded: built-in dcode tools.",
        f"key: {PROFILE_KEY}",
        "excluded_tools:",
    ]
    total = 0
    for server, names, provenance in sections:
        lines.append(f"  # {server} ({len(names)}) — {provenance}")
        lines.extend(f"  - {n}" for n in names)
        total += len(names)
        lines.append("")

    path = PLUGIN / "tool_scoping_dcode" / "profiles" / f"{PROFILE_KEY.replace(':', '-')}.yaml"
    path.write_text("\n".join(lines).rstrip() + "\n")
    print(f"regenerated {path.relative_to(REPO)}: {total} tools across {len(sections)} servers")
    for server in ("github", "equibles", "ray_inference", "nitter"):
        if server not in live:
            print(f"  NOTE: '{server}' was NOT live — captured from its fallback source" if server != "equibles"
                  else "  NOTE: 'equibles' has NO enumeration source — uncovered until it serves tools")
    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
