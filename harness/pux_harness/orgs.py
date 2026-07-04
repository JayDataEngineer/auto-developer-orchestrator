"""Org + specialist-agent loading for the deepagents harness.

System prompt = root AGENTS.md + orgs/<name>/AGENTS.md + harness addendum.
Subagents come from ``.pi/agents/<slug>`` — a Python module (``<slug>.py``
exporting a ``SUBAGENT`` dict, the deepagents-idiomatic form) plus a sibling
``<slug>.md`` holding the system-prompt prose. The org roster is
``orgs/<name>/org.yaml`` (``agents: [slug, ...]``); ``AGENTS.md`` is pure
CTO-prompt prose (no frontmatter) — restored to its conventional role as agent
*context* rather than a config vehicle.

The harness addendum corrects the one terminology drift between pi-mono and
deepagents: pi-mono delegates via ``subagent(agent, task)``; deepagents
delegates via the ``task`` tool with ``subagent_type=<name>``.

**Subagent modules (``.pi/agents/*.py``) MUST import only the stdlib** — no
``pux_harness.*`` / Docker — so they stay loadable under ``--check-contract``
(offline, no Docker/tokens). Tool + skills resolution stays CENTRAL (here, via
``_resolve_tools`` / ``_resolve_skills``) precisely so the modules stay
decoupled and CI-safe. ``importlib`` loads each module by path (``.pi/`` is NOT
on ``sys.path`` and stays off); each module's body reads its sibling ``.md``.
"""
from __future__ import annotations

import importlib.util
from pathlib import Path
from typing import Any

import yaml
from langchain_core.tools import BaseTool

from pux_harness.model import get_model

PROJECT_ROOT = Path(__file__).resolve().parents[2]

# Container bind-mount target (container.py: ``<project>:/sandbox/workspace``).
# Skills sources are mapped to container-absolute paths under this root for
# deepagents' SkillsMiddleware (which resolves them against the backend).
_WORKSPACE_ROOT = "/sandbox/workspace"


# --- path helpers (injectable — tests monkeypatch these) -------------------
# Single source of truth for where orgs/agents live. ``contract.py``
# re-exports these; the contract tests monkeypatch them at both module sites.

def _orgs_dir() -> Path:
    return PROJECT_ROOT / "orgs"


def _agents_dir() -> Path:
    return PROJECT_ROOT / ".pi" / "agents"


def _read(rel: str) -> str:
    """Read a project-relative file (used for the root ``AGENTS.md`` only —
    org/agent/skill reads go through the injectable helpers above so the loader
    is testable via monkeypatch)."""
    return (PROJECT_ROOT / rel).read_text()


def _parse_list(raw: Any) -> list[str]:
    """A list value -> stripped non-empty items. Accepts either a YAML list
    (``[a, b]``) or a comma-separated scalar (``agents: a,b``)."""
    if raw is None:
        return []
    if isinstance(raw, list):
        return [str(s).strip() for s in raw if str(s).strip()]
    return [s.strip() for s in str(raw).split(",") if s.strip()]


def discover_orgs() -> list[str]:
    """Sorted names of every org dir containing ``AGENTS.md``. Data-driven —
    no hardcoded manifest. An org's specialist roster lives in its
    ``org.yaml``. ``_shared`` and other bundles without an
    AGENTS.md are excluded by the presence rule."""
    out: list[str] = []
    orgs = _orgs_dir()
    if not orgs.is_dir():
        return out
    for child in sorted(orgs.iterdir()):
        if child.is_dir() and (child / "AGENTS.md").is_file():
            out.append(child.name)
    return out


def org_agent_slugs(name: str) -> list[str]:
    """The specialist slugs this org delegates to, read from
    ``orgs/<name>/org.yaml``."""
    org_dir = _orgs_dir() / name
    manifest = org_dir / "org.yaml"
    if not manifest.is_file():
        return []
    data = yaml.safe_load(manifest.read_text()) or {}
    if not isinstance(data, dict):
        msg = f"{name}/org.yaml: top level must be a mapping, got {type(data).__name__}"
        raise ValueError(msg)
    return _parse_list(data.get("agents"))


