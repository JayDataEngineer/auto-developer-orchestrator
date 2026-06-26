#!/usr/bin/env python3
"""Self-tests for scripts/org_build.py.

Run::

    uv run --with jinja2,pytest scripts/test_org_build.py
    # or
    uv run --with jinja2,pytest pytest scripts/test_org_build.py

Covers:
  - protected-file safety (refuses to overwrite hand-written files)
  - generated-file overwrite (header'd files ARE overwritten)
  - --check drift detection
  - rendered YAML parses cleanly
  - org.toml validation catches bad input
"""

from __future__ import annotations

import os
import shutil
import sys
import tempfile
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(REPO_ROOT))

from scripts import org_build  # noqa: E402  (after sys.path insert)


# --------------------------------------------------------------------------- #
# Fixtures
# --------------------------------------------------------------------------- #

MINIMAL_ORG_TOML = """\
name = "acme"
description = "Test org for renderer self-tests"

manifesto = "MANIFESTO.md"
staff_root = "roles"

[[roles]]
name = "worker"
description = "Worker — does work"
max_rounds = 10
model = "deepseek/deepseek-v4-flash"
imports = ["shell"]
"""


# A canonical [sandbox.bootstrap] block (TOML slice) included by every test
# fixture that declares tier='standard' or tier='custom-build'. The validator
# requires this block so the rendered bootstrap.sh is fully derived from data
# (see scripts/templates/org/bootstrap.sh.j2). Keep this in sync with the
# real orgs' bootstrap metadata.
STANDARD_BOOTSTRAP_TOML = """
[sandbox.bootstrap]
smoke_test_command = "python3 -c 'print(\\"ok\\")'"
smoke_test_description = "container up"

[[sandbox.bootstrap.hard_deps]]
name = "docker"
check = "command -v docker"
error_msg = "docker not found on PATH"
"""

STANDARD_BOOTSTRAP_TOML_CUSTOM_BUILD = """
[sandbox.bootstrap]
smoke_test_command = "python3 -c 'print(\\"ok\\")'"
smoke_test_description = "container up"

[[sandbox.bootstrap.hard_deps]]
name = "docker"
check = "command -v docker"
error_msg = "docker not found on PATH"
"""

# Dict form for the validator-level tests (avoids re-parsing TOML).
STANDARD_BOOTSTRAP_DICT = {
    "smoke_test_command": "python3 -c 'print(\"ok\")'",
    "smoke_test_description": "container up",
    "hard_deps": [
        {"name": "docker", "check": "command -v docker", "error_msg": "docker not found"},
    ],
}


@pytest.fixture
def tmp_org(tmp_path: Path) -> Path:
    """A minimal org tree at tmp_path/orgs/acme with org.toml + one role."""
    org = tmp_path / "orgs" / "acme"
    roles_dir = org / "roles" / "worker"
    roles_dir.mkdir(parents=True)
    (org / "org.toml").write_text(MINIMAL_ORG_TOML)
    (org / "MANIFESTO.md").write_text("# Acme\nHand-written.\n")
    (roles_dir / "prompt.md").write_text("You are a worker.")
    return org


@pytest.fixture
def rendered_org(tmp_org: Path) -> Path:
    """tmp_org after one render pass — config.yaml exists with the header."""
    env = org_build._make_env()
    org_build.render_org(tmp_org, env)
    return tmp_org


# --------------------------------------------------------------------------- #
# Protected-file safety
# --------------------------------------------------------------------------- #

def test_render_does_not_touch_freeform(tmp_org: Path) -> None:
    """Hand-written files (MANIFESTO.md, prompt.md) must not be touched."""
    manifest = tmp_org / "MANIFESTO.md"
    prompt = tmp_org / "roles" / "worker" / "prompt.md"

    manifest_before = manifest.read_text()
    prompt_before = prompt.read_text()

    env = org_build._make_env()
    org_build.render_org(tmp_org, env)

    assert manifest.read_text() == manifest_before
    assert prompt.read_text() == prompt_before


def test_render_refuses_to_clobber_hand_edited_config(tmp_org: Path) -> None:
    """If a config.yaml exists WITHOUT the AUTO-GENERATED header, refuse."""
    cfg = tmp_org / "roles" / "worker" / "config.yaml"
    cfg.parent.mkdir(parents=True, exist_ok=True)
    cfg.write_text("# Hand-edited\ndescription: custom\n")

    env = org_build._make_env()
    with pytest.raises(RuntimeError, match="Refusing to overwrite"):
        org_build.render_org(tmp_org, env)

    # File contents must be unchanged.
    assert "Hand-edited" in cfg.read_text()


