"""The stack factory (``stack.build_stack``) tests.

Proves the factory is the SINGLE resolver for the per-org agent stack — the
user's "one place to adjust defaults" goal:

- the registry + defaults are the documented vocabulary (``test_registry_*``);
- a no-profile org is byte-identical to the pre-factory build
  (``test_no_profile_byte_identical_baseline``);
- the ``middleware:`` override block add/remove works, scoped correctly
  (``test_override_*``);
- the deepagents ``excluded_middleware`` field is honored — it was a DEAD path
  before the factory (``test_excluded_middleware_field_honored``);
- the runtime-facts rules seam is identity today (``test_rules_seam_*``);
- unknown names / wrong scopes fail loud (``test_*_rejected``);
- the rubric gate appends ``RubricMiddleware`` iff armed (``test_rubric_*``);
- ``validate_overrides`` is the offline contract surface
  (``test_validate_overrides_*``).

Drives ``build_stack`` DIRECTLY (not via ``build_graph``) — the unit under
test. The heavy deps (``build_context_layer``, the middleware classes,
``get_model``, ``build_grader_tools``) are stubbed; the override parsing,
registry resolution, and plan assembly run for real against a scratch orgs/
tree. The wiring from ``build_graph`` → ``build_stack`` is proven separately in
``test_profile.py`` (end-to-end through the real entry point).
"""
from __future__ import annotations

from pathlib import Path

import pytest
from deepagents import HarnessProfileConfig
from langchain_core.tools import StructuredTool
from pydantic import BaseModel

from pux_harness.agent import orgs, stack


# --- fakes -----------------------------------------------------------------

class _NoArgs(BaseModel):
    """Empty args schema (mirrors tools.py's argument-less tool idiom)."""


def _mk_tool(name: str) -> StructuredTool:
    return StructuredTool(
        name=name, description="d", args_schema=_NoArgs, func=lambda: ""
    )


_SPECIALISTS = [
    _mk_tool("pux_sandbox_python"),
    _mk_tool("pux_sandbox_browser_navigate"),
]


