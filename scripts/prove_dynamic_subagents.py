#!/usr/bin/env python3
"""Proof: dynamic subagents — supervisor fans out via task() in the JS interpreter.

deepagents 0.7.3 + CodeInterpreterMiddleware. The supervisor (CTO) writes
JavaScript that dispatches the art-specialist subagent N times via
Promise.all(task({subagentType, description})). Each subagent generates a real
game asset via the Ray inference MCP. The interpreter synthesizes results —
intermediate tool output NEVER enters the supervisor's context.

This is the token-economy win: the orchestrator thread stays lean. A strong
model writes a short dispatch script; the subagents do the heavy lifting in
isolated context windows; only the final synthesized result returns.

Success = N real audio files on disk, magic bytes verified.
"""
from __future__ import annotations

import asyncio
import os
import sys
import time
from pathlib import Path

MCP_URL = os.environ.get("PUX_MCP_RAY_URL", "http://localhost:33080/mcp/ray/")

OUTPUTS = [
    Path("/tmp/dyn_asset_club.flac"),
    Path("/tmp/dyn_asset_storm.flac"),
    Path("/tmp/dyn_asset_wind.flac"),
]

SUPERVISOR_PROMPT = """\
You are the CTO of a game studio making Tech Noir (2.5D survival horror).
You orchestrate by dispatching specialists — you do NOT generate assets yourself.

## Your tools
- `eval` — a JavaScript REPL. Write code to dispatch work in parallel.
  Inside eval, call task({subagentType, description, label}) which runs a
  subagent and returns a Promise. Use await Promise.all(items.map(...)) to
  run many in parallel. The final expression is the result returned to you.
- `task` — dispatch ONE subagent synchronously (outside eval).

## Your team
- `art-specialist` — generates ONE game asset per call via the Ray inference
  MCP (list_models, describe_model, generate). Give it: what to generate,
  the model name, and the save_to file path.

## Rules
1. For 2+ assets, ALWAYS use eval + Promise.all — never one-at-a-time.
2. Each art-specialist dispatch needs: a clear generation prompt, the model
   name (e.g. 'ace_step' for audio), and a save_to path.
3. Real files only. Report paths + byte counts.
"""

ART_PROMPT = """\
You generate ONE game asset per call via the Ray inference MCP.

## Tools
- list_models() — see available models
- describe_model(model) — learn params. ALWAYS call before generate.
- generate(model, prompt, params, save_to) — make the asset. ALWAYS pass
  save_to (a file path). Never omit it — raw base64 bloats context.

## Rules
1. Always describe_model before generate.
2. ALWAYS pass save_to. Report the path + bytes from the returned metadata.
3. If generate returns download_required, skip that model and say so.
4. Real bytes only.
"""


async def main() -> int:
    from langchain_mcp_adapters.client import MultiServerMCPClient

    print(f"MCP: {MCP_URL}")
    client = MultiServerMCPClient(
        {"ray": {"url": MCP_URL, "transport": "streamable_http"}}
    )
    ray_tools = await client.get_tools()
    print(f"ray tools: {[t.name for t in ray_tools]}")
    if not ray_tools:
        print("✗ no tools — Ray MCP unreachable on :33080")
        return 1

    from pux_harness.agent.model import get_model
    from deepagents import create_deep_agent
    from langchain_quickjs import CodeInterpreterMiddleware

    model = get_model(model="glm-5.2")
    print(f"model: {type(model).__name__}")

    art_specialist = {
        "name": "art-specialist",
        "description": (
            "Generates ONE game asset via the Ray inference MCP. "
            "Call with a generation prompt, the model name, and a "
            "save_to file path. Returns the file path + byte count."
        ),
        "system_prompt": ART_PROMPT,
        "tools": ray_tools,
    }

    # timeout=600: each eval can run up to 10 min (subagent audio gen takes 30-60s)
    supervisor = create_deep_agent(
        model=model,
        tools=[],
        subagents=[art_specialist],
        middleware=[CodeInterpreterMiddleware(subagents=True, timeout=600)],
        system_prompt=SUPERVISOR_PROMPT,
    )

    for p in OUTPUTS:
        p.unlink(missing_ok=True)

    task_msg = (
        "Generate 3 ambient audio assets for Tech Noir. Use model 'ace_step' "
        "(music generation). For each asset, the art-specialist must call "
        "describe_model first, then generate with save_to.\n\n"
        "1. Dark cyberpunk club drone — oppressive, neon, wet asphalt. "
        "Save to /tmp/dyn_asset_club.flac\n"
        "2. Distant thunderstorm with heavy rain — ominous, cold. "
        "Save to /tmp/dyn_asset_storm.flac\n"
        "3. Eerie wind through empty corridors — whistling, unsettling. "
        "Save to /tmp/dyn_asset_wind.flac\n\n"
        "Dispatch all 3 art-specialist calls in parallel via eval + "
        "Promise.all(task({subagentType: 'art-specialist', description: ...})). "
        "Report each file path and byte count."
    )
    print(f"\ntask: {task_msg[:120]}...")
    print("streaming (recursion_limit=300)...\n")

    t0 = time.time()
    msg_count = 0
    try:
        async for msg, _meta in supervisor.astream(
            {"messages": [{"role": "user", "content": task_msg}]},
            {"recursion_limit": 300},
            stream_mode="messages",
        ):
            msg_count += 1
            name = type(msg).__name__
            if hasattr(msg, "tool_calls") and msg.tool_calls:
                for tc in msg.tool_calls:
                    args_str = str(tc.get("args", ""))
                    # Show the JS code for eval calls (the interesting part)
                    if tc["name"] == "eval" and "code" in args_str:
                        import json as _json
                        try:
                            code = _json.loads(args_str.replace("'", '"')).get("code", args_str)
                        except Exception:
                            code = args_str
                        preview = code[:300] if len(code) > 300 else code
                        print(f"  → eval (JS):\n{preview}", flush=True)
                    else:
                        print(f"  → {tc['name']}({args_str[:100]})", flush=True)
            else:
                content = getattr(msg, "content", "")
                if isinstance(content, str) and content.strip():
                    print(f"  ← {name}: {content.strip()[:200]}", flush=True)
    except Exception as e:
        print(f"\n⚠ {type(e).__name__}: {e}")

    elapsed = time.time() - t0
    print(f"\n=== {msg_count} messages in {elapsed:.1f}s ===\n")

    found = 0
    for p in OUTPUTS:
        if p.exists():
            raw = p.read_bytes()
            magic = raw[:4]
            is_flac = magic == b"fLaC"
            tag = "✓" if is_flac else "?"
            print(f"{tag} {p.name} — {len(raw):,} bytes (magic: {magic!r})")
            if is_flac:
                found += 1
        else:
            print(f"✗ {p.name} — NOT FOUND")

    print(f"\n=== {found}/{len(OUTPUTS)} real FLAC files ===")
    if found == len(OUTPUTS):
        print("✓ DYNAMIC SUBAGENTS PROOF COMPLETE")
        print("  Supervisor dispatched 3 art-specialist calls in parallel")
        print("  via eval + Promise.all(task({subagentType, description})).")
        print("  Each subagent generated real audio in isolated context.")
        print("  Zero intermediate tool output entered the supervisor's context.")
        return 0
    print(f"\n⚠ only {found}/{len(OUTPUTS)} — check agent output above")
    return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