_ADDENDUM = """\

## Harness addendum (deepagents) — authoritative

You are running under the Python deepagents harness, not pi-mono. Where this
addendum conflicts with the org docs above, THIS ADDENDUM wins.

- **Delegation:** ignore any `subagent(agent, task)` wording. Delegate with
  the `task` tool: `task(subagent_type="<name>", description="<what to
  do>")`. The subagents available to you are listed in the `task` tool's own
  description. The subagent sees only your `description`, not your
  conversation — give it enough context (relevant paths, the question, the
  expected output shape).
- **File/shell surface:** the file and shell tools are the NATIVE deepagents
  tools — `execute` (run a shell command), `read_file`, `write_file`,
  `edit_file`, `glob`, `grep`, `ls`. There is NO `pux_sandbox_bash` or
  `pux_sandbox_file_*`. Anywhere the org docs say `pux_sandbox_bash`, use
  `execute`; `pux_sandbox_file_read` -> `read_file`; `pux_sandbox_file_glob`
  -> `glob`; `pux_sandbox_file_grep` -> `grep`; and so on. Specialist
  capabilities remain under `pux_sandbox_*` (`pux_sandbox_python`,
  `pux_sandbox_browser_*`, `pux_sandbox_desktop_*`, `pux_sandbox_describe_image`,
  `pux_sandbox_list_skills`, `pux_sandbox_load_skill`). The workspace is at
  `/sandbox/workspace/` inside the sandbox container — the project root,
  bind-mounted. You and every subagent share this same surface.
"""


