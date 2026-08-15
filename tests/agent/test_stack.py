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
from pux_harness.agent.prompt_parts import (
    SUPERVISOR_PROMPT_PARTS,
    PromptCtx,
    PromptScope,
    _interpreter_mounted,
    assemble_prompt,
)


# --- fakes -----------------------------------------------------------------

class _NoArgs(BaseModel):
    """Empty args schema (mirrors tools.py's argument-less tool idiom)."""


def _mk_tool(name: str) -> StructuredTool:
    return StructuredTool(
        name=name, description="d", args_schema=_NoArgs, func=lambda: ""
    )


_SPECIALISTS = [
    _mk_tool("pux_sandbox_python"),
    _mk_tool("pux_sandbox_desktop_screenshot"),
]


@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """Scratch orgs/ tree with org ``p`` + one specialist subagent
    (``desktopish``). ``orgs._orgs_dir`` is the single source of truth that
    ``profile._profile_path`` (→ ``load_middleware_overrides``) and
    ``build_system_prompt`` / ``load_subagents`` resolve through, so patching
    it covers every reader in the factory."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "skills").mkdir(parents=True)
    # the GP text file is the single source for the general-purpose subagent —
    # ship it in the scratch tree + reset the module cache (read-once per proc)
    (tmp_path / "orgs" / "_shared" / "general_purpose.md").write_text(
        "---\n"
        "default_description: GP-DESC\n"
        "default_prompt: GP-PROMPT\n"
        "disabled_description: disabled general-purpose subagent\n"
        "disabled_prompt: disabled general-purpose prompt\n"
        "---\nbody\n")
    monkeypatch.setattr(orgs, "_GP_TEXT", None)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    # BrowserVisionMiddleware is env-gated (default ON); pin it OFF so the
    # byte-identical baseline tests see EXACTLY the pre-vision stack. The
    # vision mount itself is proven in test_browser_vision_mounts_* below.
    monkeypatch.setenv("PUX_BROWSER_VISION", "0")

    d = tmp_path / "orgs" / "p"
    d.mkdir(parents=True)
    (d / "AGENTS.md").write_text("# p\n\nCTO prose, no frontmatter.\n")
    (d / "org.yaml").write_text("agents: [desktopish]\n")
    bdir = d / "agents"
    bdir.mkdir()
    (bdir / "desktopish.md").write_text(
        "---\n"
        'name: "desktopish"\n'
        'description: "clicks desktop pixels"\n'
        'tools: ["desktop_screenshot"]\n'
        "---\n\ndesktop body.\n"
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

    # Middleware classes import INSIDE the builders from deepagents_context —
    # patch them at their home modules (stack re-imports per call).
    import deepagents_context as _dc
    monkeypatch.setattr(_dc, "build_context_layer", lambda **kw: ([], []))
    monkeypatch.setattr(__import__("deepagents_context.sandbox_routing", fromlist=["RoutingMiddleware"]),
                        "RoutingMiddleware", lambda: "ROUTE")
    monkeypatch.setattr(__import__("deepagents_context.session_guide", fromlist=["SessionGuideMiddleware"]),
                        "SessionGuideMiddleware", lambda: "GUIDE")
    # PromptCaptureMiddleware (gaps 4+5) — supervisor-only, default-on, mounts
    # right after session_guide. Stubbed to a marker for order observability.
    monkeypatch.setattr(_dc.prompt_capture, "PromptCaptureMiddleware", lambda: "PROMPT")
    # AuditMiddleware is opt-in; stub to a marker so the resolver test
    # can observe its presence/position without constructing a real one (which
    # would bind shared_event_store() and touch the real .pux/events.sqlite). The
    # real class is unit-tested in pux-harness/tests/test_audit.py.
    monkeypatch.setattr(__import__("deepagents_context.audit", fromlist=["AuditMiddleware"]),
                         "AuditMiddleware", lambda **kw: "AUDIT")
    # ModelRetryMiddleware is default-on for every supervisor; stub to a marker
    # so the resolved-stack tests observe its presence/position without sleeping
    # on a real backoff. ToolRetryMiddleware is gate-driven (only built when a
    # tool_retry: block ships); stubbed for the tool_retry-specific test. The
    # real retry behavior is unit-tested in test_retry_middleware.py (incl. the
    # real transient retry_on wiring).
    monkeypatch.setattr(stack, "ModelRetryMiddleware", lambda **kw: "RETRY")
    # AnthropicPromptCachingMiddleware is default-on for every scope; stub to a
    # marker so resolved-stack tests observe its presence/position without
    # constructing a real one. _FullPrefixCachingMiddleware is our subclass that
    # also tags the last message (rolling conversation cache); the builder calls
    # it by name, so stub that too. The caching behavior itself (cache_control
    # tagging + unsupported-model skip + last-message breakpoint) is unit-tested
    # via langchain_anthropic + the middleware's own unit tests.
    # Prompt caching is deepagents' native tail stack — nothing to stub.
    # ReadFileVisionMiddleware is default-on for every scope (supervisor +
    # subagent) — the automatic image/binary fallback for non-multimodal
    # drivers. Stub to a marker so resolved-stack tests observe its
    # presence/position without calling driver_multimodal (which would resolve
    # a real model). driver_multimodal is also stubbed so the builder doesn't
    # touch the real models.yaml registry.
    monkeypatch.setattr(stack, "driver_multimodal", lambda **kw: False)
    monkeypatch.setattr(__import__("deepagents_context.read_file_vision", fromlist=["ReadFileVisionMiddleware"]),
                         "ReadFileVisionMiddleware", lambda **kw: "READFILE")

    def _rubric(**kwargs):
        cap["rubric"].append(kwargs)
        return "RUBRIC"

    monkeypatch.setattr(stack, "RubricMiddleware", _rubric)
    monkeypatch.setattr(stack, "get_model", lambda *a, **k: "MODEL")
    monkeypatch.setattr(stack, "build_grader_tools",
                        lambda *a, **k: ["g1", "g2", "g3"])
    # driver_strong_orchestrator gates the CodeInterpreterMiddleware mount.
    # Without stubbing, it resolves the real default model (pro strength) and
    # a real CodeInterpreterMiddleware object mounts — the baseline tests
    # expect marker strings only, so force it False here. Tests that NEED the
    # interpreter can override this stub locally.
    # load_subagents → _build_sub → get_model(role="worker") resolves via the
    # ``orgs`` module's own import; stub it so no real model init happens.
    monkeypatch.setattr(orgs, "get_model", lambda *a, **k: "WORKER_MODEL")
    return cap


def _dc_build():
    """The live ``deepagents_context`` module (what the builders import inside
    their call sites) — the monkeypatch target for ``build_context_layer``."""
    import deepagents_context
    return deepagents_context


def _gate(*, enabled=True, max_iterations=3, default=None):
    return profile_gate(enabled=enabled, max_iterations=max_iterations, default=default)


def profile_gate(*, enabled=True, max_iterations=3, default=None):
    """Build a ``stack.RubricGate`` lazily (avoid importing profile at module
    top — not strictly necessary, but keeps the dependency direction obvious)."""
    from pux_harness.agent import profile
    return stack.RubricGate(
        enabled=enabled, max_iterations=max_iterations, default=default
    )


def _write_middleware_block(fake_tree: Path, block: str) -> None:
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(block)


# --- the registry + defaults (the documented vocabulary) -------------------

def test_no_profile_byte_identical_baseline(fake_tree, stub_factory):
    """No profile + no gate → the factory emits exactly the pre-factory stack:
    supervisor middleware [ROUTE, GUIDE] (context layer stubbed empty here),
    the assembled prompt, and the resolved subagents. Under the tool-surface
    anti-flood default, the supervisor surface carries NO registry specialists
    (an org opts in via policy.yaml ``tool_surface.groups``); the specialists
    still resolve onto their subagents, which carry the full surface."""
    plan = stack.build_stack(
        "p",
        specialists=list(_SPECIALISTS),
        profile=None,
        rubric_gate=None,
        sandbox="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "PROMPT", "RETRY", "READFILE"]
    sup_names = {t.name for t in plan.supervisor_tools}
    # No profile → no registry specialists on the supervisor surface.
    assert not any(n.startswith("pux_sandbox_") for n in sup_names), (
        f"no-profile supervisor should carry no pux_sandbox_* specialists, got {sup_names}"
    )
    # Prompt is the root + org + harness addendum (the org prose lands).
    assert "CTO prose" in plan.supervisor_prompt
    # The one specialist subagent resolved, on the worker role.
    assert [s["name"] for s in plan.subagents] == ["desktopish"]
    assert plan.subagents[0]["model"] == "WORKER_MODEL"
    # No gate → RubricMiddleware never constructed.
    assert stub_factory["rubric"] == []


def test_no_profile_subagent_middleware_is_the_context_layer(fake_tree, stub_factory):
    """The factory threads ``subagent_middleware`` into every subagent's
    ``middleware`` key (context layer stubbed empty here → []). The real
    context-reaches-subagent behavior is proven in test_context_offload.py."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, sandbox="EXEC",
    )
    # context layer stubbed empty; browser_vision env-gated OFF in fake_tree.
    # ReadFileVisionMiddleware is default-ON for subagents too (read_file is
    # universal, so the auto-describe fallback is too); prompt_caching is the
    # other default-on subagent middleware that survives.
    assert plan.subagents[0]["middleware"] == ["READFILE"]


