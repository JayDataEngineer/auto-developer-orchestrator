"""PERMANENT contract: the developer guide and the runtime base prompt are
structurally separate.

The repo root ``/AGENTS.md`` is a **developer guide** (branch strategy, testing
rules, architecture, dropped-feature history). It is NOT a runtime prompt. The
agent base prompt lives at ``orgs/general/AGENTS.md`` and flows to specialist
orgs via ``extends: general`` (the chain overlay). Nothing reads the root
``AGENTS.md`` at prompt-build time (``load_root_prompt`` was eliminated — see
``feedback_no_legacy_left_behind``).

If that separation ever regresses — e.g. someone reintroduces a root-``AGENTS.md``
read into ``build_system_prompt``, or a flatten step bakes the dev guide instead
of the base into a pack — these tests fail permanently. They are the
``test_server_retired``-style tripwire for the base/dev-guide split.

Two contracts, each proven for EVERY discovered org AND for the export
(flatten) path:

1. **Dev-guide never leaks.** Markers unique to the developer guide
   (``Branch strategy``, ``Testing harness``, ``What's NOT here``, ``pi-pivot``,
   ``Per-org harness profile``, ``console_scripts``) are ABSENT from every org's
   base prompt — in-source AND baked into a pack.
2. **Base is present.** Markers unique to the base org ``general``
   (``Orchestrator pattern``, ``Verify or die``) are PRESENT in every org's base
   prompt — the base flows to specialists via ``extends:``, not via a root read.
"""
from __future__ import annotations

import tarfile

import pytest

from pux_harness.agent.orgs import (
    build_system_prompt,
    discover_orgs,
    org_extends_chain,
)
from pux_harness.pack import pack_org

# Markers that live ONLY in the developer guide (root /AGENTS.md). Each appears
# exactly once in the dev guide and nowhere in any org overlay — so presence in
# an org's base prompt can only mean the root guide leaked back in.
DEV_GUIDE_MARKERS = [
    "Branch strategy",
    "Testing harness",
    "What's NOT here",
    "pi-pivot",
    "Per-org harness profile",
    "console_scripts",
]

# Markers that live in the base org (orgs/general/AGENTS.md). Every org CTO must
# start from the base — either it IS general or it ``extends: general``.
BASE_MARKERS = [
    "Orchestrator pattern",
    "Verify or die",
]

# Orgs that are intentionally standalone — they don't ``extends: general``
# (see each org.yaml for the rationale). Exempt from the base-present contract.
STANDALONE_ORGS = frozenset({"browser-agent", "fs-explorer", "web-search"})


def _dev_guide_leaks(prompt: str) -> list[str]:
    return [m for m in DEV_GUIDE_MARKERS if m in prompt]


def _base_missing(prompt: str) -> list[str]:
    return [m for m in BASE_MARKERS if m not in prompt]


# ---------------------------------------------------------------------------
# Contract 1 + 2 — every org's in-source base prompt
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("org", sorted(discover_orgs()))
def test_dev_guide_never_leaks_into_org_base(org: str) -> None:
    """No developer-guide marker appears in any org's built base prompt."""
    prompt = build_system_prompt(org)
    leaks = _dev_guide_leaks(prompt)
    assert not leaks, (
        f"developer-guide markers leaked into {org}'s base prompt: {leaks}. "
        f"The root /AGENTS.md must NOT be read at prompt-build time."
    )


@pytest.mark.parametrize("org", sorted(discover_orgs()))
def test_base_present_in_every_org(org: str) -> None:
    """Every org's base prompt carries the base-org markers — the base flows
    from ``general`` via ``extends:``, so a specialist that dropped its
    ``extends: general`` (or general that lost its base) fails here.

    Standalone orgs (``fs-explorer``, ``web-search``) intentionally DON'T
    extend general — they're exempt (see ``STANDALONE_ORGS``)."""
    if org in STANDALONE_ORGS:
        pytest.skip(f"{org} is a standalone org (does not extend: general)")
    prompt = build_system_prompt(org)
    missing = _base_missing(prompt)
    assert not missing, (
        f"base markers missing from {org}'s base prompt: {missing}. "
        f"Every org must start from general's base (``extends: general``)."
    )


# ---------------------------------------------------------------------------
# Contract 1 + 2 — the export (flatten) path
# ---------------------------------------------------------------------------

def _extending_orgs() -> list[str]:
    """Specialists whose chain has a parent (``extends:`` length > 1) — the
    orgs for which the flatten step bakes the chain overlay into the pack."""
    return [o for o in discover_orgs() if len(org_extends_chain(o)) > 1]


@pytest.mark.parametrize("org", _extending_orgs())
def test_pack_bakes_base_not_dev_guide(org: str, tmp_path) -> None:
    """The flatten step bakes the chain overlay (general's base + the org's
    overlay) into the packed ``orgs/<org>/AGENTS.md`` — NOT the developer guide.
    A packed org is runtime-standalone: its base prompt is a frozen literal with
    zero reference to ``orgs/general/`` or the root ``AGENTS.md``."""
    output = tmp_path / f"{org}.tar.gz"
    pack_org(org, output)

    member = f"{org}/orgs/{org}/AGENTS.md"
    with tarfile.open(output, "r:gz") as tar:
        f = tar.extractfile(member)
        assert f is not None, f"{member} missing from {org} pack"
        baked = f.read().decode("utf-8")

    leaks = _dev_guide_leaks(baked)
    assert not leaks, (
        f"developer-guide markers baked into {org}'s packed AGENTS.md: {leaks}"
    )
    missing = _base_missing(baked)
    assert not missing, (
        f"base markers missing from {org}'s packed AGENTS.md: {missing}. "
        f"The flatten step must bake general's base (chain overlay), not a "
        f"root-guide read."
    )
