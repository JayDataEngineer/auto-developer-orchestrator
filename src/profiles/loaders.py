"""Org + specialist-agent LOADING — backend-agnostic.

The portable core of ``agent/orgs.py``: reads an org (``AGENTS.md`` +
``org.yaml`` + ``agents/<slug>.md`` + ``skills/``) off the filesystem into data.
Depends ONLY on stdlib + ``yaml`` — no Docker, no model registry. Every function
takes an explicit ``project_root`` (no module-level global).
"""
from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml

from compiler.capabilities import desugar_agent_capabilities

from . import _paths

# --- pure helpers ----------------------------------------------------------

def _parse_list(raw: Any) -> list[str]:
    """A list value -> stripped non-empty items. Accepts either a YAML list
    (``[a, b]``) or a comma-separated scalar (``agents: a,b``)."""
    if raw is None:
        return []
    if isinstance(raw, list):
        return [str(s).strip() for s in raw if str(s).strip()]
    return [s.strip() for s in str(raw).split(",") if s.strip()]


def _scan_orgs(root: Path) -> list[str]:
    """Scan a single directory for org subdirs (dirs containing ``AGENTS.md``)."""
    out: list[str] = []
    if not root.is_dir():
        return out
    for child in sorted(root.iterdir()):
        if child.is_dir() and (child / "AGENTS.md").is_file():
            out.append(child.name)
    return out


def _split_frontmatter(text: str) -> tuple[dict[str, Any], str]:
    """Split a ``.md`` file into ``(frontmatter, body)``.

    Frontmatter is the optional leading ``---``-delimited YAML block, parsed
    with ``yaml.safe_load``. Body is the markdown after the closing ``---``. No
    frontmatter -> ``({}, body)``. A non-mapping frontmatter block or a YAML
    syntax error raises ``ValueError`` (fail loud).
    """
    if not text.startswith("---"):
        return {}, text.strip()
    parts = text.split("---", 2)
    if len(parts) < 3:
        return {}, text.strip()
    _, head, body = parts
    try:
        fm = yaml.safe_load(head) or {}
    except yaml.YAMLError as e:  # pragma: no cover - covered by contract tests
        msg = f"invalid YAML frontmatter: {e}"
        raise ValueError(msg) from e
    if not isinstance(fm, dict):
        msg = f"frontmatter must be a YAML mapping, got {type(fm).__name__}"
        raise TypeError(msg)
    return fm, body.strip()


# --- path helpers (project_root-parameterized) -----------------------------

def _orgs_dir(project_root: Path) -> Path:
    return project_root / "profiles"


def _specialists_dir(project_root: Path) -> Path:
    return _orgs_dir(project_root) / "specialists"


def _org_path(name: str, project_root: Path) -> Path:
    """Resolve an org's directory across all roots (delegates to ``_paths.search_org_dir``)."""
    return _paths.search_org_dir(name, project_root)


# --- org inheritance (``org.yaml extends:``) ---------------------
#
# An org may declare ``extends: <parent-org>`` in its ``org.yaml`` to inherit
# the parent's ROSTER (``agents:``), AGENTS.md overlay, and profile.yaml. This
# is the org-level analogue of an agent's ``extends:``: the parent is
# the BASE, the child SPECIALIZES. Three things compose root→child:
#
# * roster — parent ``agents:`` ∪ own (``org_agent_slugs``); an inherited slug
#   resolves through the child's agent dirs FIRST (``_agent_search_dirs`` is
#   chain-aware), so a child specializes an inherited agent by dropping a
#   same-named ``<slug>.md`` in its own ``agents/``.
# * AGENTS.md overlay — parent + own concatenated own-last (``_chain_overlay``).
# * profile.yaml — deep-merged root→child (``profile._resolved_profile_yaml``).
#
# ``policy.yaml`` is NEVER inherited (security — each org owns its egress); the
# contract warns on a policy-less child (Safeguard S6).
#
# Cycle-safety: ``org_extends_chain`` RAISES on a cycle / unresolvable parent
# (mirrors ``_load_agent_spec``'s agent-extends recursion); the runtime loaders
# use the cycle-safe ``_resolved_org_chain`` (falls back to ``[name]``), and the
# contract walks RAW (``contract._org_extends_chain_violations``) for precise
# ``org-extends-resolvable`` / ``org-extends-acyclic`` messages. Two walkers, one
# pattern — exactly the agent-extends split (``_load_agent_spec`` raises vs
# ``_agent_extends_chain_violations`` reports).