# --- the general-purpose subagent -------------------------------------------

def test_no_profile_emits_no_general_purpose(fake_tree, stub_factory):
    """No ``general_purpose_subagent`` block → pux emits NO GP spec; deepagents
    then auto-adds its own default (graph.py:716-717). This is the parity path —
    pux only intervenes when the org explicitly owns the slot, so a no-profile
    org is byte-identical to today. Proven here by absence: the roster is exactly
    the org's specialists."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, sandbox="EXEC",
    )
    assert [s["name"] for s in plan.subagents] == ["desktopish"]


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
        rubric_gate=None, sandbox="EXEC",
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
        rubric_gate=None, sandbox="EXEC",
    )
    gp = next(s for s in plan.subagents if s["name"] == "general-purpose")
    assert gp["description"] == "custom desc"
    # GP prompt then org-wide suffix (most-specific last).
    assert gp["system_prompt"] == "custom prompt\n\nORG SUFFIX"
    # Full specialist surface (ctx_tools stubbed empty here) — NOT dead.
    assert {t.name for t in gp["tools"]} == {
        "pux_sandbox_python", "pux_sandbox_desktop_screenshot",
    }
    assert gp["model"] == "WORKER_MODEL"


def test_general_purpose_not_double_emitted_when_roster_has_it(fake_tree, stub_factory):
    """If an org literally rostered a ``general-purpose`` specialist in org.yaml
    (unusual but possible), pux must NOT double-emit — the roster entry wins, one
    slot. Mirrors deepagents' own ``not any(...)`` guard at graph.py:717."""
    (fake_tree / "orgs" / "p" / "org.yaml").write_text(
        "agents: [desktopish, general-purpose]\n")
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
        rubric_gate=None, sandbox="EXEC",
    )
    gps = [s for s in plan.subagents if s["name"] == "general-purpose"]
    assert len(gps) == 1
    # The roster entry wins (its body), NOT the neutered spec.
    assert "roster body" in gps[0]["system_prompt"]


