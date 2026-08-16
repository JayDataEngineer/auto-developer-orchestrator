"""The folded dcode launch (src/run.py + the runtime modules) — oracle tests.

The wrapper's contract: the org tree projects onto dcode's OWN surface, no
re-implementation. These tests check against the INSTALLED SDK — the
``SubAgent`` TypedDict vocabulary, ``create_deep_agent``'s ability to compile
the produced profiles offline, and the fail-loud guards (unknown tool refs,
mcp refs naming no loaded server, unknown middleware).
"""
from __future__ import annotations

import asyncio
from pathlib import Path

import pytest

pytest.importorskip("deepagents_code.app")  # the wrapper IS dcode — nothing to test without it

from deepagents.backends import LocalShellBackend
from deepagents.graph import create_deep_agent
from deepagents.middleware import RubricMiddleware
from deepagents.middleware.subagents import SubAgent
from langchain_core.language_models.fake_chat_models import GenericFakeChatModel
from langchain_core.tools import BaseTool

from middlewares.rubric import agent_middlewares
from profiles.subagents import org_subagent_specs
from run import build_org_agent, plan
from tools.resolve import resolve_tool_ref

# The parent repo (profiles/ tree) — pux-harness is a submodule of it; skip the
# real-repo tests when checked out standalone.
_REPO = Path(__file__).resolve().parents[2]
_HAS_ORGS = (_REPO / "profiles" / "_shared" / "tool_servers.yaml").is_file()

# One offline oracle model for the whole module — graph BUILDING consumes no
# messages, so sharing one instance across compiles is safe.
_FAKE = GenericFakeChatModel(messages=iter(["ok"]))


@pytest.fixture
def tree(tmp_path: Path) -> Path:
    """Scratch org tree — same shape as the emit tests: acme extends base,
    worker carries pux-only frontmatter (tools/middleware/rubric)."""
    (tmp_path / "AGENTS.md").write_text("# Base\n\nAssistant.\n")
    shared = tmp_path / "profiles" / "_shared"
    (shared / "agents").mkdir(parents=True)
    (shared / "tool_servers.yaml").write_text(
        "remo:\n"
        "  kind: mcp\n"
        "  transport: http\n"
        "  url: ${PUX_MCP_REMO_URL}\n"
        "  headers: {Authorization: 'Basic ${PUX_MCP_REMO_AUTH}'}\n"
        "  tools: [search, fetch]\n"
        "dockerish:\n"
        "  kind: mcp\n"
        "  transport: stdio\n"
        "  command: docker\n"
        "  args: ['exec', '-i', '${PUX_SANDBOX_CONTAINER}', 'mc.py']\n")
    org = tmp_path / "profiles" / "acme"
    org.mkdir(parents=True)
    (org / "agents").mkdir()
    (org / "AGENTS.md").write_text("# Acme\n\nOverlay.\n")
    (org / "org.yaml").write_text(
        "extends: base\n"
        "agents: [worker]\n"
        "capabilities:\n"
        "  - {kind: mcp, ref: remo}\n"
        "  - {kind: mcp, ref: dockerish}\n")
    (org / "agents" / "worker.md").write_text(
        "---\nname: worker\ndescription: does work\ntools: [python]\n"
        "middleware: [rubric]\nrubric: 'grade hard'\nextends: baseonly\n"
        "---\n\nWorker body.\n")
    base = tmp_path / "profiles" / "base"
    base.mkdir()
    (base / "AGENTS.md").write_text("# Base Org\n\nbase overlay.\n")
    (base / "org.yaml").write_text("agents: [baseonly]\n")
    (shared / "agents" / "baseonly.md").write_text(
        "---\nname: baseonly\ndescription: inherited\n---\n\nInherited body.\n")
    return tmp_path


def _compile_org(org: str, root: Path, *, servers: dict | None = None):
    """Build the org's graph offline: declared-server names substitute for the
    MCP load, a fake model substitutes for the configured spec."""
    specs = org_subagent_specs(
        org, project_root=root, mcp_tools_by_server=servers or {},
        model="<test>", sandbox=LocalShellBackend())
    return create_deep_agent(
        model=_FAKE,
        system_prompt="test prompt",
        tools=[],
        subagents=specs or None,
        backend=LocalShellBackend(root_dir=str(root)),
        name=f"pux-{org}",
    )


# --- the profile vocabulary stays ON the installed SDK's SubAgent surface ----

def test_spec_keys_stay_within_subagent_contract(tree):
    """The wrapper emits ONLY SubAgent-annotated keys — if the installed SDK
    drops a key we use (or we invent one), this raises."""
    specs = org_subagent_specs(
        "acme", project_root=tree, mcp_tools_by_server={}, model="<test>",
        sandbox=LocalShellBackend())
    by_name = {s["name"]: s for s in specs}
    assert set(by_name) == {"baseonly", "worker"}  # chain-inherited roster
    worker = by_name["worker"]
    assert set(worker) <= set(SubAgent.__annotations__)
    # the extends chain materialized in the prompt, pux frontmatter → native
    assert worker["system_prompt"] == "Inherited body.\n\nWorker body."
    assert [t.name for t in worker["tools"]] == ["python"]
    assert isinstance(worker["middleware"][0], RubricMiddleware)
    assert worker["description"] == "does work"


def test_subagent_contract_annotations_pin_sdk_vocabulary():
    """The six keys the wrapper writes must exist on the installed SDK's
    TypedDict — a future SubAgent refactor that renames them fails HERE,
    at the wrapper's vocabulary boundary."""
    assert set(SubAgent.__annotations__) >= {
        "name", "description", "system_prompt", "tools", "model", "middleware",
    }


