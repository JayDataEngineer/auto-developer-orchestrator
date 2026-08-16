"""The ``capabilities:`` declaration sugar — the desugarer (CU-3).

ONE opt-in key, accepted in TWO homes, routed by ``kind`` to the kind-specific
declaration the leaf resolvers already read. PURE SUGAR — the leaf resolvers
(``classify_slug`` / ``resolve_tool_servers`` / ``supervisor_skills_roots``) are
unchanged; PURE DATA — takes parsed dicts, returns desugared data; no file I/O.

``mcp`` spans both homes: the org arms the server (transport/egress), the agent
routes a focused subset (two-level grant gate). ``tool`` / ``skill`` are
per-agent only; ``middleware`` / ``job`` are not in the sugar surface.
"""
from __future__ import annotations

from typing import Any

__all__ = [
    "AGENT_CAPABILITY_KINDS",
    "ORG_CAPABILITY_KINDS",
    "CapabilitiesSugarError",
    "desugar_agent_capabilities",
    "org_mcp_items_from_dict",
]


# The kinds each home accepts. ``tool`` / ``skill`` are per-agent ONLY;
# ``mcp`` is the one kind valid in BOTH homes (org arms the server; agent routes
# a focused subset — see the module docstring's two-level model). These are the
# CU-3 sugar surface: the three model add-on channels with a clean ``ref``
# semantics. ``middleware`` / ``job`` are deferred (no ref-catalog).
AGENT_CAPABILITY_KINDS = ("tool", "skill", "mcp")
ORG_CAPABILITY_KINDS = ("mcp",)


class CapabilitiesSugarError(ValueError):
    """A malformed ``capabilities:`` block — wrong kind for its home, bad shape,
    or an unknown kind. Raised loud (the contract checker surfaces it as a
    dedicated ``capabilities-sugar-*`` violation); never silently skipped."""


def _coerce_str_list(raw: Any, *, where: str) -> list[str]:
    """Coerce a frontmatter ``tools`` / ``skills`` value (list OR comma-string)
    to a ``list[str]`` — the SAME coercion ``_resolve_tools`` /
    ``_resolve_skills`` accept, so the desugared form composes with the legacy
    keys during the CU-3 transition."""
    if raw is None:
        return []
    if isinstance(raw, str):
        return [s.strip() for s in raw.split(",") if s.strip()]
    if isinstance(raw, list):
        return [str(s) for s in raw]
    raise CapabilitiesSugarError(
        f"{where}: tools/skills must be a list or comma-string, "
        f"got {type(raw).__name__}"
    )