def test_render_overwrites_generated_files(rendered_org: Path) -> None:
    """Files with the AUTO-GENERATED header are safe to overwrite."""
    cfg = rendered_org / "roles" / "worker" / "config.yaml"
    assert cfg.read_text().lstrip().startswith(org_build.GENERATED_HEADER)

    # Mutate max_rounds in org.toml, re-render, confirm the value propagates.
    toml = rendered_org / "org.toml"
    text = toml.read_text().replace("max_rounds = 10", "max_rounds = 99")
    toml.write_text(text)

    env = org_build._make_env()
    org_build.render_org(rendered_org, env)

    assert "max_rounds: 99" in cfg.read_text()


# --------------------------------------------------------------------------- #
# Drift detection
# --------------------------------------------------------------------------- #

def test_diff_trees_empty_when_in_sync(rendered_org: Path) -> None:
    """_diff_trees returns no diff when the tree matches the renderer output."""
    env = org_build._make_env()
    tmp = org_build._render_to_tmp(rendered_org, env)
    assert org_build._diff_trees(rendered_org, tmp) == []


def test_diff_trees_flags_field_mutation(rendered_org: Path) -> None:
    """_diff_trees returns a diff when a generated file has been mutated."""
    cfg = rendered_org / "roles" / "worker" / "config.yaml"
    text = cfg.read_text().replace("max_rounds: 10", "max_rounds: 999")
    cfg.write_text(text)

    env = org_build._make_env()
    tmp = org_build._render_to_tmp(rendered_org, env)
    diff = org_build._diff_trees(rendered_org, tmp)
    assert diff, "expected drift after mutating a generated file"
    assert any("max_rounds" in line for line in diff)


def test_diff_trees_flags_stale_files(rendered_org: Path) -> None:
    """_diff_trees flags generated files with no renderer source."""
    stale = rendered_org / "roles" / "ghost" / "config.yaml"
    stale.parent.mkdir(parents=True)
    stale.write_text(
        f"{org_build.GENERATED_HEADER}\ndescription: deleted-from-org.toml\n"
    )

    env = org_build._make_env()
    tmp = org_build._render_to_tmp(rendered_org, env)
    diff = org_build._diff_trees(rendered_org, tmp)
    assert any("stale" in line.lower() or "ghost" in line for line in diff), diff


def test_check_mode_cli_detects_drift(tmp_org: Path) -> None:
    """--check CLI exits non-zero on drift (exercises main() end-to-end)."""
    env = org_build._make_env()
    org_build.render_org(tmp_org, env)

    # Mutate a generated file.
    cfg = tmp_org / "roles" / "worker" / "config.yaml"
    cfg.write_text(cfg.read_text().replace("max_rounds: 10", "max_rounds: 999"))

    # Point the module-level ORGS_DIR at our tmp tree and run main().
    original = org_build.ORGS_DIR
    org_build.ORGS_DIR = tmp_org.parent
    try:
        rc = org_build.main(["--check", tmp_org.name])
    finally:
        org_build.ORGS_DIR = original
    assert rc != 0


def test_check_mode_cli_clean_when_in_sync(tmp_org: Path) -> None:
    """--check CLI exits 0 when in sync."""
    env = org_build._make_env()
    org_build.render_org(tmp_org, env)

    original = org_build.ORGS_DIR
    org_build.ORGS_DIR = tmp_org.parent
    try:
        rc = org_build.main(["--check", tmp_org.name])
    finally:
        org_build.ORGS_DIR = original
    assert rc == 0


# --------------------------------------------------------------------------- #
# YAML validity
# --------------------------------------------------------------------------- #

def test_rendered_yaml_parses(rendered_org: Path) -> None:
    """Every rendered .yaml file must be parseable YAML."""
    yaml = pytest.importorskip("yaml")
    for p in rendered_org.rglob("*.yaml"):
        data = yaml.safe_load(p.read_text())
        assert data is not None, f"{p} rendered to null"


def test_pux_yaml_has_required_fields(rendered_org: Path) -> None:
    """The generated pux.yaml must contain the kernel-required fields."""
    yaml = pytest.importorskip("yaml")
    data = yaml.safe_load((rendered_org / "pux.yaml").read_text())
    assert data["name"] == "acme"
    assert data["description"]
    assert data["staff_root"] == "roles"


def test_role_config_has_imports(rendered_org: Path) -> None:
    """The worker role's imports survive the render."""
    yaml = pytest.importorskip("yaml")
    data = yaml.safe_load(
        (rendered_org / "roles" / "worker" / "config.yaml").read_text()
    )
    assert data["imports"] == ["shell"]
    assert data["model"] == "deepseek/deepseek-v4-flash"


# --------------------------------------------------------------------------- #
# org.toml validation
# --------------------------------------------------------------------------- #

