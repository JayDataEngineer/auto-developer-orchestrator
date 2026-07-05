"""MCP server that wraps the Pux Agent Protocol REST API.

Exposes the agent graph as MCP tools so any MCP client (Zed, Claude Desktop,
etc.) can drive Pux without knowing the Agent Protocol wire format.

Requires the Agent Protocol server to be running: ``pux serve``.
"""
from __future__ import annotations

import asyncio
import json
import os
from typing import Any

import httpx
from fastmcp import FastMCP

PUX_API = os.environ.get("PUX_API_URL", "http://127.0.0.1:9988")
TIMEOUT = float(os.environ.get("PUX_MCP_TIMEOUT", "300"))

mcp = FastMCP(
    "pux",
    instructions=(
        "Pux agent harness. Use list_orgs to see available agents, "
        "then run_agent to execute a task. Threads persist across calls."
    ),
)


async def _post(path: str, body: dict[str, Any] | None = None) -> dict[str, Any]:
    async with httpx.AsyncClient(timeout=TIMEOUT) as c:
        r = await c.post(f"{PUX_API}{path}", json=body or {})
        r.raise_for_status()
        return r.json()


async def _get(path: str) -> dict[str, Any]:
    async with httpx.AsyncClient(timeout=TIMEOUT) as c:
        r = await c.get(f"{PUX_API}{path}")
        r.raise_for_status()
        return r.json()


async def _delete(path: str) -> dict[str, Any]:
    async with httpx.AsyncClient(timeout=TIMEOUT) as c:
        r = await c.delete(f"{PUX_API}{path}")
        r.raise_for_status()
        return r.json() if r.content else {}


@mcp.tool()
async def list_orgs() -> str:
    """List available Pux orgs (agents) and their specialist subagents."""
    data = await _post("/agents/search", {"metadata": {}, "page": 1})
    agents = data.get("agents", [])
    lines = []
    for a in agents:
        specialists = a.get("metadata", {}).get("specialists", [])
        lines.append(f"- **{a['agent_id']}**: {', '.join(specialists) or '(none)'}")
    return "\n".join(lines) or "No orgs found."


@mcp.tool()
async def run_agent(
    task: str,
    org: str = "general",
    thread_id: str | None = None,
) -> str:
    """Run a task on a Pux org. Returns the agent's final answer.

    Args:
        task: The task/prompt to send to the agent.
        org: The org to use (default: general).
        thread_id: Optional thread ID to continue a previous conversation.
    """
    body: dict[str, Any] = {
        "agent_id": org,
        "input": task,
        "metadata": {},
    }
    if thread_id:
        body["thread_id"] = thread_id
    data = await _post("/runs/wait", body)
    return _extract_answer(data)


@mcp.tool()
async def create_thread(org: str = "general") -> str:
    """Create a new conversation thread for an org.

    Returns the thread_id you can pass to run_agent for multi-turn conversations.
    """
    data = await _post("/threads", {"agent_id": org, "metadata": {}})
    return data.get("thread_id", "")


@mcp.tool()
async def get_thread(thread_id: str) -> str:
    """Get the current state of a thread (messages, status)."""
    data = await _get(f"/threads/{thread_id}")
    messages = data.get("values", {}).get("messages", [])
    if not messages:
        return "Thread is empty."
    lines = []
    for m in messages:
        role = m.get("role", "?")
        content = m.get("content", "")
        if isinstance(content, list):
            content = " ".join(
                str(b.get("text", b)) if isinstance(b, dict) else str(b)
                for b in content
            )
        lines.append(f"**{role}**: {content}")
    return "\n\n".join(lines)


@mcp.tool()
async def list_threads(org: str | None = None) -> str:
    """List recent threads, optionally filtered by org."""
    body: dict[str, Any] = {"metadata": {}, "page": 1}
    if org:
        body["agent_id"] = org
    data = await _post("/threads/search", body)
    threads = data.get("threads", [])
    if not threads:
        return "No threads found."
    lines = []
    for t in threads:
        tid = t.get("thread_id", "?")
        created = t.get("created_at", "?")[:19]
        agent = t.get("metadata", {}).get("agent_id", "?")
        lines.append(f"- `{tid}` — {agent} — {created}")
    return "\n".join(lines)


@mcp.tool()
async def get_thread_history(thread_id: str) -> str:
    """Get the full revision history (checkpoints) of a thread."""
    data = await _get(f"/threads/{thread_id}/history")
    history = data if isinstance(data, list) else data.get("history", [])
    if not history:
        return "No history."
    lines = []
    for i, h in enumerate(history):
        ts = h.get("created_at", "?")[:19]
        msgs = h.get("values", {}).get("messages", [])
        last_content = ""
        if msgs:
            last = msgs[-1]
            c = last.get("content", "")
            if isinstance(c, list):
                c = " ".join(str(b.get("text", b)) if isinstance(b, dict) else str(b) for b in c)
            last_content = str(c)[:120]
        lines.append(f"{i+1}. [{ts}] {last_content}")
    return "\n".join(lines)


@mcp.tool()
async def delete_thread(thread_id: str) -> str:
    """Delete a thread."""
    await _delete(f"/threads/{thread_id}")
    return f"Thread {thread_id} deleted."


def _extract_answer(data: dict[str, Any]) -> str:
    """Pull the final assistant message from a /runs/wait response."""
    messages = data.get("messages", [])
    if not messages:
        return json.dumps(data, indent=2)
    last = messages[-1]
    content = last.get("content", "")
    if isinstance(content, list):
        parts = []
        for b in content:
            if isinstance(b, dict):
                parts.append(str(b.get("text", b)))
            else:
                parts.append(str(b))
        content = "\n".join(parts)
    return str(content) if content else json.dumps(data, indent=2)


def main() -> None:
    mcp.run(transport="sse", host="0.0.0.0", port=9987)


if __name__ == "__main__":
    main()
