"""Green gate for the declarative org contract (Phase 2).

Two tiers, mirroring ``contract.py``:

* **Structural** (no server, no tokens): one parametrized case per discovered
  org asserts ``check_org(org) == []``. This is the gate — if an org's bundle
  drifts (bad frontmatter, unresolvable slug, malformed policy), this fails.
* **Tool-resolution** (rule 4): exercised against a deliberately-empty bridge
  surface so stale/unknown tool refs fail loud — proving the rule fires.
* **Violation classes** (rules 1,2,3,5): built in a tmp tree, each asserts the
  right rule fires. Proves the enforcer catches what it claims to.

The live ``--check-contract`` (server up) is the full tool-resolution proof;
these tests are the structural + logic proof, runnable without Docker.
"""
from __future__ import annotations

from pathlib import Path

import pytest

from pux_harness import contract
from pux_harness.contract import (
    KNOWN_AGENT_KEYS,
    KNOWN_ORG_KEYS,
    KNOWN_POLICY_SECTIONS,
    NATIVE_FS_TOOLS,
    check_harness,
    check_org,
    discover_orgs,
    orphan_agents,
)


# --- the green gate ------------------------------------------------------

EXPECTED_ORGS = {
    "_demo", "deep-research-engine", "dev-bot", "game-studio", "general",
    "invest", "social-media-pipeline", "telegram-agent", "twitter-agent",
    "video-production",
}


def test_discover_orgs_finds_all_ten():
    found = set(discover_orgs())
    assert found == EXPECTED_ORGS, f"missing={EXPECTED_ORGS - found} extra={found - EXPECTED_ORGS}"


@pytest.mark.parametrize("org", sorted(EXPECTED_ORGS))
def test_org_bundle_is_green(org):
    """Structural green: AGENTS.md, frontmatter keys, slug resolution, policy."""
    violations = check_org(org)
    errors = [v for v in violations if v.severity == "error"]
    assert errors == [], f"{org}: {errors}"


def test_no_orphan_agents():
    """Every specialist is owned by >=1 org (rule 7)."""
    assert orphan_agents() == [], f"orphan agents: {orphan_agents()}"


def test_every_org_has_a_forcing_task():
    """The in-process runner (``pux direct --org <name>``) drives a delegation-
    forcing task per org. Every discovered org must have a DEFAULT_TASKS entry —
    a missing entry would make ``--org <name>`` fail with a KeyError instead of
    running. (Phase 5: all 10 orgs ported to RUN on deepagents.)"""
    from pux_harness.main import DEFAULT_TASKS

    missing = set(discover_orgs()) - set(DEFAULT_TASKS)
    assert not missing, f"orgs without a DEFAULT_TASKS forcing task: {missing}"


def test_harness_has_no_hardcoded_manifest():
    """Rule 6: the org->agent map lives in frontmatter, not harness code."""
    assert check_harness() == []


# --- rule 4 fires (tool resolution against a live-shaped surface) --------

def test_rule4_unknown_tool_fails_loud():
    """An empty bridge surface makes every pux_sandbox_* ref fail — proves the
    resolver actually checks the live surface rather than trusting the names."""
    vs = check_org("general", bridge_tools=set())
    rules = {v.rule for v in vs}
    assert "tool-resolves" in rules, f"expected tool-resolves error, got: {vs}"


def test_rule4_native_fs_tool_always_allowed(fake_tree):
    """A native fs tool in a whitelist never fails — it comes from the backend,
    not the MCP surface. Build an agent listing both a native name and a bridge
    tool; under an empty bridge, ONLY the bridge tool fails."""
    add_org, add_agent = fake_tree
    add_agent("mix", tools="read_file, mcp:pux-sandbox/bash")
    add_org("o", agents="mix")
    vs = check_org("o", bridge_tools=set())
    resolves = [v for v in vs if v.rule == "tool-resolves"]
    assert len(resolves) == 1, vs
    assert "bash" in resolves[0].message and "pux_sandbox_bash" in resolves[0].message
    assert "read_file" not in resolves[0].message
    # and the constant is exactly the documented set
    assert NATIVE_FS_TOOLS == {"ls", "read_file", "write_file", "edit_file",
                               "glob", "grep", "execute"}