# --- BrowserVisionMiddleware mount (env-gated, default ON) ------------------

# --- prompt assembly (profile.base_system_prompt / system_prompt_suffix) ----

def test_profile_base_system_prompt_replaces_assembled(fake_tree, stub_factory):
    """``profile.base_system_prompt`` was REMOVED — it was a global-REPLACE
    that wiped the assembled prompt. A profile shipping it must FAIL loud
    (a stray one is a gap, not a silent drop). The factory rejects it."""
    cfg = HarnessProfileConfig(base_system_prompt="FULL_REPLACE")
    with pytest.raises(ValueError, match="base_system_prompt.*removed"):
        stack.build_stack(
            "p", specialists=list(_SPECIALISTS), profile=cfg,
            rubric_gate=None, sandbox="EXEC",
        )


def test_profile_system_prompt_suffix_appends(fake_tree, stub_factory):
    """``profile.system_prompt_suffix`` is appended to the assembled prompt
    (after a base_system_prompt replace if both are set)."""
    cfg = HarnessProfileConfig(system_prompt_suffix="SUFFIX_MARKER")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=cfg,
        rubric_gate=None, sandbox="EXEC",
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
        rubric_gate=_gate(max_iterations=5), sandbox="EXEC",
    )
    mw = plan.supervisor_middleware
    assert mw == ["ROUTE", "GUIDE", "PROMPT", "RUBRIC", "RETRY", "READFILE"]
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
        rubric_gate=_gate(enabled=False), sandbox="EXEC",
    )
    assert plan.supervisor_middleware == ["ROUTE", "GUIDE", "PROMPT", "RETRY", "READFILE"]
    assert stub_factory["rubric"] == []


