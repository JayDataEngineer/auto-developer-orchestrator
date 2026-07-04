"""Declarative org contract — the portable org <-> harness interface.

An org is a directory ``orgs/<name>/`` containing ``AGENTS.md``. This module
enforces that every org is a self-contained, portable bundle with **no
harness-level per-org code coupling** — pillar (a) of the deepagents pivot:
orgs declare what they need, the harness treats them generically.

Two validation tiers:

* **Structural** (always checked, no server, no model tokens):
  1. ``AGENTS.md`` present.
  2. Org frontmatter uses only known keys (the standardized ``agents:``).
  3. Every ``agents:`` slug resolves to a valid ``.pi/agents/<slug>.md`` with
     known frontmatter + a description.
  5. Optional ``policy.yaml`` parses and uses known sections.

* **Tool-resolution** (rule 4 — always on, no server): every entry in an
  agent's ``tools:`` whitelist resolves to a native fs tool OR a name in the
  static specialist surface (``SPECIALIST_TOOL_NAMES`` from ``native_tools``).
  Both surfaces are Python constants, so this runs offline in pytest and in
  ``--check-contract`` with no container or Go server.

* **Harness-level** (rule 6) and **global** (rules 7-8):
  6. No hardcoded org->agent manifest in the harness source.
  7. No orphan agents (every specialist owned by >=1 org).
  8. Skill hygiene: every ``SKILL.md`` is Agent-Spec well-formed, and no ``.md``
     sits loose directly under a skills root (``check_skill_roots``).

Rule 4 resolves against the *static* native surface: the specialist names are
a Python frozenset (the single source of truth shared with ``graph.py``), so a
stale ``tools:`` reference fails loud here without any process to probe.
"""
from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml
from deepagents import FilesystemPermission

from pux_harness import policy as policy_mod
from pux_harness.native_tools import SPECIALIST_TOOL_NAMES
from pux_harness.orgs import (
    PROJECT_ROOT,
    _agents_dir,
    _orgs_dir,
    _parse_list,
    _split_frontmatter,
    discover_orgs,
    org_agent_slugs,
)
# ``_orgs_dir`` / ``_agents_dir`` are re-exported here (bound into THIS
# module's namespace by the import) so the contract tests can monkeypatch
# ``contract._orgs_dir`` / ``_agents_dir`` at the existing call sites.

# --- the contract vocabulary ----------------------------------------------

# Org-level AGENTS.md frontmatter may use only this key. It declares the
# specialist slugs the org delegates to (comma-separated), replacing the old
# hardcoded ``ORG_AGENTS`` manifest.
KNOWN_ORG_KEYS: frozenset[str] = frozenset({"agents"})

# ``.pi/agents/<slug>.md`` frontmatter may use only these keys. The first 8 are
# the carry-over pi-mono shape (harness ignores the last 4 of those — bools
# like ``inheritSkills``); the last 5 are the Phase-10 deepagents ``SubAgent``
# vocabulary the loader resolves in ``orgs.load_subagents`` (resolution table in
# ``orgs.py``). ``middleware`` is deliberately absent: ``SubAgentMiddleware``
# does not forward a raw spec's ``middleware`` key (Phase 7), so permitting it
# here would greenlight a silent no-op.
KNOWN_AGENT_KEYS: frozenset[str] = frozenset({
    "name", "description", "tools", "systemPromptMode", "output",
    "inheritSkills", "inheritProjectContext", "defaultProgress",
    "model", "skills", "response_format", "permissions", "interrupt_on",
})

# Optional ``orgs/<name>/policy.yaml`` top-level sections (matches Go policy.go).
KNOWN_POLICY_SECTIONS: frozenset[str] = frozenset({
    "workspace", "egress", "credentials", "sandbox", "browser",
})

# Native fs/shell tools deepagents exposes via the backend (Phase 3). Accepted
# bare in an agent ``tools:`` whitelist because they come from the backend, not
# the specialist registry.
NATIVE_FS_TOOLS: frozenset[str] = frozenset({
    "ls", "read_file", "write_file", "edit_file", "glob", "grep", "execute",
})

