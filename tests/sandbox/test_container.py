"""Parity tests for the Python sandbox lifecycle port.

Mirrors the spirit of the Go ``backend/internal/sandbox`` unit tests
(``runtime_test.go``, ``cache_test.go``, ``defaults_test.go``) — the pure
decision functions don't need a Docker daemon. The create/destroy path is
proven end-to-end by ``pux sandbox start`` against a live Docker (see the
verify log), not asserted here.
"""

from __future__ import annotations

import hashlib
import os

import pytest

from pux_harness.sandbox import container as C


# --- resolve_runtime (port of runtime.go::resolveRuntime) ---------------------


@pytest.mark.parametrize(
    "tier,env,runsc,want",
    [
        # Bridged never overrides — runsc + NET_HOST + Xvfb is untested.
        ("bridged", "", True, None),
        ("bridged", "runsc", True, None),
        ("bridged", "none", True, None),
        ("bridged", "kata-runtime", True, None),
        # env "none" = explicit opt-out.
        ("isolated", "none", True, None),
        ("isolated", "none", False, None),
        # env set (non-none) wins regardless of tier/runsc.
        ("isolated", "runsc", False, "runsc"),
        ("isolated", "kata-runtime", True, "kata-runtime"),
        # env unset + isolated + runsc present → default-on runsc.
        ("isolated", "", True, "runsc"),
        # env unset + isolated + runsc absent → runc.
        ("isolated", "", False, None),
        # native tier is not isolated → no default-on.
        ("native", "", True, None),
    ],
)
def test_resolve_runtime(tier, env, runsc, want, monkeypatch):
    if env == "":
        monkeypatch.delenv("PUX_SANDBOX_RUNTIME", raising=False)
    else:
        monkeypatch.setenv("PUX_SANDBOX_RUNTIME", env)
    assert C.resolve_runtime(tier, runsc) == want


# --- cache_volume_name (port of cache.go::cacheVolumeName) --------------------


def test_cache_volume_name_deterministic():
    p = "/proj/any-project"
    expect = "pux-cache-" + hashlib.sha256(os.path.abspath(p).encode()).hexdigest()[:16]
    assert C.cache_volume_name(p) == expect


def test_cache_volume_name_matches_live_container(monkeypatch):
    """The cache volume name for the REAL project path is stable and matches
    what the live sandbox container uses. Deriving from resolve_project_path()
    (not a hardcoded absolute path) keeps this correct on any host — the old
    assertion pinned the ubuntu-cloud hash and went stale on the first move.

    When Docker is reachable, we cross-check against the actual volume that the
    live container is bind-mounted with (the ground truth). Without Docker we
    still prove the name is the deterministic sha256 of the resolved path."""
    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    real_path = C.resolve_project_path()
    expected = "pux-cache-" + hashlib.sha256(os.path.abspath(real_path).encode()).hexdigest()[:16]
    assert C.cache_volume_name(real_path) == expected

    # Cross-check against the live Docker volume if a daemon is present.
    try:
        import docker  # noqa: PLC0415
        client = docker.from_env()
        volumes = [v.name for v in client.volumes.list(filters={"name": "pux-cache-"})]
    except Exception:  # noqa: BLE001 — no docker daemon in CI / dev-laptop
        return
    assert expected in volumes, (
        f"live docker has no volume {expected!r} for path {real_path!r}; "
        f"present: {volumes[:5]}"
    )


def test_cache_volume_name_differs_per_project():
    a = C.cache_volume_name("/proj/a")
    b = C.cache_volume_name("/proj/b")
    assert a != b
    assert a.startswith("pux-cache-") and b.startswith("pux-cache-")


def test_cache_enabled_default(monkeypatch):
    monkeypatch.delenv("PUX_CACHE_VOLUME", raising=False)
    assert C.cache_enabled() is True


def test_cache_disabled(monkeypatch):
    monkeypatch.setenv("PUX_CACHE_VOLUME", "off")
    assert C.cache_enabled() is False


def test_cache_disabled_only_exact_off(monkeypatch):
    monkeypatch.setenv("PUX_CACHE_VOLUME", "false")  # not the sentinel
    assert C.cache_enabled() is True


# --- env-int defaults (port of defaults.go::resolveResourceDefaults) ----------


def test_env_int_default(monkeypatch):
    monkeypatch.delenv("PUX_SANDBOX_MEMORY_MB", raising=False)
    assert C._env_int("PUX_SANDBOX_MEMORY_MB", 2048) == 2048


def test_env_int_override(monkeypatch):
    monkeypatch.setenv("PUX_SANDBOX_MEMORY_MB", "4096")
    assert C._env_int("PUX_SANDBOX_MEMORY_MB", 2048) == 4096


