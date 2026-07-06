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


def test_load_valid_yaml(tmp_path: Path) -> None:
    body = """
workspace:
  mounts:
    - host: ${HOME}
      container: /workspace/home
      mode: ro
  run_as_host_user: true
egress:
  allow:
    - host: 1.2.3.4
      port: 443
    - host: example.com
      ports: [80, 443]
credentials:
  required: [ALPHA]
  optional: [BETA]
"""
    p = policy.load("acme", _write_policy(tmp_path, "acme", body))
    assert p.workspace.run_as_host_user is True
    assert len(p.workspace.mounts) == 1
    assert p.workspace.mounts[0].container == "/workspace/home"
    assert len(p.egress.allow) == 2
    assert p.egress.allow[0].protocol == "tcp"  # default tcp
    assert p.credentials.required == ["ALPHA"]


def test_load_malformed_yaml(tmp_path: Path) -> None:
    # Tabs are explicitly invalid in YAML — guaranteed parser failure.
    with pytest.raises(policy.PolicyError):
        policy.load("broken", _write_policy(tmp_path, "broken", "workspace:\n\tmounts: oops\n"))


def test_load_sandbox_image_and_tier(tmp_path: Path) -> None:
    body = """
sandbox:
  image: video-production-video-producer:latest
  tier: isolated
"""
    p = policy.load("video-production", _write_policy(tmp_path, "video-production", body))
    assert p.sandbox.image == "video-production-video-producer:latest"
    assert p.sandbox.tier == "isolated"


def test_load_empty_file_is_empty_policy(tmp_path: Path) -> None:
    # An empty (but present) policy.yaml is valid — opts in with no sections.
    p = policy.load("blank", _write_policy(tmp_path, "blank", ""))
    assert p == policy.Policy()


def test_load_unknown_top_level_keys_ignored(tmp_path: Path) -> None:
    # Go's yaml.v3 ignores unknown keys (lenient). The Python port matches;
    # the *contract* (contract.py rule 5) adds the strict unknown-section check.
    p = policy.load("loose", _write_policy(tmp_path, "loose", "bogus: whatever\nsandbox:\n  tier: isolated\n"))
    assert p.sandbox.tier == "isolated"


# --- validate_env + env_vars --------------------------------------------------


