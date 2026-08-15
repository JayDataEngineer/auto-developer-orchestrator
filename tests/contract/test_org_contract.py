"""Green gate for the declarative org contract.

Two tiers, mirroring ``audit.py``:

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

from pux_harness.agent import orgs
from pux_harness.agent.orgs import discover_orgs
from pux_harness.validation import audit as ov
check_org = ov.audit_org
from pux_harness.validation.schemas import KNOWN_POLICY_SECTIONS, REQUIRED_AGENT_KEYS
from pux_harness.sandbox.tools import SPECIALIST_TOOL_NAMES
# ``NATIVE_FS_TOOLS`` lives canonically in the tools registry — ``audit.py``
# re-exported it once as a dead convenience surface and that was removed (the
# daemon-recovery sweep). Import it from its home, not the contract module.
from pux_harness.sandbox.tools.registry import NATIVE_FS_TOOLS


# --- the green gate ------------------------------------------------------

EXPECTED_ORGS = {
    "_demo", "browser-agent", "coder", "deep-research-engine", "fs-explorer",
    "game-studio", "general", "invest", "media-studio", "orchestrator",
    "social-media-pipeline", "telegram-agent", "twitter-agent",
    "video-production", "web-search",
}


def test_discover_orgs_finds_all_specialists():
    found = set(discover_orgs())
    assert found == EXPECTED_ORGS, f"missing={EXPECTED_ORGS - found} extra={found - EXPECTED_ORGS}"


@pytest.mark.parametrize("org", sorted(EXPECTED_ORGS))
def test_org_bundle_is_green(org):
    """Structural green: AGENTS.md, org.yaml, slug resolution, policy."""
    violations = check_org(org)
    errors = [v for v in violations if v.severity == "error"]
    assert errors == [], f"{org}: {errors}"


def test_every_org_has_a_forcing_task():
    """The in-process runner (``pux direct --org <name>``) drives a delegation-
    forcing task per org. Every discovered org must have a DEFAULT_TASKS entry —
    a missing entry would make ``--org <name>`` fail with a KeyError instead of
    running. (All 10 orgs ported to RUN on deepagents.)"""
    from tests.integration.default_tasks import DEFAULT_TASKS

    missing = set(discover_orgs()) - set(DEFAULT_TASKS)
    assert not missing, f"orgs without a DEFAULT_TASKS forcing task: {missing}"


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

    Both ``ov._orgs_dir`` AND ``orgs._orgs_dir`` are patched: rule-3
    resolution delegates into ``orgs._load_agent_spec`` (which reads
    ``orgs._orgs_dir`` via ``_agent_search_dirs``), while audit's own
    path helpers read ``ov._orgs_dir`` — patching only one lets the two
    halves see different trees. ``_shared/{agents,skills}`` are pre-created
    (the shared agent search dir + the default skills root)."""
    (tmp_path / "orgs").mkdir()
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    (tmp_path / "orgs" / "_shared" / "skills").mkdir(parents=True)
    monkeypatch.setattr(ov, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
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
                policy: str | None = None, org_yaml_extra: str = "") -> None:
        """Write org.yaml (roster) + prose-only AGENTS.md (+ optional policy +
        extra org.yaml lines, e.g. ``roster_deny:``)."""
        d = tmp_path / "orgs" / org
        d.mkdir(exist_ok=True)
        if agents is not None:
            (d / "org.yaml").write_text(
                f"agents: [{', '.join(agents)}]\n" + org_yaml_extra)
        (d / "AGENTS.md").write_text(body)
        if policy is not None:
            (d / "policy.yaml").write_text(policy)

    return add_org, add_agent


def test_rule1_missing_agents_md(fake_tree):
    add_org, _ = fake_tree
    add_org("empty")  # AGENTS.md created, but test a dir without it:
    (ov._orgs_dir() / "empty" / "AGENTS.md").unlink()
    vs = check_org("empty")
    assert any(v.rule == "org-agents-md" for v in vs)


