#!/usr/bin/env python3
"""System A Python contract test.

Enforces the contract that every human-shipped backbone script (the
"[sandbox].init_files" entries — the System A tier of the two-tier Python
separation) follows the canonical CLI / output / path-resolution pattern.

Why this matters: backbone scripts are the substrate for the agent's
command-and-control architecture. The agent invokes them by name + flags;
if a script silently changes its CLI shape (raw sys.argv instead of argparse,
hardcoded /sandbox/ path that breaks the host↔container bind-mount), the
agent's invocation fails opaquely. Drift compounds because scripts are
copy-pasted as templates for new ones.

Contract enforced per script:
  - Library modules (no `if __name__ == "__main__"` guard): SKIPPED. They're
    imported by other scripts, not invoked by the agent. Their hardcoded
    /sandbox/ paths are still checked (they propagate to runtime via imports).
  - CLI scripts (have a __main__ guard):
    1. `python3 <script> --help` exits 0 OR exits non-zero with JSON
       `{"error": "..."}` on stdout (graceful dep-missing is valid).
    2. `python3 <script> --bogus-flag-x` exits non-zero (unknown args
       rejected). Allowlisted for single-purpose no-arg tools.
    3. No hardcoded `/sandbox/...` absolute paths in source code lines
       (path resolution must go through paths.py or __file__-relative —
       bind-mount can move).

Allowlist: scripts that legitimately can't conform (single-purpose no-arg
tools, third-party-vendored, awaiting paths.py refactor) are documented in
system_a_allowlist.yaml with a one-line reason per entry. The allowlist is
intentionally tiny — adding to it is a reviewer flag, not a default.

Run:
    uv run --with pyyaml --with pytest --with jinja2 \\
        python3 scripts/test_system_a_contract.py
    uv run --with pyyaml --with pytest --with jinja2 \\
        pytest scripts/test_system_a_contract.py -v
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parent.parent
ORGS_DIR = REPO_ROOT / "orgs"
ALLOWLIST_PATH = REPO_ROOT / "scripts" / "system_a_allowlist.yaml"


# --------------------------------------------------------------------------- #
# Allowlist loading
# --------------------------------------------------------------------------- #

def _load_allowlist() -> dict[str, str]:
    """Returns {script_basename: reason} from system_a_allowlist.yaml."""
    if not ALLOWLIST_PATH.exists():
        return {}
    data = yaml.safe_load(ALLOWLIST_PATH.read_text()) or {}
    return {entry["script"]: entry["reason"] for entry in data.get("allowlist", [])}


# --------------------------------------------------------------------------- #
# Script discovery
# --------------------------------------------------------------------------- #

def _resolve_init_file(org_dir: Path, declared: str) -> Path:
    """Resolve a declared init_files entry to a real path on disk.

    @shared/foo/bar.py → orgs/_shared/foo/bar.py
    sandbox/foo.py     → <org_dir>/sandbox/foo.py
    """
    if declared.startswith("@shared/"):
        rel = declared[len("@shared/"):]
        return REPO_ROOT / "orgs" / "_shared" / rel
    if declared.startswith("/"):
        raise ValueError(f"absolute init_files path is non-canonical: {declared}")
    return org_dir / declared


def discover_system_a_scripts() -> list[tuple[str, str, Path]]:
    """Walk every org's [sandbox].init_files and yield (org, declared, path)."""
    if not ORGS_DIR.exists():
        return []
    sys.path.insert(0, str(REPO_ROOT / "scripts"))
    import org_build  # noqa: E402  -- path-dependent import

    out: list[tuple[str, str, Path]] = []
    for org_dir in org_build.discover_orgs():
        with open(org_dir / "org.toml", "rb") as f:
            import tomllib
            raw = tomllib.load(f)
        for declared in raw.get("sandbox", {}).get("init_files", []):
            try:
                path = _resolve_init_file(org_dir, declared)
            except ValueError:
                continue
            if path.exists():
                out.append((org_dir.name, declared, path))
    return out


# --------------------------------------------------------------------------- #
# Pattern detectors
# --------------------------------------------------------------------------- #

def has_main_guard(source: str) -> bool:
    """True if the script has `if __name__ == "__main__"`.

    Library modules without a main guard are imported by other scripts —
    they're not directly invoked by the agent, so the --help / --bogus-flag
    checks don't apply. The hardcoded-/sandbox/ check still applies because
    library-level constants propagate to runtime via imports.
    """
    return '__name__ == "__main__"' in source or "__name__ == '__main__'" in source