# Agent-Skills spec: a skill dir name (and its ``SKILL.md`` ``name``) must be
# kebab-case — lowercase letters/digits joined by single hyphens.
_SKILL_NAME_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")


@dataclass(frozen=True)
class Violation:
    """One contract failure. ``severity`` is "error" (fails green) or "warn"
    (SHOULD). The green gate treats only errors as blocking."""

    severity: str  # "error" | "warn"
    rule: str
    message: str

    def __str__(self) -> str:
        return f"[{self.severity.upper()}] {self.rule}: {self.message}"


# --- discovery (orgs + agent-slugs live in the low-level orgs module) ----
# ``_orgs_dir`` / ``_agents_dir`` / ``_skills_dir`` / ``_parse_list`` /
# ``_split_frontmatter`` are all imported from ``orgs`` (re-exported above) —
# single source of truth. The contract tests monkeypatch them at
# ``contract._orgs_dir`` etc., which still works because the import binds the
# names into THIS module's namespace.


# --- per-org checks (rules 1-5) ------------------------------------------


def _validate_rich_fields(afm: dict[str, Any], slug: str) -> list[Violation]:
    """Offline structural validation of the Phase-10 SubAgent fields.

    Mirrors what ``orgs.load_subagents`` enforces by raising at runtime, so a
    malformed field fails at ``--check-contract`` time (pytest) instead of
    mid-run. No model init, no Docker. ``model`` is checked as a string here —
    the loader resolves it through ``get_model`` (token-free in the dry-run, but
    the contract itself never constructs the model, so it stays offline).
    """
    out: list[Violation] = []

    if "model" in afm and not isinstance(afm["model"], str):
        out.append(Violation(
            "error", "model-shape",
            f"{slug}: model must be a string shorthand (e.g. 'glm-5.2'), "
            f"got {type(afm['model']).__name__}"))

    if "skills" in afm:
        project = _orgs_dir().parent
        for p in _parse_list(afm["skills"]):
            if (not isinstance(p, str) or not p
                    or p.startswith("/") or ".." in Path(p).parts):
                out.append(Violation(
                    "error", "skill-source-shape",
                    f"{slug}: skills source must be a project-relative path "
                    f"(got {p!r})"))
                continue
            src = project / p
            if not src.is_dir():
                out.append(Violation(
                    "error", "skill-source-resolves",
                    f"{slug}: skills source {p!r} -> not a directory under the "
                    f"project root"))
                continue
            # A declared source must hold >=1 Agent-Spec well-formed skill;
            # otherwise SkillsMiddleware silently loads nothing from it (the
            # exact regression that stranded the org playbooks). Specific
            # per-skill malformations are reported globally by
            # ``check_skill_roots``; this rule guards the dead declaration.
            if not _well_formed_skill_dirs(src):
                out.append(Violation(
                    "error", "skill-source-resolves",
                    f"{slug}: skills source {p!r} -> no well-formed skill "
                    f"(expected <source>/<skill-name>/SKILL.md)"))

    if "response_format" in afm and not isinstance(afm["response_format"], dict):
        out.append(Violation(
            "error", "response-format-shape",
            f"{slug}: response_format must be a JSON-schema mapping, "
            f"got {type(afm['response_format']).__name__}"))

    if "permissions" in afm:
        raw = afm["permissions"]
        allowed_keys = {"operations", "paths", "mode"}
        allowed_ops = {"read", "write"}
        allowed_modes = {"allow", "deny", "interrupt"}
        if not isinstance(raw, list):
            out.append(Violation(
                "error", "permissions-shape",
                f"{slug}: permissions must be a list of mappings, "
                f"got {type(raw).__name__}"))
        else:
            for i, entry in enumerate(raw):
                if not isinstance(entry, dict):
                    out.append(Violation(
                        "error", "permissions-shape",
                        f"{slug}: permissions[{i}] must be a mapping, "
                        f"got {type(entry).__name__}"))
                    continue
                bad = sorted(k for k in entry if k not in allowed_keys)
                if bad:
                    out.append(Violation(
                        "error", "permissions-shape",
                        f"{slug}: permissions[{i}] unknown keys {bad}; "
                        f"allowed: {sorted(allowed_keys)}"))
                    continue
                ops = entry.get("operations", [])
                bad_ops = [o for o in ops if o not in allowed_ops]
                if not isinstance(ops, list) or bad_ops:
                    out.append(Violation(
                        "error", "permissions-shape",
                        f"{slug}: permissions[{i}].operations must be a list of "
                        f"'read'/'write'; invalid: {bad_ops!r}"))
                    continue
                mode = entry.get("mode", "allow")
                if mode not in allowed_modes:
                    out.append(Violation(
                        "error", "permissions-shape",
                        f"{slug}: permissions[{i}].mode must be one of "
                        f"{sorted(allowed_modes)}, got {mode!r}"))
                    continue
                # Reuse deepagents' own path validation (leading '/', no '..'/'~').
                try:
                    FilesystemPermission(**entry)
                except (ValueError, TypeError) as e:
                    out.append(Violation(
                        "error", "permissions-shape",
                        f"{slug}: permissions[{i}] invalid: {e}"))

    if "interrupt_on" in afm:
        raw = afm["interrupt_on"]
        if (not isinstance(raw, dict)
                or not all(isinstance(v, bool) for v in raw.values())):
            out.append(Violation(
                "error", "interrupt-on-shape",
                f"{slug}: interrupt_on must be a mapping of tool-name -> bool, "
                f"got {raw!r}"))

    return out


