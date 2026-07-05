"""Shared constants, utilities, and the specialist roster for the tools/ package.

Extracted from the original monolithic ``tools.py`` to break circular-dependency
risk: every file in this package imports from ``_shared``; ``_shared`` imports
from NOTHING in this package.
"""

from __future__ import annotations

import json
import logging
from pathlib import Path

from pydantic import BaseModel

log = logging.getLogger(__name__)

PUX_PREFIX = "pux_sandbox_"
PUX_GRADER_PREFIX = "pux_grader_"
PROJECT_ROOT = Path(__file__).resolve().parents[3]
SKILL_FILE = "SKILL.md"


def _skills_dirs(org: str | None = None) -> list[Path]:
    """Skills-ROOT directories to search, highest-priority first.

    With an ``org``: that org's ``skills/`` wins, then ``orgs/_shared/skills``,
    then every other org's skills (so a cross-org skill is still discoverable).
    Without an ``org`` (the offline ``--check`` smoke path): all roots in stable
    sorted order, no priority. Non-existent dirs are filtered out.

    Scans both ``orgs/`` and ``orgs/specialists/`` for org skills dirs."""
    orgs = PROJECT_ROOT / "orgs"
    roots: list[Path] = []
    if org:
        for candidate in [orgs / org / "skills", orgs / "specialists" / org / "skills"]:
            if candidate.is_dir():
                roots.append(candidate)
                break
    roots.append(orgs / "_shared" / "skills")
    seen = {str(r) for r in roots}
    for base in [orgs, orgs / "specialists"]:
        if base.is_dir():
            for p in sorted(base.glob("*/skills")):
                if str(p) not in seen:
                    roots.append(p)
                    seen.add(str(p))
    return [r for r in roots if r.is_dir()]


# The complete set of unprefixed specialist names the harness implements
# natively.
SPECIALISTS: frozenset[str] = frozenset({
    "python", "list_skills", "load_skill", "describe_image", "multimodal", "multimodal_mega",
    "browser_navigate", "browser_click", "browser_type", "browser_screenshot", "browser_evaluate",
    "browser_search", "browser_scroll", "browser_go_back", "browser_wait", "browser_find_text",
    "browser_extract", "browser_extract_images", "browser_save_screenshot", "browser_download",
    "browser_upload", "browser_tabs", "browser_new_tab", "browser_switch_tab", "browser_close_tab",
    "browser_dropdown_options", "browser_select_dropdown",
    "browser_save_session", "browser_restore_session",
    "desktop_screenshot", "desktop_click", "desktop_type", "desktop_key",
})

SPECIALIST_TOOL_NAMES: frozenset[str] = frozenset(
    {PUX_PREFIX + s for s in SPECIALISTS}
)


def _tail(text: str, n: int = 800) -> str:
    """Last ``n`` chars of ``text`` — keeps stderr tails out of result
    envelopes without leaking megabytes. Mirrors the Go ``tailOutput`` helper."""
    return text if len(text) <= n else "..." + text[len(text) - n:]


def _result(obj: dict) -> str:
    """Serialize a tool-result dict to the exact JSON the Go bridge surfaced
    (2-space indent + sorted map keys at every level)."""
    return json.dumps(obj, indent=2, sort_keys=True)


class _NoArgs(BaseModel):
    """Schema for argument-less tools (list_skills, browser_tabs, etc.)."""
