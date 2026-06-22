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
# Integration with the real orgs/ tree
# --------------------------------------------------------------------------- #

@pytest.mark.skipif(
    not (REPO_ROOT / "orgs").exists(),
    reason="orgs/ directory not present",
)
def test_all_real_orgs_render_cleanly() -> None:
    """Every checked-in org.toml renders without exception."""
    env = org_build._make_env()
    for org_dir in org_build.discover_orgs():
        with tempfile.TemporaryDirectory() as td:
            # Render into tmp to avoid mutating the working tree.
            org_build.render_org(org_dir, env, target_dir=Path(td))


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-v"]))