def test_validate_env_all_present(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("REQUIRED_ONE", "a")
    monkeypatch.setenv("REQUIRED_TWO", "b")
    p = policy.Policy(credentials=policy.Credentials(required=["REQUIRED_ONE", "REQUIRED_TWO"]))
    policy.validate_env(p)  # must not raise


def test_validate_env_missing_lists_all(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("PRESENT_ONE", "x")
    p = policy.Policy(
        credentials=policy.Credentials(
            required=["PRESENT_ONE", "MISSING_ONE", "MISSING_TWO"]
        )
    )
    with pytest.raises(policy.MissingCreds) as ei:
        policy.validate_env(p)
    assert ei.value.missing == ["MISSING_ONE", "MISSING_TWO"]


def test_validate_env_none_is_noop() -> None:
    policy.validate_env(None)  # must not raise


def test_env_vars_required_and_optional(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("REQ", "rv")
    monkeypatch.setenv("OPT_SET", "ov")
    # OPT_UNSET intentionally not set.
    p = policy.Policy(
        credentials=policy.Credentials(
            required=["REQ"], optional=["OPT_SET", "OPT_UNSET"]
        )
    )
    got = set(policy.env_vars(p))
    assert got == {"REQ=rv", "OPT_SET=ov"}


def test_env_vars_cookies_env_injected(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("TWITTER_COOKIES_B64", "eyJmb28iOiAiYmFyIn0=")
    p = policy.Policy(browser=policy.BrowserSpec(cookies_env="TWITTER_COOKIES_B64"))
    out = policy.env_vars(p)
    assert "TWITTER_COOKIES_B64=eyJmb28iOiAiYmFyIn0=" in out
    assert "SEED_COOKIES_ENV=TWITTER_COOKIES_B64" in out


def test_env_vars_cookies_env_absent_skipped(monkeypatch: pytest.MonkeyPatch) -> None:
    # Declared but operator didn't export it = silent skip, no partial entries.
    monkeypatch.delenv("TWITTER_COOKIES_B64", raising=False)
    p = policy.Policy(browser=policy.BrowserSpec(cookies_env="TWITTER_COOKIES_B64"))
    assert "SEED_COOKIES_ENV=TWITTER_COOKIES_B64" not in policy.env_vars(p)


def test_env_vars_none_is_empty() -> None:
    assert policy.env_vars(None) == []


# --- resolve_mounts -----------------------------------------------------------


def test_resolve_mounts_placeholder_expansion(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("MY_VAR", "/tmp/expanded")
    p = policy.Policy(
        workspace=policy.Workspace(
            mounts=[policy.Mount(host="${MY_VAR}", container="/workspace/x", mode="rw")]
        )
    )
    out = policy.resolve_mounts(p)
    assert len(out) == 1
    assert out[0].host == "/tmp/expanded"


def test_resolve_mounts_unset_var_fails_loud() -> None:
    p = policy.Policy(
        workspace=policy.Workspace(
            mounts=[policy.Mount(host="${UNSET_MOUNT_VAR}", container="/workspace/x")]
        )
    )
    with pytest.raises(policy.UnresolvedMount) as ei:
        policy.resolve_mounts(p)
    assert ei.value.missing_var == "UNSET_MOUNT_VAR"
    assert ei.value.unresolved == "${UNSET_MOUNT_VAR}"
    assert ei.value.container == "/workspace/x"


def test_resolve_mounts_relative_container_rejected() -> None:
    p = policy.Policy(
        workspace=policy.Workspace(
            mounts=[policy.Mount(host="/abs/path", container="relative/path")]
        )
    )
    with pytest.raises(policy.PolicyError):
        policy.resolve_mounts(p)


def test_resolve_mounts_bad_mode_rejected() -> None:
    p = policy.Policy(
        workspace=policy.Workspace(
            mounts=[policy.Mount(host="/abs/path", container="/workspace/x", mode="execute")]
        )
    )
    with pytest.raises(policy.PolicyError):
        policy.resolve_mounts(p)


def test_resolve_mounts_default_mode_rw() -> None:
    p = policy.Policy(
        workspace=policy.Workspace(
            mounts=[policy.Mount(host="/abs/path", container="/workspace/x")]
        )
    )
    out = policy.resolve_mounts(p)
    assert out[0].mode == "rw"


def test_resolve_mounts_none_is_empty() -> None:
    assert policy.resolve_mounts(None) == []


# --- egress_rules -------------------------------------------------------------


def test_egress_rules_literal_ip() -> None:
    p = policy.Policy(egress=policy.Egress(allow=[policy.Rule(host="1.2.3.4", port=443)]))
    assert policy.egress_rules(p) == "1.2.3.4 443\n"


def test_egress_rules_ipv6_literal() -> None:
    p = policy.Policy(egress=policy.Egress(allow=[policy.Rule(host="::1", port=443)]))
    assert policy.egress_rules(p) == "::1 443\n"


def test_egress_rules_container_resolved_host() -> None:
    # host.docker.internal is a Docker-internal /etc/hosts entry — must pass
    # through verbatim, NOT hit DNS (would fail offline), NOT get a refresh
    # comment (the refresh script would try to re-resolve it host-side + fail).
    p = policy.Policy(egress=policy.Egress(allow=[policy.Rule(host="host.docker.internal", port=8000)]))
    assert policy.egress_rules(p) == "host.docker.internal 8000\n"


def test_egress_rules_container_resolved_mixed() -> None:
    # A host.docker.internal rule must not poison an adjacent rule with a stray
    # refresh comment; literal IPs stay comment-free too.
    p = policy.Policy(
        egress=policy.Egress(
            allow=[
                policy.Rule(host="host.docker.internal", port=8000),
                policy.Rule(host="1.2.3.4", port=443),
            ]
        )
    )
    out = policy.egress_rules(p)
    for line in out.splitlines():
        if "host.docker.internal" in line:
            assert not line.startswith("#"), f"container-resolved host got a comment: {line!r}"
    assert "host.docker.internal 8000" in out
    assert "1.2.3.4 443" in out


def test_egress_rules_ports_list_fanout() -> None:
    p = policy.Policy(
        egress=policy.Egress(allow=[policy.Rule(host="10.0.0.1", ports=[80, 443, 8080])])
    )
    assert policy.egress_rules(p) == "10.0.0.1 80\n10.0.0.1 443\n10.0.0.1 8080\n"


def test_egress_rules_dns_resolution_real() -> None:
    # Hits real DNS — intentional. Skip rather than fail if offline.
    p = policy.Policy(egress=policy.Egress(allow=[policy.Rule(host="localhost", port=80)]))
    try:
        out = policy.egress_rules(p)
    except policy.PolicyError:
        pytest.skip("DNS resolution failed (likely offline)")
    assert out, "expected non-empty rules"


def test_egress_rules_dns_failure_is_error() -> None:
    p = policy.Policy(
        egress=policy.Egress(
            allow=[policy.Rule(host="this-host-does-not-exist.invalid", port=443)]
        )
    )
    with pytest.raises(policy.PolicyError):
        policy.egress_rules(p)


def test_egress_rules_port_out_of_range() -> None:
    p = policy.Policy(egress=policy.Egress(allow=[policy.Rule(host="1.2.3.4", port=99999)]))
    with pytest.raises(policy.PolicyError):
        policy.egress_rules(p)


def test_egress_rules_no_port() -> None:
    p = policy.Policy(egress=policy.Egress(allow=[policy.Rule(host="1.2.3.4")]))
    with pytest.raises(policy.PolicyError):
        policy.egress_rules(p)


def test_egress_rules_empty_policy_returns_empty() -> None:
    assert policy.egress_rules(policy.Policy()) == ""


def test_egress_rules_none_is_empty() -> None:
    assert policy.egress_rules(None) == ""


def test_egress_rules_dns_host_gets_refresh_comment() -> None:
    # A DNS-resolved hostname emits a "# host: <name>" comment first (for the
    # periodic DNS refresh script). localhost resolves to 127.0.0.1/::1 offline.
    p = policy.Policy(egress=policy.Egress(allow=[policy.Rule(host="localhost", port=80)]))
    try:
        out = policy.egress_rules(p)
    except policy.PolicyError:
        pytest.skip("DNS resolution failed (likely offline)")
    assert out.splitlines()[0] == "# host: localhost"


# --- resolve_tier -------------------------------------------------------------


def test_resolve_tier_no_override() -> None:
    assert policy.resolve_tier(policy.Policy(), "bridged") == "bridged"
    assert policy.resolve_tier(None, "isolated") == "isolated"


def test_resolve_tier_override_wins() -> None:
    p = policy.Policy(sandbox=policy.SandboxSpec(tier="isolated"))
    assert policy.resolve_tier(p, "bridged") == "isolated"


# --- host_setup + sandbox.build -----------------------------------------------


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
    p = policy.load("none", _write_policy(tmp_path, "none", "sandbox:\n  tier: isolated\n"))
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
    p = policy.load("nobuild", _write_policy(tmp_path, "nobuild", "sandbox:\n  image: foo:latest\n"))
    assert policy.build_spec(p) is None


def test_build_spec_no_dockerfile_is_none(tmp_path: Path) -> None:
    # A build mapping with no dockerfile == no build requested.
    p = policy.load("empty", _write_policy(tmp_path, "empty", "sandbox:\n  build:\n    context: orgs/x\n"))
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
    from pux_harness.agent import contract
    assert "host_setup" in contract.KNOWN_POLICY_SECTIONS


# --- shipped policies (integration) ------------------------------------------


def test_shipped_policies_parse_cleanly() -> None:
    """Every orgs/<name>/policy.yaml shipped in the repo loads + resolves
    through the new engine — catches drift between the Go schema and the YAML
    operators actually write. Mirrors Go's TestLoad_ShippedPolicies."""
    repo_root = Path(__file__).resolve().parents[2]
    orgs_dir = repo_root / "orgs"
    if not orgs_dir.is_dir():
        pytest.skip(f"no orgs dir at {orgs_dir} — running outside repo?")
    count = 0
    for org_dir in sorted(orgs_dir.iterdir()):
        if not org_dir.is_dir():
            continue
        if not (org_dir / "policy.yaml").is_file():
            continue
        count += 1
        p = policy.load(org_dir.name, repo_root)
        # Go's TestLoad_ShippedPolicies does `_ = ValidateEnv(p)` — it discards
        # the missing-creds error (the test env legitimately lacks ALPACA keys
        # etc.) and only asserts the call doesn't blow up on the shipped schema.
        # MissingCreds is that expected signal; a PolicyError (schema bug) still
        # fails the test.
        try:
            policy.validate_env(p)
        except policy.MissingCreds:
            pass
        # ResolveMounts must not error on shipped policies — none use ${VAR}
        # placeholders (the only occurrence is commented in _demo).
        policy.resolve_mounts(p)
        # EgressRules resolves DNS; shipped policies may cite host-side services
        # (host.docker.internal sentinel, or real hosts) — log, don't fail on DNS.
        try:
            policy.egress_rules(p)
        except policy.PolicyError:
            pass
    if count == 0:
        pytest.skip("no shipped policy.yaml files found")
