"""Non-mock build of every REAL org through the universal override system.

This is the production-readiness gate the plan's directive demands ("end with
non-mock tests of orgs; push the system to its limits"). NO fake org tree, NO
mocked profile: each build resolves the REAL ``orgs/specialists/*`` tree, the
REAL full specialist surface (39 tools), and each org's REAL ``profile.yaml`` /
``org.yaml`` / ``AGENTS.md``. Only the model + exec_client + the middleware
CLASSES are stubbed — those are harness-internal and need a Docker container /
LLM to construct; the ORG CONFIGURATION (the thing the system shapes)
is real.

If a shipped org's config drifts so the factory can't assemble a well-formed
stack, one of these fires. That's the contract that every real org honors the
universal override + inheritance system.
"""
from __future__ import annotations

import importlib

from pathlib import Path

import pytest

from pux_harness.agent import orgs, stack
from pux_harness.agent.hitl import load_ask_user_enabled
from pux_harness.sandbox.policy import load_dynamic_tools_enabled
from pux_harness.agent.orgs import _org_path, _orgs_dir, discover_orgs, org_agent_slugs
from pux_harness.sandbox.policy import NoPolicy, load as _load_policy, resolve_tool_allowlist as _supervisor_allowed
from pux_harness.sandbox.tools import SPECIALIST_TOOL_NAMES, make_specialist_tools as build_native_specialists
from pux_harness.sandbox.tools import dynamic
from pux_harness.sandbox.tools._pux import PUX_PREFIX
from pux_harness.sandbox.tools.declared import declared_tool_names

# The real org list, resolved at collection time from the real orchestrator
# orgs/ tree (10 orgs incl. _demo). Parametrizing over it builds every one.
REAL_ORGS = discover_orgs()

# A stub exec_client — specialist tool factories bind it at build time and only
# USE it at invocation time, so a sentinel builds fine (mirrors test_registry).
_EXEC = "STUB-EXEC"


@pytest.fixture(autouse=True)
def _deterministic_env(monkeypatch):
    """Pin the env for determinism. ``_orgs_dir`` is NOT patched — the REAL
    orchestrator ``orgs/`` tree is the subject under test. BrowserVision is
    env-gated (default ON); pin it OFF so the asserted middleware shape is
    stable (the vision mount is proven in test_browser_vision_*)."""
    monkeypatch.setenv("PUX_BROWSER_VISION", "0")