def test_env_int_invalid_falls_back(monkeypatch):
    monkeypatch.setenv("PUX_SANDBOX_MEMORY_MB", "not-a-number")
    assert C._env_int("PUX_SANDBOX_MEMORY_MB", 2048) == 2048


def test_env_int_nonpositive_falls_back(monkeypatch):
    monkeypatch.setenv("PUX_SANDBOX_MEMORY_MB", "0")
    assert C._env_int("PUX_SANDBOX_MEMORY_MB", 2048) == 2048


def test_env_float_default(monkeypatch):
    monkeypatch.delenv("PUX_SANDBOX_CPU_CORES", raising=False)
    assert C._env_float("PUX_SANDBOX_CPU_CORES", 2.0) == 2.0


def test_env_float_override(monkeypatch):
    monkeypatch.setenv("PUX_SANDBOX_CPU_CORES", "4.5")
    assert C._env_float("PUX_SANDBOX_CPU_CORES", 2.0) == 4.5


# --- resolve_project_path -----------------------------------------------------


def test_resolve_project_path_default(monkeypatch):
    monkeypatch.delenv("PUX_PROJECT_PATH", raising=False)
    p = C.resolve_project_path()
    assert p.startswith("/") and "auto-developer-orchestrator" in p


def test_resolve_project_path_override(monkeypatch):
    monkeypatch.setenv("PUX_PROJECT_PATH", "/tmp/some-project")
    assert C.resolve_project_path() == "/tmp/some-project"


def test_resolve_project_path_rejects_url(monkeypatch):
    monkeypatch.setenv("PUX_PROJECT_PATH", "ssh://user@host/path")
    with pytest.raises(C.ContainerError, match="local filesystem path"):
        C.resolve_project_path()


# --- _build_env / _build_binds against a no-policy org ------------------------
# Uses a SandboxContainer with org="" → no policy → the default env/binds that
# match the live Go-managed container byte-for-byte.


def test_build_env_no_policy_defaults():
    sb = C.SandboxContainer(org="", project_path="/proj")
    env = sb._build_env(None)
    assert env[0] == "SANDBOX_POLICY=developer"
    assert "DOCKER_HOST=unix:///var/run/docker.sock" in env
    assert "HOST_GATEWAY=host.docker.internal" in env
    # serve-reach passthroughs so in-sandbox warmup/probes know where serve lives
    assert any(x.startswith("PUX_API_HOST=") for x in env)
    assert any(x.startswith("PUX_API_PORT=") for x in env)


def test_build_env_no_policy_has_no_creds():
    sb = C.SandboxContainer(org="", project_path="/proj")
    env = sb._build_env(None)
    # No policy → only the 7 base vars (SANDBOX_POLICY, DOCKER_HOST,
    # HOST_GATEWAY, PUX_API_HOST, PUX_API_PORT, UV_PROJECT_ENVIRONMENT,
    # UV_CACHE_DIR), no injected creds/cookies. The two UV_* vars redirect
    # in-sandbox uv invocations to container-local paths so the host .venv
    # cannot be clobbered — see _build_env docstring.
    assert len(env) == 7
    assert any(x.startswith("UV_PROJECT_ENVIRONMENT=/sandbox/") for x in env), (
        "UV_PROJECT_ENVIRONMENT must point inside /sandbox/ (container-local), "
        "not /sandbox/workspace/ (the bind-mount) — else uv sync inside the "
        "sandbox clobbers the host .venv"
    )
    assert any(x.startswith("UV_CACHE_DIR=/sandbox/") for x in env)
    # positively: no API key / cookie / token vars leak in without a policy
    assert not any(
        any(tok in x for tok in ("API_KEY", "TOKEN", "COOKIE", "SECRET", "PASSWORD"))
        for x in env
    )


def test_build_env_dead_vars_absent():
    """The legacy NETWORK_ALLOW / FS_READONLY / FS_READWRITE env vars were
    removed — nothing in the image or harness reads them (grep-verified
    against the live container 2026-07-11). Their presence misled operators
    into thinking egress/filesystem protection was active when it wasn't."""
    sb = C.SandboxContainer(org="", project_path="/proj")
    env = sb._build_env(None)
    for var in ("NETWORK_ALLOW", "FS_READONLY", "FS_READWRITE"):
        assert not any(x.startswith(f"{var}=") for x in env), (
            f"dead env var {var} leaked into container env"
        )


# --- prepare(universal_warmup=) — serve path spins up the event warmup for
# EVERY sandbox, even NoPolicy orgs, so a webhook-less client (Hermes) can
# observe completions. direct path (default) skips it. Proven with a fake exec
# client — no Docker. ---------------------------------------------------------


def _raise_no_policy(org, project_path):  # noqa: ANN001
    raise C.policy.NoPolicy


