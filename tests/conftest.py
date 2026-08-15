"""Shared fixtures for the pux test suite."""
from __future__ import annotations

from pathlib import Path

import pytest


@pytest.fixture(autouse=True)
def _provider_test_keys(monkeypatch):
    """Seed EVERY provider's ``api_key_env`` (declared in models.yaml) with a
    dummy value so model instantiation never KeyErrors — regardless of which
    providers are configured. Model-agnostic: add/remove a provider profile in
    models.yaml and the tests follow automatically, with no per-test key
    hardcoding. Models are never called in unit tests (no network); the dummy
    values just satisfy ``os.environ[api_key_env]`` at build time."""
    from pux_harness.agent import model as _model  # local import: tests may patch it
    for prof in _model._providers().values():
        env = prof.get("api_key_env")
        if env:
            monkeypatch.setenv(env, "test-key")


@pytest.fixture(scope="session")
def project_root() -> Path:
    """The project root (parent of tests/)."""
    return Path(__file__).resolve().parent.parent


@pytest.fixture
def fake_orgs_tree(tmp_path: Path, monkeypatch):
    """Scratch orgs/ tree with both contract._orgs_dir and orgs._orgs_dir
    patched. _shared/agents is pre-created. Returns tmp_path."""
    from pux_harness.agent import orgs
    from pux_harness.validation import audit as ov
    (tmp_path / "orgs" / "_shared" / "agents").mkdir(parents=True)
    monkeypatch.setattr(ov, "_orgs_dir", lambda: tmp_path / "orgs")
    monkeypatch.setattr(orgs, "_orgs_dir", lambda: tmp_path / "orgs")
    return tmp_path


# --- tree-build helpers for contract/export tests ----------------------------


def add_org(
    root: Path, name: str, *,
    extends: str | None = None,
    agents: list[str] | None = None,
    body: str = "# Org\n",
    policy: str | None = None,
) -> Path:
    """Write ``orgs/<name>/AGENTS.md`` + ``org.yaml`` + optional ``policy.yaml``."""
    d = root / "orgs" / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "AGENTS.md").write_text(body)
    lines: list[str] = []
    if agents is not None:
        lines.append(f"agents: [{', '.join(agents)}]")
    if extends is not None:
        lines.append(f"extends: {extends}")
    if lines:
        (d / "org.yaml").write_text("\n".join(lines) + "\n")
    if policy is not None:
        (d / "policy.yaml").write_text(policy)
    return d


def add_agent(
    root: Path, slug: str, org: str, *,
    body: str = "prose body\n",
    extends: str | None = None,
    extra_frontmatter: dict[str, str] | None = None,
) -> Path:
    """Write a frontmatter+body ``orgs/<org>/agents/<slug>.md``.

    ``extra_frontmatter`` is merged into the frontmatter dict (name/description
    are set automatically; extends is set when provided).
    """
    agents_dir = root / "orgs" / org / "agents"
    agents_dir.mkdir(parents=True, exist_ok=True)
    fm: dict[str, str] = {"name": f'"{slug}"', "description": f'"{slug} specialist"'}
    if extends is not None:
        fm["extends"] = extends
    if extra_frontmatter:
        fm.update(extra_frontmatter)
    lines = ["---"] + [f"{k}: {v}" for k, v in fm.items()] + ["---"]
    path = agents_dir / f"{slug}.md"
    path.write_text("\n".join(lines) + f"\n\n{body}")
    return path


def write_profile(root: Path, org: str, text: str) -> Path:
    """Write ``orgs/<org>/profile.yaml``."""
    path = root / "orgs" / org / "profile.yaml"
    path.write_text(text)
    return path