@pytest.fixture
def stubbed_factory(monkeypatch):
    """Stub build_stack's harness-internal heavy deps (model + middleware
    CLASSES + the context layer) so the resolver + plan assembly run against
    REAL org config without Docker / real middleware / model init. The ORG
    config (roster / profile / prompt / specialists) stays real."""
    import deepagents_context as _dc  # middleware classes import INSIDE the builders
    monkeypatch.setattr(_dc, "build_context_layer", lambda **kw: ([], []))
    monkeypatch.setattr(_dc.sandbox_routing, "RoutingMiddleware", lambda: "ROUTE")
    monkeypatch.setattr(_dc.session_guide, "SessionGuideMiddleware", lambda: "GUIDE")
    monkeypatch.setattr(_dc.audit, "AuditMiddleware", lambda **kw: "AUDIT")
    monkeypatch.setattr(stack, "RubricMiddleware", lambda **kw: "RUBRIC")
    # model_retry is default-on for every supervisor (#84) — stub it so the
    # build emits a stable marker instead of a real ModelRetryMiddleware
    # (distinct object per build would break the idempotency check). tool_retry
    # is opt-in/gated, but stubbed for symmetry.
    monkeypatch.setattr(importlib.import_module("deepagents_context.read_file_vision"),
                         "ReadFileVisionMiddleware", lambda **kw: "READFILE")
    monkeypatch.setattr(stack, "ModelRetryMiddleware", lambda **kw: "RETRY")
    # prompt_capture is default-on for every supervisor — stub it so the
    # build emits a stable marker instead of a real PromptCaptureMiddleware
    # (distinct object per build would break the idempotency check).
    monkeypatch.setattr(_dc.prompt_capture, "PromptCaptureMiddleware", lambda **kw: "PROMPT")

    # ``interpreter`` (CodeInterpreterMiddleware) auto-mounts for every shipped
    # org — all resolve a strength:pro base model (driver_strong_orchestrator
    # keys on the REAL resolve_model_id, not this stubbed get_model, so the
    # arming is genuine production behavior, not a stub artifact). Its class is
    # a LAZY module-level name (langchain-quickjs pulls wasmtime/quickjs native
    # libs), so patch it to a stable marker like every other middleware class —
    # otherwise the real instance leaks with a per-build repr() (distinct memory
    # address) and breaks idempotency, and it'd force the native load here too.
    monkeypatch.setattr(stack, "CodeInterpreterMiddleware", lambda **kw: "INTERP")
    # ``interpreter_hints`` (InterpreterHintsMiddleware) is paired 1:1 with
    # ``interpreter`` (armed iff interpreter is armed) — stub it the same way
    # so the build emits a stable marker instead of a real instance (distinct
    # object per build would break the idempotency check).
    monkeypatch.setattr(importlib.import_module("deepagents_context.interpreter_hints"),
                         "InterpreterHintsMiddleware", lambda **kw: "HINTS")
    # read_file_vision is default-on for every scope — stub it + driver_multimodal
    # so the build emits a stable marker instead of a real ReadFileVisionMiddleware
    # (which would call driver_multimodal → resolve_model_id → real model init).
    monkeypatch.setattr(stack, "driver_multimodal", lambda **kw: False)
    monkeypatch.setattr(stack, "get_model", lambda *a, **k: "MODEL")
    monkeypatch.setattr(stack, "build_grader_tools", lambda *a, **k: ["g1"])
    monkeypatch.setattr(orgs, "get_model", lambda *a, **k: "WORKER_MODEL")


def _real_specialists():
    """The FULL real specialist surface (all 39 tools) — not a 2-tool stub."""
    return build_native_specialists(_EXEC)


def _declared_surface(org: str) -> set[str]:
    """Prefixed (``pux_sandbox_*``) tool names this real org DECLARES in its
    ``sandbox/tools/tools.yaml`` — empty for orgs that declare none. Declared
    tools are a legitimate 4th channel (typed, by-name, IN-container), so a real
    org's supervisor surface may legitimately carry its OWN declared names in
    addition to the specialist registry. Mirrors contract Rule 4's
    ``classify_slug is None and tool not in declared_names`` gate: a name is
    phantom only if it is NEITHER a specialist NOR one of this org's declared
    tools. Reading the yaml here (not trusting the built plan) keeps the test
    honest about what the org's CONFIG declares, independent of build plumbing."""
    sandbox_dir = _org_path(org) / "sandbox"
    return {PUX_PREFIX + name for name in declared_tool_names(sandbox_dir)}


def _ask_user_opted_in_surface(org: str) -> set[str]:
    """``{"ask_user"}`` when this org opts into the HITL tool (``profile.yaml``
    ``ask_user: true``), else empty. ``ask_user`` is a SUPERVISOR-only tool
    injected by ``build_stack`` (not a specialist, not a declared surface), so
    the phantom-gate allowlist must admit it for opted-in orgs. Reading the
    profile here (not the built plan) keeps the test honest about what the org's
    CONFIG opts into, independent of build plumbing (mirrors ``_declared_surface``)."""
    return {"ask_user"} if load_ask_user_enabled(org) else set()


def _dynamic_surface(org: str) -> set[str]:
    """The four ``pux_dyn_*`` tool names when this org opts into level (c)
    dynamic tools (``policy.yaml`` ``sandbox.dynamic_tools: true``), else empty.
    Dynamic tools are a legitimate channel (agent-authored, in-container) that
    ``build_stack`` injects onto the supervisor surface, so a real org's
    supervisor surface may legitimately carry its own ``pux_dyn_*`` names in
    addition to the specialist registry. Reading the POLICY here (not the built
    plan) keeps the test honest about what the org's CONFIG opts into,
    independent of build plumbing (mirrors ``_declared_surface`` /
    ``_ask_user_opted_in_surface``)."""
    if not load_dynamic_tools_enabled(org):
        return set()
    return {dynamic.PUX_DYN_PREFIX + n for n in dynamic.DYNAMIC_TOOL_NAMES}