class _RecordingExec:
    """Fake DockerExecClient: records exec() commands, returns (stdout, rc)."""

    def __init__(self, *, out: str = "warmup_webhook: OK", rc: int = 0) -> None:
        self.calls: list[str] = []
        self._out, self._rc = out, rc

    def exec(self, command, *, timeout=None):  # noqa: ANN001
        self.calls.append(command)
        return self._out, self._rc


class _ExplodingExec:
    def exec(self, command, *, timeout=None):  # noqa: ANN001
        raise OSError("exec crashed")


def test_prepare_universal_warmup_runs_even_for_no_policy_org(monkeypatch):
    """serve path (universal_warmup=True): the warmup runs for EVERY org,
    including ones with no policy at all — that is the 'ALL sandboxes' guarantee.
    The historical no-specs short-circuit is bypassed only when requested."""
    monkeypatch.setattr(C.policy, "load", _raise_no_policy)
    exec_client = _RecordingExec()
    results = C.prepare("nopolicy-org", project_path="/proj",
                        exec_client=exec_client, universal_warmup=True)
    assert len(results) == 1, results
    assert results[0]["name"] == "warmup_webhook"
    assert results[0]["status"] == "ok"
    assert any("warmup_webhook.py" in c for c in exec_client.calls)


def test_prepare_universal_warmup_records_failure_nonfatally(monkeypatch):
    """An unreachable serve (rc!=0) surfaces as status=failed WITHOUT raising —
    prep must never block the agent run, the runner just logs the warning."""
    monkeypatch.setattr(C.policy, "load", _raise_no_policy)
    exec_client = _RecordingExec(out="warmup_webhook: UNREACHABLE", rc=1)
    results = C.prepare("nopolicy-org", project_path="/proj",
                        exec_client=exec_client, universal_warmup=True)
    assert results[0]["status"] == "failed"
    assert "UNREACHABLE" in results[0]["error"]


def test_prepare_universal_warmup_swallows_exec_crash(monkeypatch):
    """An exec() that RAISES is caught — warmup reports failed, run continues."""
    monkeypatch.setattr(C.policy, "load", _raise_no_policy)
    results = C.prepare("nopolicy-org", project_path="/proj",
                        exec_client=_ExplodingExec(), universal_warmup=True)
    assert results[0]["name"] == "warmup_webhook"
    assert results[0]["status"] == "failed"
    assert "exec crashed" in results[0]["error"]


def test_prepare_default_does_not_run_universal_warmup(monkeypatch):
    """direct path (universal_warmup=False default): a NoPolicy org returns []
    IMMEDIATELY and never calls exec — historical behavior preserved."""

    class _BoomExec:
        def exec(self, *a, **kw):  # noqa: ANN002, ANN003
            raise AssertionError("exec must not run without universal_warmup")

    monkeypatch.setattr(C.policy, "load", _raise_no_policy)
    assert C.prepare("nopolicy-org", project_path="/proj",
                     exec_client=_BoomExec()) == []


# --- host_setup ordering in create() ----------------------------------------
# create() must run host_setup BEFORE validate_env, so creds the hooks produce
# (e.g. TWITTER_COOKIES_B64) are visible to the existing cred/cookie chain.
# Proven via a recorder: host_setup records + returns {}, validate_env records
# + raises a sentinel — so create() aborts before touching Docker, and the
# recorder shows the order.


class _AbortBeforeDocker(Exception):
    pass


def test_create_runs_host_setup_before_validate_env(monkeypatch):
    order: list[str] = []

    def fake_host_setup(pol, root):
        order.append("host_setup")
        return {}  # no exports → os.environ.update skipped, validate_env still runs

    def fake_validate(pol, env=None):
        order.append("validate_env")
        raise _AbortBeforeDocker  # stop create() before any Docker call

    monkeypatch.setattr(C.host_setup, "run_host_setup", fake_host_setup)
    monkeypatch.setattr(C.policy, "validate_env", fake_validate)

    sb = C.SandboxContainer(org="x", project_path="/proj")
    monkeypatch.setattr(
        sb,
        "_resolve_policy",
        lambda: (
            C.policy.Policy(
                host_setup=[
                    C.policy.HostSetupHook(name="h", helper_script="x.py", exports={"X": "stdout"})
                ]
            ),
            "isolated",
        ),
    )
    with pytest.raises(_AbortBeforeDocker):
        sb.create()
    assert order == ["host_setup", "validate_env"]


