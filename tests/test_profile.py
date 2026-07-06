"""Per-org ``HarnessProfile`` wiring (Phase 16.3b).

Proves the profile seam through the REAL ``build_graph`` entry point — not just
the helpers — per the prepare-wiring-e2e-gap rule (a wiring seam proven only by
isolated unit test is unproven). ``build_graph``'s heavy deps (model init,
Docker exec client, the offload middleware, ``create_deep_agent`` itself) are
stubbed; the profile-loading + application logic under test runs for real.

What must hold:
- ``system_prompt_suffix`` lands on BOTH the CTO prompt AND each subagent's
  prompt (org-wide override reaches the shared browser agent, not just CTO).
- ``tool_description_overrides`` rewrites the named tool's description in BOTH
  the main tool list AND the subagent's resolved whitelist.
- ``excluded_tools`` is filtered from BOTH places.
- No ``profile.yaml`` -> ``build_graph`` is byte-identical to today (regression).
- ``load_profile`` / ``validate_profile`` read + parse the YAML faithfully.
"""
from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest
from deepagents import HarnessProfileConfig
from langchain_core.tools import StructuredTool
from pydantic import BaseModel

from pux_harness.agent import graph, orgs, profile, stack
from pux_harness.context.layer import build_context_layer


# --- fakes -----------------------------------------------------------------

class _NoArgs(BaseModel):
    """Empty args schema (mirrors tools.py's argument-less tool idiom)."""


def _mk_tool(name: str, desc: str) -> StructuredTool:
    """A real BaseTool (so ``_apply_tool_description_overrides`` rewrites it)."""
    return StructuredTool(
        name=name, description=desc, args_schema=_NoArgs, func=lambda: ""
    )


_SPECIALISTS = [
    _mk_tool("pux_sandbox_python", "run python"),
    _mk_tool("pux_sandbox_browser_save_session", "save session (original desc)"),
    _mk_tool("pux_sandbox_browser_navigate", "navigate to a url"),
]


def _ctx() -> dict:
    """The context layer the stack factory builds and threads into subagents.
    ``load_subagents`` requires it explicitly now (one way: the loader no longer
    builds the layer itself)."""
    mw, tools = build_context_layer()
    return {"subagent_middleware": mw, "retrieval_tools": tools}

# The middleware ``build_graph`` ALWAYS mounts, in order, regardless of profile:
# the unified context layer's middleware (capture+offload in one
# ContextMiddleware — graph.py calls ``build_context_layer()``) PLUS
# RoutingMiddleware + SessionGuideMiddleware. The ``captured_build`` fixture
# stubs ``build_context_layer`` to empty + the two named middleware to marker
# strings, so a captured ``middleware`` list with no rubric gate is exactly
# this. Asserted in the no-gate / disabled-gate tests; updating the baseline
# lives here, once. (The capture+offload behavior itself is proven in
# test_context_offload.py — here it's stubbed away to keep the profile test
# focused on the wiring shape.)
_BASELINE_MIDDLEWARE = ["ROUTE", "GUIDE"]