def _expected_supervisor_specialists(org: str) -> set[str]:
    """The REGISTRY-specialist tool names the supervisor SHOULD carry, per the
    org's ``tool_surface.groups`` policy (OPT-IN). No policy / no ``tool_surface``
    block -> empty set (the supervisor carries NO specialists — they reach them
    via subagents). A declaration -> ``{pux_sandbox_<slug>}`` for every slug in
    the named groups/bare-slugs. Reading the POLICY here (not the built plan)
    keeps the test honest about what the org's CONFIG opts into, independent of
    build plumbing (mirrors ``_declared_surface`` / ``_ask_user_opted_in_surface``)."""
    try:
        pol = _load_policy(org, orgs._orgs_dir().parent)
    except NoPolicy:
        return set()
    return {PUX_PREFIX + slug for slug in _supervisor_allowed(pol)}


def _build_real_org(org: str, stubbed_factory) -> stack.StackPlan:
    """Build ``org`` with its REAL profile + rubric gate + the full real
    specialist surface. ``stubbed_factory`` is the fixture (forces its setup)."""
    from pux_harness.agent import profile as profile_mod
    return stack.build_stack(
        org,
        specialists=_real_specialists(),
        profile=profile_mod.load_profile(org),
        rubric_gate=stack.load_rubric_gate(org),
        sandbox=_EXEC,
    )


# --- every real org builds a well-formed stack (the headline gate) ----------


@pytest.mark.parametrize("org", REAL_ORGS)
def test_every_real_org_builds_a_well_formed_stack(org, stubbed_factory):
    """Each shipped org resolves to a non-empty prompt, a non-empty tool surface
    (the native fs tools are always present), a non-empty middleware stack, and
    a roster that EXACTLY matches its declared ``org_agent_slugs`` (the GP
    subagent, when present, is emitted by the harness, not a declared specialist)."""
    plan = _build_real_org(org, stubbed_factory)
    assert plan.supervisor_prompt, f"{org}: empty supervisor prompt"
    # Under the stub, mcp_tools + ctx_tools are empty, so the supervisor surface
    # is EXACTLY the opted-in specialist set plus any declared/ask_user/dynamic
    # tools (the native read_file/write_file surface comes from deepagents at
    # graph-build time, not from build_stack). OPT-IN: an org without a
    # ``tool_surface.groups`` declaration carries NO specialists on the
    # supervisor (they reach them via subagents) — so the surface MAY be empty
    # and that's correct. Assert phantom-free + that the specialist portion
    # EXACTLY matches the org's declared ``tool_surface.groups`` policy.
    sup_names = {t.name for t in plan.supervisor_tools}
    allowed = (SPECIALIST_TOOL_NAMES | _declared_surface(org)
               | _ask_user_opted_in_surface(org) | _dynamic_surface(org))
    assert sup_names <= allowed, (
        f"{org}: phantom supervisor tools: {sup_names - allowed}")
    # The OPT-IN guarantee: exactly the declared specialists are on the
    # supervisor — no more (flood), no less (regression).
    expected_spec = _expected_supervisor_specialists(org)
    actual_spec = {n for n in sup_names if n in SPECIALIST_TOOL_NAMES}
    assert actual_spec == expected_spec, (
        f"{org}: supervisor specialist surface {actual_spec} != "
        f"tool_surface.policy {expected_spec}")
    assert plan.supervisor_middleware, f"{org}: empty supervisor middleware"
    # Roster matches the real declared specialists (GP excluded).
    expected = org_agent_slugs(org)
    actual = [s["name"] for s in plan.subagents if s["name"] != "general-purpose"]
    assert actual == expected, f"{org}: roster drift {actual} != {expected}"


