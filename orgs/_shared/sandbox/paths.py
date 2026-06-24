#!/usr/bin/env python3
"""Canonical path resolver for org sandbox scripts.

WHY THIS EXISTS
---------------
System A backbone scripts used to hardcode ``/sandbox/<file>`` paths for
cookies, credentials, calendars. That drifted: the same constant was
defined in 3 places (session, post, helpers), and any layout change broke
them silently.

This module is the single source of truth. Scripts import from here
instead of hardcoding. Defaults match the in-container layout
(``/sandbox/<name>.py`` for init_files, ``/sandbox/workspace/`` for the
project bind-mount). Override via env vars for testing or non-standard
layouts.

USAGE
-----
    from paths import twitter_cookies, telegram_credentials

    cookies_path = twitter_cookies()
    if not cookies_path.exists():
        print(json.dumps({"error": f"no twitter session at {cookies_path}"}))
        sys.exit(1)

LAYOUT
------
- ``/sandbox/<name>.py`` — backbone scripts (chmod 0444, git-tracked)
- ``/sandbox/workspace/`` — project bind-mount (host: <project>/)
- ``/sandbox/workspace/data/`` — project-scoped data (host: <project>/data/)
- ``/sandbox/workspace/scripts/`` — agent-authored scratch (System B)

The data/ dir is the canonical home for session files because it survives
container restarts and is reachable from both host extractors (e.g.
twitter-agent/scripts/extract_brave_cookies.py) and in-container code.
"""
from __future__ import annotations

import os
from pathlib import Path


def _env_path(env_var: str, default: Path) -> Path:
    """Resolve a path from env, falling back to default."""
    val = os.environ.get(env_var)
    if val:
        return Path(val).expanduser()
    return default


def sandbox_root() -> Path:
    """Root of the in-container sandbox layout (default: /sandbox)."""
    return _env_path("PUX_SANDBOX_ROOT", Path("/sandbox"))


def workspace_root() -> Path:
    """Project bind-mount root (default: /sandbox/workspace)."""
    return _env_path("PUX_WORKSPACE_ROOT", sandbox_root() / "workspace")


def data_dir() -> Path:
    """Project-scoped data dir (default: /sandbox/workspace/data).

    Created on first access if missing.
    """
    d = _env_path("PUX_DATA_DIR", workspace_root() / "data")
    d.mkdir(parents=True, exist_ok=True)
    return d


def scripts_dir() -> Path:
    """System B agent-authored scratch dir (default: /sandbox/workspace/scripts)."""
    return _env_path("PUX_SCRIPTS_DIR", workspace_root() / "scripts")


# --------------------------------------------------------------------- #
# Twitter session + artifacts
# --------------------------------------------------------------------- #

def twitter_cookies() -> Path:
    """Twitter session cookies JSON. Project-scoped via data_dir.

    Canonical home is ``data_dir() / .twitter-session.json``. Bootstraps
    that write directly to ``/sandbox/.twitter-session.json`` (legacy
    in-container root) are still read via ``twitter_cookies_legacy()``.
    """
    return _env_path("PUX_TWITTER_COOKIES", data_dir() / ".twitter-session.json")


def twitter_cookies_legacy() -> Path:
    """Legacy in-container root for Twitter cookies.

    Kept as a fallback chain candidate so old bootstrap outputs (VNC login,
    --cookies-from-browser writing directly to /sandbox/) keep working
    after the path refactor.
    """
    return _env_path("PUX_TWITTER_COOKIES_LEGACY", sandbox_root() / ".twitter-session.json")


def twitter_calendar() -> Path:
    """Posting calendar JSON. Project-scoped via workspace_root."""
    return _env_path("PUX_TWITTER_CALENDAR", workspace_root() / "calendar.json")


def twitter_calendar_legacy() -> Path:
    """Legacy in-container root for the posting calendar."""
    return _env_path("PUX_TWITTER_CALENDAR_LEGACY", sandbox_root() / "calendar.json")


