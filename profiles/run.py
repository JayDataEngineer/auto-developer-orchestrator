#!/usr/bin/env python3
"""Profile launcher — dcode's own programmatic API, nothing else.

A profile is the repo's MCP lane (`profiles/<name>/.mcp.json`) plus its
default model. The launcher's whole job is opening a native dcode session
in a folder with that lane attached:

  0. loads the workspace `.env` through dcode's own dotenv loader
     (`config._load_dotenv` — the same function the CLI's bootstrap runs),
     anchored to the repo rather than the launch folder, so a session
     started from anywhere (dwork in any folder, make in the repo) still
     gets the workspace credentials (QWEN_API_KEY, the OPENAI_* bridge,
     GITHUB_*). Shell exports always win; `~/.deepagents/.env` fills gaps;
  1. cd's the process into the session folder (--cwd, or the launch
     folder). dcode's server derives its whole project context — roster,
     skills, AGENTS.md, `.mcp.json` — from the process CWD via git-root
     discovery, so this single chdir is the native seam; there is no
     ProjectContext override to set and none is needed;
  2. attaches the profile's MCP lane through the native `--mcp-config`
     seam (the same `mcp_config_path` the CLI's own flag uses), so the
     lane's servers — or, for coding, the lane's deliberate ZERO — ride
     into any folder;
  3. hands off to dcode's own entry points: run_textual_app (the TUI,
     with server_kwargs so the app launches the graph server itself and
     the TUI talks to a server-backed RemoteAgent — the approval-mode
     Store lives on that server) or run_non_interactive (-n, the headless
     E2E seam — the same server machinery).

Zero monkey patches, zero custom graph code: every seam below is a public
function of deepagents_code.
"""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
PROFILES_DIR = REPO / "profiles"


def _available_profiles() -> list[str]:
    return sorted(
        p.name
        for p in PROFILES_DIR.iterdir()
        if p.is_dir() and (p / ".mcp.json").is_file() and not p.name.startswith(("_", "."))
    )


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="profiles/run.py",
        description="Open a native dcode session with a profile's MCP lane.",
    )
    parser.add_argument("profile", help=f"profile name: {', '.join(_available_profiles())}")
    parser.add_argument("-M", "--model", default=None,
                        help="model spec 'provider:model' (default: dcode's own default)")
    parser.add_argument("-m", "--message", default=None,
                        help="initial prompt (interactive session)")
    parser.add_argument("-n", "--non-interactive", default=None, metavar="TEXT",
                        help="run one task headless and exit (dcode's run_non_interactive)")
    parser.add_argument("--no-mcp", action="store_true",
                        help="skip MCP server resolution for this session")
    parser.add_argument("--dry-run", action="store_true",
                        help="print the resolved session (roster, skills, MCP lane, model) and exit")
    parser.add_argument("--cwd", default=None,
                        help="session working directory (default: the launch folder). The "
                             "server derives its whole project context from this folder — "
                             "git-root discovery picks up the roster/skills/.mcp.json there — "
                             "and the profile's MCP lane rides along via --mcp-config.")
    return parser


# ── dry run ───────────────────────────────────────────────────────────────────


def _lane_mcp_servers(profile_root: Path) -> dict[str, object]:
    """The profile's declared MCP servers (empty for the zero-MCP lanes)."""
    try:
        return json.loads((profile_root / ".mcp.json").read_text()).get("mcpServers", {})
    except (OSError, json.JSONDecodeError):
        return {}


def _dry_run(profile: str, ctx, model: str) -> None:
    from deepagents_code.subagents import list_subagents

    roster = list_subagents(project_agents_dir=ctx.project_agents_dir())
    skills_dir = ctx.project_skills_dir()
    skills = sorted(p.name for p in skills_dir.iterdir()) if skills_dir else []

    print(f"profile      : {profile}")
    print(f"user_cwd     : {ctx.user_cwd}")
    print(f"project_root : {ctx.project_root or '(none)'}")
    print(f"model        : {model}")
    print(f"roster       : {len(roster)} subagents")
    for s in roster:
        tag = f" (model: {s['model']})" if s.get("model") else ""
        print(f"  - {s['name']}{tag}")
    print(f"skills       : {', '.join(skills) if skills else '(none)'}")

    lane = _lane_mcp_servers(PROFILES_DIR / profile)
    names = ", ".join(lane) if lane else "none — zero MCP"
    print(f"mcp servers  : {len(lane)} (lane: {names})")
    print(f"mcp tools    : (declared only — no server handshake in dry run)")
    print("(dry run — no server, no model)")


async def main() -> int:
    # Workspace .env first, before anything touches dcode's `settings`
    # (whose lazy bootstrap would anchor dotenv discovery to the process
    # CWD — the launch folder — and miss the repo when dwork runs elsewhere).
    # Same loader the CLI's bootstrap runs; override=False keeps shell
    # exports on top; missing file is a no-op.
    from deepagents_code.config import _load_dotenv as _load_workspace_dotenv

    _load_workspace_dotenv(start_path=REPO)

    args = _build_parser().parse_args()

    profile_root = PROFILES_DIR / args.profile
    if not (profile_root / ".mcp.json").is_file():
        print(f"unknown profile: {args.profile!r} "
              f"(available: {', '.join(_available_profiles())})", file=sys.stderr)
        return 2

    user_cwd = Path.cwd()
    if args.cwd:
        user_cwd = Path(args.cwd).expanduser().resolve()
        if not user_cwd.is_dir():
            print(f"bad --cwd: {user_cwd} is not a directory", file=sys.stderr)
            return 2

    from deepagents_code.config import _get_default_model_spec
    from deepagents_code.project_utils import ProjectContext

    model_spec = args.model or _get_default_model_spec()
    ctx = ProjectContext.from_user_cwd(user_cwd)  # native discovery — what a real session gets
    assistant_id = f"orchestrator-{args.profile}"
    # The lane — native --mcp-config seam. An empty lane (coding's deliberate
    # ZERO) means no config at all: dcode's own loader rejects an empty
    # 'mcpServers' (mcp_tools._validate_mcp_config_top_level), and "no config"
    # is exactly "zero MCP" natively.
    mcp_config_path = str(profile_root / ".mcp.json") if _lane_mcp_servers(profile_root) else None

    if args.dry_run:
        _dry_run(args.profile, ctx, model_spec)
        return 0

    # The server derives its project context (roster, skills, AGENTS.md,
    # .mcp.json) from the process CWD via git-root discovery — the native
    # seam. cd there, then hand off to dcode's own entry points.
    os.chdir(user_cwd)

    common = dict(
        assistant_id=assistant_id,
        model_name=model_spec,
        mcp_config_path=mcp_config_path,
        no_mcp=args.no_mcp,
    )

    if args.non_interactive is not None:
        from deepagents_code.client.non_interactive import run_non_interactive

        return await run_non_interactive(args.non_interactive, **common)

    from deepagents_code.app import run_textual_app

    result = await run_textual_app(
        assistant_id=assistant_id,
        backend=None,
        cwd=user_cwd,
        initial_prompt=args.message,
        title="dcode",
        sub_title=f"profile · {args.profile}",
        model_explicitly_set=args.model is not None,
        server_kwargs={
            **common,
            "interactive": True,
            "enable_ask_user": True,
        },
    )
    return int(getattr(result, "exit_code", 0) or 0)


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
