"""Parity gate for the policy engine (mirrors the 22 Go tests in
``backend/internal/policy``).

Every test here is a 1:1 port of a Go test (``policy_test.go`` /
``egress_test.go``): same inputs, same assertions. The point is to PROVE the
Python port (``pux_harness.sandbox.policy``) behaves identically to the Go package it
replaces — so swapping the engine behind the harness's policy resolution is
invisible. The real, network-touching runs (DNS) skip offline rather than fail,
matching Go's ``-short`` mode.

The shipped-policy integration test also re-proves (against the live
``orgs/*/policy.yaml`` files) that every operator-authored policy parses cleanly
through the new engine — the schema-drift catcher.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from pux_harness.sandbox import policy


# --- helpers ------------------------------------------------------------------


def _write_policy(tmp_path: Path, org: str, body: str) -> Path:
    """Write a policy.yaml under a fake orgs/<org>/ and return project_root."""
    d = tmp_path / "orgs" / org
    d.mkdir(parents=True)
    (d / "policy.yaml").write_text(body)
    return tmp_path


# --- Load ---------------------------------------------------------------------


def test_load_no_file(tmp_path: Path) -> None:
    with pytest.raises(policy.NoPolicy):
        policy.load("ghost", tmp_path)


def test_load_empty_org_name(tmp_path: Path) -> None:
    # Defensive — empty org name must not even attempt a read.
    with pytest.raises(policy.NoPolicy):
        policy.load("", "/tmp")


def test_load_empty_file_is_empty_policy(tmp_path: Path) -> None:
    # An empty (but present) policy.yaml is valid — opts in with no sections.
    p = policy.load("blank", _write_policy(tmp_path, "blank", ""))
    assert p == policy.Policy()


def test_load_unknown_top_level_keys_ignored(tmp_path: Path) -> None:
    # Go's yaml.v3 ignores unknown keys (lenient). The Python port matches;
    # the *contract* (contract.py rule 5) adds the strict unknown-section check.
    p = policy.load(
        "loose", _write_policy(tmp_path, "loose", "bogus: whatever\nsandbox:\n  image: img:latest\n")
    )
    assert p.sandbox.image == "img:latest"


# --- validate_env + env_vars --------------------------------------------------


def test_load_host_setup_parses(tmp_path: Path) -> None:
    body = """
host_setup:
  - name: extract_cookies
    helper_script: orgs/_shared/sandbox/extract_browser_cookies.py
    python_deps: [browser-cookie3, pycryptodome]
    args: [--browser, brave, --domain, x.com, --b64]
    exports:
      TWITTER_COOKIES_B64: stdout
"""
    p = policy.load("tw", _write_policy(tmp_path, "tw", body))
    assert len(p.host_setup) == 1
    hook = p.host_setup[0]
    assert hook.name == "extract_cookies"
    assert hook.helper_script == "orgs/_shared/sandbox/extract_browser_cookies.py"
    assert hook.python_deps == ["browser-cookie3", "pycryptodome"]
    assert hook.args == ["--browser", "brave", "--domain", "x.com", "--b64"]
    assert hook.exports == {"TWITTER_COOKIES_B64": "stdout"}


def test_load_host_setup_absent_is_empty(tmp_path: Path) -> None:
    p = policy.load("none", _write_policy(tmp_path, "none", "sandbox:\n  image: img:latest\n"))
    assert p.host_setup == []
    assert policy.host_setup_hooks(p) == []


def test_host_setup_hooks_none_is_empty() -> None:
    assert policy.host_setup_hooks(None) == []


def test_load_host_setup_not_a_list_fails(tmp_path: Path) -> None:
    # A mapping instead of a list — must fail loud, not silently coerce.
    body = "host_setup:\n  name: oops\n"
    with pytest.raises(policy.PolicyError):
        policy.load("bad", _write_policy(tmp_path, "bad", body))


def test_load_host_setup_entry_not_a_mapping_fails(tmp_path: Path) -> None:
    body = "host_setup:\n  - just-a-string\n"
    with pytest.raises(policy.PolicyError):
        policy.load("bad", _write_policy(tmp_path, "bad", body))


def test_load_sandbox_build_parses(tmp_path: Path) -> None:
    body = """
sandbox:
  image: video-production-video-producer:latest
  build:
    dockerfile: orgs/specialists/video-production/Dockerfile
    context: orgs/specialists/video-production
