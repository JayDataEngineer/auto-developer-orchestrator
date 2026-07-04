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

* **Harness-level** (rule 6) and **global** (rule 7):
  6. No hardcoded org->agent manifest in the harness source.
  7. No orphan agents (every specialist owned by >=1 org).

Rule 4 resolves against the *static* native surface: the specialist names are
a Python frozenset (the single source of truth shared with ``graph.py``), so a
stale ``tools:`` reference fails loud here without any process to probe.
"""
from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path

import yaml

from pux_harness import policy as policy_mod
from pux_harness.native_tools import SPECIALIST_TOOL_NAMES
from pux_harness.orgs import (
    PROJECT_ROOT,
    _split_frontmatter,
    discover_orgs,
    org_agent_slugs,
)

# --- the contract vocabulary ----------------------------------------------

# Org-level AGENTS.md frontmatter may use only this key. It declares the
# specialist slugs the org delegates to (comma-separated), replacing the old
# hardcoded ``ORG_AGENTS`` manifest.
KNOWN_ORG_KEYS: frozenset[str] = frozenset({"agents"})

# ``.pi/agents/<slug>.md`` frontmatter may use only these keys (pi-mono shape).
KNOWN_AGENT_KEYS: frozenset[str] = frozenset({
    "name", "description", "tools", "systemPromptMode", "output",
    "inheritSkills", "inheritProjectContext", "defaultProgress",
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


def _orgs_dir() -> Path:
    return PROJECT_ROOT / "orgs"


def _agents_dir() -> Path:
    return PROJECT_ROOT / ".pi" / "agents"


def _parse_list(raw: str) -> list[str]:
    """Comma-separated scalar -> stripped non-empty items (the ``agents:`` and
    ``tools:`` frontmatter shape, consistent with pi-mono's ``tools:`` line)."""
    return [s.strip() for s in raw.split(",") if s.strip()]


# --- per-org checks (rules 1-5) ------------------------------------------


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


def check_all() -> dict[str, list[Violation]]:
    """Per-org violations for every discovered org. Global checks live in
    ``check_harness()``; the CLI runs both."""
    return {org: check_org(org) for org in discover_orgs()}


def has_errors(violations: list[Violation]) -> bool:
    return any(x.severity == "error" for x in violations)
