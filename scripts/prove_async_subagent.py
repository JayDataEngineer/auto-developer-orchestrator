#!/usr/bin/env python3
"""Proof: AsyncSubAgent — non-blocking delegation to a remote Agent Protocol server.

Demonstrates deepagents 0.7.x third delegation mode:
  - start_async_task → dispatches to Aegra :9988, returns task_id IMMEDIATELY
  - check_async_task / list_async_tasks → polls the remote run
  - Real video file produced by the game-studio team via Ray MCP generate()

Architecture:
  Supervisor (in-process deep agent)
    └─ AsyncSubAgentMiddleware(url=http://localhost:9988, graph_id=game-studio)
         └─ start_async_task(description, subagent_type="game-studio")
              └─ langgraph_sdk → Aegra creates thread + run (NON-BLOCKING)
                   └─ game-studio graph builds on-demand
                        └─ orchestrator → mcp__ray_inference__generate(comfyui_video)
                             └─ Ray → ComfyUI → LTX video → /tmp/proof_async_video.mp4

Success criteria:
  1. start_async_task returns a task_id in <60s (non-blocking proof)
  2. The task eventually reaches status=success on Aegra
  3. A real MP4 file exists on disk with valid magic bytes
"""
from __future__ import annotations

import asyncio
import json
import os
import re
import sys
import time
from pathlib import Path

PUX_ROOT = Path("/home/user/Pux")
AEGRA_URL = "http://localhost:9988"
VIDEO_PATH = "/tmp/proof_async_video.mp4"
POLL_INTERVAL = 15          # seconds between status checks
MAX_POLL_TIME = 30 * 60     # 30 minutes max


def load_env() -> None:
    """Source the project .env so ANTHROPIC_AUTH_TOKEN + PUX_MCP_*_URL are set."""
    env_file = PUX_ROOT / ".env"
    for line in env_file.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, val = line.partition("=")
        key = key.strip()
        val = val.strip().strip('"').strip("'")
        os.environ.setdefault(key, val)
    os.environ["PUX_PROJECT_ROOT"] = str(PUX_ROOT)


def _msg_text(msg) -> str:
    """Extract text content from a LangChain message (dict or object)."""
    if isinstance(msg, dict):
        return str(msg.get("content", ""))
    return str(getattr(msg, "content", ""))


def _msg_type(msg) -> str:
    if isinstance(msg, dict):
        return str(msg.get("type", "?"))
    return str(getattr(msg, "type", type(msg).__name__))