@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """Scratch orgs/ tree with org ``p`` + one specialist subagent
    (``browserish``). ``orgs._orgs_dir`` is the single source of truth that
    ``profile._profile_path`` (→ ``load_middleware_overrides``) and
    ``build_system_prompt`` / ``load_subagents`` resolve through, so patching
    it covers every reader in the factory."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "skills").mkdir(parents=True)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    # BrowserVisionMiddleware is env-gated (default ON); pin it OFF so the
    # byte-identical baseline tests see EXACTLY the pre-vision stack. The
    # vision mount itself is proven in test_browser_vision_mounts_* below.
    monkeypatch.setenv("PUX_BROWSER_VISION", "0")

    d = tmp_path / "orgs" / "p"
    d.mkdir(parents=True)
    (d / "AGENTS.md").write_text("# p\n\nCTO prose, no frontmatter.\n")
    (d / "org.yaml").write_text("agents: [browserish]\n")
    bdir = d / "agents"
    bdir.mkdir()
    (bdir / "browserish.md").write_text(
        "---\n"
        'name: "browserish"\n'
        'description: "browses"\n'
        'tools: ["browser_navigate"]\n'
        "---\n\nbrowser body.\n"
    )
    return tmp_path


@pytest.fixture
def stub_factory(monkeypatch):
    """Stub build_stack's heavy deps so the resolver + plan assembly run for
    real without Docker / real middleware / model init. Marker strings make the
    resolved middleware ORDER observable (the thing the factory owns). Returns a
    ``cap`` dict carrying the captured ``RubricMiddleware`` construction kwargs
    so the rubric tests can assert on them."""
    cap: dict = {"rubric": []}

    monkeypatch.setattr(stack, "build_context_layer", lambda: ([], []))
    monkeypatch.setattr(stack, "RoutingMiddleware", lambda: "ROUTE")
    monkeypatch.setattr(stack, "SessionGuideMiddleware", lambda: "GUIDE")
    # AuditMiddleware is opt-in; stub to a marker so the resolver test
    # can observe its presence/position without constructing a real one (which
    # would bind shared_event_store() and touch the real .pux/events.sqlite). The
    # real class is unit-tested in pux-harness/tests/test_audit.py.
    monkeypatch.setattr(stack, "AuditMiddleware", lambda **kw: "AUDIT")
    # ModelRetryMiddleware is default-on for every supervisor; stub to a marker
    # so the resolved-stack tests observe its presence/position without sleeping
    # on a real backoff. ToolRetryMiddleware is gate-driven (only built when a
    # tool_retry: block ships); stubbed for the tool_retry-specific test. The
    # real retry behavior is unit-tested in test_retry_middleware.py (incl. the
    # real transient retry_on wiring).
    monkeypatch.setattr(stack, "ModelRetryMiddleware", lambda **kw: "RETRY")
    monkeypatch.setattr(stack, "ToolRetryMiddleware", lambda **kw: "TOOLRETRY")

    def _rubric(**kwargs):
        cap["rubric"].append(kwargs)
        return "RUBRIC"

    monkeypatch.setattr(stack, "RubricMiddleware", _rubric)
    monkeypatch.setattr(stack, "get_model", lambda *a, **k: "MODEL")
    monkeypatch.setattr(stack, "build_grader_tools",
                        lambda *a, **k: ["g1", "g2", "g3"])
    # load_subagents → _build_sub → get_model(role="worker") resolves via the
    # ``orgs`` module's own import; stub it so no real model init happens.
    monkeypatch.setattr(orgs, "get_model", lambda *a, **k: "WORKER_MODEL")
    return cap


def _gate(*, enabled=True, max_iterations=3, default=None):
    return profile_gate(enabled=enabled, max_iterations=max_iterations, default=default)


def profile_gate(*, enabled=True, max_iterations=3, default=None):
    """Build a ``profile.RubricGate`` lazily (avoid importing profile at module
    top — not strictly necessary, but keeps the dependency direction obvious)."""
    from pux_harness.agent import profile
    return profile.RubricGate(
        enabled=enabled, max_iterations=max_iterations, default=default
    )


def _write_middleware_block(fake_tree: Path, block: str) -> None:
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(block)


# --- the registry + defaults (the documented vocabulary) -------------------

def test_registry_lists_documented_names():
    """The registry is the single vocabulary; ``middleware_names`` is the
    contract/test surface that reads it. ``context`` +
    ``browser_vision`` were folded in as first-class (default-on, removable) specs;
    added the opt-in ``audit`` spec (default OFF)."""
    names = stack.middleware_names()
    assert set(names) == {"audit", "context", "routing", "session_guide",
                          "rubric", "model_retry", "tool_retry", "browser_vision"}
    # No duplicate registrations.
    assert len(names) == len(set(names))


def test_defaults_match_pre_factory_baseline():
    """The defaults ARE the mount order, now expressed through the
    registry: context + routing + session_guide + browser_vision on the
    supervisor, context + browser_vision on subagents. (``rubric`` is
    gate-driven, not a default.)"""
    assert stack.DEFAULT_SUPERVISOR == ["context", "routing", "session_guide",
                                        "model_retry", "browser_vision"]
    assert stack.DEFAULT_SUBAGENT == ["context", "browser_vision"]


def test_registry_scopes_are_correct():
    """``audit`` + ``context`` + ``browser_vision`` are dual-scope (supervisor
    AND subagent); ``routing`` / ``session_guide`` / ``rubric`` are supervisor-
    only (the subagent scope grows when a subagent-scoped middleware is
    registered)."""
    by_name = {s.name: s for s in stack.MIDDLEWARE_REGISTRY}
    for name in ("routing", "session_guide", "rubric", "model_retry", "tool_retry"):
        assert by_name[name].scope == {stack.Scope.SUPERVISOR}, name
    for name in ("audit", "context", "browser_vision"):
        assert by_name[name].scope == {stack.Scope.SUPERVISOR,
                                       stack.Scope.SUBAGENT}, name


# --- the byte-identical baseline (the regression guarantee) ----------------

def test_no_profile_byte_identical_baseline(fake_tree, stub_factory):
    """No profile + no gate → the factory emits exactly the pre-factory stack:
    supervisor middleware [ROUTE, GUIDE] (context layer stubbed empty here),
    the specialist tools, the assembled prompt, and the resolved subagents."""
    plan = stack.build_stack(
        "p",
        specialists=list(_SPECIALISTS),
        profile=None,
        rubric_gate=None,
        exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "RETRY"]
    sup_names = {t.name for t in plan.supervisor_tools}
    assert {"pux_sandbox_python", "pux_sandbox_browser_navigate"} <= sup_names
    # Prompt is the root + org + harness addendum (the org prose lands).
    assert "CTO prose" in plan.supervisor_prompt
    # The one specialist subagent resolved, on the worker role.
    assert [s["name"] for s in plan.subagents] == ["browserish"]
    assert plan.subagents[0]["model"] == "WORKER_MODEL"
    # No gate → RubricMiddleware never constructed.
    assert stub_factory["rubric"] == []


def test_no_profile_subagent_middleware_is_the_context_layer(fake_tree, stub_factory):
    """The factory threads ``subagent_middleware`` into every subagent's
    ``middleware`` key (context layer stubbed empty here → []). The real
    context-reaches-subagent behavior is proven in test_context_offload.py."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.subagents[0]["middleware"] == []


# --- the general-purpose subagent -------------------------------------------

def test_no_profile_emits_no_general_purpose(fake_tree, stub_factory):
    """No ``general_purpose_subagent`` block → pux emits NO GP spec; deepagents
    then auto-adds its own default (graph.py:716-717). This is the parity path —
    pux only intervenes when the org explicitly owns the slot, so a no-profile
    org is byte-identical to today. Proven here by absence: the roster is exactly
    the org's specialists."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert [s["name"] for s in plan.subagents] == ["browserish"]


def test_disabled_general_purpose_is_neutered(fake_tree, stub_factory):
    """``general_purpose_subagent: {enabled: false}`` → pux emits a NEUTERED
    ``general-purpose`` spec: present (so deepagents skips the heavy auto-add),
    but DEAD — empty tools + empty middleware + an honest disabled
    description/prompt, so even a stray delegation returns immediately
    (Safeguard S1). The slot is dead weight on purpose; full removal would need
    the model-keyed registry pux can't safely use."""
    from deepagents import GeneralPurposeSubagentProfile
    cfg = HarnessProfileConfig(
        general_purpose_subagent=GeneralPurposeSubagentProfile(enabled=False),
    )
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=cfg,
        rubric_gate=None, exec_client="EXEC",
    )
    names = [s["name"] for s in plan.subagents]
    assert "general-purpose" in names
    gp = next(s for s in plan.subagents if s["name"] == "general-purpose")
    assert gp["tools"] == []                       # dead — no tools
    assert gp["middleware"] == []                  # dead — no middleware
    assert gp["model"] == "WORKER_MODEL"
    assert "disabled" in gp["description"].lower()
    assert "disabled" in gp["system_prompt"].lower()


