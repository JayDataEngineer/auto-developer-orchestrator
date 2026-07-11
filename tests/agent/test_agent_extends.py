"""Per-agent override + ``extends:`` at the RUNTIME layer.

Scope is deliberately narrow and NON-duplicative. The other two test
files own their own layers:

* ``pux-harness/tests/test_kit_loaders.py`` — ``_merge_extends`` (every merge
  rule, dict-in/dict-out) + ``_load_agent_spec`` recursion (cycle / unresolvable
  / multi-level), the library-level UNIT tests.
* ``tests/test_org_contract.py`` — the contract rules (``no-legacy-subagents-
  block`` / ``agent-extends-resolvable`` / ``agent-extends-acyclic``) on both
  the real repo and a fake tree.

THIS file owns what only the orchestrator-integration layer can prove: that
``orgs.load_subagents`` honors the per-agent frontmatter fields at runtime, on
REAL ``BaseTool`` objects, through the full ``_build_sub`` → ``apply_profile_to_tools``
path — i.e. the fold actually reaches the tool list the agent graph compiles
with. tmpdir + monkeypatch only (no server, no Docker, no tokens).
"""
from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest
from langchain_core.tools import BaseTool

from pux_harness.agent import contract, orgs as orgs_mod


class _FakeTool(BaseTool):
    """Minimal BaseTool so deepagents' ``_apply_tool_description_overrides``
    (which calls ``model_copy(update=...)``) actually rewrites it. Plain objects
    are returned UNCHANGED by that helper — the override is silently dropped,
    which is exactly what this suite must NOT assert away."""

    name: str = ""
    description: str = ""

    def _run(self, *args: Any, **kwargs: Any) -> Any:  # pragma: no cover
        raise NotImplementedError

    async def _arun(self, *args: Any, **kwargs: Any) -> Any:  # pragma: no cover
        raise NotImplementedError


# --- shared helpers -------------------------------------------------------

_BODY = "prose body\n"


def _write_agent(
    root: Path, slug: str, *, org: str, body: str = _BODY,
    tools: list[str] | None = None, fm_extra: str = "",
) -> Path:
    """Write ``orgs/<org>/agents/<slug>.md``. ``fm_extra`` is raw YAML lines
    appended after the standard name/description/tools fields."""
    agents_dir = root / "orgs" / org / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    lines = ["---", f'name: "{slug}"', f'description: "{slug} specialist"']
    if tools is not None:
        lines.append(f"tools: [{', '.join(tools)}]")
    if fm_extra:
        lines.append(fm_extra.rstrip())
    lines.append("---")
    path = agents_dir / f"{slug}.md"
    path.write_text("\n".join(lines) + f"\n\n{body}")
    return path


def _add_org(root: Path, name: str, *, agents: list[str] | None = None) -> Path:
    d = root / "orgs" / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "AGENTS.md").write_text(f"# {name}\n")
    if agents is not None:
        (d / "org.yaml").write_text(f"agents: [{', '.join(agents)}]\n")
    return d


def _ctx() -> dict:
    """The context layer the stack factory threads into every subagent."""
    from pux_harness.context.layer import build_context_layer
    mw, tools = build_context_layer()
    return {"subagent_middleware": mw, "retrieval_tools": tools}


