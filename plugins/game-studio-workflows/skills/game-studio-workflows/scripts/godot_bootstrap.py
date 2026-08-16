#!/usr/bin/env python3
"""Download headless Godot from GitHub releases into the sandbox.

Resolution order: (1) ``godot`` on PATH → use it, zero download; (2) cached
cached binary → use it; (3) download the latest stable 4.x
Linux x86_64 release zip from godotengine/godot-builds, extract, chmod +x.

The Godot 4.x binary is a single executable — the SAME binary runs headless
(``--headless``), as an editor (``-e``), or exports builds (``--export-release``).
There is no separate "headless server" build; the flag is the switch.

Usage:
  python3 godot_bootstrap.py          # ensure + print the binary path
  python3 godot_bootstrap.py --check  # exit 0 if available, 1 if not

Env:
  GODOT_BOOTSTRAP_VERSION  # pin a version (default: latest stable 4.x)
  GODOT_BOOTSTRAP_DIR      # install dir (default: <repo>/.cache/godot)
"""
from __future__ import annotations

import json
import os
import shutil
import stat
import subprocess
import sys
import urllib.request
import urllib.error
import zipfile
from pathlib import Path


def _workspace_root():
    """Layout-robust workspace root: env override, else nearest .git ancestor,
    else cwd (works in-repo under plugins/, in dcode's plugin cache, anywhere)."""
    for var in ("WORKSPACE_ROOT", "CLAUDE_PROJECT_DIR"):
        v = os.environ.get(var)
        if v:
            return Path(v).expanduser()
    for anc in Path(__file__).resolve().parents:
        if (anc / ".git").exists():
            return anc
    return Path.cwd()


_API = "https://api.github.com"
_REPO = "godotengine/godot-builds"
_TIMEOUT = 120  # Godot zip is ~72 MB


def _on_path() -> str | None:
    """Return the godot binary path if on PATH, else None."""
    return shutil.which("godot") or shutil.which("godot4")


def _arch_asset() -> str:
    """Return the release-asset name pattern for this host's arch.

    Godot names Linux x86_64 assets ``Godot_v<version>_linux.x86_64.zip``.
    ARM64 hosts get the ``arm64`` variant.
    """
    import platform
    machine = platform.machine().lower()
    if machine in ("aarch64", "arm64"):
        return "linux.arm64"
    return "linux.x86_64"


def _latest_stable_4x() -> str | None:
    """Fetch the latest stable (non-dev/rc/beta) 4.x tag from the releases API."""
    try:
        req = urllib.request.Request(
            f"{_API}/repos/{_REPO}/releases?per_page=20",
            headers={"Accept": "application/vnd.github+json"},
        )
        tok = os.environ.get("GITHUB_TOKEN") or os.environ.get(
            "GITHUB_PERSONAL_ACCESS_TOKEN"
        )
        if tok:
            req.add_header("Authorization", f"Bearer {tok}")
        with urllib.request.urlopen(req, timeout=_TIMEOUT) as r:
            releases = json.loads(r.read().decode())
    except Exception:
        return None
    for rel in releases:
        tag = rel.get("tag_name", "")
        if tag.startswith("4.") and "-stable" in tag:
            return tag
    return None


def _download_and_extract(tag: str, dest_dir: Path) -> Path | None:
    """Download the release zip for *tag* and extract the binary to *dest_dir*."""
    try:
        req = urllib.request.Request(
            f"{_API}/repos/{_REPO}/releases/tags/{tag}",
            headers={"Accept": "application/vnd.github+json"},
        )
        tok = os.environ.get("GITHUB_TOKEN") or os.environ.get(
            "GITHUB_PERSONAL_ACCESS_TOKEN"
        )
        if tok:
            req.add_header("Authorization", f"Bearer {tok}")
        with urllib.request.urlopen(req, timeout=_TIMEOUT) as r:
            release = json.loads(r.read().decode())
    except Exception:
        return None

    arch_pat = _arch_asset()
    asset_url = None
    asset_name = None
    for a in release.get("assets", []):
        name = a.get("name", "")
        if arch_pat in name and name.endswith(".zip") and "mono" not in name:
            asset_url = a.get("browser_download_url")
            asset_name = name
            break
    if not asset_url:
        return None

    dest_dir.mkdir(parents=True, exist_ok=True)
    tmp_zip = dest_dir / (asset_name or "godot.zip")
    try:
        urllib.request.urlretrieve(asset_url, tmp_zip)
        with zipfile.ZipFile(tmp_zip) as zf:
            # The zip contains one executable (e.g. "Godot_v4.7-stable_linux.x86_64")
            for member in zf.namelist():
                extracted = zf.extract(member, dest_dir)
                os.chmod(extracted, os.stat(extracted).st_mode | stat.S_IEXEC)
        tmp_zip.unlink(missing_ok=True)
    except Exception:
        tmp_zip.unlink(missing_ok=True)
        return None

    # Find the extracted binary — the zip yields one executable whose name
    # starts with "Godot". Don't filter by suffix: the asset name has dots
    # in the arch suffix (e.g. "Godot_v4.7-stable_linux.x86_64") so
    # Path.suffix returns ".x86_64", not "".
    for p in sorted(dest_dir.iterdir(), key=lambda x: len(x.name)):
        if p.is_file() and os.access(p, os.X_OK) and p.name.startswith("Godot"):
            return p
    return None


def ensure_godot() -> str | None:
    """Return the path to a usable godot binary, downloading if necessary.

    Resolution: PATH → cache ($GODOT_BOOTSTRAP_DIR or <repo>/.cache/godot) → GitHub release download.
    Returns None on total failure (never raises — the caller decides what
    to do).
    """
    # (1) on PATH
    on_path = _on_path()
    if on_path:
        return on_path

    dest_dir = Path(os.environ.get(
        "GODOT_BOOTSTRAP_DIR",
        str(_workspace_root() / ".cache" / "godot"),
    ))

    # (2) cache hit — any Godot executable in dest_dir from a prior run
    if dest_dir.is_dir():
        for p in dest_dir.iterdir():
            if p.is_file() and os.access(p, os.X_OK) and p.name.startswith("Godot"):
                return str(p)

    # (3) download
    version = os.environ.get("GODOT_BOOTSTRAP_VERSION")
    if not version:
        version = _latest_stable_4x()
    if not version:
        return None

    binary = _download_and_extract(version, dest_dir)
    return str(binary) if binary else None


def main() -> int:
    check_only = "--check" in sys.argv
    path = ensure_godot()
    if path is None:
        if check_only:
            return 1
        print("GODOT_UNAVAILABLE", file=sys.stderr)
        return 1
    if check_only:
        return 0
    # Print the path so callers can capture it
    print(path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