def test_rubric_in_default_list_is_noop_without_gate(fake_tree, stub_factory):
    """RubricMiddleware's build returns None when no gate is armed, so even if
    'rubric' were in the default list it would contribute nothing — the name
    can sit in defaults without forcing construction. (Today it's appended at
    resolve time only when the gate arms; this test pins the None-skip.)"""
    # Simulate 'rubric' in the resolved names with no gate: build returns None.
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, sandbox="EXEC",
    )
    assert "RUBRIC" not in plan.supervisor_middleware


# --- the ``middleware:`` override block (the org override layer) -----------

# --- the opt-in ``audit`` spec ---------------------------------------------

# --- the deepagents ``excluded_middleware`` field (was a dead path) --------


# --- fail-loud: unknown names + wrong scopes -------------------------------

# --- the rules seam (runtime-facts policy layer) ---------------------------

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

# --- the full supervisor order (context layer armed + gate armed) ------------

def test_full_supervisor_order_is_canonical_registry_order(fake_tree, stub_factory, monkeypatch):
    """The FULL order — context, routing, session_guide, prompt_capture, rubric,
    model_retry, read_file_vision — when the context layer AND the gate are
    armed. MIDDLEWARE_NAMES order is canonical, so the emitted supervisor stack
    is exactly the registry's sequence (browser_vision is GONE from the stack:
    browser capability migrated to the ``sandbox_browser`` MCP server, whose
    in-container server does its own vision handling)."""
    monkeypatch.setattr(_dc_build(), "build_context_layer",
                        lambda **kw: (["CONTEXT"], []))
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=_gate(), sandbox="EXEC",
    )
    mw = plan.supervisor_middleware
    assert mw == ["CONTEXT", "ROUTE", "GUIDE", "PROMPT", "RUBRIC",
                  "RETRY", "READFILE"]


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
        rubric_gate=None, sandbox="EXEC",
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
        rubric_gate=None, sandbox="EXEC",
        facts=stack.RuntimeFacts(transport="serve"),
    )
    assert "ask_user" in _names(plan)
    assert "END your turn" not in plan.supervisor_prompt