# --- tools / middleware refs --------------------------------------------------

def test_tool_ref_resolves_to_live_tool():
    tool = resolve_tool_ref("python", sandbox=LocalShellBackend())
    assert isinstance(tool, BaseTool)
    assert tool.name == "python"


def test_unknown_tool_ref_raises():
    with pytest.raises(ValueError, match="unknown tool ref 'ghost'"):
        resolve_tool_ref("ghost")


def test_unknown_middleware_ref_raises():
    with pytest.raises(ValueError, match="unknown middleware ref 'bogus'"):
        agent_middlewares({"middleware": ["bogus"]}, model="<test>")


def test_rubric_maps_to_native_rubric_middleware():
    mw = agent_middlewares({"middleware": ["rubric"], "rubric": "grade hard"},
                           model="<test>")
    assert len(mw) == 1 and isinstance(mw[0], RubricMiddleware)


# --- mcp refs: fail loud on a server that never loaded ------------------------

def test_mcp_ref_not_among_loaded_servers_raises(tree):
    (tree / "profiles" / "acme" / "agents" / "worker.md").write_text(
        "---\nname: worker\ndescription: d\nmcp: [{kind: mcp, ref: remo}]\n"
        "---\n\nBody.\n")
    with pytest.raises(ValueError,
                       match="mcp ref 'remo' is not among the org's loaded servers"):
        org_subagent_specs("acme", project_root=tree, mcp_tools_by_server={},
                           model="<test>", sandbox=LocalShellBackend())


def test_mcp_ref_attaches_loaded_server_tools(tree):
    from tools.resolve import resolve_tool_ref

    (tree / "profiles" / "acme" / "agents" / "worker.md").write_text(
        "---\nname: worker\ndescription: d\nmcp: [{kind: mcp, ref: remo}]\n"
        "---\n\nBody.\n")
    server_tool = resolve_tool_ref("python", sandbox=LocalShellBackend())
    specs = org_subagent_specs(
        "acme", project_root=tree, mcp_tools_by_server={"remo": [server_tool]},
        model="<test>", sandbox=LocalShellBackend())
    worker = next(s for s in specs if s["name"] == "worker")
    assert [t.name for t in worker["tools"]] == ["python"]


# --- graph compilation (the THIN proof: dcode builds our profiles) ------------

def test_graph_compiles_offline_scratch_org(tree):
    """create_deep_agent — dcode's own graph builder — compiles the scratch
    org's profiles offline (no MCP, fake model). If the wrapper emitted
    something dcode can't consume, this raises."""
    agent = _compile_org("acme", tree)
    assert agent.get_graph().nodes  # a compiled graph with nodes


def test_plan_dry_run(tree):
    """plan() resolves the full profile without loading MCP or touching a
    model — the dry-run the CLI's --dry-run runs."""
    p = plan("acme", project_root=tree)
    assert p["org"] == "acme"
    assert p["mcp_servers"] == ["dockerish", "remo"]
    assert p["model_default"]  # dcode's native default resolves
    worker = next(s for s in p["subagents"] if s["name"] == "worker")
    assert worker["tools"] == ["python"]
    assert worker["middleware"] == ["RubricMiddleware"]


def test_build_org_agent_offline_without_mcp(tree):
    """The full builder path minus the MCP load (explicit opt-out) — model,
    chained prompt, backend, subagents all resolve. A chat-model INSTANCE is
    passed because create_deep_agent eagerly resolves string specs."""
    agent, backend = asyncio.run(build_org_agent(
        "acme", project_root=tree, model=_FAKE, load_mcp=False))
    assert agent is not None
    assert backend is not None


# --- real repo ----------------------------------------------------------------

@pytest.mark.skipif(not _HAS_ORGS, reason="no parent profiles/ tree (standalone checkout)")
def test_real_coder_plan():
    p = plan("coder", project_root=_REPO)
    assert p["mcp_servers"] == ["github", "sandbox_browser"]
    by_name = {s["name"]: s for s in p["subagents"]}
    assert list(by_name) == ["coder-explorer", "code-worker", "web-agent"]
    assert by_name["coder-explorer"]["tools"] == ["python"]
    assert by_name["coder-explorer"]["middleware"] == []
    assert by_name["code-worker"]["tools"] == ["python"]
    assert by_name["code-worker"]["middleware"] == ["RubricMiddleware"]
    assert by_name["web-agent"]["tools"] == []  # mcp-only — dry-run loads no server tools
    assert by_name["web-agent"]["middleware"] == ["RubricMiddleware"]


@pytest.mark.skipif(not _HAS_ORGS, reason="no parent profiles/ tree (standalone checkout)")
def test_real_coder_compiles_offline():
    """The REAL coder org (3 subagents, rubric middleware, mcp-only web-agent)
    compiles through dcode's own graph builder with zero network."""
    agent = _compile_org(
        "coder", _REPO, servers={"github": [], "sandbox_browser": []})
    assert agent.get_graph().nodes


@pytest.mark.skipif(not _HAS_ORGS, reason="no parent profiles/ tree (standalone checkout)")
def test_real_coder_only_validates_referenced_servers():
    """Per-agent validation checks the servers a SUBAGENT references — an
    org-level server no agent references is not a subagent's contract. (The
    org-level surface has its own fail-loud at load time, in ``_load_mcp``.)"""
    specs = org_subagent_specs(
        "coder", project_root=_REPO,
        mcp_tools_by_server={"sandbox_browser": []},  # github absent — unused
        model="<test>", sandbox=LocalShellBackend())
    web = next(s for s in specs if s["name"] == "web-agent")
    assert "tools" not in web  # no server tools loaded in this offline probe