def org_extends(name: str, project_root: Path) -> str | None:
    """This org's single-hop ``extends:`` parent (a raw read of ``org.yaml``), or
    ``None``. ``None`` when the org ships no ``org.yaml``, no ``extends`` key, or
    a non-string ``extends``. The RAW reader the contract's chain walker +
    ``org_extends_chain`` build on (no recursion, no merge — single hop only)."""
    manifest = _org_path(name, project_root) / "org.yaml"
    if not manifest.is_file():
        return None
    data = yaml.safe_load(manifest.read_text()) or {}
    if not isinstance(data, dict):
        return None
    parent = data.get("extends")
    if not isinstance(parent, str) or not parent.strip():
        return None
    return parent.strip()


def org_extends_chain(name: str, project_root: Path) -> list[str]:
    """The org's inheritance chain, ROOT→CHILD, walking ``extends:`` recursively. Raises ``ValueError`` on cycle, ``FileNotFoundError`` on unresolvable parent. Mirrors ``_load_agent_spec``'s agent-extends recursion; the contract walks RAW via ``_org_extends_chain_violations``."""
    upward: list[str] = []  # built child→root, reversed before return
    visited: set[str] = set()
    cur = name
    while True:
        if cur in visited:
            cycle = " -> ".join([*upward, cur])
            msg = f"org {name!r}: extends cycle ({cycle})"
            raise ValueError(msg)
        visited.add(cur)
        upward.append(cur)
        parent = org_extends(cur, project_root)
        if parent is None:
            break  # chain terminates cleanly
        try:
            pdir = _org_path(parent, project_root)
        except FileNotFoundError as exc:
            msg = f"org {name!r}: extends {parent!r} -> no such org"
            raise FileNotFoundError(msg) from exc
        if not (pdir / "AGENTS.md").is_file():
            msg = f"org {name!r}: extends {parent!r} -> no AGENTS.md (not a valid base org)"
            raise FileNotFoundError(msg)
        cur = parent
    upward.reverse()  # root→child
    return upward


def _resolved_org_chain(name: str, project_root: Path) -> list[str]:
    """Cycle-safe inheritance chain, ROOT→CHILD. Falls back to ``[name]`` on a
    broken chain (cycle / unresolvable parent) so runtime loaders NEVER crash —
    the contract's ``org-extends-*`` rules report the real fault offline. For an
    org with no ``extends:`` this is just ``[name]`` (byte-identical to today)."""
    try:
        return org_extends_chain(name, project_root)
    except (ValueError, FileNotFoundError):
        return [name]


def _agent_search_dirs(org: str, project_root: Path) -> list[Path]:
    """Directories searched for an agent ``<slug>.md``: child-local first, then
    each ancestor's, then ``_shared``. First hit wins (child overrides inherited
    or shared). Chain-aware via ``_resolved_org_chain`` reversed; cycle-safe."""
    chain = _resolved_org_chain(org, project_root)  # root→child
    local: list[Path] = []
    for ancestor in reversed(chain):  # child→root (child's agents win)
        try:
            adir = _org_path(ancestor, project_root) / "agents"
        except FileNotFoundError:
            continue  # ancestor doesn't resolve (minimal fixture / broken chain)
        if adir.is_dir():
            local.append(adir)
    return [*local, _orgs_dir(project_root) / "_shared" / "agents"]


# --- org discovery + roster ------------------------------------------------

def discover_orgs(project_root: Path) -> list[str]:
    """Sorted names of every org dir containing ``AGENTS.md``. Scans the
    project's ``profiles/`` (top-level) + ``profiles/specialists/`` (nested), then each
    ``$PUX_ORG_PATHS`` root (top-level + its ``specialists/``). Library bases
    (``pux:``) are NOT auto-discovered — they're opt-in via the namespace, so a
    consumer app's org list stays its own. De-duped + sorted."""
    names: list[str] = []
    for root in [_orgs_dir(project_root), *_paths.extra_org_roots()]:
        names.extend(_scan_orgs(root))
        names.extend(_scan_orgs(root / "specialists"))
    return sorted(set(names))