def desugar_agent_capabilities(
    fm: dict[str, Any], slug: str
) -> dict[str, Any]:
    """Expand a frontmatter ``capabilities:`` block into ``tools:`` / ``skills:``
    / ``mcp:`` on ``fm`` (returns the merged dict). Accepts ``kind ∈ {tool,
    skill, mcp}``; ``middleware`` / ``job`` raise ``CapabilitiesSugarError``.

    ``kind == mcp`` desugars to a ``mcp:`` key (NOT ``tools:``) — same shape
    ``org_mcp_items_from_dict`` produces. ``capabilities:`` adds to any explicit
    ``tools:`` / ``skills:`` on ``fm``, then is popped. Runs in
    ``_load_agent_spec`` BEFORE the ``extends:`` merge. No ``mcp`` / ``mcp_add``
    delta vocabulary yet — declare ``mcp:`` on the leaf agent.
    """
    block = fm.get("capabilities")
    if block is None:
        return fm
    if not isinstance(block, list):
        raise CapabilitiesSugarError(
            f"agent {slug!r}: capabilities: must be a list of mappings, "
            f"got {type(block).__name__}"
        )
    where = f"agent {slug!r}"
    tools = _coerce_str_list(fm.get("tools"), where=where)
    skills = _coerce_str_list(fm.get("skills"), where=where)
    mcp_items: list[Any] = []
    for i, entry in enumerate(block):
        if not isinstance(entry, dict):
            raise CapabilitiesSugarError(
                f"agent {slug!r}: capabilities[{i}] must be a mapping, "
                f"got {type(entry).__name__}"
            )
        kind = entry.get("kind")
        ref = entry.get("ref")
        if kind not in AGENT_CAPABILITY_KINDS:
            raise CapabilitiesSugarError(
                f"agent {slug!r}: capabilities[{i}] kind={kind!r} is not in the "
                f"agent frontmatter sugar surface; accepts {list(AGENT_CAPABILITY_KINDS)} "
                f"(middleware/job stay in profile.yaml/policy.yaml)"
            )
        if not isinstance(ref, str) or not ref.strip():
            raise CapabilitiesSugarError(
                f"agent {slug!r}: capabilities[{i}] kind={kind!r} ref must be a "
                f"non-empty string"
            )
        ref = ref.strip()
        if kind == "mcp":
            # The one kind with an optional per-tool allowlist → ``mcp:`` key, in
            # the SAME shape ``org_mcp_items_from_dict`` emits (bare ref string,
            # or ``{ref, tools}``). Routed by ``_resolve_mcp``, NOT ``_resolve_tools``.
            allow = entry.get("allowlist", entry.get("tools"))
            if allow is None:
                mcp_items.append(ref)
            elif isinstance(allow, list):
                mcp_items.append({"ref": ref, "tools": [str(t) for t in allow]})
            else:
                raise CapabilitiesSugarError(
                    f"agent {slug!r}: capabilities[{i}] mcp allowlist must be a "
                    f"list, got {type(allow).__name__}"
                )
            continue
        target = tools if kind == "tool" else skills
        if ref not in target:
            target.append(ref)
    if tools:
        fm["tools"] = tools
    else:
        fm.pop("tools", None)
    if skills:
        fm["skills"] = skills
    else:
        fm.pop("skills", None)
    if mcp_items:
        fm["mcp"] = mcp_items
    else:
        fm.pop("mcp", None)
    fm.pop("capabilities", None)  # consumed — the leaves never see it
    return fm


def org_mcp_items_from_dict(
    data: dict[str, Any] | None, org: str
) -> list[Any]:
    """The ``mcp`` entries from an ``org.yaml`` ``capabilities:`` block, in the
    shape ``resolve_tool_servers`` consumes. Empty when absent. Accepts ONLY
    ``kind == mcp``; other kinds raise. ``data`` is the parsed mapping (or None).
    """
    if data is None:
        return []
    block = data.get("capabilities")
    if not block:
        return []
    if not isinstance(block, list):
        raise CapabilitiesSugarError(
            f"{org}/org.yaml: capabilities: must be a list of mappings, "
            f"got {type(block).__name__}"
        )
    items: list[Any] = []
    for i, entry in enumerate(block):
        if not isinstance(entry, dict):
            raise CapabilitiesSugarError(
                f"{org}/org.yaml: capabilities[{i}] must be a mapping, "
                f"got {type(entry).__name__}"
            )
        kind = entry.get("kind")
        if kind == "mcp":
            ref = entry.get("ref")
            if not isinstance(ref, str) or not ref.strip():
                raise CapabilitiesSugarError(
                    f"{org}/org.yaml: capabilities[{i}] mcp ref must be a "
                    f"non-empty string"
                )
            allow = entry.get("allowlist", entry.get("tools"))
            if allow is None:
                items.append(ref.strip())
            else:
                if not isinstance(allow, list):
                    raise CapabilitiesSugarError(
                        f"{org}/org.yaml: capabilities[{i}] mcp allowlist "
                        f"must be a list, got {type(allow).__name__}"
                    )
                items.append({"ref": ref.strip(), "tools": [str(t) for t in allow]})
        elif kind in AGENT_CAPABILITY_KINDS:
            raise CapabilitiesSugarError(
                f"{org}/org.yaml: capabilities[{i}] kind={kind!r} is a per-agent "
                f"kind — declare it in agent frontmatter, not org.yaml"
            )
        else:
            raise CapabilitiesSugarError(
                f"{org}/org.yaml: capabilities[{i}] kind={kind!r} is not in the "
                f"sugar surface; org.yaml accepts {list(ORG_CAPABILITY_KINDS)} "
                f"(middleware/job stay in profile.yaml/policy.yaml)"
            )
    return items