def _split_frontmatter(text: str) -> tuple[dict[str, Any], str]:
    """Split a ``.md`` file into ``(frontmatter, body)``.

    Frontmatter is the optional leading ``---``-delimited YAML block, parsed
    with ``yaml.safe_load`` (so lists + nested mappings work — Phase 10 retired
    the scalar-only hand-rolled parser). Body is the markdown after the closing
    ``---``. No frontmatter -> ``({}, body)``. A non-mapping frontmatter block
    or a YAML syntax error raises ``ValueError`` (fail loud — the old parser
    silently produced junk values).

    Still used for the root ``AGENTS.md`` + SKILL.md (contract rule 8) —
    the migrated ``.pi/agents/*.md`` (prose only) and ``org.yaml`` paths
    no longer go through it.
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
        raise ValueError(msg)
    return fm, body.strip()


def load_root_prompt() -> str:
    """Body of the root AGENTS.md (the base 'Pux' system prompt)."""
    return _split_frontmatter(_read("AGENTS.md"))[1]


def load_org_prompt(name: str) -> str:
    """Body of orgs/<name>/AGENTS.md (the per-org CTO overlay)."""
    return _split_frontmatter((_orgs_dir() / name / "AGENTS.md").read_text())[1]


def build_system_prompt(org: str) -> str:
    """root AGENTS.md + org overlay + harness addendum (mirrors pi-mono's
    append-org-to-root assembly, plus the deepagents terminology bridge)."""
    return f"{load_root_prompt()}\n\n{load_org_prompt(org)}{_ADDENDUM}"


def _resolve_tools(raw: Any, tool_map: dict[str, BaseTool]) -> list[BaseTool]:
    """Map an agent's ``tools`` list to specialist StructuredTools.

    Each entry is a bare slug (``"python"``); we prepend ``pux_sandbox_`` and
    look up the tool map. Unknown tools fail loud (no silent skip) — a stale
    reference is a real bug. Native fs tools (``execute``/``read_file``/…) are
    NOT resolved here — they come from the backend's ``FilesystemMiddleware``
    for every subagent regardless of its ``tools:`` whitelist, so they never
    belong in the specialist whitelist.
    """
    resolved: list[BaseTool] = []
    for entry in _parse_list(raw):
        tool_name = entry.rsplit("/", 1)[-1]
        key = "pux_sandbox_" + tool_name
        if key not in tool_map:
            raise KeyError(
                f"agent references unknown tool {entry!r} "
                f"(resolved {key!r}, not in the specialist tool map)"
            )
        resolved.append(tool_map[key])
    return resolved


def _resolve_skills(raw: Any, slug: str) -> list[str]:
    """``skills`` value -> container-absolute skills-ROOT paths.

    deepagents' ``SkillsMiddleware`` resolves each source against the BACKEND
    (the sandbox container) and loads EVERY ``<source>/<skill>/SKILL.md``
    beneath it — a source is a skills **root** directory, not an individual
    skill (passing an individual skill dir loads nothing: its only child is the
    SKILL.md *file*). So a value is a **project-relative** directory (e.g.
    ``.pi/skills``); we validate it exists on the host (the project is
    bind-mounted 1:1 at ``/sandbox/workspace``, so host existence == container
    existence) and map it to a container-absolute path for deepagents.

    E2E-proven: ``backend.ls('/sandbox/workspace/.pi/skills')`` lists
    ``source-citation``; the middleware then reads its ``SKILL.md``.
    """
    out: list[str] = []
    project = _orgs_dir().parent
    for p in _parse_list(raw):
        if not isinstance(p, str) or not p:
            msg = f"{slug}: each skills source must be a non-empty path string"
            raise ValueError(msg)
        if p.startswith("/") or ".." in Path(p).parts:
            msg = f"{slug}: skills source must be project-relative (got {p!r})"
            raise ValueError(msg)
        if not (project / p).is_dir():
            raise KeyError(
                f"{slug}: skills source {p!r} -> no such directory under the "
                f"project root")
        out.append(f"{_WORKSPACE_ROOT}/{p}")
    return out


def _load_agent_module(slug: str) -> Any:
    """Load ``.pi/agents/<slug>.py`` by path and return its module object, or
    ``None`` if no ``.py`` exists (the caller, ``load_subagents``, raises —
    there is NO legacy ``.md`` fallback; the ``no-legacy-agent-frontmatter``
    contract tripwire guarantees every roster slug has a ``.py``).

    Path-loaded (``importlib.util.spec_from_file_location``) — ``.pi/`` is NOT
    on ``sys.path`` and this never adds it; the module's ``__file__`` is set so
    its body can read the sibling ``<slug>.md`` for the system-prompt prose.
    The module MUST export a ``SUBAGENT`` dict and import only the stdlib (no
    ``pux_harness.*`` / Docker) so it stays loadable under ``--check-contract``
    (offline). A missing sibling ``.md`` raises at ``exec_module`` (fail loud).
    """
    path = _agents_dir() / f"{slug}.py"
    if not path.is_file():
        return None
    spec = importlib.util.spec_from_file_location(f"_pi_agent_{slug}", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def _build_sub(
    slug: str, spec: dict[str, Any], tool_map: dict[str, BaseTool], system_prompt: str
) -> dict[str, Any]:
    """Build a deepagents SubAgent dict from a spec mapping (the module's
    ``SUBAGENT`` dict). ``system_prompt`` is passed in explicitly.

    Omitted ``tools`` -> inherit the main agent's tools; omitted ``model`` ->
    inherit the parent model (``create_deep_agent`` injects it).
    """
    sub: dict[str, Any] = {
        "name": spec.get("name", slug),
        "description": spec.get("description", slug),
        "system_prompt": system_prompt,
    }
    if spec.get("tools"):
        sub["tools"] = _resolve_tools(spec["tools"], tool_map)
    if "model" in spec:
        sub["model"] = get_model(spec["model"])
    if "skills" in spec:
        sub["skills"] = _resolve_skills(spec["skills"], slug)
    return sub


def load_subagents(org: str, all_tools: list[BaseTool]) -> list[dict[str, Any]]:
    """Build deepagents SubAgent dicts for ``org``'s specialists.

    For each slug in ``org.yaml``, load ``.pi/agents/<slug>.py`` and build
    from its ``SUBAGENT`` dict. The module reads its sibling ``.md`` for the
    system-prompt prose.

    No ``middleware`` key: deepagents' ``SubAgentMiddleware`` does not forward a
    raw spec's ``middleware`` key into the compiled specialist (verified in the
    Phase 7 E2E), so setting it would be a silent no-op. Context-offload runs on
    the main agent only; see ``context_offload.py`` for the rationale + how to
    add subagent offload properly later (CompiledSubAgent pre-compilation).
    """
    if org not in discover_orgs():
        raise KeyError(f"unknown org {org!r}; discovered orgs: {discover_orgs()}")
    tool_map: dict[str, BaseTool] = {t.name: t for t in all_tools}
    subs: list[dict[str, Any]] = []
    for slug in org_agent_slugs(org):
        mod = _load_agent_module(slug)
        if mod is None:
            raise FileNotFoundError(
                f".pi/agents/{slug}.py not found — agents must be .py modules")
        spec = mod.SUBAGENT
        sub = _build_sub(slug, spec, tool_map, spec["system_prompt"])
        subs.append(sub)
    return subs
