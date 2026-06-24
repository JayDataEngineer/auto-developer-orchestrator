#!/usr/bin/env python3
"""Render Pux org trees from org.toml + Jinja2 templates.

Each org lives in ``orgs/<name>/`` and contains:
  - ``org.toml`` (hand-written source of truth)
  - ``MANIFESTO.md``, ``prompts/*.md``, ``skills/*.md``, ``sandbox/*.py``
    (hand-written, never touched by this renderer)
  - ``pux.yaml``, ``roles/*/config.yaml``, ``tool_packages/*.yaml``
    (GENERATED — overwritten on every render)

The renderer refuses to overwrite a file that lacks the
``# AUTO-GENERATED`` header, which protects hand-edited files from
clobbering if someone accidentally renames a template output.

Usage::

    uv run --with jinja2 scripts/org_build.py [--check] [<org>...]

    --check    Dry-run: render to a tmpdir, diff against checked-in tree,
               exit non-zero on any diff. For CI.
    <org>...   Render only named orgs. Default: all orgs under orgs/.
"""

from __future__ import annotations

import argparse
import difflib
import os
import sys
import tempfile
import tomllib
from pathlib import Path
from typing import Any

from jinja2 import Environment, FileSystemLoader, StrictUndefined

REPO_ROOT = Path(__file__).resolve().parent.parent
ORGS_DIR = REPO_ROOT / "orgs"
TEMPLATES_DIR = REPO_ROOT / "scripts" / "templates" / "org"
GENERATED_HEADER = "# AUTO-GENERATED"

# Map K8s-style runtime_class names to Docker --runtime values.
# K8s uses gVisor/Kata brand names; Docker uses the binary name installed by
# the runtime class installer. "runc" is Docker's default — emit nothing.
RUNTIME_CLASS_MAP: dict[str, str | None] = {
    "runc": None,
    "gvisor": "runsc",
    "kata": "kata",
}


def _docker_cpu(k8s_cpu: str | int | float) -> str:
    """Translate K8s CPU request/limit → Docker compose ``cpus`` value.

    K8s accepts ``500m`` (millicores), ``2`` (cores), ``0.5`` (decimal).
    Docker compose wants a decimal number as a string: ``'0.5'``, ``'2'``.
    """
    s = str(k8s_cpu).strip()
    if not s:
        return ""
    if s.endswith("m") and s[:-1].isdigit():
        # millicores → decimal cores
        return f"{int(s[:-1]) / 1000:g}"
    # Already a number (cores). Pass through, normalized.
    try:
        return f"{float(s):g}"
    except ValueError:
        # Unknown shape — emit as-is and let Docker complain.
        return s


def _docker_mem(k8s_mem: str | int) -> str:
    """Translate K8s memory request/limit → Docker compose ``memory`` value.

    K8s accepts ``512Mi``, ``2Gi``, ``1G`` (binary + decimal SI suffixes).
    Docker compose accepts ``512M``, ``2G`` (decimal SI only — no ``Mi``/``Gi``).
    Bare numbers are interpreted as bytes by both.
    """
    s = str(k8s_mem).strip()
    if not s:
        return ""
    suffix_map = {"Ki": "K", "Mi": "M", "Gi": "G", "Ti": "T"}
    for k8s_suf, docker_suf in suffix_map.items():
        if s.endswith(k8s_suf):
            return s[: -len(k8s_suf)] + docker_suf
    return s


# --------------------------------------------------------------------------- #
# Schema validation                                                           #
# --------------------------------------------------------------------------- #

# sandbox.tier — three explicit contracts. See declarative-cooking-wolf.md §A1.
#
# standard     — stock pux-sandbox:latest image, full kernel isolation
#                (gVisor, warm_pool, resources, PUX_ORG_PATH env).
#
# custom-build — org ships its own Dockerfile. Requires a `justification`
#                string forcing the author to articulate why. Used by orgs
#                with heavy system deps (Manim, LaTeX, etc).
#
# skeleton     — config-only org, no sandbox container. The [sandbox] block
#                must be EMPTY when tier = skeleton; if it isn't, the org
#                has contradicted itself and we fail loud.
SANDBOX_TIERS: tuple[str, ...] = ("standard", "custom-build", "skeleton")


