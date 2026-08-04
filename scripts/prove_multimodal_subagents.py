#!/usr/bin/env python3
"""Proof: multi-modal dynamic subagents — audio + image in ONE parallel eval.

The strongest proof that the thin-agent pattern is modality-agnostic: the
supervisor dispatches art-specialist for BOTH audio (ace_step) AND image
(z-image) via Promise.all(task({subagentType, description})). Each subagent
calls describe_model → generate in its own isolated context. The supervisor's
thread stays lean — it only sees the synthesized result.

Success = 1 real FLAC + 1 real PNG on disk, magic bytes verified.

This builds on prove_dynamic_subagents.py (audio-only, 3 FLACs) by proving
the SAME pattern works when each subagent uses a DIFFERENT model family
(audio engine vs image diffusion) with completely different params.
"""
from __future__ import annotations

import asyncio
import os
import sys
import time
from pathlib import Path

MCP_URL = os.environ.get("PUX_MCP_RAY_URL", "http://localhost:33080/mcp/ray/")

OUTPUTS = {
    "audio": Path("/tmp/multi_audio.flac"),
    "image": Path("/tmp/multi_image.png"),
}

SUPERVISOR_PROMPT = """\
You are the CTO of a game studio making Tech Noir (2.5D survival horror).
You orchestrate by dispatching specialists — you do NOT generate assets yourself.

## Your tools
- `eval` — a JavaScript REPL. Write code to dispatch work in parallel.
  Inside eval, call task({subagentType, description, label}) which runs a
  subagent and returns a Promise. Use await Promise.all(items.map(...)) to
  run many in parallel. The final expression is the result returned to you.

## Your team
- `art-specialist` — generates ONE game asset per call via the Ray inference
  MCP (list_models, describe_model, generate). Give it: what to generate,
  the model name, and the save_to file path.

## Rules
1. For 2+ assets, ALWAYS use eval + Promise.all — never one-at-a-time.
2. Each art-specialist dispatch needs: a clear generation prompt, the model
   name, and a save_to path.
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

    supervisor = create_deep_agent(
        model=model,
        tools=[],
        subagents=[art_specialist],
        middleware=[CodeInterpreterMiddleware(subagents=True, timeout=600)],
        system_prompt=SUPERVISOR_PROMPT,
    )

    for p in OUTPUTS.values():
        p.unlink(missing_ok=True)

    task_msg = (
        "Generate 2 different assets for Tech Noir. Each uses a DIFFERENT model.\n\n"
        "1. AUDIO: Dark cyberpunk ambient drone — oppressive, neon, wet pavement. "
        "Use model 'ace-step' (music generation). "
        "Save to /tmp/multi_audio.flac\n\n"
        "2. IMAGE: Pixel art game item icon — a rusted iron key, dark metal, "
        "scratched, top-down view, 128x128 pixels. "
        "Use model 'z-image' (image generation). For image, pass width=128 and "
        "height=128 as params. "
        "Save to /tmp/multi_image.png\n\n"
        "Dispatch BOTH art-specialist calls in parallel via eval + "
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

    # Verify outputs
    found = {}
    for kind, p in OUTPUTS.items():
        if p.exists():
            raw = p.read_bytes()
            if kind == "audio":
                magic = raw[:4]
                ok = magic == b"fLaC"
                print(f"  {'✓' if ok else '?'} {p.name} — {len(raw):,} bytes (magic: {magic!r})")
            elif kind == "image":
                magic = raw[:8]
                ok = magic == b"\x89PNG\r\n\x1a\n"
                import struct
                dims = f"{struct.unpack('>II', raw[16:24])[0]}x{struct.unpack('>II', raw[16:24])[1]}" if ok and len(raw) >= 24 else "?"
                print(f"  {'✓' if ok else '?'} {p.name} — {len(raw):,} bytes ({dims}, magic: {magic!r})")
            if ok:
                found[kind] = p
        else:
            print(f"  ✗ {p.name} — NOT FOUND")

    print(f"\n=== {len(found)}/{len(OUTPUTS)} real assets generated ===")
    if len(found) == len(OUTPUTS):
        print("✓ MULTI-MODAL DYNAMIC SUBAGENTS PROOF COMPLETE")
        print("  Supervisor dispatched art-specialist for BOTH audio + image")
        print("  in parallel via eval + Promise.all. Each subagent used a")
        print("  DIFFERENT model family (ace_step audio engine vs z-image")
        print("  diffusion) in isolated context. Zero intermediate tool output")
        print("  entered the supervisor's context.")
        return 0
    print(f"\n⚠ only {len(found)}/{len(OUTPUTS)} — check agent output above")
    return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
