"""Model-keyed harness profiles — the MCP scoping bridge for dcode.

dcode subagents inherit the main agent's full toolset (uniform MCP
inheritance; measured in docs/isolation-patterns.md). Until the upstream
per-subagent ``tools:`` frontmatter PR lands, the one sanctioned seam for
per-subagent tool scoping is deepagents' model-keyed harness profiles:

    subagent frontmatter ``model: openai:glm-5-turbo``
      -> the subagent re-runs profile resolution against ITS OWN model
      -> this profile's ``excluded_tools`` installs _ToolExclusionMiddleware
         on THAT subagent only (siblings and the main agent are untouched)

Every ``profiles/*.yaml`` next to this module registers one profile. The
registry key is explicit in the file (``key:``), so filenames stay cosmetic.

This is a DENY list and therefore fails OPEN: a server that adds a tool
after this list was written leaks into scoped subagents. ``make
scoping-check`` (profiles/scoping_check.py) is the tripwire — it rebuilds
the profiles, computes every scoped agent's effective MCP toolset and fails
loudly naming any leak, including the case where this plugin itself failed
to register (deepagents isolates plugin errors, so a broken registration is
silent without the check).
"""

from __future__ import annotations

from pathlib import Path

import yaml
from deepagents import HarnessProfileConfig, register_harness_profile

_PROFILES_DIR = Path(__file__).parent / "profiles"


def register() -> None:
    """Register every shipped YAML profile (entry-point target, zero-arg).

    Raises on malformed files — the caller (deepagents' profile bootstrap)
    isolates the failure per-plugin, and ``make scoping-check`` fails loudly
    when the profile is missing from the registry.
    """
    for yaml_path in sorted(_PROFILES_DIR.glob("*.yaml")):
        data = yaml.safe_load(yaml_path.read_text())
        if not isinstance(data, dict) or "key" not in data or "excluded_tools" not in data:
            msg = f"{yaml_path.name}: expected 'key' and 'excluded_tools' fields"
            raise ValueError(msg)
        key = data.pop("key")
        register_harness_profile(key, HarnessProfileConfig.from_dict(data))