def test_ask_user_over_editor_interrupts_no_suffix(fake_tree, stub_factory):
    """Opt-in + editor transport (acp) → ask_user IS present AND the supervisor
    prompt does NOT gain the end-turn suffix. Over ACP the tool halts via a real
    langgraph interrupt (the server presents the question, ends the turn, and the
    user's next freeform message resumes the thread) — so the suffix is unneeded,
    same as the web path. Only ``direct``/``tui`` (no resumable channel) append
    the suffix; see ``test_ask_user_turn_based_predicate`` in test_hitl.py."""
    _opt_in_ask_user(fake_tree)
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, sandbox="EXEC",
        facts=stack.RuntimeFacts(transport="acp"),
    )
    assert "ask_user" in _names(plan)
    assert "END your turn" not in plan.supervisor_prompt


def test_ask_user_dropped_over_mcp_even_if_opted_in(fake_tree, stub_factory):
    """The MCP caller can't answer an ask_user → the tool is DROPPED at
    construction (absent from the surface), not constructed-then-no-op'd. The
    org flag is the opt-in half; the runtime gate (``mcp_active``) is the other."""
    _opt_in_ask_user(fake_tree)
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, sandbox="EXEC",
        facts=stack.RuntimeFacts(transport="mcp", mcp_active=True),
    )
    assert "ask_user" not in _names(plan)


def test_ask_user_dropped_when_autonomous_even_if_opted_in(fake_tree, stub_factory):
    """Headless/autonomous runtimes have no human to resume an interrupt →
    ask_user is dropped at construction."""
    _opt_in_ask_user(fake_tree)
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, sandbox="EXEC",
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


# --- constructed-from-parts prompt assembly (prompt_parts) ------------------
# The supervisor prompt is no longer scattered ad-hoc string ops in build_stack —
# it is ``assemble_prompt`` over ``SUPERVISOR_PROMPT_PARTS``. These two prove the
# factory actually ROUTES through that registry (no inline ops left behind) and
# that the dead ``base_system_prompt`` nuclear-replace is a runtime failure too.
# The org-agnostic assembler unit tests + the per-agent / validate_profile
# rejection sites live in pux-harness/tests/harness/test_prompt_parts.py.


def test_build_stack_rejects_base_system_prompt_profile(fake_tree, stub_factory):
    """Runtime defense-in-depth for the long-lived server path (which bypasses
    the offline ``validate_profile`` tripwire): a ``base_system_prompt`` on the
    profile fails BEFORE prompt assembly + model init. The guard fires after
    middleware resolution, so the stubbed resolver keeps it cheap."""
    with pytest.raises(ValueError, match="base_system_prompt"):
        stack.build_stack(
            "p",
            specialists=list(_SPECIALISTS),
            profile=HarnessProfileConfig(base_system_prompt="NUKE"),
            rubric_gate=None,
            sandbox="EXEC",
        )


def test_supervisor_prompt_is_the_assembled_registry(fake_tree, stub_factory):
    """The factory's supervisor prompt EQUALS ``assemble_prompt`` over the
    registry with the SAME inputs build_stack uses — proving no inline string
    op shapes the prompt. The static base is ``orgs.build_system_prompt`` (the
    very call agents_md_core wraps); each conditional flag is read off its
    AUTHORITATIVE source — the interpreter gate from the RESOLVED middleware
    (the same ``_interpreter_mounted(supervisor_middleware)`` call the factory
    makes), the ask_user gate from the suffix's presence in the live prompt —
    so the test asserts the decomposition whatever the mount/flag decision is
    today (the prompt routes through the registry regardless)."""
    from pux_harness.agent.hitl import ASK_USER_PROMPT_SUFFIX

    plan = stack.build_stack(
        "p",
        specialists=list(_SPECIALISTS),
        profile=None,
        rubric_gate=None,
        sandbox="EXEC",
    )
    expected = assemble_prompt(
        SUPERVISOR_PROMPT_PARTS,
        PromptCtx(
            agents_md_base=orgs.build_system_prompt("p"),
            system_prompt_suffix=None,
            ask_user_active=ASK_USER_PROMPT_SUFFIX in plan.supervisor_prompt,
            interpreter_mounted=_interpreter_mounted(plan.supervisor_middleware),
        ),
        PromptScope.SUPERVISOR,
    )
    assert plan.supervisor_prompt == expected


