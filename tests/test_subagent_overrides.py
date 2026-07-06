"""Phase 2 — per-subagent overrides in ``profile.yaml``.

Three tiers mirroring the feature layers:
1. **profile.py** — ``load_subagent_overrides`` parses + validates the block.
2. **orgs.py::load_subagents** — overrides are applied to the correct subagent.
3. **contract.py::check_org** — a typo'd slug fails ``--check-contract``.

All use tmpdir + monkeypatch (no server, no Docker, no tokens).
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest
from deepagents import HarnessProfileConfig

from pux_harness.agent import contract, orgs as orgs_mod
from pux_harness.agent.profile import (
    _SUBAGENT_OVERRIDE_KEYS,
    _subagent_overrides_from_block,
    load_subagent_overrides,
)


# --- helpers ---------------------------------------------------------------

_AGENT_BODY = "prose body\n"


def _write_profile(org_dir: Path, **blocks: str) -> None:
    """Write a ``profile.yaml`` with the given top-level blocks as YAML."""
    lines: list[str] = []
    for key, val in blocks.items():
        lines.append(f"{key}:")
        for line in val.split("\n"):
            lines.append(f"  {line}" if line else "")
    (org_dir / "profile.yaml").write_text("\n".join(lines) + "\n")


def _add_agent(org_dir: Path, slug: str, *,
               tools: list[str] | None = None) -> None:
    """Write an agent ``<slug>.md`` with optional tools."""
    agents_dir = org_dir / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    fm = ["---", f'name: "{slug}"', f'description: "{slug} specialist"']
    if tools is not None:
        fm.append(f"tools: {json.dumps(tools)}")
    fm.append("---")
    (agents_dir / f"{slug}.md").write_text("\n".join(fm) + f"\n\n{_AGENT_BODY}")


def _add_org(base: Path, name: str, *, agents: list[str] | None = None) -> Path:
    """Create an org with AGENTS.md + optional org.yaml roster."""
    d = base / "orgs" / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "AGENTS.md").write_text(f"# {name}\n")
    if agents is not None:
        (d / "org.yaml").write_text(f"agents: [{', '.join(agents)}]\n")
    return d


# --- 1. profile.py: load_subagent_overrides -------------------------------


def test_load_subagent_overrides_no_profile_returns_empty(tmp_path):
    """No profile.yaml → empty dict (not an error)."""
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    assert load_subagent_overrides("o") == {}


def test_load_subagent_overrides_no_subagents_block_returns_empty(
    tmp_path, monkeypatch,
):
    """profile.yaml present but no subagents: block → empty dict."""
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    _write_profile(d, system_prompt_suffix='"global"')
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    assert load_subagent_overrides("o") == {}


def test_load_subagent_overrides_valid_block(tmp_path, monkeypatch):
    """Well-formed subagents: block returns correct slug -> HarnessProfileConfig."""
    d = _add_org(tmp_path, "o", agents=["a", "b"])
    _add_agent(d, "a", tools=["read_file"])
    _add_agent(d, "b", tools=["execute"])
    _write_profile(
        d,
        subagents=(
            'a:\n'
            '  system_prompt_suffix: "suffix A"\n'
            '  excluded_tools: [read_file]\n'
        ),
    )
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    result = load_subagent_overrides("o")
    assert set(result) == {"a"}
    cfg = result["a"]
    assert isinstance(cfg, HarnessProfileConfig)
    assert cfg.system_prompt_suffix == "suffix A"
    assert cfg.excluded_tools == frozenset({"read_file"})


def test_load_subagent_overrides_complex_block(tmp_path, monkeypatch):
    """Multiple subagents with mixed override fields."""
    d = _add_org(tmp_path, "o", agents=["x", "y"])
    _add_agent(d, "x")
    _add_agent(d, "y")
    _write_profile(
        d,
        subagents=(
            'x:\n'
            '  base_system_prompt: "You are X."\n'
            '  tool_description_overrides:\n'
            '    pux_sandbox_python: "Run python."\n'
            'y:\n'
            '  system_prompt_suffix: "Be brief."\n'
        ),
    )
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    result = load_subagent_overrides("o")
    assert set(result) == {"x", "y"}
    assert result["x"].base_system_prompt == "You are X."
    assert result["y"].system_prompt_suffix == "Be brief."


def test_load_subagent_overrides_rejects_middleware_key(tmp_path, monkeypatch):
    """excluded_middleware in a subagent block fails with helpful error."""
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    _write_profile(
        d,
        subagents='a:\n  excluded_middleware: [routing]\n',
    )
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    with pytest.raises(TypeError, match="excluded_middleware.*not supported"):
        load_subagent_overrides("o")


def test_load_subagent_overrides_rejects_general_purpose_key(tmp_path, monkeypatch):
    """general_purpose_subagent in a subagent block fails with helpful error."""
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    _write_profile(
        d,
        subagents='a:\n  general_purpose_subagent:\n    name: gp\n',
    )
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    with pytest.raises(TypeError, match="general_purpose_subagent"):
        load_subagent_overrides("o")


def test_load_subagent_overrides_unknown_key(tmp_path, monkeypatch):
    """A key not in _SUBAGENT_OVERRIDE_KEYS raises TypeError."""
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    _write_profile(
        d,
        subagents='a:\n  bogus_field: 1\n',
    )
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    with pytest.raises(TypeError, match="bogus_field.*not a valid"):
        load_subagent_overrides("o")


def test_load_subagent_overrides_non_mapping_block(tmp_path, monkeypatch):
    """A non-mapping subagents: value raises TypeError."""
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    _write_profile(
        d,
        subagents="- just\n- a\n- list\n",
    )
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    with pytest.raises(TypeError, match="subagents: must be a mapping"):
        load_subagent_overrides("o")


def test_load_subagent_overrides_non_dict_sub_block(tmp_path, monkeypatch):
    """A sub-block that is not a mapping raises TypeError."""
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    _write_profile(
        d,
        subagents="a: just a string\n",
    )
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    with pytest.raises(TypeError, match="subagents.a: must be a mapping"):
        load_subagent_overrides("o")


def test_subagent_overrides_from_block_empty():
    """_subagent_overrides_from_block with empty dict returns empty."""
    assert _subagent_overrides_from_block("o", {}) == {}


def test_subagent_overrides_from_block_keeps_known_keys():
    """All _SUBAGENT_OVERRIDE_KEYS are accepted."""
    block = {
        "a": {
            "system_prompt_suffix": "suffix",
            "base_system_prompt": "base",
            "excluded_tools": ["tool_a"],
            "tool_description_overrides": {"pux_sandbox_python": "Run python"},
        },
    }
    result = _subagent_overrides_from_block("o", block)
    assert "a" in result


# --- 2. load_subagents: per-subagent overrides ----------------------------


def test_subagent_override_suffix_appended_to_named_only(tmp_path, monkeypatch):
    """Per-subagent suffix lands on the target subagent, not its sibling."""
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    d = _add_org(tmp_path, "o", agents=["alpha", "beta"])
    _add_agent(d, "alpha")
    _add_agent(d, "beta")
    _write_profile(
        d,
        subagents='alpha:\n  system_prompt_suffix: "ONLY ALPHA"\n',
    )
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")

    # Fake specialist tools (load_subagents only needs .name).
    class _T:
        def __init__(self, name):
            self.name = name
    from pux_harness.context.layer import build_context_layer
    mw, ctx_tools = build_context_layer()
    ov = load_subagent_overrides("o")
    subs = orgs_mod.load_subagents(
        "o", [], subagent_overrides=ov,
        subagent_middleware=mw, retrieval_tools=ctx_tools,
    )
    by_name = {s["name"]: s for s in subs}
    assert "ALPHA" in by_name["alpha"]["system_prompt"]
    assert "ALPHA" not in by_name["beta"]["system_prompt"]


def test_subagent_override_base_prompt_replaces_body(tmp_path, monkeypatch):
    """Per-subagent base_system_prompt replaces the .md body entirely."""
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    _write_profile(
        d,
        subagents='a:\n  base_system_prompt: "REPLACED"\n',
    )
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    from pux_harness.context.layer import build_context_layer
    mw, ctx_tools = build_context_layer()
    ov = load_subagent_overrides("o")
    subs = orgs_mod.load_subagents(
        "o", [], subagent_overrides=ov,
        subagent_middleware=mw, retrieval_tools=ctx_tools,
    )
    prompt = subs[0]["system_prompt"]
    assert _AGENT_BODY.strip() not in prompt
    assert "REPLACED" in prompt


def test_subagent_override_tools_prune_target_only(tmp_path, monkeypatch):
    """Per-subagent excluded_tools removes tools from the target only."""
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    d = _add_org(tmp_path, "o", agents=["alpha", "beta"])
    _add_agent(d, "alpha", tools=["browser_navigate"])
    _add_agent(d, "beta", tools=["browser_navigate"])
    _write_profile(
        d,
        subagents='alpha:\n  excluded_tools: [pux_sandbox_browser_navigate]\n',
    )
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")

    class _T:
        def __init__(self, name):
            self.name = name
    specialists = [_T("pux_sandbox_browser_navigate"), _T("ctx_recall"), _T("ctx_search")]
    from pux_harness.context.layer import build_context_layer
    mw, ctx_tools = build_context_layer()
    ov = load_subagent_overrides("o")
    subs = orgs_mod.load_subagents(
        "o", specialists, subagent_overrides=ov,
        subagent_middleware=mw, retrieval_tools=ctx_tools,
    )
    by_name = {s["name"]: s for s in subs}
    alpha_tools = {t.name for t in by_name["alpha"]["tools"]}
    beta_tools = {t.name for t in by_name["beta"]["tools"]}
    assert "pux_sandbox_browser_navigate" not in alpha_tools, alpha_tools
    assert "pux_sandbox_browser_navigate" in beta_tools, beta_tools


def test_subagent_override_precedence_prompt(tmp_path, monkeypatch):
    """Precedence: .md → per-agent base → org-wide suffix → per-agent suffix."""
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    d = _add_org(tmp_path, "o", agents=["a"])
    _add_agent(d, "a")
    _write_profile(
        d,
        system_prompt_suffix='"ORG WIDE"',
        subagents=(
            'a:\n'
            '  base_system_prompt: "BASE REPLACE"\n'
            '  system_prompt_suffix: "PER AGENT"\n'
        ),
    )
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")

    from pux_harness.agent.profile import load_profile
    from pux_harness.context.layer import build_context_layer
    mw, ctx_tools = build_context_layer()
    cfg = load_profile("o")
    ov = load_subagent_overrides("o")
    subs = orgs_mod.load_subagents(
        "o", [], profile=cfg, subagent_overrides=ov,
        subagent_middleware=mw, retrieval_tools=ctx_tools,
    )
    prompt = subs[0]["system_prompt"]
    assert _AGENT_BODY.strip() not in prompt, ".md body should be replaced"
    assert "BASE REPLACE" in prompt
    assert "ORG WIDE" in prompt
    assert prompt.index("ORG WIDE") > prompt.index("BASE REPLACE"), \
        "org-wide suffix should come after base replacement"
    assert prompt.index("PER AGENT") > prompt.index("ORG WIDE"), \
        "per-agent suffix should be last"


# --- 3. contract.py: check_org slug validation ----------------------------


def test_check_org_subagent_overrides_unknown_slug(tmp_path, monkeypatch):
    """A slug in subagents: that isn't in the org's roster fails check_org."""
    d = _add_org(tmp_path, "o", agents=["real-agent"])
    _add_agent(d, "real-agent")
    _write_profile(
        d,
        subagents='typo-slug:\n  system_prompt_suffix: "X"\n',
    )
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    vs = contract.check_org("o")
    assert any(v.rule == "subagent-overrides-unknown-slug" for v in vs), vs


def test_check_org_subagent_overrides_real_slug_passes(tmp_path, monkeypatch):
    """A slug in subagents: that IS in the roster is clean."""
    d = _add_org(tmp_path, "o", agents=["real-agent"])
    _add_agent(d, "real-agent")
    _write_profile(
        d,
        subagents='real-agent:\n  system_prompt_suffix: "X"\n',
    )
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    vs = contract.check_org("o")
    assert not any(v.rule == "subagent-overrides-unknown-slug" for v in vs), vs
