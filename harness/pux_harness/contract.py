"""Declarative org contract — the portable org <-> harness interface.

An org is a directory ``orgs/<name>/`` containing ``AGENTS.md`` (CTO prompt
prose) and optionally ``org.yaml`` (the specialist roster). This module
enforces that every org is a self-contained, portable bundle with **no
harness-level per-org code coupling** — pillar (a) of the deepagents pivot:
orgs declare what they need, the harness treats them generically.

Two validation tiers:

* **Structural** (always checked, no server, no model tokens):
  1. ``AGENTS.md`` present.
  2. ``AGENTS.md`` carries no frontmatter (prose-only); roster lives in
     ``org.yaml``.
  3. Every ``org.yaml`` slug resolves to a valid ``.pi/agents/<slug>.py``
     exporting a ``SUBAGENT`` dict with ``name``, ``description``, and
     ``system_prompt``.
  5. Optional ``policy.yaml`` parses and uses known sections.

* **Tool-resolution** (rule 4 — always on, no server): every entry in an
  agent's ``SUBAGENT["tools"]`` list resolves to a native fs tool OR a name in
  the static specialist surface (``SPECIALIST_TOOL_NAMES`` from ``native_tools``).
  Both surfaces are Python constants, so this runs offline in pytest and in
  ``--check-contract`` with no container or Go server.

* **Harness-level** (rule 6) and **global** (rules 7-8):
  6. No hardcoded org->agent manifest in the harness source.
  7. No orphan agents (every specialist owned by >=1 org).
  8. Skill hygiene: every ``SKILL.md`` is Agent-Spec well-formed, and no ``.md``
     sits loose directly under a skills root (``check_skill_roots``).

* **Permanent legacy tripwires** (no-legacy-agent-frontmatter,
  no-legacy-org-roster): the legacy ``.md``-with-frontmatter agent form and
  the ``agents:``-key-on-AGENTS.md org form are structurally forbidden —
  ``--check-contract`` blocks any commit that reintroduces them.

Rule 4 resolves against the *static* native surface: the specialist names are
a Python frozenset (the single source of truth shared with ``graph.py``), so a
stale ``tools:`` reference fails loud here without any process to probe.
"""
from __future__ import annotations

import importlib.util
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

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

