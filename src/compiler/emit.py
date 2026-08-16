"""Emit dcode-native artifacts from the profiles tree — the workspace compiler.

One source of truth (``profiles/``) drives the dcode-native surface. Every
capability is standardized on the MCP boundary, the catalog at
``profiles/_shared/tool_servers.yaml`` is that registry, and this compiler
projects it into the standard ``.mcp.json`` dcode already reads.

``emit_dcode`` writes the three dcode-discovered surfaces for one org:

* ``<out>/.deepagents/agents/<name>/AGENTS.md`` — every rostered agent,
  materialized (``extends:`` chains resolved) in dcode's frontmatter+body
  format. pux-only frontmatter (``tools`` / ``middleware`` / ``rubric`` /
  ``skills``) is dropped: dcode's FILE loader reads only ``name`` /
  ``description`` / ``model``. Per-subagent tools/middleware are NOT lost
  here — they ride the PROGRAMMATIC surface (``src/run.py``: ``SubAgent``
  ``tools`` / ``middleware`` on ``create_deep_agent``), which is the
  segmentation this layout feeds; the emitted layout is the file-based /
  plugin path, where segmentation is the MCP server boundary.
* ``<out>/.deepagents/skills/<skill>/`` — the supervisor skill roots
  (``profiles/_shared/skills`` + the org's own), copied so dcode's
  project-level skills discovery finds them.
* ``<out>/.mcp.json`` — the org's declared ``capabilities:`` (``kind: mcp``),
  resolved against the shared catalog into the standard ``mcpServers`` schema.
  ``${VAR}`` placeholders pass through VERBATIM — dcode interpolates them from
  the operator env at activation, the same convention the catalog uses. An
  existing ``.mcp.json`` is MERGED (org refs win; foreign entries preserved).

``emit_union`` writes the same three surfaces for the WHOLE workspace at once
— the checked-in layout at the project root, with every org's roster and
capabilities merged into one dcode-discovered surface (name-keyed; sorted org
order, later org wins on collisions).

Pure data + filesystem — no pux runtime, no Docker, no tokens.
"""
from __future__ import annotations

import hashlib
import json
import shutil
import tempfile
from pathlib import Path
from typing import Any

import yaml

from profiles._paths import project_root as _default_project_root
from profiles.loaders import (
    _load_agent_spec,
    discover_orgs,
    org_agent_slugs,
    supervisor_skills_roots,
)
from protocol.mcp import _org_mcp_servers


def _agents_md(spec: dict[str, Any]) -> str:
    """Materialize one agent spec as a dcode ``AGENTS.md`` (frontmatter
    ``name``/``description`` (+ ``model`` when declared) + the merged body)."""
    fm = {
        "name": spec.get("name"),
        "description": spec.get("description"),
    }
    model = spec.get("model")
    if isinstance(model, str) and model.strip():
        fm["model"] = model.strip()
    head = yaml.safe_dump(
        {k: v for k, v in fm.items() if v},
        sort_keys=False, allow_unicode=True, default_flow_style=False,
    ).strip()
    return f"---\n{head}\n---\n\n{str(spec.get('system_prompt', '')).strip()}\n"


def _copy_skills(org: str, root: Path, base: Path) -> list[str]:
    """Copy the supervisor skill roots into ``base`` (``<base>/<skill>/``),
    returning the copied names. ``base`` is created lazily — an org with no
    skills leaves no empty dir behind."""
    names: list[str] = []
    for skills_root in supervisor_skills_roots(org, root):
        for skill in sorted(Path(skills_root).iterdir()):
            if not (skill / "SKILL.md").is_file():
                continue
            dst = base / skill.name
            if dst.exists():
                shutil.rmtree(dst)
            shutil.copytree(skill, dst)
            names.append(skill.name)
    return names


def emit_dcode(
    org: str, *, project_root: Path | str | None = None, out: Path | str | None = None,
) -> dict[str, Any]:
    """Emit the dcode-native layout for ``org``. ``out`` defaults to the project
    root (``dcode`` is then run there); point it at a staging dir to keep the
    repo clean. Returns ``{"agents": [...], "skills": [...], "mcp": [...],
    "out": str}`` — the summary the CLI prints."""
    root = Path(project_root).resolve() if project_root is not None else _default_project_root()
    target = (Path(out) if out is not None else root).resolve()
    written: dict[str, Any] = {"agents": [], "skills": [], "mcp": [], "out": str(target)}

    for slug in org_agent_slugs(org, root):
        spec = _load_agent_spec(slug, org, root)
        if spec is None:
            msg = f"no agent {slug!r} for org {org!r}"
            raise FileNotFoundError(msg)
        name = str(spec.get("name") or slug)
        d = target / ".deepagents" / "agents" / name
        d.mkdir(parents=True, exist_ok=True)
        (d / "AGENTS.md").write_text(_agents_md(spec), encoding="utf-8")
        written["agents"].append(name)

    written["skills"] = _copy_skills(org, root, target / ".deepagents" / "skills")

    servers = _org_mcp_servers(org, root)
    if servers:
        cfg_path = target / ".mcp.json"
        cfg: dict[str, Any] = {}
        if cfg_path.is_file():
            try:
                cfg = json.loads(cfg_path.read_text()) or {}
            except json.JSONDecodeError:
                cfg = {}
        merged = {**(cfg.get("mcpServers") or {}), **servers}
        cfg_path.write_text(json.dumps({"mcpServers": merged}, indent=2) + "\n")
        written["mcp"] = sorted(servers)
    return written


