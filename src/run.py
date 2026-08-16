"""The dcode launch — org tree → native deepagents graph → dcode's own TUI.

``build_org_agent`` composes the SAME machinery dcode's CLI uses: the model
default is dcode's own ``_get_default_model_spec()`` (the operator's
``[models].default`` config), the org's MCP servers load through dcode's own
``resolve_and_load_mcp_tools``, the roster projects onto native ``SubAgent``
dicts with per-subagent tools + middleware, and the graph is built with
``create_deep_agent``. ``launch`` hands that graph to ``run_textual_app`` —
dcode's app loop, the same TUI you get from a bare ``dcode`` run.

No monkey patches: every piece here is a function dcode itself calls, with
the org tree projected onto the native surface. ``plan`` is the pure dry-run
(no MCP load, no model, no sandbox gateway).
"""
from __future__ import annotations

import asyncio
import json
import tempfile
from pathlib import Path
from typing import Any, cast

from deepagents.backends import LocalShellBackend
from deepagents.graph import create_deep_agent
from deepagents_code.app import run_textual_app
from deepagents_code.config import _get_default_model_spec
from deepagents_code.mcp_tools import MCPServerInfo, resolve_and_load_mcp_tools
from langchain_core.language_models import BaseChatModel

from profiles._paths import project_root as _default_project_root
from profiles.loaders import build_system_prompt
from profiles.subagents import org_subagent_specs
from protocol.mcp import _org_mcp_servers
from sandbox.local import local_backend

# The org's temp ``.mcp.json`` is only read at load time, but keep it for the
# process lifetime anyway — never risk a lazy re-read of a vanished file.
_KEEP_ALIVE: list[tempfile.TemporaryDirectory[str]] = []


def _tools_by_server(
    all_tools: list[Any], infos: list[MCPServerInfo],
) -> dict[str, list[Any]]:
    """Group the loaded (stateless, ``{server}_{tool}``-prefixed) tools by their
    server, using the per-server tool names dcode recorded on each
    ``MCPServerInfo``."""
    by_name = {t.name: t for t in all_tools}
    out: dict[str, list[Any]] = {}
    for info in infos:
        names = [ti.name for ti in info.tools]
        out[info.name] = [by_name[n] for n in names if n in by_name]
    return out


async def _load_mcp(org: str, root: Path) -> tuple[list[Any], dict[str, list[Any]]]:
    """Load the org's declared MCP servers through dcode's own loader.

    The org's ``capabilities:`` mcp refs are written to a temp ``.mcp.json``
    and passed as ``explicit_config_path`` — an explicit config loads with
    full trust, no project-discovery, no approval prompts. Fail-loud: every
    declared server must come back with status ``ok``; a skipped server
    (config error, unauthenticated, disabled) raises with its status + error
    instead of silently shrinking the org's capability surface.
    """
    servers = _org_mcp_servers(org, root)
    if not servers:
        return [], {}
    keeper = tempfile.TemporaryDirectory(prefix="pux-dcode-")
    _KEEP_ALIVE.append(keeper)
    cfg = Path(keeper.name) / ".mcp.json"
    cfg.write_text(json.dumps({"mcpServers": servers}, indent=2), encoding="utf-8")
    tools, _, infos = await resolve_and_load_mcp_tools(
        explicit_config_path=str(cfg),
        no_mcp=False,
        trust_project_mcp=False,
        project_context=None,
        additional_configs=(),
        stateless=True,
        session_manager=None,
    )
    by_name = {info.name: info for info in infos}
    for name in servers:
        info = by_name.get(name)
        if info is None or info.status != "ok":
            detail = f"({info.status}: {info.error})" if info is not None else "(not resolved)"
            raise RuntimeError(
                f"org {org!r}: declared MCP server {name!r} did not load {detail} — "
                "the org's capability surface is reduced; fix or drop the "
                "capability before launching"
            )
    return tools, _tools_by_server(tools, infos)


async def build_org_agent(
    org: str, *,
    project_root: Path | str | None = None,
    model: str | BaseChatModel | None = None,
    cwd: Path | str | None = None,
    load_mcp: bool = True,
) -> tuple[Any, LocalShellBackend]:
    """The org as a native deepagents graph — the object dcode's TUI runs.

    Base-agent tools = the org's MCP servers (the shared capability surface);
    each subagent additionally carries its declared tools + middleware
    natively via ``SubAgent``. ``model`` defaults to dcode's own default
    (``_get_default_model_spec``). Returns ``(agent, backend)``.
    """
    root = Path(project_root).resolve() if project_root is not None else _default_project_root()
    resolved_model = model or _get_default_model_spec()
    backend = local_backend(cwd=cwd)
    base_tools: list[Any] = []
    by_server: dict[str, list[Any]] = {}
    if load_mcp:
        base_tools, by_server = await _load_mcp(org, root)
    subagents = org_subagent_specs(
        org, project_root=root, mcp_tools_by_server=by_server, model=resolved_model,
    )
    agent = create_deep_agent(
        model=resolved_model,
        system_prompt=build_system_prompt(org, project_root=root),
        tools=base_tools,
        subagents=subagents or None,
        backend=backend,
        name=f"pux-{org}",
    )
    return agent, backend


