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


def _agent_py(
    slug: str,
    root: Path,
    *,
    tools: list[str] | None = None,
    skills: list[str] | None = None,
    prose: str = "prose body",
) -> None:
    """Write the NEW-form ``.pi/agents/<slug>.py`` (a ``SUBAGENT`` dict, bare-slug
    ``tools`` + path-list ``skills``) plus its sibling ``<slug>.md`` prose file.
    Mirrors what the Phase-2 one-shot converter emits. The module imports only
    stdlib (``pathlib``) — the contract's CI-safety guard depends on that."""
    agents_dir = root / ".pi" / "agents"
    lines = ["from pathlib import Path", "", "SUBAGENT = {",
             f'    "name": "{slug}",',
             f'    "description": "{slug} subagent",']
    if tools:
        lines.append(f"    \"tools\": {tools!r},")
    if skills:
        lines.append(f"    \"skills\": {skills!r},")
    lines.append("    \"system_prompt\": Path(__file__).with_suffix(\".md\").read_text(),")
    lines.append("}")
    (agents_dir / f"{slug}.py").write_text("\n".join(lines) + "\n")
    (agents_dir / f"{slug}.md").write_text(prose)


def _org_yaml(name: str, agents_list: list[str], root: Path) -> None:
    """Write ``orgs/<name>/org.yaml`` (the NEW roster source) + a frontmatter-free
    AGENTS.md (CTO prose only — the migrated shape)."""
    d = root / "orgs" / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "AGENTS.md").write_text(f"# {name}\n\nCTO prose, no frontmatter.\n")
    (d / "org.yaml").write_text(f"agents: [{', '.join(agents_list)}]\n")


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


# --- Phase 1: the new .py + org.yaml path (dual-read) ----------------------
# These prove the migrated form loads correctly while the legacy .md branch
# (covered above) still works. The legacy branch + these tests' legacy helpers
# are REMOVED in Phase 3 once all 22 agents + 10 orgs are migrated.

def test_py_module_loads(fake_tree):
    """The new path: a ``.pi/agents/<slug>.py`` SUBAGENT dict loads, with
    ``tools`` (bare slugs) + ``skills`` (path list) resolved centrally."""
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
    # omitted model -> no key -> deepagents injects the parent model
    assert "model" not in sub
    # middleware is never set (Phase 7)
    assert "middleware" not in sub


def test_py_path_does_not_split_md_frontmatter(fake_tree):
    """When a ``.py`` exists, the sibling ``.md`` is PROSE (read verbatim by the
    module), NOT frontmatter-split — proves the dual-read takes the ``.py``
    branch even if the prose happens to start with ``---``."""
    root = fake_tree
    _agent_py(
        "p", root, tools=["python"],
        prose="---\nname: legacy\n---\n\nthis looks like frontmatter but is prose",
    )
    _org_yaml("o", ["p"], root)

    sub = orgs.load_subagents("o", _specialists())[0]
    # name comes from SUBAGENT, NOT the .md pseudo-frontmatter
    assert sub["name"] == "p"
    # system_prompt is the WHOLE .md verbatim (frontmatter not split on this path)
    assert sub["system_prompt"].startswith("---")
    assert "this looks like frontmatter but is prose" in sub["system_prompt"]


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
    """``org.yaml`` is the new roster source (a YAML list)."""
    root = fake_tree
    _org_yaml("o", ["a", "b"], root)
    assert orgs.org_agent_slugs("o") == ["a", "b"]


def test_org_agent_slugs_falls_back_to_agents_md(fake_tree):
    """No ``org.yaml`` -> read the AGENTS.md ``agents:`` frontmatter (legacy
    branch, transitional). Removed in Phase 3."""
    root = fake_tree
    _org("o", "a, b", root)
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