def test_customized_general_purpose_carries_surface(fake_tree, stub_factory):
    """A customized GP (``enabled`` absent → treated as on) carries the org's
    custom description/prompt AND the full specialist surface (profile-filtered
    the same way every roster subagent's whitelist is). The org-wide suffix
    layers on top of the GP prompt — same precedence every subagent follows."""
    from deepagents import GeneralPurposeSubagentProfile
    cfg = HarnessProfileConfig(
        system_prompt_suffix="ORG SUFFIX",
        general_purpose_subagent=GeneralPurposeSubagentProfile(
            description="custom desc", system_prompt="custom prompt",
        ),
    )
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=cfg,
        rubric_gate=None, exec_client="EXEC",
    )
    gp = next(s for s in plan.subagents if s["name"] == "general-purpose")
    assert gp["description"] == "custom desc"
    # GP prompt then org-wide suffix (most-specific last).
    assert gp["system_prompt"] == "custom prompt\n\nORG SUFFIX"
    # Full specialist surface (ctx_tools stubbed empty here) — NOT dead.
    assert {t.name for t in gp["tools"]} == {
        "pux_sandbox_python", "pux_sandbox_browser_navigate",
    }
    assert gp["model"] == "WORKER_MODEL"


def test_general_purpose_not_double_emitted_when_roster_has_it(fake_tree, stub_factory):
    """If an org literally rostered a ``general-purpose`` specialist in org.yaml
    (unusual but possible), pux must NOT double-emit — the roster entry wins, one
    slot. Mirrors deepagents' own ``not any(...)`` guard at graph.py:717."""
    (fake_tree / "orgs" / "p" / "org.yaml").write_text(
        "agents: [browserish, general-purpose]\n")
    gdir = fake_tree / "orgs" / "p" / "agents"
    (gdir / "general-purpose.md").write_text(
        "---\n"
        'name: "general-purpose"\n'
        'description: "roster gp"\n'
        "---\n\nroster body.\n")
    from deepagents import GeneralPurposeSubagentProfile
    cfg = HarnessProfileConfig(
        general_purpose_subagent=GeneralPurposeSubagentProfile(enabled=False),
    )
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=cfg,
        rubric_gate=None, exec_client="EXEC",
    )
    gps = [s for s in plan.subagents if s["name"] == "general-purpose"]
    assert len(gps) == 1
    # The roster entry wins (its body), NOT the neutered spec.
    assert "roster body" in gps[0]["system_prompt"]


# --- BrowserVisionMiddleware mount (env-gated, default ON) ------------------

def test_browser_vision_mounts_innermost_when_enabled(fake_tree, stub_factory, monkeypatch):
    """Default ON (the shipped driver is the multimodal mimo-v2.5): the factory
    appends BrowserVisionMiddleware to BOTH scopes INNERMOST (last), after the
    context layer + toggles. ``fake_tree`` pins it OFF for the baseline tests;
    flip it back on here. The real screenshot→image-block behavior is proven
    live in tests/integration/test_browser_e2e.py and deterministically in
    test_browser_vision.py."""
    from pux_harness.context.browser_vision import BrowserVisionMiddleware

    monkeypatch.setenv("PUX_BROWSER_VISION", "1")  # override fake_tree's OFF pin
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    # mounted on BOTH scopes, INNERMOST (last element)
    assert isinstance(plan.supervisor_middleware[-1], BrowserVisionMiddleware)
    assert isinstance(plan.subagents[0]["middleware"][-1], BrowserVisionMiddleware)
    # the rest of the stack is the same baseline (ROUTE, GUIDE, RETRY)
    assert plan.supervisor_middleware[:-1] == ["ROUTE", "GUIDE", "RETRY"]


def test_browser_vision_absent_when_disabled(fake_tree, stub_factory):
    """``PUX_BROWSER_VISION=0`` (a text-only driver) → NOT mounted at all on
    either scope (clean absent-from-list, not mounted-but-off)."""
    from pux_harness.context.browser_vision import BrowserVisionMiddleware

    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "RETRY"]
    assert not any(isinstance(m, BrowserVisionMiddleware)
                   for m in plan.supervisor_middleware)
    assert not any(isinstance(m, BrowserVisionMiddleware)
                   for m in plan.subagents[0]["middleware"])


# --- prompt assembly (profile.base_system_prompt / system_prompt_suffix) ----

def test_profile_base_system_prompt_replaces_assembled(fake_tree, stub_factory):
    """``profile.base_system_prompt`` REPLACES the assembled (root + org +
    addendum) prompt rather than appending — the override that lets an org
    swap the whole CTO persona. (This behavior moved out of graph.py in
    it lives in the factory now.)"""
    cfg = HarnessProfileConfig(base_system_prompt="FULL_REPLACE")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=cfg,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_prompt == "FULL_REPLACE"