# ``.pi/agents/<slug>.py`` SUBAGENT dict must contain these keys.
_REQUIRED_AGENT_KEYS: frozenset[str] = frozenset({
    "name", "description", "system_prompt",
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


def _load_agent_subagent(slug: str) -> dict[str, Any] | None:
    """Import ``.pi/agents/<slug>.py`` and return its ``SUBAGENT`` dict.

    Path-loaded (``importlib.util.spec_from_file_location``) — ``.pi/`` is NOT
    on ``sys.path`` and this never adds it. Returns ``None`` if no ``.py``
    exists; raises on import error or missing ``SUBAGENT`` (fail loud — a
    broken agent is a contract violation, not a silent skip).
    """
    path = _agents_dir() / f"{slug}.py"
    if not path.is_file():
        return None
    spec = importlib.util.spec_from_file_location(f"_pi_agent_{slug}", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)  # type: ignore[union-attr]
    return mod.SUBAGENT  # type: ignore[attr-defined]


def check_org(name: str) -> list[Violation]:
    """Validate one org's bundle — fully offline (no server, no tokens).

    Rules 1,2,3,5 are structural. Rule 4 (tool-resolution) resolves every
    agent ``SUBAGENT["tools"]`` entry against ``NATIVE_FS_TOOLS`` ∪
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

    # Rule 2: AGENTS.md is prose-only (no frontmatter). The roster lives in
    # org.yaml. Permanent tripwire against reintroduction.
    fm, _ = _split_frontmatter(agents_md.read_text())
    if fm:
        v.append(Violation(
            "error", "no-legacy-org-roster",
            f"{name}: AGENTS.md carries YAML frontmatter — the roster must "
            f"live in orgs/{name}/org.yaml and AGENTS.md must be prose-only"))

    # Read slugs from org.yaml (the only valid roster source).
    org_yaml = org_dir / "org.yaml"
    if org_yaml.is_file():
        data = yaml.safe_load(org_yaml.read_text()) or {}
        if not isinstance(data, dict):
            v.append(Violation(
                "error", "org-yaml-shape",
                f"{name}: org.yaml top-level must be a mapping, "
                f"got {type(data).__name__}"))
            slugs: list[str] = []
        else:
            slugs = _parse_list(data.get("agents"))
    elif not fm:
        # No org.yaml AND AGENTS.md has no frontmatter → empty roster (valid
        # for a CTO-only org with no specialists).
        slugs = []
    else:
        # No org.yaml but AGENTS.md has frontmatter — already reported above.
        slugs = _parse_list(fm.get("agents", ""))

    # Rule 3: every slug resolves to a valid .py agent with required keys.
    agent_subagents: dict[str, dict[str, Any]] = {}
    for slug in slugs:
        try:
            sub = _load_agent_subagent(slug)
        except Exception as exc:
            v.append(Violation(
                "error", "agent-resolves",
                f"{name}: agents: {slug!r} -> .pi/agents/{slug}.py "
                f"failed to import: {exc}"))
            continue
        if sub is None:
            v.append(Violation(
                "error", "agent-resolves",
                f"{name}: agents: {slug!r} -> "
                f"no .pi/agents/{slug}.py"))
            continue
        agent_subagents[slug] = sub
        missing = sorted(_REQUIRED_AGENT_KEYS - sub.keys())
        if missing:
            v.append(Violation(
                "error", "agent-missing-keys",
                f"{name}/{slug}: SUBAGENT dict missing required "
                f"keys: {missing}"))

    # Rule 4: tool whitelist resolves against the static native surface
    # (fs tools ∪ the specialist registry). No server probe — both halves are
    # Python constants, so this runs identically in pytest and --check-contract.
    for slug, sub in agent_subagents.items():
        for raw in _parse_list(sub.get("tools", [])):
            tool = raw.rsplit("/", 1)[-1]
            if tool in NATIVE_FS_TOOLS:
                continue
            key = "pux_sandbox_" + tool
            if key not in SPECIALIST_TOOL_NAMES:
                v.append(Violation("error", "tool-resolves",
                                   f"{name}/{slug}: tool {raw!r} -> "
                                   f"{key!r} not a native fs tool or a "
                                   f"pux_sandbox_* specialist"))

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
    """Agent slugs owned by no org (not listed in any ``org.yaml``).
    Rule 7 — SHOULD (warn), not blocking."""
    owned: set[str] = set()
    for org in discover_orgs():
        owned.update(org_agent_slugs(org))
    all_agents = {p.stem for p in _agents_dir().glob("*.py")}
    return sorted(all_agents - owned)


def _no_legacy_agent_frontmatter() -> list[Violation]:
    """No .pi/agents/*.md may carry YAML frontmatter — prose-only.

    Permanent tripwire (Phase 2). A new agent added as .md-with-frontmatter is
    a HARD contract failure, not a silent dual-read. The .py form is the only
    valid agent config path.
    """
    v: list[Violation] = []
    for md in sorted(_agents_dir().glob("*.md")):
        text = md.read_text()
        if text.startswith("---"):
            v.append(Violation(
                "error", "no-legacy-agent-frontmatter",
                f".pi/agents/{md.name}: carries YAML frontmatter — agents "
                f"must be .py (SUBAGENT dict) + prose-only .md"))
    return v


def check_harness() -> list[Violation]:
    """Rule 6 (no hardcoded org->agent manifest) + rule 7 (no orphan agents)
    + permanent legacy tripwires. Global — not per-org."""
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
    v.extend(_no_legacy_agent_frontmatter())
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