def _validate_sandbox_tier_fields(
    tier: str, sandbox: dict[str, Any]
) -> list[str]:
    """Per-tier required-field checks. Called only when tier value is valid.

    The point isn't to constrain what orgs can declare — it's to make the
    implicit contract explicit so that drift becomes visible at validate
    time instead of at runtime.
    """
    errs: list[str] = []

    if tier == "standard":
        # Standard tier requires the kernel isolation primitives. Missing
        # any of these means the org silently falls back to Docker defaults,
        # which is the drift we're trying to surface.
        if sandbox.get("runtime_class") not in ("gvisor", "kata"):
            errs.append(
                "org.toml: sandbox.runtime_class must be 'gvisor' or 'kata' "
                "for tier='standard' (runc = Docker default, no isolation)"
            )
        wp = sandbox.get("warm_pool")
        if not isinstance(wp, int) or isinstance(wp, bool) or wp < 1:
            errs.append(
                "org.toml: sandbox.warm_pool must be a positive integer "
                "for tier='standard'"
            )
        env = sandbox.get("env") or {}
        if not isinstance(env, dict) or "PUX_ORG_PATH" not in env:
            errs.append(
                "org.toml: sandbox.env.PUX_ORG_PATH is required for "
                "tier='standard' (kernel uses this to label-discover the "
                "container at sandbox creation time)"
            )
        # resources block is required — both requests + limits.
        res = sandbox.get("resources") or {}
        if not isinstance(res, dict):
            errs.append(
                "org.toml: sandbox.resources is required for tier='standard'"
            )
        else:
            for sub in ("requests", "limits"):
                block = res.get(sub) or {}
                if not isinstance(block, dict) or not block:
                    errs.append(
                        f"org.toml: sandbox.resources.{sub} is required for "
                        f"tier='standard' (cpu + memory)"
                    )
        # Standard tier must NOT declare a build block — that's the
        # custom-build tier's job. If both are present, the org has
        # contradicted itself.
        if sandbox.get("build"):
            errs.append(
                "org.toml: sandbox.build is forbidden for tier='standard' "
                "(use tier='custom-build' if a Dockerfile is needed)"
            )

    elif tier == "custom-build":
        build = sandbox.get("build")
        if not isinstance(build, dict):
            errs.append(
                "org.toml: sandbox.build is required for tier='custom-build' "
                "(must be a table with context + dockerfile + justification)"
            )
        else:
            if not build.get("context"):
                errs.append(
                    "org.toml: sandbox.build.context is required for "
                    "tier='custom-build'"
                )
            if not build.get("dockerfile"):
                errs.append(
                    "org.toml: sandbox.build.dockerfile is required for "
                    "tier='custom-build'"
                )
            # justification is the forcing function: articulate WHY this
            # org needs a custom build. Empty string = reject.
            justification = build.get("justification", "").strip()
            if not justification:
                errs.append(
                    "org.toml: sandbox.build.justification is required for "
                    "tier='custom-build' — explain why pux-sandbox:latest "
                    "is insufficient (system deps, vendored binaries, etc)."
                )
        # Custom-build still needs env.PUX_ORG_PATH — label discovery is
        # the adoption mechanism regardless of image source.
        env = sandbox.get("env") or {}
        if not isinstance(env, dict) or "PUX_ORG_PATH" not in env:
            errs.append(
                "org.toml: sandbox.env.PUX_ORG_PATH is required for "
                "tier='custom-build' (same label-discovery contract as "
                "tier='standard')"
            )

    elif tier == "skeleton":
        # Skeleton tier = no sandbox. The [sandbox] block is allowed to
        # declare tier='skeleton' (so the org can document intent) but
        # MUST NOT declare any sandbox configuration fields. Any non-tier
        # key in the block means the org has contradicted itself.
        forbidden_keys = sorted(
            k for k in sandbox.keys() if k not in ("tier",)
        )
        if forbidden_keys:
            errs.append(
                f"org.toml: sandbox block must be empty for "
                f"tier='skeleton' (found: {forbidden_keys}). Skeleton "
                f"orgs don't have a sandbox — declare tier='standard' or "
                f"tier='custom-build' if you need one."
            )

    return errs