def check_org(name: str) -> list[Violation]:
    """Validate one org's bundle — fully offline (no server, no tokens).

    Rules 1,2,3,5 are structural. Rule 4 (tool-resolution) resolves every
    agent ``tools:`` entry against ``NATIVE_FS_TOOLS`` ∪
    ``SPECIALIST_TOOL_NAMES`` — both Python constants — so it runs in pytest
    and ``--check-contract`` with nothing live.
    """
    v: list[Violation] = []
    org_dir = _orgs_dir() / name
    agents_md = org_dir / "AGENTS.md"

    # Rule 1.
    if not agents_md.is_file():
        return [Violation("error", "org-agents-md",
                          f"{name}: orgs/{name}/AGENTS.md missing")]

    fm, _ = _split_frontmatter(agents_md.read_text())

    # Rule 2: org frontmatter keys known.
    for key in fm:
        if key not in KNOWN_ORG_KEYS:
            v.append(Violation("error", "org-frontmatter-keys",
                               f"{name}: unknown frontmatter key {key!r}; "
                               f"allowed: {sorted(KNOWN_ORG_KEYS)}"))

    slugs = _parse_list(fm.get("agents", ""))

    # Rule 3: every slug resolves to a valid agent file.
    agent_frontmatter: dict[str, dict[str, str]] = {}
    for slug in slugs:
        agent_file = _agents_dir() / f"{slug}.md"
        if not agent_file.is_file():
            v.append(Violation("error", "agent-resolves",
                               f"{name}: agents: {slug!r} -> "
                               f"no .pi/agents/{slug}.md"))
            continue
        afm, _ = _split_frontmatter(agent_file.read_text())
        agent_frontmatter[slug] = afm
        bad = sorted(k for k in afm if k not in KNOWN_AGENT_KEYS)
        if bad:
            v.append(Violation("error", "agent-frontmatter-keys",
                               f"{name}/{slug}: unknown frontmatter keys {bad}; "
                               f"allowed: {sorted(KNOWN_AGENT_KEYS)}"))
        if not afm.get("description"):
            v.append(Violation("error", "agent-description",
                               f"{name}/{slug}: missing 'description' "
                               f"(deepagents needs it for the task tool)"))

    # Rule 4: tool whitelist resolves against the static native surface
    # (fs tools ∪ the specialist registry). No server probe — both halves are
    # Python constants, so this runs identically in pytest and --check-contract.
    for slug, afm in agent_frontmatter.items():
        for raw in _parse_list(afm.get("tools", "")):
            tool = raw.rsplit("/", 1)[-1]
            if tool in NATIVE_FS_TOOLS:
                continue
            key = "pux_sandbox_" + tool
            if key not in SPECIALIST_TOOL_NAMES:
                v.append(Violation("error", "tool-resolves",
                                   f"{name}/{slug}: tool {raw!r} -> "
                                   f"{key!r} not a native fs tool or a "
                                   f"pux_sandbox_* specialist"))

    # Rule 4b: Phase-10 rich SubAgent fields (model/skills/response_format/
    # permissions/interrupt_on) are structurally valid — offline, mirrors the
    # runtime resolvers in ``orgs.load_subagents``. A malformed value fails here
    # rather than aborting a real run.
    for slug, afm in agent_frontmatter.items():
        for violation in _validate_rich_fields(afm, f"{name}/{slug}"):
            v.append(violation)

    # Rule 5: policy.yaml parses + valid schema + known sections.
    policy_path = org_dir / "policy.yaml"
    if policy_path.is_file():
        try:
            parsed = yaml.safe_load(policy_path.read_text())
        except yaml.YAMLError as e:
            v.append(Violation("error", "policy-parse",
                               f"{name}: policy.yaml is not valid YAML: {e}"))
            parsed = None
        if isinstance(parsed, dict):
            bad = sorted(k for k in parsed if k not in KNOWN_POLICY_SECTIONS)
            if bad:
                v.append(Violation("error", "policy-sections",
                                   f"{name}: policy.yaml unknown sections "
                                   f"{bad}; allowed: "
                                   f"{sorted(KNOWN_POLICY_SECTIONS)}"))
            # Deep schema: the real policy engine (Phase 6 port of the Go
            # package) catches malformed mounts/creds the shallow section check
            # misses. Go's parser is lenient on unknown keys, so the strict
            # unknown-section check above runs first; this catches everything
            # else — non-mapping sections (load) + bad mount paths/modes +
            # unset ${VAR} placeholders (resolve_mounts). egress_rules is
            # intentionally NOT called here: it resolves real DNS (network),
            # which the contract must not depend on.
            try:
                pol = policy_mod.load(name, _orgs_dir().parent)
                policy_mod.resolve_mounts(pol)
            except policy_mod.PolicyError as e:
                v.append(Violation("error", "policy-schema",
                                   f"{name}: policy.yaml schema error: {e}"))
            except policy_mod.NoPolicy:
                pass
        elif parsed is not None:
            v.append(Violation("error", "policy-shape",
                               f"{name}: policy.yaml top-level must be a "
                               f"mapping, got {type(parsed).__name__}"))

    return v


