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
    p = "/home/ubuntu/Documents/programs/dev/auto-developer-orchestrator"
    expect = "pux-cache-" + hashlib.sha256(os.path.abspath(p).encode()).hexdigest()[:16]
    assert C.cache_volume_name(p) == expect


def test_cache_volume_name_matches_live_container():
    # Verified against the live container's bind 2026-07-03.
    assert (
        C.cache_volume_name("/home/ubuntu/Documents/programs/dev/auto-developer-orchestrator")
        == "pux-cache-c6f1b24fe47c2162"
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
    assert any(x.startswith("NETWORK_ALLOW=") for x in env)
    assert "FS_READONLY=/etc,/usr,/bin,/lib,/lib64" in env
    assert "FS_READWRITE=/sandbox/workspace,/sandbox/tmp" in env
    assert "DOCKER_HOST=unix:///var/run/docker.sock" in env
    assert "HOST_GATEWAY=host.docker.internal" in env


def test_build_env_no_policy_has_no_creds():
    sb = C.SandboxContainer(org="", project_path="/proj")
    env = sb._build_env(None)
    # No policy → only the 6 base vars, no injected creds/cookies.
    assert len(env) == 6


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