async def launch(
    org: str, *,
    project_root: Path | str | None = None,
    model: str | None = None,
    cwd: Path | str | None = None,
) -> dict[str, Any]:
    """Build the org's graph and run it inside dcode's own TUI.

    Returns ``{"org", "return_code", "thread_id"}`` after the app exits.
    """
    agent, backend = await build_org_agent(
        org, project_root=project_root, model=model, cwd=cwd)
    result = await run_textual_app(
        agent=agent,
        # run_textual_app annotates CompositeBackend | None, but dcode's own
        # create_cli_agent (deepagents_code/agent.py) builds a bare
        # LocalShellBackend and threads it into the same param — runtime-valid.
        backend=backend,  # type: ignore[arg-type]
        assistant_id=f"pux-{org}",
        cwd=str(Path(cwd).resolve()) if cwd is not None else None,
    )
    return {
        "org": org,
        "return_code": result.return_code,
        "thread_id": result.thread_id,
    }


def plan(org: str, *, project_root: Path | str | None = None) -> dict[str, Any]:
    """Dry-run — the full profile with NO MCP loading, NO model, NO sandbox
    gateway.

    Resolves every subagent's declared tools/middleware against the registry
    (tool construction uses a throwaway ``LocalShellBackend`` so the sandbox
    gateway is never touched) and validates ``mcp:`` refs against the org's
    DECLARED servers — a ref that names no declared server raises here,
    before anything runs. Reports the shape the launch would build.
    """
    root = Path(project_root).resolve() if project_root is not None else _default_project_root()
    servers = _org_mcp_servers(org, root)
    declared_only: dict[str, list[Any]] = {name: [] for name in servers}
    subagents = org_subagent_specs(
        org, project_root=root, mcp_tools_by_server=declared_only,
        model="<default>",
        sandbox=LocalShellBackend(),  # type: ignore[arg-type]  # LocalShellBackend implements the sandbox protocol; deepagents' BaseSandbox typing is loose (its own CLI passes LocalShellBackend)
    )
    return {
        "org": org,
        "model_default": _get_default_model_spec(),
        "mcp_servers": sorted(servers),
        "subagents": [
            {
                "name": s["name"],
                "tools": [t.name for t in cast(Any, s.get("tools", []))],
                "middleware": [type(m).__name__ for m in s.get("middleware", [])],
            }
            for s in subagents
        ],
    }


def _main() -> int:
    """The launch entrypoint: an org profile → dcode's own TUI. Everything is
    dcode's native surface (``create_deep_agent`` + ``run_textual_app``); this
    script only picks the org and hands it over."""
    import argparse

    ap = argparse.ArgumentParser(
        prog="pux-run",
        description="Launch an org profile in dcode's own TUI "
                    "(create_deep_agent + run_textual_app).",
    )
    ap.add_argument("--org", default="general",
                    help="profile/org to launch (default: general)")
    ap.add_argument("--model", default=None,
                    help="model override (default: dcode's configured default)")
    ap.add_argument("--cwd", default=None, help="backend working directory")
    ap.add_argument("--project-root", default=None,
                    help="project root (default: $PUX_PROJECT_ROOT or cwd)")
    ap.add_argument("--dry-run", action="store_true",
                    help="print the launch plan and exit (no MCP load, no model)")
    args = ap.parse_args()

    if args.dry_run:
        info = plan(args.org, project_root=args.project_root)
        print(f"org: {info['org']}")
        print(f"model default: {info['model_default']}")
        print("mcp servers: " + (", ".join(info["mcp_servers"]) or "(none)"))
        for s in info["subagents"]:
            tools = ", ".join(s["tools"]) or "(none)"
            middleware = ", ".join(s["middleware"]) or "(none)"
            print(f"  subagent {s['name']}: tools=[{tools}] middleware=[{middleware}]")
        return 0

    result = asyncio.run(launch(
        args.org, project_root=args.project_root, model=args.model, cwd=args.cwd,
    ))
    return int(result["return_code"] or 0)


if __name__ == "__main__":
    raise SystemExit(_main())
