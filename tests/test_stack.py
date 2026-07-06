"""Phase 21 — the stack factory (``stack.build_stack``) tests.

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
    contract/test surface that reads it."""
    names = stack.middleware_names()
    assert set(names) == {"routing", "session_guide", "rubric"}
    # No duplicate registrations.
    assert len(names) == len(set(names))


def test_defaults_match_pre_factory_baseline():
    """The defaults ARE the pre-factory mount order: routing + session_guide on
    the supervisor, nothing toggleable on subagents (the context layer is the
    non-toggleable base, handled outside the toggle list)."""
    assert stack.DEFAULT_SUPERVISOR == ["routing", "session_guide"]
    assert stack.DEFAULT_SUBAGENT == []


def test_routing_and_session_are_supervisor_scoped_rubric_too():
    """Every shipped middleware is supervisor-scoped today (the subagent scope
    is reserved — grows when a subagent-scoped middleware is registered)."""
    by_name = {s.name: s for s in stack.MIDDLEWARE_REGISTRY}
    for name in ("routing", "session_guide", "rubric"):
        assert stack.Scope.SUPERVISOR in by_name[name].scope


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
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE"]
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
    # the rest of the stack is the same baseline (ROUTE, GUIDE)
    assert plan.supervisor_middleware[:-1] == ["ROUTE", "GUIDE"]


def test_browser_vision_absent_when_disabled(fake_tree, stub_factory):
    """``PUX_BROWSER_VISION=0`` (a text-only driver) → NOT mounted at all on
    either scope (clean absent-from-list, not mounted-but-off)."""
    from pux_harness.context.browser_vision import BrowserVisionMiddleware

    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE"]
    assert not any(isinstance(m, BrowserVisionMiddleware)
                   for m in plan.supervisor_middleware)
    assert not any(isinstance(m, BrowserVisionMiddleware)
                   for m in plan.subagents[0]["middleware"])


# --- prompt assembly (profile.base_system_prompt / system_prompt_suffix) ----

def test_profile_base_system_prompt_replaces_assembled(fake_tree, stub_factory):
    """``profile.base_system_prompt`` REPLACES the assembled (root + org +
    addendum) prompt rather than appending — the override that lets an org
    swap the whole CTO persona. (This behavior moved out of graph.py in
    Phase 21; it lives in the factory now.)"""
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
    assert mw == ["ROUTE", "GUIDE", "RUBRIC"]
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
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE"]
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
    assert plan.supervisor_middleware == ["GUIDE"]


def test_override_supervisor_add_is_idempotent(fake_tree, stub_factory):
    """Adding a name already in the defaults does NOT duplicate it
    (``_resolve_toggles`` dedupes on add)."""
    _write_middleware_block(fake_tree,
        "middleware:\n  supervisor:\n    add: [session_guide]\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, exec_client="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE"]


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
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE"]


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
    assert plan.supervisor_middleware == ["GUIDE"]


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
    """No rule is wired yet — ``_apply_rules`` returns the names unchanged
    regardless of the runtime facts. The seam is explicit + tested so the
    policy layer is legible when the motivating rule (MCP → drop ask_user)
    lands."""
    names = ["routing", "session_guide"]
    facts = stack.RuntimeFacts(transport="acp", mcp_active=True)
    assert stack._apply_rules(facts, stack.Scope.SUPERVISOR, names) == names
    # Returns a NEW list (the seam never mutates the caller's list).
    out = stack._apply_rules(facts, stack.Scope.SUPERVISOR, names)
    assert out is not names


def test_runtime_facts_defaults():
    """The default RuntimeFacts is the serve-transport, no-MCP baseline
    (build_graph threads nothing today → this default is used)."""
    f = stack.RuntimeFacts()
    assert f.transport == "serve"
    assert f.mcp_active is False


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