def emit_union(
    *, project_root: Path | str | None = None, out: Path | str | None = None,
) -> dict[str, Any]:
    """Emit the UNION dcode surface across every non-underscore org — the
    checked-in workspace layout at the project root. Same three surfaces as
    ``emit_dcode``, but for the whole tree at once:

    * agents — every rostered agent across all orgs, name-keyed (shared
      agents like ``researcher`` emit identically from each org and dedupe).
      Sorted org order makes the merge deterministic; a later org wins on a
      name collision.
    * skills — the union of every org's supervisor skill roots.
    * ``.mcp.json`` — the merged ``mcpServers`` of every org's declared
      servers (foreign entries in an existing file preserved, as in
      ``emit_dcode``).

    Returns ``{"agents", "skills", "mcp", "out"}``."""
    root = Path(project_root).resolve() if project_root is not None else _default_project_root()
    target = (Path(out) if out is not None else root).resolve()
    orgs = sorted(o for o in discover_orgs(root) if not o.startswith("_"))
    if not orgs:
        raise FileNotFoundError(f"no profiles/ tree under {root}")

    agents: dict[str, dict[str, Any]] = {}
    skills: list[str] = []
    servers: dict[str, Any] = {}
    for org in orgs:
        for slug in org_agent_slugs(org, root):
            spec = _load_agent_spec(slug, org, root)
            if spec is None:
                msg = f"no agent {slug!r} for org {org!r}"
                raise FileNotFoundError(msg)
            name = str(spec.get("name") or slug)
            agents[name] = spec  # sorted org order -> deterministic, later org wins
        skills += _copy_skills(org, root, target / ".deepagents" / "skills")
        servers.update(_org_mcp_servers(org, root))

    for name, spec in agents.items():
        d = target / ".deepagents" / "agents" / name
        d.mkdir(parents=True, exist_ok=True)
        (d / "AGENTS.md").write_text(_agents_md(spec), encoding="utf-8")

    if servers:
        cfg_path = target / ".mcp.json"
        cfg: dict[str, Any] = {}
        if cfg_path.is_file():
            try:
                cfg = json.loads(cfg_path.read_text()) or {}
            except json.JSONDecodeError:
                cfg = {}
        merged = {**(cfg.get("mcpServers") or {}), **servers}
        cfg_path.write_text(json.dumps({"mcpServers": merged}, indent=2) + "\n")

    return {
        "agents": sorted(agents), "skills": sorted(set(skills)),
        "mcp": sorted(servers), "out": str(target),
    }


def _tree_map(root: Path) -> dict[str, str]:
    """``{rel_path: sha256}`` for every file under ``root`` (empty when the
    dir does not exist)."""
    out: dict[str, str] = {}
    if not root.is_dir():
        return out
    for p in sorted(root.rglob("*")):
        if p.is_file():
            out[str(p.relative_to(root))] = hashlib.sha256(p.read_bytes()).hexdigest()
    return out


def check_sync(
    *, project_root: Path | str | None = None, out: Path | str | None = None,
) -> dict[str, Any]:
    """Drift check for the checked-in union surface (``pux sync --check``).

    Emits the union into a temp dir, then compares against ``out`` (default:
    the project root):

    * ``.deepagents/`` — BYTE compare (sha256 per file, BOTH directions): the
      emitted surface IS the checked-in tree, so a file only in the target is
      ``stale``, a file only in the emission is ``missing``, a differing file
      is ``drifted``.
    * ``.mcp.json`` — STRUCTURAL compare: every emitted server must be present
      in the target with an equal config; foreign entries (preserved by the
      merge, e.g. an operator-owned bridge) are allowed.

    Returns ``{"drifted", "missing", "stale", "mcp_drift", "ok"}``.
    """
    root = Path(project_root).resolve() if project_root is not None else _default_project_root()
    target = (Path(out) if out is not None else root).resolve()

    with tempfile.TemporaryDirectory(prefix="pux-sync-") as td:
        emitted_dir = Path(td) / "emitted"
        emit_union(project_root=root, out=emitted_dir)
        em = _tree_map(emitted_dir / ".deepagents")
        tgt = _tree_map(target / ".deepagents")
        drifted = sorted(p for p in em if tgt.get(p) != em[p])
        missing = sorted(set(em) - set(tgt))
        stale = sorted(set(tgt) - set(em))
        mcp_drift: list[str] = []
        mc = emitted_dir / ".mcp.json"
        if mc.is_file():
            e_servers = (json.loads(mc.read_text()) or {}).get("mcpServers") or {}
            tc = target / ".mcp.json"
            if tc.is_file():
                t_servers = (json.loads(tc.read_text()) or {}).get("mcpServers") or {}
                mcp_drift = sorted(k for k, v in e_servers.items() if t_servers.get(k) != v)
            else:
                mcp_drift = sorted(e_servers)

    return {
        "drifted": drifted,
        "missing": missing,
        "stale": stale,
        "mcp_drift": mcp_drift,
        "ok": not (drifted or missing or stale or mcp_drift),
    }
