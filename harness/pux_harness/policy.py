"""Declarative sandbox policy engine (Phase 6 — port of
``backend/internal/policy``).

Pure logic — no Docker, no container state. Loads ``orgs/<name>/policy.yaml``,
validates credentials against the live env, resolves ``${VAR}`` mount
placeholders, and renders the iptables egress allowlist. The harness owns
policy *resolution*; container-side *enforcement* (binds/env/caps/egress.conf
staging) transfers at Phase 8 once the harness owns container creation.

Faithful 1:1 port of the Go package (``policy.go`` / ``validate.go`` /
``egress.go``) so behavior + the parity test gate (``tests/test_policy.py``,
mirroring the 22 Go tests) match. The shipped-policy gate runs against the
same real ``orgs/*/policy.yaml`` files the Go side enforces.

Design notes that survive the port:

- ``NoPolicy`` is a *sentinel* (raised when the org has no policy.yaml) — callers
  MUST branch on it as the "not opted in" path, distinct from ``PolicyError``.
- ``validate_env`` / ``env_vars`` / ``resolve_mounts`` read the operator's live
  ``os.environ`` by default (matching Go's ``os.Getenv``); an explicit ``env``
  param is accepted for tests. Empty-string values are treated as absent, like
  Go's ``os.Getenv``.
- ``egress_rules`` resolves DNS *now* (sandbox-create time), not in-container at
  boot — by the time the firewall runs, DNS may be blocked. Container-resolved
  names (``host.docker.internal``) pass through verbatim: they don't resolve on
  the host but do inside the container via ``/etc/hosts``; the boot script
  resolves them via ``getent`` (no network).
"""
from __future__ import annotations

import ipaddress
import os
import re
import socket
from dataclasses import dataclass, field
from pathlib import Path
from typing import Mapping

import yaml

# ${VAR} placeholder, mirroring validate.go:13.
_PLACEHOLDER_RE = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")

# Docker-internal /etc/hosts entries that do NOT resolve on the host (where
# EgressRules runs) but DO inside the container. Passed through verbatim;
# apply-egress-policy.sh resolves them at boot via getent (no DNS, works under
# deny-by-default). This is how bridge-networked orgs reach host-side services
# (e.g. a shared SurrealDB on the operator's machine) through the firewall.
_CONTAINER_RESOLVED = frozenset({"host.docker.internal"})


# --- exceptions ---------------------------------------------------------------


class NoPolicy(Exception):
    """Sentinel: the org has no ``policy.yaml``. Callers MUST treat this as
    "feature not opted in" (today's behavior), NOT an error."""


class PolicyError(Exception):
    """A real failure: malformed YAML, bad path, unresolvable mount, bad port…"""


class MissingCreds(PolicyError):
    """One or more required credentials absent from the operator env. ``missing``
    lists every absent name so the operator sees the full repair list in one
    shot, not one round-trip per credential."""

    def __init__(self, missing: list[str]) -> None:
        self.missing = list(missing)
        super().__init__("missing required credentials: " + ", ".join(self.missing))


class UnresolvedMount(PolicyError):
    """A ``${VAR}`` placeholder in a mount's ``Host`` has no matching env var.
    Failing loud beats silently mounting the wrong directory."""

    def __init__(self, container: str, unresolved: str, missing_var: str) -> None:
        self.container = container
        self.unresolved = unresolved
        self.missing_var = missing_var
        super().__init__(
            f'mount {container}: host "{unresolved}" references unset env var {missing_var}'
        )


# --- schema (mirrors policy.go structs) ---------------------------------------


@dataclass
class Mount:
    host: str = ""
    container: str = ""
    mode: str = ""  # "rw" (default) or "ro"


@dataclass
class Workspace:
    mounts: list[Mount] = field(default_factory=list)
    run_as_host_user: bool = False


@dataclass
class Rule:
    host: str = ""
    port: int = 0
    ports: list[int] = field(default_factory=list)
    protocol: str = ""  # default tcp; only tcp supported today


@dataclass
class Egress:
    allow: list[Rule] = field(default_factory=list)


@dataclass
class Credentials:
    required: list[str] = field(default_factory=list)
    optional: list[str] = field(default_factory=list)


@dataclass
class SandboxSpec:
    image: str = ""
    tier: str = ""  # "isolated" or "bridged"


@dataclass
class BrowserSpec:
    cookies_env: str = ""


@dataclass
class Policy:
    workspace: Workspace = field(default_factory=Workspace)
    egress: Egress = field(default_factory=Egress)
    credentials: Credentials = field(default_factory=Credentials)
    sandbox: SandboxSpec = field(default_factory=SandboxSpec)
    browser: BrowserSpec = field(default_factory=BrowserSpec)


@dataclass
class ResolvedMount:
    host: str
    container: str
    mode: str  # "rw" or "ro"