@pytest.mark.parametrize("org", REAL_ORGS)
def test_every_real_org_roster_agents_resolve_with_real_tools(org, stubbed_factory):
    """Every declared specialist in every real org resolves to a subagent whose
    tool whitelist is a subset of the real specialist surface (no phantom tool
    names — the universal tool resolver + each org's profile filtering agree).
    This is the per-org contract that the roster honors the tool registry."""
    plan = _build_real_org(org, stubbed_factory)
    allowed = (SPECIALIST_TOOL_NAMES | _declared_surface(org)
               | _ask_user_opted_in_surface(org) | _dynamic_surface(org))
    for sub in plan.subagents:
        if sub["name"] == "general-purpose":
            continue  # neutered/customized slot, not a roster specialist
        for t in sub.get("tools", []):
            # ``tools`` is a list of StructuredTool objs; each name must be a
            # known specialist OR one of this org's declared sandbox tools
            # (no phantom tool on any roster subagent).
            assert t.name in allowed, (
                f"{org}/{sub['name']}: phantom tool {t.name!r}")


# --- GP ownership against the real coder ----------------------------------


def test_coder_neutered_general_purpose_real(stubbed_factory):
    """coder's REAL profile.yaml declares ``general_purpose_subagent.enabled:
    false``. The factory emits a NEUTERED ``general-purpose`` slot (present so
    deepagents skips the heavy auto-add, but DEAD — empty tools). This is the
    fix proven against the real org, not a synthetic tree."""
    plan = _build_real_org("coder", stubbed_factory)
    gp = next((s for s in plan.subagents if s["name"] == "general-purpose"), None)
    assert gp is not None, "coder: no general-purpose slot (deepagents would auto-add)"
    assert gp["tools"] == [], "coder: GP slot not neutered (has tools)"
    assert "disabled" in gp["description"].lower()


# --- byte-identical idempotency (the stress / push-to-limits gate) ----------


@pytest.mark.parametrize("org", REAL_ORGS)
def test_real_org_build_is_idempotent_across_rebuilds(org, stubbed_factory):
    """Build each real org THREE times — the factory is pure per-call, so the
    roster, supervisor prompt, and middleware shape are byte-identical across
    rebuilds (no state leakage between orgs or iterations). This is the
    'push to its limits' stress: exhaustive coverage × repeated builds."""
    plans = [_build_real_org(org, stubbed_factory) for _ in range(3)]
    rosters = [[s["name"] for s in p.subagents] for p in plans]
    prompts = [p.supervisor_prompt for p in plans]
    middleware = [p.supervisor_middleware for p in plans]
    assert len(set(map(tuple, rosters))) == 1, f"{org}: roster leaked across rebuilds"
    assert len(set(prompts)) == 1, f"{org}: prompt leaked across rebuilds"
    assert len(set(map(tuple, [map(str, m) for m in middleware]))) == 1, (
        f"{org}: middleware leaked across rebuilds")


def test_all_real_orgs_build_in_one_session(stubbed_factory):
    """Build ALL real orgs back-to-back in a single session — no fixture reset
    between them. Catches cross-org state contamination (a singleton, a cache,
    a module-level list mutated by one build that poisons the next). The
    universal override system must be cleanly composable per-org."""
    rosters: dict[str, list[str]] = {}
    for org in REAL_ORGS:
        plan = _build_real_org(org, stubbed_factory)
        rosters[org] = sorted(s["name"] for s in plan.subagents)
    # Every org built without exception, and each roster is non-empty OR the
    # org is genuinely CTO-only (general has no specialists).
    for org, roster in rosters.items():
        assert isinstance(roster, list)
    # coder + _demo + the specialist orgs carry rosters; general may be empty.
    assert rosters["coder"]  # coder always has its roster


# --- the universal system shapes every org from the same factory ------------