def validate_org_data(data: dict[str, Any], org_name: str, org_dir: Path) -> list[str]:
    """Return a list of human-readable error strings. Empty list = valid."""
    errs: list[str] = []

    if data.get("name") != org_name:
        errs.append(
            f"org.toml name ({data.get('name')!r}) does not match directory "
            f"name ({org_name!r})"
        )
    if not data.get("description"):
        errs.append("org.toml: description is required")

    roles = data.get("roles") or []
    if not isinstance(roles, list):
        errs.append("org.toml: [[roles]] must be a list of tables")
        roles = []

    for i, role in enumerate(roles):
        if not role.get("name"):
            errs.append(f"org.toml: roles[{i}].name is required")
        if not role.get("description"):
            errs.append(f"org.toml: roles[{i}].description is required")
        # Kernel contract: prompt.md must live next to config.yaml in the
        # role folder. Validate that the source tree already has it.
        prompt_path = org_dir / "roles" / role["name"] / "prompt.md"
        if role.get("name") and not prompt_path.exists():
            errs.append(
                f"org.toml: roles[{i}].name={role.get('name')!r} expects "
                f"prompt.md at {prompt_path.relative_to(org_dir)} but it's missing"
            )

    pkgs = data.get("tool_packages") or []
    for i, pkg in enumerate(pkgs):
        if not pkg.get("name"):
            errs.append(f"org.toml: tool_packages[{i}].name is required")
        if not pkg.get("description"):
            errs.append(f"org.toml: tool_packages[{i}].description is required")

    for k, cfg in (data.get("databases") or {}).items():
        if not isinstance(cfg, dict):
            errs.append(f"org.toml: databases.{k} must be a table")

    # Sandbox profile validation. Only validates the K8s-style sandbox
    # abstractions (runtime_class, warm_pool, resources). init_files /
    # pip_packages / env are validated downstream by the Go kernel when it
    # boots the sandbox.
    sandbox = data.get("sandbox")
    if isinstance(sandbox, dict):
        # sandbox.tier — explicit declaration of which sandbox contract the
        # org runs under. Three values, mutually exclusive. Missing tier is
        # a hard failure (forcing function: every org declares its tier).
        # See plan: declarative-cooking-wolf.md §A1.
        valid_tiers = {"standard", "custom-build", "skeleton"}
        tier = sandbox.get("tier")
        if not tier:
            errs.append(
                "org.toml: sandbox.tier is required (one of "
                f"{sorted(valid_tiers)}). See declarative-cooking-wolf.md §A1."
            )
        elif tier not in valid_tiers:
            errs.append(
                f"org.toml: sandbox.tier={tier!r} must be one of "
                f"{sorted(valid_tiers)}"
            )
        else:
            # Tier-specific required-field checks. Run only when tier is
            # known — otherwise we'd cascade confusing errors.
            tier_errs = _validate_sandbox_tier_fields(tier, sandbox)
            errs.extend(tier_errs)

        # sandbox.idle_shutdown_secs — Phase C container lifecycle. 0 = off.
        # Negative values are nonsensical; non-ints break the watchdog.
        idle = sandbox.get("idle_shutdown_secs", 0)
        if not isinstance(idle, int) or isinstance(idle, bool) or idle < 0:
            errs.append(
                f"org.toml: sandbox.idle_shutdown_secs={idle!r} must be a "
                f"non-negative integer (0 = never auto-shutdown). See "
                f"declarative-cooking-wolf.md §C1."
            )

        rc = sandbox.get("runtime_class", "runc")
        if rc not in RUNTIME_CLASS_MAP:
            errs.append(
                f"org.toml: sandbox.runtime_class={rc!r} must be one of "
                f"{sorted(RUNTIME_CLASS_MAP)}"
            )
        # sandbox.mode: per-org isolation toggle. Default "contained" (CTO
        # locked in /sandbox/workspace/). "host-access" opts the org out —
        # right for coding-agent orgs whose purpose is editing files in a
        # real repo. Anything else is a typo; fail loud so a misspelled
        # "host_acess" doesn't silently lock down an org that meant to opt
        # out.
        valid_modes = {"contained", "host-access"}
        mode = sandbox.get("mode", "contained")
        if mode not in valid_modes:
            errs.append(
                f"org.toml: sandbox.mode={mode!r} must be one of "
                f"{sorted(valid_modes)}"
            )
        wp = sandbox.get("warm_pool", 1)
        if not isinstance(wp, int) or wp < 1:
            errs.append(
                f"org.toml: sandbox.warm_pool={wp!r} must be a positive integer"
            )
        res = sandbox.get("resources")
        if res is not None and not isinstance(res, dict):
            errs.append("org.toml: sandbox.resources must be a table")
        else:
            for tier in ("requests", "limits"):
                block = (res or {}).get(tier)
                if block is None:
                    continue
                if not isinstance(block, dict):
                    errs.append(f"org.toml: sandbox.resources.{tier} must be a table")
                    continue
                for k in ("cpu", "memory"):
                    v = block.get(k)
                    if v is not None and not isinstance(v, str):
                        errs.append(
                            f"org.toml: sandbox.resources.{tier}.{k} must be a "
                            f"string like '200m' or '256Mi'"
                        )

        # Build context — must have either image OR build, not both preferrable
        build = sandbox.get("build")
        if build is not None:
            if not isinstance(build, dict):
                errs.append("org.toml: sandbox.build must be a table")
            elif not build.get("context") and not build.get("dockerfile"):
                errs.append(
                    "org.toml: sandbox.build requires at least context or dockerfile"
                )

        # Restart policy
        valid_restarts = {"", "no", "always", "on-failure", "unless-stopped"}
        if sandbox.get("restart", "") not in valid_restarts:
            errs.append(
                f"org.toml: sandbox.restart={sandbox.get('restart')!r} must be "
                f"one of {sorted(valid_restarts)}"
            )

        # Volumes
        for i, v in enumerate(sandbox.get("volumes") or []):
            if not isinstance(v, dict):
                errs.append(f"org.toml: sandbox.volumes[{i}] must be a table")
                continue
            vtype = v.get("type", "volume")
            if vtype not in ("volume", "bind"):
                errs.append(
                    f"org.toml: sandbox.volumes[{i}].type must be 'volume' or 'bind'"
                )
                continue
            if not v.get("container"):
                errs.append(
                    f"org.toml: sandbox.volumes[{i}].container is required"
                )
            if vtype == "bind" and not v.get("host"):
                errs.append(
                    f"org.toml: sandbox.volumes[{i}].host is required for type=bind"
                )
            if vtype == "volume" and not v.get("name"):
                errs.append(
                    f"org.toml: sandbox.volumes[{i}].name is required for type=volume"
                )

        # Networks
        for i, n in enumerate(sandbox.get("networks") or []):
            if not isinstance(n, dict):
                errs.append(f"org.toml: sandbox.networks[{i}] must be a table")
                continue
            if not n.get("name"):
                errs.append(f"org.toml: sandbox.networks[{i}].name is required")

        # Healthcheck
        hc = sandbox.get("healthcheck")
        if hc is not None:
            if not isinstance(hc, dict):
                errs.append("org.toml: sandbox.healthcheck must be a table")
            elif not hc.get("test"):
                errs.append("org.toml: sandbox.healthcheck.test is required")

    return errs


