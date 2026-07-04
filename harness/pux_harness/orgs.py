"""Org + specialist-agent loading for the deepagents harness.

Mirrors pi-mono's shape so the org IP ports verbatim:
  - system_prompt = root AGENTS.md + orgs/<name>/AGENTS.md + harness addendum
  - SubAgents[]   = .pi/agents/<name>.md  -> deepagents SubAgent dicts

The harness addendum corrects the one terminology drift between pi-mono and
deepagents: pi-mono delegates via `subagent(agent, task)`; deepagents
delegates via the `task` tool with `subagent_type=<name>`. The org overlays
are left intact (shared with the pi-mono path) and the addendum bridges the
two — verified in Phase 0 that deepagents' injected `task`-tool guidance
already wins, so this is belt-and-suspenders, not load-bearing.

Phase 10: the frontmatter parser is real YAML (``yaml.safe_load``), so the
full deepagents ``SubAgent`` vocabulary is expressible. ``load_subagents``
resolves the rich optional fields — ``model`` (via our router), ``skills``
(container skills-ROOT paths — see ``_resolve_skills``), ``response_format``
(JSON-schema dict), ``permissions`` (``FilesystemPermission`` list),
``interrupt_on`` (``dict[str,bool]``). See the resolution table in the repo
``CLAUDE.md``. ``middleware`` is deliberately NOT passed:
``SubAgentMiddleware`` does not forward a raw spec's ``middleware`` key into
the compiled specialist (Phase 7), so setting it would be a silent no-op;
subagent middleware is a ``CompiledSubAgent`` pre-compilation concern.
"""
from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml
from deepagents import FilesystemPermission
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
    """A frontmatter list value -> stripped non-empty items. Accepts either a
    YAML list (``[a, b]``) or the historical comma-separated scalar
    (``agents: a,b`` / ``tools: mcp:pux-sandbox/python, read_file``) so the
    existing frontmatter parses unchanged under real YAML."""
    if raw is None:
        return []
    if isinstance(raw, list):
        return [str(s).strip() for s in raw if str(s).strip()]
    return [s.strip() for s in str(raw).split(",") if s.strip()]


def discover_orgs() -> list[str]:
    """Sorted names of every org dir containing ``AGENTS.md``. Data-driven —
    no hardcoded manifest. The org -> agent map lives in each org's
    ``agents:`` frontmatter (see ``org_agent_slugs``). ``_shared`` and other
    bundles without an AGENTS.md are excluded by the presence rule."""
    out: list[str] = []
    orgs = _orgs_dir()
    if not orgs.is_dir():
        return out
    for child in sorted(orgs.iterdir()):
        if child.is_dir() and (child / "AGENTS.md").is_file():
            out.append(child.name)
    return out


def org_agent_slugs(name: str) -> list[str]:
    """The specialist slugs this org delegates to, read from its
    ``AGENTS.md`` ``agents:`` frontmatter (scalar or YAML list)."""
    fm, _ = _split_frontmatter((_orgs_dir() / name / "AGENTS.md").read_text())
    return _parse_list(fm.get("agents"))


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
    """Map an agent's ``tools`` frontmatter value to specialist StructuredTools.

    Each entry is ``mcp:<server>/<tool>``; we take the part after the last ``/``
    and look up ``pux_sandbox_<tool>``. Unknown tools fail loud (no silent
    skip) — a stale frontmatter reference is a real bug. Native fs tools
    (``execute``/``read_file``/…) are NOT resolved here — they come from the
    backend's ``FilesystemMiddleware`` for every subagent regardless of its
    ``tools:`` whitelist, so they never belong in the specialist whitelist.
    """
    resolved: list[BaseTool] = []
    for entry in _parse_list(raw):
        tool_name = entry.rsplit("/", 1)[-1]
        key = "pux_sandbox_" + tool_name
        if key not in tool_map:
            raise KeyError(
                f"agent frontmatter references unknown tool {entry!r} "
                f"(resolved {key!r}, not in the specialist tool map)"
            )
        resolved.append(tool_map[key])
    return resolved