# --- tmp-tree fixtures for the per-rule violation proofs -----------------

@pytest.fixture
def fake_tree(tmp_path: Path, monkeypatch):
    """A scratch orgs/.pi tree. Returns helpers to build orgs + agents, with
    contract's path helpers patched onto it."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / ".pi" / "agents").mkdir(parents=True)
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(contract, "_agents_dir", lambda: tmp_path / ".pi" / "agents")

    def add_agent(slug: str, tools: str = "", desc: str = "a specialist") -> None:
        fm = f"---\nname: {slug}\ndescription: {desc}\n"
        if tools:
            fm += f"tools: {tools}\n"
        fm += "---\n\nbody\n"
        (tmp_path / ".pi" / "agents" / f"{slug}.md").write_text(fm)

    def add_org(org: str, agents: str = "", body: str = "# Org\n",
                policy: str | None = None) -> None:
        d = tmp_path / "orgs" / org
        d.mkdir()
        head = "---\n"
        if agents:
            head += f"agents: {agents}\n"
        head += f"---\n\n{body}"
        (d / "AGENTS.md").write_text(head)
        if policy is not None:
            (d / "policy.yaml").write_text(policy)

    return add_org, add_agent


def test_rule1_missing_agents_md(fake_tree):
    add_org, _ = fake_tree
    add_org("empty")  # AGENTS.md created, but test a dir without it:
    (contract._orgs_dir() / "empty" / "AGENTS.md").unlink()
    vs = check_org("empty")
    assert any(v.rule == "org-agents-md" for v in vs)


def test_rule2_unknown_org_frontmatter_key(fake_tree):
    add_org, _ = fake_tree
    add_org("badorg")
    (contract._orgs_dir() / "badorg" / "AGENTS.md").write_text(
        "---\nbogus: yes\n---\n\n# Org\n")
    vs = check_org("badorg")
    assert any(v.rule == "org-frontmatter-keys" for v in vs)
    assert KNOWN_ORG_KEYS == {"agents"}


def test_rule3_unresolvable_slug(fake_tree):
    add_org, _ = fake_tree
    add_org("orphan", agents="nope")
    vs = check_org("orphan")
    assert any(v.rule == "agent-resolves" for v in vs)


def test_rule3_agent_missing_description(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("nodesc")
    # rewrite without description
    (contract._agents_dir() / "nodesc.md").write_text(
        "---\nname: nodesc\n---\n\nbody\n")
    add_org("o", agents="nodesc")
    vs = check_org("o")
    assert any(v.rule == "agent-description" for v in vs)


def test_rule3_unknown_agent_frontmatter_key(fake_tree):
    add_org, add_agent = fake_tree
    (contract._agents_dir() / "sp.md").write_text(
        "---\nname: sp\ndescription: x\nweirdkey: 1\n---\n\nbody\n")
    add_org("o", agents="sp")
    vs = check_org("o")
    assert any(v.rule == "agent-frontmatter-keys" for v in vs)
    assert KNOWN_AGENT_KEYS == {
        "name", "description", "tools", "systemPromptMode", "output",
        "inheritSkills", "inheritProjectContext", "defaultProgress",
    }


def test_rule5_policy_parse_error(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents="r", policy="egress: [this is : : broken\n")
    vs = check_org("o")
    assert any(v.rule in {"policy-parse", "policy-shape", "policy-sections"}
               for v in vs)


def test_rule5_policy_unknown_section(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents="r", policy="teleport:\n  beam: up\n")
    vs = check_org("o")
    assert any(v.rule == "policy-sections" for v in vs)
    assert KNOWN_POLICY_SECTIONS == {
        "workspace", "egress", "credentials", "sandbox", "browser",
    }


def test_rule5_policy_empty_is_ok(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents="r", policy="")  # _demo's real policy.yaml is empty
    assert check_org("o") == []


def test_rule5_policy_valid_sections_ok(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents="r",
            policy="egress:\n  allow: []\ncredentials:\n  required: []\n")
    assert check_org("o") == []