# --------------------------------------------------------------------------- #
# Rendering                                                                   #
# --------------------------------------------------------------------------- #

def _make_env() -> Environment:
    return Environment(
        loader=FileSystemLoader(str(TEMPLATES_DIR)),
        undefined=StrictUndefined,
        trim_blocks=True,
        lstrip_blocks=True,
        keep_trailing_newline=True,
    )


def _render_template(env: Environment, template_name: str, ctx: dict[str, Any]) -> str:
    tmpl = env.get_template(template_name)
    return tmpl.render(**ctx)


def _normalize(data: dict[str, Any]) -> dict[str, Any]:
    """Fill in defaults so templates can use StrictUndefined safely.

    Templates iterate over these fields unconditionally — if a field is
    missing from org.toml, we inject an empty/none value so the {% if %}
    guards in the templates evaluate cleanly. Nested tables (``sandbox``)
    get their commonly-accessed keys pre-filled too.
    """
    sandbox = dict(data.get("sandbox") or {})
    sandbox.setdefault("init_files", [])
    sandbox.setdefault("pip_packages", [])
    sandbox.setdefault("env", {})
    sandbox.setdefault("runtime_class", "runc")
    sandbox.setdefault("warm_pool", 1)
    sandbox.setdefault("mode", "contained")
    sandbox.setdefault("resources", {})
    sandbox.setdefault("build", {})
    sandbox.setdefault("container_name", "")
    sandbox.setdefault("restart", "")
    sandbox.setdefault("working_dir", "")
    sandbox.setdefault("command", [])
    sandbox.setdefault("volumes", [])
    sandbox.setdefault("networks", [])
    sandbox.setdefault("healthcheck", {})
    sandbox.setdefault("network_mode_disabled", False)
    sandbox.setdefault("service_name", "")
    sandbox.setdefault("image", "")
    # Phase C container lifecycle — 0 means no idle auto-shutdown. Preserved
    # in pux.yaml so the kernel's watchdog can read it.
    sandbox.setdefault("idle_shutdown_secs", 0)
    # Phase A: tier field is required by validate_org_data() when [sandbox]
    # is present. Default to empty here so StrictUndefined doesn't trip on
    # templates that probe `sandbox.tier` — validator catches missing tier.
    sandbox.setdefault("tier", "")
    # For orgs without a build context and without an explicit image, default
    # to the stock kernel sandbox image. Orgs with a Dockerfile get their
    # composed image tag derived from project+service (matches what
    # `docker compose build` produces) so Pux's sandbox manager can launch
    # the org's specialized container directly.
    if not sandbox.get("build") and not sandbox.get("image"):
        sandbox["image"] = "pux-sandbox:latest"
    elif sandbox.get("build") and not sandbox.get("image"):
        org_name = data.get("name") or "org"
        service = sandbox.get("service_name") or "sandbox"
        sandbox["image"] = f"{org_name}-{service}:latest"
    # Pre-fill optional sub-keys on volumes/networks so StrictUndefined
    # doesn't trip when the template probes docker_name / external.
    sandbox["volumes"] = [
        {"type": "volume", "docker_name": "", "external": False, **v}
        for v in sandbox["volumes"]
        if isinstance(v, dict)
    ]
    sandbox["networks"] = [
        {"docker_name": "", "external": False, **n}
        for n in sandbox["networks"]
        if isinstance(n, dict)
    ]
    # Pre-fill healthcheck sub-keys so StrictUndefined doesn't trip on the
    # interval/timeout/start_period/retries probes. `test` defaults to []
    # so the outer `{% if sandbox.healthcheck %}` check still needs to be
    # gated by whether the user actually declared a healthcheck.
    if isinstance(sandbox["healthcheck"], dict):
        hc = sandbox["healthcheck"]
        hc.setdefault("test", [])
        for k in ("interval", "timeout", "start_period"):
            hc.setdefault(k, "")
        hc.setdefault("retries", 0)
        # Empty healthcheck table — treat as undeclared so the template's
        # outer `{% if sandbox.healthcheck %}` gate works as a presence check.
        if not any(hc.values()):
            sandbox["healthcheck"] = {}
    else:
        sandbox["healthcheck"] = {}
    # Pre-fill requests/limits so StrictUndefined doesn't trip in pux.yaml.j2.
    # Both default to {} so the `{% if requests or limits %}` guard is falsy.
    # `sandbox["resources"]` preserves the K8s-style source values (`250m`,
    # `1Gi`) — pux.yaml is the CRD view. The Docker-translated view lives in
    # `sandbox["resources_docker"]` (`0.25`, `1G`) and is what compose reads.
    resources = sandbox["resources"]
    if not isinstance(resources, dict):
        sandbox["resources"] = {"requests": {}, "limits": {}}
    sandbox["resources"].setdefault("requests", {})
    sandbox["resources"].setdefault("limits", {})
    resources_docker = {"requests": {}, "limits": {}}
    for tier in ("requests", "limits"):
        tier_cfg = sandbox["resources"].get(tier) or {}
        for key, raw_val in tier_cfg.items():
            if key == "cpu":
                resources_docker[tier][key] = _docker_cpu(raw_val)
            elif key == "memory":
                resources_docker[tier][key] = _docker_mem(raw_val)
            else:
                resources_docker[tier][key] = raw_val
    sandbox["resources_docker"] = resources_docker
    # Translate K8s runtime_class name to Docker --runtime value. Invalid
    # values fall through to None (no `runtime:` line) and the schema check
    # in validate_org_data flags them.
    raw_runtime = sandbox.get("runtime_class") or "runc"
    sandbox["runtime_class_docker"] = RUNTIME_CLASS_MAP.get(raw_runtime)

    return {
        "name": data.get("name", ""),
        "description": data.get("description", ""),
        "manifesto": data.get("manifesto", ""),
        "staff_root": data.get("staff_root", ""),
        "tool_packages_root": data.get("tool_packages_root", ""),
        "skills_dir": data.get("skills_dir", ""),
        "data_dir": data.get("data_dir", ""),
        "extensions_dir": data.get("extensions_dir", ""),
        "schedules": data.get("schedules", []) or [],
        "databases": data.get("databases", {}) or {},
        "sandbox": sandbox,
        "mcp_servers": data.get("mcp_servers", []) or [],
        "roles": data.get("roles", []) or [],
        "tool_packages": data.get("tool_packages", []) or [],
    }