@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """Scratch orgs/ tree on the ``orgs`` module path helpers. The org ``p``
    roster includes a browser-like agent with a tools whitelist so the
    subagent-side application is observable."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "skills").mkdir(parents=True)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    # BrowserVisionMiddleware is env-gated (default ON); pin it OFF so the
    # baseline tests below see EXACTLY _BASELINE_MIDDLEWARE (the always-mounted
    # routing+session_guide pair). The vision mount is proven in test_stack.py.
    monkeypatch.setenv("PUX_BROWSER_VISION", "0")

    # org "p" with a CTO overlay + a browser-like specialist.
    d = tmp_path / "orgs" / "p"
    d.mkdir(parents=True)
    (d / "AGENTS.md").write_text("# p\n\nCTO prose, no frontmatter.\n")
    (d / "org.yaml").write_text(
        "agents: [browserish]\n"
    )
    bdir = d / "agents"
    bdir.mkdir()
    (bdir / "browserish.md").write_text(
        "---\n"
        'name: "browserish"\n'
        'description: "browses"\n'
        'tools: ["browser_save_session", "browser_navigate"]\n'
        "---\n\nbrowser body.\n"
    )
    return tmp_path


def _cfg(
    *,
    suffix: str | None = None,
    overrides: dict[str, str] | None = None,
    excluded: set[str] | None = None,
    base: str | None = None,
) -> HarnessProfileConfig:
    return HarnessProfileConfig(
        base_system_prompt=base,
        system_prompt_suffix=suffix,
        tool_description_overrides=overrides or {},
        excluded_tools=frozenset(excluded or ()),
    )


@pytest.fixture
def captured_build(monkeypatch):
    """Stub build_graph's heavy deps; capture the kwargs handed to
    ``create_deep_agent``. Returns the capture dict."""
    cap: dict[str, Any] = {}

    monkeypatch.setattr(graph, "get_model", lambda *a, **k: "MODEL")
    monkeypatch.setattr(graph, "shared_exec", lambda: "EXEC")
    monkeypatch.setattr(graph, "shared_backend", lambda: "BACKEND")
    monkeypatch.setattr(graph, "build_native_specialists",
                        lambda *a, **k: list(_SPECIALISTS))
    # Phase 21: the middleware assembly moved into ``stack.build_stack`` (the
    # factory), so the build_context_layer + RoutingMiddleware +
    # SessionGuideMiddleware stubs target the ``stack`` module's namespace
    # (where ``build_stack`` looks them up), NOT ``graph``'s. ``graph.py`` is
    # now thin — it imports none of those (the no-legacy-middleware-in-graph
    # contract tripwire enforces that). Stubbed to empty/baseline so the
    # no-gate middleware is just ROUTE+GUIDE; the capture/offload behavior is
    # proven separately in test_context_offload.py. ``stack`` resolves the
    # grader model via its OWN ``get_model`` import when a rubric gate arms —
    # stub that too so the rubric tests see ``"MODEL"``.
    monkeypatch.setattr(stack, "build_context_layer", lambda: ([], []))
    monkeypatch.setattr(stack, "RoutingMiddleware", lambda: "ROUTE")
    monkeypatch.setattr(stack, "SessionGuideMiddleware", lambda: "GUIDE")
    monkeypatch.setattr(stack, "get_model", lambda *a, **k: "MODEL")
    monkeypatch.setattr(
        graph, "create_deep_agent",
        lambda **kw: cap.update(kw) or "GRAPH",
    )
    return cap


# --- build_graph end-to-end application ------------------------------------

def test_suffix_lands_on_cto_and_subagent(fake_tree, captured_build):
    """system_prompt_suffix is appended to BOTH the CTO prompt and each
    specialist's prompt (the org-wide override reaches the shared browser
    agent, the user's stated goal)."""
    cfg = _cfg(suffix="SUFFIX_MARKER")
    monkeypatch_target = pytest.MonkeyPatch()
    monkeypatch_target.setattr(graph, "load_profile", lambda org: cfg)
    try:
        graph.build_graph("p", checkpointer=None)
    finally:
        monkeypatch_target.undo()

    assert captured_build["system_prompt"].endswith("SUFFIX_MARKER")
    # The browserish subagent inherits the suffix too.
    sub = next(s for s in captured_build["subagents"] if s["name"] == "browserish")
    assert sub["system_prompt"].endswith("SUFFIX_MARKER")


