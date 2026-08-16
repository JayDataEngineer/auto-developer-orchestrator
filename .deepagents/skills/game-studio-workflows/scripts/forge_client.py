#!/usr/bin/env python3
"""Forge client — HTTP wrapper for the Ray cluster's Forge generation API.

Usage:
  python3 forge_client.py health
  python3 forge_client.py generate "prompt text" --mode image --out images/x.png
  python3 forge_client.py generate "prompt" --mode image --params '{"seed":42,"width":1024,"height":1024}'

Env:
  MCP_HUB_ENDPOINT — Forge API base (default: http://localhost:30080).
                     Set this env var when the MCP hub is hosted on a remote
                     cluster or tailnet node.
"""
import argparse
import base64
import json
import os
import sys
import urllib.parse
import urllib.request

ENDPOINT = os.environ.get("MCP_HUB_ENDPOINT", "http://localhost:30080")
TIMEOUT = 180  # generation can take a while on cold GPU


def _post_json(url, payload, timeout=TIMEOUT):
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        body = resp.read().decode("utf-8")
        if resp.status != 200:
            raise RuntimeError(f"HTTP {resp.status}: {body}")
        return json.loads(body)


def _get(url, timeout=10):
    with urllib.request.urlopen(url, timeout=timeout) as resp:
        return resp.read()


def health():
    """Check Forge router health."""
    try:
        out = _get(f"{ENDPOINT}/forge/", timeout=5).decode("utf-8", errors="replace")
        print(out)
        return 0
    except Exception as e:
        print(f"FORGE DOWN: {e}", file=sys.stderr)
        return 1


def _save_output(output, out_path):
    """Persist Forge output to disk. Handles URL, base64, and dict shapes."""
    os.makedirs(os.path.dirname(out_path) or ".", exist_ok=True)

    # Case 1: output is a URL string
    if isinstance(output, str) and output.startswith(("http://", "https://")):
        data = _get(output, timeout=TIMEOUT)
        with open(out_path, "wb") as f:
            f.write(data)
        return f"saved {len(data)} bytes from URL"

    # Case 2: output is a base64 string
    if isinstance(output, str):
        try:
            data = base64.b64decode(output)
            with open(out_path, "wb") as f:
                f.write(data)
            return f"saved {len(data)} bytes from base64"
        except Exception:
            pass

    # Case 3: output is a dict with url or b64 field
    if isinstance(output, dict):
        for key in ("url", "image", "output", "data"):
            if key in output:
                return _save_output(output[key], out_path)

    # Fallback: write raw JSON
    with open(out_path, "w") as f:
        json.dump(output, f, indent=2)
    return f"saved raw JSON (unrecognized shape)"


def generate(prompt, mode="image", params=None, out_path=None):
    """Call Forge /generate. If out_path given, persist output there."""
    payload = {"mode": mode, "prompt": prompt}
    if params:
        if isinstance(params, str):
            payload["params"] = json.loads(params)
        else:
            payload["params"] = params

    print(f"FORGE request: mode={mode} prompt={prompt!r}", file=sys.stderr)
    result = _post_json(f"{ENDPOINT}/forge/generate", payload)

    if result.get("status") != "ok" and result.get("error"):
        print(f"FORGE ERROR: {result['error']}", file=sys.stderr)
        return 1

    output = result.get("output")
    if out_path and output:
        msg = _save_output(output, out_path)
        print(f"OK: {msg} → {out_path}")
    else:
        print(json.dumps(result, indent=2))
    return 0


def main():
    parser = argparse.ArgumentParser(description="Forge client")
    sub = parser.add_subparsers(dest="cmd", required=True)

    sub.add_parser("health", help="Check Forge router health")

    gen = sub.add_parser("generate", help="Generate via Forge")
    gen.add_argument("prompt", help="Generation prompt")
    gen.add_argument("--mode", default="image",
                     choices=["image", "3d", "music", "video"])
    gen.add_argument("--params", default=None,
                     help="JSON string of extra params (seed, width, height, etc.)")
    gen.add_argument("--out", default=None,
                     help="Output file path (image/video modes)")

    args = parser.parse_args()

    if args.cmd == "health":
        return health()
    if args.cmd == "generate":
        return generate(args.prompt, mode=args.mode,
                        params=args.params, out_path=args.out)
    return 2


if __name__ == "__main__":
    sys.exit(main())