def twitter_drafts() -> Path:
    """Draft tweets JSON. Project-scoped via workspace_root."""
    return _env_path("PUX_TWITTER_DRAFTS", workspace_root() / "drafts.json")


def twitter_drafts_legacy() -> Path:
    """Legacy in-container root for draft tweets."""
    return _env_path("PUX_TWITTER_DRAFTS_LEGACY", sandbox_root() / "drafts.json")


# --------------------------------------------------------------------- #
# Telegram session + credentials
# --------------------------------------------------------------------- #

def telegram_credentials() -> Path:
    """Telegram API credentials JSON (api_id, api_hash, phone)."""
    return _env_path("PUX_TELEGRAM_CREDENTIALS", data_dir() / ".telegram-credentials.json")


def telegram_credentials_legacy() -> Path:
    """Legacy in-container root for Telegram credentials."""
    return _env_path("PUX_TELEGRAM_CREDENTIALS_LEGACY", sandbox_root() / ".telegram-credentials.json")


def telegram_session() -> Path:
    """Telethon SQLite session file (auth state)."""
    return _env_path("PUX_TELEGRAM_SESSION", data_dir() / ".telegram-session.session")


def telegram_session_legacy() -> Path:
    """Legacy in-container root for Telethon session."""
    return _env_path("PUX_TELEGRAM_SESSION_LEGACY", sandbox_root() / ".telegram-session.session")


# --------------------------------------------------------------------- #
# Sibling module resolver (for scripts that import other /sandbox/*.py)
# --------------------------------------------------------------------- #

def sandbox_module(name: str) -> Path:
    """Resolve a sibling sandbox module by basename.

    Used by scripts that need to dynamically import another backbone script
    (e.g. ``video_frames`` → ``surreal_client``). Returns the first existing
    candidate from a resolution chain:

    1. ``PUX_SANDBOX_MODULE_<NORMALIZED>`` env var (explicit override)
    2. ``<sandbox_root>/<name>`` (in-container, chmod 0444)
    3. ``<cwd>/<name>`` (dev / test / project-local)
    4. ``<this file's dir>/<name>`` (sibling discovery)

    Falls back to the in-container path so error messages are useful when
    nothing exists.
    """
    normalized = name.upper().replace(".", "_").replace("-", "_")
    env_var = f"PUX_SANDBOX_MODULE_{normalized}"
    candidates = [
        _env_path(env_var, Path("/nonexistent")),  # explicit override or miss
        sandbox_root() / name,
        Path.cwd() / name,
        Path(__file__).resolve().parent / name,
    ]
    for c in candidates:
        if c.exists():
            return c
    return sandbox_root() / name  # useful default for error messages


# --------------------------------------------------------------------- #
# CLI (for agent-facing introspection)
# --------------------------------------------------------------------- #

def _main() -> int:
    """Print all resolved paths as JSON. Used by debugging + agent introspection.

    Accepts no flags — every path is resolved from env vars or defaults.
    Unknown flags exit non-zero (argparse default) so the contract test
    can prove the CLI surface is bounded.
    """
    import argparse
    import json
    parser = argparse.ArgumentParser(
        prog="paths.py",
        description="Print all resolved sandbox paths as JSON.",
    )
    parser.parse_args()  # rejects unknown flags
    paths = {
        "sandbox_root": str(sandbox_root()),
        "workspace_root": str(workspace_root()),
        "data_dir": str(data_dir()),
        "scripts_dir": str(scripts_dir()),
        "twitter_cookies": str(twitter_cookies()),
        "twitter_calendar": str(twitter_calendar()),
        "twitter_drafts": str(twitter_drafts()),
        "telegram_credentials": str(telegram_credentials()),
        "telegram_session": str(telegram_session()),
    }
    print(json.dumps(paths, indent=2))
    return 0


if __name__ == "__main__":
    import sys
    sys.exit(_main())