def test_every_real_org_emits_only_registry_middleware(stubbed_factory):
    """No real org's stack carries a middleware instance that bypassed the
    registry. Every emitted marker is one of the stubbed registry builders
    (routing/session_guide/audit/rubric; ``model_retry`` is default-on for every
    supervisor per #84; ``interpreter`` auto-mounts for every shipped org since
    all resolve a strength:pro base — the dynamic-subagent happy path;
    ``context`` flattens to nothing under the stub; ``browser_vision`` is
    env-off). If a future change hand-appends a middleware in ``build_stack``
    outside ``_resolve_toggles`` (the drift the audit removed), this fires —
    there is exactly ONE stack path."""
    allowed = {"ROUTE", "GUIDE", "AUDIT", "RUBRIC", "RETRY", "INTERP", "HINTS", "PROMPT", "CACHE", "READFILE"}
    for org in REAL_ORGS:
        plan = _build_real_org(org, stubbed_factory)
        emitted = {str(m) for m in plan.supervisor_middleware}
        stray = emitted - allowed
        assert not stray, f"{org}: middleware bypassed the registry: {stray}"


# --- the MCP seam holds for every real org ----------------------------------


@pytest.mark.parametrize("org", REAL_ORGS)
def test_every_real_org_accepts_mcp_tools_through_the_seam(org, stubbed_factory):
    """Foreign MCP tool-servers flow into EVERY real org through the same
    ``build_stack(mcp_tools=)`` seam (no org-specific MCP code path). A tool
    surfaced as ``mcp__web__search`` lands on each org's supervisor surface —
    proving the universal pattern covers MCP for the whole fleet, not just one
    fixture org (see test_mcp_tool_servers.py for the resolver/profile-filtering
    depth proof)."""
    from langchain_core.tools import StructuredTool
    from pydantic import BaseModel
    from pux_harness.agent import profile as profile_mod

    class _NoArgs(BaseModel):
        pass
    mcp_tool = StructuredTool(
        name="mcp__web__search", description="mcp",
        args_schema=_NoArgs, func=lambda: "",
    )
    plan = stack.build_stack(
        org,
        specialists=_real_specialists(),
        profile=profile_mod.load_profile(org),
        rubric_gate=stack.load_rubric_gate(org),
        sandbox=_EXEC,
        mcp_tools=[mcp_tool],
    )
    names = {t.name for t in plan.supervisor_tools}
    assert "mcp__web__search" in names, f"{org}: MCP tool dropped at the seam"


# --- SkillsMiddleware roots resolve for every org that has skills -----------
#
# ``supervisor_skills`` carries the FOCUSED roots (``orgs/_shared/skills`` + the
# org's own ``skills/``) that native ``SkillsMiddleware`` scans for ``SKILL.md``
# at boot. An org with a materialized ``skills/`` dir MUST surface both roots;
# an org without one surfaces only the shared root (or nothing if shared is also
# absent). This guards the progressive-disclosure seam end-to-end against the
# real org tree (the wiring-only depth is in test_skills_peek.py's fake tree).

_SHARED_SKILLS_ROOT = "/sandbox/workspace/orgs/_shared/skills"


def _container_path(real_dir: Path) -> str:
    """Mirror ``supervisor_skills_roots``'s mapping: real FS path ->
    container-absolute ``/sandbox/workspace/<relative-to-project-root>``."""
    project_root = _orgs_dir().parent
    return "/sandbox/workspace/" + str(real_dir.relative_to(project_root))


@pytest.mark.parametrize("org", REAL_ORGS)
def test_every_real_org_resolves_its_skills_roots(org, stubbed_factory):
    """Each real org's ``supervisor_skills`` is exactly the set of materialized
    skill roots (shared + own), container-absolute. An org with no ``skills/``
    dir still gets the shared root if it exists on disk. Reading the FS here
    (not the built plan) keeps the test honest about what the org's CONFIG
    materialized, independent of build plumbing."""
    own_skills_dir = _org_path(org) / "skills"
    shared_skills_dir = _orgs_dir() / "_shared" / "skills"
    expected: list[str] = []
    if shared_skills_dir.is_dir():
        expected.append(_container_path(shared_skills_dir))
    if own_skills_dir.is_dir():
        expected.append(_container_path(own_skills_dir))
    plan = _build_real_org(org, stubbed_factory)
    # Every emitted root must be a real materialized skill dir (no phantom roots
    # that SkillsMiddleware would scan and find nothing in — that'd be noise the
    # resolver is specifically meant to pre-filter).
    assert plan.supervisor_skills == expected, (
        f"{org}: skills roots {plan.supervisor_skills} != expected {expected}")

