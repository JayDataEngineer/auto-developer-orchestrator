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

**Phase 17.B — RubricMiddleware verify-gate.** An org may add a ``rubric:``
block to its ``profile.yaml`` to opt into a post-agent grader loop (deepagents'
beta ``RubricMiddleware``). The block is peeled out of the YAML BEFORE
``HarnessProfileConfig.from_dict`` (which rejects unknown keys), so the
deepagents schema stays untouched, and surfaced via ``load_rubric_gate`` +
``default_rubric``. ``load_profile``'s signature is unchanged on purpose: zero
ripple to existing callers/tests (the gate is wired separately in
``graph.build_graph``). The grader IS the tester + reviewer — one non-skippable
gate rather than a subagent the CTO might skip.
"""
from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml
from deepagents import HarnessProfileConfig
from deepagents._tools import _apply_tool_description_overrides
from langchain_core.tools import BaseTool

from pux_harness.agent import orgs as _orgs_mod
from pux_harness.agent.model import ROLE_KEYS

__all__ = [
    "RubricGate",
    "load_profile",
    "load_rubric_gate",
    "default_rubric",
    "validate_profile",
    "apply_profile_to_tools",
]


def _profile_path(org: str) -> Path:
    # Resolve via the orgs module at CALL time (not an import-time binding) so
    # the contract tests' monkeypatch of ``orgs._orgs_dir`` reaches this module
    # too — same single-source-of-truth discipline contract.py relies on.
    # Specialists-aware (orgs/<org> then orgs/specialists/<org>) but NON-raising:
    # callers (``_read_profile_yaml``) handle a missing file as ``None``, so an
    # unknown org yields a non-existent path rather than ``FileNotFoundError``.
    base = _orgs_mod._orgs_dir()
    top = base / org
    if top.is_dir():
        return top / "profile.yaml"
    return base / "specialists" / org / "profile.yaml"


@dataclass(frozen=True)
class RubricGate:
    """Per-org ``RubricMiddleware`` verify-gate config (Phase 17.B).

    An org opts into a post-agent grader loop by adding a ``rubric:`` block to
    its ``profile.yaml``. deepagents' ``RubricMiddleware`` (beta) runs the
    ``default`` rubric — a ship-gate checklist ("tests pass", "lint clean",
    "no out-of-scope changes") — using sandbox grader tools after the main agent
    finishes, returns a verdict (``satisfied`` / ``needs_revision`` /
    ``max_iterations_reached`` / ``failed`` / ``grader_error``), and the agent
    revises until ``satisfied`` or ``max_iterations`` is hit. The grader IS the
    tester + reviewer (folded into one non-skippable gate rather than a
    subagent the CTO might skip — the Phase 17 design decision).

    The block is peeled out of the YAML BEFORE ``HarnessProfileConfig.from_dict``
    (which rejects unknown keys), so the deepagents schema stays untouched. See
    ``graph.build_graph`` for the middleware wiring (it resolves the grader
    model via ``get_model(role="grader", org=org)`` — the model is NOT a field
    here; override it per-org under the top-level ``models:`` map, like any
    other role), ``tools.build_grader_tools`` for the grader's sandbox tools,
    and ``server._execute`` / ``main._run`` for the default-rubric injection.

    Beta mitigation: the gate is per-org opt-in (only orgs that add the block)
    and behind ``enabled: true``; orgs without a block are byte-identical to
    today. A future deepagents API break hits only opted-in orgs and is killed
    by flipping ``enabled: false``.
    """

    enabled: bool = True
    max_iterations: int = 3
    default: str | None = None  # the ship-gate rubric text


def _validate_models_block(org: str, data: dict) -> None:
    """Validate the top-level ``models:`` map (Phase 17.B.0).

    Keys must be ⊆ ``model.ROLE_KEYS`` (``base_model`` / ``worker_model`` /
    ``multimodal_model`` / ``grader_model``); values must be non-empty strings.
    Raises ``TypeError`` on a bad shape so a typo (``grader_modle:``) fails at
    load / contract time — otherwise the org would silently fall back to the
    shipped default and wonder why its override isn't taking. No silent skip."""
    models = data.get("models")
    if models is None:
        return
    if not isinstance(models, dict):
        msg = (
            f"{org}/profile.yaml: models: must be a mapping, "
            f"got {type(models).__name__}"
        )
        raise TypeError(msg)
    known = set(ROLE_KEYS)
    unknown = set(models) - known
    if unknown:
        msg = (
            f"{org}/profile.yaml: models: unknown key(s) {sorted(unknown)}; "
            f"valid keys: {sorted(known)}"
        )
        raise TypeError(msg)
    for key, val in models.items():
        if not isinstance(val, str) or not val:
            msg = (
                f"{org}/profile.yaml: models.{key} must be a non-empty "
                f"string, got {val!r}"
            )
            raise TypeError(msg)


def _read_profile_yaml(org: str) -> dict | None:
    """Read + parse ``orgs/<org>/profile.yaml`` -> mapping; ``None`` if absent.

    Shared by ``load_profile``, ``load_rubric_gate``, and the model-role
    resolver (``model._org_role_override``) so the file is read under ONE shape
    contract. Validates the top-level ``models:`` map (``_validate_models_block``)
    so a bad role key fails every reader, not just the model one. A non-mapping
    top level (e.g. a bare list) raises ``TypeError`` — no silent skip; a
    malformed profile is a real bug."""
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
    _validate_models_block(org, data)
    return data