def render_org(org_dir: Path, env: Environment, target_dir: Path | None = None) -> list[Path]:
    """Render all generated files for an org.

    Writes to ``org_dir`` itself by default. When ``target_dir`` is provided
    (used by --check), writes the generated files under ``target_dir`` instead,
    preserving relative paths. Returns the list of rendered file paths (in the
    target tree).
    """
    org_toml = org_dir / "org.toml"
    if not org_toml.exists():
        raise FileNotFoundError(f"{org_toml} does not exist")

    with open(org_toml, "rb") as f:
        raw = tomllib.load(f)

    errs = validate_org_data(raw, org_dir.name, org_dir)
    if errs:
        raise ValueError(
            f"org.toml validation failed for {org_dir.name}:\n  - "
            + "\n  - ".join(errs)
        )

    data = _normalize(raw)
    # Normalize schedule entries so StrictUndefined doesn't trip on missing
    # `model` / `enabled` keys in schedules that intentionally omit them.
    sched_defaults = {"model": "", "enabled": True}
    data["schedules"] = [
        {**sched_defaults, **s} for s in data["schedules"]
    ]
    write_root = target_dir or org_dir
    written: list[Path] = []

    # pux.yaml
    pux_out = _render_template(env, "pux.yaml.j2", data)
    pux_path = write_root / "pux.yaml"
    _atomic_write(pux_path, pux_out, allow_overwrite_header=True)
    written.append(pux_path)

    # docker-compose.yml — only for orgs that declare a [sandbox] block.
    # The compose file is the ops view of the sandbox profile; the kernel
    # still uses its own Docker SDK for sandbox lifecycle (compose is for
    # `docker compose up` by humans/CI and for documenting intent).
    # Standard filename so `docker compose <cmd>` works with no -f flag.
    if raw.get("sandbox"):
        compose_out = _render_template(env, "sandbox.compose.yml.j2", data)
        compose_path = write_root / "docker-compose.yml"
        _atomic_write(compose_path, compose_out, allow_overwrite_header=True)
        written.append(compose_path)

    # roles/<name>/config.yaml
    role_defaults = {
        "imports": [],
        "model": "",
        "max_rounds": 0,
        "temperature": None,
        "tools": [],
        "capabilities": [],
        "mcp_servers": [],
        "delegates_to": [],
        "division": "",
        "thinking": False,
        "sandbox": "",
        "sandbox_tier": "",
        "hint": "",
    }
    for role_raw in data["roles"]:
        role = dict(role_raw)
        for k, default in role_defaults.items():
            role.setdefault(k, default)
        ctx = {"role": role}
        out = _render_template(env, "role.config.yaml.j2", ctx)
        path = write_root / "roles" / role["name"] / "config.yaml"
        _atomic_write(path, out, allow_overwrite_header=True)
        written.append(path)

    # tool_packages/<name>.yaml
    pkg_defaults = {
        "tools": [],
        "mcp_servers": [],
        "sandbox_tier": "",
    }
    for pkg_raw in data["tool_packages"]:
        pkg = dict(pkg_raw)
        for k, default in pkg_defaults.items():
            pkg.setdefault(k, default)
        ctx = {"pkg": pkg}
        out = _render_template(env, "tool_package.yaml.j2", ctx)
        path = write_root / "tool_packages" / f"{pkg['name']}.yaml"
        _atomic_write(path, out, allow_overwrite_header=True)
        written.append(path)

    return written


