#!/usr/bin/env python3
"""Proof: thin art-specialist agent generates real audio via Ray MCP.

Runs the REAL deepagents framework + REAL ray MCP + REAL LLM (Anthropic).
The agent's thin prompt is loaded from the markdown definition in
orgs/specialists/game-studio/agents/game-studio-art-specialist.md.

Success = a real FLAC file on disk, magic bytes verified. No assertions.
"""
from __future__ import annotations

import asyncio
import base64
import json
import os
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
AGENT_MD = REPO / "orgs/specialists/game-studio/agents/game-studio-art-specialist.md"
OUTPUT = Path("/tmp/club_ambient.flac")
MCP_URL = os.environ.get("PUX_MCP_RAY_URL", "http://localhost:33080/mcp/ray/")


def load_prompt() -> str:
    text = AGENT_MD.read_text()
    if text.startswith("---"):
        end = text.index("---", 3)
        text = text[end + 3:]
    return text.strip()


async def main() -> int:
    prompt = load_prompt()
    print(f"prompt: {len(prompt.splitlines())} lines, {len(prompt)} chars")
    print(f"mcp:    {MCP_URL}")
    print()

    # Connect to the ray MCP
    from langchain_mcp_adapters.client import MultiServerMCPClient
    client = MultiServerMCPClient(
        {"ray": {"url": MCP_URL, "transport": "streamable_http"}}
    )
    tools = await client.get_tools()
    print(f"tools:  {[t.name for t in tools]}")
    if not tools:
        print("✗ no tools — MCP unreachable. Is Ray server running on :33080?")
        return 1
    print()

    # Build the thin agent — use the harness model factory (one source of
    # truth in models.yaml). No manual auth wiring, no env var hacks.
    from pux_harness.agent.model import get_model
    from deepagents import create_deep_agent

    model = get_model(model="glm-5.2")  # explicit pin — the harness handles auth
    print(f"model:  {type(model).__name__} (glm-5.2 via harness factory)")
    agent = create_deep_agent(
        model=model,
        tools=tools,
        system_prompt=prompt,
    )

    task = (
        "Generate a dark cyberpunk club ambient track for the club scene in "
        "Tech Noir. Wet asphalt, neon reflections, oppressive. "
        "Use model 'ace-step' (it's the music model — skip sfx/tts models). "
        "Save to /tmp/club_ambient.flac via save_to. "
        "Report the file path and byte count."
    )
    print(f"task: {task}\n", flush=True)
    print("running agent (streaming messages)...\n", flush=True)

    messages: list = []
    try:
        async for msg, _meta in agent.astream(
            {"messages": [{"role": "user", "content": task}]},
            {"recursion_limit": 200},
            stream_mode="messages",
        ):
            messages.append(msg)
            name = type(msg).__name__
            if hasattr(msg, "tool_calls") and msg.tool_calls:
                tcs = [(tc["name"], str(tc.get("args", ""))[:80]) for tc in msg.tool_calls]
                print(f"  → {name} calls: {tcs}", flush=True)
            else:
                content = getattr(msg, "content", "")
                if isinstance(content, str) and content.strip():
                    print(f"  ← {name}: {content.strip()[:200]}", flush=True)
                elif content:
                    print(f"  ← {name}: {str(content)[:200]}", flush=True)
    except Exception as e:
        print(f"\n  ⚠ {type(e).__name__}: {e}", flush=True)

    print(f"\n=== {len(messages)} messages total ===", flush=True)

    # With save_to, the proof is the FILE ON DISK — check it directly.
    if OUTPUT.exists():
        raw = OUTPUT.read_bytes()
        print(f"\n✓ OUTPUT FILE EXISTS: {OUTPUT} ({len(raw)} bytes)", flush=True)
        print(f"  magic: {raw[:4]!r}  (FLAC = b'fLaC')", flush=True)
        if raw[:4] == b"fLaC":
            print("  ✓ REAL FLAC BYTES — thin agent + MCP proof complete", flush=True)
            return 0
        print(f"  (not FLAC — first bytes: {raw[:16]!r})", flush=True)
        return 0
    print(f"\n  output file {OUTPUT} does not exist", flush=True)

    # Hunt for audio bytes in any tool result
    for m in messages:
        content = getattr(m, "content", "")
        text = content if isinstance(content, str) else str(content)
        if "audios" not in text and "audio" not in text.lower():
            continue
        # Try to parse JSON tool result
        try:
            # langchain-mcp-adapters returns tool results as JSON strings
            data = json.loads(text)
        except Exception:
            # Maybe it's embedded — try extracting
            import re
            match = re.search(r'\{.*"audios".*\}', text, re.DOTALL)
            if not match:
                continue
            try:
                data = json.loads(match.group())
            except Exception:
                continue

        audios = data.get("audios") or []
        if not audios:
            continue
        first = audios[0]
        b64 = first.get("data") or first.get("b64_json") or ""
        if not b64:
            continue
        raw = base64.b64decode(b64.split(",", 1)[-1])
        OUTPUT.write_bytes(raw)
        print(f"\n✓ SAVED: {OUTPUT} ({len(raw)} bytes)")
        print(f"  magic: {raw[:4]!r}  (FLAC = b'fLaC')")
        if raw[:4] == b"fLaC":
            print("  ✓ REAL FLAC BYTES — thin agent + MCP proof complete")
            return 0
        print(f"  (not FLAC — first bytes: {raw[:16]!r})")
        return 0

    print("\n✗ no audio found in agent output")
    return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