def test_create_exports_flow_into_environ(monkeypatch):
    # host_setup's exports are os.environ.update'd so the cred/cookie chain sees
    # them. Proven by capturing os.environ inside fake_validate.
    seen: dict[str, str] = {}

    def fake_host_setup(pol, root):
        return {"TWITTER_COOKIES_B64": "FROM-HOOK"}

    def fake_validate(pol, env=None):
        seen["TWITTER_COOKIES_B64"] = os.environ.get("TWITTER_COOKIES_B64", "")
        raise _AbortBeforeDocker

    monkeypatch.setattr(C.host_setup, "run_host_setup", fake_host_setup)
    monkeypatch.setattr(C.policy, "validate_env", fake_validate)
    sb = C.SandboxContainer(org="x", project_path="/proj")
    monkeypatch.setattr(
        sb,
        "_resolve_policy",
        lambda: (C.policy.Policy(host_setup=[C.policy.HostSetupHook(name="h")]), "isolated"),
    )
    with pytest.raises(_AbortBeforeDocker):
        sb.create()
    assert seen["TWITTER_COOKIES_B64"] == "FROM-HOOK"


class _FakeImages:
    def __init__(self, *, present=False):
        self.present = present
        self.built: dict = {}
        self.pulled: list[str] = []

    def get(self, image):
        if self.present:
            return object()
        raise C.ImageNotFound(image)

    def build(self, *, path, dockerfile, tag):
        self.built = dict(path=path, dockerfile=dockerfile, tag=tag)

    def pull(self, image):
        self.pulled.append(image)


class _FakeClient:
    def __init__(self, images):
        self.images = images


# --- bridged tier DISPLAY passthrough removal ---------------------------------


def test_bridged_tier_does_not_inject_host_display(monkeypatch):
    """Even with host DISPLAY=:0 set, the bridged tier must not pass it through
    to the container env or mount /tmp/.X11-unix. The container runs its own
    Xvfb on :99 (Dockerfile ENV DISPLAY=:99)."""
    monkeypatch.setenv("DISPLAY", ":0")

    # Bypass policy loading, validation, and host_setup.
    monkeypatch.setattr(C.host_setup, "run_host_setup", lambda pol, root: {})
    monkeypatch.setattr(C.policy, "validate_env", lambda pol, env=None: None)
    monkeypatch.setattr(C.policy, "resolve_mounts", lambda pol: [])

    captured: dict = {}

    class _FakeContainers:
        def create(self, **kwargs):
            captured.update(kwargs)
            raise _AbortBeforeDocker  # stop before start()

        def get(self, name):
            raise C.NotFound("no container")

        def list(self, **kwargs):
            return []

    class _FakeClientWithContainers(_FakeClient):
        def __init__(self):
            super().__init__(_FakeImages(present=True))
            self.containers = _FakeContainers()

        def info(self):
            return {}

    sb = C.SandboxContainer(org="x", project_path="/proj", client=_FakeClientWithContainers())
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (None, "bridged"))
    monkeypatch.setattr(sb, "_remove_stale", lambda: None)
    monkeypatch.setattr(sb, "_build_binds", lambda pol: [])
    monkeypatch.setattr(C, "_is_runsc_available", lambda client: False)

    with pytest.raises(_AbortBeforeDocker):
        sb.create()

    env_list = captured.get("environment", [])
    # No DISPLAY= entry should be injected by the harness.
    display_entries = [e for e in env_list if e.startswith("DISPLAY=")]
    assert display_entries == [], f"bridged tier injected host DISPLAY: {display_entries}"
    # No /tmp/.X11-unix mount.
    volumes = captured.get("volumes", [])
    x11_mounts = [v for v in volumes if ".X11-unix" in v]
    assert x11_mounts == [], f"bridged tier mounted /tmp/.X11-unix: {x11_mounts}"
    # Host networking IS still set for bridged.
    assert captured.get("network_mode") == "host"


def test_ensure_image_builds_when_spec_set_and_absent():
    images = _FakeImages(present=False)
    sb = C.SandboxContainer(org="x", project_path="/proj", client=_FakeClient(images))
    build = C.policy.BuildSpec(
        dockerfile="orgs/specialists/video-production/Dockerfile",
        context="orgs/specialists/video-production",
    )
    sb._ensure_image("vp:latest", build)
    assert images.built["tag"] == "vp:latest"
    assert images.built["dockerfile"] == "Dockerfile"  # basename, relative to ctx
    assert images.built["path"] == os.path.join("/proj", "orgs/specialists/video-production")
    assert images.pulled == []  # build, not pull


def test_ensure_image_context_defaults_to_dockerfile_dir():
    images = _FakeImages(present=False)
    sb = C.SandboxContainer(org="x", project_path="/proj", client=_FakeClient(images))
    # No context → defaults to the Dockerfile's parent, resolved project-relative.
    build = C.policy.BuildSpec(dockerfile="orgs/specialists/video-production/Dockerfile")
    sb._ensure_image("vp:latest", build)
    assert images.built["path"] == os.path.join("/proj", "orgs/specialists/video-production")
    assert images.built["dockerfile"] == "Dockerfile"


