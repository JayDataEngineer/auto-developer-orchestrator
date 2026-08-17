#!/usr/bin/env python3
"""Profile launcher — dcode's own programmatic API, nothing else.

A profile is a dcode project root (`profiles/<name>`) carrying a scoped
roster (`.deepagents/agents/`, symlinked to the authored union), a persona
(`.deepagents/AGENTS.md`), skills (`.deepagents/skills/`) and a scoped MCP
set (`.mcp.json`). This launcher:

  1. builds a ProjectContext(user_cwd=--cwd or repo, project_root=profiles/<name>)
     — the explicit constructor, because git-root discovery would otherwise
     scope the session to the whole repo; --cwd moves where the session
     starts and the shell runs, the workspace (roster/skills/MCP) stays the
     profile;
  2. gates the profile's project MCP servers through dcode's native trust
     store (`load_mcp_server_trust_lists` / `add_enabled_project_mcp_servers`)
     with a terminal prompt on first launch;
  3. resolves that profile's MCP servers through dcode's own pipeline;
  4. calls create_cli_agent(...) — dcode's public entry point;
  5. runs run_textual_app(...) — dcode's own TUI.

Zero monkey patches, zero custom graph code: every seam below is a public
function of deepagents_code (the two model_config trust helpers are the same
ones the CLI's own approval flow reads and writes).
"""
from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
PROFILES_DIR = REPO / "profiles"


def _available_profiles() -> list[str]:
    return sorted(
        p.name
        for p in PROFILES_DIR.iterdir()
        if p.is_dir() and (p / ".deepagents" / "agents").is_dir() and not p.name.startswith(("_", "."))
    )


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="profiles/run.py",
        description="Launch a scoped dcode profile (native API only).",
    )
    parser.add_argument("profile", help=f"profile name: {', '.join(_available_profiles())}")
    parser.add_argument("-M", "--model", default=None,
                        help="model spec 'provider:model' (default: dcode's own default)")
    parser.add_argument("-m", "--message", default=None,
                        help="initial prompt (interactive session)")
    parser.add_argument("--no-mcp", action="store_true",
                        help="skip MCP server resolution for this session")
    parser.add_argument("--dry-run", action="store_true",
                        help="print the resolved profile (roster, skills, MCP, model) and exit")
    parser.add_argument("--cwd", default=None,
                        help="session working directory (default: the repo). The profile's "
                             "workspace — roster, skills, MCP scoping — stays the repo; only "
                             "where the session starts and the shell runs changes.")
    return parser


# ── native MCP trust gate ─────────────────────────────────────────────────────
# Mirrors the CLI's own first-run approval: scoped approvals live in the
# user-level config.toml keyed by project root + server fingerprint, so a
# committed .mcp.json can never self-approve. Denied -> only already-approved
# servers load; session-only -> whole-config trust for this resolve call
# (exactly what the CLI passes for "allow once").

_PENDING = object()  # sentinel: unresolved servers exist


def _pending_mcp_servers(profile_root: Path) -> list[str] | object:
    """Server names still needing trust, or _PENDING sentinel on read error."""
    from deepagents_code.model_config import load_mcp_server_trust_lists

    servers: dict[str, object] = {}
    mcp_file = profile_root / ".mcp.json"
    if mcp_file.is_file():
        try:
            servers = json.loads(mcp_file.read_text()).get("mcpServers", {})
        except (OSError, json.JSONDecodeError):
            servers = {}
    trust_lists = load_mcp_server_trust_lists()
    if trust_lists.read_error is not None:
        return _PENDING  # fail closed
    return [
        name
        for name, cfg in servers.items()
        if name not in trust_lists.disabled
        and not trust_lists.is_enabled(name, project_root=profile_root, server=cfg)
    ]


def _gate_mcp_trust(profile_root: Path) -> bool | None:
    """Prompt about unapproved profile servers; return the trust flag.

    True  -> trust the whole profile config this session
    False -> trust nothing beyond already-approved rows
    None  -> nothing to decide (all approved / no servers)
    """
    from deepagents_code.model_config import add_enabled_project_mcp_servers

    pending = _pending_mcp_servers(profile_root)
    if pending is _PENDING:
        print("warning: user trust policy unreadable — treating profile MCP "
              "servers as untrusted", file=sys.stderr)
        return False
    if not pending:
        return None

    print(f"\nApprove profile MCP servers ({profile_root.name}):")
    for name in pending:
        print(f'  "{name}"')
    try:
        answer = input("[a]lways / [s]ession / [N]ever (default N): ").strip().lower()
    except (EOFError, KeyboardInterrupt):
        print()
        return False
    if answer in ("a", "always"):
        try:
            servers = json.loads((profile_root / ".mcp.json").read_text())["mcpServers"]
            add_enabled_project_mcp_servers(
                pending, project_root=profile_root, server_configs=servers,
            )
            print(f"remembered {len(pending)} servers for this profile "
                  "(~/.deepagents/config.toml)")
        except Exception as exc:  # persisted choice failed — session still trusted
            print(f"warning: could not persist approval ({exc})", file=sys.stderr)
        return True
    if answer in ("s", "session"):
        print(f"allowing {len(pending)} servers for this session only")
        return True
    print(f"denied {len(pending)} servers (already-approved ones still load)")
    return False