def test_tool_description_override_applies_everywhere(fake_tree, captured_build):
    """tool_description_overrides rewrites the named tool's description in the
    main tool list AND the subagent's resolved whitelist."""
    cfg = _cfg(overrides={
        "pux_sandbox_browser_save_session": "OVERRIDE_MARKER",
    })
    mp = pytest.MonkeyPatch()
    mp.setattr(graph, "load_profile", lambda org: cfg)
    try:
        graph.build_graph("p", checkpointer=None)
    finally:
        mp.undo()

    main = next(t for t in captured_build["tools"]
                if t.name == "pux_sandbox_browser_save_session")
    assert main.description == "OVERRIDE_MARKER"

    sub = next(s for s in captured_build["subagents"] if s["name"] == "browserish")
    ssub = next(t for t in sub["tools"]
                if t.name == "pux_sandbox_browser_save_session")
    assert ssub.description == "OVERRIDE_MARKER"


def test_excluded_tool_filtered_everywhere(fake_tree, captured_build):
    """excluded_tools drops the named tool from BOTH the main list and the
    subagent whitelist."""
    cfg = _cfg(excluded={"pux_sandbox_browser_save_session"})
    mp = pytest.MonkeyPatch()
    mp.setattr(graph, "load_profile", lambda org: cfg)
    try:
        graph.build_graph("p", checkpointer=None)
    finally:
        mp.undo()

    names = {t.name for t in captured_build["tools"]}
    assert "pux_sandbox_browser_save_session" not in names

    sub = next(s for s in captured_build["subagents"] if s["name"] == "browserish")
    sub_names = {t.name for t in sub["tools"]}
    assert "pux_sandbox_browser_save_session" not in sub_names
    # Non-excluded tools survive.
    assert "pux_sandbox_browser_navigate" in sub_names


def test_base_system_prompt_replaces(fake_tree, captured_build):
    """base_system_prompt (when set) REPLACES the assembled CTO prompt rather
    than appending."""
    cfg = _cfg(base="FULL_REPLACE")
    mp = pytest.MonkeyPatch()
    mp.setattr(graph, "load_profile", lambda org: cfg)
    try:
        graph.build_graph("p", checkpointer=None)
    finally:
        mp.undo()

    assert captured_build["system_prompt"] == "FULL_REPLACE"


def test_no_profile_is_byte_identical(fake_tree, captured_build):
    """Regression: no profile.yaml -> build_graph behaves exactly as today
    (no suffix, no filtering, no override)."""
    mp = pytest.MonkeyPatch()
    mp.setattr(graph, "load_profile", lambda org: None)
    try:
        graph.build_graph("p", checkpointer=None)
    finally:
        mp.undo()

    assert "SUFFIX_MARKER" not in captured_build["system_prompt"]
    names = {t.name for t in captured_build["tools"]}
    assert names == {t.name for t in _SPECIALISTS}
    # The original description is untouched.
    main = next(t for t in captured_build["tools"]
                if t.name == "pux_sandbox_browser_save_session")
    assert main.description == "save session (original desc)"


def test_build_graph_requests_base_and_multimodal_roles(fake_tree, monkeypatch):
    """Phase 17.B.0 wiring proof (prepare-wiring-e2e-gap): build_graph drives
    the model-role spec through the REAL entry point — it asks get_model for the
    ``base`` role (the CTO driver) AND the ``multimodal`` role (describe_image,
    decoupled from base) with the org threaded through. Not assumed from a
    stub that swallows kwargs — captured here by recording the call args."""
    calls: list[tuple[str, str | None]] = []

    def _fake_get_model(*, role="base", org=None, model=None):
        calls.append((role, org))
        return f"MODEL-{role}"

    monkeypatch.setattr(graph, "get_model", _fake_get_model)
    monkeypatch.setattr(graph, "shared_exec", lambda: "EXEC")
    monkeypatch.setattr(graph, "shared_backend", lambda: "BACKEND")
    monkeypatch.setattr(graph, "build_native_specialists",
                        lambda *a, **k: list(_SPECIALISTS))
    # Phase 21: middleware assembly lives in ``stack.build_stack``; stub the
    # stack-level names so the factory runs to completion. This test only
    # asserts which roles ``graph.get_model`` was asked for at the graph layer
    # — not the middleware shape (that's captured_build's job elsewhere).
    monkeypatch.setattr(stack, "build_context_layer", lambda: ([], []))
    monkeypatch.setattr(stack, "RoutingMiddleware", lambda: "ROUTING")
    monkeypatch.setattr(stack, "SessionGuideMiddleware", lambda: "SESSION")
    monkeypatch.setattr(graph, "create_deep_agent", lambda **kw: "GRAPH")
    monkeypatch.setattr(graph, "load_profile", lambda org: None)

    graph.build_graph("p", checkpointer=None)

    # base (CTO) + multimodal (describe_image) both resolved, both carry the org.
    assert ("base", "p") in calls
    assert ("multimodal", "p") in calls
    # No other role leaks in at the graph layer for a NO-GATE org (worker is
    # per-subagent; grader is only resolved when the org opts into the gate).
    roles = {role for role, _ in calls}
    assert roles == {"base", "multimodal"}