# --- YAML -> dataclasses ------------------------------------------------------


def _mount(d: Mapping) -> Mount:
    return Mount(
        host=str(d.get("host", "") or ""),
        container=str(d.get("container", "") or ""),
        mode=str(d.get("mode", "") or ""),
    )


def _rule(d: Mapping) -> Rule:
    ports = d.get("ports") or []
    return Rule(
        host=str(d.get("host", "") or ""),
        port=int(d.get("port", 0) or 0),
        ports=[int(p) for p in ports],
        protocol=str(d.get("protocol", "") or ""),
    )


def _policy_from_dict(d: Mapping) -> Policy:
    """Lenient mapping, like Go's yaml.v3 into structs: unknown keys ignored,
    missing fields default. Raises PolicyError only on a non-mapping section."""
    pol = Policy()

    def _section(name: str) -> Mapping:
        sec = d.get(name)
        if sec is None:
            return {}
        if not isinstance(sec, Mapping):
            raise PolicyError(f"policy: section {name!r} must be a mapping")
        return sec

    ws = _section("workspace")
    pol.workspace = Workspace(
        mounts=[_mount(m) for m in (ws.get("mounts") or []) if isinstance(m, Mapping)],
        run_as_host_user=bool(ws.get("run_as_host_user", False)),
    )
    eg = _section("egress")
    pol.egress = Egress(allow=[_rule(r) for r in (eg.get("allow") or []) if isinstance(r, Mapping)])
    cr = _section("credentials")
    pol.credentials = Credentials(
        required=[str(x) for x in (cr.get("required") or [])],
        optional=[str(x) for x in (cr.get("optional") or [])],
    )
    sb = _section("sandbox")
    pol.sandbox = SandboxSpec(
        image=str(sb.get("image", "") or ""),
        tier=str(sb.get("tier", "") or ""),
    )
    br = _section("browser")
    pol.browser = BrowserSpec(cookies_env=str(br.get("cookies_env", "") or ""))
    return pol


# --- load ---------------------------------------------------------------------


def load(org_name: str, project_root: str | Path) -> Policy:
    """Read ``orgs/<org_name>/policy.yaml`` under ``project_root``. Raises
    ``NoPolicy`` (sentinel) if absent; ``PolicyError`` on a malformed file."""
    if not org_name:
        raise NoPolicy()
    if not project_root:
        raise PolicyError("policy.load: project_root required")
    path = Path(project_root) / "orgs" / org_name / "policy.yaml"
    try:
        data = path.read_text()
    except FileNotFoundError:
        raise NoPolicy()
    except OSError as e:
        raise PolicyError(f"policy.load {path}: {e}") from e
    try:
        parsed = yaml.safe_load(data)
    except yaml.YAMLError as e:
        raise PolicyError(f"policy.load {path}: {e}") from e
    if parsed is None:
        return Policy()
    if not isinstance(parsed, Mapping):
        raise PolicyError(f"policy.load {path}: top-level must be a mapping")
    pol = _policy_from_dict(parsed)
    # Default Protocol to "tcp" on every rule with an empty value (policy.go:141).
    for rule in pol.egress.allow:
        if not rule.protocol:
            rule.protocol = "tcp"
    return pol


# --- credentials --------------------------------------------------------------


def _env(env: Mapping[str, str] | None) -> Mapping[str, str]:
    return os.environ if env is None else env


def validate_env(p: Policy | None, env: Mapping[str, str] | None = None) -> None:
    """Raise ``MissingCreds`` if any required credential is absent from env.
    Optional creds are not checked (absence is silent). No-op if ``p`` is None.
    Empty-string values count as absent, like Go's ``os.Getenv``."""
    if p is None:
        return
    e = _env(env)
    missing = [n for n in p.credentials.required if not e.get(n, "")]
    if missing:
        raise MissingCreds(missing)


def env_vars(p: Policy | None, env: Mapping[str, str] | None = None) -> list[str]:
    """``KEY=VALUE`` strings to inject into the container env. Required creds
    always (ValidateEnv proved them set); optional creds only when present;
    browser cookies value + ``SEED_COOKIES_ENV=<name>`` pointer when the
    cookies var is set."""
    if p is None:
        return []
    e = _env(env)
    out: list[str] = []
    for name in p.credentials.required:
        out.append(f"{name}={e.get(name, '')}")
    for name in p.credentials.optional:
        v = e.get(name, "")
        if v:
            out.append(f"{name}={v}")
    if p.browser.cookies_env:
        v = e.get(p.browser.cookies_env, "")
        if v:
            out.append(f"{p.browser.cookies_env}={v}")
            out.append(f"SEED_COOKIES_ENV={p.browser.cookies_env}")
    return out


# --- mounts -------------------------------------------------------------------