def test_rule2_legacy_frontmatter_rejected(fake_tree):
    """AGENTS.md with any frontmatter is rejected by the no-legacy-org-roster
    tripwire — the roster must live in org.yaml."""
    add_org, _ = fake_tree
    add_org("badorg")
    (ov._orgs_dir() / "badorg" / "AGENTS.md").write_text(
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
    agents_dir = ov._orgs_dir() / "o" / "agents"
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
    agents_dir = ov._orgs_dir() / "o" / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    # colon-space in an unquoted scalar is a YAML parse error
    (agents_dir / "broken.md").write_text(
        "---\nname: broken\ndescription: bad: value\n---\n\nprose\n")
    add_org("o", agents=["broken"])
    vs = check_org("o")
    assert any(v.rule == "agent-resolves" for v in vs), vs


def test_rule3_unknown_agent_key_warned(fake_tree):
    """A SUBAGENT dict with unexpected keys still loads (extra keys are
    harmless) — but REQUIRED_AGENT_KEYS must be present."""
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
        "jobs", "tool_servers", "protocols", "tool_surface",
    }


def test_rule5_policy_unknown_protocol_rejected(fake_tree):
    """A ``protocols:`` entry that isn't a known surface (acp/agui) is a typo —
    the contract must flag it rather than silently ignore it (a misspelled
    surface would otherwise mean the org isn't served where the operator
    thinks). Proves the ``_validate_protocols`` rule + its ``protocols`` rule
    string end-to-end through ``check_org``."""
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="protocols:\n  - acp\n  - telepathy\n")
    vs = check_org("o")
    proto_vs = [v for v in vs if v.rule == "protocols"]
    assert len(proto_vs) == 1, vs
    assert "telepathy" in proto_vs[0].message
    # acp is valid — only the bogus entry is flagged. One violation (not two)
    # is the proof acp passed; the message also lists the allowed set, so we
    # don't substring-match "acp" there.


def test_rule5_policy_known_protocols_ok(fake_tree):
    """The known surface set passes the contract (no protocols violation)."""
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="protocols:\n  - agui\n")
    assert check_org("o") == []


def test_rule5_policy_unknown_protocol_caught(fake_tree):
    """A protocol outside KNOWN_PROTOCOLS (e.g. the retired 'acp' surface)
    fails loud — narrowing is explicit, typos can't silently widen."""
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="protocols:\n  - acp\n")
    vs = check_org("o")
    assert any(v.rule == "protocols" and "acp" in v.message for v in vs), vs


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
    """Deep schema: a known section that is NOT a mapping passes the
    shallow section check (the key is known) but fails the real policy engine —
    ``policy.load`` raises 'section must be a mapping'. Proves the load layer."""
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="sandbox: not-a-mapping\n")
    vs = check_org("o")
    assert any(v.rule == "policy-schema" for v in vs), vs
    # and the shallow section check must NOT fire — the key IS known
    assert not any(v.rule == "policy-sections" for v in vs), vs


def test_rule5_policy_bad_section_type_caught(fake_tree):
    """Deep schema: a scalar ``jobs:`` section parses as valid YAML + a known
    section key, so both shallow checks pass — only ``policy.load`` catches it
    ('section must be a list'). The workspace-mounts variant retired with the
    deleted mounts surface. No network — safe offline."""
    add_org, add_agent = fake_tree
    add_agent("r")
    add_org("o", agents=["r"], policy="jobs: not-a-list\n")
    vs = check_org("o")
    assert any(v.rule == "policy-schema" for v in vs), vs
    assert not any(v.rule == "policy-sections" for v in vs), vs



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
    assert REQUIRED_AGENT_KEYS == {"name", "description", "system_prompt"}


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
    from pux_harness.validation.audit import check_skill_dir as _check_skill_dir
    (tmp_path / "ghost").mkdir()
    vs = _check_skill_dir(tmp_path / "ghost")
    assert [v.rule for v in vs] == ["skill-well-formed"]
    assert "missing SKILL.md" in vs[0].message


def test_skill_well_formed_name_must_match_dir(tmp_path):
    from pux_harness.validation.audit import check_skill_dir as _check_skill_dir
    _write_skill(tmp_path, "real-name", name="wrong-name")
    vs = _check_skill_dir(tmp_path / "real-name")
    assert any("must equal the dir name" in v.message for v in vs), vs


def test_skill_well_formed_name_must_be_kebab(tmp_path):
    from pux_harness.validation.audit import check_skill_dir as _check_skill_dir
    _write_skill(tmp_path, "Bad_Name", name="Bad_Name")  # caps + underscore
    vs = _check_skill_dir(tmp_path / "Bad_Name")
    assert any("kebab-case" in v.message for v in vs), vs