def _atomic_write(
    path: Path,
    content: str,
    allow_overwrite_header: bool = False,
) -> None:
    """Write content to path, creating parent dirs.

    Safety: if the target file exists and does NOT start with the
    ``# AUTO-GENERATED`` marker, refuse to overwrite. This catches accidental
    clobbering of hand-edited files.
    """
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.exists() and allow_overwrite_header:
        existing = path.read_text(encoding="utf-8")
        if not existing.lstrip().startswith(GENERATED_HEADER):
            raise RuntimeError(
                f"Refusing to overwrite {path}: existing file lacks "
                f"{GENERATED_HEADER!r} header. Hand-written files must not be "
                f"replaced by the renderer."
            )
    path.write_text(content, encoding="utf-8")


# --------------------------------------------------------------------------- #
# Diff / check mode                                                           #
# --------------------------------------------------------------------------- #

def _render_to_tmp(org_dir: Path, env: Environment) -> Path:
    """Render the org into a fresh tmpdir and return its root."""
    tmp = Path(tempfile.mkdtemp(prefix=f"org-build-{org_dir.name}-"))
    render_org(org_dir, env, target_dir=tmp)
    return tmp


def _diff_trees(checked_in: Path, rendered: Path) -> list[str]:
    """Compare rendered files against the checked-in tree.

    Walks the rendered tmp tree (since some checked-in paths may not exist
    yet on first render). Returns unified diff lines.
    """
    diff_lines: list[str] = []
    rendered_files = sorted(p for p in rendered.rglob("*") if p.is_file())

    for rpath in rendered_files:
        rel = rpath.relative_to(rendered)
        cpath = checked_in / rel
        existing = cpath.read_text(encoding="utf-8") if cpath.exists() else ""
        new = rpath.read_text(encoding="utf-8")
        if existing != new:
            diff_lines.extend(
                difflib.unified_diff(
                    existing.splitlines(keepends=True),
                    new.splitlines(keepends=True),
                    fromfile=str(cpath),
                    tofile=str(cpath),
                    n=3,
                )
            )

    # Also flag checked-in generated files that have no rendered counterpart
    # (i.e. org.toml no longer references them — they're stale).
    for cpath in sorted(p for p in checked_in.rglob("*") if p.is_file()):
        if not _looks_generated(cpath):
            continue
        rel = cpath.relative_to(checked_in)
        rpath = rendered / rel
        if not rpath.exists():
            diff_lines.append(
                f"Only in checked-in tree (stale, should be deleted): {cpath}\n"
            )

    return diff_lines