def has_hardcoded_sandbox_path(source: str) -> list[str]:
    """Returns code lines that hardcode /sandbox/ as a runtime path.

    Heuristic: skip lines inside triple-quoted blocks and stripped comment
    lines. Skip lines that are obviously documentation prose (contain
    "mounted at", "copies to", "Reach the", etc.). What remains must use
    paths.py / __file__ / env lookup.
    """
    violations = []
    in_docstring = False
    for line in source.splitlines():
        stripped = line.strip()
        # Count triple-quote markers on this stripped line.
        n_dbl = stripped.count('"""')
        n_sgl = stripped.count("'''")
        # A line with even-count triple-quotes opens AND closes a docstring
        # on the same line (single-line docstring). Skip it — the contents
        # are documentation, not runtime code.
        if (n_dbl >= 2 and n_dbl % 2 == 0) or (n_sgl >= 2 and n_sgl % 2 == 0):
            continue
        # A line with odd-count triple-quote markers toggles docstring state.
        if n_dbl % 2 == 1 or n_sgl % 2 == 1:
            in_docstring = not in_docstring
            continue
        if in_docstring:
            continue
        if stripped.startswith("#"):
            continue
        if "/sandbox/" in stripped and not _looks_like_path_doc(stripped):
            violations.append(stripped)
    return violations


def _looks_like_path_doc(line: str) -> bool:
    """True if the line is documenting a path rather than resolving one."""
    doc_markers = (
        "Lives at", "lives at", "mounted at", "copies to",
        "copied to", "rendered to", "available at", "Reach the",
        "reach the", "in-container", "in-sandbox",
        "python3 /sandbox",  # command-string in next_step / hint
        "Run:", "run:",  # "Run: python3 /sandbox/X" — agent-facing instruction
        "next_step",       # JSON key whose value is a command string
        "hint",
    )
    return any(m in line for m in doc_markers)


def _is_graceful_dep_missing(result: subprocess.CompletedProcess) -> bool:
    """True if --help failed but the script printed a JSON error.

    System A scripts handle missing optional deps (requests, surrealdb, etc.)
    by printing `{"error": "X not installed. Add to pip_packages."}` and
    exiting non-zero. That's contract-compliant behavior — the script's
    argparse wiring is intact; it just can't import. The agent sees the
    error and knows to add the dep.
    """
    if result.returncode == 0:
        return False
    out = result.stdout.strip()
    if not out:
        return False
    try:
        data = json.loads(out.splitlines()[0])
    except (json.JSONDecodeError, IndexError):
        return False
    return isinstance(data, dict) and "error" in data


# --------------------------------------------------------------------------- #
# Test parametrization
# --------------------------------------------------------------------------- #

_SCRIPTS = discover_system_a_scripts()
_ALLOWLIST = _load_allowlist()


def _script_id(case) -> str:
    org, declared, _ = case
    return f"{org}:{declared}"


@pytest.mark.skipif(not _SCRIPTS, reason="no System A scripts discovered")
@pytest.mark.parametrize("case", _SCRIPTS, ids=_script_id)
def test_system_a_help_works(case) -> None:
    """--help exits 0 OR exits non-zero with JSON `{"error": "..."}` on stdout.

    The second path is the graceful-dep-missing case: the script handles
    missing optional deps (requests, surrealdb) by printing JSON error +
    exiting non-zero. That's valid System A behavior — argparse wiring is
    intact; the import guard fires first.
    """
    org, declared, path = case
    if path.name in _ALLOWLIST:
        pytest.skip(f"allowlisted: {_ALLOWLIST[path.name]}")
    if not has_main_guard(path.read_text()):
        pytest.skip("library module (no __main__ guard)")
    result = subprocess.run(
        [sys.executable, str(path), "--help"],
        capture_output=True, text=True, timeout=10,
    )
    if result.returncode == 0:
        return
    if _is_graceful_dep_missing(result):
        return
    pytest.fail(
        f"{org}:{declared} --help exited {result.returncode} without JSON error.\n"
        f"stdout: {result.stdout[:300]}\n"
        f"stderr: {result.stderr[:300]}"
    )


@pytest.mark.skipif(not _SCRIPTS, reason="no System A scripts discovered")
@pytest.mark.parametrize("case", _SCRIPTS, ids=_script_id)
def test_system_a_rejects_unknown_flag(case) -> None:
    """--bogus-flag-x exits non-zero — unknown args must not silently pass.

    Allowlisted: single-purpose no-arg tools (desktop_observe.py) where
    argv is never inspected. Document the reason in system_a_allowlist.yaml.

    Skipped: library modules (no main guard) — they don't parse argv.
    """
    org, declared, path = case
    if path.name in _ALLOWLIST:
        pytest.skip(f"allowlisted: {_ALLOWLIST[path.name]}")
    if not has_main_guard(path.read_text()):
        pytest.skip("library module (no __main__ guard)")
    result = subprocess.run(
        [sys.executable, str(path), "--bogus-flag-x-xyz"],
        capture_output=True, text=True, timeout=10,
    )
    if result.returncode == 0:
        # One more chance: did the script print a JSON error for missing dep?
        # If so, it didn't "accept" the flag — it failed before parsing argv.
        try:
            data = json.loads(result.stdout.strip().splitlines()[0])
            if isinstance(data, dict) and "error" in data:
                return  # graceful dep-missing, not silent acceptance
        except (json.JSONDecodeError, IndexError):
            pass
        pytest.fail(
            f"{org}:{declared} accepted --bogus-flag-x-xyz silently (exit 0). "
            f"Scripts must reject unknown args or fail before parsing argv."
        )


