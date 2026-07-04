"""Runtime resolution of the Phase-10 rich SubAgent fields (``orgs.load_subagents``).

The contract test (``test_org_contract.py``) proves these fields are *structurally*
valid offline. This module proves the loader *resolves* them into the shapes
deepagents consumes: ``model`` -> a ``ChatOpenAI`` instance via our router (not a
provider:string), ``skills`` -> absolute dirs that exist, ``permissions`` ->
``FilesystemPermission`` objects, ``response_format``/``interrupt_on`` -> raw
dicts, and (critically) NO ``middleware`` key (Phase 7: ``SubAgentMiddleware``
doesn't round-trip a raw spec's ``middleware`` key).

Token- and Docker-free: ``load_subagents`` only uses the tool list to resolve
*names* (it builds a ``{name: tool}`` map and never invokes them), so we pass a
minimal fake tool rather than constructing a ``DockerExecClient`` (which would
contact the Docker daemon to discover a container). ``get_model`` reads
``OPENCODE_API_KEY`` at CALL time — set a throwaway key; no real chat happens.
"""
from __future__ import annotations

from pathlib import Path

import pytest
from deepagents import FilesystemPermission
from langchain_openai import ChatOpenAI

from pux_harness import orgs


class _FakeTool:
    """A tool stand-in with just a ``.name`` — all ``load_subagents`` needs to
    build the resolver map (it never invokes the tools)."""

    def __init__(self, name: str) -> None:
        self.name = name


def _specialists() -> list[_FakeTool]:
    return [_FakeTool("pux_sandbox_python")]


@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """A scratch orgs/.pi tree patched onto the ``orgs`` module (the loader's
    own path helpers, not contract's)."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / ".pi" / "agents").mkdir(parents=True)
    (tmp_path / ".pi" / "skills").mkdir(parents=True)
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs, "_agents_dir", lambda: tmp_path / ".pi" / "agents")
    monkeypatch.setenv("OPENCODE_API_KEY", "test-key")
    return tmp_path


def _org(name: str, agents: str, root: Path) -> None:
    d = root / "orgs" / name
    d.mkdir(parents=True)
    (d / "AGENTS.md").write_text(f"---\nagents: {agents}\n---\n\n# {name}\n")


def _agent(slug: str, extra_fm: str, root: Path) -> None:
    (root / ".pi" / "agents" / f"{slug}.md").write_text(
        f"---\nname: {slug}\ndescription: d\n{extra_fm}---\n\nbody\n")


def test_resolves_all_rich_fields(fake_tree):
    """All five Phase-10 fields resolve to their deepagents-consumable shapes."""
    root = fake_tree
    _agent(
        "rich",
        "tools: mcp:pux-sandbox/python\n"
        "model: glm-5.2\n"
        "skills: .pi/skills\n"
        "response_format:\n  type: object\n"
        "permissions:\n  - operations: [read, write]\n"
        "    paths: [/sandbox/workspace]\n"
        "interrupt_on:\n  task: true\n",
        root,
    )
    _org("o", "rich", root)

    subs = orgs.load_subagents("o", _specialists())
    assert len(subs) == 1
    sub = subs[0]

    # base shape
    assert sub["name"] == "rich"
    assert sub["description"] == "d"
    assert sub["system_prompt"] == "body"

    # tools resolved to the bound specialist surface (names, not invoked)
    assert [t.name for t in sub["tools"]] == ["pux_sandbox_python"]

    # model -> OUR ChatOpenAI instance (bare shorthand routed via get_model,
    # NOT a provider:string that init_chat_model would choke on)
    assert isinstance(sub["model"], ChatOpenAI)
    assert sub["model"].model_name == "glm-5.2"

    # skills -> container-absolute ROOT paths (deepagents' SkillsMiddleware
    # resolves these against the backend and scans each for <skill>/SKILL.md;
    # a source is a root, not an individual skill dir)
    assert sub["skills"] == ["/sandbox/workspace/.pi/skills"]

    # response_format -> raw JSON-schema dict passthrough
    assert sub["response_format"] == {"type": "object"}

    # permissions -> FilesystemPermission objects (path-validated at construction)
    assert len(sub["permissions"]) == 1
    perm = sub["permissions"][0]
    assert isinstance(perm, FilesystemPermission)
    assert perm.operations == ["read", "write"]
    assert perm.paths == ["/sandbox/workspace"]

    # interrupt_on -> dict[str, bool] passthrough
    assert sub["interrupt_on"] == {"task": True}

    # middleware is deliberately NOT passed (Phase 7)
    assert "middleware" not in sub


def test_model_omitted_means_inherit(fake_tree):
    """No ``model`` field -> the dict has no ``model`` key, so deepagents
    injects the parent model (``spec.get("model", model)``). This is the
    correct inheritance behavior, not a harness default."""
    root = fake_tree
    _agent("bare", "tools: mcp:pux-sandbox/python\n", root)
    _org("o", "bare", root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert "model" not in sub


def test_interrupt_on_non_bool_raises(fake_tree):
    """A non-bool value fails loud at load time, not as a silent no-op later."""
    root = fake_tree
    _agent("bad", 'interrupt_on:\n  task: "yes"\n', root)
    _org("o", "bad", root)

    with pytest.raises(ValueError, match="interrupt_on"):
        orgs.load_subagents("o", _specialists())


def test_unknown_skills_source_raises(fake_tree):
    """A skills source dir that doesn't exist under the project root fails loud
    (deepagents would otherwise silently load nothing from it)."""
    root = fake_tree
    _agent("bad", "skills: ghost\n", root)
    _org("o", "bad", root)

    with pytest.raises(KeyError, match="ghost"):
        orgs.load_subagents("o", _specialists())


def test_skills_accepts_yaml_list(fake_tree):
    """``skills`` accepts a YAML list of source roots (``[.pi/skills]``) or a
    comma scalar; each maps to a container-absolute path."""
    root = fake_tree
    _agent("multi", "skills: [.pi/skills]\n", root)
    _org("o", "multi", root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert sub["skills"] == ["/sandbox/workspace/.pi/skills"]