def test_validation_rejects_name_mismatch(tmp_path: Path) -> None:
    org = tmp_path / "acme"
    (org / "roles" / "w").mkdir(parents=True)
    (org / "roles" / "w" / "prompt.md").write_text("p")
    (org / "org.toml").write_text(
        'name = "wrong"\ndescription = "x"\nmanifesto = "M.md"\nstaff_root = "roles"\n'
    )
    (org / "M.md").write_text("m")

    errs = org_build.validate_org_data(
        {"name": "wrong", "description": "x"}, "acme", org
    )
    assert any("does not match directory" in e for e in errs)


def test_validation_rejects_missing_prompt(tmp_path: Path) -> None:
    org = tmp_path / "acme"
    (org / "roles" / "worker").mkdir(parents=True)
    # No prompt.md — should flag it.
    raw = {
        "name": "acme",
        "description": "x",
        "roles": [{"name": "worker", "description": "w"}],
    }
    errs = org_build.validate_org_data(raw, "acme", org)
    assert any("prompt.md" in e for e in errs)


def test_validation_rejects_missing_description(tmp_path: Path) -> None:
    errs = org_build.validate_org_data({"name": "x"}, "x", tmp_path)
    assert any("description" in e for e in errs)


# --------------------------------------------------------------------------- #
# Sandbox profile (K8s-style abstractions on Docker substrate)
# --------------------------------------------------------------------------- #

SANDBOX_ORG_TOML = """\
name = "acme"
description = "Sandbox-profile fixture"

manifesto = "MANIFESTO.md"
staff_root = "roles"

[sandbox]
image = "acme-sandbox:latest"
tier = "standard"
runtime_class = "gvisor"
warm_pool = 2
init_files = ["sandbox/run.py"]

[sandbox.env]
PUX_ORG_PATH = "/sandbox/workspace"

[sandbox.resources.requests]
cpu = "250m"
memory = "256Mi"

[sandbox.resources.limits]
cpu = "1"
memory = "1Gi"
""" + STANDARD_BOOTSTRAP_TOML + """\

[[roles]]
name = "worker"
description = "Worker"
max_rounds = 5
imports = ["shell"]
"""


@pytest.fixture
def sandbox_org(tmp_path: Path) -> Path:
    org = tmp_path / "acme"
    roles_dir = org / "roles" / "worker"
    roles_dir.mkdir(parents=True)
    (org / "org.toml").write_text(SANDBOX_ORG_TOML)
    (org / "MANIFESTO.md").write_text("# acme")
    (roles_dir / "prompt.md").write_text("worker")
    return org


def test_sandbox_compose_rendered_when_block_present(sandbox_org: Path) -> None:
    """Orgs declaring [sandbox] get a docker-compose.yml alongside pux.yaml."""
    env = org_build._make_env()
    org_build.render_org(sandbox_org, env)
    compose = sandbox_org / "docker-compose.yml"
    assert compose.exists(), "docker-compose.yml should be generated"
    text = compose.read_text()
    assert text.startswith(org_build.GENERATED_HEADER)
    assert "acme-sandbox:latest" in text
    assert "runtime: runsc" in text, "gvisor runtime_class must map to runsc"
    assert "scale: 2" in text, "warm_pool=2 must emit scale: 2"


def test_sandbox_compose_omitted_when_no_block(tmp_org: Path) -> None:
    """Orgs without [sandbox] do NOT get a docker-compose.yml."""
    env = org_build._make_env()
    org_build.render_org(tmp_org, env)
    assert not (tmp_org / "docker-compose.yml").exists()


def test_sandbox_runc_omits_runtime_line(tmp_path: Path) -> None:
    """runtime_class=runc (Docker default) must not emit a runtime: line.

    Note: runc + tier='standard' is rejected by the validator. This fixture
    uses tier='custom-build' (which allows runc) to exercise the rendering
    contract independently of the isolation contract."""
    org = tmp_path / "acme"
    (org / "roles" / "worker").mkdir(parents=True)
    (org / "roles" / "worker" / "prompt.md").write_text("p")
    (org / "MANIFESTO.md").write_text("m")
    (org / "Dockerfile").write_text("FROM pux-sandbox:latest\n")
    (org / "org.toml").write_text(
        MINIMAL_ORG_TOML
        + '\n[sandbox]\n'
        'tier = "custom-build"\n'
        'runtime_class = "runc"\n'
        'init_files = ["sandbox/x.py"]\n'
        '[sandbox.env]\n'
        'PUX_ORG_PATH = "/sandbox/workspace"\n'
        '[sandbox.build]\n'
        'context = "."\n'
        'dockerfile = "Dockerfile"\n'
        'justification = "test fixture — runc rendering"\n'
        + STANDARD_BOOTSTRAP_TOML_CUSTOM_BUILD
    )
    env = org_build._make_env()
    org_build.render_org(org, env)
    text = (org / "docker-compose.yml").read_text()
    assert "runtime:" not in text, "runc is Docker default — must not emit"


