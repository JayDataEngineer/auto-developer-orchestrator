#!/usr/bin/env python3
"""Headless Godot test harness — the fallback when the MCP bridge is down.

When ``godot_client.py health`` returns ``GODOT_MCP_DOWN``, this script uses
the locally-bootstrapped Godot binary (see ``godot_bootstrap.py``) to run
headless operations directly — no editor bridge, no MCP server needed.

Usage:
  python3 godot_test.py version      # print the Godot version
  python3 godot_test.py syntax       # GDScript syntax check on all .gd files
  python3 godot_test.py import       # headless import (generates .godot/imported)
  python3 godot_test.py screenshot --scene res://scenes/main.tscn --out shot.png
  python3 godot_test.py validate     # headless scene validation
  python3 godot_test.py run --script res://tests/run_tests.gd

The binary is resolved by godot_bootstrap.py (PATH → cache → GitHub download).
"""
from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
BOOTSTRAP = SCRIPT_DIR / "godot_bootstrap.py"


def _godot_path() -> str | None:
    """Resolve the Godot binary via the bootstrap script."""
    result = subprocess.run(
        ["python3", str(BOOTSTRAP)],
        capture_output=True, text=True, timeout=300,
    )
    if result.returncode != 0:
        return None
    return result.stdout.strip() or None


def _run_godot(godot: str, args: list[str], timeout: int = 120) -> tuple[str, int]:
    """Run godot --headless with *args*. Returns (combined_output, exit_code)."""
    cmd = [godot, "--headless"] + args
    try:
        result = subprocess.run(
            cmd, capture_output=True, text=True, timeout=timeout,
            cwd=os.environ.get("GODOT_PROJECT_DIR", str(SCRIPT_DIR)),
        )
        output = (result.stdout + result.stderr).strip()
        return output, result.returncode
    except subprocess.TimeoutExpired:
        return f"TIMEOUT after {timeout}s", 1


def cmd_version(args: argparse.Namespace) -> int:
    godot = _godot_path()
    if not godot:
        print("GODOT_UNAVAILABLE — run godot_bootstrap.py first")
        return 1
    output, rc = _run_godot(godot, ["--version"])
    print(f"godot binary: {godot}")
    print(f"version: {output}")
    return rc


def cmd_syntax(args: argparse.Namespace) -> int:
    """Check GDScript syntax on all .gd files under the project."""
    godot = _godot_path()
    if not godot:
        print("GODOT_UNAVAILABLE")
        return 1

    project_dir = Path(os.environ.get("GODOT_PROJECT_DIR", str(SCRIPT_DIR)))
    gd_files = list(project_dir.rglob("*.gd"))
    if not gd_files:
        print("No .gd files found")
        return 0

    errors = 0
    for gd in sorted(gd_files):
        output, rc = _run_godot(godot, ["--check-gdscript", str(gd)], timeout=30)
        if rc != 0:
            print(f"FAIL {gd.relative_to(project_dir)}: {output[:200]}")
            errors += 1
        else:
            print(f"OK   {gd.relative_to(project_dir)}")

    print(f"\n{len(gd_files) - errors}/{len(gd_files)} scripts OK")
    return 1 if errors else 0


def cmd_import(args: argparse.Namespace) -> int:
    """Headless import — generates .godot/imported/ metadata."""
    godot = _godot_path()
    if not godot:
        print("GODOT_UNAVAILABLE")
        return 1
    output, rc = _run_godot(godot, ["--import"], timeout=180)
    print(output[:2000] if output else "(no output)")
    return rc


def cmd_screenshot(args: argparse.Namespace) -> int:
    """Render a scene headlessly and save a screenshot."""
    godot = _godot_path()
    if not godot:
        print("GODOT_UNAVAILABLE")
        return 1
    out_path = Path(args.out)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    output, rc = _run_godot(godot, [
        "--render-thread", "safe",
        str(args.scene),
        "--screenshot", str(out_path),
    ], timeout=120)
    if rc == 0 and out_path.is_file():
        print(f"Screenshot saved: {out_path} ({out_path.stat().st_size} bytes)")
    else:
        print(f"Screenshot failed (rc={rc}): {output[:300]}")
    return rc


def cmd_validate(args: argparse.Namespace) -> int:
    """Open the project headlessly to validate scene structure."""
    godot = _godot_path()
    if not godot:
        print("GODOT_UNAVAILABLE")
        return 1
    output, rc = _run_godot(godot, ["--editor", "--quit"], timeout=60)
    # --editor --quit opens the project, validates, then quits
    has_error = "SCRIPT ERROR" in output or "ERROR:" in output
    if has_error:
        print(f"Validation FAILED:\n{output[:1000]}")
        return 1
    print("Validation OK — no script errors")
    return 0


def cmd_run(args: argparse.Namespace) -> int:
    """Run a GDScript test suite headlessly."""
    godot = _godot_path()
    if not godot:
        print("GODOT_UNAVAILABLE")
        return 1
    output, rc = _run_godot(godot, ["-s", args.script], timeout=args.timeout)
    print(output[:3000] if output else "(no output)")
    return rc


def main() -> int:
    parser = argparse.ArgumentParser(description="Headless Godot test harness")
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("version", help="Print the Godot version")
    sub.add_parser("syntax", help="GDScript syntax check on all .gd files")
    sub.add_parser("import", help="Headless asset import")
    p_shot = sub.add_parser("screenshot", help="Render a scene and screenshot")
    p_shot.add_argument("--scene", required=True, help="Godot resource path (res://...)")
    p_shot.add_argument("--out", required=True, help="Output PNG path")
    sub.add_parser("validate", help="Headless project/scene validation")
    p_run = sub.add_parser("run", help="Run a GDScript test suite")
    p_run.add_argument("--script", required=True, help="Godot resource path to the test script")
    p_run.add_argument("--timeout", type=int, default=120)

    args = parser.parse_args()

    handlers = {
        "version": cmd_version,
        "syntax": cmd_syntax,
        "import": cmd_import,
        "screenshot": cmd_screenshot,
        "validate": cmd_validate,
        "run": cmd_run,
    }
    return handlers[args.command](args)


if __name__ == "__main__":
    sys.exit(main())
