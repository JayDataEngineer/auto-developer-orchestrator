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
    # No other role leaks in at the graph layer (worker is per-subagent; grader
    # is the Phase 17.B middleware, not yet wired).
    roles = {role for role, _ in calls}
    assert roles == {"base", "multimodal"}


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