def test_skill_well_formed_description_required(tmp_path):
    from pux_harness.validation.audit import check_skill_dir as _check_skill_dir
    d = tmp_path / "no-desc"
    d.mkdir()
    (d / "SKILL.md").write_text(
        "---\nname: no-desc\ndescription:\n---\n\n# x\n")
    vs = _check_skill_dir(d)
    assert any("description" in v.message for v in vs), vs


def test_skill_well_formed_unparseable_frontmatter(tmp_path):
    """A colon-space in an unquoted scalar fails YAML parsing — caught here
    as a skill-well-formed error, not mid-run (the game-studio regression)."""
    from pux_harness.validation.audit import check_skill_dir as _check_skill_dir
    d = tmp_path / "broken"
    d.mkdir()
    (d / "SKILL.md").write_text(
        "---\nname: broken\ndescription: bad: value\n---\n\n# x\n")
    vs = _check_skill_dir(d)
    assert [v.rule for v in vs] == ["skill-well-formed"]
    assert "parse" in vs[0].message


def test_no_legacy_org_roster_on_real_repo():
    """No orgs/*/AGENTS.md carries an agents: key — roster in org.yaml."""
    for org in discover_orgs():
        vs = check_org(org)
        roster_vs = [v for v in vs if v.rule == "no-legacy-org-roster"]
        assert roster_vs == [], f"{org}: {roster_vs}"


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
    assert any(v.rule == "host-setup-shape" and "helper_script" in v.message
               for v in vs), vs


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
    assert any(v.rule == "jobs-shape" and "script" in v.message
               for v in vs), vs


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
    assert any(v.rule == "jobs-shape" and "name" in v.message
               for v in vs), vs


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


# --- Browser agent + per-org profile ---------------------------------------

def test_browser_agent_resolves_from_shared_on_real_repo():
    """The shipped browser agent (orgs/_shared/agents/browser.md) is rostered by
    `general` + `_demo`, resolves from `_shared`, and its full whitelist passes
    rule 4 (every slug is a registered specialist or native fs tool)."""
    for org in ("general", "_demo"):
        vs = check_org(org)
        # No structural violations (the green gate already parametrizes this,
        # but this pins the browser addition explicitly).
        assert vs == [], f"{org}: {vs}"
        roster = orgs.org_agent_slugs(org)
        assert "browser" in roster, f"{org} does not roster browser"


def test_browser_family_is_mcp_only():
    """The browser family MIGRATED off the specialist registry onto the
    in-container ``sandbox_browser`` MCP server. No ``pux_sandbox_browser_*``
    specialist may reappear (rule 4b would accept one silently — the
    capability's home is MCP now), and every shipped agent that grants
    ``sandbox_browser`` sits in an org that DECLARES it (the two-level gate)."""
    import yaml as _yaml
    from pathlib import Path as _Path
    repo = _Path(__file__).resolve().parents[2]
    assert not any(n.startswith("pux_sandbox_browser_")
                   for n in SPECIALIST_TOOL_NAMES)
    # every granting agent's org declares the server (or extends such an org)
    for org_dir in sorted((repo / "orgs").glob("*/")) + \
            sorted((repo / "orgs" / "specialists").glob("*/")):
        if not (org_dir / "org.yaml").is_file():
            continue
        cfg = _yaml.safe_load((org_dir / "org.yaml").read_text()) or {}
        caps = {c.get("ref") for c in (cfg.get("capabilities") or [])
                if isinstance(c, dict)}
        roster = cfg.get("agents") or []
        grants = False
        for slug in roster:
            for f in (org_dir / "agents" / f"{slug}.md",
                      repo / "orgs" / "_shared" / "agents" / f"{slug}.md"):
                if f.is_file() and "ref: sandbox_browser" in f.read_text():
                    grants = True
        if grants:
            assert "sandbox_browser" in caps, (
                f"{org_dir.name} rosters a sandbox_browser agent but does not "
                f"declare the server in org.yaml capabilities")


# --- explorer agent (shared, rostered by general) --------------------------