def test_profile_system_prompt_suffix_appends(fake_tree, stub_factory):
    """``profile.system_prompt_suffix`` is appended to the assembled prompt
    (after a base_system_prompt replace if both are set)."""
    cfg = HarnessProfileConfig(system_prompt_suffix="SUFFIX_MARKER")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=cfg,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_prompt.endswith("SUFFIX_MARKER")
    # The assembled org prose is still there (suffix appends, doesn't replace).
    assert "CTO prose" in plan.supervisor_prompt


# --- the rubric gate (runtime fact, not an org override) -------------------

def test_rubric_gate_armed_appends_rubric(fake_tree, stub_factory):
    """An armed gate appends RubricMiddleware AFTER the baseline, built with the
    grader role model + the 3 grader tools + the gate's max_iterations."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=_gate(max_iterations=5), exec_client="EXEC",
    )
    mw = plan.supervisor_middleware
    assert mw == ["ROUTE", "GUIDE", "RUBRIC", "RETRY"]
    assert mw.index("ROUTE") < mw.index("RUBRIC")  # baseline before rubric
    assert stub_factory["rubric"][0]["max_iterations"] == 5
    # Grader model via the role-resolved path (stub_factory → "MODEL"); 3 tools.
    assert stub_factory["rubric"][0]["model"] == "MODEL"
    assert len(stub_factory["rubric"][0]["tools"]) == 3


def test_rubric_gate_disabled_mounts_no_rubric(fake_tree, stub_factory):
    """``enabled: false`` is the operator kill-switch — the gate is present but
    the middleware is NOT mounted (byte-identical to no gate)."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=_gate(enabled=False), exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "RETRY"]
    assert stub_factory["rubric"] == []


def test_rubric_in_default_list_is_noop_without_gate(fake_tree, stub_factory):
    """RubricMiddleware's build returns None when no gate is armed, so even if
    'rubric' were in the default list it would contribute nothing — the name
    can sit in defaults without forcing construction. (Today it's appended at
    resolve time only when the gate arms; this test pins the None-skip.)"""
    # Simulate 'rubric' in the resolved names with no gate: build returns None.
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert "RUBRIC" not in plan.supervisor_middleware


# --- the ``middleware:`` override block (the org override layer) -----------

def test_override_supervisor_remove_drops_routing(fake_tree, stub_factory):
    """``middleware.supervisor.remove: [routing]`` drops routing from the
    baseline — the override reaches the resolved stack."""
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    remove: [routing]\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["GUIDE", "RETRY"]


def test_override_supervisor_add_is_idempotent(fake_tree, stub_factory):
    """Adding a name already in the defaults does NOT duplicate it
    (``_resolve_toggles`` dedupes on add)."""
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    add: [session_guide]\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "RETRY"]


def test_override_add_wins_over_remove(fake_tree, stub_factory):
    """A same-named add+remove resolves to PRESENT (add applies after remove,
    so an org can be explicit without accidentally dropping a middleware)."""
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    remove: [routing]\n    add: [routing]\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert "ROUTE" in plan.supervisor_middleware


