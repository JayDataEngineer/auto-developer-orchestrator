"""The tripwire front-door — every repo-wide check in
``tests/guards/tripwire_checks.py`` is asserted green here against the real
source tree (``verify-or-die``: a tripwire that stops firing is a deleted
guard, not a passing property). Provocations drive the pure, path-parameterised
scanners directly to prove each rule still bites.
"""

from __future__ import annotations

import pytest

from tests.guards import tripwire_checks as tc

pytestmark = pytest.mark.guards

GREEN_CHECKS: list[tuple[str, object]] = [
    ("kit-import-isolation", tc._kit_import_isolation),
    ("no-pux-harness-refs", tc._no_pux_harness_refs),
]


@pytest.mark.parametrize("rule,check", GREEN_CHECKS, ids=[rule for rule, _ in GREEN_CHECKS])
def test_tripwire_green_on_real_repo(rule, check):
    """Each tripwire is clean on the shipped source — green today, enforced
    forever."""
    problems = check()
    assert problems == [], f"{rule}: {problems}"