# --- global checks (rules 6-7) -------------------------------------------

_MANIFEST_RE = re.compile(r"^\s*ORG_AGENTS\s*[:=]", re.MULTILINE)


def orphan_agents() -> list[str]:
    """Agent slugs owned by no org (not listed in any ``agents:``
    frontmatter). Rule 7 — SHOULD (warn), not blocking."""
    owned: set[str] = set()
    for org in discover_orgs():
        owned.update(org_agent_slugs(org))
    all_agents = {p.stem for p in _agents_dir().glob("*.md")}
    return sorted(all_agents - owned)


def check_harness() -> list[Violation]:
    """Rule 6 (no hardcoded org->agent manifest) + rule 7 (no orphan agents).
    Global — not per-org."""
    v: list[Violation] = []
    src = Path(__file__).with_name("orgs.py")
    if src.is_file() and _MANIFEST_RE.search(src.read_text()):
        v.append(Violation("error", "no-hardcoded-manifest",
                           "pux_harness/orgs.py: a hardcoded ORG_AGENTS "
                           "manifest re-couples the harness to orgs; use the "
                           "`agents:` frontmatter + discover_orgs() instead"))
    for orphan in orphan_agents():
        v.append(Violation("warn", "no-orphan-agents",
                           f"agent {orphan!r} is owned by no org (not in any "
                           f"`agents:` frontmatter)"))
    return v


# --- rule 8 — global skill hygiene ---------------------------------------