def test_override_empty_block_is_byte_identical(fake_tree, stub_factory):
    """``middleware: {}`` (or all-empty lists) yields empty overrides —
    byte-identical to no block at all."""
    _write_middleware_block(fake_tree, "middleware: {}\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "RETRY"]


# --- the opt-in ``audit`` spec ---------------------------------------------

def test_audit_is_default_off(fake_tree, stub_factory):
    """``audit`` is opt-in — a no-block org emits NO audit middleware (the
    default-on baseline is unchanged; audit never costs anything unless asked
    for). The registry lists it, but neither DEFAULT list includes it."""
    assert "audit" not in stack.DEFAULT_SUPERVISOR
    assert "audit" not in stack.DEFAULT_SUBAGENT
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert "AUDIT" not in plan.supervisor_middleware


def test_audit_opt_in_supervisor_mounts_outermost(fake_tree, stub_factory):
    """``middleware.supervisor.add: [audit]`` mounts AuditMiddleware FIRST
    (outermost observer) — its ``handler(request)`` then wraps the whole
    pipeline so elapsed/outcome measure the real call. The default-on layers
    (context/routing/session_guide/browser_vision) follow in registry order."""
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    add: [audit]\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    mw = plan.supervisor_middleware
    assert mw[0] == "AUDIT", mw  # outermost
    # the default-on baseline still follows, unchanged, in registry order
    assert mw[1:] == ["ROUTE", "GUIDE", "RETRY"], mw


def test_audit_opt_in_subagent_scope_allowed(fake_tree, stub_factory):
    """``audit`` is dual-scope — ``middleware.subagent.add: [audit]`` is a valid
    override (validated against the registry scope), not a scope-mismatch error.
    Drives ``validate_overrides`` directly since ``build_stack`` returns the
    supervisor middleware only (subagent middleware is threaded into the
    subagent specs at graph-build time)."""
    _write_middleware_block(fake_tree,
        "middleware:\n  subagent:\n    add: [audit]\n")
    assert stack.validate_overrides("p") == []


# --- the deepagents ``excluded_middleware`` field (was a dead path) --------


def test_excluded_middleware_field_honored(fake_tree, stub_factory):
    """``HarnessProfileConfig.excluded_middleware`` is treated as an UNSCOPED
    supervisor remove — honoring a field ``create_deep_agent`` only fires
    through a *registered* profile (which the harness doesn't use). Before the
    factory this was a DEAD path; now both forms (the scoped ``middleware:``
    block AND this field) route through the same remove-set."""
    cfg = HarnessProfileConfig(excluded_middleware=frozenset({"routing"}))
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=cfg,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["GUIDE", "RETRY"]


# --- fail-loud: unknown names + wrong scopes -------------------------------

def test_unknown_middleware_name_rejected(fake_tree, stub_factory):
    """An override name not in the registry fails loud at build time, not
    silently skipped."""
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    add: [bogus]\n")
    with pytest.raises(ValueError, match="unknown middleware name"):
        stack.build_stack(
            "p", specialists=list(_SPECIALISTS), profile=None,
            rubric_gate=None, exec_client="EXEC",
        )


def test_supervisor_only_middleware_rejected_on_subagent(fake_tree, stub_factory):
    """A supervisor-scoped middleware added to the subagent scope fails loud —
    the scope guard is real (today every shipped middleware is supervisor-only,
    so a subagent add of any of them trips this; the seam grows when a
    subagent-scoped middleware is registered)."""
    _write_middleware_block(fake_tree,
        "middleware:\n  subagent:\n    add: [routing]\n")
    with pytest.raises(ValueError, match="not allowed in the subagent scope"):
        stack.build_stack(
            "p", specialists=list(_SPECIALISTS), profile=None,
            rubric_gate=None, exec_client="EXEC",
        )


# --- the rules seam (runtime-facts policy layer) ---------------------------

def test_rules_seam_is_identity_today():
    """``_apply_rules`` is the MIDDLEWARE-level seam and stays identity: the
    runtime-facts rule that landed (drop ``ask_user`` over MCP/autonomous) is
    TOOL-level, so it's construction-gated in ``build_stack`` rather than
    filtered through here. A future rule that toggles a MIDDLEWARE on transport
    would wire into this function; until then it returns the names unchanged."""
    names = ["routing", "session_guide"]
    facts = stack.RuntimeFacts(transport="acp", mcp_active=True)
    assert stack._apply_rules(facts, stack.Scope.SUPERVISOR, names) == names
    # Returns a NEW list (the seam never mutates the caller's list).
    out = stack._apply_rules(facts, stack.Scope.SUPERVISOR, names)
    assert out is not names


def test_runtime_facts_defaults():
    """The default RuntimeFacts is the serve-transport, no-MCP, non-autonomous
    baseline (correct for the runner + AG-UI web path; the ACP / direct / mcp
    entrypoints pass a real RuntimeFacts)."""
    f = stack.RuntimeFacts()
    assert f.transport == "serve"
    assert f.mcp_active is False
    assert f.autonomous is False


@pytest.mark.parametrize("raw,expected", [
    ("1", True), ("true", True), ("TRUE", True), ("yes", True), (" on ", True),
    ("0", False), ("false", False), ("", False), ("no", False), ("maybe", False),
])
def test_autonomous_from_env(monkeypatch, raw, expected):
    """``PUX_AUTONOMOUS`` is the cross-entrypoint autonomous signal — the
    ask_user construction gate keys on it (autonomous → tool dropped). Truthy
    set is 1/true/yes/on (case-insensitive, whitespace-tolerant); everything
    else is False."""
    monkeypatch.setenv("PUX_AUTONOMOUS", raw)
    assert stack.autonomous_from_env() is expected


def test_autonomous_from_env_unset(monkeypatch):
    """Unset → False (the default interactive flow; ask_user is NOT dropped)."""
    monkeypatch.delenv("PUX_AUTONOMOUS", raising=False)
    assert stack.autonomous_from_env() is False


# --- validate_overrides (the offline contract surface) ---------------------

def test_validate_overrides_clean_when_no_block(fake_tree, stub_factory):
    """No middleware block → no errors (the common case)."""
    assert stack.validate_overrides("p") == []


def test_validate_overrides_catches_unknown_name(fake_tree, stub_factory):
    """``validate_overrides`` returns errors (doesn't raise) so the contract
    checker can collect + report them — a typo'd name fails --check-contract."""
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    add: [bogus]\n")
    errs = stack.validate_overrides("p")
    assert len(errs) == 1
    assert "bogus" in errs[0]


def test_validate_overrides_catches_wrong_scope(fake_tree, stub_factory):
    """A supervisor-only middleware on the subagent scope is an error here too
    — the offline check and the runtime build agree."""
    _write_middleware_block(fake_tree,
        "middleware:\n  subagent:\n    add: [routing]\n")
    errs = stack.validate_overrides("p")
    assert len(errs) == 1
    assert "subagent scope" in errs[0]


def test_validate_overrides_accepts_valid_block(fake_tree, stub_factory):
    """A well-formed, in-scope override block produces no errors."""
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    remove: [routing]\n")
    assert stack.validate_overrides("p") == []


# --- context + browser_vision are first-class removable specs ---------------

def test_context_mounts_outermost_and_emits_tools(fake_tree, stub_factory, monkeypatch):
    """The context spec mounts at registry position 0 (OUTERMOST) and
    its coupled retrieval tools escape via ``ctx.emitted_tools_supervisor`` into
    ``supervisor_tools``. ``stub_factory`` blanks the layer to ``([], [])`` for
    the baseline tests; here we stub it to a marker mw + a marker tool so both
    halves of the coupled pair are observable."""
    ctx_tool = _mk_tool("ctx_recall")
    monkeypatch.setattr(stack, "build_context_layer",
                        lambda: (["CONTEXT"], [ctx_tool]))
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    # context outermost, then routing, session_guide, model_retry (vision off).
    assert plan.supervisor_middleware == ["CONTEXT", "ROUTE", "GUIDE", "RETRY"]
    # The retrieval tool escaped the spec into the supervisor surface.
    assert "ctx_recall" in {t.name for t in plan.supervisor_tools}


def test_context_is_now_removable(fake_tree, stub_factory, monkeypatch):
    """The formerly-NON-toggleable context layer is now a registry spec, so
    ``middleware.supervisor.remove: [context]`` drops it AND its retrieval tool
    (the spec never ran → nothing emitted) — the user's 'selectively remove
    middleware' request, applied to the base capture/offload layer too."""
    monkeypatch.setattr(stack, "build_context_layer",
                        lambda: (["CONTEXT"], [_mk_tool("ctx_recall")]))
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    remove: [context]\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "RETRY"]
    assert "ctx_recall" not in {t.name for t in plan.supervisor_tools}