def test_ensure_image_present_skips_build_and_pull():
    images = _FakeImages(present=True)
    sb = C.SandboxContainer(org="x", project_path="/proj", client=_FakeClient(images))
    sb._ensure_image("vp:latest", C.policy.BuildSpec(dockerfile="x/Dockerfile"))
    assert images.built == {}
    assert images.pulled == []


def test_ensure_image_pulls_when_no_build_spec():
    images = _FakeImages(present=False)
    sb = C.SandboxContainer(org="x", project_path="/proj", client=_FakeClient(images))
    sb._ensure_image("pux-sandbox:latest", None)
    assert images.pulled == ["pux-sandbox:latest"]
    assert images.built == {}


# --- sandbox.display watchable desktop (OpenComputer value extraction P1) -----
# display_port() + _resolve_watch_url() are pure decisions over a (container,
# DisplaySpec, tier) triple — no Docker daemon. The full port-publish path
# (create_kwargs["ports"]) is proven LIVE by `pux sandbox ensure` + curl
# against the pux-sandbox image (the image already runs the noVNC stack under
# supervisord); not asserted here, matching this file's create-path philosophy.

from types import SimpleNamespace  # noqa: E402


def _fake_container(ports: dict | None = None):
    """Stand-in docker container for ``_resolve_watch_url`` (reload/ports only)."""
    return SimpleNamespace(reload=lambda *a, **k: None, ports=ports or {})


def test_display_port_per_backend():
    assert C.display_port("standard") == 6080
    assert C.display_port("kasm") == 8444
    assert C.display_port("unknown") == 6080  # safe fallback to the noVNC port


def test_resolve_watch_url_none_when_watch_off():
    sb = C.SandboxContainer(org="", project_path="/proj")
    # disp=None (watch off) -> no URL regardless of container/tier.
    assert sb._resolve_watch_url(_fake_container(), None, "isolated") is None


def test_resolve_watch_url_isolated_reads_published_port():
    sb = C.SandboxContainer(org="", project_path="/proj")
    disp = C.policy.DisplaySpec(watch=True, backend="standard")
    # docker published 6080/tcp -> 127.0.0.1:49152 (ephemeral host port).
    c = _fake_container({"6080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "49152"}]})
    assert sb._resolve_watch_url(c, disp, "isolated") == "http://127.0.0.1:49152/vnc.html"


def test_resolve_watch_url_bridged_uses_container_port_directly():
    sb = C.SandboxContainer(org="", project_path="/proj")
    disp = C.policy.DisplaySpec(watch=True, backend="standard")
    # host networking: no published binding; the container's 6080 IS the host's.
    assert (
        sb._resolve_watch_url(_fake_container(), disp, "bridged")
        == "http://127.0.0.1:6080/vnc.html"
    )


def test_resolve_watch_url_kasm_is_tls_root_path():
    sb = C.SandboxContainer(org="", project_path="/proj")
    disp = C.policy.DisplaySpec(watch=True, backend="kasm")
    c = _fake_container({"8444/tcp": [{"HostIp": "127.0.0.1", "HostPort": "49153"}]})
    # KasmVNC serves its own TLS web client on the port (https, root path).
    assert sb._resolve_watch_url(c, disp, "isolated") == "https://127.0.0.1:49153/"


def test_resolve_watch_url_isolated_no_binding_returns_none():
    sb = C.SandboxContainer(org="", project_path="/proj")
    disp = C.policy.DisplaySpec(watch=True, backend="standard")
    # Port declared but not yet published (queried mid-start) -> None, no crash.
    assert sb._resolve_watch_url(_fake_container(), disp, "isolated") is None


def test_watch_url_none_when_display_watch_off(monkeypatch):
    # Default Policy (no display block) -> watch_url None, no docker lookup.
    sb = C.SandboxContainer(org="x", project_path="/proj")
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (C.policy.Policy(), "isolated"))
    assert sb.watch_url is None


def test_watch_url_returns_create_cache_without_docker():
    # After create() caches _watch_url, the property short-circuits — proven by
    # not needing any client/container to resolve it.
    sb = C.SandboxContainer(org="", project_path="/proj")
    sb._watch_url = "http://127.0.0.1:9999/vnc.html"
    assert sb.watch_url == "http://127.0.0.1:9999/vnc.html"


# --- security hardening (sandbox audit 2026-07-11) ---------------------------
# Two gaps surfaced by the live-container audit are closed here:
#   1. no-new-privileges is injected into every container's security_opt
#   2. ensure() fail-closes when a reused container lacks NET_ADMIN but the
#      policy declares an egress.allow block (deny-by-default can't install)
# The dead NETWORK_ALLOW / FS_READONLY / FS_READWRITE env vars are also gone
# (test_build_env_dead_vars_absent above) — they were vestigial from the Go
# entrypoint and misled operators into thinking egress/filesystem protection
# was active when nothing consumed them.


def test_create_injects_no_new_privileges(monkeypatch):
    """Every container gets security_opt=['no-new-privileges:true'] — blocks
    setuid/setcap escalation from inside the container."""
    monkeypatch.setattr(C.host_setup, "run_host_setup", lambda pol, root: {})
    monkeypatch.setattr(C.policy, "validate_env", lambda pol, env=None: None)
    monkeypatch.setattr(C.policy, "resolve_mounts", lambda pol: [])

    captured: dict = {}

    class _FakeContainers:
        def create(self, **kwargs):
            captured.update(kwargs)
            raise _AbortBeforeDocker

        def get(self, name):
            raise C.NotFound("no")

        def list(self, **kwargs):
            return []

    class _Client(_FakeClient):
        def __init__(self):
            super().__init__(_FakeImages(present=True))
            self.containers = _FakeContainers()

        def info(self):
            return {}

    sb = C.SandboxContainer(org="", project_path="/proj", client=_Client())
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (None, "isolated"))
    monkeypatch.setattr(sb, "_remove_stale", lambda: None)
    monkeypatch.setattr(sb, "_build_binds", lambda pol: [])
    monkeypatch.setattr(C, "_is_runsc_available", lambda client: False)

    with pytest.raises(_AbortBeforeDocker):
        sb.create()

    assert captured.get("security_opt") == ["no-new-privileges:true"]