# --- per-org tool-surface scoping (policy.yaml tool_surface.groups) --------
# The SUPERVISOR's specialist tools are scoped by capability group; the
# subagent surface stays full (subagents resolve their own tools: allowlist
# against the un-scoped tools_surface). This is the prompt-bloat fix: a coding
# org drops browser/desktop from the CTO prompt without losing capability.

def test_scope_supervisor_tools_unit():
    """``_scope_supervisor_tools`` drops only ``pux_sandbox_*`` specialists whose
    group/slug isn't allowed; native (no prefix), mcp (``mcp__``), context
    retrieval, and ``ask_user`` pass through untouched. ``frozenset()`` is the
    default (no specialists on the supervisor surface); a non-empty allowlist
    opts the named slugs back in."""
    tools = [
        _mk_tool("pux_sandbox_python"),
        _mk_tool("pux_sandbox_desktop_screenshot"),
        _mk_tool("execute"),          # native fs/shell (no prefix)
        _mk_tool("mcp__web_research__search"),
        _mk_tool("ctx_recall"),       # context retrieval
        _mk_tool("ask_user"),         # HITL, appended after scoping
    ]
    # Empty allow set → every registry specialist scoped away; non-specialists
    # (native, mcp, ctx, ask_user) pass through untouched.
    empty_names = {t.name for t in stack._scope_supervisor_tools(tools, frozenset())}
    assert empty_names == {
        "execute", "mcp__web_research__search", "ctx_recall", "ask_user",
    }
    # Only python allowed → browser dropped, everything else kept.
    scoped = stack._scope_supervisor_tools(tools, frozenset({"python"}))
    names = {t.name for t in scoped}
    assert names == {
        "pux_sandbox_python", "execute", "mcp__web_research__search",
        "ctx_recall", "ask_user",
    }


def _write_policy(fake_tree: Path, body: str) -> None:
    (fake_tree / "orgs" / "p" / "policy.yaml").write_text(body)


def test_tool_surface_scopes_supervisor_not_subagents(fake_tree, stub_factory, monkeypatch):
    """With ``tool_surface.groups: [code]`` the supervisor loses
    ``desktop_screenshot`` but the ``desktopish`` subagent (which lists
    ``desktop_screenshot`` in its ``tools:``) KEEPS it — it resolves against the
    full ``tools_surface``, never the scoped supervisor list."""
    # build_stack reads policy via its OWN imported ``_orgs_dir`` binding, so the
    # fixture's monkeypatch on orgs._orgs_dir doesn't reach it — patch both.
    monkeypatch.setattr(stack, "_orgs_dir", orgs._orgs_dir)
    _write_policy(fake_tree, "tool_surface:\n  groups: [code]\n")
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, sandbox="EXEC",
    )
    sup_names = {t.name for t in plan.supervisor_tools}
    assert "pux_sandbox_python" in sup_names
    assert "pux_sandbox_desktop_screenshot" not in sup_names
    # subagent still resolves its declared browser tool
    assert plan.subagents[0]["name"] == "desktopish"
    sub_tools = {t.name for t in plan.subagents[0]["tools"]}
    assert "pux_sandbox_desktop_screenshot" in sub_tools


def test_no_tool_surface_is_byte_identical(fake_tree, stub_factory):
    """An org with NO ``tool_surface`` block carries NO registry specialists on
    the supervisor (the anti-flood default). Specialists still reach the
    supervisor via subagents."""
    plan = stack.build_stack(
        "p", specialists=list(_SPECIALISTS), profile=None,
        rubric_gate=None, sandbox="EXEC",
    )
    sup_names = {t.name for t in plan.supervisor_tools}
    assert not any(n.startswith("pux_sandbox_") for n in sup_names), (
        f"no-tool_surface supervisor should carry no pux_sandbox_* specialists, got {sup_names}"
    )