def test_browser_vision_is_now_removable_when_enabled(fake_tree, stub_factory, monkeypatch):
    """``browser_vision`` is a registry spec, so ``middleware.supervisor.remove:
    [browser_vision]`` drops it EVEN when the env pin is ON — selectable like
    every other middleware, not just env-toggleable."""
    from pux_harness.context.browser_vision import BrowserVisionMiddleware
    monkeypatch.setenv("PUX_BROWSER_VISION", "1")  # override fake_tree's OFF pin
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    remove: [browser_vision]\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "RETRY"]
    assert not any(isinstance(m, BrowserVisionMiddleware)
                   for m in plan.supervisor_middleware)


def test_full_supervisor_order_is_canonical_registry_order(fake_tree, stub_factory, monkeypatch):
    """The byte-identical FULL order — context, routing, session_guide, rubric,
    browser_vision — when the gate is armed AND vision is on. Registry order is
    canonical, so browser_vision stays INNERMOST past rubric (the previous
    append-last behavior, now registry-driven rather than special-cased)."""
    from pux_harness.context.browser_vision import BrowserVisionMiddleware
    monkeypatch.setattr(stack, "build_context_layer",
                        lambda: (["CONTEXT"], []))
    monkeypatch.setenv("PUX_BROWSER_VISION", "1")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=_gate(), exec_client="EXEC",
    )
    mw = plan.supervisor_middleware
    assert mw[:4] == ["CONTEXT", "ROUTE", "GUIDE", "RUBRIC"]
    assert isinstance(mw[-1], BrowserVisionMiddleware)
    assert len(mw) == 6  # context, routing, session_guide, rubric, model_retry, browser_vision


# --- ask_user HITL construction gate (opt-in AND not mcp/autonomous) --------

def _opt_in_ask_user(fake_tree: Path) -> None:
    """Write ``ask_user: true`` to org ``p``'s profile.yaml — the ORG opt-in half
    of the construction gate. (``build_stack`` reads it via ``load_ask_user_enabled``
    independent of the ``profile`` param, which stays ``None`` here.)"""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text("ask_user: true\n")


def _names(plan) -> set[str]:
    return {t.name for t in plan.supervisor_tools}


def test_ask_user_absent_when_not_opted_in(fake_tree, stub_factory):
    """No ``ask_user:`` flag → the tool is absent from the supervisor surface
    (the byte-identical default). The model can't call what isn't there."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
        facts=stack.RuntimeFacts(transport="serve"),
    )
    assert "ask_user" not in _names(plan)


def test_ask_user_present_when_opted_in_over_web(fake_tree, stub_factory):
    """Opt-in + web transport + not mcp/autonomous → ask_user IS in the
    supervisor surface. The web branch interrupts (the reply resumes the graph),
    so the supervisor prompt is NOT amended with the end-turn suffix."""
    _opt_in_ask_user(fake_tree)
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
        facts=stack.RuntimeFacts(transport="serve"),
    )
    assert "ask_user" in _names(plan)
    assert "END your turn" not in plan.supervisor_prompt


def test_ask_user_over_editor_appends_end_turn_suffix(fake_tree, stub_factory):
    """Opt-in + editor transport (acp) → ask_user present AND the supervisor
    prompt gains the end-turn suffix (the editor can't free-text in a permission
    popover, so the agent must stop + the user answers next turn)."""
    _opt_in_ask_user(fake_tree)
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
        facts=stack.RuntimeFacts(transport="acp"),
    )
    assert "ask_user" in _names(plan)
    assert "END your turn" in plan.supervisor_prompt


def test_ask_user_dropped_over_mcp_even_if_opted_in(fake_tree, stub_factory):
    """The MCP caller can't answer an ask_user → the tool is DROPPED at
    construction (absent from the surface), not constructed-then-no-op'd. The
    org flag is the opt-in half; the runtime gate (``mcp_active``) is the other."""
    _opt_in_ask_user(fake_tree)
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
        facts=stack.RuntimeFacts(transport="mcp", mcp_active=True),
    )
    assert "ask_user" not in _names(plan)


def test_ask_user_dropped_when_autonomous_even_if_opted_in(fake_tree, stub_factory):
    """Headless/autonomous runtimes have no human to resume an interrupt →
    ask_user is dropped at construction."""
    _opt_in_ask_user(fake_tree)
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
        facts=stack.RuntimeFacts(transport="serve", autonomous=True),
    )
    assert "ask_user" not in _names(plan)


# --- configurable retry middleware (ModelRetry default-on + ToolRetry opt-in) -

def _write_profile(fake_tree: Path, body: str) -> None:
    """Write arbitrary content to org ``p``'s profile.yaml (not just a
    ``middleware:`` block) — for the model_retry/tool_retry config tests."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(body)