def _rubric_gate_from_block(org: str, block: object) -> RubricGate:
    """Build a ``RubricGate`` from the parsed ``rubric:`` block.

    Validates shape (``enabled`` bool, ``max_iterations`` a positive int,
    ``default`` a string) so a typo fails loud at load / contract time, not at
    the first invoke. Unknown keys are rejected — in particular the legacy
    ``rubric.grader_model`` (the grader model moved to the top-level ``models:``
    map in Phase 17.B.0; surface it there as ``grader_model: <id>``). No silent
    skip — a stale form is a real bug."""
    if not isinstance(block, dict):
        msg = (
            f"{org}/profile.yaml: rubric: must be a mapping, "
            f"got {type(block).__name__}"
        )
        raise TypeError(msg)
    known = {"enabled", "max_iterations", "default"}
    unknown = set(block) - known
    if unknown:
        msg = (
            f"{org}/profile.yaml: rubric: unknown key(s) {sorted(unknown)}; "
            f"valid keys: {sorted(known)}. (grader_model moved to the top-level "
            f"`models:` map in Phase 17.B.0.)"
        )
        raise TypeError(msg)
    enabled = block.get("enabled", True)
    if not isinstance(enabled, bool):
        msg = (
            f"{org}/profile.yaml: rubric.enabled must be a bool, "
            f"got {type(enabled).__name__}"
        )
        raise TypeError(msg)
    max_iterations = block.get("max_iterations", 3)
    # bool is a subclass of int — reject it explicitly so `true` (parsed as
    # bool) isn't silently accepted as 1.
    if not isinstance(max_iterations, int) or isinstance(max_iterations, bool):
        msg = (
            f"{org}/profile.yaml: rubric.max_iterations must be an int, "
            f"got {type(max_iterations).__name__}"
        )
        raise TypeError(msg)
    if max_iterations < 1:
        msg = (
            f"{org}/profile.yaml: rubric.max_iterations must be >= 1, "
            f"got {max_iterations}"
        )
        raise ValueError(msg)
    default = block.get("default")
    if default is not None and not isinstance(default, str):
        msg = (
            f"{org}/profile.yaml: rubric.default must be a string, "
            f"got {type(default).__name__}"
        )
        raise TypeError(msg)
    return RubricGate(enabled=enabled, max_iterations=max_iterations, default=default)


def load_profile(org: str) -> HarnessProfileConfig | None:
    """Read ``orgs/<org>/profile.yaml`` -> ``HarnessProfileConfig``, or ``None``.

    ``None`` (no file) is the COMMON case — most orgs ship no profile and the
    ``build_graph`` path is byte-identical to today (the regression guarantee).
    If present, the ``rubric:`` block (Phase 17.B) and the ``models:`` map
    (Phase 17.B.0) are PEELED out before ``HarnessProfileConfig.from_dict``
    (which would otherwise reject them as unknown keys) — the rubric block is
    surfaced separately by ``load_rubric_gate``, and the models map is read by
    the model-role resolver (``model._org_role_override``). ``from_dict``
    validates the rest of the schema: unknown keys + bad shapes raise
    ``TypeError``; bad ``excluded_middleware`` grammar raises ``ValueError``.
    A non-mapping top level raises ``TypeError`` here. No silent skip — a
    malformed profile is a real bug.
    """
    data = _read_profile_yaml(org)
    if data is None:
        return None
    peeled = {k: v for k, v in data.items() if k not in ("rubric", "models")}
    return HarnessProfileConfig.from_dict(peeled)


def load_rubric_gate(org: str) -> RubricGate | None:
    """Read the ``rubric:`` block from ``orgs/<org>/profile.yaml`` -> ``RubricGate``.

    ``None`` when the org ships no ``profile.yaml`` OR no ``rubric:`` block —
    the common case (no gate, byte-identical to today). When present, the block
    is shape-validated (``_rubric_gate_from_block`` raises on a malformed
    entry). Read independently of ``load_profile`` so the gate can be wired in
    ``build_graph`` without disturbing the ``HarnessProfileConfig`` path
    (load_profile's signature stays stable → zero ripple to existing callers).
    """
    data = _read_profile_yaml(org)
    if data is None or "rubric" not in data:
        return None
    return _rubric_gate_from_block(org, data["rubric"])


def default_rubric(org: str) -> str | None:
    """The rubric text to inject at invoke time when the operator supplies none.

    Returns ``RubricGate.default`` ONLY when the gate is present + enabled + has
    a default. ``None`` otherwise (no gate, gate disabled, or no default text).
    ``None`` means ``server._execute`` / ``main._run`` skip injection, so
    ``RubricMiddleware`` does not run (its contract: "When no rubric is supplied
    on input state, the middleware does not run")."""
    gate = load_rubric_gate(org)
    if gate is None or not gate.enabled or not gate.default:
        return None
    return gate.default


def validate_profile(org: str) -> HarnessProfileConfig | None:
    """Offline contract check (no Docker, no model). Exercises BOTH loaders so
    a malformed ``rubric:`` block OR a bad ``models:`` role key fails
    ``--check-contract`` too — not just the ``HarnessProfileConfig`` schema
    (both readers route through ``_read_profile_yaml`` →
    ``_validate_models_block``). Raises on malformed; ``None`` when the org
    ships no ``profile.yaml`` (the contract checker treats absence as 'skipped',
    not a violation). Called from ``--check-contract`` for every discovered org.
    """
    cfg = load_profile(org)  # raises on a malformed non-rubric schema
    load_rubric_gate(org)    # raises on a malformed rubric: block
    return cfg


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
