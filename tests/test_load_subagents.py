"""Runtime resolution of SubAgent fields (``orgs.load_subagents``).

The contract test (``test_org_contract.py``) proves agents are *structurally*
valid offline. This module proves the loader *resolves* them into the shapes
deepagents consumes: ``model`` -> a ``ChatOpenAI`` instance via our router,
``skills`` -> absolute dirs that exist, and (Phase 19) a ``middleware`` key
carrying the unified context layer (``ContextMiddleware`` — capture + offload)
PLUS the ``ctx_recall``/``ctx_search`` retrieval tools appended to the
whitelist. deepagents' ``SubAgentMiddleware`` forwards that ``middleware`` key
into the compiled subagent (verified against 0.6.12), so the layer intercepts
each subagent's own tool calls — the old "NO middleware key / main-agent-only"
Phase-7 claim is retracted.

Agents are frontmatter+body ``.md`` files resolved org-local first, then
``orgs/_shared/agents/`` — this module covers both paths.

Token- and Docker-free: ``load_subagents`` only uses the tool list to resolve
*names* (it builds a ``{name: tool}`` map and never invokes them), so we pass a
minimal fake tool rather than constructing a ``DockerExecClient``. ``get_model``
reads ``OPENCODE_API_KEY`` at CALL time — set a throwaway key; no real chat
happens.
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest
from langchain_openai import ChatOpenAI

from pux_harness.agent import orgs
from pux_harness.context.middleware import ContextMiddleware


class _FakeTool:
    """A tool stand-in with just a ``.name`` — all ``load_subagents`` needs to
    build the resolver map (it never invokes the tools)."""

    def __init__(self, name: str) -> None:
        self.name = name


def _specialists() -> list[_FakeTool]:
    return [_FakeTool("pux_sandbox_python")]


@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """A scratch orgs/ tree patched onto the ``orgs`` module (the loader's own
    path helpers, not contract's). ``_shared`` carries the cross-shared agent +
    skills roots; each test's org is created by ``_org_yaml``."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "skills").mkdir(parents=True)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    return tmp_path


def _agent_md(
    slug: str,
    root: Path,
    *,
    org: str = "o",
    tools: list[str] | None = None,
    skills: list[str] | None = None,
    model: str | None = None,
    body: str = "prose body",
    description: str | None = None,
) -> None:
    """Write a frontmatter+body ``orgs/<org>/agents/<slug>.md``.

    ``org="_shared"`` writes a cross-shared agent (resolved when no org-local
    file exists). List fields are emitted as YAML flow sequences (JSON is a
    valid YAML subset)."""
    agents_dir = root / "orgs" / org / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    desc = description or f"{slug} subagent"
    fm = ["---", f'name: "{slug}"', f'description: "{desc}"']
    if tools:
        fm.append(f"tools: {json.dumps(tools)}")
    if skills:
        fm.append(f"skills: {json.dumps(skills)}")
    if model:
        fm.append(f'model: "{model}"')
    fm.append("---")
    content = "\n".join(fm) + "\n\n" + body + "\n"
    (agents_dir / f"{slug}.md").write_text(content)


def _org_yaml(name: str, agents_list: list[str], root: Path) -> None:
    """Write ``orgs/<name>/org.yaml`` + prose-only ``AGENTS.md``."""
    d = root / "orgs" / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "AGENTS.md").write_text(f"# {name}\n\nCTO prose, no frontmatter.\n")
    (d / "org.yaml").write_text(f"agents: [{', '.join(agents_list)}]\n")


