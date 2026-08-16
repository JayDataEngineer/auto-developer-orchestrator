"""The DISTRIBUTION surface — every org as a dcode plugin.

``emit_plugin`` / ``emit_marketplace`` emit ``<out>/plugins/<org>/`` (with
``.claude-plugin/plugin.json`` + ``skills/`` + ``.mcp.json``) and the catalog
at ``<out>/.agents/plugins/marketplace.json``. ``dcode plugin marketplace add
<out> && dcode plugin install <org>@pux-orgs`` then installs the org's
capabilities cross-project, enable/disable per session. Plugins carry
capabilities + skills ONLY — dcode ignores ``agents/`` inside plugins (its own
``_UNSUPPORTED_COMPONENT_DIRS``), so the roster stays a project layout
(``compiler.emit``); the plugin is the MCP-segmented surface.

Pure data + filesystem — no pux runtime, no Docker, no tokens.
"""
from __future__ import annotations

import json
import shutil
from pathlib import Path
from typing import Any

import yaml

from compiler.emit import _copy_skills
from profiles._paths import project_root as _default_project_root
from profiles.loaders import _org_path, discover_orgs
from protocol.mcp import _org_mcp_servers


def emit_plugin(
    org: str, *, project_root: Path | str | None = None, out: Path | str | None = None,
) -> dict[str, Any] | None:
    """Emit ONE org as a dcode plugin — the installable capability unit:
    ``<out>/plugins/<org>/`` with ``.claude-plugin/plugin.json``, ``skills/``,
    and ``.mcp.json``. Returns ``{"name", "skills", "mcp", "path"}``, or
    ``None`` when the org has nothing to distribute (no skills, no servers) —
    a roster-only org is a project layout (``compiler.emit``), not a plugin."""
    root = Path(project_root).resolve() if project_root is not None else _default_project_root()
    plugin = (Path(out) if out is not None else root).resolve() / "plugins" / org
    skills = _copy_skills(org, root, plugin / "skills")
    servers = _org_mcp_servers(org, root)
    if not skills and not servers:
        if plugin.exists():
            shutil.rmtree(plugin)
        return None
    manifest: dict[str, Any] = {"name": org}
    if skills:
        manifest["skills"] = "./skills"
    if servers:
        manifest["mcpServers"] = "./.mcp.json"
    (plugin / ".claude-plugin").mkdir(parents=True, exist_ok=True)
    (plugin / ".claude-plugin" / "plugin.json").write_text(
        json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    if servers:
        (plugin / ".mcp.json").write_text(
            json.dumps({"mcpServers": servers}, indent=2) + "\n", encoding="utf-8")
    return {"name": org, "skills": sorted(skills), "mcp": sorted(servers), "path": str(plugin)}


MARKETPLACE_NAME = "pux-orgs"


def emit_marketplace(
    orgs: list[str] | None = None, *,
    project_root: Path | str | None = None, out: Path | str | None = None,
) -> dict[str, Any]:
    """Emit every org (or the given ones) as plugins + the dcode marketplace
    catalog at ``<out>/.agents/plugins/marketplace.json`` — ``dcode plugin
    marketplace add <out> && dcode plugin install <org>@pux-orgs`` installs
    the org's capability surface cross-project. Empty orgs are skipped; an
    existing catalog is MERGED (emitted orgs win, foreign entries preserved).
    Returns ``{"marketplace", "plugins", "out"}``."""
    root = Path(project_root).resolve() if project_root is not None else _default_project_root()
    target = (Path(out) if out is not None else root).resolve()
    if not (root / "profiles").is_dir():
        raise FileNotFoundError(f"no profiles/ tree under {root}")
    # the SAME discovery the workspace uses (top-level + specialists/ nesting);
    # underscore-prefixed orgs (_demo, like _shared) are internal, not shipped
    wanted = [o for o in (orgs or discover_orgs(root)) if not o.startswith("_")]
    entries: list[dict[str, Any]] = []
    for org in wanted:
        if emit_plugin(org, project_root=root, out=target) is None:
            continue
        data = yaml.safe_load((_org_path(org, root) / "org.yaml").read_text()) or {}
        entry: dict[str, Any] = {"name": org, "source": f"./plugins/{org}"}
        if isinstance(data.get("description"), str):
            entry["description"] = data["description"]
        entries.append(entry)

    mpath = target / ".agents" / "plugins" / "marketplace.json"
    catalog: dict[str, Any] = {}
    if mpath.is_file():
        try:
            catalog = json.loads(mpath.read_text()) or {}
        except json.JSONDecodeError:
            catalog = {}
    # OUR generated entries (source ./plugins/*) are REPLACED — the catalog is
    # regenerable, so a removed/renamed org leaves no stale entry. Entries with
    # any other source are foreign (operator-owned) and preserved.
    by_name = {
        e.get("name"): e for e in catalog.get("plugins") or []
        if isinstance(e, dict) and not str(e.get("source", "")).startswith("./plugins/")
    }
    by_name.update({e["name"]: e for e in entries})
    mpath.parent.mkdir(parents=True, exist_ok=True)
    mpath.write_text(
        json.dumps({"name": MARKETPLACE_NAME, "plugins": list(by_name.values())}, indent=2) + "\n",
        encoding="utf-8")
    return {"marketplace": MARKETPLACE_NAME, "plugins": [e["name"] for e in entries],
            "out": str(target)}