# --- Phase 17.B.3: RubricMiddleware wiring ---------------------------------

def test_rubric_gate_appends_middleware(fake_tree, captured_build, monkeypatch):
    """Phase 17.B.3 wiring proof (prepare-wiring-e2e-gap): an org that opts into
    the rubric gate gets ``RubricMiddleware`` appended after the offload
    middleware, constructed with the GRADER role model, the 3 grader tools, and
    the gate's max_iterations. Drives the REAL build_graph; RubricMiddleware is
    captured via a stub so its construction kwargs are asserted directly."""
    rubric_kwargs: dict[str, Any] = {}

    def _fake_rubric_mw(**kwargs):
        rubric_kwargs.update(kwargs)
        return "RUBRIC_MW"

    monkeypatch.setattr(stack, "RubricMiddleware", _fake_rubric_mw)
    # Write org p a rubric gate (no other profile fields).
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: true\n"
        "  max_iterations: 3\n"
        "  default: 'ship-gate rubric'\n"
    )

    graph.build_graph("p", checkpointer=None)

    mw = captured_build["middleware"]
    assert "ROUTE" in mw            # baseline context middleware still mounted
    assert "RUBRIC_MW" in mw        # RubricMiddleware appended
    assert mw.index("ROUTE") < mw.index("RUBRIC_MW")  # baseline before rubric
    assert rubric_kwargs["max_iterations"] == 3
    # The grader's 3 evidence tools (execute / read_file / grep).
    assert len(rubric_kwargs["tools"]) == 3
    # The grader model came from get_model (captured_build stubs stack.get_model
    # -> "MODEL"); proving build_stack asked for it via the role-resolved path.
    assert rubric_kwargs["model"] == "MODEL"


def test_no_rubric_gate_no_rubric_middleware(fake_tree, captured_build, monkeypatch):
    """Regression: an org with NO rubric block mounts only the offload
    middleware — byte-identical to today. RubricMiddleware is never constructed."""
    constructed = []

    def _bomb_rubric_mw(**kwargs):
        constructed.append(kwargs)
        raise AssertionError("RubricMiddleware must not be constructed without a gate")

    monkeypatch.setattr(stack, "RubricMiddleware", _bomb_rubric_mw)
    # org p has no profile.yaml in fake_tree.
    graph.build_graph("p", checkpointer=None)

    assert constructed == []
    assert captured_build["middleware"] == _BASELINE_MIDDLEWARE


def test_rubric_gate_disabled_mounts_no_middleware(fake_tree, captured_build, monkeypatch):
    """``rubric.enabled: false`` is the operator kill-switch — the gate is
    present but disabled, so no RubricMiddleware mounts (a future deepagents API
    break is killed by flipping one flag, per the beta mitigation)."""
    constructed: list[dict] = []
    monkeypatch.setattr(
        stack, "RubricMiddleware", lambda **k: constructed.append(k) or "RUBRIC_MW"
    )
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: false\n"
        "  max_iterations: 3\n"
    )

    graph.build_graph("p", checkpointer=None)

    assert constructed == []
    assert captured_build["middleware"] == _BASELINE_MIDDLEWARE


