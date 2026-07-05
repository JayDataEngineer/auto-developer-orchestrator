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

from pux_harness.agent import graph, orgs, profile


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
    monkeypatch.setattr(graph, "build_ctx_tools", lambda: [])
    monkeypatch.setattr(graph, "ContextOffloadMiddleware", lambda: "OFFLOAD")
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
    monkeypatch.setattr(graph, "build_ctx_tools", lambda: [])
    monkeypatch.setattr(graph, "ContextOffloadMiddleware", lambda: "OFFLOAD")
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

    monkeypatch.setattr(graph, "RubricMiddleware", _fake_rubric_mw)
    # Write org p a rubric gate (no other profile fields).
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: true\n"
        "  max_iterations: 3\n"
        "  default: 'ship-gate rubric'\n"
    )

    graph.build_graph("p", checkpointer=None)

    mw = captured_build["middleware"]
    assert "OFFLOAD" in mw          # ContextOffloadMiddleware still mounted
    assert "RUBRIC_MW" in mw        # RubricMiddleware appended
    assert mw.index("OFFLOAD") < mw.index("RUBRIC_MW")  # offload first
    assert rubric_kwargs["max_iterations"] == 3
    # The grader's 3 evidence tools (execute / read_file / grep).
    assert len(rubric_kwargs["tools"]) == 3
    # The grader model came from get_model (captured_build stubs it -> "MODEL");
    # proving build_graph asked for it via the role-resolved path.
    assert rubric_kwargs["model"] == "MODEL"


def test_no_rubric_gate_no_rubric_middleware(fake_tree, captured_build, monkeypatch):
    """Regression: an org with NO rubric block mounts only the offload
    middleware — byte-identical to today. RubricMiddleware is never constructed."""
    constructed = []

    def _bomb_rubric_mw(**kwargs):
        constructed.append(kwargs)
        raise AssertionError("RubricMiddleware must not be constructed without a gate")

    monkeypatch.setattr(graph, "RubricMiddleware", _bomb_rubric_mw)
    # org p has no profile.yaml in fake_tree.
    graph.build_graph("p", checkpointer=None)

    assert constructed == []
    assert captured_build["middleware"] == ["OFFLOAD"]


def test_rubric_gate_disabled_mounts_no_middleware(fake_tree, captured_build, monkeypatch):
    """``rubric.enabled: false`` is the operator kill-switch — the gate is
    present but disabled, so no RubricMiddleware mounts (a future deepagents API
    break is killed by flipping one flag, per the beta mitigation)."""
    constructed: list[dict] = []
    monkeypatch.setattr(
        graph, "RubricMiddleware", lambda **k: constructed.append(k) or "RUBRIC_MW"
    )
    (fake_tree / "orgs" / "p" / "profile.yaml").write_text(
        "rubric:\n"
        "  enabled: false\n"
        "  max_iterations: 3\n"
    )

    graph.build_graph("p", checkpointer=None)

    assert constructed == []
    assert captured_build["middleware"] == ["OFFLOAD"]


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
    """load_subagents(org, tools) with no profile arg is byte-identical to
    today (every existing call site)."""
    subs = orgs.load_subagents("p", _SPECIALISTS)
    sub = next(s for s in subs if s["name"] == "browserish")
    assert sub["system_prompt"] == "browser body."
    assert {t.name for t in sub["tools"]} == {
        "pux_sandbox_browser_save_session",
        "pux_sandbox_browser_navigate",
    }


def test_load_subagents_applies_profile(fake_tree):
    """Direct proof at the load_subagents layer: suffix + override + exclude
    land on the subagent even without going through build_graph."""
    cfg = _cfg(
        suffix="EXTRA",
        overrides={"pux_sandbox_browser_save_session": "REDONE"},
        excluded={"pux_sandbox_browser_navigate"},
    )
    subs = orgs.load_subagents("p", _SPECIALISTS, profile=cfg)
    sub = next(s for s in subs if s["name"] == "browserish")
    assert sub["system_prompt"].endswith("EXTRA")
    names = {t.name for t in sub["tools"]}
    assert "pux_sandbox_browser_navigate" not in names
    kept = next(t for t in sub["tools"]
                if t.name == "pux_sandbox_browser_save_session")
    assert kept.description == "REDONE"