def test_create_no_new_privileges_with_egress_caps(monkeypatch, tmp_path):
    """When egress.allow is declared, the container gets BOTH no-new-privileges
    AND NET_ADMIN — they don't conflict."""
    monkeypatch.setattr(C.host_setup, "run_host_setup", lambda pol, root: {})
    monkeypatch.setattr(C.policy, "validate_env", lambda pol, env=None: None)
    monkeypatch.setattr(C.policy, "resolve_mounts", lambda pol: [])
    monkeypatch.setattr(
        C.policy, "egress_rules",
        lambda pol: "1.2.3.4 443\n",
    )

    captured: dict = {}

    class _FakeContainers:
        def create(self, **kwargs):
            captured.update(kwargs)
            raise _AbortBeforeDocker

        def get(self, name):
            raise C.NotFound("no")

        def list(self, **kwargs):
            return []

    class _Client(_FakeClient):
        def __init__(self):
            super().__init__(_FakeImages(present=True))
            self.containers = _FakeContainers()

        def info(self):
            return {}

    pol = C.policy.Policy(
        egress=C.policy.Egress(allow=[C.policy.Rule(host="example.com", port=443)]),
    )
    sb = C.SandboxContainer(org="x", project_path=str(tmp_path), client=_Client())
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (pol, "isolated"))
    monkeypatch.setattr(sb, "_remove_stale", lambda: None)
    monkeypatch.setattr(sb, "_build_binds", lambda pol: [])
    monkeypatch.setattr(C, "_is_runsc_available", lambda client: False)

    with pytest.raises(_AbortBeforeDocker):
        sb.create()

    assert captured.get("security_opt") == ["no-new-privileges:true"]
    assert "NET_ADMIN" in captured.get("cap_add", [])
    # egress.conf was actually staged to disk (real write, not just env var)
    assert (tmp_path / ".pux" / "egress.conf").read_text().strip() == "1.2.3.4 443"


# --- ensure() fail-closed egress validation ---------------------------------


class _ReuseClient:
    """Fake DockerClient for ensure() reuse tests. Returns a running container
    with configurable HostConfig.CapAdd."""

    def __init__(self, cap_add: list[str] | None):
        self._cap_add = cap_add

    @property
    def containers(self):
        return self

    def list(self, *, filters=None, status=None, **kw):
        return [_FakeReuseContainer(self._cap_add)]

    def get(self, name):
        return _FakeReuseContainer(self._cap_add)


class _FakeReuseContainer:
    """Minimal container stub for ensure() validation."""

    def __init__(self, cap_add: list[str] | None):
        self.name = "orchestrator-sandbox-test"
        self.status = "running"
        self.attrs = {"HostConfig": {"CapAdd": cap_add}}

    def reload(self, *a, **kw):
        pass


def test_ensure_rejects_reuse_without_net_admin_when_egress_declared(monkeypatch):
    """Fail-closed: a reused container lacking NET_ADMIN but whose org policy
    declares egress.allow is REJECTED — the deny-by-default firewall can't be
    installed without the cap, so reusing it means unrestricted egress."""
    pol = C.policy.Policy(
        egress=C.policy.Egress(allow=[C.policy.Rule(host="example.com", port=443)]),
    )
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (pol, "isolated"))
    with pytest.raises(C.ContainerError, match="lacks NET_ADMIN"):
        sb.ensure()