# --- Phase 17.B.1: RubricGate + default_rubric (helper level) --------------

def test_load_rubric_gate_parses_block(fake_tree):
    """load_rubric_gate reads the rubric: block into a RubricGate with the
    expected fields (enabled / max_iterations / default)."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: true\n"
        "  max_iterations: 5\n"
        "  default: 'ship it'\n"
    )
    gate = profile.load_rubric_gate("p")
    assert gate == profile.RubricGate(enabled=True, max_iterations=5, default="ship it")


def test_load_rubric_gate_none_when_no_block(fake_tree):
    """Regression: a profile.yaml with no rubric: block -> no gate (the common
    case; build_graph's no-op path)."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "system_prompt_suffix: 'hi'\n"
    )
    assert profile.load_rubric_gate("p") is None


def test_load_rubric_gate_none_when_no_profile(fake_tree):
    """No profile.yaml at all -> no gate."""
    assert profile.load_rubric_gate("p") is None
    assert profile.load_rubric_gate("general") is None


def test_default_rubric_returns_text(fake_tree):
    """default_rubric returns the gate's default text (what _execute/_run inject)."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: true\n"
        "  default: 'GRADE ME'\n"
    )
    assert profile.default_rubric("p") == "GRADE ME"


def test_default_rubric_none_when_disabled(fake_tree):
    """enabled: false -> default_rubric returns None (operator kill-switch; the
    gate is not armed even though a default is present)."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: false\n"
        "  default: 'GRADE ME'\n"
    )
    assert profile.default_rubric("p") is None


def test_default_rubric_none_when_no_default(fake_tree):
    """A gate with no default text -> None (nothing to inject; the middleware
    stays a no-op)."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: true\n"
    )
    assert profile.default_rubric("p") is None


def test_rubric_block_peeled_from_profile_config(fake_tree):
    """The rubric: block is peeled out BEFORE HarnessProfileConfig.from_dict
    (which would reject it as unknown) AND does not leak into the config object.
    A coexisting profile field still loads alongside it."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "system_prompt_suffix: 'SUFFIX'\n"
        "rubric:\n"
        "  enabled: true\n"
        "  default: 'ship it'\n"
    )
    cfg = profile.load_profile("p")
    assert cfg is not None
    assert cfg.system_prompt_suffix == "SUFFIX"   # profile still loads
    # The gate reads independently from the same file.
    assert profile.load_rubric_gate("p").default == "ship it"


def test_load_rubric_gate_rejects_legacy_grader_model(fake_tree):
    """no-legacy-left-behind: the OLD ``rubric.grader_model`` form (moved to the
    top-level ``models:`` map in Phase 17.B.0) is a PERMANENT contract failure,
    not silently ignored. The error points at the new home."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: true\n"
        "  grader_model: glm-5.2\n"
    )
    with pytest.raises(TypeError, match="grader_model moved"):
        profile.load_rubric_gate("p")


def test_load_rubric_gate_rejects_bad_max_iterations(fake_tree):
    """A non-int max_iterations fails loud at load time, not at the first invoke."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: true\n"
        "  max_iterations: 'three'\n"
    )
    with pytest.raises(TypeError, match="max_iterations"):
        profile.load_rubric_gate("p")


# --- load_profile / validate_profile ---------------------------------------

def test_load_profile_none_when_absent(fake_tree):
    """Most orgs ship no profile -> None (build_graph's no-op path)."""
    assert profile.load_profile("p") is None


def test_load_profile_reads_real_twitter_profile():
    """The shipped sample parses into a HarnessProfileConfig with the expected
    fields (proves the sample + the loader against the real repo)."""
    cfg = profile.load_profile("twitter-agent")
    assert cfg is not None
    assert "draft" in (cfg.system_prompt_suffix or "")
    assert "pux_sandbox_browser_save_session" in cfg.tool_description_overrides


