"""The workspace compiler CLI — ``profiles/`` → dcode-native surface.

``pux sync`` emits the UNION dcode surface (``.deepagents/`` + ``.mcp.json``)
at the project root — the checked-in workspace layout. ``pux sync --check`` /
``pux check`` fail loud on drift from the checked-in surface. ``pux compile``
emits ONE org's dcode layout (or the plugin marketplace) into a staging dir.
"""
from __future__ import annotations

import argparse
import sys

from compiler.bootstrap import bootstrap_env_and_logging
from compiler.emit import check_sync, emit_dcode, emit_union
from plugins.marketplace import emit_marketplace


def _parse() -> argparse.ArgumentParser:
    ap = argparse.ArgumentParser(
        prog="pux",
        description="profiles/ → dcode-native surface (the workspace compiler).",
    )
    sub = ap.add_subparsers(dest="cmd", required=True)

    p_sync = sub.add_parser(
        "sync", help="emit the union dcode surface at the project root")
    p_sync.add_argument(
        "--check", action="store_true",
        help="drift-check the checked-in surface instead of emitting (exit 1 on drift)")
    p_sync.add_argument(
        "--project-root", default=None,
        help="project root (default: $PUX_PROJECT_ROOT or cwd)")
    p_sync.add_argument(
        "--out", default=None,
        help="emit target (default: the project root)")

    p_check = sub.add_parser(
        "check", help="alias for `sync --check`")
    p_check.add_argument("--project-root", default=None)
    p_check.add_argument("--out", default=None)

    p_compile = sub.add_parser(
        "compile", help="emit one org's dcode layout, or the plugin marketplace")
    p_compile.add_argument("--org", default=None, help="org to emit (default: all orgs)")
    p_compile.add_argument(
        "--marketplace", action="store_true",
        help="emit every org as a dcode plugin + the marketplace catalog")
    p_compile.add_argument("--project-root", default=None)
    p_compile.add_argument("--out", default=None, help="staging dir (default: project root)")
    return ap


def _print_summary(label: str, summary: dict) -> None:
    agents = summary.get("agents") or []
    skills = summary.get("skills") or []
    mcp = summary.get("mcp") or []
    print(f"{label}: {len(agents)} agents, {len(skills)} skills, "
          f"{len(mcp)} mcp servers -> {summary.get('out')}")
    for name in agents:
        print(f"  agent {name}")
    for name in skills:
        print(f"  skill {name}")
    for name in mcp:
        print(f"  mcp {name}")


def _do_check(project_root: str | None, out: str | None) -> None:
    result = check_sync(project_root=project_root, out=out)
    for kind in ("drifted", "missing", "stale"):
        for path in result[kind]:
            print(f"{kind}: {path}", file=sys.stderr)
    for name in result["mcp_drift"]:
        print(f"mcp_drift: server {name!r}", file=sys.stderr)
    if not result["ok"]:
        print("sync --check: DRIFT — the checked-in surface differs from the "
              "compiler output; run `pux sync` to regenerate", file=sys.stderr)
        raise SystemExit(1)
    print("sync --check: the checked-in surface matches the compiler output")


def main() -> None:
    bootstrap_env_and_logging()
    args = _parse().parse_args()
    if args.cmd in ("sync", "check"):
        if args.cmd == "sync" and args.check:
            _do_check(args.project_root, args.out)
        elif args.cmd == "check":
            _do_check(args.project_root, args.out)
        else:
            summary = emit_union(project_root=args.project_root, out=args.out)
            _print_summary("sync", summary)
        return
    if args.cmd == "compile":
        if args.marketplace:
            summary = emit_marketplace(project_root=args.project_root, out=args.out)
            _print_summary("marketplace", summary)
        else:
            summary = emit_dcode(args.org, project_root=args.project_root, out=args.out)
            _print_summary("compile", summary)
        return
    raise SystemExit(f"unknown command: {args.cmd}")


if __name__ == "__main__":
    main()