def test_ensure_rejects_reuse_with_empty_capadd_when_egress_declared(monkeypatch):
    """CapAdd=[] (not just None) also fails — the container was created without
    the cap either way."""
    pol = C.policy.Policy(
        egress=C.policy.Egress(allow=[C.policy.Rule(host="example.com", port=443)]),
    )
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=[]))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (pol, "isolated"))
    with pytest.raises(C.ContainerError, match="lacks NET_ADMIN"):
        sb.ensure()


def test_ensure_accepts_reuse_with_net_admin_when_egress_declared(monkeypatch):
    """Container with NET_ADMIN + egress.allow → reuse is fine."""
    pol = C.policy.Policy(
        egress=C.policy.Egress(allow=[C.policy.Rule(host="example.com", port=443)]),
    )
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=["NET_ADMIN"]))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (pol, "isolated"))
    name = sb.ensure()
    assert name == "orchestrator-sandbox-test"


def test_ensure_accepts_reuse_without_net_admin_when_no_egress(monkeypatch):
    """No egress.allow block → no NET_ADMIN needed → reuse any container.
    This is the default (egress is opt-in via policy.yaml)."""
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (C.policy.Policy(), "isolated"))
    name = sb.ensure()
    assert name == "orchestrator-sandbox-test"


def test_ensure_accepts_reuse_without_net_admin_when_bridged(monkeypatch):
    """Bridged tier (host networking) skips iptables — NET_ADMIN is never
    granted and never required, even with an egress.allow block."""
    pol = C.policy.Policy(
        egress=C.policy.Egress(allow=[C.policy.Rule(host="example.com", port=443)]),
    )
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (pol, "bridged"))
    name = sb.ensure()
    assert name == "orchestrator-sandbox-test"


def test_ensure_accepts_reuse_without_net_admin_when_no_policy(monkeypatch):
    """No policy at all → no egress → any container is fine."""
    sb = C.SandboxContainer(org="", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (None, "isolated"))
    name = sb.ensure()
    assert name == "orchestrator-sandbox-test"


def test_ensure_creates_when_no_running_container(monkeypatch):
    """No running container → create() is called (not reuse). Proven by
    create() aborting via _AbortBeforeDocker before returning a name."""

    def _boom_create():
        raise _AbortBeforeDocker

    sb = C.SandboxContainer(org="", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (None, "isolated"))
    monkeypatch.setattr(sb, "_running_for_project", lambda: None)
    monkeypatch.setattr(sb, "create", _boom_create)

    with pytest.raises(_AbortBeforeDocker):
        sb.ensure()


# --- ensure() network-health self-healing ------------------------------------
# A reused container can look healthy (Status=running, all processes up) but
# have broken networking — a docker-daemon veth-attachment race where packets
# never reach the bridge. The agent experiences this as "browser unavailable"
# because sb_server can't load any page. ensure() must detect this via the
# gateway reachability probe and AUTO-RECREATE before the agent sees the
# failure — this is the "browser must NEVER fail to launch" contract.


def test_network_healthy_returns_true_when_gateway_unreachable_in_attrs():
    """If the container attrs lack a gateway, the probe can't run. Return
    True (assume healthy) — we only act on POSITIVE evidence of breakage,
    never on missing data. This also covers old test stubs whose fake
    containers don't carry NetworkSettings."""
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    # _FakeReuseContainer.attrs has only HostConfig — no NetworkSettings →
    # no gateway → _network_healthy returns True.
    assert sb._network_healthy("orchestrator-sandbox-test") is True


def test_network_healthy_returns_true_when_exec_unsupported(monkeypatch):
    """If exec_run raises (mock without it, daemon unreachable, etc.) we
    treat the container as healthy — false-negative is far better than
    false-positive here (a false broken verdict churns a good container)."""
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))

    class _FakeCWithGateway:
        attrs = {
            "NetworkSettings": {
                "Networks": {"shared-infra": {"Gateway": "10.100.23.1"}}
            }
        }
        # No exec_run attribute → AttributeError on call.

    monkeypatch.setattr(sb.client.containers, "get", lambda name: _FakeCWithGateway())
    assert sb._network_healthy("orchestrator-sandbox-test") is True