def test_explorer_agent_resolves_from_shared_on_real_repo():
    """The shipped explorer agent (orgs/_shared/agents/explorer.md) is rostered
    by `general`, resolves from `_shared`, and its contract is green (the
    capabilities: sugar desugars to a registered `python` tool + a valid
    skills root)."""
    from pux_harness.kit._paths import project_root
    from pux_harness.kit.loaders import _load_agent_spec

    vs = check_org("general")
    assert vs == [], f"general: {vs}"
    roster = orgs.org_agent_slugs("general")
    assert "explorer" in roster, "general does not roster explorer"
    # general's own agents (researcher, browser) come before the shared explorer
    assert roster.index("explorer") > roster.index("researcher")
    assert roster.index("explorer") > roster.index("browser")

    spec = _load_agent_spec("explorer", "general", project_root())
    assert spec is not None
    assert "explorer" in spec.get("name", "")
    assert "context" in spec.get("description", "").lower()


def test_explorer_capabilities_desugar_to_tools_and_skills():
    """explorer.md declares a unified `capabilities:` block (CU-3 sugar):
    kind: tool -> python, kind: skill -> orgs/_shared/skills. The loader must
    desugar it into the legacy `tools:` / `skills:` keys so the contract's
    rule-4 + skill-resolution passes see them."""
    from pux_harness.kit._paths import project_root
    from pux_harness.kit.loaders import _load_agent_spec

    spec = _load_agent_spec("explorer", "general", project_root())
    # python -> pux_sandbox_python resolves as a registered specialist
    assert spec.get("tools") == ["python"], spec.get("tools")
    # the shared skills root resolves as a skills directory
    assert spec.get("skills") == ["orgs/_shared/skills"], spec.get("skills")


def test_profile_yaml_valid_no_violation(fake_tree):
    """A well-formed optional profile.yaml produces no contract violation."""
    add_org, _ = fake_tree
    add_org("o")
    (ov._orgs_dir() / "o" / "profile.yaml").write_text(
        "system_prompt_suffix: 'be concise'\n"
        "tool_description_overrides:\n"
        "  pux_sandbox_python: 'run python code'\n"
        "excluded_tools: []\n"
    )
    vs = check_org("o")
    assert not any(v.rule == "profile-schema" for v in vs), vs


def test_profile_yaml_unknown_key_reports_violation(fake_tree):
    """An unknown key in profile.yaml fails --check-contract (profile-schema)."""
    add_org, _ = fake_tree
    add_org("o")
    (ov._orgs_dir() / "o" / "profile.yaml").write_text(
        "bogus_field: 1\n"
    )
    vs = check_org("o")
    assert any(v.rule == "profile-schema" and "Unknown keys" in v.message
               for v in vs), vs


def test_profile_yaml_non_mapping_reports_violation(fake_tree):
    """A non-mapping top level in profile.yaml fails --check-contract."""
    add_org, _ = fake_tree
    add_org("o")
    (ov._orgs_dir() / "o" / "profile.yaml").write_text(
        "- just\n- a\n- list\n"
    )
    vs = check_org("o")
    assert any(v.rule == "profile-schema" and "mapping" in v.message
               for v in vs), vs


# --- coder roster redesign + no-general tripwire -------------------------

def test_coder_roster_on_real_repo():
    """coder's shipped roster is exactly the three specialists — explorer
    (recon), code-worker (mechanical one-shot execution), web-agent (e2e
    verification). The CTO does all the thinking; these three are the only
    delegation targets."""
    slugs = orgs.org_agent_slugs("coder")
    assert slugs == ["coder-explorer", "code-worker", "web-agent"], slugs


def test_coder_no_general_subagent_tripwire_on_real_repo():
    """The permanent tripwire: coder must NOT roster a generic catch-all
    subagent (general / general-purpose / researcher) — that would let the CTO
    delegate the DESIGN itself, the exact anti-pattern the roster prevents.
    The shipped roster is clean."""
    vs = check_org("coder")
    assert not any(v.rule == "roster-deny-enforced" for v in vs), vs


_CODER_DENY = ("roster_deny: [general, general-purpose, researcher]\n"
               "inherit_roster: false\n")