def test_sandbox_profile_in_pux_yaml(sandbox_org: Path) -> None:
    """The K8s-style profile fields surface in pux.yaml for kernel opt-in."""
    yaml = pytest.importorskip("yaml")
    env = org_build._make_env()
    org_build.render_org(sandbox_org, env)
    data = yaml.safe_load((sandbox_org / "pux.yaml").read_text())
    sb = data["sandbox"]
    assert sb["image"] == "acme-sandbox:latest"
    assert sb["runtime_class"] == "gvisor"
    assert sb["warm_pool"] == 2
    assert sb["resources"]["requests"]["cpu"] == "250m"
    assert sb["resources"]["limits"]["memory"] == "1Gi"


def test_sandbox_resources_translated_for_compose(sandbox_org: Path) -> None:
    """Docker compose gets translated units (250m→0.25, 1Gi→1G); pux.yaml keeps K8s."""
    env = org_build._make_env()
    org_build.render_org(sandbox_org, env)
    compose = (sandbox_org / "docker-compose.yml").read_text()
    # Compose view — Docker-translated.
    assert "cpus: '0.25'" in compose, "250m must translate to 0.25 for compose"
    assert "memory: 256M" in compose, "256Mi must translate to 256M for compose"
    assert "cpus: '1'" in compose, "1 must stay as 1 for compose"
    assert "memory: 1G" in compose, "1Gi must translate to 1G for compose"
    # Compose must NOT contain K8s suffixes — `docker compose up` rejects them.
    assert "250m" not in compose, "K8s millicore suffix leaked into compose"
    assert "1Gi" not in compose, "K8s Mi/Gi suffix leaked into compose"
    # pux.yaml preserves the K8s view (already covered by the test above, but
    # assert it here too so the contract is documented in one place).
    import yaml as _yaml
    data = _yaml.safe_load((sandbox_org / "pux.yaml").read_text())
    assert data["sandbox"]["resources"]["requests"]["cpu"] == "250m"


def test_validation_rejects_bad_runtime_class(tmp_path: Path) -> None:
    errs = org_build.validate_org_data(
        {
            "name": "acme",
            "description": "x",
            "sandbox": {"runtime_class": "fake-runtime"},
        },
        "acme",
        tmp_path,
    )
    assert any("runtime_class" in e for e in errs)


def test_validation_rejects_zero_warm_pool(tmp_path: Path) -> None:
    errs = org_build.validate_org_data(
        {
            "name": "acme",
            "description": "x",
            "sandbox": {"warm_pool": 0},
        },
        "acme",
        tmp_path,
    )
    assert any("warm_pool" in e for e in errs)


def test_validation_accepts_known_sandbox_modes(tmp_path: Path) -> None:
    """Both 'contained' (default) and 'host-access' must pass validation.
    A typo must fail loud so a misspelled 'host_acess' doesn't silently lock
    down an org that meant to opt out."""
    for ok in ("contained", "host-access"):
        errs = org_build.validate_org_data(
            {
                "name": "acme",
                "description": "x",
                "sandbox": {"mode": ok},
            },
            "acme",
            tmp_path,
        )
        assert not any("sandbox.mode" in e for e in errs), (
            f"expected {ok!r} to be valid, got errs={errs}"
        )


def test_validation_rejects_unknown_sandbox_mode(tmp_path: Path) -> None:
    errs = org_build.validate_org_data(
        {
            "name": "acme",
            "description": "x",
            "sandbox": {"mode": "host_acess"},  # typo: underscore, not hyphen
        },
        "acme",
        tmp_path,
    )
    assert any("sandbox.mode" in e for e in errs), errs


def test_pux_yaml_renders_host_access_mode(tmp_path: Path) -> None:
    """When org.toml sets sandbox.mode = 'host-access', the generated pux.yaml
    must carry that field so the kernel's OrgManifest.SandboxMode() picks it up."""
    org_dir = tmp_path / "coder"
    org_dir.mkdir()
    (org_dir / "org.toml").write_text(
        'name = "coder"\n'
        'description = "coding agent — host reach"\n'
        '[sandbox]\n'
        'tier = "standard"\n'
        'mode = "host-access"\n'
        'runtime_class = "gvisor"\n'
        'warm_pool = 1\n'
        '[sandbox.env]\n'
        'PUX_ORG_PATH = "/sandbox/workspace"\n'
        '[sandbox.resources.requests]\n'
        'cpu = "250m"\n'
        'memory = "256Mi"\n'
        '[sandbox.resources.limits]\n'
        'cpu = "1"\n'
        'memory = "1Gi"\n'
        + STANDARD_BOOTSTRAP_TOML
    )
    env = org_build._make_env()
    org_build.render_org(org_dir, env)
    pux = (org_dir / "pux.yaml").read_text()
    assert "mode: host-access" in pux, pux