def test_tools_resolved_to_specialist_surface(fake_tree):
    """Tools (bare slugs) resolve to pux_sandbox_* StructuredTools."""
    root = fake_tree
    _agent_md("t", root, tools=["python"])
    _org_yaml("o", ["t"], root)

    subs = orgs.load_subagents("o", _specialists())
    assert len(subs) == 1
    sub = subs[0]
    assert sub["name"] == "t"
    assert sub["description"] == "t subagent"
    assert sub["system_prompt"] == "prose body"
    # Phase 19: the unified context layer threads ``ctx_recall``/``ctx_search``
    # into every subagent (specialist whitelist = python; the retrieval pair is
    # appended on top — graph.py retracts the old main-agent-only claim).
    assert {t.name for t in sub["tools"]} == {
        "pux_sandbox_python", "ctx_recall", "ctx_search",
    }
    # Phase 19: each subagent carries the unified ContextMiddleware (capture +
    # offload in one pass) so the layer intercepts its own tool calls — the old
    # main-agent-only Phase-7 claim is retracted (file docstring above).
    assert isinstance(sub["middleware"], list) and sub["middleware"]
    assert isinstance(sub["middleware"][0], ContextMiddleware)


def test_model_resolved_via_get_model(fake_tree):
    """``model`` in frontmatter resolves through our router (bare shorthand
    routed via get_model, NOT a provider:string that init_chat_model would
    choke on)."""
    root = fake_tree
    _agent_md("m", root, model="glm-5.2", body="body")
    _org_yaml("o", ["m"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert isinstance(sub["model"], ChatOpenAI)
    assert sub["model"].model_name == "glm-5.2"


def test_model_omitted_uses_worker_role(fake_tree):
    """No ``model`` field -> the subagent runs on the WORKER role (Phase
    17.B.0), resolved through models.yaml + org profile + env. The shipped
    worker default is mimo-v2.5; an org can override it via the top-level
    ``models:`` map without touching the agent file."""
    root = fake_tree
    _agent_md("bare", root, tools=["python"])
    _org_yaml("o", ["bare"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert isinstance(sub["model"], ChatOpenAI)
    assert sub["model"].model_name == "mimo-v2.5"


def test_model_omitted_worker_role_org_override(fake_tree):
    """The worker role picks up an org-level ``models: worker_model:`` override
    from profile.yaml (the spec's per-org override seam)."""
    root = fake_tree
    _agent_md("bare", root, tools=["python"])
    _org_yaml("o", ["bare"], root)
    (root / "orgs" / "o" / "profile.yaml").write_text(
        "models:\n  worker_model: glm-5.2\n"
    )

    sub = orgs.load_subagents("o", _specialists())[0]
    assert sub["model"].model_name == "glm-5.2"


def test_skills_resolved_to_container_paths(fake_tree):
    """``skills`` source roots map to container-absolute paths."""
    root = fake_tree
    _agent_md("sk", root, skills=["orgs/_shared/skills"])
    _org_yaml("o", ["sk"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert sub["skills"] == ["/sandbox/workspace/orgs/_shared/skills"]


def test_unknown_skills_source_raises(fake_tree):
    """A skills source dir that doesn't exist under the project root fails loud
    (deepagents would otherwise silently load nothing from it)."""
    root = fake_tree
    _agent_md("bad", root, skills=["ghost"])
    _org_yaml("o", ["bad"], root)

    with pytest.raises(KeyError, match="ghost"):
        orgs.load_subagents("o", _specialists())


def test_skills_accepts_yaml_list(fake_tree):
    """``skills`` accepts a YAML list of source roots; each maps to a
    container-absolute path."""
    root = fake_tree
    (root / "orgs" / "o" / "skills").mkdir(parents=True)
    _agent_md("multi", root, skills=["orgs/_shared/skills", "orgs/o/skills"])
    _org_yaml("o", ["multi"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert sub["skills"] == [
        "/sandbox/workspace/orgs/_shared/skills",
        "/sandbox/workspace/orgs/o/skills",
    ]


def test_md_agent_loads(fake_tree):
    """A frontmatter+body .md loads with tools + skills resolved."""
    root = fake_tree
    _agent_md("mdagent", root, tools=["python"], skills=["orgs/_shared/skills"])
    _org_yaml("o", ["mdagent"], root)

    subs = orgs.load_subagents("o", _specialists())
    assert len(subs) == 1
    sub = subs[0]
    assert sub["name"] == "mdagent"
    assert sub["description"] == "mdagent subagent"
    assert sub["system_prompt"] == "prose body"
    # Phase 19: the unified context layer threads ``ctx_recall``/``ctx_search``
    # into every subagent (specialist whitelist = python; the retrieval pair is
    # appended on top — graph.py retracts the old main-agent-only claim).
    assert {t.name for t in sub["tools"]} == {
        "pux_sandbox_python", "ctx_recall", "ctx_search",
    }
    assert sub["skills"] == ["/sandbox/workspace/orgs/_shared/skills"]
    assert sub["model"].model_name == "mimo-v2.5"
    # Phase 19: each subagent carries the unified ContextMiddleware (capture +
    # offload in one pass) so the layer intercepts its own tool calls — the old
    # main-agent-only Phase-7 claim is retracted (file docstring above).
    assert isinstance(sub["middleware"], list) and sub["middleware"]
    assert isinstance(sub["middleware"][0], ContextMiddleware)


def test_shared_agent_resolves(fake_tree):
    """An agent absent from the org's own ``agents/`` dir resolves from
    ``orgs/_shared/agents/``."""
    root = fake_tree
    _agent_md("sharedone", root, org="_shared", tools=["python"])
    _org_yaml("o", ["sharedone"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert sub["name"] == "sharedone"
    assert sub["system_prompt"] == "prose body"


def test_org_local_overrides_shared(fake_tree):
    """A same-named agent in the org's own ``agents/`` dir wins over the
    ``_shared`` one (specialization)."""
    root = fake_tree
    _agent_md("dup", root, org="_shared", body="shared body", description="shared")
    _agent_md("dup", root, org="o", body="org body", description="org")
    _org_yaml("o", ["dup"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert sub["system_prompt"] == "org body"
    assert sub["description"] == "org"


def test_missing_agent_md_raises(fake_tree):
    """A roster slug with no ``<slug>.md`` in org-local or ``_shared`` fails
    loud — no silent empty agent."""
    root = fake_tree
    _org_yaml("o", ["ghost"], root)

    with pytest.raises(FileNotFoundError, match="ghost"):
        orgs.load_subagents("o", _specialists())


def test_org_agent_slugs_reads_org_yaml(fake_tree):
    """``org.yaml`` is the roster source."""
    root = fake_tree
    _org_yaml("o", ["a", "b"], root)
    assert orgs.org_agent_slugs("o") == ["a", "b"]


def test_org_yaml_top_level_must_be_mapping(fake_tree):
    """A malformed ``org.yaml`` (not a mapping) fails loud rather than silently
    yielding an empty roster."""
    root = fake_tree
    d = root / "orgs" / "o"
    d.mkdir(parents=True)
    (d / "AGENTS.md").write_text("# o\n")
    (d / "org.yaml").write_text("- just\n- a\n- list\n")
    with pytest.raises(ValueError, match="mapping"):
        orgs.org_agent_slugs("o")


# --- Phase 16: shared browser agent + its full whitelist ------------------

def test_real_browser_whitelist_resolves(monkeypatch):
    """The shipped browser agent (orgs/_shared/agents/browser.md) is rostered by
    `general`; its full ~24-tool whitelist resolves against the REAL specialist
    registry (every slug is a registered pux_sandbox_* tool). load_subagents
    would raise KeyError on any unresolved slug, so reaching the assertions
    proves the whole whitelist binds."""
    from pux_harness.sandbox.tools import build_native_specialists

    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")  # worker-role model build
    specialists = build_native_specialists("DUMMY", None, None)
    subs = orgs.load_subagents("general", specialists)
    browser = next(s for s in subs if s["name"] == "browser")
    names = {t.name for t in browser["tools"]}
    # Representative coverage across navigate / search / screenshot / tabs /
    # sessions / vision — every Phase-16 family is present + resolved.
    for slug in (
        "browser_navigate", "browser_search", "browser_click", "browser_type",
        "browser_scroll", "browser_screenshot", "browser_save_screenshot",
        "browser_evaluate", "browser_extract_images", "browser_download",
        "browser_new_tab", "browser_switch_tab", "browser_close_tab",
        "browser_save_session", "browser_restore_session",
        "describe_image",
    ):
        assert "pux_sandbox_" + slug in names, f"{slug} not resolved"