@pytest.mark.parametrize("forbidden_slug", ["general", "general-purpose", "researcher"])
def test_coder_no_general_subagent_tripwire_fires(fake_tree, forbidden_slug):
    """Rostering any ``roster_deny:`` slug is a HARD contract failure
    (``roster-deny-enforced``) — the gate blocks the commit, not a silent
    drift (no-legacy-left-behind). The rule is DATA-DRIVEN: any org declares
    the same focus-CTO shape via ``roster_deny:`` (coder is the shipped
    example)."""
    add_org, add_agent = fake_tree
    add_agent(forbidden_slug, org="coder")
    add_org("coder", agents=[forbidden_slug], org_yaml_extra=_CODER_DENY)
    vs = check_org("coder")
    rule_vs = [v for v in vs if v.rule == "roster-deny-enforced"]
    assert len(rule_vs) == 1, vs
    assert forbidden_slug in rule_vs[0].message


def test_coder_tripwire_does_not_fire_for_other_orgs(fake_tree):
    """The tripwire is coder-scoped — another org rostering ``researcher``
    (the shared general-purpose investigator) is fine. Only coder's CTO
    refuses a generic subagent."""
    add_org, add_agent = fake_tree
    add_agent("researcher", org="invest")
    add_org("invest", agents=["researcher"])
    vs = check_org("invest")
    assert not any(v.rule == "roster-deny-enforced" for v in vs), vs


# --- coder-disables-general-purpose (sibling tripwire) -------------------

def test_coder_disables_general_purpose_on_real_repo():
    """Sibling tripwire (defense in depth, NEW code path): coder's
    profile.yaml MUST declare ``general_purpose_subagent: {enabled: false}``.
    The roster rule above reads org.yaml and so NEVER sees the general-purpose
    slot deepagents auto-adds to every graph (graph.py:716-717); this rule reads
    profile.yaml and closes that gap. The shipped repo is clean."""
    vs = check_org("coder")
    assert not any(v.rule == "coder-disables-general-purpose" for v in vs), vs


def test_coder_disables_general_purpose_fires_when_absent(fake_tree):
    """A coder whose profile.yaml OMITS the field trips the rule — deepagents
    would otherwise auto-add a heavy generic worker the roster rule can't see.
    Only the explicit neuter (``enabled: false``) satisfies coder's intent."""
    add_org, add_agent = fake_tree
    add_agent("code-worker", org="coder")
    add_org("coder", agents=["code-worker"], org_yaml_extra=_CODER_DENY)
    # profile.yaml present but WITHOUT general_purpose_subagent.
    (ov._orgs_dir() / "coder" / "profile.yaml").write_text(
        "system_prompt_suffix: |\n  be terse.\n")
    vs = check_org("coder")
    rule_vs = [v for v in vs if v.rule == "roster-deny-disables-general-purpose"]
    assert len(rule_vs) == 1, vs
    assert "general_purpose_subagent" in rule_vs[0].message


def test_coder_disables_general_purpose_fires_when_enabled_true(fake_tree):
    """A coder that EXPLICITLY enables the GP also trips the rule — only
    ``enabled: false`` (the neuter spec) satisfies the no-catch-all intent."""
    add_org, add_agent = fake_tree
    add_agent("code-worker", org="coder")
    add_org("coder", agents=["code-worker"], org_yaml_extra=_CODER_DENY)
    (ov._orgs_dir() / "coder" / "profile.yaml").write_text(
        "general_purpose_subagent:\n  enabled: true\n")
    vs = check_org("coder")
    rule_vs = [v for v in vs if v.rule == "roster-deny-disables-general-purpose"]
    assert len(rule_vs) == 1, vs

def test_coder_specialists_resolve_on_worker_role(monkeypatch):
    """code-worker + web-agent have no frontmatter ``model:`` → both resolve on
    the ``worker`` role (cheap mimo, the "small one-shot worker" the user asked
    for). Drives the REAL load_subagents('coder') — no Docker, no tokens.
    Fake key only (get_model reads it at construction, never sends a request).
    web-agent's browser surface now arrives via the sandbox_browser MCP seam."""
    from pux_harness.agent import orgs as orgs_mod
    from pux_harness.sandbox.tools import SPECIALIST_TOOL_NAMES

    # Stand-in tools covering the WHOLE specialist registry — load_subagents
    # only needs .name to resolve each agent's tools whitelist.
    class _T:
        def __init__(self, name):
            self.name = name
    specialists = [_T(n) for n in SPECIALIST_TOOL_NAMES]
    mcp_tools = [_T(f"mcp__sandbox_browser__{n}")
                 for n in ("browser_navigate", "browser_evaluate",
                           "browser_screenshot")]
    monkeypatch.setenv("OPENAI_API_KEY", "sk-fake")
    subs = orgs_mod.load_subagents(
        "coder", specialists,
        subagent_middleware=[], retrieval_tools=[], mcp_tools=mcp_tools,
    )
    by_name = {s["name"]: s for s in subs}
    assert set(by_name) == {"coder-explorer", "code-worker", "web-agent"}, \
        set(by_name)
    # worker role resolves to a concrete model id (mimo-v2.5 default); the
    # resolved value is NOT a hardcoded literal — it comes from models.yaml.
    for slug in ("code-worker", "web-agent"):
        assert by_name[slug]["model"] is not None, f"{slug} model unresolved"
    # web-agent's MCP surface binds (the migrated browser family).
    web_tools = {t.name for t in by_name["web-agent"]["tools"]}
    assert "mcp__sandbox_browser__browser_navigate" in web_tools