def test_pux_yaml_renders_contained_default(tmp_path: Path) -> None:
    """When org.toml is silent on sandbox.mode, the renderer defaults to
    'contained' so the kernel always sees an explicit value."""
    org_dir = tmp_path / "locked"
    org_dir.mkdir()
    (org_dir / "org.toml").write_text(
        'name = "locked"\n'
        'description = "investment org — pure sandbox"\n'
        '[sandbox]\n'
        'tier = "standard"\n'
        'runtime_class = "gvisor"\n'
        'warm_pool = 1\n'
        '[sandbox.env]\n'
        'PUX_ORG_PATH = "/sandbox/workspace"\n'
        '[sandbox.resources.requests]\n'
        'cpu = "250m"\n'
        'memory = "256Mi"\n'
        '[sandbox.resources.limits]\n'
        'cpu = "1"\n'
        'memory = "1Gi"\n'
        + STANDARD_BOOTSTRAP_TOML
    )
    env = org_build._make_env()
    org_build.render_org(org_dir, env)
    pux = (org_dir / "pux.yaml").read_text()
    assert "mode: contained" in pux, pux


# --------------------------------------------------------------------------- #
# Phase A §A1: sandbox.tier contract                                          #
# --------------------------------------------------------------------------- #
# Every [sandbox] block must declare a tier. Three tiers, mutually exclusive.
# See plan: declarative-cooking-wolf.md §A1.

_VALID_STANDARD_SANDBOX = {
    "tier": "standard",
    "runtime_class": "gvisor",
    "warm_pool": 1,
    "env": {"PUX_ORG_PATH": "/sandbox/workspace"},
    "resources": {
        "requests": {"cpu": "250m", "memory": "256Mi"},
        "limits": {"cpu": "1", "memory": "1Gi"},
    },
    "bootstrap": dict(STANDARD_BOOTSTRAP_DICT),
}


def _org_with_sandbox(sandbox: dict) -> dict:
    """Helper — wrap a sandbox block in a minimal valid org dict."""
    return {
        "name": "acme",
        "description": "test org",
        "sandbox": sandbox,
    }


def test_validation_rejects_sandbox_without_tier(tmp_path: Path) -> None:
    """Missing tier = hard failure (forcing function)."""
    errs = org_build.validate_org_data(
        _org_with_sandbox({"runtime_class": "gvisor"}),
        "acme",
        tmp_path,
    )
    assert any("sandbox.tier is required" in e for e in errs), errs


def test_validation_rejects_unknown_tier(tmp_path: Path) -> None:
    """Bogus tier value must fail loud."""
    errs = org_build.validate_org_data(
        _org_with_sandbox({"tier": "premium"}),
        "acme",
        tmp_path,
    )
    assert any("sandbox.tier" in e and "premium" in e for e in errs), errs


def test_validation_accepts_standard_tier(tmp_path: Path) -> None:
    """A well-formed standard tier passes cleanly."""
    errs = org_build.validate_org_data(
        _org_with_sandbox(dict(_VALID_STANDARD_SANDBOX)),
        "acme",
        tmp_path,
    )
    tier_errs = [e for e in errs if "tier" in e.lower()]
    assert tier_errs == [], tier_errs


def test_validation_rejects_standard_with_runc(tmp_path: Path) -> None:
    """standard tier requires gVisor or Kata — runc = no isolation."""
    bad = dict(_VALID_STANDARD_SANDBOX)
    bad["runtime_class"] = "runc"
    errs = org_build.validate_org_data(_org_with_sandbox(bad), "acme", tmp_path)
    assert any("runtime_class must be 'gvisor' or 'kata'" in e for e in errs), errs


def test_validation_accepts_standard_without_pux_org_path(tmp_path: Path) -> None:
    """PUX_ORG_PATH env is OPTIONAL — kernel uses openshell.project-path LABEL
    from bootstrap.sh for container adoption, not the env var. The env var is
    just an in-container hint that scripts can opt into via /sandbox/.env.
    Three production orgs (deep-research-engine, social-media-pipeline,
    tech-noir) don't declare it and work correctly."""
    good = dict(_VALID_STANDARD_SANDBOX)
    good["env"] = {}  # no PUX_ORG_PATH — still valid
    errs = org_build.validate_org_data(_org_with_sandbox(good), "acme", tmp_path)
    assert not errs, errs


