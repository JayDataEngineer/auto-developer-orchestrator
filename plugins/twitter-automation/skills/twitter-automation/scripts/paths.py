#!/usr/bin/env python3
"""Canonical path resolver for skill scripts.

Single source of truth for session/credential/artifact paths so sibling
scripts never hardcode them. Defaults are repo-relative: this module lives
at ``plugins/<family>/skills/<family>/scripts/paths.py`` (or
``.deepagents/skills/<skill>/scripts/`` for workspace skills); the
workspace root is found by walking up to the nearest ``.git``, and runtime
data lands under ``<root>/data/``. Every path is overridable via env vars
(no prefix — plain names).

USAGE
-----
    from paths import browser_session, telegram_credentials

    session = browser_session("linkedin.com")
    if not session.exists():
        print(json.dumps({"error": f"no browser session at {session}"}))
        sys.exit(1)

LAYOUT
------
- ``plugins/<family>/skills/<family>/scripts/`` — these scripts (git-tracked)
- ``<workspace root>/`` — the repo you run dcode in
- ``<workspace root>/data/`` — runtime data (sessions, credentials, caches)
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


_WORKSPACE_ROOT = _workspace_root()


def workspace_root() -> Path:
    """The workspace (repo) root this skill lives in."""
    return _env_path("WORKSPACE_ROOT", _WORKSPACE_ROOT)


def data_dir() -> Path:
    """Project-scoped data dir (default: <workspace root>/data).

    Created on first access if missing.
    """
    d = _env_path("DATA_DIR", workspace_root() / "data")
    d.mkdir(parents=True, exist_ok=True)
    return d


def scripts_dir() -> Path:
    """Agent-authored scratch dir (default: <workspace root>/scripts)."""
    return _env_path("SCRIPTS_DIR", workspace_root() / "scripts")


# --------------------------------------------------------------------- #
# Generic browser sessions (any domain, any host browser)
# --------------------------------------------------------------------- #
# This is the canonical path family for host-extracted browser cookies.
# ``extract_browser_cookies.py`` writes JSON files here; the browser MCP's
# ``restore_session`` tool reads them back. Domain-keyed so the
# same org can hold sessions for multiple sites (linkedin.com, x.com, ...)
# without collisions. Twitter-specific helpers below are kept for back-compat
# with older bootstraps but new code should prefer browser_session(domain).

def browser_session(domain: str) -> Path:
    """Return the canonical session JSON path for the given domain.

    Output of ``extract_browser_cookies.py --domain <domain>`` is expected
    here. File shape matches what ``restore_session`` consumes (cookies +
    localStorage + url + saved_at + source + domain).

    The filename is derived by stripping the leading subdomain so
    ``linkedin.com`` and ``www.linkedin.com`` resolve to the same file.
    """
    safe = domain.lstrip(".").split(":")[0]  # strip port if present
    if safe.startswith("www."):
        safe = safe[4:]
    return _env_path(
        f"BROWSER_SESSION_{safe.replace('.', '_').upper()}",
        data_dir() / f".browser-session-{safe}.json",
    )


def browser_session_search(domain: str) -> list[Path]:
    """Return candidate session paths for the given domain.

    Returns a single-element list with the canonical session path.
    """
    return [
        browser_session(domain),
    ]


# --------------------------------------------------------------------- #
# Twitter session + artifacts
# --------------------------------------------------------------------- #

def twitter_cookies() -> Path:
    """Twitter session cookies JSON. Project-scoped via data_dir."""
    return _env_path("TWITTER_COOKIES", data_dir() / ".twitter-session.json")


def twitter_calendar() -> Path:
    """Posting calendar JSON. Project-scoped via workspace_root."""
    return _env_path("TWITTER_CALENDAR", workspace_root() / "calendar.json")


def twitter_drafts() -> Path:
    """Draft tweets JSON. Project-scoped via workspace_root."""
    return _env_path("TWITTER_DRAFTS", workspace_root() / "drafts.json")


# --------------------------------------------------------------------- #
# Telegram session + credentials
# --------------------------------------------------------------------- #

def telegram_credentials() -> Path:
    """Telegram API credentials JSON (api_id, api_hash, phone)."""
    return _env_path("TELEGRAM_CREDENTIALS", data_dir() / ".telegram-credentials.json")


def telegram_session() -> Path:
    """Telethon SQLite session file (auth state)."""
    return _env_path("TELEGRAM_SESSION", data_dir() / ".telegram-session.session")



# --------------------------------------------------------------------- #
# Sibling module resolver (for scripts that import other scripts/*.py)
# --------------------------------------------------------------------- #

def sandbox_module(name: str) -> Path:
    """Resolve a sibling sandbox module by basename.

    Used by scripts that need to dynamically import another backbone script
    (e.g. ``video_frames`` → ``surreal_client``). Returns the first existing
    candidate from a resolution chain:

    1. ``SKILL_MODULE_<NORMALIZED>`` env var (explicit override)
    2. ``<sandbox_root>/<name>`` (in-container, chmod 0444)
    3. ``<cwd>/<name>`` (dev / test / project-local)
    4. ``<this file's dir>/<name>`` (sibling discovery)

    Falls back to the in-container path so error messages are useful when
    nothing exists.
    """
    normalized = name.upper().replace(".", "_").replace("-", "_")
    env_var = f"SKILL_MODULE_{normalized}"
    candidates = [
        _env_path(env_var, Path("/nonexistent")),  # explicit override or miss
        Path(__file__).resolve().parent / name,    # sibling script (default)
        Path.cwd() / name,
    ]
    for c in candidates:
        if c.exists():
            return c
    return Path(__file__).resolve().parent / name  # useful default for errors


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
        "browser_session_x_com": str(browser_session("x.com")),
        "browser_session_linkedin_com": str(browser_session("linkedin.com")),
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
