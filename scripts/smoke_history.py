#!/usr/bin/env python3
"""smoke_history.py — end-to-end verification of the history sidecar.

Boots the MCP server with PUX_HISTORY_DIR + PUX_LLM_API_KEY set,
dispatches a task against the _demo org, polls to completion, then
shells out to pux-history list/show/search and verifies each returns
the expected content.

Requires:
  - mcpserver binary at $MCPSERVER_BIN (default: /tmp/mcpserver-hist)
  - pux-history binary at $PUX_HISTORY_BIN (default: /tmp/pux-history)
  - $PUX_PROJECT_PATH (defaults to repo root so orgs/_demo is visible)
  - $PUX_LLM_API_KEY (or passed via env)
  - $PUX_LLM_BASE_URL (e.g. https://api.deepseek.com/anthropic)
  - $PUX_LLM_MODEL (e.g. deepseek-v4-flash)

Exit code 0 = all checks passed. Non-zero = first failure.
"""
from __future__ import annotations

import json
import os
import socket
import subprocess
import sys
import time
import urllib.request
import uuid
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
HIST_DIR = Path(os.environ.get("PUX_HIST_SMOKEDIR", "/tmp/pux-hist-smoke"))
ADDR = "127.0.0.1:19876"
BASE = f"http://{ADDR}"

MCPSERVER_BIN = os.environ.get("MCPSERVER_BIN", "/tmp/mcpserver-hist")
PUX_HISTORY_BIN = os.environ.get("PUX_HISTORY_BIN", "/tmp/pux-history")


def die(msg: str) -> None:
    print(f"FAIL  {msg}", file=sys.stderr)
    sys.exit(1)


def expect(cond: bool, msg: str) -> None:
    if cond:
        print(f"  PASS  {msg}")
    else:
        die(msg)


def rpc(method: str, params: dict, session_id: str | None = None) -> dict:
    """Send one JSON-RPC request; return the result object."""
    payload = {
        "jsonrpc": "2.0",
        "id": str(uuid.uuid4()),
        "method": method,
        "params": params,
    }
    headers = {"content-type": "application/json"}
    if session_id:
        headers["mcp-session-id"] = session_id
    req = urllib.request.Request(
        BASE, data=json.dumps(payload).encode(), headers=headers, method="POST"
    )
    with urllib.request.urlopen(req, timeout=120) as r:
        body = json.loads(r.read())
    if "error" in body:
        die(f"{method} returned error: {body['error']}")
    return body.get("result", {})


def wait_for_port(addr: str, timeout_s: float = 30.0) -> None:
    host, port = addr.split(":")
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        try:
            with socket.create_connection((host, int(port)), timeout=0.5):
                return
        except OSError:
            time.sleep(0.25)
    die(f"server didn't bind {addr} within {timeout_s}s")


def poll_until(task_id: str, session_id: str, deadline_s: float) -> dict:
    """Poll get_task_status until status is complete/failed or deadline hits."""
    deadline = time.time() + deadline_s
    last = None
    while time.time() < deadline:
        result = rpc(
            "tools/call",
            {"name": "get_task_status", "arguments": {"task_id": task_id}},
            session_id,
        )
        # result.content[0].text is a JSON-encoded string of the status body
        text = result["content"][0]["text"]
        body = json.loads(text)
        last = body
        if body.get("status") in ("complete", "failed"):
            return body
        time.sleep(1.5)
    die(f"task {task_id} didn't finish within {deadline_s}s. last status: {last}")


