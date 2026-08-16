#!/usr/bin/env python3
"""Godot MCP client — HTTP bridge to a separately-running IvanMurzak/Godot-MCP server.

The server is run by the user on the host (or anywhere reachable from this
sandbox) and proxies 39 Godot-editor tools over HTTP. If the server is down,
every call returns "GODOT_MCP_DOWN" so the CTO can route around it.

Usage:
  python3 godot_client.py health
  python3 godot_client.py scene-open res://scenes/main.tscn
  python3 godot_client.py scene-save
  python3 godot_client.py script-read res://scripts/player.gd
  python3 godot_client.py script-update res://scripts/player.gd --content /path/to/new.gd
  python3 godot_client.py screenshot-viewport --out qa/shot.png
  python3 godot_client.py runtime-errors-get
  python3 godot_client.py call <tool_name> --args '{"key":"value"}'

Env:
  GODOT_MCP_URL — base URL of the gamedev-mcp-server (default: http://localhost:8080)
"""
import argparse
import json
import os
import sys
import urllib.error
import urllib.request

BASE = os.environ.get("GODOT_MCP_URL", "http://localhost:8080")
TIMEOUT = 90  # Godot editor ops are usually fast; screenshots can lag


def _rpc(tool_name, arguments):
    """Invoke a tools/call JSON-RPC request. Returns parsed JSON or error dict."""
    body = json.dumps({
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {"name": tool_name, "arguments": arguments or {}},
    }).encode("utf-8")
    req = urllib.request.Request(
        f"{BASE.rstrip('/')}/mcp",
        data=body,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
            return json.loads(r.read().decode("utf-8"))
    except urllib.error.URLError as e:
        return {"error": "GODOT_MCP_DOWN", "detail": str(e)}
    except Exception as e:  # noqa: BLE001
        return {"error": "GODOT_MCP_CALL_FAILED", "detail": str(e)}


def _is_down(resp):
    return isinstance(resp, dict) and resp.get("error") == "GODOT_MCP_DOWN"


def cmd_health(args):
    resp = _rpc("editor-application-get-state", {})
    if _is_down(resp):
        print("GODOT_MCP_DOWN")
        return 1
    print(json.dumps({"status": "ok", "editor": resp.get("result", {})}))
    return 0


def cmd_scene_open(args):
    resp = _rpc("scene-open", {"path": args.path})
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def cmd_scene_save(args):
    resp = _rpc("scene-save", {})
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def cmd_script_read(args):
    resp = _rpc("script-read", {"path": args.path})
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def cmd_script_update(args):
    try:
        with open(args.content_file, "r", encoding="utf-8") as f:
            content = f.read()
    except OSError as e:
        print(json.dumps({"error": "READ_FAILED", "detail": str(e)}))
        return 2
    resp = _rpc("script-update", {"path": args.path, "content": content})
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def cmd_screenshot(args):
    resp = _rpc("screenshot-viewport", {"output_path": args.out})
    if _is_down(resp):
        print(json.dumps(resp))
        return 1
    print(json.dumps({"saved_to": args.out, "raw": resp}))
    return 0


def cmd_runtime_errors(args):
    resp = _rpc("runtime-errors-get", {})
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def cmd_console_logs(args):
    resp = _rpc("console-get-logs", {})
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def cmd_call(args):
    """Escape hatch — call any tool by name with JSON args."""
    try:
        arguments = json.loads(args.args) if args.args else {}
    except json.JSONDecodeError as e:
        print(json.dumps({"error": "BAD_ARGS_JSON", "detail": str(e)}))
        return 2
    resp = _rpc(args.tool_name, arguments)
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def main():
    p = argparse.ArgumentParser(description="Godot MCP HTTP bridge")
    sub = p.add_subparsers(dest="cmd", required=True)

    sub.add_parser("health", help="Check if Godot MCP server is reachable")

    s_open = sub.add_parser("scene-open", help="Open a scene in the editor")
    s_open.add_argument("path", help="res:// path to the scene")

    sub.add_parser("scene-save", help="Save the currently-active scene")

    s_read = sub.add_parser("script-read", help="Read a GDScript file via the editor")
    s_read.add_argument("path", help="res:// path to the script")

    s_upd = sub.add_parser("script-update", help="Update a GDScript file via the editor")
    s_upd.add_argument("path", help="res:// path to the script")
    s_upd.add_argument("--content", dest="content_file", required=True,
                       help="Local file whose contents become the new script body")

    s_shot = sub.add_parser("screenshot-viewport", help="Capture the editor viewport")
    s_shot.add_argument("--out", required=True, help="Where to save the PNG")

    sub.add_parser("runtime-errors-get", help="List runtime errors from the editor")
    sub.add_parser("console-logs", help="Dump console output")

    s_call = sub.add_parser("call", help="Call any tool by name (escape hatch)")
    s_call.add_argument("tool_name")
    s_call.add_argument("--args", default="{}", help='JSON arguments, e.g. \'{"path":"res://x"}\'')

    args = p.parse_args()
    handlers = {
        "health": cmd_health,
        "scene-open": cmd_scene_open,
        "scene-save": cmd_scene_save,
        "script-read": cmd_script_read,
        "script-update": cmd_script_update,
        "screenshot-viewport": cmd_screenshot,
        "runtime-errors-get": cmd_runtime_errors,
        "console-logs": cmd_console_logs,
        "call": cmd_call,
    }
    return handlers[args.cmd](args)


if __name__ == "__main__":
    sys.exit(main())
