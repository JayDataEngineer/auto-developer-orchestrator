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


# --------------------------------------------------------------------------- #
# Schema validation                                                           #
# --------------------------------------------------------------------------- #

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
