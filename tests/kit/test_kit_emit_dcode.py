"""The thin-wrapper compiler (``kit.emit_dcode``) — org tree → dcode-native.

``pux compile --org <org>`` projects the SAME source of truth the runtime
loads (the profiles/ tree) onto the three surfaces Deep Agents Code
discovers natively:
``.deepagents/agents/<name>/AGENTS.md`` (roster, extends-chains materialized,
pux-only frontmatter dropped), ``.deepagents/skills/`` (supervisor skill
roots), and ``.mcp.json`` (``capabilities:`` mcp refs → the standard
``mcpServers`` schema, ``${VAR}`` placeholders passed through verbatim — dcode
interpolates them at activation).

The dcode-oracle tests load the emitted artifacts with **dcode's own loaders**
(``deepagents_code.subagents`` / ``mcp_tools.load_mcp_config``) — the proof the
wrapper is THIN: dcode consumes the org with zero pux code on the path.
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest
import yaml

from compiler.emit import emit_dcode
from plugins.marketplace import emit_marketplace, emit_plugin

# The parent repo (profiles/ tree) — the real-repo tests activate only when the
# workspace tree is present (_HAS_ORGS); they skip when the suite runs standalone.
_REPO = Path(__file__).resolve().parents[2]
_HAS_ORGS = (_REPO / "profiles" / "_shared" / "tool_servers.yaml").is_file()


@pytest.fixture
def tree(tmp_path: Path) -> Path:
    """Scratch org tree: one org extending another, a catalog with one server
    per transport, and a shared skill. Roster agent carries pux-only frontmatter
    (tools/middleware/rubric) + an ``extends:`` base — both must be handled."""
    (tmp_path / "AGENTS.md").write_text("# Base\n\nAssistant.\n")
    shared = tmp_path / "profiles" / "_shared"
    (shared / "agents").mkdir(parents=True)
    (shared / "skills" / "cite").mkdir(parents=True)
    (shared / "skills" / "cite" / "SKILL.md").write_text(
        "---\nname: cite\ndescription: cite things\n---\n\n# Cite\n\nalways cite\n")
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
        "  args: ['exec', '-i', '${PUX_SANDBOX_CONTAINER}', 'mc.py']\n"
    )
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
        "middleware: [rubric]\n"
        "extends: baseonly\n"
        "---\n\nWorker body.\n")
    base = tmp_path / "profiles" / "base"
    base.mkdir()
    (base / "AGENTS.md").write_text("# Base Org\n\nbase overlay.\n")
    (base / "org.yaml").write_text("agents: [baseonly]\n")
    (shared / "agents" / "baseonly.md").write_text(
        "---\nname: baseonly\ndescription: inherited\n---\n\nInherited body.\n")
    return tmp_path


def _agents_md(out: Path, name: str) -> dict:
    text = (out / ".deepagents" / "agents" / name / "AGENTS.md").read_text()
    fm_raw, body = text.split("\n---\n", 1)
    fm = yaml.safe_load(fm_raw.removeprefix("---\n"))
    return {**fm, "body": body.strip()}


# --- agents -----------------------------------------------------------------

def test_agents_materialized_pux_keys_dropped(tree):
    out = tree / "stage"
    w = emit_dcode("acme", project_root=tree, out=out)
    # chain-inherited roster (base's baseonly ∪ acme's worker), both emitted
    assert sorted(w["agents"]) == ["baseonly", "worker"]
    worker = _agents_md(out, "worker")
    assert worker["name"] == "worker"
    assert worker["description"] == "does work"
    # body = merged extends-chain prompt (base first, delta last)
    assert worker["body"] == "Inherited body.\n\nWorker body."
    # pux-only frontmatter is GONE (dcode reads name/description/model only)
    assert not any(k in worker for k in ("tools", "middleware", "rubric", "extends", "capabilities"))


def test_agent_extends_chain_materialized(tree):
    """An agent whose body extends a library base emits the MERGED prompt (the
    chain resolves), not just the delta file."""
    out = tree / "stage"
    emit_dcode("acme", project_root=tree, out=out)
    body = _agents_md(out, "worker")["body"]
    assert "Worker body." in body  # the delta survived the merge


def test_model_frontmatter_passes_through(tree):
    (tree / "profiles" / "acme" / "agents" / "worker.md").write_text(
        "---\nname: worker\ndescription: d\nmodel: anthropic:claude-haiku-4-5\n"
        "---\n\nBody.\n")
    out = tree / "stage"
    emit_dcode("acme", project_root=tree, out=out)
    assert _agents_md(out, "worker")["model"] == "anthropic:claude-haiku-4-5"


# --- skills -----------------------------------------------------------------

def test_skills_copied_from_supervisor_roots(tree):
    out = tree / "stage"
    w = emit_dcode("acme", project_root=tree, out=out)
    assert w["skills"] == ["cite"]
    copied = (out / ".deepagents" / "skills" / "cite" / "SKILL.md").read_text()
    assert "always cite" in copied


def test_no_skills_no_dir(tree):
    """An org with no skill roots writes no ``.deepagents/skills`` dir."""
    (tree / "profiles" / "_shared" / "skills").mkdir(parents=True, exist_ok=True)
    import shutil
    shutil.rmtree(tree / "profiles" / "_shared" / "skills")
    out = tree / "stage"
    w = emit_dcode("acme", project_root=tree, out=out)
    assert w["skills"] == []
    assert not (out / ".deepagents" / "skills").exists()


# --- .mcp.json -----------------------------------------------------------------

def test_mcp_json_shapes_and_placeholder_passthrough(tree):
    out = tree / "stage"
    w = emit_dcode("acme", project_root=tree, out=out)
    assert w["mcp"] == ["dockerish", "remo"]
    cfg = json.loads((out / ".mcp.json").read_text())["mcpServers"]
    # stdio → command/args/env; allowlist → allowedTools; ${VAR} stays RAW
    assert cfg["dockerish"] == {
        "command": "docker",
        "args": ["exec", "-i", "${PUX_SANDBOX_CONTAINER}", "mc.py"],
    }
    assert cfg["remo"] == {
        "type": "http", "url": "${PUX_MCP_REMO_URL}",
        "headers": {"Authorization": "Basic ${PUX_MCP_REMO_AUTH}"},
        "allowedTools": ["search", "fetch"],
    }


def test_mcp_json_merges_foreign_entries(tree, tmp_path):
    out = tree / "stage"
    out.mkdir()
    (out / ".mcp.json").write_text(json.dumps(
        {"mcpServers": {"operator-manual": {"command": "foo"}}}))
    emit_dcode("acme", project_root=tree, out=out)
    cfg = json.loads((out / ".mcp.json").read_text())["mcpServers"]
    assert set(cfg) == {"operator-manual", "remo", "dockerish"}  # foreign kept


def test_unknown_ref_fails_loud(tree):
    (tree / "profiles" / "acme" / "org.yaml").write_text(
        "agents: [worker]\ncapabilities:\n  - {kind: mcp, ref: ghost}\n")
    with pytest.raises(ValueError, match="unknown catalog ref 'ghost'"):
        emit_dcode("acme", project_root=tree, out=tree / "stage")


def test_no_capabilities_writes_no_mcp_json(tree):
    (tree / "profiles" / "acme" / "org.yaml").write_text("agents: [worker]\n")
    out = tree / "stage"
    w = emit_dcode("acme", project_root=tree, out=out)
    assert w["mcp"] == []
    assert not (out / ".mcp.json").exists()


# --- plugin / marketplace (the distribution surface) --------------------------

def test_plugin_carries_skills_and_mcp(tree):
    """One org → one installable plugin: skills + the org's MCP servers. The
    plugin is the CAPABILITY unit — no agents/ (dcode ignores it in plugins)."""
    w = emit_plugin("acme", project_root=tree, out=tree / "stage")
    assert w is not None
    assert w["name"] == "acme"
    assert w["skills"] == ["cite"]
    assert w["mcp"] == ["dockerish", "remo"]
    plugin = tree / "stage" / "plugins" / "acme"
    manifest = json.loads((plugin / ".claude-plugin" / "plugin.json").read_text())
    assert manifest == {"name": "acme", "skills": "./skills", "mcpServers": "./.mcp.json"}
    # the SAME mcpServers shapes the project emission produces
    mcp = json.loads((plugin / ".mcp.json").read_text())["mcpServers"]
    assert mcp["remo"]["allowedTools"] == ["search", "fetch"]
    assert (plugin / "skills" / "cite" / "SKILL.md").is_file()
    assert not (plugin / "agents").exists()


def test_marketplace_emits_catalog_and_oracle_loads_it(tree):
    """Every org (with something to distribute) → a plugin + the pux-orgs
    catalog. THE oracle: dcode's own marketplace/manifest parsers consume the
    emitted layout with zero pux code on the path."""
    (tree / "profiles" / "acme" / "org.yaml").write_text(
        "extends: base\n"
        "description: Acme things\n"
        "agents: [worker]\n"
        "capabilities:\n"
        "  - {kind: mcp, ref: remo}\n"
        "  - {kind: mcp, ref: dockerish}\n")
    (tree / "profiles" / "_skipme").mkdir()
    (tree / "profiles" / "_skipme" / "AGENTS.md").write_text("# internal\n")
    w = emit_marketplace(project_root=tree, out=tree / "stage")
    assert w["marketplace"] == "pux-orgs"
    assert sorted(w["plugins"]) == ["acme", "base"]  # base carries the shared skill
    assert "_skipme" not in w["plugins"]  # underscore orgs are internal
    cat = json.loads((tree / "stage" / ".agents" / "plugins" / "marketplace.json").read_text())
    by_name = {e["name"]: e for e in cat["plugins"]}
    assert by_name["acme"]["source"] == "./plugins/acme"
    assert by_name["acme"]["description"] == "Acme things"

    pytest.importorskip("deepagents_code.app")
    from deepagents_code.plugins.manifest import build_inventory, load_manifest
    from deepagents_code.plugins.marketplace import load_marketplace
    from deepagents_code.plugins.models import LocalPluginSource

    mkt = load_marketplace(tree / "stage")
    assert mkt.name == "pux-orgs"
    assert not mkt.warnings
    acme = next(e for e in mkt.plugins if e.name == "acme")
    assert isinstance(acme.source, LocalPluginSource) and acme.source.path == "./plugins/acme"
    m, _, warns = load_manifest(tree / "stage" / "plugins" / "acme")
    assert m is not None and m.name == "acme" and not warns
    inv = build_inventory(tree / "stage" / "plugins" / "acme", m)
    assert inv.skills and inv.mcp_files and not inv.unsupported


def test_marketplace_preserves_foreign_replaces_stale(tree):
    """Operator-owned entries (non-``./plugins/`` sources) survive; OUR stale
    ``./plugins/*`` entries are dropped on re-emit — the catalog is
    regenerable, a removed org leaves no ghost."""
    out = tree / "stage"
    out.mkdir()
    (out / ".agents" / "plugins").mkdir(parents=True)
    (out / ".agents" / "plugins" / "marketplace.json").write_text(json.dumps(
        {"name": "pux-orgs", "plugins": [
            {"name": "operator-tool", "source": "./vendor/operator-tool"},
            {"name": "ghost", "source": "./plugins/ghost"}]}))
    emit_marketplace(project_root=tree, out=out)
    cat = json.loads((out / ".agents" / "plugins" / "marketplace.json").read_text())
    names = {e["name"] for e in cat["plugins"]}
    assert names >= {"operator-tool", "acme", "base"}
    assert "ghost" not in names


def test_empty_org_is_not_a_plugin(tree):
    """A roster-only org (no skills, no capabilities) emits NO plugin and no
    catalog entry — the roster is a project layout, not a distribution unit."""
    import shutil
    shutil.rmtree(tree / "profiles" / "_shared" / "skills")  # drop the shared skill
    (tree / "profiles" / "base" / "org.yaml").write_text("agents: [baseonly]\n")  # no capabilities
    w = emit_marketplace(project_root=tree, out=tree / "stage")
    assert w["plugins"] == ["acme"]  # acme still has remo+dockerish
    assert not (tree / "stage" / "plugins" / "base").exists()
    cat = json.loads((tree / "stage" / ".agents" / "plugins" / "marketplace.json").read_text())
    assert [e["name"] for e in cat["plugins"]] == ["acme"]


# --- real repo -----------------------------------------------------------------

@pytest.mark.skipif(not _HAS_ORGS, reason="no parent profiles/ tree (standalone checkout)")
def test_real_coder_emits_full_layout(tmp_path):
    w = emit_dcode("coder", project_root=_REPO, out=tmp_path)
    assert sorted(w["agents"]) == ["code-worker", "coder-explorer", "web-agent"]
    assert sorted(w["mcp"]) == ["github", "sandbox_browser"]
    # the in-container placeholder stays raw — dcode resolves it at activation
    mcp = json.loads((tmp_path / ".mcp.json").read_text())["mcpServers"]
    assert "${PUX_SANDBOX_CONTAINER}" in mcp["sandbox_browser"]["args"]
    assert mcp["github"]["env"]["GITHUB_PERSONAL_ACCESS_TOKEN"] == "${GITHUB_TOKEN}"


@pytest.mark.skipif(not _HAS_ORGS, reason="no parent profiles/ tree (standalone checkout)")
def test_dcode_loads_the_emitted_layout(tmp_path):
    """THE thin-wrapper proof: dcode's OWN discovery (subagents + MCP config
    validation) consumes the emitted layout with zero pux code on the path."""
    pytest.importorskip("deepagents_code.app")
    from deepagents_code.mcp_tools import load_mcp_config
    from deepagents_code.subagents import _load_subagents_from_dir

    emit_dcode("coder", project_root=_REPO, out=tmp_path)
    subs = _load_subagents_from_dir(tmp_path / ".deepagents" / "agents", "project")
    assert sorted(subs) == ["code-worker", "coder-explorer", "web-agent"]
    for s in subs.values():
        assert s["description"] and s["system_prompt"]
    cfg = load_mcp_config(str(tmp_path / ".mcp.json"))
    assert sorted(cfg["mcpServers"]) == ["github", "sandbox_browser"]


@pytest.mark.skipif(not _HAS_ORGS, reason="no parent profiles/ tree (standalone checkout)")
def test_real_marketplace_loads_in_dcode(tmp_path):
    """The REAL org tree as a marketplace: dcode's own parser loads the whole
    catalog warning-free, and the coder plugin's inventory carries MCP."""
    pytest.importorskip("deepagents_code.app")
    from deepagents_code.plugins.manifest import build_inventory, load_manifest
    from deepagents_code.plugins.marketplace import load_marketplace

    w = emit_marketplace(project_root=_REPO, out=tmp_path)
    assert "coder" in w["plugins"]
    mkt = load_marketplace(tmp_path)
    assert not mkt.warnings
    assert {e.name for e in mkt.plugins} == set(w["plugins"])
    m, _, _ = load_manifest(tmp_path / "plugins" / "coder")
    assert m is not None and m.name == "coder"
    assert build_inventory(tmp_path / "plugins" / "coder", m).mcp_files
