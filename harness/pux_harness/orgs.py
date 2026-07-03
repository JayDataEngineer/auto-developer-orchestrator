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
"""
from __future__ import annotations

from pathlib import Path
from typing import Any

from langchain_core.tools import BaseTool

PROJECT_ROOT = Path(__file__).resolve().parents[2]

def discover_orgs() -> list[str]:
    """Sorted names of every org dir containing ``AGENTS.md``. Data-driven —
    no hardcoded manifest. The org -> agent map lives in each org's
    ``agents:`` frontmatter (see ``org_agent_slugs``). ``_shared`` and other
    bundles without an AGENTS.md are excluded by the presence rule."""
    out: list[str] = []
    for child in sorted((PROJECT_ROOT / "orgs").iterdir()):
        if child.is_dir() and (child / "AGENTS.md").is_file():
            out.append(child.name)
    return out


def org_agent_slugs(name: str) -> list[str]:
    """The specialist slugs this org delegates to, read from its
    ``AGENTS.md`` ``agents:`` frontmatter (comma-separated)."""
    fm, _ = _split_frontmatter(_read(f"orgs/{name}/AGENTS.md"))
    return [s.strip() for s in fm.get("agents", "").split(",") if s.strip()]

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


def _split_frontmatter(text: str) -> tuple[dict[str, str], str]:
    """Minimal frontmatter parse — pull scalar `key: value` fields only."""
    fm: dict[str, str] = {}
    if text.startswith("---"):
        _, head, body = text.split("---", 2)
        for line in head.strip().splitlines():
            if ":" in line:
                key, _, val = line.partition(":")
                fm[key.strip()] = val.strip().strip('"').strip("'")
        return fm, body.strip()
    return fm, text.strip()


def _read(rel: str) -> str:
    return (PROJECT_ROOT / rel).read_text()


def load_root_prompt() -> str:
    """Body of the root AGENTS.md (the base 'Pux' system prompt)."""
    return _split_frontmatter(_read("AGENTS.md"))[1]


def load_org_prompt(name: str) -> str:
    """Body of orgs/<name>/AGENTS.md (the per-org CTO overlay)."""
    return _split_frontmatter(_read(f"orgs/{name}/AGENTS.md"))[1]


def build_system_prompt(org: str) -> str:
    """root AGENTS.md + org overlay + harness addendum (mirrors pi-mono's
    append-org-to-root assembly, plus the deepagents terminology bridge)."""
    return f"{load_root_prompt()}\n\n{load_org_prompt(org)}{_ADDENDUM}"


def _resolve_tools(spec: str, bridge: dict[str, BaseTool]) -> list[BaseTool]:
    """Map a pi-mono `tools` frontmatter line to bridge StructuredTools.

    Each entry is `mcp:<server>/<tool>`; we take the part after the last `/`
    and look up `pux_sandbox_<tool>`. Unknown tools fail loud (no silent
    skip) — a stale frontmatter reference is a real bug.
    """
    resolved: list[BaseTool] = []
    for raw in spec.split(","):
        raw = raw.strip()
        if not raw:
            continue
        tool_name = raw.rsplit("/", 1)[-1]
        key = "pux_sandbox_" + tool_name
        if key not in bridge:
            raise KeyError(
                f"agent frontmatter references unknown tool {raw!r} "
                f"(resolved {key!r}, not in bridge tools)"
            )
        resolved.append(bridge[key])
    return resolved


def load_subagents(org: str, all_tools: list[BaseTool]) -> list[dict[str, Any]]:
    """Build deepagents SubAgent dicts for `org`'s specialists.

    Each `.pi/agents/<name>.md` -> {name, description, system_prompt, tools?}.
    `tools` comes from the frontmatter whitelist (mapped to bridge tools);
    omitted means inherit the main agent's tools. `model` is omitted so
    specialists inherit the main agent's model (mimo), matching pi-mono.
    """
    if org not in discover_orgs():
        raise KeyError(f"unknown org {org!r}; discovered orgs: {discover_orgs()}")
    bridge: dict[str, BaseTool] = {t.name: t for t in all_tools}
    subs: list[dict[str, Any]] = []
    for slug in org_agent_slugs(org):
        fm, body = _split_frontmatter(_read(f".pi/agents/{slug}.md"))
        sub: dict[str, Any] = {
            "name": fm.get("name", slug),
            "description": fm.get("description", slug),
            "system_prompt": body,
        }
        if fm.get("tools"):
            sub["tools"] = _resolve_tools(fm["tools"], bridge)
        subs.append(sub)
    return subs
