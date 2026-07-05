"""Green gate for the declarative org contract.

Two tiers, mirroring ``contract.py``:

* **Structural** (no server, no tokens): one parametrized case per discovered
  org asserts ``check_org(org) == []``. This is the gate — if an org's bundle
  drifts (bad frontmatter, unresolvable slug, malformed policy), this fails.
* **Tool-resolution** (rule 4): the org contract resolves every agent
  ``SUBAGENT["tools"]`` entry against ``NATIVE_FS_TOOLS`` ∪
  ``SPECIALIST_TOOL_NAMES`` (both Python constants). These tests feed a bogus
  tool so the rule fires loud, and a real native-fs name so it's correctly
  allowed.
* **Violation classes** (rules 1,3,5): built in a tmp tree, each asserts the
  right rule fires. Proves the enforcer catches what it claims to.

Both ``--check-contract`` and these tests resolve against the same static
surface — no Go server, no Docker, no tokens.
"""
from __future__ import annotations

import json
from pathlib import Path

import pytest

from pux_harness.agent import contract, orgs
from pux_harness.agent.contract import (
    KNOWN_POLICY_SECTIONS,
    NATIVE_FS_TOOLS,
    _REQUIRED_AGENT_KEYS,
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
    """Structural green: AGENTS.md, org.yaml, slug resolution, policy."""
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
    """Rule 6: the org->agent map lives in org.yaml, not harness code."""
    assert check_harness() == []


# --- rule 4 fires (tool resolution against the static native surface) -----

def test_rule4_unknown_tool_fails_loud(fake_tree):
    """An agent ``tools:`` entry that's neither a native fs tool nor a known
    ``pux_sandbox_*`` specialist fails loud — proves the resolver checks the
    static surface rather than trusting the names."""
    add_org, add_agent = fake_tree
    add_agent("ghost", tools=["no_such_tool"])
    add_org("o", agents=["ghost"])
    vs = check_org("o")
    rules = {v.rule for v in vs}
    assert "tool-resolves" in rules, f"expected tool-resolves error, got: {vs}"


def test_rule4_native_fs_tool_always_allowed(fake_tree):
    """A native fs tool in a whitelist never fails — it comes from the backend,
    not the specialist registry. Build an agent listing both a native name and
    an unknown specialist tool; ONLY the unknown specialist tool fails."""
    add_org, add_agent = fake_tree
    add_agent("mix", tools=["read_file", "bash"])
    add_org("o", agents=["mix"])
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
    """A scratch orgs/ tree (no .pi/). Returns helpers to build orgs + agents.

    Both ``contract._orgs_dir`` AND ``orgs._orgs_dir`` are patched: rule-3
    resolution delegates into ``orgs._load_agent_spec`` (which reads
    ``orgs._orgs_dir`` via ``_agent_search_dirs``), while the contract's own
    path helpers read ``contract._orgs_dir`` — patching only one lets the two
    halves see different trees. ``_shared/{agents,skills}`` are pre-created
    (the shared agent search dir + the default skills root)."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "skills").mkdir(parents=True)
    monkeypatch.setattr(contract, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")

    def add_agent(slug: str, tools: list[str] | None = None,
                  desc: str = "a specialist",
                  skills: list[str] | None = None,
                  org: str = "o") -> None:
        """Write a frontmatter+body ``orgs/<org>/agents/<slug>.md`` — the ONE
        file per agent (mirrors the SKILL.md convention). List fields are
        emitted as YAML flow sequences (JSON is a valid YAML subset)."""
        agents_dir = tmp_path / "orgs" / org / "agents"
        agents_dir.mkdir(parents=True, exist_ok=True)
        fm = ["---", f'name: "{slug}"', f'description: "{desc}"']
        if tools is not None:
            fm.append(f"tools: {json.dumps(tools)}")
        if skills is not None:
            fm.append(f"skills: {json.dumps(skills)}")
        fm.append("---")
        (agents_dir / f"{slug}.md").write_text("\n".join(fm) + "\n\nprose body\n")

    def add_org(org: str, agents: list[str] | None = None, body: str = "# Org\n",
                policy: str | None = None) -> None:
        """Write org.yaml (roster) + prose-only AGENTS.md (+ optional policy)."""
        d = tmp_path / "orgs" / org
        d.mkdir(exist_ok=True)
        if agents is not None:
            (d / "org.yaml").write_text(f"agents: [{', '.join(agents)}]\n")
        (d / "AGENTS.md").write_text(body)
        if policy is not None:
            (d / "policy.yaml").write_text(policy)

    return add_org, add_agent


def test_rule1_missing_agents_md(fake_tree):
    add_org, _ = fake_tree
    add_org("empty")  # AGENTS.md created, but test a dir without it:
    (contract._orgs_dir() / "empty" / "AGENTS.md").unlink()
    vs = check_org("empty")
    assert any(v.rule == "org-agents-md" for v in vs)


def test_rule2_legacy_frontmatter_rejected(fake_tree):
    """AGENTS.md with any frontmatter is rejected by the no-legacy-org-roster
    tripwire — the roster must live in org.yaml."""
    add_org, _ = fake_tree
    add_org("badorg")
    (contract._orgs_dir() / "badorg" / "AGENTS.md").write_text(
        "---\nagents: x\n---\n\n# Org\n")
    vs = check_org("badorg")
    assert any(v.rule == "no-legacy-org-roster" for v in vs)


def test_rule3_unresolvable_slug(fake_tree):
    add_org, _ = fake_tree
    add_org("orphan", agents=["nope"])
    vs = check_org("orphan")
    assert any(v.rule == "agent-resolves" for v in vs)


def test_rule3_agent_missing_required_key(fake_tree):
    add_org, _ = fake_tree
    # Write an agent .md whose frontmatter is missing 'description'
    agents_dir = contract._orgs_dir() / "o" / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    (agents_dir / "nodesc.md").write_text("---\nname: nodesc\n---\n\nprose\n")
    add_org("o", agents=["nodesc"])
    vs = check_org("o")
    assert any(v.rule == "agent-missing-keys" for v in vs), vs


def test_rule3_agent_frontmatter_parse_error(fake_tree):
    """A roster agent .md whose frontmatter doesn't parse is caught as
    agent-resolves — the loader fails loud (yaml.safe_load raises) rather than
    yielding a junk spec. (.py agents are forbidden, so the old import-error
    path is gone; the equivalent failure mode is a malformed .md.)"""
    add_org, _ = fake_tree
    agents_dir = contract._orgs_dir() / "o" / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    # colon-space in an unquoted scalar is a YAML parse error
    (agents_dir / "broken.md").write_text(
        "---\nname: broken\ndescription: bad: value\n---\n\nprose\n")
    add_org("o", agents=["broken"])
    vs = check_org("o")
    assert any(v.rule == "agent-resolves" for v in vs), vs


def test_rule3_unknown_agent_key_warned(fake_tree):
    """A SUBAGENT dict with unexpected keys still loads (extra keys are
    harmless) — but _REQUIRED_AGENT_KEYS must be present."""
    add_org, add_agent = fake_tree
    add_agent("sp", desc="x")
    add_org("o", agents=["sp"])
    vs = check_org("o")
    # No violation — unknown keys are fine, required keys are present
    assert not any(v.rule == "agent-missing-keys" for v in vs)


def test_rule5_policy_parse_error(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="egress: [this is : : broken\n")
    vs = check_org("o")
    assert any(v.rule in {"policy-parse", "policy-shape", "policy-sections"}
               for v in vs)


def test_rule5_policy_unknown_section(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="teleport:\n  beam: up\n")
    vs = check_org("o")
    assert any(v.rule == "policy-sections" for v in vs)
    assert KNOWN_POLICY_SECTIONS == {
        "workspace", "egress", "credentials", "sandbox", "browser", "host_setup",
        "jobs",
    }


def test_rule5_policy_empty_is_ok(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="")  # _demo's real policy.yaml is empty
    assert check_org("o") == []


def test_rule5_policy_valid_sections_ok(fake_tree):
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"],
            policy="egress:\n  allow: []\ncredentials:\n  required: []\n")
    assert check_org("o") == []


def test_rule5_policy_non_mapping_section_caught(fake_tree):
    """Deep schema (Phase 6): a known section that is NOT a mapping passes the
    shallow section check (the key is known) but fails the real policy engine —
    ``policy.load`` raises 'section must be a mapping'. Proves the load layer."""
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="egress: not-a-mapping\n")
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
    add_org("o", agents=["r"],
            policy="workspace:\n  mounts:\n    - host: /abs/path\n"
                   "      container: relative/path\n")
    vs = check_org("o")
    assert any(v.rule == "policy-schema" for v in vs), vs
    assert not any(v.rule == "policy-sections" for v in vs)


def test_rule4_tool_resolution_for_agent(fake_tree):
    """Tool resolution reads from SUBAGENT['tools'] — a bare slug that
    resolves to pux_sandbox_<name> is checked against the static surface."""
    add_org, add_agent = fake_tree
    add_agent("a", tools=["python"])
    add_org("o", agents=["a"])
    vs = check_org("o")
    # python -> pux_sandbox_python -> in SPECIALIST_TOOL_NAMES -> green
    assert not any(v.rule == "tool-resolves" for v in vs)


def test_required_agent_keys():
    """The required-keys set is exactly name + description + system_prompt."""
    assert _REQUIRED_AGENT_KEYS == {"name", "description", "system_prompt"}


# --- rule 8 — skill hygiene (well-formedness + global root scan) ---------

def _write_skill(root: Path, slug: str, *, name: str | None = None,
                 desc: str = "does a thing", body: str = "# body\n") -> Path:
    """Write a (by default well-formed) skill at ``<root>/<slug>/SKILL.md`` and
    return its dir. ``name`` defaults to ``slug`` (spec: they must match)."""
    d = root / slug
    d.mkdir(parents=True, exist_ok=True)
    (d / "SKILL.md").write_text(
        f"---\nname: {name if name is not None else slug}\n"
        f"description: {desc}\n---\n\n{body}")
    return d


def test_skill_well_formed_missing_skill_md(tmp_path):
    """A skill dir without SKILL.md is not a skill."""
    from pux_harness.agent.contract import _check_skill_dir
    (tmp_path / "ghost").mkdir()
    vs = _check_skill_dir(tmp_path / "ghost")
    assert [v.rule for v in vs] == ["skill-well-formed"]
    assert "missing SKILL.md" in vs[0].message


def test_skill_well_formed_name_must_match_dir(tmp_path):
    from pux_harness.agent.contract import _check_skill_dir
    _write_skill(tmp_path, "real-name", name="wrong-name")
    vs = _check_skill_dir(tmp_path / "real-name")
    assert any("must equal the dir name" in v.message for v in vs), vs


def test_skill_well_formed_name_must_be_kebab(tmp_path):
    from pux_harness.agent.contract import _check_skill_dir
    _write_skill(tmp_path, "Bad_Name", name="Bad_Name")  # caps + underscore
    vs = _check_skill_dir(tmp_path / "Bad_Name")
    assert any("kebab-case" in v.message for v in vs), vs


def test_skill_well_formed_description_required(tmp_path):
    from pux_harness.agent.contract import _check_skill_dir
    d = tmp_path / "no-desc"
    d.mkdir()
    (d / "SKILL.md").write_text(
        "---\nname: no-desc\ndescription:\n---\n\n# x\n")
    vs = _check_skill_dir(d)
    assert any("description" in v.message for v in vs), vs


def test_skill_well_formed_unparseable_frontmatter(tmp_path):
    """A colon-space in an unquoted scalar fails YAML parsing — caught here
    as a skill-well-formed error, not mid-run (the game-studio regression)."""
    from pux_harness.agent.contract import _check_skill_dir
    d = tmp_path / "broken"
    d.mkdir()
    (d / "SKILL.md").write_text(
        "---\nname: broken\ndescription: bad: value\n---\n\n# x\n")
    vs = _check_skill_dir(d)
    assert [v.rule for v in vs] == ["skill-well-formed"]
    assert "parse" in vs[0].message


def test_skill_well_formed_clean(tmp_path):
    from pux_harness.agent.contract import _check_skill_dir
    _write_skill(tmp_path, "clean-skill", desc="does the thing")
    assert _check_skill_dir(tmp_path / "clean-skill") == []


def test_check_skill_roots_flags_loose_md(fake_tree, monkeypatch):
    """A loose .md directly under a skills root warns — it's invisible to
    SkillsMiddleware (the stranded-playbook regression)."""
    monkeypatch.setattr(contract, "PROJECT_ROOT", contract._orgs_dir().parent)
    root = contract._orgs_dir() / "_shared" / "skills"
    _write_skill(root, "good-one")
    (root / "STRAY_PLAYBOOK.md").write_text("# a loose playbook\n")
    vs = contract.check_skill_roots()
    assert ("skill-dir-not-loose", "warn") in {
        (v.rule, v.severity) for v in vs}, vs
    # the well-formed sibling is NOT flagged
    assert not any(v.rule == "skill-well-formed" for v in vs), vs


def test_check_skill_roots_flags_malformed_skill(fake_tree, monkeypatch):
    """A malformed skill anywhere under a root surfaces as skill-well-formed."""
    monkeypatch.setattr(contract, "PROJECT_ROOT", contract._orgs_dir().parent)
    root = contract._orgs_dir() / "_shared" / "skills"
    _write_skill(root, "mismatch", name="not-the-dir-name")  # name mismatch
    vs = contract.check_skill_roots()
    assert any(v.rule == "skill-well-formed" for v in vs), vs


def test_check_skill_roots_clean_on_real_repo():
    """The shipped repo: every SKILL.md well-formed, no loose .md under any
    skills root. The regression guard for the skills reorg."""
    assert contract.check_skill_roots() == [], contract.check_skill_roots()


def test_no_legacy_agent_py_on_real_repo():
    """No .py agents ship + every agent .md carries frontmatter (name +
    description) + a non-empty body — the FLIPPED tripwire (Phase 15). The
    old ``.pi/agents/<slug>.py`` SUBAGENT-dict form is permanently forbidden."""
    vs = [v for v in check_harness()
          if v.rule == "no-legacy-agent-py"]
    assert vs == [], vs


def test_no_legacy_org_roster_on_real_repo():
    """No orgs/*/AGENTS.md carries an agents: key — roster in org.yaml."""
    for org in discover_orgs():
        vs = check_org(org)
        roster_vs = [v for v in vs if v.rule == "no-legacy-org-roster"]
        assert roster_vs == [], f"{org}: {roster_vs}"


def test_no_legacy_sandbox_artifacts_rejected(fake_tree):
    """A bootstrap.sh / docker-compose.yml under any org is a HARD contract
    failure — the harness owns the full sandbox lifecycle now (no bash/compose
    shadow). Mirrors no-legacy-org-roster / no-legacy-agent-py."""
    add_org, _ = fake_tree
    add_org("badorg")
    (contract._orgs_dir() / "badorg" / "bootstrap.sh").write_text("#!/bin/sh\n")
    (contract._orgs_dir() / "badorg" / "docker-compose.yml").write_text("services: {}\n")
    vs = check_harness()
    art_vs = [v for v in vs if v.rule == "no-legacy-sandbox-artifacts"]
    assert len(art_vs) == 2, vs
    msgs = "\n".join(v.message for v in art_vs)
    assert "bootstrap.sh" in msgs and "docker-compose.yml" in msgs


def test_no_legacy_sandbox_artifacts_on_real_repo():
    """No orgs/*/{bootstrap.sh,docker-compose.yml,docker-compose.override.yml}
    ships — the shadow lifecycle is gone (Phase 13)."""
    vs = [v for v in check_harness()
          if v.rule == "no-legacy-sandbox-artifacts"]
    assert vs == [], vs


def test_host_setup_validator_missing_helper(fake_tree):
    """A host_setup hook whose helper_script doesn't resolve under the project
    root fails --check-contract (offline) before Docker is ever touched."""
    add_org, _ = fake_tree
    add_org("cookbook", policy=(
        "host_setup:\n"
        "  - name: bad\n"
        "    helper_script: orgs/_shared/sandbox/does_not_exist.py\n"
        "    python_deps: [foo]\n"
        "    exports:\n"
        "      OUT: stdout\n"))
    vs = check_org("cookbook")
    assert any(v.rule == "host-setup-helper-missing" for v in vs), vs


def test_build_validator_missing_dockerfile(fake_tree):
    """A sandbox.build whose dockerfile doesn't exist fails --check-contract."""
    add_org, _ = fake_tree
    add_org("cookbook", policy=(
        "sandbox:\n"
        "  image: foo:latest\n"
        "  build:\n"
        "    dockerfile: orgs/cookbook/Dockerfile\n"
        "    context: orgs/cookbook\n"))
    vs = check_org("cookbook")
    assert any(v.rule == "sandbox-build-shape" for v in vs), vs


def test_jobs_validator_missing_script(fake_tree, tmp_path):
    """A job whose script doesn't exist fails --check-contract."""
    add_org, _ = fake_tree
    add_org("cookbook", policy=(
        "jobs:\n"
        "  - name: diarize\n"
        "    script: orgs/cookbook/sandbox/diarize.py\n"
        "    timeout: 3600\n"))
    vs = check_org("cookbook")
    assert any(v.rule == "jobs-script-missing" for v in vs), vs


def test_jobs_validator_missing_name(fake_tree, tmp_path):
    """A job without a name fails --check-contract."""
    add_org, _ = fake_tree
    # Create the script so it doesn't fail for missing script
    script_dir = tmp_path / "orgs" / "cookbook" / "sandbox"
    script_dir.mkdir(parents=True)
    (script_dir / "diarize.py").write_text("# fake")
    add_org("cookbook", policy=(
        "jobs:\n"
        "  - script: orgs/cookbook/sandbox/diarize.py\n"
        "    timeout: 3600\n"))
    vs = check_org("cookbook")
    assert any(v.rule == "jobs-shape" and "missing 'name'" in v.message for v in vs), vs


def test_jobs_validator_duplicate_names(fake_tree, tmp_path):
    """Duplicate job names fail --check-contract."""
    add_org, _ = fake_tree
    script_dir = tmp_path / "orgs" / "cookbook" / "sandbox"
    script_dir.mkdir(parents=True)
    (script_dir / "a.py").write_text("# fake")
    (script_dir / "b.py").write_text("# fake")
    add_org("cookbook", policy=(
        "jobs:\n"
        "  - name: diarize\n"
        "    script: orgs/cookbook/sandbox/a.py\n"
        "  - name: diarize\n"
        "    script: orgs/cookbook/sandbox/b.py\n"))
    vs = check_org("cookbook")
    assert any(v.rule == "jobs-shape" and "duplicate" in v.message for v in vs), vs


def test_jobs_validator_negative_timeout(fake_tree, tmp_path):
    """Negative timeout fails --check-contract."""
    add_org, _ = fake_tree
    script_dir = tmp_path / "orgs" / "cookbook" / "sandbox"
    script_dir.mkdir(parents=True)
    (script_dir / "a.py").write_text("# fake")
    add_org("cookbook", policy=(
        "jobs:\n"
        "  - name: run\n"
        "    script: orgs/cookbook/sandbox/a.py\n"
        "    timeout: -5\n"))
    vs = check_org("cookbook")
    assert any(v.rule == "jobs-shape" and "timeout" in v.message for v in vs), vs


def test_jobs_validator_valid_spec_passes(fake_tree, tmp_path):
    """A valid job spec passes --check-contract."""
    add_org, _ = fake_tree
    script_dir = tmp_path / "orgs" / "cookbook" / "sandbox"
    script_dir.mkdir(parents=True)
    (script_dir / "diarize.py").write_text("# fake")
    add_org("cookbook", policy=(
        "jobs:\n"
        "  - name: diarize\n"
        "    script: orgs/cookbook/sandbox/diarize.py\n"
        "    timeout: 3600\n"
        "    description: \"Diarize audio files\"\n"))
    vs = check_org("cookbook")
    job_vs = [v for v in vs if v.rule.startswith("jobs")]
    assert not job_vs, f"unexpected job violations: {job_vs}"