def _retry_ctx(*, model_retry=None, tool_retry=None) -> "stack.StackCtx":
    """A minimal StackCtx carrying only the retry configs — for driving the
    REAL ``_build_model_retry`` / ``_build_tool_retry`` against a fake handler
    (the verify-or-die mechanism proof, no factory / Docker needed)."""
    from pux_harness.agent.profile import ModelRetryConfig
    return stack.StackCtx(
        org="t",
        facts=stack.RuntimeFacts(),
        rubric_gate=None,
        exec_client=None,
        model_retry_cfg=model_retry if model_retry is not None else ModelRetryConfig(),
        tool_retry_cfg=tool_retry,
    )


def test_model_retry_retries_transient_then_succeeds():
    """The REAL ModelRetryMiddleware, built the way the factory builds it,
    RETRIES a transient provider error and succeeds — the mechanism proof (not
    just a wiring assert). Delays zeroed so the test is instant; the retry_on
    is the shipped transient set (same as the fallback chain)."""
    import openai
    import httpx
    from pux_harness.agent.profile import ModelRetryConfig
    from pux_harness.agent.model import _TRANSIENT_EXCEPTIONS
    # Zero the backoff so time.sleep(0); keep max_retries=2, on_failure=error.
    cfg = ModelRetryConfig(initial_delay=0.0, backoff_factor=0.0, jitter=False)
    mw = stack._build_model_retry(_retry_ctx(model_retry=cfg), stack.Scope.SUPERVISOR)
    assert mw is not None
    assert mw.retry_on == _TRANSIENT_EXCEPTIONS  # one transient definition

    calls = {"n": 0}

    def handler(_req):
        calls["n"] += 1
        if calls["n"] == 1:
            raise openai.APITimeoutError(
                request=httpx.Request("GET", "https://x.test"))
        return "OK"

    assert mw.wrap_model_call(object(), handler) == "OK"
    assert calls["n"] == 2  # one transient failure → retried once → success


def test_model_retry_does_not_retry_non_transient():
    """A NON-transient error (not in the retry_on set) is NOT retried; with the
    shipped ``on_failure='error'`` it re-raises immediately — three layers
    exhausted means fail-loud, never inject error text as content."""
    from pux_harness.agent.profile import ModelRetryConfig
    cfg = ModelRetryConfig(initial_delay=0.0, backoff_factor=0.0, jitter=False)
    mw = stack._build_model_retry(_retry_ctx(model_retry=cfg), stack.Scope.SUPERVISOR)
    calls = {"n": 0}

    def handler(_req):
        calls["n"] += 1
        raise ValueError("not transient — a request/config bug")

    with pytest.raises(ValueError):
        mw.wrap_model_call(object(), handler)
    assert calls["n"] == 1  # no retry on a non-transient error


def test_model_retry_continue_returns_ai_message_on_exhaustion():
    """``on_failure='continue'`` (an org's opt-in for extra autonomous
    resilience) returns an AIMessage describing the error after retries
    exhaust, instead of re-raising — letting the agent loop adapt."""
    from langchain_core.messages import AIMessage
    from pux_harness.agent.profile import ModelRetryConfig
    import openai
    import httpx
    cfg = ModelRetryConfig(max_retries=1, on_failure="continue",
                           initial_delay=0.0, backoff_factor=0.0, jitter=False)
    mw = stack._build_model_retry(_retry_ctx(model_retry=cfg), stack.Scope.SUPERVISOR)
    calls = {"n": 0}

    def handler(_req):
        calls["n"] += 1
        raise openai.APITimeoutError(
            request=httpx.Request("GET", "https://x.test"))

    out = mw.wrap_model_call(object(), handler)
    # on_failure='continue' → a ModelResponse wrapping an AIMessage that
    # describes the exhausted retry (NOT a bare AIMessage, NOT a re-raise).
    inner = getattr(out, "result", out)
    if isinstance(inner, list):
        inner = inner[0]
    assert isinstance(inner, AIMessage)
    assert "failed" in inner.content.lower()
    assert calls["n"] == 2  # initial + 1 retry, then gave up → AIMessage


def test_model_retry_default_on_supervisor_not_subagent(fake_tree, stub_factory):
    """No ``model_retry:`` block → the shipped default config → RETRY is built
    and mounted on the supervisor (default-on conservative). It is
    SUPERVISOR-scoped, so the subagent tree does NOT get it (subagents already
    carry the fallback chain in their model)."""
    plan = stack.build_stack("p", specialists=list(_SPECIALISTS),
                             profile=None, rubric_gate=None, exec_client="EXEC")
    assert "RETRY" in plan.supervisor_middleware
    assert "RETRY" not in plan.subagents[0]["middleware"]