# --- subagent `extends:` + the legacy `subagents:`-block fold ----------------

def test_no_legacy_subagents_block_on_real_repo():
    """No shipped org's profile.yaml carries a top-level ``subagents:`` key —
    the legacy second partial-override surface is gone (folded into per-agent
    ``extends:`` + delta frontmatter). The permanent tripwire keeps it gone."""
    for org in discover_orgs():
        vs = check_org(org)
        assert not any(v.rule == "no-legacy-subagents-block" for v in vs), \
            f"{org}: {vs}"


def test_no_legacy_subagents_block_fires(fake_tree):
    """A profile.yaml with a top-level ``subagents:`` block is a HARD contract
    failure pointing at the replacement (``extends:`` + delta fields) — the
    no-legacy-left-behind permanent gate. Mirrors the no-legacy-org-roster /
    no-legacy-sandbox-artifacts provocation shape."""
    add_org, _ = fake_tree
    add_org("o")
    (ov._orgs_dir() / "o" / "profile.yaml").write_text(
        "subagents:\n"
        "  some-slug:\n"
        "    system_prompt_suffix: be terse\n"
    )
    vs = check_org("o")
    rule_vs = [v for v in vs if v.rule == "no-legacy-subagents-block"]
    assert len(rule_vs) == 1, vs
    assert "extends" in rule_vs[0].message
    assert "tools_add" in rule_vs[0].message  # points at the delta vocabulary


def test_agent_extends_resolvable_fires(fake_tree):
    """An agent whose ``extends:`` references a non-existent agent fires
    ``agent-extends-resolvable`` — the dedicated rule (not a generic
    agent-resolves), with the chain in the message."""
    add_org, _ = fake_tree
    agents_dir = ov._orgs_dir() / "o" / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    (agents_dir / "lonely.md").write_text(
        "---\nname: lonely\nextends: ghost\n---\n\nbody\n")
    add_org("o", agents=["lonely"])
    vs = check_org("o")
    rule_vs = [v for v in vs if v.rule == "agent-extends-resolvable"]
    assert len(rule_vs) == 1, vs
    assert "ghost" in rule_vs[0].message


def test_agent_extends_acyclic_fires(fake_tree):
    """An ``extends:`` cycle (x -> y -> x) fires ``agent-extends-acyclic``.
    The roster lists only the entry point; ``y`` exists on disk to close the
    cycle but is walked via the chain, not the roster."""
    add_org, _ = fake_tree
    agents_dir = ov._orgs_dir() / "o" / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    (agents_dir / "x.md").write_text("---\nname: x\nextends: y\n---\n\nbody\n")
    (agents_dir / "y.md").write_text("---\nname: y\nextends: x\n---\n\nbody\n")
    add_org("o", agents=["x"])
    vs = check_org("o")
    rule_vs = [v for v in vs if v.rule == "agent-extends-acyclic"]
    assert len(rule_vs) == 1, vs
    assert "cycle" in rule_vs[0].message
    assert "x" in rule_vs[0].message and "y" in rule_vs[0].message


def test_agent_extends_clean_on_real_repo():
    """No shipped agent has a broken ``extends:`` chain (unresolvable or cyclic)
    — the two rules are green across every org."""
    for org in discover_orgs():
        vs = check_org(org)
        bad = [v for v in vs
               if v.rule in ("agent-extends-resolvable", "agent-extends-acyclic")]
        assert bad == [], f"{org}: {bad}"
