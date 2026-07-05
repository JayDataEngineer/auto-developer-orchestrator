"""Per-org harness profile loader (OPTIONAL ``orgs/<org>/profile.yaml``).

A profile lets an org apply small overrides to the deepagents stack the harness
compiles for it — a system-prompt suffix (org-wide), per-tool description
rewrites, and tool exclusions. The user's stated use case: when the shared
browser agent (``orgs/_shared/agents/browser.md``) is rostered into orgs like
deep-research / twitter, an org can nudge the *whole stack* (CTO + every
specialist) without forking the agent itself.

**Why a per-org YAML and NOT the global ``_HARNESS_PROFILES`` registry.** The
registry is a flat ``dict[str, HarnessProfile]`` keyed by *model spec* (e.g.
``openai:gpt-4o``), and ``_get_harness_profile`` rejects any key with more than
one colon — it resolves only ``provider:model`` -> bare ``provider``. There is
no per-org namespace. Two orgs sharing a model (twitter + deep-research both on
``mimo-v2.5``) would merge-collide, and the long-lived ``server.py``/ACP path
builds graphs for multiple orgs in one process -> a cross-org leak. So we reuse
deepagents' own ``HarnessProfileConfig`` SCHEMA (faithful field set) but apply
its three fields directly at the ``build_graph(org)`` call site — collision-free
and server-safe. See ``graph.build_graph`` for the application, and the plan's
Phase 16.3b for the full rationale.

Path resolution calls ``orgs._orgs_dir()`` at runtime (via the module, not an
import-time binding) — single source of truth, so the contract tests'
monkeypatch of ``orgs._orgs_dir`` covers this module too.
"""
from __future__ import annotations

from pathlib import Path
from typing import Any

import yaml
from deepagents import HarnessProfileConfig
from deepagents._tools import _apply_tool_description_overrides
from langchain_core.tools import BaseTool

from pux_harness.agent import orgs as _orgs_mod

__all__ = ["load_profile", "validate_profile", "apply_profile_to_tools"]


def _profile_path(org: str) -> Path:
    # Resolve via the orgs module at CALL time (not an import-time binding) so
    # the contract tests' monkeypatch of ``orgs._orgs_dir`` reaches this module
    # too — same single-source-of-truth discipline contract.py relies on.
    return _orgs_mod._orgs_dir() / org / "profile.yaml"


def load_profile(org: str) -> HarnessProfileConfig | None:
    """Read ``orgs/<org>/profile.yaml`` -> ``HarnessProfileConfig``, or ``None``.

    ``None`` (no file) is the COMMON case — most orgs ship no profile and the
    ``build_graph`` path is byte-identical to today (the regression guarantee).
    If present, the YAML mapping is parsed by ``HarnessProfileConfig.from_dict``,
    which validates the schema: unknown keys + bad shapes raise ``TypeError``;
    bad ``excluded_middleware`` grammar raises ``ValueError``. A non-mapping
    top level (e.g. a bare list) raises ``TypeError`` here. No silent skip — a
    malformed profile is a real bug.
    """
    path = _profile_path(org)
    if not path.is_file():
        return None
    data = yaml.safe_load(path.read_text()) or {}
    if not isinstance(data, dict):
        msg = (
            f"{org}/profile.yaml: top level must be a mapping, "
            f"got {type(data).__name__}"
        )
        raise TypeError(msg)
    return HarnessProfileConfig.from_dict(data)


def validate_profile(org: str) -> HarnessProfileConfig | None:
    """Offline contract check (no Docker, no model). Raises on a malformed
    profile; ``None`` when the org ships no ``profile.yaml`` (the contract
    checker treats absence as 'skipped', not a violation). Called from
    ``--check-contract`` for every discovered org."""
    return load_profile(org)


def apply_profile_to_tools(
    tools: list[BaseTool], cfg: HarnessProfileConfig
) -> list[BaseTool]:
    """Apply ``tool_description_overrides`` + ``excluded_tools`` to a tool list.

    Used at both application sites — the MAIN agent stack (in ``build_graph``)
    and EACH subagent's resolved whitelist (in ``load_subagents``) — so an
    org-wide override reaches the browser subagent, not just the CTO.
    ``_apply_tool_description_overrides`` copies + rewrites (it never mutates
    caller-owned tools), so this is safe to call per-subagent. Filtering by
    ``tool.name`` (the prefixed ``pux_sandbox_*`` identifier the profile keys
    on)."""
    out: list[BaseTool] = tools
    if cfg.tool_description_overrides:
        out = _apply_tool_description_overrides(out, cfg.tool_description_overrides)
    if cfg.excluded_tools:
        out = [t for t in out if t.name not in cfg.excluded_tools]
    return out