def test_validation_rejects_standard_with_build_block(tmp_path: Path) -> None:
    """standard + build = contradiction. Use custom-build."""
    bad = dict(_VALID_STANDARD_SANDBOX)
    bad["build"] = {"context": ".", "dockerfile": "Dockerfile"}
    errs = org_build.validate_org_data(_org_with_sandbox(bad), "acme", tmp_path)
    assert any("forbidden for tier='standard'" in e for e in errs), errs


def test_validation_rejects_custom_build_without_justification(tmp_path: Path) -> None:
    """custom-build requires justification — articulate WHY pux-sandbox:latest
    is insufficient."""
    errs = org_build.validate_org_data(
        _org_with_sandbox({
            "tier": "custom-build",
            "build": {
                "context": ".",
                "dockerfile": "Dockerfile",
                "justification": "",  # empty
            },
            "env": {"PUX_ORG_PATH": "/sandbox/workspace"},
        }),
        "acme",
        tmp_path,
    )
    assert any("justification" in e for e in errs), errs


def test_validation_accepts_custom_build_with_justification(tmp_path: Path) -> None:
    """A custom-build tier with a real justification passes."""
    errs = org_build.validate_org_data(
        _org_with_sandbox({
            "tier": "custom-build",
            "build": {
                "context": ".",
                "dockerfile": "Dockerfile",
                "justification": "Manim + LaTeX + Kokoro — too heavy for base image",
            },
            "env": {"PUX_ORG_PATH": "/sandbox/workspace"},
            "bootstrap": dict(STANDARD_BOOTSTRAP_DICT),
        }),
        "acme",
        tmp_path,
    )
    tier_errs = [e for e in errs if "tier" in e.lower() or "justification" in e]
    assert tier_errs == [], tier_errs


def test_validation_rejects_skeleton_with_sandbox_body(tmp_path: Path) -> None:
    """skeleton tier = no sandbox config. Any non-tier key in the block = error."""
    errs = org_build.validate_org_data(
        _org_with_sandbox({"tier": "skeleton", "image": "pux-sandbox:latest"}),
        "acme",
        tmp_path,
    )
    assert any("tier='skeleton'" in e and "image" in e for e in errs), errs


def test_validation_accepts_skeleton_tier_with_only_tier_key(tmp_path: Path) -> None:
    """skeleton tier with empty sandbox body (just tier marker) is valid —
    documents intent without contradicting the contract."""
    errs = org_build.validate_org_data(
        _org_with_sandbox({"tier": "skeleton"}),
        "acme",
        tmp_path,
    )
    tier_errs = [e for e in errs if "tier" in e.lower()]
    assert tier_errs == [], tier_errs


def test_validation_rejects_org_without_sandbox_block_silently(tmp_path: Path) -> None:
    """An org with NO [sandbox] block at all is treated as a 'skeleton' org
    implicitly. The validator does NOT require tier in that case — this is
    how legacy minimal orgs (dev-bot, general) work."""
    errs = org_build.validate_org_data(
        {"name": "acme", "description": "minimal"},
        "acme",
        tmp_path,
    )
    assert not any("tier" in e.lower() for e in errs), errs


# --------------------------------------------------------------------------- #
# Phase C §C1: idle_shutdown_secs contract                                    #
# --------------------------------------------------------------------------- #

def test_validation_accepts_zero_idle_shutdown(tmp_path: Path) -> None:
    """0 = never auto-shutdown (default). Must be valid."""
    sandbox = dict(_VALID_STANDARD_SANDBOX)
    sandbox["idle_shutdown_secs"] = 0
    errs = org_build.validate_org_data(_org_with_sandbox(sandbox), "acme", tmp_path)
    assert not any("idle_shutdown_secs" in e for e in errs), errs


def test_validation_accepts_positive_idle_shutdown(tmp_path: Path) -> None:
    """1800 = 30 min auto-shutdown. Must be valid."""
    sandbox = dict(_VALID_STANDARD_SANDBOX)
    sandbox["idle_shutdown_secs"] = 1800
    errs = org_build.validate_org_data(_org_with_sandbox(sandbox), "acme", tmp_path)
    assert not any("idle_shutdown_secs" in e for e in errs), errs


def test_validation_rejects_negative_idle_shutdown(tmp_path: Path) -> None:
    """Negative = nonsensical."""
    sandbox = dict(_VALID_STANDARD_SANDBOX)
    sandbox["idle_shutdown_secs"] = -1
    errs = org_build.validate_org_data(_org_with_sandbox(sandbox), "acme", tmp_path)
    assert any("idle_shutdown_secs" in e for e in errs), errs


def test_validation_rejects_non_int_idle_shutdown(tmp_path: Path) -> None:
    """String or bool — watchdog needs an int."""
    sandbox = dict(_VALID_STANDARD_SANDBOX)
    sandbox["idle_shutdown_secs"] = "1800"
    errs = org_build.validate_org_data(_org_with_sandbox(sandbox), "acme", tmp_path)
    assert any("idle_shutdown_secs" in e for e in errs), errs