def _own_org_agent_slugs(name: str, project_root: Path) -> list[str]:
    """This org's OWN roster (``org.yaml agents:``) — NO inheritance. Raises
    ``ValueError`` on a malformed ``org.yaml`` (non-mapping top level). Returns
    ``[]`` when the org ships no ``org.yaml`` (valid for a CTO-only org).

    Factored out of ``org_agent_slugs`` so the chain-aware reader can call it per
    ancestor without re-implementing the parse."""
    org_dir = _org_path(name, project_root)
    manifest = org_dir / "org.yaml"
    if not manifest.is_file():
        return []
    data = yaml.safe_load(manifest.read_text()) or {}
    if not isinstance(data, dict):
        msg = f"{name}/org.yaml: top level must be a mapping, got {type(data).__name__}"
        raise TypeError(msg)
    return _parse_list(data.get("agents"))


def _org_inherit_roster(name: str, project_root: Path) -> bool:
    """Whether ``name`` opts INTO the parent-roster union. ``inherit_roster:``
    defaults True (a child that ``extends:`` inherits the parent's roster). A
    specialist that extends a base org for the base PROMPT but whose own roster
    is authoritative sets ``inherit_roster: false`` — ``org_agent_slugs`` then
    returns only its own ``agents:`` (the AGENTS.md overlay still flows via
    ``_chain_overlay``; this is a roster-only opt-out, the D4 mechanism).

    Malformed yaml is left to ``_own_org_agent_slugs`` to raise the real error."""
    manifest = _org_path(name, project_root) / "org.yaml"
    if not manifest.is_file():
        return True
    data = yaml.safe_load(manifest.read_text()) or {}
    if not isinstance(data, dict):
        return True
    return bool(data.get("inherit_roster", True))


def org_agent_slugs(name: str, project_root: Path) -> list[str]:
    """The specialist slugs this org delegates to — the chain-INHERITED roster
    (parent ``agents:`` ∪ own, walked root→child; a slug in both appears once at
    the parent's position, own redeclarations specialized via the child's local
    ``agents/`` dir). Cycle-safe: a broken ``extends:`` chain falls back to
    ``[name]`` (the contract's ``org-extends-*`` rules report the fault).

    For a non-extending org the chain is ``[name]`` → byte-identical to reading
    just its own ``org.yaml``. An org that declares ``inherit_roster: false``
    opts out of the union: its OWN ``agents:`` is authoritative (the base
    PROMPT still flows via ``extends:``; only the roster is pruned)."""
    if not _org_inherit_roster(name, project_root):
        return _own_org_agent_slugs(name, project_root)
    seen: set[str] = set()
    roster: list[str] = []
    for org in _resolved_org_chain(name, project_root):  # root→child
        for slug in _own_org_agent_slugs(org, project_root):
            if slug not in seen:
                seen.add(slug)
                roster.append(slug)
    return roster


# --- prompt assembly -------------------------------------------------------

def load_org_prompt(name: str, project_root: Path) -> str:
    """Body of ``profiles/<name>/AGENTS.md`` (the per-org CTO overlay)."""
    return _split_frontmatter((_org_path(name, project_root) / "AGENTS.md").read_text())[1]


def _chain_overlay(org: str, project_root: Path) -> str:
    """Concatenated ``AGENTS.md`` overlays across the inheritance chain,
    root→child (own LAST). Cycle-safe. Each ancestor's overlay is read by
    ``load_org_prompt`` (frontmatter stripped, body only). A child extends a
    parent's CTO prose the same way an agent extends a base prompt — by
    APPENDING. For a non-extending org this is just its own overlay
    (byte-identical)."""
    parts: list[str] = []
    for ancestor in _resolved_org_chain(org, project_root):  # root→child
        body = load_org_prompt(ancestor, project_root)
        if body:
            parts.append(body)
    return "\n\n".join(parts)


def build_system_prompt(org: str, *, project_root: Path, addendum: str = "") -> str:
    """The chain-inherited org overlay + ``addendum`` — the static base of the
    supervisor prompt. Cycle-safe."""
    return f"{_chain_overlay(org, project_root)}{addendum}"