@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    monkeypatch.setattr(orgs_mod, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    return tmp_path


# --- runtime: per-agent frontmatter overrides -----------------------------


def test_per_agent_suffix_after_org_wide(fake_tree):
    """Per-agent ``system_prompt_suffix`` appends AFTER the org-wide suffix
    (most-specific = last word). Precedence: body → org-wide → per-agent."""
    root = fake_tree
    _write_agent(root, "a", org="o", fm_extra='system_prompt_suffix: "PER AGENT"')
    _add_org(root, "o", agents=["a"])
    (root / "orgs" / "o" / "profile.yaml").write_text(
        'system_prompt_suffix: "ORG WIDE"\n')
    from pux_harness.agent.profile import load_profile
    cfg = load_profile("o")
    sub = orgs_mod.load_subagents("o", [], profile=cfg, **_ctx())[0]
    prompt = sub["system_prompt"]
    assert prompt.index("ORG WIDE") < prompt.index("PER AGENT")
    assert _BODY.strip() in prompt  # .md body still present (no base_system_prompt)


def test_per_agent_base_system_prompt_replaces_body(fake_tree):
    """Per-agent ``base_system_prompt`` was REMOVED — it was a global-REPLACE
    that wiped the agent's own body. A frontmatter entry shipping it must FAIL
    loud (a stray one is a gap, not a silent drop). Use ``system_prompt_suffix``
    (append) instead."""
    root = fake_tree
    _write_agent(root, "a", org="o", fm_extra='base_system_prompt: "REPLACED"')
    _add_org(root, "o", agents=["a"])
    with pytest.raises(ValueError, match="base_system_prompt.*removed"):
        orgs_mod.load_subagents("o", [], **_ctx())


def test_per_agent_excluded_tools(fake_tree):
    """Per-agent ``excluded_tools`` drops a tool from THAT agent only (an
    unlisted sibling keeps it) — the prune is per-agent, not org-wide."""
    root = fake_tree
    _write_agent(root, "alpha", org="o", tools=["browser_navigate"],
                 fm_extra="excluded_tools: [pux_sandbox_browser_navigate]")
    _write_agent(root, "beta", org="o", tools=["browser_navigate"])
    _add_org(root, "o", agents=["alpha", "beta"])
    specialists = [_FakeTool(name="pux_sandbox_browser_navigate",
                             description="navigate")]
    subs = {s["name"]: s for s in orgs_mod.load_subagents("o", specialists, **_ctx())}
    alpha = {t.name for t in subs["alpha"]["tools"]}
    beta = {t.name for t in subs["beta"]["tools"]}
    assert "pux_sandbox_browser_navigate" not in alpha, alpha
    assert "pux_sandbox_browser_navigate" in beta, beta


def test_per_agent_tool_description_overrides(fake_tree):
    """Per-agent ``tool_description_overrides`` rewrites a real BaseTool's
    description (the path that silently no-ops on plain objects — so a FakeTool
    that is a real BaseTool is required to prove the rewrite lands)."""
    root = fake_tree
    _write_agent(root, "a", org="o", tools=["python"],
                 fm_extra="tool_description_overrides:\n  pux_sandbox_python: \"rewritten desc\"")
    _add_org(root, "o", agents=["a"])
    specialists = [_FakeTool(name="pux_sandbox_python", description="original")]
    sub = orgs_mod.load_subagents("o", specialists, **_ctx())[0]
    py = next(t for t in sub["tools"] if t.name == "pux_sandbox_python")
    assert py.description == "rewritten desc"


def test_extends_adds_tool_at_runtime(fake_tree):
    """An ``extends:`` child of a shared agent gains a tool ADDITIVELY at
    runtime — the user's 'slightly modify the browser agent' case, proven end
    to end through load_subagents (not just the kit-level merge)."""
    root = fake_tree
    _write_agent(root, "browser", org="_shared",
                 tools=["browser_navigate", "browser_click"])
    _write_agent(root, "my-browser", org="o",
                 fm_extra="extends: browser\ntools_add: [browser_scroll]")
    _add_org(root, "o", agents=["my-browser"])
    specialists = [_FakeTool(name="pux_sandbox_browser_navigate"),
                   _FakeTool(name="pux_sandbox_browser_click"),
                   _FakeTool(name="pux_sandbox_browser_scroll")]
    sub = orgs_mod.load_subagents("o", specialists, **_ctx())[0]
    names = {t.name for t in sub["tools"]}
    assert {"pux_sandbox_browser_navigate", "pux_sandbox_browser_click",
            "pux_sandbox_browser_scroll"} <= names, names
