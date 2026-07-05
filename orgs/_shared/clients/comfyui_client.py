#!/usr/bin/env python3
"""ComfyUI client — HTTP wrapper for the Ray cluster's ComfyUI proxy.

Routes through the Ray cluster ingress at $COMFYUI_URL. If ComfyUI
is not currently loaded (cold GPU, worktree swap), returns "COMFYUI_DOWN" and
the studio-director falls back to Forge for image gen.

Usage:
  python3 comfyui_client.py health
  python3 comfyui_client.py queue
  python3 comfyui_client.py post-workflow --file /sandbox/workspace/wf.json
  python3 comfyui_client.py post-prompt --workflow '{"3":{"class_type":"KSampler",...}}'

Env:
  COMFYUI_URL — base URL (default: http://localhost:30080/image/comfyui).
                 Set this env var when ComfyUI is hosted on a remote cluster
                 or tailnet node.
"""
import argparse
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

BASE = os.environ.get("COMFYUI_URL", "http://localhost:30080/image/comfyui").rstrip("/")
TIMEOUT = 180  # ComfyUI workflow can run minutes on a cold GPU


def _get(path):
    url = f"{BASE}{path}"
    try:
        with urllib.request.urlopen(url, timeout=30) as r:
            return json.loads(r.read().decode("utf-8"))
    except urllib.error.URLError as e:
        return {"error": "COMFYUI_DOWN", "detail": str(e)}
    except Exception as e:  # noqa: BLE001
        return {"error": "COMFYUI_CALL_FAILED", "detail": str(e)}


def _post_json(path, payload, timeout=TIMEOUT):
    url = f"{BASE}{path}"
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read().decode("utf-8"))
    except urllib.error.URLError as e:
        return {"error": "COMFYUI_DOWN", "detail": str(e)}
    except Exception as e:  # noqa: BLE001
        return {"error": "COMFYUI_CALL_FAILED", "detail": str(e)}


def _is_down(resp):
    return isinstance(resp, dict) and resp.get("error") in ("COMFYUI_DOWN", "COMFYUI_CALL_FAILED")


def cmd_health(args):
    resp = _get("/system_stats")
    if _is_down(resp):
        print("COMFYUI_DOWN")
        return 1
    print(json.dumps(resp, indent=2))
    return 0


def cmd_queue(args):
    resp = _get("/queue")
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def cmd_post_workflow(args):
    try:
        with open(args.file, "r", encoding="utf-8") as f:
            workflow = json.load(f)
    except OSError as e:
        print(json.dumps({"error": "READ_FAILED", "detail": str(e)}))
        return 2
    except json.JSONDecodeError as e:
        print(json.dumps({"error": "BAD_WORKFLOW_JSON", "detail": str(e)}))
        return 2
    resp = _post_json("/prompt", {"prompt": workflow})
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def cmd_post_prompt(args):
    try:
        workflow = json.loads(args.workflow)
    except json.JSONDecodeError as e:
        print(json.dumps({"error": "BAD_WORKFLOW_JSON", "detail": str(e)}))
        return 2
    resp = _post_json("/prompt", {"prompt": workflow})
    print(json.dumps(resp, indent=2))
    return 0 if not _is_down(resp) else 1


def main():
    p = argparse.ArgumentParser(description="ComfyUI HTTP bridge")
    sub = p.add_subparsers(dest="cmd", required=True)

    sub.add_parser("health", help="Check ComfyUI reachability via /system_stats")
    sub.add_parser("queue", help="Inspect the ComfyUI queue")

    wf = sub.add_parser("post-workflow", help="POST a workflow JSON file to /prompt")
    wf.add_argument("--file", required=True, help="Local JSON file with the workflow")

    pp = sub.add_parser("post-prompt", help="POST inline workflow JSON to /prompt")
    pp.add_argument("--workflow", required=True, help="Workflow JSON string")

    args = p.parse_args()
    handlers = {
        "health": cmd_health,
        "queue": cmd_queue,
        "post-workflow": cmd_post_workflow,
        "post-prompt": cmd_post_prompt,
    }
    return handlers[args.cmd](args)


if __name__ == "__main__":
    sys.exit(main())