# ── dry run ───────────────────────────────────────────────────────────────────


def _dry_run(profile: str, ctx, model: str, mcp_tools, server_infos) -> None:
    from deepagents_code.subagents import list_subagents

    roster = list_subagents(project_agents_dir=ctx.project_agents_dir())
    skills_dir = ctx.project_skills_dir()
    skills = sorted(p.name for p in skills_dir.iterdir()) if skills_dir else []

    print(f"profile      : {profile}")
    print(f"project_root : {ctx.project_root}")
    print(f"user_cwd     : {ctx.user_cwd}")
    print(f"model        : {model}")
    print(f"roster       : {len(roster)} subagents")
    for s in roster:
        tag = f" (model: {s['model']})" if s.get("model") else ""
        print(f"  - {s['name']}{tag}")
    print(f"skills       : {', '.join(skills) if skills else '(none)'}")

    pending = _pending_mcp_servers(ctx.project_root)
    if pending is _PENDING:
        print("mcp trust    : UNREADABLE (fails closed)")
    elif pending:
        print(f"mcp trust    : {len(pending)} pending → prompts on first launch "
              f"({', '.join(pending)})")
    print(f"mcp servers  : {len(server_infos)}")
    for info in server_infos:
        print(f"  - {info.name}: {len(info.tools)} tools")
    print(f"mcp tools    : {len(mcp_tools)}")
    print("(dry run — no agent, no TUI)")


async def main() -> int:
    args = _build_parser().parse_args()

    profile_root = PROFILES_DIR / args.profile
    if not (profile_root / ".deepagents" / "agents").is_dir():
        print(f"unknown profile: {args.profile!r} "
              f"(available: {', '.join(_available_profiles())})", file=sys.stderr)
        return 2

    user_cwd = REPO
    if args.cwd:
        user_cwd = Path(args.cwd).expanduser().resolve()
        if not user_cwd.is_dir():
            print(f"bad --cwd: {user_cwd} is not a directory", file=sys.stderr)
            return 2

    # ── native imports (public seams only) ────────────────────────────────────
    from deepagents_code.agent import create_cli_agent
    from deepagents_code.app import run_textual_app
    from deepagents_code.config import _get_default_model_spec, create_model
    from deepagents_code.mcp_tools import resolve_and_load_mcp_tools
    from deepagents_code.project_utils import ProjectContext

    ctx = ProjectContext(user_cwd=user_cwd, project_root=profile_root)
    model_spec = args.model or _get_default_model_spec()
    # create_model resolves custom class_path providers from config.toml
    # (a raw 'provider:model' string would only cover langchain-known ones)
    model_result = create_model(model_spec)
    model_result.apply_to_settings()
    assistant_id = f"orchestrator-{args.profile}"

    trust = None if (args.no_mcp or args.dry_run) else _gate_mcp_trust(profile_root)
    mcp_tools, session_manager, server_infos = await resolve_and_load_mcp_tools(
        project_context=ctx, no_mcp=args.no_mcp, trust_project_mcp=trust,
    )

    if args.dry_run:
        _dry_run(args.profile, ctx, model_spec, mcp_tools, server_infos)
        return 0

    agent, backend = create_cli_agent(
        model=model_result.model,
        assistant_id=assistant_id,
        tools=mcp_tools,
        mcp_tools=mcp_tools,
        mcp_server_info=server_infos,
        project_context=ctx,
        cwd=user_cwd,
    )

    result = await run_textual_app(
        agent=agent,
        backend=backend,
        assistant_id=assistant_id,
        cwd=user_cwd,
        initial_prompt=args.message,
        title="dcode",
        sub_title=f"profile · {args.profile}",
        model_explicitly_set=args.model is not None,
    )
    return int(getattr(result, "exit_code", 0) or 0)


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