# --------------------------------------------------------------------------- #
# host.docker.internal trap — Linux host network mode does not resolve it.     #
# Any env var or smoke-test string carrying it is a latent bug on Linux.       #
# See feedback_host_docker_internal_linux_trap.md.                             #
# --------------------------------------------------------------------------- #

def test_validation_rejects_host_docker_internal_in_env(tmp_path: Path) -> None:
    """env var carrying host.docker.internal must fail validation."""
    sandbox = dict(_VALID_STANDARD_SANDBOX)
    sandbox["env"] = {"SURREALDB_URL": "http://host.docker.internal:8000/surreal"}
    errs = org_build.validate_org_data(_org_with_sandbox(sandbox), "acme", tmp_path)
    hits = [e for e in errs if "host.docker.internal" in e and "env.SURREALDB_URL" in e]
    assert hits, f"expected host.docker.internal error, got: {errs}"
    assert "localhost" in hits[0], "error must point at the localhost fix"


def test_validation_rejects_host_docker_internal_in_smoke_test(tmp_path: Path) -> None:
    """Smoke-test shim .replace('host.docker.internal','localhost') is the
    symptom-level patch we're trying to prevent. Reject at source."""
    sandbox = dict(_VALID_STANDARD_SANDBOX)
    sandbox["bootstrap"] = dict(STANDARD_BOOTSTRAP_DICT)
    sandbox["bootstrap"]["smoke_test_command"] = (
        "python3 -c \"import os; url=os.environ['X'].replace("
        "'host.docker.internal','localhost'); ...\""
    )
    errs = org_build.validate_org_data(_org_with_sandbox(sandbox), "acme", tmp_path)
    hits = [e for e in errs if "host.docker.internal" in e and "smoke_test_command" in e]
    assert hits, f"expected smoke_test error, got: {errs}"
    assert ".replace()" in hits[0], "error must call out the shim anti-pattern"


def test_validation_rejects_host_docker_internal_in_dep_check(tmp_path: Path) -> None:
    """hard_deps[].check + soft_deps[].check also can't carry it."""
    sandbox = dict(_VALID_STANDARD_SANDBOX)
    sandbox["bootstrap"] = dict(STANDARD_BOOTSTRAP_DICT)
    sandbox["bootstrap"]["hard_deps"] = [
        {"name": "X", "check": "curl http://host.docker.internal:8000", "error_msg": "x"}
    ]
    errs = org_build.validate_org_data(_org_with_sandbox(sandbox), "acme", tmp_path)
    hits = [e for e in errs if "host.docker.internal" in e and "hard_deps[0].check" in e]
    assert hits, f"expected hard_deps error, got: {errs}"


def test_validation_accepts_localhost_env(tmp_path: Path) -> None:
    """Positive control — localhost is the correct value."""
    sandbox = dict(_VALID_STANDARD_SANDBOX)
    sandbox["env"] = {
        "SURREALDB_URL": "http://localhost:8000/surreal",
        "PUX_ORG_PATH": "/sandbox/workspace",
    }
    errs = org_build.validate_org_data(_org_with_sandbox(sandbox), "acme", tmp_path)
    hdi_errs = [e for e in errs if "host.docker.internal" in e]
    assert not hdi_errs, f"expected zero host.docker.internal errors, got: {hdi_errs}"


# --------------------------------------------------------------------------- #
# Phase A §A1: tier renders into pux.yaml                                      #
# --------------------------------------------------------------------------- #

def test_tier_renders_into_pux_yaml(tmp_path: Path) -> None:
    """The tier field must land in pux.yaml so the Go kernel can read it."""
    org = tmp_path / "acme"
    (org / "roles" / "worker").mkdir(parents=True)
    (org / "roles" / "worker" / "prompt.md").write_text("p")
    (org / "MANIFESTO.md").write_text("m")
    (org / "org.toml").write_text(
        MINIMAL_ORG_TOML
        + '\n[sandbox]\n'
        'tier = "standard"\n'
        'runtime_class = "gvisor"\n'
        'warm_pool = 1\n'
        'env = { PUX_ORG_PATH = "/sandbox/workspace" }\n'
        '\n[sandbox.resources.requests]\n'
        'cpu = "250m"\n'
        'memory = "256Mi"\n'
        '\n[sandbox.resources.limits]\n'
        'cpu = "1"\n'
        'memory = "1Gi"\n'
        + STANDARD_BOOTSTRAP_TOML
    )
    env = org_build._make_env()
    org_build.render_org(org, env)
    pux = (org / "pux.yaml").read_text()
    assert "tier: standard" in pux, pux