def test_network_healthy_returns_false_on_probe_timeout(monkeypatch):
    """The DEFINITIVE breakage signal: the gateway TCP probe times out
    (exit 124 from `timeout`). This is exactly the docker veth race symptom
    — packets leave eth0 but never appear on the bridge."""
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))

    class _R:
        exit_code = 124  # timeout(1) killed the probe
        output = b""

    class _FakeCWithGateway:
        attrs = {
            "NetworkSettings": {
                "Networks": {"shared-infra": {"Gateway": "10.100.23.1"}}
            }
        }
        def exec_run(self, cmd, **kw):
            return _R()

    monkeypatch.setattr(sb.client.containers, "get", lambda name: _FakeCWithGateway())
    assert sb._network_healthy("orchestrator-sandbox-test") is False


def test_network_healthy_returns_true_on_connection_refused(monkeypatch):
    """Gateway probe gets 'connection refused' (exit 1) — the TCP SYN reached
    the gateway and got a RST back. Networking is FINE; nothing listens on
    :53. This must NOT trigger a recreate."""
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))

    class _R:
        exit_code = 1  # connection refused — network works
        output = b""

    class _FakeCWithGateway:
        attrs = {
            "NetworkSettings": {
                "Networks": {"shared-infra": {"Gateway": "10.100.23.1"}}
            }
        }
        def exec_run(self, cmd, **kw):
            return _R()

    monkeypatch.setattr(sb.client.containers, "get", lambda name: _FakeCWithGateway())
    assert sb._network_healthy("orchestrator-sandbox-test") is True


def test_ensure_force_removes_reused_container_with_broken_network(monkeypatch):
    """THE CORE CONTRACT: a reused container whose gateway probe times out
    is force-removed and a fresh one is created. The agent never sees
    "browser unavailable" — the harness self-heals before any tool call
    reaches the broken container."""
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (None, "isolated"))

    # Network probe: reused container (name from _FakeReuseContainer) is
    # broken; the fresh one create() returns is healthy.
    def _health(name):
        return name != "orchestrator-sandbox-test"
    monkeypatch.setattr(sb, "_network_healthy", _health)

    removed: list[str] = []
    monkeypatch.setattr(sb, "_force_remove_unhealthy",
                        lambda name: removed.append(name))

    def _fake_create():
        return "orchestrator-sandbox-fresh"
    monkeypatch.setattr(sb, "create", _fake_create)

    name = sb.ensure()
    assert removed == ["orchestrator-sandbox-test"], (
        f"broken reused container must be force-removed; got {removed}"
    )
    assert name == "orchestrator-sandbox-fresh", (
        "ensure() must return the freshly-created healthy container"
    )


def test_ensure_reuses_container_when_network_healthy(monkeypatch):
    """Symmetric: when the probe says the reused container is healthy, NO
    force-remove happens and NO create() is called. This guards against
    false-positives that would churn a working container on every ensure()."""
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (None, "isolated"))
    monkeypatch.setattr(sb, "_network_healthy", lambda name: True)

    removed: list[str] = []
    monkeypatch.setattr(sb, "_force_remove_unhealthy",
                        lambda name: removed.append(name))

    created: list[str] = []
    monkeypatch.setattr(sb, "create",
                        lambda: created.append("x") or "should-not-happen")

    name = sb.ensure()
    assert removed == [], f"healthy container must NOT be removed; got {removed}"
    assert created == [], f"create() must NOT run on healthy reuse; got {created}"
    assert name == "orchestrator-sandbox-test"


def test_ensure_retries_create_when_fresh_container_has_broken_network(monkeypatch):
    """The same race can strike at create() time. ensure() must verify the
    fresh container + retry once. Without this, the agent's FIRST tool call
    after a bad create() would fail."""
    sb = C.SandboxContainer(org="x", project_path="/proj",
                            client=_ReuseClient(cap_add=None))
    monkeypatch.setattr(sb, "_resolve_policy", lambda: (None, "isolated"))
    monkeypatch.setattr(sb, "_running_for_project", lambda: None)

    # First create() returns a name whose probe fails; second succeeds.
    create_calls: list[int] = []

    def _fake_create():
        create_calls.append(1)
        return "orchestrator-sandbox-fresh"  # same name both calls

    monkeypatch.setattr(sb, "create", _fake_create)

    probe_calls: list[str] = []
    def _health(name):
        probe_calls.append(name)
        # First probe (after first create) fails; second probe passes.
        return len(probe_calls) > 1
    monkeypatch.setattr(sb, "_network_healthy", _health)

    removed: list[str] = []
    monkeypatch.setattr(sb, "_force_remove_unhealthy",
                        lambda name: removed.append(name))

    name = sb.ensure()
    assert len(create_calls) == 2, (
        f"create() must be called twice (initial + retry); got {len(create_calls)}"
    )
    assert removed == ["orchestrator-sandbox-fresh"], (
        f"first fresh container must be force-removed; got {removed}"
    )
    assert name == "orchestrator-sandbox-fresh"
