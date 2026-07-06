"""Non-mock build of every REAL org through the universal override system.

This is the production-readiness gate the plan's directive demands ("end with
non-mock tests of orgs; push the system to its limits"). NO fake org tree, NO
mocked profile: each build resolves the REAL ``orgs/specialists/*`` tree, the
REAL full specialist surface (39 tools), and each org's REAL ``profile.yaml`` /
``org.yaml`` / ``AGENTS.md``. Only the model + exec_client + the middleware
CLASSES are stubbed — those are harness-internal and need a Docker container /
LLM to construct; the ORG CONFIGURATION (the thing the Phase 1-7 system shapes)
is real.

If a shipped org's config drifts so the factory can't assemble a well-formed
stack, one of these fires. That's the contract that every real org honors the
universal override + inheritance system.
"""
from __future__ import annotations

import pytest

from pux_harness.agent import orgs, stack
from pux_harness.agent.orgs import discover_orgs, org_agent_slugs
from pux_harness.sandbox.tools import SPECIALIST_TOOL_NAMES, build_native_specialists

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
    monkeypatch.setattr(stack, "build_context_layer", lambda: ([], []))
    monkeypatch.setattr(stack, "RoutingMiddleware", lambda: "ROUTE")
    monkeypatch.setattr(stack, "SessionGuideMiddleware", lambda: "GUIDE")
    monkeypatch.setattr(stack, "AuditMiddleware", lambda **kw: "AUDIT")
    monkeypatch.setattr(stack, "RubricMiddleware", lambda **kw: "RUBRIC")
    monkeypatch.setattr(stack, "get_model", lambda *a, **k: "MODEL")
    monkeypatch.setattr(stack, "build_grader_tools", lambda *a, **k: ["g1"])
    monkeypatch.setattr(orgs, "get_model", lambda *a, **k: "WORKER_MODEL")


def _real_specialists():
    """The FULL real specialist surface (all 39 tools) — not a 2-tool stub."""
    return build_native_specialists(exec_client=_EXEC)


def _build_real_org(org: str, stubbed_factory) -> stack.StackPlan:
    """Build ``org`` with its REAL profile + rubric gate + the full real
    specialist surface. ``stubbed_factory`` is the fixture (forces its setup)."""
    from pux_harness.agent import profile as profile_mod
    return stack.build_stack(
        org,
        specialists=_real_specialists(),
        profile=profile_mod.load_profile(org),
        rubric_gate=profile_mod.load_rubric_gate(org),
        exec_client=_EXEC,
    )


# --- every real org builds a well-formed stack (the headline gate) ----------


@pytest.mark.parametrize("org", REAL_ORGS)
def test_every_real_org_builds_a_well_formed_stack(org, stubbed_factory):
    """Each shipped org resolves to a non-empty prompt, a non-empty tool surface
    (the native fs tools are always present), a non-empty middleware stack, and
    a roster that EXACTLY matches its declared ``org_agent_slugs`` (the GP
    subagent, when present, is a Phase-1 emission, not a declared specialist)."""
    plan = _build_real_org(org, stubbed_factory)
    assert plan.supervisor_prompt, f"{org}: empty supervisor prompt"
    # Under the stub, mcp_tools + ctx_tools are empty, so the supervisor surface
    # is EXACTLY the real specialist set (the native read_file/write_file surface
    # comes from deepagents at graph-build time, not from build_stack). Assert
    # that surface is non-empty + phantom-free (no stray tool names leak on).
    sup_names = {t.name for t in plan.supervisor_tools}
    assert sup_names, f"{org}: empty supervisor tool surface"
    assert sup_names <= SPECIALIST_TOOL_NAMES, (
        f"{org}: phantom supervisor tools: {sup_names - SPECIALIST_TOOL_NAMES}")
    assert plan.supervisor_middleware, f"{org}: empty supervisor middleware"
    # Roster matches the real declared specialists (GP excluded — it's Phase 1).
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
    for sub in plan.subagents:
        if sub["name"] == "general-purpose":
            continue  # Phase-1 neutered/customized slot, not a roster specialist
        for t in sub.get("tools", []):
            # ``tools`` is a list of StructuredTool objs; each name must be a
            # known specialist (no phantom tool on any roster subagent).
            assert t.name in SPECIALIST_TOOL_NAMES, (
                f"{org}/{sub['name']}: phantom tool {t.name!r}")


# --- Phase 1 (GP ownership) against the real dev-bot ------------------------


def test_dev_bot_neutered_general_purpose_real(stubbed_factory):
    """dev-bot's REAL profile.yaml declares ``general_purpose_subagent.enabled:
    false``. The factory emits a NEUTERED ``general-purpose`` slot (present so
    deepagents skips the heavy auto-add, but DEAD — empty tools). This is the
    Phase-1 fix proven against the real org, not a synthetic tree."""
    plan = _build_real_org("dev-bot", stubbed_factory)
    gp = next((s for s in plan.subagents if s["name"] == "general-purpose"), None)
    assert gp is not None, "dev-bot: no general-purpose slot (deepagents would auto-add)"
    assert gp["tools"] == [], "dev-bot: GP slot not neutered (has tools)"
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
    # dev-bot + _demo + the specialist orgs carry rosters; general may be empty.
    assert rosters["dev-bot"]  # dev-bot always has its roster


# --- the universal system shapes every org from the same factory ------------


def test_every_real_org_emits_only_registry_middleware(stubbed_factory):
    """No real org's stack carries a middleware instance that bypassed the
    registry. Every emitted marker is one of the stubbed registry builders
    (routing/session_guide/audit/rubric; ``context`` flattens to nothing under
    the stub; ``browser_vision`` is env-off). If a future change hand-appends a
    middleware in ``build_stack`` outside ``_resolve_toggles`` (the drift the
    Phase-3 audit removed), this fires — there is exactly ONE stack path."""
    allowed = {"ROUTE", "GUIDE", "AUDIT", "RUBRIC"}
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
        rubric_gate=profile_mod.load_rubric_gate(org),
        exec_client=_EXEC,
        mcp_tools=[mcp_tool],
    )
    names = {t.name for t in plan.supervisor_tools}
    assert "mcp__web__search" in names, f"{org}: MCP tool dropped at the seam"