def _resolve_skills(raw: Any, slug: str) -> list[str]:
    """Frontmatter ``skills`` value -> container-absolute skills-ROOT paths.

    deepagents' ``SkillsMiddleware`` resolves each source against the BACKEND
    (the sandbox container) and loads EVERY ``<source>/<skill>/SKILL.md``
    beneath it — a source is a skills **root** directory, not an individual
    skill (passing an individual skill dir loads nothing: its only child is the
    SKILL.md *file*). So a frontmatter value is a **project-relative** directory
    (e.g. ``.pi/skills``); we validate it exists on the host (the project is
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


def _resolve_permissions(raw: Any, slug: str) -> list[FilesystemPermission]:
    """Frontmatter ``permissions`` (list of mappings) -> ``FilesystemPermission``
    objects. Construction runs deepagents' own path validation (``__post_init__``
    rejects paths without a leading ``/`` or containing ``..``/``~``), so a bad
    rule fails at load time, not at a later tool call.
    """
    if not isinstance(raw, list):
        msg = (f"{slug}: permissions must be a list of mappings, "
               f"got {type(raw).__name__}")
        raise ValueError(msg)
    out: list[FilesystemPermission] = []
    for entry in raw:
        if not isinstance(entry, dict):
            msg = f"{slug}: each permission must be a mapping, got {type(entry).__name__}"
            raise ValueError(msg)
        out.append(FilesystemPermission(**entry))
    return out


def _resolve_interrupt_on(raw: Any, slug: str) -> dict[str, bool]:
    """Frontmatter ``interrupt_on`` -> ``dict[str, bool]``.

    Only the tool-name -> bool toggle form is supported. The
    ``InterruptOnConfig`` (``allowed_decisions``) form is deferred; a non-bool
    value raises so a malformed rule never silently becomes a no-op.
    """
    if not isinstance(raw, dict) or not all(isinstance(v, bool) for v in raw.values()):
        msg = (f"{slug}: interrupt_on must be a mapping of tool-name -> bool, "
               f"got {raw!r}")
        raise ValueError(msg)
    return raw


def load_subagents(org: str, all_tools: list[BaseTool]) -> list[dict[str, Any]]:
    """Build deepagents SubAgent dicts for ``org``'s specialists.

    Each ``.pi/agents/<name>.md`` -> ``{name, description, system_prompt,
    tools?}`` plus any rich optional fields present in its frontmatter (Phase
    10). ``tools`` comes from the frontmatter whitelist (mapped to the
    specialist tool map); omitted means inherit the main agent's tools. ``model``
    omitted means inherit the main agent's model (``create_deep_agent`` itself
    injects the parent model — ``deepagents/graph.py`` ``spec.get("model",
    model)``); when set, it resolves through OUR ``get_model`` so the bare
    shorthand (e.g. ``glm-5.2``) routes via the OpenCode Zen Go router (deepagents'
    ``init_chat_model`` can't carry our base_url/api_key).

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
        fm, body = _split_frontmatter((_agents_dir() / f"{slug}.md").read_text())
        sub: dict[str, Any] = {
            "name": fm.get("name", slug),
            "description": fm.get("description", slug),
            "system_prompt": body,
        }
        if fm.get("tools"):
            sub["tools"] = _resolve_tools(fm["tools"], tool_map)
        # Phase 10: rich SubAgent fields. Each resolver fails loud on a
        # malformed value; a field absent from the frontmatter is simply not
        # set, so deepagents applies its own default (inherit / empty).
        if "model" in fm:
            sub["model"] = get_model(fm["model"])
        if "skills" in fm:
            sub["skills"] = _resolve_skills(fm["skills"], slug)
        if "response_format" in fm:
            sub["response_format"] = fm["response_format"]
        if "permissions" in fm:
            sub["permissions"] = _resolve_permissions(fm["permissions"], slug)
        if "interrupt_on" in fm:
            sub["interrupt_on"] = _resolve_interrupt_on(fm["interrupt_on"], slug)
        subs.append(sub)
    return subs