def test_load_profile_parses_written_yaml(fake_tree):
    """A profile.yaml written into the fake tree loads."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "system_prompt_suffix: 'hi'\n"
        "tool_description_overrides:\n"
        "  pux_sandbox_python: 'x'\n"
        "excluded_tools: []\n"
    )
    cfg = profile.load_profile("p")
    assert cfg.system_prompt_suffix == "hi"
    assert cfg.tool_description_overrides == {"pux_sandbox_python": "x"}


def test_load_profile_rejects_unknown_key(fake_tree):
    """An unknown key fails loud (HarnessProfileConfig.from_dict TypeError)."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "bogus_field: 1\n"
    )
    with pytest.raises(TypeError, match="Unknown keys"):
        profile.load_profile("p")


def test_load_profile_rejects_legacy_subagents_block(fake_tree):
    """no-legacy-left-behind (Phase 2 fold): the top-level ``subagents:`` block —
    the old second partial-override surface — is no longer peeled before
    ``HarnessProfileConfig.from_dict``, so it fails loud as an unknown key (the
    build-time second layer beneath the contract's ``no-legacy-subagents-block``
    tripwire). Per-agent overrides now live in each agent's own frontmatter +
    ``extends:``."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "subagents:\n"
        "  some-slug:\n"
        "    system_prompt_suffix: be terse\n"
    )
    with pytest.raises(TypeError, match="Unknown keys"):
        profile.load_profile("p")


def test_load_profile_rejects_non_mapping(fake_tree):
    """A non-mapping top level fails loud (not silently treated as empty)."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "- just\n- a\n- list\n"
    )
    with pytest.raises(TypeError, match="mapping"):
        profile.load_profile("p")


def test_validate_profile_round_trips(fake_tree):
    """validate_profile returns the cfg (or None) and raises on malformed —
    the contract checker's entry point."""
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "system_prompt_suffix: 's'\n"
    )
    assert profile.validate_profile("p") is not None
    assert profile.validate_profile("general") is None  # no profile -> None


def test_load_subagents_default_profile_none_preserves_behavior(fake_tree):
    """load_subagents(org, tools) with no profile arg preserves the specialist
    whitelist resolution (the regression contract) — the browserish subagent
    gets exactly its two declared ``browser_*`` tools. Phase 19 additionally
    threads the unified context layer into every subagent, so the retrieval
    tools ``ctx_recall`` + ``ctx_search`` are appended to the whitelist too
    (graph.py retracts the old "main-agent-only" claim — verified against
    deepagents 0.6.12)."""
    subs = orgs.load_subagents("p", _SPECIALISTS, **_ctx())
    sub = next(s for s in subs if s["name"] == "browserish")
    assert sub["system_prompt"] == "browser body."
    names = {t.name for t in sub["tools"]}
    # The declared specialist whitelist resolved faithfully.
    assert {"pux_sandbox_browser_save_session",
            "pux_sandbox_browser_navigate"} <= names
    # Phase 19: the context retrieval surface reaches every subagent.
    assert {"ctx_recall", "ctx_search"} <= names
    # No surprise specialists leak in (python is NOT in this subagent's whitelist).
    assert "pux_sandbox_python" not in names


def test_load_subagents_applies_profile(fake_tree):
    """Direct proof at the load_subagents layer: suffix + override + exclude
    land on the subagent even without going through build_graph."""
    cfg = _cfg(
        suffix="EXTRA",
        overrides={"pux_sandbox_browser_save_session": "REDONE"},
        excluded={"pux_sandbox_browser_navigate"},
    )
    subs = orgs.load_subagents("p", _SPECIALISTS, profile=cfg, **_ctx())
    sub = next(s for s in subs if s["name"] == "browserish")
    assert sub["system_prompt"].endswith("EXTRA")
    names = {t.name for t in sub["tools"]}
    assert "pux_sandbox_browser_navigate" not in names
    kept = next(t for t in sub["tools"]
                if t.name == "pux_sandbox_browser_save_session")
    assert kept.description == "REDONE"
