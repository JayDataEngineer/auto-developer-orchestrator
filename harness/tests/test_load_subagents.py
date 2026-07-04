"""Runtime resolution of SubAgent fields (``orgs.load_subagents``).

The contract test (``test_org_contract.py``) proves agents are *structurally*
valid offline. This module proves the loader *resolves* them into the shapes
deepagents consumes: ``model`` -> a ``ChatOpenAI`` instance via our router,
``skills`` -> absolute dirs that exist, and (critically) NO ``middleware`` key.

Token- and Docker-free: ``load_subagents`` only uses the tool list to resolve
*names* (it builds a ``{name: tool}`` map and never invokes them), so we pass a
minimal fake tool rather than constructing a ``DockerExecClient``. ``get_model``
reads ``OPENCODE_API_KEY`` at CALL time — set a throwaway key; no real chat
happens.
"""
from __future__ import annotations

from pathlib import Path

import pytest
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


def _agent_py(
    slug: str,
    root: Path,
    *,
    tools: list[str] | None = None,
    skills: list[str] | None = None,
    prose: str = "prose body",
    description: str | None = None,
) -> None:
    """Write the NEW-form ``.pi/agents/<slug>.py`` + prose ``<slug>.md``."""
    agents_dir = root / ".pi" / "agents"
    desc = description or f"{slug} subagent"
    lines = ["from pathlib import Path", "", "SUBAGENT = {",
             f'    "name": "{slug}",',
             f'    "description": "{desc}",']
    if tools:
        lines.append(f'    "tools": {tools!r},')
    if skills:
        lines.append(f'    "skills": {skills!r},')
    lines.append('    "system_prompt": Path(__file__).with_suffix(".md").read_text(),')
    lines.append("}")
    (agents_dir / f"{slug}.py").write_text("\n".join(lines) + "\n")
    (agents_dir / f"{slug}.md").write_text(prose)


def _org_yaml(name: str, agents_list: list[str], root: Path) -> None:
    """Write ``orgs/<name>/org.yaml`` + prose-only ``AGENTS.md``."""
    d = root / "orgs" / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "AGENTS.md").write_text(f"# {name}\n\nCTO prose, no frontmatter.\n")
    (d / "org.yaml").write_text(f"agents: [{', '.join(agents_list)}]\n")


def test_tools_resolved_to_specialist_surface(fake_tree):
    """Tools (bare slugs) resolve to pux_sandbox_* StructuredTools."""
    root = fake_tree
    _agent_py("t", root, tools=["python"])
    _org_yaml("o", ["t"], root)

    subs = orgs.load_subagents("o", _specialists())
    assert len(subs) == 1
    sub = subs[0]
    assert sub["name"] == "t"
    assert sub["description"] == "t subagent"
    assert sub["system_prompt"] == "prose body"
    assert [t.name for t in sub["tools"]] == ["pux_sandbox_python"]
    assert "middleware" not in sub


def test_model_resolved_via_get_model(fake_tree):
    """``model`` in SUBAGENT dict resolves through our router (bare shorthand
    routed via get_model, NOT a provider:string that init_chat_model would
    choke on)."""
    root = fake_tree
    agents_dir = root / ".pi" / "agents"
    (agents_dir / "m.py").write_text(
        "from pathlib import Path\n"
        "SUBAGENT = {\n"
        '    "name": "m",\n'
        '    "description": "m subagent",\n'
        '    "model": "glm-5.2",\n'
        '    "system_prompt": Path(__file__).with_suffix(".md").read_text(),\n'
        "}\n"
    )
    (agents_dir / "m.md").write_text("body")
    _org_yaml("o", ["m"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert isinstance(sub["model"], ChatOpenAI)
    assert sub["model"].model_name == "glm-5.2"


def test_model_omitted_means_inherit(fake_tree):
    """No ``model`` field -> the dict has no ``model`` key, so deepagents
    injects the parent model (``spec.get("model", model)``)."""
    root = fake_tree
    _agent_py("bare", root, tools=["python"])
    _org_yaml("o", ["bare"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert "model" not in sub


def test_skills_resolved_to_container_paths(fake_tree):
    """``skills`` source roots map to container-absolute paths."""
    root = fake_tree
    _agent_py("sk", root, skills=[".pi/skills"])
    _org_yaml("o", ["sk"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert sub["skills"] == ["/sandbox/workspace/.pi/skills"]


def test_unknown_skills_source_raises(fake_tree):
    """A skills source dir that doesn't exist under the project root fails loud
    (deepagents would otherwise silently load nothing from it)."""
    root = fake_tree
    _agent_py("bad", root, skills=["ghost"])
    _org_yaml("o", ["bad"], root)

    with pytest.raises(KeyError, match="ghost"):
        orgs.load_subagents("o", _specialists())


def test_skills_accepts_yaml_list(fake_tree):
    """``skills`` accepts a YAML list of source roots; each maps to a
    container-absolute path."""
    root = fake_tree
    _agent_py("multi", root, skills=[".pi/skills"])
    _org_yaml("o", ["multi"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    assert sub["skills"] == ["/sandbox/workspace/.pi/skills"]


def test_py_module_loads(fake_tree):
    """The .py path: a SUBAGENT dict loads with tools + skills resolved."""
    root = fake_tree
    _agent_py("pyagent", root, tools=["python"], skills=[".pi/skills"])
    _org_yaml("o", ["pyagent"], root)

    subs = orgs.load_subagents("o", _specialists())
    assert len(subs) == 1
    sub = subs[0]
    assert sub["name"] == "pyagent"
    assert sub["description"] == "pyagent subagent"
    assert sub["system_prompt"] == "prose body"
    assert [t.name for t in sub["tools"]] == ["pux_sandbox_python"]
    assert sub["skills"] == ["/sandbox/workspace/.pi/skills"]
    assert "model" not in sub
    assert "middleware" not in sub


def test_py_module_missing_sibling_md_raises(fake_tree):
    """A ``.py`` whose ``system_prompt`` reads a missing sibling ``.md`` fails
    loud at load (``exec_module`` raises) — no silent empty prompt."""
    root = fake_tree
    (root / ".pi" / "agents" / "orphan.py").write_text(
        "from pathlib import Path\n"
        "SUBAGENT = {'name': 'orphan', 'description': 'd', "
        "'system_prompt': Path(__file__).with_suffix('.md').read_text()}\n"
    )
    _org_yaml("o", ["orphan"], root)

    with pytest.raises(FileNotFoundError):
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