def _check_skill_dir(skill_dir: Path) -> list[Violation]:
    """Agent-Spec well-formedness of one ``<source>/<name>/`` skill dir.

    Well-formed == it contains a ``SKILL.md`` whose YAML frontmatter parses,
    whose ``name`` equals the dir name (kebab-case), and whose ``description``
    is non-empty (the spec requires both, and SkillsMiddleware needs the
    ``description`` for level-1 metadata discovery). Returns one
    ``skill-well-formed`` error per failure; an empty list means the skill is
    well-formed.
    """
    name = skill_dir.name
    skill_md = skill_dir / "SKILL.md"
    if not skill_md.is_file():
        return [Violation("error", "skill-well-formed",
                          f"skill {name!r}: missing SKILL.md "
                          f"(expected <source>/<name>/SKILL.md)")]
    try:
        fm, _ = _split_frontmatter(skill_md.read_text())
    except ValueError as e:
        return [Violation("error", "skill-well-formed",
                          f"skill {name!r}: SKILL.md frontmatter does not "
                          f"parse: {e}")]
    out: list[Violation] = []
    if fm.get("name") != name:
        out.append(Violation("error", "skill-well-formed",
                             f"skill {name!r}: frontmatter name "
                             f"{fm.get('name')!r} must equal the dir name "
                             f"{name!r}"))
    if not _SKILL_NAME_RE.match(name):
        out.append(Violation("error", "skill-well-formed",
                             f"skill {name!r}: dir name must be kebab-case "
                             f"(lowercase letters/digits joined by '-')"))
    if not fm.get("description"):
        out.append(Violation("error", "skill-well-formed",
                             f"skill {name!r}: SKILL.md missing a non-empty "
                             f"'description' (required for skills-middleware "
                             f"discovery)"))
    return out


def _well_formed_skill_dirs(source: Path) -> list[Path]:
    """Skill dirs directly under ``source`` that pass well-formedness.

    Used by ``skill-source-resolves`` to require a declared source carry at
    least one real skill (a source of only malformed/empty dirs silently loads
    nothing)."""
    if not source.is_dir():
        return []
    return [c for c in sorted(source.iterdir())
            if c.is_dir() and not _check_skill_dir(c)]


def _skill_roots() -> list[Path]:
    """Every skills-ROOT directory in the project: the global ``.pi/skills``
    plus each ``orgs/<name>/skills``. Scanned regardless of whether any agent
    declares the root — a loose playbook or malformed skill is a regression
    even if undeclared."""
    orgs = _orgs_dir()
    roots = [PROJECT_ROOT / ".pi" / "skills"]
    roots += sorted(p for p in orgs.glob("*/skills") if p.is_dir())
    return [r for r in roots if r.is_dir()]


def check_skill_roots() -> list[Violation]:
    """Global skill hygiene (rule 8). Scans EVERY skills root in the project
    whether or not an agent declares it:

    * each ``<root>/<name>/SKILL.md`` is Agent-Spec well-formed
      (``skill-well-formed`` error);
    * no ``.md`` sits loose directly under a root (``skill-dir-not-loose``
      warn) — a loose playbook is invisible to SkillsMiddleware, the exact
      regression that stranded the org playbooks before this rule.

    The contract CLI runs this alongside ``check_harness``; the per-org pass
    (``skill-source-resolves``) guards declared sources, this one guards the
    filesystem as a whole.
    """
    v: list[Violation] = []
    for root in _skill_roots():
        rel_root = root.relative_to(PROJECT_ROOT)
        for child in sorted(root.iterdir()):
            if child.is_dir():
                v.extend(_check_skill_dir(child))
            elif child.suffix == ".md":
                v.append(Violation(
                    "warn", "skill-dir-not-loose",
                    f"{rel_root}/{child.name}: loose .md under a skills root "
                    f"is invisible to SkillsMiddleware — move it into a "
                    f"<skill-name>/ dir (or its references/)."))
    return v


def check_all() -> dict[str, list[Violation]]:
    """Per-org violations for every discovered org. Global checks live in
    ``check_harness()``; the CLI runs both."""
    return {org: check_org(org) for org in discover_orgs()}


def has_errors(violations: list[Violation]) -> bool:
    return any(x.severity == "error" for x in violations)