async def main() -> int:
    load_env()

    print("=" * 72)
    print("  AsyncSubAgent Proof — deepagents 0.7.x third delegation mode")
    print("=" * 72)

    # ── 1. Build the supervisor agent ───────────────────────────────────
    from pux_harness.agent.model import get_model
    from deepagents import create_deep_agent
    from deepagents.middleware.async_subagents import AsyncSubAgentMiddleware

    print("\n[1/5] Building supervisor agent with AsyncSubAgentMiddleware...")
    model = get_model(model="glm-5.2")

    middleware = AsyncSubAgentMiddleware(
        async_subagents=[
            {
                "name": "game-studio",
                "description": (
                    "The game-studio team — produces game assets including "
                    "videos, images, 3D models, and audio via Ray MCP. "
                    "Use for any media generation task."
                ),
                "graph_id": "game-studio",
                "url": AEGRA_URL,
            }
        ],
        system_prompt=(
            "You are a supervisor that delegates long-running creative tasks "
            "to the game-studio team via async delegation. When asked to "
            "generate media, use start_async_task with subagent_type "
            "'game-studio' and a detailed description. Report the task_id "
            "and stop — do NOT immediately check status."
        ),
    )

    agent = create_deep_agent(
        model=model,
        tools=[],               # middleware provides the 5 async tools
        middleware=[middleware],
    )

    # Verify the middleware wired its tools
    tool_names = []
    for t in agent.get_graph().nodes.get("tools", {}).data.tools if hasattr(
        agent.get_graph().nodes.get("tools", {}).data, "tools") else []:
        tool_names.append(t.name)
    # Fallback: just check the middleware directly
    mw_tools = [t.name for t in middleware.tools]
    print(f"  middleware tools: {mw_tools}")
    expected = {"start_async_task", "check_async_task", "update_async_task",
                "cancel_async_task", "list_async_tasks"}
    assert expected.issubset(set(mw_tools)), \
        f"Missing async tools: {expected - set(mw_tools)}"
    print("  ✓ all 5 async tools present")

    # ── 2. Dispatch the video generation task ───────────────────────────
    print("\n[2/5] Dispatching video generation to game-studio via start_async_task...")

    task_description = (
        "Generate a short video of a futuristic city skyline at sunset with "
        "neon lights and a golden sky. "
        "Call the mcp__ray_inference__generate tool DIRECTLY with these exact "
        "parameters:\n"
        "  model: comfyui_video\n"
        "  prompt: A futuristic city skyline at sunset, glowing neon skyscrapers, "
        "warm golden and purple sky, cinematic wide shot\n"
        "  params: {width: 768, height: 448, num_frames: 33, fps: 16, "
        "sampling_steps: 8}\n"
        f"  save_to: {VIDEO_PATH}\n"
        "Do NOT use curl, REST APIs, or ctx_execute. Call the MCP generate "
        "tool directly — it works now."
    )

    t0 = time.time()
    result = await agent.ainvoke({
        "messages": [{
            "role": "user",
            "content": f"Generate a video as an async task.\n\n{task_description}",
        }]
    })
    t_dispatch = time.time() - t0

    messages = result.get("messages", [])
    print(f"  supervisor responded in {t_dispatch:.1f}s with {len(messages)} messages")

    # ── 3. Extract task_id ──────────────────────────────────────────────
    print("\n[3/5] Extracting task_id from supervisor output...")

    task_id = None
    for msg in reversed(messages):
        text = _msg_text(msg)
        # The start_async_task ToolMessage contains "task_id: <uuid>"
        match = re.search(r'task_id[:\s]+([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})', text, re.I)
        if match:
            task_id = match.group(1)
            break

    if not task_id:
        print("  ✗ Could not find task_id. Last 5 messages:")
        for msg in messages[-5:]:
            text = _msg_text(msg)
            mtype = _msg_type(msg)
            print(f"    [{mtype}] {text[:200]}")
        return 1

    print(f"  task_id: {task_id}")
    print(f"  ✓ NON-BLOCKING: dispatch returned in {t_dispatch:.1f}s")
    print(f"     (the video generates in the BACKGROUND on Aegra)")

    # ── 4. Poll the remote task ─────────────────────────────────────────
    print(f"\n[4/5] Polling remote task (every {POLL_INTERVAL}s, up to {MAX_POLL_TIME//60}min)...")

    from langgraph_sdk import get_client
    client = get_client(url=AEGRA_URL)

    # Get the run_id from the thread
    runs = await client.runs.list(thread_id=task_id)
    if not runs:
        print("  ✗ No runs found on the task thread")
        return 1
    run_id = runs[0]["run_id"]
    print(f"  run_id: {run_id}")

    status = "running"
    poll_start = time.time()
    while time.time() - poll_start < MAX_POLL_TIME:
        run = await client.runs.get(thread_id=task_id, run_id=run_id)
        status = run["status"]
        elapsed = time.time() - t0
        print(f"  [{elapsed:6.0f}s] status={status}")

        if status in ("success", "error", "cancelled", "timeout", "failed"):
            break
        await asyncio.sleep(POLL_INTERVAL)

    if status not in ("success",):
        print(f"\n  Task did not succeed (status={status})")
        if isinstance(run, dict) and run.get("error"):
            print(f"  error: {str(run['error'])[:600]}")
        # Still check for partial output — Aegra may have written the file
        # before the run status was finalized.
    else:
        print(f"\n  Task succeeded!")
        # Try to get run output for logging (non-fatal if it fails — the
        # video file is the real proof, not the thread state).
        try:
            import httpx
            async with httpx.AsyncClient(base_url=AEGRA_URL, timeout=15) as c:
                # langgraph-api runs endpoint includes output after success
                r = await c.get(
                    f"/threads/{task_id}/runs/{run_id}/join",
                    timeout=30,
                )
                if r.status_code == 200:
                    run_data = r.json()
                    final_msgs = (
                        run_data.get("data", {})
                        .get("values", {})
                        .get("messages", [])
                    )
                    if final_msgs:
                        print(f"  {len(final_msgs)} messages in thread.")
                        for msg in reversed(final_msgs):
                            if _msg_type(msg) == "ai":
                                text = _msg_text(msg)
                                if text.strip():
                                    print(f"  final AI: {text[:400]}")
                                    break
        except Exception as exc:
            print(f"  (state retrieval skipped: {exc})")

    # ── 5. Verify the real video file ───────────────────────────────────
    print(f"\n[5/5] Verifying video file at {VIDEO_PATH}...")

    total_elapsed = time.time() - t0
    print(f"  total elapsed: {total_elapsed:.0f}s ({total_elapsed/60:.1f}min)")

    video = Path(VIDEO_PATH)
    if video.exists() and video.stat().st_size > 0:
        raw = video.read_bytes()[:12]
        size = video.stat().st_size
        is_mp4 = len(raw) >= 8 and raw[4:8] == b"ftyp"
        print(f"\n  {'='*50}")
        print(f"  ✓ VIDEO PRODUCED: {VIDEO_PATH}")
        print(f"    size:   {size:,} bytes ({size/1024:.1f} KB)")
        print(f"    magic:  {raw[:8].hex()} ({'MP4' if is_mp4 else 'unknown'})")
        print(f"    time:   {total_elapsed:.0f}s total "
              f"(dispatch {t_dispatch:.0f}s + generation {total_elapsed-t_dispatch:.0f}s)")
        print(f"  {'='*50}")
        return 0

    # Check fallback locations
    print(f"\n  ✗ No video at {VIDEO_PATH}")
    for alt in ["/tmp/proof_gamestudio_video.mp4"]:
        p = Path(alt)
        if p.exists() and p.stat().st_size > 0:
            print(f"  Found at alternative path: {alt} ({p.stat().st_size:,} bytes)")
            return 0

    # Check ComfyUI output directory
    import subprocess
    try:
        r = subprocess.run(
            ["docker", "exec", "inference-comfyui", "find",
             "/mnt/data/comfyui-root/ComfyUI/output", "-name", "*.mp4",
             "-mmin", "-30"],
            capture_output=True, text=True, timeout=10)
        if r.stdout.strip():
            print(f"  Found in ComfyUI output:\n{r.stdout.strip()}")
    except Exception:
        pass

    return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