"""
    p = policy.load("vp", _write_policy(tmp_path, "vp", body))
    assert p.sandbox.image == "video-production-video-producer:latest"
    assert p.sandbox.build.dockerfile == "orgs/specialists/video-production/Dockerfile"
    assert p.sandbox.build.context == "orgs/specialists/video-production"


def test_load_sandbox_build_not_a_mapping_fails(tmp_path: Path) -> None:
    body = "sandbox:\n  build: oops\n"
    with pytest.raises(policy.PolicyError):
        policy.load("bad", _write_policy(tmp_path, "bad", body))


def test_build_spec_absent_is_none(tmp_path: Path) -> None:
    p = policy.load(
        "nobuild", _write_policy(tmp_path, "nobuild", "sandbox:\n  image: foo:latest\n")
    )
    assert policy.build_spec(p) is None


def test_build_spec_no_dockerfile_is_none(tmp_path: Path) -> None:
    # A build mapping with no dockerfile == no build requested.
    p = policy.load(
        "empty", _write_policy(tmp_path, "empty", "sandbox:\n  build:\n    context: orgs/x\n")
    )
    assert policy.build_spec(p) is None


def test_build_spec_none_is_none() -> None:
    assert policy.build_spec(None) is None


def test_build_spec_returns_spec(tmp_path: Path) -> None:
    body = """
sandbox:
  build:
    dockerfile: orgs/specialists/video-production/Dockerfile
"""
    p = policy.load("vp", _write_policy(tmp_path, "vp", body))
    bs = policy.build_spec(p)
    assert bs is not None
    assert bs.dockerfile == "orgs/specialists/video-production/Dockerfile"


def test_known_policy_sections_includes_host_setup() -> None:
    # contract.py consults this; host_setup must be a known section or a
    # twitter/video-production policy.yaml carrying it would trip the
    # unknown-section rule.
    from pux_harness.validation.schemas import KNOWN_POLICY_SECTIONS

    assert "host_setup" in KNOWN_POLICY_SECTIONS


# --- protocols (agent-facing client surfaces) ---------------------------------


def test_load_protocols_parses(tmp_path: Path) -> None:
    body = "protocols:\n  - acp\n  - agui\n"
    p = policy.load("acme", _write_policy(tmp_path, "acme", body))
    assert p.protocols == ["acp", "agui"]


def test_load_protocols_absent_is_empty(tmp_path: Path) -> None:
    # No `protocols:` -> empty list (resolve_protocols supplies the default).
    p = policy.load("acme", _write_policy(tmp_path, "acme", "sandbox:\n  image: img:latest\n"))
    assert p.protocols == []


def test_load_protocols_not_a_list_fails(tmp_path: Path) -> None:
    # A scalar instead of a list — must fail loud, not silently coerce.
    body = "protocols: acp\n"
    with pytest.raises(policy.PolicyError):
        policy.load("bad", _write_policy(tmp_path, "bad", body))


def test_load_protocols_entry_not_a_string_coerced(tmp_path: Path) -> None:
    # Non-string entries are stringified (mirrors tool_servers leniency) — but a
    # bogus NAME still trips the contract validator downstream (tested in
    # test_org_contract); here we only assert the loader survives it.
    body = "protocols:\n  - acp\n  - 42\n"
    p = policy.load("weird", _write_policy(tmp_path, "weird", body))
    assert p.protocols == ["acp", "42"]


def test_known_protocols_is_the_default_set() -> None:
    # KNOWN_PROTOCOLS is the validator's allowlist; it equals DEFAULT_PROTOCOLS
    # so the contract never rejects a defaulted policy.
    assert set(policy.DEFAULT_PROTOCOLS) == set(policy.KNOWN_PROTOCOLS)
    assert set(policy.KNOWN_PROTOCOLS) == {"agui"}


# --- shipped policies (integration) ------------------------------------------


def test_shipped_policies_parse_cleanly() -> None:
    """Every shipped org's policy.yaml loads through the real loader without a
    schema error (the Go port's TestLoad_ShippedPolicies equivalent — minus the
    deleted validate_env/resolve_mounts/egress_rules surfaces)."""
    repo_root = Path(__file__).resolve().parents[2]
    count = 0
    for org_dir in sorted((repo_root / "orgs").glob("*/")) + \
            sorted((repo_root / "orgs" / "specialists").glob("*/")):
        if not (org_dir / "policy.yaml").is_file():
            continue
        count += 1
        policy.load(org_dir.name, repo_root)  # raises PolicyError on drift
    if count == 0:
        pytest.skip("no shipped policy.yaml files found")


def test_twitter_agent_has_warmup_browser_job() -> None:
    """The twitter-agent is a browser-heavy org — its policy.yaml must declare
    the warmup_browser job so the SeleniumBase Chrome stack is pre-warmed before
    the agent runs (matching general and dev-bot)."""
    repo_root = Path(__file__).resolve().parents[2]
    pol_path = repo_root / "orgs" / "specialists" / "twitter-agent" / "policy.yaml"
    if not pol_path.is_file():
        pytest.skip("twitter-agent policy.yaml not found")
    p = policy.load("twitter-agent", repo_root)
    job_names = [j.name for j in p.jobs]
    assert "warmup_browser" in job_names, (
        f"twitter-agent must declare warmup_browser job; found: {job_names}"
    )
