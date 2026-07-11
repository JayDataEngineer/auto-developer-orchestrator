"""Runtime resolution of SubAgent fields (``orgs.load_subagents``).

The contract test (``test_org_contract.py``) proves agents are *structurally*
valid offline. This module proves the loader *resolves* them into the shapes
deepagents consumes: ``model`` -> a ``ChatOpenAI`` instance via our router,
``skills`` -> absolute dirs that exist, and a ``middleware`` key
carrying the unified context layer (``ContextMiddleware`` — capture + offload)
PLUS the ``ctx_recall``/``ctx_search`` retrieval tools appended to the
whitelist. deepagents' ``SubAgentMiddleware`` forwards that ``middleware`` key
into the compiled subagent (verified against 0.6.12), so the layer intercepts
each subagent's own tool calls — the old "NO middleware key / main-agent-only"
old claim is retracted.

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
from pux_harness.context.layer import build_context_layer
from pux_harness.context.middleware import ContextMiddleware


class _FakeTool:
    """A tool stand-in with just a ``.name`` — all ``load_subagents`` needs to
    build the resolver map (it never invokes the tools)."""

    def __init__(self, name: str) -> None:
        self.name = name


def _specialists() -> list[_FakeTool]:
    return [_FakeTool("pux_sandbox_python")]


def _ctx() -> dict:
    """The unified context layer the stack factory builds and threads into every
    subagent. ``load_subagents`` takes it explicitly now (one way: the loader
    no longer builds the layer itself), so direct callers pass this."""
    mw, tools = build_context_layer()
    return {"subagent_middleware": mw, "retrieval_tools": tools}


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

    subs = orgs.load_subagents("o", _specialists(), **_ctx())
    assert len(subs) == 1
    sub = subs[0]
    assert sub["name"] == "t"
    assert sub["description"] == "t subagent"
    assert sub["system_prompt"] == "prose body"
    # The unified context layer threads ``ctx_recall``/``ctx_search``
    # into every subagent (specialist whitelist = python; the retrieval pair is
    # appended on top — graph.py retracts the old main-agent-only claim).
    assert {t.name for t in sub["tools"]} == {
        "pux_sandbox_python",
        "ctx_recall", "ctx_search", "ctx_index",
        "ctx_stats", "ctx_doctor", "ctx_purge",
    }
    # Each subagent carries the unified ContextMiddleware (capture +
    # offload in one pass) so the layer intercepts its own tool calls — the old
    # main-agent-only claim is retracted (file docstring above).
    assert isinstance(sub["middleware"], list) and sub["middleware"]
    assert isinstance(sub["middleware"][0], ContextMiddleware)


def test_model_resolved_via_get_model(fake_tree):
    """``model`` in frontmatter resolves through our router (bare shorthand
    routed via get_model, NOT a provider:string that init_chat_model would
    choke on)."""
    root = fake_tree
    _agent_md("m", root, model="glm-5.2", body="body")
    _org_yaml("o", ["m"], root)

    sub = orgs.load_subagents("o", _specialists(), **_ctx())[0]
    # glm-5.2 is bound to the zai-coding provider (kind: openai) in
    # models.yaml -> our router returns a ChatOpenAI subclass for it. Pro
    # reasoning models wrap as ReasoningChatOpenAI (a ChatOpenAI subclass).
    # The point proven here is the bare shorthand still resolves through
    # get_model to a real chat model (not a provider:string), and the right id.
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

    sub = orgs.load_subagents("o", _specialists(), **_ctx())[0]
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

    sub = orgs.load_subagents("o", _specialists(), **_ctx())[0]
    assert sub["model"].model == "glm-5.2"


def test_skills_resolved_to_container_paths(fake_tree):
    """``skills`` source roots map to container-absolute paths."""
    root = fake_tree
    _agent_md("sk", root, skills=["orgs/_shared/skills"])
    _org_yaml("o", ["sk"], root)

    sub = orgs.load_subagents("o", _specialists(), **_ctx())[0]
    assert sub["skills"] == ["/sandbox/workspace/orgs/_shared/skills"]


def test_unknown_skills_source_raises(fake_tree):
    """A skills source dir that doesn't exist under the project root fails loud
    (deepagents would otherwise silently load nothing from it)."""
    root = fake_tree
    _agent_md("bad", root, skills=["ghost"])
    _org_yaml("o", ["bad"], root)

    with pytest.raises(KeyError, match="ghost"):
        orgs.load_subagents("o", _specialists(), **_ctx())


def test_skills_accepts_yaml_list(fake_tree):
    """``skills`` accepts a YAML list of source roots; each maps to a
    container-absolute path."""
    root = fake_tree
    (root / "orgs" / "o" / "skills").mkdir(parents=True)
    _agent_md("multi", root, skills=["orgs/_shared/skills", "orgs/o/skills"])
    _org_yaml("o", ["multi"], root)

    sub = orgs.load_subagents("o", _specialists(), **_ctx())[0]
    assert sub["skills"] == [
        "/sandbox/workspace/orgs/_shared/skills",
        "/sandbox/workspace/orgs/o/skills",
    ]


def test_md_agent_loads(fake_tree):
    """A frontmatter+body .md loads with tools + skills resolved."""
    root = fake_tree
    _agent_md("mdagent", root, tools=["python"], skills=["orgs/_shared/skills"])
    _org_yaml("o", ["mdagent"], root)

    subs = orgs.load_subagents("o", _specialists(), **_ctx())
    assert len(subs) == 1
    sub = subs[0]
    assert sub["name"] == "mdagent"
    assert sub["description"] == "mdagent subagent"
    assert sub["system_prompt"] == "prose body"
    # The unified context layer threads ``ctx_recall``/``ctx_search``
    # into every subagent (specialist whitelist = python; the retrieval pair is
    # appended on top — graph.py retracts the old main-agent-only claim).
    assert {t.name for t in sub["tools"]} == {
        "pux_sandbox_python",
        "ctx_recall", "ctx_search", "ctx_index",
        "ctx_stats", "ctx_doctor", "ctx_purge",
    }
    assert sub["skills"] == ["/sandbox/workspace/orgs/_shared/skills"]
    assert sub["model"].model_name == "mimo-v2.5"
    # Each subagent carries the unified ContextMiddleware (capture +
    # offload in one pass) so the layer intercepts its own tool calls — the old
    # main-agent-only claim is retracted (file docstring above).
    assert isinstance(sub["middleware"], list) and sub["middleware"]
    assert isinstance(sub["middleware"][0], ContextMiddleware)


def test_shared_agent_resolves(fake_tree):
    """An agent absent from the org's own ``agents/`` dir resolves from
    ``orgs/_shared/agents/``."""
    root = fake_tree
    _agent_md("sharedone", root, org="_shared", tools=["python"])
    _org_yaml("o", ["sharedone"], root)

    sub = orgs.load_subagents("o", _specialists(), **_ctx())[0]
    assert sub["name"] == "sharedone"
    assert sub["system_prompt"] == "prose body"


def test_org_local_overrides_shared(fake_tree):
    """A same-named agent in the org's own ``agents/`` dir wins over the
    ``_shared`` one (specialization)."""
    root = fake_tree
    _agent_md("dup", root, org="_shared", body="shared body", description="shared")
    _agent_md("dup", root, org="o", body="org body", description="org")
    _org_yaml("o", ["dup"], root)

    sub = orgs.load_subagents("o", _specialists(), **_ctx())[0]
    assert sub["system_prompt"] == "org body"
    assert sub["description"] == "org"


def test_missing_agent_md_raises(fake_tree):
    """A roster slug with no ``<slug>.md`` in org-local or ``_shared`` fails
    loud — no silent empty agent."""
    root = fake_tree
    _org_yaml("o", ["ghost"], root)

    with pytest.raises(FileNotFoundError, match="ghost"):
        orgs.load_subagents("o", _specialists(), **_ctx())


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


# --- Shared browser agent + its full whitelist -----------------------------

def test_real_browser_whitelist_resolves(monkeypatch):
    """The shipped browser agent (orgs/_shared/agents/browser.md) is rostered by
    `general`; its full ~24-tool whitelist resolves against the REAL specialist
    registry (every slug is a registered pux_sandbox_* tool). load_subagents
    would raise KeyError on any unresolved slug, so reaching the assertions
    proves the whole whitelist binds."""
    from pux_harness.sandbox.tools import build_native_specialists

    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")  # worker-role model build
    specialists = build_native_specialists("DUMMY", None, None)
    subs = orgs.load_subagents("general", specialists, **_ctx())
    browser = next(s for s in subs if s["name"] == "browser")
    names = {t.name for t in browser["tools"]}
    # Representative coverage across navigate / search / screenshot / tabs /
    # sessions / vision — every browser family is present + resolved.
    for slug in (
        "browser_navigate", "browser_search", "browser_click", "browser_type",
        "browser_scroll", "browser_screenshot", "browser_save_screenshot",
        "browser_evaluate", "browser_extract_images", "browser_download",
        "browser_new_tab", "browser_switch_tab", "browser_close_tab",
        "browser_save_session", "browser_restore_session",
        "describe_image",
    ):
        assert "pux_sandbox_" + slug in names, f"{slug} not resolved"


# --- extends: inheritance through the REAL load_subagents path --------------

def test_extends_inherits_base_tools_and_body(fake_tree):
    """An org-local child with ``extends: <shared base>`` + ``tools_add``
    inherits the base's body + tool whitelist AND adds the new tool — driven
    through the REAL ``load_subagents`` entry point, not just the kit-layer unit
    test (prepare-wiring-e2e-gap: a wiring seam proven only in isolation is
    unproven). The base lives in ``_shared``; the org-local child specializes it
    without forking the base prompt. This is the universal per-agent override
    surface replacing the deleted top-level ``subagents:`` block."""
    root = fake_tree
    # base: shared agent, owns the core whitelist + body
    _agent_md("base", root, org="_shared", tools=["python"],
              body="BASE PROMPT.", description="the base")
    # child: org-local, extends base + adds a tool + appends a body
    child_dir = root / "orgs" / "o" / "agents"
    child_dir.mkdir(parents=True, exist_ok=True)
    (child_dir / "special.md").write_text(
        "---\n"
        'name: "special"\n'
        'description: "the specialist"\n'
        "extends: base\n"
        'tools_add: ["browser_navigate"]\n'
        "---\n\n"
        "CHILD PROMPT.\n"
    )
    _org_yaml("o", ["special"], root)

    # Two resolvable specialists so the inherited + added tools are BOTH
    # observable end-to-end (python inherited from base, browser_navigate added).
    specialists = [
        _FakeTool("pux_sandbox_python"),
        _FakeTool("pux_sandbox_browser_navigate"),
    ]
    sub = orgs.load_subagents("o", specialists, **_ctx())[0]
    assert sub["name"] == "special"
    # Base body + child body both present (concatenation, NOT full-replace).
    assert "BASE PROMPT." in sub["system_prompt"]
    assert "CHILD PROMPT." in sub["system_prompt"]
    # Inherited base tool + added tool both resolve (union over the base
    # whitelist), plus the ctx retrieval pair appended by the context layer.
    assert {t.name for t in sub["tools"]} == {
        "pux_sandbox_python", "pux_sandbox_browser_navigate",
        "ctx_recall", "ctx_search", "ctx_index",
        "ctx_stats", "ctx_doctor", "ctx_purge",
    }


def test_extends_cycle_raises_through_load_subagents(fake_tree):
    """A cycle in ``extends:`` surfaces loudly through ``load_subagents`` (the
    runtime path), not just the kit loader — the contract tripwire is a belt,
    this is the suspenders."""
    root = fake_tree
    a_dir = root / "orgs" / "o" / "agents"
    a_dir.mkdir(parents=True, exist_ok=True)
    (a_dir / "x.md").write_text(
        "---\nname: x\nextends: y\n---\n\nX BODY\n")
    (a_dir / "y.md").write_text(
        "---\nname: y\nextends: x\n---\n\nY BODY\n")
    _org_yaml("o", ["x"], root)

    with pytest.raises(ValueError, match="extends cycle"):
        orgs.load_subagents("o", _specialists(), **_ctx())