def load_shared_prompt_body(filename: str, project_root: Path) -> str:
    """Read ``profiles/_shared/<filename>`` -> body (frontmatter stripped,
    ``.strip()``-clean). Returns ``""`` when the file is absent — the common
    case for minimal fixtures / packed archives that omit ``_shared/``.

    This is the kit's ONE reader for EVERY ``profiles/_shared/*.md`` prompt block
    (harness_addendum, dynamic_dispatch_suffix, ask_user_suffix). The caller
    adds any seam / fallback logic. CWD-independent (takes ``project_root``
    explicitly)."""
    path = _orgs_dir(project_root) / "_shared" / filename
    if not path.is_file():
        return ""
    return _split_frontmatter(path.read_text())[1]


# --- agent specs + skills --------------------------------------------------

def _merge_extends(base: dict[str, Any], delta_fm: dict[str, Any], body: str) -> dict[str, Any]:
    """Merge a delta agent (declares ``extends:``) onto its fully-resolved base.
    Returns a spec dict in the ``{**fm, "system_prompt": body}`` shape
    ``_build_sub`` consumes.

    Merge rules: ``name`` / ``description`` / ``model`` — delta wins
    (``description_append`` concatenates). ``tools`` — explicit full-replace,
    else ∪ ``tools_add`` − ``tools_remove``. ``skills`` — explicit full-replace,
    else ∪ ``skills_add``. ``tool_description_overrides`` — per-key, delta wins.
    ``system_prompt`` — base body + delta body joined with ``\\n\\n``.
    """
    merged: dict[str, Any] = dict(base)

    # name — delta wins.
    if "name" in delta_fm:
        merged["name"] = delta_fm["name"]

    # description — delta wins; description_append concatenates onto the
    # effective description (delta if given, else base).
    desc = merged.get("description")
    if "description" in delta_fm:
        desc = delta_fm["description"]
    if delta_fm.get("description_append"):
        desc = f"{desc or ''} {delta_fm['description_append']}".strip()
    if desc is not None:
        merged["description"] = desc

    # model — delta wins.
    if "model" in delta_fm:
        merged["model"] = delta_fm["model"]

    # tools — explicit full-replace, else additive set union/diff on suffixes.
    if "tools" in delta_fm:
        merged["tools"] = delta_fm["tools"]
    else:
        base_tools = _parse_list(base.get("tools"))
        add = _parse_list(delta_fm.get("tools_add"))
        rem = {t.rsplit("/", 1)[-1] for t in _parse_list(delta_fm.get("tools_remove"))}
        if add or rem:
            seen: set[str] = set()
            out: list[str] = []
            for entry in [*base_tools, *add]:
                suffix = entry.rsplit("/", 1)[-1]
                if suffix in rem or suffix in seen:
                    continue
                seen.add(suffix)
                out.append(entry)
            merged["tools"] = out

    # skills — explicit full-replace, else additive (dedup, order preserved).
    if "skills" in delta_fm:
        merged["skills"] = delta_fm["skills"]
    elif delta_fm.get("skills_add"):
        base_skills = _parse_list(base.get("skills"))
        add = _parse_list(delta_fm.get("skills_add"))
        seen_s: set[str] = set()
        out_s: list[str] = []
        for entry in [*base_skills, *add]:
            if entry in seen_s:
                continue
            seen_s.add(entry)
            out_s.append(entry)
        merged["skills"] = out_s

    # tool_description_overrides — per-key merge, delta wins (Safeguard S5).
    tdo = dict(base.get("tool_description_overrides") or {})
    if delta_fm.get("tool_description_overrides"):
        tdo.update(delta_fm["tool_description_overrides"])
    if tdo:
        merged["tool_description_overrides"] = tdo

    # Everything else the delta declared (``middleware`` / ``rubric`` / ``mcp``
    # / any future key) carries over, delta wins — a declared key must NEVER
    # silently vanish from the merged spec. Only the merge-sugar keys are
    # excluded (they were consumed above / are not declarative surface).
    for k, v in delta_fm.items():
        if k in ("extends", "capabilities", "tools_add", "tools_remove",
                 "skills_add", "description_append",
                 "name", "description", "model", "tools", "skills",
                 "tool_description_overrides", "system_prompt"):
            continue  # handled explicitly above (delta already won)
        merged[k] = v

    # system_prompt — base body + delta body (the delta IS prompt_append).
    base_body = base.get("system_prompt", "")
    merged["system_prompt"] = f"{base_body}\n\n{body}".strip() if body else base_body

    return merged