def _looks_generated(path: Path) -> bool:
    """Heuristic: file is generated iff its first non-empty line starts with
    the AUTO-GENERATED marker."""
    try:
        text = path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError):
        return False
    return text.lstrip().startswith(GENERATED_HEADER)


# --------------------------------------------------------------------------- #
# Discovery / CLI                                                             #
# --------------------------------------------------------------------------- #

def discover_orgs() -> list[Path]:
    return sorted(
        p
        for p in ORGS_DIR.iterdir()
        if p.is_dir() and not p.name.startswith("_") and (p / "org.toml").exists()
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--check",
        action="store_true",
        help="Dry-run: diff rendered output against checked-in tree.",
    )
    parser.add_argument(
        "orgs",
        nargs="*",
        help="Orgs to render (default: all orgs with org.toml under orgs/).",
    )
    args = parser.parse_args(argv)

    env = _make_env()

    if args.orgs:
        org_paths = []
        for name in args.orgs:
            org_dir = ORGS_DIR / name
            if not org_dir.exists():
                print(f"error: org {name!r} not found at {org_dir}", file=sys.stderr)
                return 2
            if not (org_dir / "org.toml").exists():
                print(f"error: org {name!r} has no org.toml", file=sys.stderr)
                return 2
            org_paths.append(org_dir)
    else:
        org_paths = discover_orgs()
        if not org_paths:
            print("no orgs with org.toml found under orgs/", file=sys.stderr)
            return 1

    failures = 0

    for org_dir in org_paths:
        try:
            if args.check:
                tmp = _render_to_tmp(org_dir, env)
                diff = _diff_trees(org_dir, tmp)
                if diff:
                    failures += 1
                    print(f"FAIL {org_dir.name}: rendered output drifts from checked-in tree")
                    sys.stdout.write("".join(diff))
                else:
                    print(f"OK   {org_dir.name}")
            else:
                written = render_org(org_dir, env)
                print(f"OK   {org_dir.name}: wrote {len(written)} files")
        except Exception as e:
            failures += 1
            print(f"FAIL {org_dir.name}: {e}", file=sys.stderr)

    if failures:
        print(f"\n{failures} org(s) failed.", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