def _expand_placeholders(
    value: str, container_path: str, env: Mapping[str, str]
) -> tuple[str, UnresolvedMount | None]:
    """Replace ``${VAR}`` with the env value. Returns the expanded string plus
    the first unresolved placeholder (or None). Mirrors validate.go:147 —
    replaces all set vars, tracks the first unset, raises on it after."""
    first: UnresolvedMount | None = None

    def _repl(m: re.Match) -> str:
        nonlocal first
        var = m.group(1)
        v = env.get(var, "")
        if v:
            return v
        if first is None:
            first = UnresolvedMount(container_path, m.group(0), var)
        return m.group(0)

    expanded = _PLACEHOLDER_RE.sub(_repl, value)
    return expanded, first


def resolve_mounts(
    p: Policy | None, env: Mapping[str, str] | None = None
) -> list[ResolvedMount]:
    """Walk ``p.Workspace.Mounts``: expand ``${VAR}``, require absolute
    container paths, normalize mode. Raises ``UnresolvedMount`` on the first
    unset var (fail-fast) and ``PolicyError`` on a bad path/mode."""
    if p is None or not p.workspace.mounts:
        return []
    e = _env(env)
    out: list[ResolvedMount] = []
    for m in p.workspace.mounts:
        host, unresolved = _expand_placeholders(m.host, m.container, e)
        if unresolved is not None:
            raise unresolved
        if not os.path.isabs(m.container):
            raise PolicyError(
                f"mount {m.container}: container path must be absolute, got {m.container!r}"
            )
        mode = m.mode or "rw"
        if mode not in ("rw", "ro"):
            raise PolicyError(f"mount {m.container}: mode must be 'rw' or 'ro', got {mode!r}")
        out.append(ResolvedMount(host=host, container=m.container, mode=mode))
    return out


def host_user() -> str:
    """``UID:GID`` for the host user, suitable for Docker's ``User`` field.
    Only meaningful when ``workspace.run_as_host_user`` is true."""
    return f"{os.getuid()}:{os.getgid()}"


# --- egress -------------------------------------------------------------------


def _is_container_resolved(host: str) -> bool:
    return host.lower() in _CONTAINER_RESOLVED


def _is_literal_ip(host: str) -> bool:
    try:
        ipaddress.ip_address(host)
        return True
    except ValueError:
        return False


def _resolve_host(host: str) -> list[str]:
    """One or more IPs for a hostname, or validates a literal IP. Literal
    IPv4/IPv6 short-circuits (no DNS). DNS resolves via getaddrinfo; all IPs
    are included (multi-A-record fan-out), deduped."""
    if not host:
        raise PolicyError("empty host")
    if _is_literal_ip(host):
        return [host]
    try:
        infos = socket.getaddrinfo(host, None)
    except socket.gaierror as e:
        raise PolicyError(str(e)) from e
    seen: dict[str, None] = {}
    out: list[str] = []
    for _fam, _stype, _proto, _canon, sockaddr in infos:
        ip = sockaddr[0]
        if ip not in seen:
            seen[ip] = None
            out.append(ip)
    if not out:
        raise PolicyError(f"no IPs for {host}")
    return out


def egress_rules(p: Policy | None) -> str:
    """Render the iptables allow lines — one ``<ip> <port>`` per line, hostname
    pre-resolved to IP(s). DNS-resolved hosts get a ``# host: <name>`` comment
    first (for the periodic DNS refresh script); literal IPs + container-
    resolved names get none. Empty/None policy → ``""`` (no conf staged).

    Raises ``PolicyError`` on DNS failure, a rule with no port, or an
    out-of-range port."""
    if p is None or not p.egress.allow:
        return ""
    lines: list[str] = []
    for rule in p.egress.allow:
        if _is_container_resolved(rule.host):
            ips = [rule.host]
        else:
            ips = _resolve_host(rule.host)
        ports = list(rule.ports)
        if rule.port:
            ports = [rule.port, *ports]
        if not ports:
            raise PolicyError(f"egress: rule for {rule.host} has no port(s)")
        if not _is_literal_ip(rule.host) and not _is_container_resolved(rule.host):
            lines.append(f"# host: {rule.host}")
        for ip in ips:
            for port in ports:
                if port < 1 or port > 65535:
                    raise PolicyError(f"egress: port {port} for {rule.host} out of range")
                lines.append(f"{ip} {port}")
    return "\n".join(lines) + "\n"


# --- tier ---------------------------------------------------------------------


def resolve_tier(p: Policy | None, fallback: str) -> str:
    """The policy's ``sandbox.tier`` override, or ``fallback`` when unset/None.
    Single source of truth for the effective tier — callers consult this rather
    than reading ``p.sandbox.tier`` directly so the empty-vs-unset distinction
    is handled consistently."""
    if p is None or not p.sandbox.tier:
        return fallback
    return p.sandbox.tier
