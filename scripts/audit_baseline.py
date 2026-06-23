#!/usr/bin/env python3
"""
audit_baseline — principle 6 helper: prompts are tested, not asserted.

Wraps audit_lib to compare a session's tag rates against a checked-in
baseline JSON. The baseline stores expected rates per tag; any session
that regresses (rate exceeds baseline by >50%) is flagged.

Workflow:
    task audit-baseline-set SESSION=.pux/sessions/foo.jsonl
        # captures session rates as the new baseline at scripts/audit_baseline.json

    task audit-baseline SESSION=.pux/sessions/bar.jsonl
        # compares session rates to baseline; exits non-zero on regressions

The baseline should be refreshed when prompts materially change (e.g.,
diligence substrate updated, new diligence landmines added). Commit
the new baseline alongside the prompt change so reviewers see the
delta.
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

SCRIPTS_DIR = Path(__file__).resolve().parent
if str(SCRIPTS_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPTS_DIR))

import audit_lib  # noqa: E402

BASELINE_PATH = SCRIPTS_DIR / "audit_baseline.json"
REGRESSION_THRESHOLD = 1.5  # session rate > 1.5x baseline = regression


def cmd_set(args: argparse.Namespace) -> int:
    """Capture a session's rates as the new baseline."""
    _, turns = audit_lib.load_transcript(args.session)
    _, summary = audit_lib.audit(turns)
    baseline = {
        "source_session": str(args.session),
        "total_turns": summary["total_turns"],
        "tag_rates": summary["tag_rates"],
        "note": (
            "Baseline rates for the Fable/Mythos six-pattern taxonomy. "
            "Refresh when prompts materially change. See "
            "CLAUDE.md → Transcript Auditing → Audit Baseline."
        ),
    }
    BASELINE_PATH.write_text(json.dumps(baseline, indent=2) + "\n")
    print(f"Baseline written: {BASELINE_PATH}")
    print(f"  source: {args.session}")
    print(f"  turns:  {baseline['total_turns']}")
    for tag, rate in baseline["tag_rates"].items():
        print(f"  {tag:42s} {rate:.1%}")
    return 0


def cmd_compare(args: argparse.Namespace) -> int:
    """Compare session rates to baseline; flag regressions."""
    if not BASELINE_PATH.exists():
        print(f"Baseline file not found: {BASELINE_PATH}", file=sys.stderr)
        print("Run `task audit-baseline-set SESSION=<path>` first.", file=sys.stderr)
        return 1

    baseline = json.loads(BASELINE_PATH.read_text())
    _, turns = audit_lib.load_transcript(args.session)
    _, summary = audit_lib.audit(turns)

    regressions = []
    print(f"Comparing {args.session}")
    print(f"  against baseline from {baseline.get('source_session', '?')}")
    print(f"  baseline turns: {baseline['total_turns']}  session turns: {summary['total_turns']}")
    print()
    print(f"  {'tag':42s} {'baseline':>10s}  {'session':>10s}  {'delta':>10s}  status")
    print(f"  {'-' * 42}  {'-' * 10}  {'-' * 10}  {'-' * 10}  {'-' * 7}")

    for tag in audit_lib.PATTERN_TAGS:
        base_rate = baseline["tag_rates"].get(tag, 0.0)
        sess_rate = summary["tag_rates"].get(tag, 0.0)
        delta = sess_rate - base_rate
        if sess_rate > base_rate * REGRESSION_THRESHOLD and sess_rate > 0:
            status = "REGRESSION"
            regressions.append(tag)
        elif sess_rate < base_rate * 0.5 and base_rate > 0:
            status = "improved"
        else:
            status = "ok"
        print(f"  {tag:42s}  {base_rate:>9.1%}   {sess_rate:>9.1%}   {delta:>+9.1%}  {status}")

    print()
    if regressions:
        print(f"FAIL: {len(regressions)} regression(s): {', '.join(regressions)}", file=sys.stderr)
        print("If intentional (e.g., you added a new landmine pattern that flags more), "
              "update the baseline: `task audit-baseline-set SESSION=<this session>`.",
              file=sys.stderr)
        return 2
    print("OK: no regressions vs baseline.")
    return 0


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_set = sub.add_parser("set", help="Capture a session's rates as the new baseline")
    p_set.add_argument("session", help="Path to .jsonl session transcript")
    p_set.set_defaults(func=cmd_set)

    p_cmp = sub.add_parser("compare", help="Compare session rates to baseline")
    p_cmp.add_argument("session", help="Path to .jsonl session transcript")
    p_cmp.set_defaults(func=cmd_compare)

    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