def test_model_retry_disabled_via_config_block(fake_tree, stub_factory):
    """``model_retry: {enabled: false}`` → no RETRY (the per-org disable)."""
    _write_profile(fake_tree, "model_retry:\n  enabled: false\n")
    plan = stack.build_stack("p", specialists=list(_SPECIALISTS),
                             profile=None, rubric_gate=None, exec_client="EXEC")
    assert "RETRY" not in plan.supervisor_middleware


def test_model_retry_disabled_via_bool_shorthand(fake_tree, stub_factory):
    """``model_retry: false`` (the bool shorthand) → disabled."""
    _write_profile(fake_tree, "model_retry: false\n")
    plan = stack.build_stack("p", specialists=list(_SPECIALISTS),
                             profile=None, rubric_gate=None, exec_client="EXEC")
    assert "RETRY" not in plan.supervisor_middleware


def test_model_retry_removable_via_middleware_block(fake_tree, stub_factory):
    """``middleware.supervisor.remove: [model_retry]`` drops it — the uniform
    toggle surface (removable like every other default-on middleware, no
    special-cased kill switch)."""
    _write_profile(fake_tree,
                   "middleware:\n  supervisor:\n    remove: [model_retry]\n")
    plan = stack.build_stack("p", specialists=list(_SPECIALISTS),
                             profile=None, rubric_gate=None, exec_client="EXEC")
    assert "RETRY" not in plan.supervisor_middleware


def test_tool_retry_is_opt_in_gate_driven(fake_tree, stub_factory):
    """No ``tool_retry:`` block → no TOOLRETRY (opt-in). WITH a block → mounted
    (gate-driven, like rubric), scoped to the declared tool names."""
    plan = stack.build_stack("p", specialists=list(_SPECIALISTS),
                             profile=None, rubric_gate=None, exec_client="EXEC")
    assert "TOOLRETRY" not in plan.supervisor_middleware

    _write_profile(fake_tree,
                   "tool_retry:\n  tools: [mcp__web_research__search]\n"
                   "  max_retries: 3\n")
    plan2 = stack.build_stack("p", specialists=list(_SPECIALISTS),
                              profile=None, rubric_gate=None, exec_client="EXEC")
    assert "TOOLRETRY" in plan2.supervisor_middleware


def test_tool_retry_mechanism_retries_scoped_transient():
    """The REAL ToolRetryMiddleware, built the way the factory builds it,
    retries a transient transport error (httpx) on a SCOPED tool. The factory
    hard-pins retry_on to the network set + on_failure='continue'."""
    import types
    import httpx
    from pux_harness.agent.profile import ToolRetryConfig
    cfg = ToolRetryConfig(
        tools=("flaky_search",),
        max_retries=2, initial_delay=0.0, backoff_factor=0.0, jitter=False,
    )
    mw = stack._build_tool_retry(_retry_ctx(tool_retry=cfg), stack.Scope.SUPERVISOR)
    assert mw is not None
    assert mw.retry_on == stack._TOOL_TRANSIENT_EXCEPTIONS
    assert mw.on_failure == "continue"
    # The scoped tool filter lives in ``_tool_filter`` (``.tools`` is the
    # unrelated AgentMiddleware ADD-tools hook, always []).
    assert mw._tool_filter == ["flaky_search"]

    calls = {"n": 0}

    def handler(_req):
        calls["n"] += 1
        if calls["n"] == 1:
            raise httpx.HTTPError("transient transport error")
        return "DONE"

    req = types.SimpleNamespace(tool=None, tool_call={"name": "flaky_search", "id": "1"})
    assert mw.wrap_tool_call(req, handler) == "DONE"
    assert calls["n"] == 2


def test_model_retry_config_tuning_parsed(fake_tree, stub_factory):
    """The ``model_retry:`` block tunes max_retries + on_failure (the only
    knobs an operator reaches for; retry_on is hard-pinned to the transient
    set, NOT configurable)."""
    from pux_harness.agent import profile
    _write_profile(fake_tree,
                   "model_retry:\n  max_retries: 5\n  on_failure: continue\n")
    cfg = profile.load_model_retry("p")
    assert cfg.enabled is True
    assert cfg.max_retries == 5
    assert cfg.on_failure == "continue"


def test_model_retry_malformed_rejected(fake_tree, stub_factory):
    """A malformed ``model_retry:`` block fails loud at load / contract time
    (unknown key, bad on_failure) — no silent skip."""
    from pux_harness.agent import profile
    _write_profile(fake_tree, "model_retry:\n  bogus: 1\n")
    with pytest.raises(TypeError, match="unknown key"):
        profile.load_model_retry("p")
    _write_profile(fake_tree, "model_retry:\n  on_failure: explode\n")
    with pytest.raises(TypeError, match="on_failure"):
        profile.load_model_retry("p")


def test_tool_retry_requires_nonempty_tools(fake_tree, stub_factory):
    """``tool_retry:`` WITHOUT a non-empty ``tools:`` list is rejected —
    tool-retry is NEVER global (a schema error must not loop)."""
    from pux_harness.agent import profile
    _write_profile(fake_tree, "tool_retry:\n  max_retries: 2\n")
    with pytest.raises(TypeError, match="tools"):
        profile.load_tool_retry("p")