def main() -> None:
    # Sanity-check env
    for key in ("PUX_LLM_API_KEY",):
        if not os.environ.get(key):
            die(f"{key} must be set in env")
    if not Path(MCPSERVER_BIN).exists():
        die(f"mcpserver binary not found at {MCPSERVER_BIN}")
    if not Path(PUX_HISTORY_BIN).exists():
        die(f"pux-history binary not found at {PUX_HISTORY_BIN}")

    HIST_DIR.mkdir(parents=True, exist_ok=True)
    # Wipe any stale sqlite so the smoke is reproducible.
    for stale in HIST_DIR.glob("history.sqlite*"):
        stale.unlink()

    project = os.environ.get("PUX_PROJECT_PATH", str(REPO_ROOT))

    print("=== booting server ===")
    server_env = {
        **os.environ,
        "PUX_HISTORY_DIR": str(HIST_DIR),
        "PUX_PROJECT_PATH": project,
    }
    server = subprocess.Popen(
        [MCPSERVER_BIN, "--addr", ADDR, "--project", project],
        env=server_env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    try:
        wait_for_port(ADDR)

        # Initialize MCP session, capturing Mcp-Session-Id from the
        # response header (the body doesn't carry it).
        init_payload = json.dumps({
            "jsonrpc": "2.0", "id": "init",
            "method": "initialize",
            "params": {
                "protocolVersion": "2025-03-26",
                "capabilities": {},
                "clientInfo": {"name": "smoke_history", "version": "1.0"},
            },
        }).encode()
        init_req = urllib.request.Request(
            BASE, data=init_payload,
            headers={"content-type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(init_req, timeout=30) as r:
            init_body = json.loads(r.read())
            session_id = r.headers.get("mcp-session-id")
        expect(init_body.get("result", {}).get("protocolVersion") == "2025-03-26",
               f"protocol handshake (got {init_body.get('result', {}).get('protocolVersion')})")
        expect(bool(session_id), f"got Mcp-Session-Id header ({session_id})")

        # Send the initialized notification (fire-and-forget)
        notif_payload = json.dumps({
            "jsonrpc": "2.0",
            "method": "notifications/initialized",
            "params": {},
        }).encode()
        notif_req = urllib.request.Request(
            BASE, data=notif_payload,
            headers={"content-type": "application/json",
                     "mcp-session-id": session_id},
            method="POST",
        )
        with urllib.request.urlopen(notif_req, timeout=10) as r:
            r.read()  # notifications/initialized returns no response

        # Dispatch a task that REQUIRES bash tool use so we can verify
        # tool_calls row in history.
        print("\n=== dispatching task ===")
        task_desc = (
            "Use the bash tool to run: echo hello-from-cto. "
            "Then reply with the literal output you observed."
        )
        dispatch_result = rpc("tools/call", {
            "name": "dispatch_task",
            "arguments": {"org_name": "_demo", "task_description": task_desc},
        }, session_id)
        dispatch_body = json.loads(dispatch_result["content"][0]["text"])
        task_id = dispatch_body.get("task_id")
        expect(bool(task_id), f"dispatch_task returned task_id ({task_id})")
        print(f"  dispatched task_id={task_id}")

        # Poll to completion (DeepSeek is fast; allow 90s budget)
        print("\n=== polling to completion ===")
        final = poll_until(task_id, session_id, deadline_s=120.0)
        expect(final.get("status") == "complete",
               f"task status complete (got {final.get('status')}: {final.get('error', '')[:120]})")
        result_text = (final.get("result") or "").lower()
        print(f"  task result: {final.get('result', '')[:120]}")

        # === Verify pux-history list ===
        print("\n=== pux-history list ===")
        list_out = subprocess.run(
            [PUX_HISTORY_BIN, "list"],
            env={**os.environ, "PUX_HISTORY_DIR": str(HIST_DIR)},
            capture_output=True, text=True, timeout=10,
        )
        if list_out.returncode != 0:
            die(f"pux-history list failed: {list_out.stderr}")
        expect(task_id in list_out.stdout,
               f"pux-history list mentions task_id ({task_id})")

        # === Verify pux-history show ===
        print("\n=== pux-history show ===")
        show_out = subprocess.run(
            [PUX_HISTORY_BIN, "show", task_id],
            env={**os.environ, "PUX_HISTORY_DIR": str(HIST_DIR)},
            capture_output=True, text=True, timeout=10,
        )
        if show_out.returncode != 0:
            die(f"pux-history show failed: {show_out.stderr}")
        show_text = show_out.stdout
        expect("Task     " in show_text, "show prints Task header")
        expect("_demo" in show_text, "show mentions org _demo")
        expect("Status   complete" in show_text,
               "show prints status=complete")
        # Tool-call recording: bash should appear in the transcript.
        # If the model never called bash (just answered from training data),
        # this is a soft fail — flag but don't die.
        if "tool bash" in show_text:
            print("  PASS  show contains bash tool call")
            expect("hello-from-cto" in show_text,
                   "show contains the echo output 'hello-from-cto'")
        else:
            print(f"  WARN  no bash tool call recorded — model may have skipped tool use")
            print(f"        show output:\n{show_text}")

        # === Verify pux-history search ===
        print("\n=== pux-history search ===")
        search_out = subprocess.run(
            [PUX_HISTORY_BIN, "search", "hello-from-cto"],
            env={**os.environ, "PUX_HISTORY_DIR": str(HIST_DIR)},
            capture_output=True, text=True, timeout=10,
        )
        if search_out.returncode != 0:
            die(f"pux-history search failed: {search_out.stderr}")
        expect("hello-from-cto" in search_out.stdout,
               "pux-history search finds 'hello-from-cto'")

        # === Role correlation: dispatch a delegation task ===============
        # Force the CTO to delegate to the researcher role so we can verify
        # the role column lands as "researcher" in the resulting rows.
        print("\n=== delegation: dispatch + verify role column ===")
        deleg_desc = (
            "Use the delegate_to tool to delegate this task to the "
            "'researcher' role: 'use bash to run: echo ping-from-researcher'. "
            "Reply with the literal result the researcher returned."
        )
        deleg_result = rpc("tools/call", {
            "name": "dispatch_task",
            "arguments": {"org_name": "_demo", "task_description": deleg_desc},
        }, session_id)
        deleg_body = json.loads(deleg_result["content"][0]["text"])
        deleg_id = deleg_body.get("task_id")
        expect(bool(deleg_id), f"delegation dispatch returned task_id ({deleg_id})")
        print(f"  dispatched delegation task_id={deleg_id}")

        deleg_final = poll_until(deleg_id, session_id, deadline_s=180.0)
        expect(deleg_final.get("status") == "complete",
               f"delegation status complete (got {deleg_final.get('status')}: "
               f"{deleg_final.get('error', '')[:120]})")
        print(f"  delegation result: {deleg_final.get('result', '')[:120]}")

        deleg_show = subprocess.run(
            [PUX_HISTORY_BIN, "show", deleg_id],
            env={**os.environ, "PUX_HISTORY_DIR": str(HIST_DIR)},
            capture_output=True, text=True, timeout=10,
        )
        if deleg_show.returncode != 0:
            die(f"delegation show failed: {deleg_show.stderr}")
        deleg_text = deleg_show.stdout
        # CTO line + researcher line should both appear — that's the
        # delegation chain landing in one task ID.
        expect("| cto]" in deleg_text,
               "delegation transcript has [round N | cto] entry")
        # Soft check on researcher — model could theoretically answer
        # without delegating. If so, warn rather than fail.
        if "| researcher]" in deleg_text:
            print("  PASS  delegation transcript has [round N | researcher] entry")
            if "ping-from-researcher" in deleg_text:
                print("  PASS  researcher echo output landed in transcript")
            else:
                print("  WARN  researcher role row exists but no ping-from-researcher text")
        else:
            print("  WARN  no researcher role row — model may have skipped delegation")
            print(f"        show output:\n{deleg_text}")

        print("\n=== ALL CHECKS PASSED ===")
    finally:
        server.terminate()
        try:
            server.wait(timeout=15)
        except subprocess.TimeoutExpired:
            server.kill()


if __name__ == "__main__":
    main()