@pytest.mark.skipif(not _SCRIPTS, reason="no System A scripts discovered")
@pytest.mark.parametrize("case", _SCRIPTS, ids=_script_id)
def test_system_a_no_hardcoded_sandbox_path(case) -> None:
    """No hardcoded /sandbox/ absolute paths in source — bind-mount can move.

    Applies to ALL scripts (library + CLI) — library-level constants
    propagate to runtime via imports, so they're just as load-bearing.

    Allowlisted: scripts waiting for paths.py refactor (twitter_session,
    telegram_session). Document the reason + track as drift debt.
    """
    org, declared, path = case
    if path.name in _ALLOWLIST:
        pytest.skip(f"allowlisted: {_ALLOWLIST[path.name]}")
    source = path.read_text()
    violations = has_hardcoded_sandbox_path(source)
    if violations:
        pytest.fail(
            f"{org}:{declared} hardcodes /sandbox/ in runtime code "
            f"(use paths.py or __file__-relative lookup):\n  - " +
            "\n  - ".join(violations[:5])
        )


# --------------------------------------------------------------------------- #
# Meta: allowlist itself must be valid YAML + every entry must name a real script
# --------------------------------------------------------------------------- #

def test_allowlist_yaml_well_formed() -> None:
    """The allowlist must parse + every entry references a real script."""
    if not ALLOWLIST_PATH.exists():
        pytest.skip("no allowlist file")
    data = yaml.safe_load(ALLOWLIST_PATH.read_text())
    assert isinstance(data, dict), "allowlist must be a mapping"
    assert "allowlist" in data, "allowlist must have an 'allowlist:' key"

    seen = set()
    for entry in data["allowlist"]:
        assert "script" in entry, f"entry missing 'script': {entry}"
        assert "reason" in entry, f"entry missing 'reason': {entry}"
        assert entry["script"] not in seen, f"duplicate allowlist entry: {entry['script']}"
        seen.add(entry["script"])


def test_allowlist_entries_reference_real_scripts() -> None:
    """Every allowlisted script name must exist in some org's init_files.

    Stale allowlist entries (for scripts that were renamed/deleted) silently
    disable the contract; this test catches the drift.
    """
    if not _ALLOWLIST:
        pytest.skip("no allowlist entries")
    real_names = {path.name for _, _, path in _SCRIPTS}
    for script_name in _ALLOWLIST:
        if script_name not in real_names:
            pytest.fail(
                f"allowlist entry {script_name!r} does not match any "
                f"init_files script. Stale entry — remove it."
            )


# --------------------------------------------------------------------------- #
# CLI entrypoint for direct invocation
# --------------------------------------------------------------------------- #

if __name__ == "__main__":
    print(f"Discovered {len(_SCRIPTS)} System A scripts across orgs/")
    if _ALLOWLIST:
        print(f"Allowlist: {len(_ALLOWLIST)} entries")
    failures = 0
    for org, declared, path in _SCRIPTS:
        if path.name in _ALLOWLIST:
            print(f"  SKIP {org}:{declared} (allowlisted: {_ALLOWLIST[path.name]})")
            continue
        source = path.read_text()
        is_cli = has_main_guard(source)
        try:
            if is_cli:
                r = subprocess.run(
                    [sys.executable, str(path), "--help"],
                    capture_output=True, text=True, timeout=10,
                )
                if r.returncode != 0 and not _is_graceful_dep_missing(r):
                    print(f"  FAIL {org}:{declared} --help exited {r.returncode}")
                    failures += 1
                    continue
                r = subprocess.run(
                    [sys.executable, str(path), "--bogus-flag-x"],
                    capture_output=True, text=True, timeout=10,
                )
                if r.returncode == 0:
                    try:
                        data = json.loads(r.stdout.strip().splitlines()[0])
                        if not (isinstance(data, dict) and "error" in data):
                            print(f"  FAIL {org}:{declared} accepted --bogus-flag-x")
                            failures += 1
                            continue
                    except (json.JSONDecodeError, IndexError):
                        print(f"  FAIL {org}:{declared} accepted --bogus-flag-x")
                        failures += 1
                        continue
            # Hardcoded path check applies to library + CLI.
            violations = has_hardcoded_sandbox_path(source)
            if violations:
                print(f"  FAIL {org}:{declared} hardcodes /sandbox/: {violations[0]}")
                failures += 1
                continue
            label = "CLI " if is_cli else "LIB "
            print(f"  OK   {label} {org}:{declared}")
        except subprocess.TimeoutExpired:
            print(f"  FAIL {org}:{declared} --help timed out")
            failures += 1
    print()
    if failures:
        print(f"{failures} script(s) failed contract")
        sys.exit(1)
    print("All System A scripts conform.")
