"""Green gate for the declarative org contract (Phase 2).

Two tiers, mirroring ``contract.py``:

* **Structural** (no server, no tokens): one parametrized case per discovered
  org asserts ``check_org(org) == []``. This is the gate — if an org's bundle
  drifts (bad frontmatter, unresolvable slug, malformed policy), this fails.
* **Tool-resolution** (rule 4): the org contract resolves every agent
  ``tools:`` entry against ``NATIVE_FS_TOOLS`` ∪ ``SPECIALIST_TOOL_NAMES``
  (both Python constants). These tests feed a bogus tool so the rule fires
  loud, and a real native-fs name so it's correctly allowed.
* **Violation classes** (rules 1,2,3,5): built in a tmp tree, each asserts the
  right rule fires. Proves the enforcer catches what it claims to.

Both ``--check-contract`` and these tests resolve against the same static
surface — no Go server, no Docker, no tokens.
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


# --- rule 4 fires (tool resolution against the static native surface) -----

def test_rule4_unknown_tool_fails_loud(fake_tree):
    """An agent ``tools:`` entry that's neither a native fs tool nor a known
    ``pux_sandbox_*`` specialist fails loud — proves the resolver checks the
    static surface rather than trusting the names."""
    add_org, add_agent = fake_tree
    add_agent("ghost", tools="mcp:pux-sandbox/no_such_tool")
    add_org("o", agents="ghost")
    vs = check_org("o")
    rules = {v.rule for v in vs}
    assert "tool-resolves" in rules, f"expected tool-resolves error, got: {vs}"


def test_rule4_native_fs_tool_always_allowed(fake_tree):
    """A native fs tool in a whitelist never fails — it comes from the backend,
    not the specialist registry. Build an agent listing both a native name and
    an unknown specialist tool; ONLY the unknown specialist tool fails."""
    add_org, add_agent = fake_tree
    add_agent("mix", tools="read_file, mcp:pux-sandbox/bash")
    add_org("o", agents="mix")
    vs = check_org("o")
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
    (tmp_path / ".pi" / "skills").mkdir(parents=True)
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
        "model", "skills", "response_format", "permissions", "interrupt_on",
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


def test_rule5_policy_non_mapping_section_caught(fake_tree):
    """Deep schema (Phase 6): a known section that is NOT a mapping passes the
    shallow section check (the key is known) but fails the real policy engine —
    ``policy.load`` raises 'section must be a mapping'. Proves the load layer."""
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents="r", policy="egress: not-a-mapping\n")
    vs = check_org("o")
    assert any(v.rule == "policy-schema" for v in vs), vs
    # and the shallow section check must NOT fire — the key IS known
    assert not any(v.rule == "policy-sections" for v in vs)


def test_rule5_policy_bad_mount_caught(fake_tree):
    """Deep schema (Phase 6): a workspace mount with a relative container path
    parses as valid YAML + known sections, so both the shallow checks pass —
    only ``resolve_mounts`` (called by the contract's deep check) catches it.
    Proves the resolve_mounts layer (no network — safe offline)."""
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents="r",
            policy="workspace:\n  mounts:\n    - host: /abs/path\n"
                   "      container: relative/path\n")
    vs = check_org("o")
    assert any(v.rule == "policy-schema" for v in vs), vs
    assert not any(v.rule == "policy-sections" for v in vs)


# --- rule 4b fires (Phase-10 rich SubAgent fields, offline validation) ----

def _write_agent(slug: str, extra_fm: str) -> None:
    """Write an agent file with arbitrary extra frontmatter (the Phase-10 rich
    fields). Uses contract's patched ``_agents_dir`` so it lands in fake_tree."""
    (contract._agents_dir() / f"{slug}.md").write_text(
        f"---\nname: {slug}\ndescription: x\n{extra_fm}---\n\nbody\n")


def test_rule4b_model_must_be_string(fake_tree):
    add_org, _ = fake_tree
    _write_agent("a", "model: 5\n")
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "model-shape" for v in vs), vs


def test_rule4b_skill_source_must_resolve(fake_tree):
    """A skills source that isn't a directory under the project root fails loud
    (deepagents loads skills via the backend, so a missing source loads nothing)."""
    add_org, _ = fake_tree
    _write_agent("a", "skills: nope\n")
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "skill-source-resolves" for v in vs), vs


def test_rule4b_skill_source_must_be_relative(fake_tree):
    """A skills source must be project-relative (deepagents resolves it against
    the backend); an absolute path or parent-escape is rejected."""
    add_org, _ = fake_tree
    _write_agent("a", "skills: /etc\n")
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "skill-source-shape" for v in vs), vs


def test_rule4b_skill_source_root_resolves(fake_tree):
    """A skills ROOT path (project-relative dir whose children are <skill>/) is
    valid — deepagents scans it for every child skill."""
    add_org, _ = fake_tree
    _write_agent("a", "skills: .pi/skills\n")
    add_org("o", agents="a")
    assert check_org("o") == []


def test_rule4b_response_format_must_be_mapping(fake_tree):
    add_org, _ = fake_tree
    _write_agent("a", "response_format: not-a-map\n")
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "response-format-shape" for v in vs), vs


def test_rule4b_permissions_must_be_list(fake_tree):
    add_org, _ = fake_tree
    _write_agent("a", "permissions: {operations: [read]}\n")
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "permissions-shape" for v in vs), vs


def test_rule4b_permissions_unknown_key(fake_tree):
    add_org, _ = fake_tree
    _write_agent("a", "permissions:\n  - operations: [read]\n"
                       "    paths: [/x]\n    bogus: y\n")
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "permissions-shape" for v in vs), vs


def test_rule4b_permissions_bad_mode(fake_tree):
    add_org, _ = fake_tree
    _write_agent("a", "permissions:\n  - operations: [read]\n"
                       "    paths: [/x]\n    mode: teleport\n")
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "permissions-shape" for v in vs), vs


def test_rule4b_permissions_bad_path_rejected(fake_tree):
    """A relative path slips past the key/mode checks but deepagents'
    ``FilesystemPermission.__post_init__`` rejects it — the contract reuses
    that validation so a bad path fails here, not mid-run."""
    add_org, _ = fake_tree
    _write_agent("a", "permissions:\n  - operations: [read]\n"
                       "    paths: [relative]\n")
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "permissions-shape" for v in vs), vs


def test_rule4b_interrupt_on_must_be_bool_map(fake_tree):
    add_org, _ = fake_tree
    _write_agent("a", 'interrupt_on:\n  some_tool: "yes"\n')
    add_org("o", agents="a")
    vs = check_org("o")
    assert any(v.rule == "interrupt-on-shape" for v in vs), vs


def test_rule4b_all_rich_fields_green(fake_tree):
    """All five Phase-10 fields valid -> the org is green (the loader will
    resolve them; the contract validates them offline without get_model)."""
    add_org, _ = fake_tree
    _write_agent(
        "a",
        "model: glm-5.2\n"
        "skills: .pi/skills\n"
        "response_format:\n  type: object\n"
        "permissions:\n  - operations: [read, write]\n"
        "    paths: [/sandbox/workspace]\n"
        "interrupt_on:\n  task: true\n",
    )
    add_org("o", agents="a")
    assert check_org("o") == [], check_org("o")