def _load_agent_spec(
    slug: str,
    org: str,
    project_root: Path,
    _chain: tuple[str, ...] = (),
) -> dict[str, Any] | None:
    """Read ``<slug>.md`` from org-local then ``_shared`` agent dir → spec dict
    (``name``/``description`` + optional frontmatter fields; ``system_prompt`` =
    body). ``None`` if not found.

    ``extends:`` is recursive + cycle-detected; base resolves from the same
    search dirs, delta merges via ``_merge_extends``. A ``pux:``-namespaced slug
    resolves only against library bases. ``_chain`` is the internal recursion guard.
    """
    pux = _paths.is_pux_namespace(slug)
    look = _paths.strip_namespace(slug) if pux else slug
    search_dirs = (
        _paths.library_base_agent_dirs() if pux
        else _agent_search_dirs(org, project_root)
    )
    for d in search_dirs:
        path = d / f"{look}.md"
        if path.is_file():
            fm, body = _split_frontmatter(path.read_text())
            # CU-3: desugar an opt-in ``capabilities:`` block (kind ∈ {tool,
            # skill, mcp}) into this agent's ``tools:`` / ``skills:`` / ``mcp:``
            # — BEFORE the ``extends:`` merge so a parent's ``capabilities:``
            # becomes the parent's tools/skills and the existing inheritance
            # machinery operates on the desugared form. A malformed block raises
            # ``CapabilitiesSugarError`` (the contract surfaces it as
            # ``capabilities-sugar-agent``); ``None``/no-block is a no-op.
            fm = desugar_agent_capabilities(fm, slug)
            extends = fm.pop("extends", None)
            if extends is not None:
                if not isinstance(extends, str) or not extends.strip():
                    raise ValueError(
                        f"agent {slug!r}: extends must be a non-empty agent slug, "
                        f"got {extends!r}")
                extends = extends.strip()
                if slug in _chain:
                    chain = " -> ".join([*_chain, slug])
                    raise ValueError(f"agent {slug!r}: extends cycle ({chain})")
                base = _load_agent_spec(extends, org, project_root, (*_chain, slug))
                if base is None:
                    base_dirs = (
                        _paths.library_base_agent_dirs()
                        if _paths.is_pux_namespace(extends)
                        else _agent_search_dirs(org, project_root)
                    )
                    searched = [str(p / f"{_paths.strip_namespace(extends)}.md")
                                for p in base_dirs]
                    raise FileNotFoundError(
                        f"agent {slug!r}: extends {extends!r} -> no such agent "
                        f"(searched {searched})")
                return _merge_extends(base, fm, body)
            return {**fm, "system_prompt": body}
    return None


def _resolve_skills(
    raw: Any, slug: str, *, project_root: Path, workspace_root: str | None = None,
) -> list[str]:
    """``skills`` value → skills-ROOT paths for deepagents' ``SkillsMiddleware``.
    Each value is a project-relative directory (validated to exist). Returns
    host-absolute paths by default, or container-absolute when ``workspace_root``
    is set (the harness bind-mounts at ``/sandbox/workspace``).
    """
    out: list[str] = []
    for p in _parse_list(raw):
        if not isinstance(p, str) or not p:
            msg = f"{slug}: each skills source must be a non-empty path string"
            raise ValueError(msg)
        if p.startswith("/") or ".." in Path(p).parts:
            msg = f"{slug}: skills source must be project-relative (got {p!r})"
            raise ValueError(msg)
        if not (project_root / p).is_dir():
            raise KeyError(
                f"{slug}: skills source {p!r} -> no such directory under the project root"
            )
        out.append(
            f"{workspace_root}/{p}" if workspace_root else str(project_root / p)
        )
    return out


def supervisor_skills_roots(
    org: str, project_root: Path, workspace_root: str | None = None,
) -> list[str]:
    """Skills-ROOT paths for the supervisor's ``SkillsMiddleware`` — focused set
    (``profiles/_shared/skills`` + this org's own), existing dirs only, mapped per
    ``workspace_root``. Returns ``[]`` when neither exists. Wires native
    progressive disclosure on the CTO.
    """
    candidates = [
        "profiles/_shared/skills",
        f"profiles/{org}/skills",
        f"profiles/specialists/{org}/skills",
    ]
    existing = [c for c in candidates if (project_root / c).is_dir()]
    return _resolve_skills(
        existing, "supervisor", project_root=project_root, workspace_root=workspace_root,
    )