def test_sandbox_compose_yaml_parses(sandbox_org: Path) -> None:
    """Generated compose must be valid YAML."""
    yaml = pytest.importorskip("yaml")
    env = org_build._make_env()
    org_build.render_org(sandbox_org, env)
    data = yaml.safe_load((sandbox_org / "docker-compose.yml").read_text())
    assert "services" in data
    assert "acme-sandbox" in data["services"]


def test_sandbox_service_name_overrides_default(tmp_path: Path) -> None:
    """sandbox.service_name overrides the default '<org>-sandbox' service name
    so existing docs/scripts that hard-code the original name keep working."""
    org = tmp_path / "acme"
    (org / "roles" / "worker").mkdir(parents=True)
    (org / "roles" / "worker" / "prompt.md").write_text("p")
    (org / "MANIFESTO.md").write_text("m")
    (org / "org.toml").write_text(
        MINIMAL_ORG_TOML
        + '\n[sandbox]\n'
        'tier = "standard"\n'
        'service_name = "video-producer"\n'
        'runtime_class = "gvisor"\n'
        'warm_pool = 1\n'
        'init_files = ["sandbox/x.py"]\n'
        '[sandbox.env]\n'
        'PUX_ORG_PATH = "/sandbox/workspace"\n'
        '[sandbox.resources.requests]\n'
        'cpu = "250m"\n'
        'memory = "256Mi"\n'
        '[sandbox.resources.limits]\n'
        'cpu = "1"\n'
        'memory = "1Gi"\n'
        + STANDARD_BOOTSTRAP_TOML
    )
    env = org_build._make_env()
    org_build.render_org(org, env)
    text = (org / "docker-compose.yml").read_text()
    assert "video-producer:" in text
    assert "acme-sandbox" not in text


def test_sandbox_docker_name_without_external_does_not_emit_external_flag(
    tmp_path: Path,
) -> None:
    """Regression: video-production's volume was being marked external=true just
    because it had a custom Docker name. `docker_name` only sets the physical
    name; `external` is a separate explicit flag."""
    org = tmp_path / "acme"
    (org / "roles" / "worker").mkdir(parents=True)
    (org / "roles" / "worker" / "prompt.md").write_text("p")
    (org / "MANIFESTO.md").write_text("m")
    (org / "org.toml").write_text(
        MINIMAL_ORG_TOML
        + '\n[sandbox]\n'
        'tier = "standard"\n'
        'runtime_class = "gvisor"\n'
        'warm_pool = 1\n'
        'init_files = ["sandbox/x.py"]\n'
        '[sandbox.env]\n'
        'PUX_ORG_PATH = "/sandbox/workspace"\n'
        '[sandbox.resources.requests]\n'
        'cpu = "250m"\n'
        'memory = "256Mi"\n'
        '[sandbox.resources.limits]\n'
        'cpu = "1"\n'
        'memory = "1Gi"\n'
        + STANDARD_BOOTSTRAP_TOML
        + "\n[[sandbox.volumes]]\n"
        'type = "volume"\n'
        'name = "workspace"\n'
        'docker_name = "acme_workspace"\n'
        'container = "/workspace"\n'
        "\n[[sandbox.networks]]\n"
        'name = "backend"\n'
        'docker_name = "acme_backend"\n'
        "external = true\n"
    )
    env = org_build._make_env()
    org_build.render_org(org, env)
    text = (org / "docker-compose.yml").read_text()
    # Volume: name override present, NOT followed by external: true
    assert "name: acme_workspace" in text
    assert "name: acme_workspace\n    external: true" not in text, (
        "Volume with docker_name but no external flag must not be marked external"
    )
    # Network: name override present AND external: true follows it
    assert "name: acme_backend\n    external: true" in text


# --------------------------------------------------------------------------- #
# Integration with the real orgs/ tree
# --------------------------------------------------------------------------- #

@pytest.mark.skipif(
    not (REPO_ROOT / "orgs").exists(),
    reason="orgs/ directory not present",
)
def test_all_real_orgs_render_cleanly() -> None:
    """Every checked-in org.toml renders without exception."""
    env = org_build._make_env()
    failures: list[str] = []
    for org_dir in org_build.discover_orgs():
        with tempfile.TemporaryDirectory() as td:
            try:
                org_build.render_org(org_dir, env, target_dir=Path(td))
            except Exception as e:
                failures.append(f"{org_dir.name}: {e}")
    if failures:
        # Surface every failing org so the migration plan is visible in
        # CI output. PR2 should drive this list to zero.
        raise AssertionError(
            "Orgs failing PR1 tier validation (expected until PR2 migration):\n  - "
            + "\n  - ".join(failures)
        )


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-v"]))
