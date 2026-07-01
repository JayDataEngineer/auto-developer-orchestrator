#!/usr/bin/env python3
"""walk_progress.py — Per-date progress visibility for the historical walk.

The historical walk in backtest_scan.md processes N dates sequentially.
Without progress visibility, the user sees nothing until the whole walk
completes (which can be 10+ minutes). This script provides a queryable
record of progress that the agent updates after each date.

The world is not ephemeral — progress state should survive crashes.

Schema (data/walkthrough_progress.json):
    {
      "walk_id": "2026-04-01_to_2026-04-15_step7",
      "started_at": "2026-06-20T14:00:00Z",
      "updated_at": "2026-06-20T14:03:12Z",
      "total_dates": 3,
      "completed": [
        {
          "date": "2026-04-01",
          "status": "ok",
          "signals_recorded": 7,
          "news_articles": 12,
          "filings_read": 3,
          "duration_ms": 45000,
          "notes": ""
        }
      ],
      "current": {"date": "2026-04-08", "started_at": "..."},
      "failed": [],
      "estimated_remaining_ms": 90000
    }

CLI:
    python3 walk_progress.py init --dates 2026-04-01,2026-04-08,2026-04-15
    python3 walk_progress.py start-date --date 2026-04-08
    python3 walk_progress.py complete-date --date 2026-04-08 --signals 7 --news 12 --filings 3
    python3 walk_progress.py fail-date --date 2026-04-08 --reason "yfinance timeout"
    python3 walk_progress.py show
    python3 walk_progress.py summary
"""
import argparse
import json
import os
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import paths


PROGRESS_FILE = Path(os.environ.get(
    "WALK_PROGRESS_FILE",
    os.path.join(paths.DATA_DIR, "walkthrough_progress.json")
))


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def load() -> dict:
    if not PROGRESS_FILE.exists():
        return {}
    try:
        return json.loads(PROGRESS_FILE.read_text())
    except Exception:
        return {}


def save(state: dict) -> None:
    PROGRESS_FILE.parent.mkdir(parents=True, exist_ok=True)
    state["updated_at"] = now_iso()
    # Re-estimate remaining time from completed durations
    completed = state.get("completed", [])
    if completed:
        avg_ms = sum(c.get("duration_ms", 0) for c in completed) / len(completed)
        remaining = state.get("total_dates", 0) - len(completed)
        state["estimated_remaining_ms"] = int(avg_ms * remaining)
    tmp = PROGRESS_FILE.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(state, indent=2))
    tmp.replace(PROGRESS_FILE)


def cmd_init(args):
    dates = [d.strip() for d in args.dates.split(",") if d.strip()]
    state = {
        "walk_id": f"{dates[0]}_to_{dates[-1]}_step{args.step}" if len(dates) > 1 else dates[0],
        "started_at": now_iso(),
        "updated_at": now_iso(),
        "total_dates": len(dates),
        "planned_dates": dates,
        "completed": [],
        "failed": [],
        "current": None,
        "estimated_remaining_ms": None,
    }
    save(state)
    print(f"Walk initialized: {len(dates)} dates")
    print(f"  ID: {state['walk_id']}")
    print(f"  File: {PROGRESS_FILE}")


def cmd_start_date(args):
    state = load()
    if not state:
        print("ERROR: no walk initialized. Run 'init' first.", file=sys.stderr)
        sys.exit(2)
    state["current"] = {"date": args.date, "started_at": now_iso(), "started_epoch": time.time()}
    save(state)
    print(f"Started: {args.date}")


def _finish_date(args, status: str, extra: dict):
    state = load()
    if not state:
        print("ERROR: no walk initialized.", file=sys.stderr)
        sys.exit(2)
    cur = state.get("current") or {}
    started_epoch = cur.get("started_epoch") or time.time()
    duration_ms = int((time.time() - started_epoch) * 1000)

    entry = {
        "date": args.date,
        "status": status,
        "duration_ms": duration_ms,
        **extra,
    }
    # Remove from failed if it was there
    state["failed"] = [f for f in state.get("failed", []) if f.get("date") != args.date]
    # Replace if already in completed
    state["completed"] = [c for c in state.get("completed", []) if c.get("date") != args.date]
    state["completed"].append(entry)
    state["current"] = None
    save(state)
    print(f"{status.upper()}: {args.date} ({duration_ms}ms)")


def cmd_complete_date(args):
    _finish_date(args, "ok", {
        "signals_recorded": args.signals or 0,
        "news_articles": args.news or 0,
        "filings_read": args.filings or 0,
        "notes": args.notes or "",
    })


def cmd_fail_date(args):
    _finish_date(args, "failed", {"reason": args.reason or "unknown"})


def cmd_show(args):
    state = load()
    if not state:
        print("No walk in progress.")
        return
    print(json.dumps(state, indent=2))


def cmd_summary(args):
    state = load()
    if not state:
        print("No walk in progress.")
        return
    total = state.get("total_dates", 0)
    completed = state.get("completed", [])
    failed = state.get("failed", [])
    ok = [c for c in completed if c.get("status") == "ok"]
    avg_ms = sum(c.get("duration_ms", 0) for c in ok) / len(ok) if ok else 0
    remaining = total - len(completed)
    current = state.get("current") or {}
    print(f"Walk: {state.get('walk_id', '?')}")
    print(f"  Total dates: {total}")
    print(f"  OK: {len(ok)}  Failed: {len(failed)}  Remaining: {remaining}")
    if current:
        print(f"  Current: {current.get('date')}")
    if avg_ms:
        print(f"  Avg per date: {avg_ms/1000:.1f}s")
        print(f"  ETA: {avg_ms * remaining / 1000:.0f}s remaining")
    if args.json:
        print(json.dumps({
            "total": total,
            "completed": len(ok),
            "failed": len(failed),
            "remaining": remaining,
            "avg_ms": avg_ms,
            "eta_ms": avg_ms * remaining,
        }, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_init = sub.add_parser("init", help="Start a new walk")
    p_init.add_argument("--dates", required=True, help="Comma-separated YYYY-MM-DD")
    p_init.add_argument("--step", type=int, default=7, help="Step in days (for walk_id label)")
    p_init.set_defaults(func=cmd_init)

    p_start = sub.add_parser("start-date", help="Mark a date as started")
    p_start.add_argument("--date", required=True)
    p_start.set_defaults(func=cmd_start_date)

    p_done = sub.add_parser("complete-date", help="Mark a date as completed OK")
    p_done.add_argument("--date", required=True)
    p_done.add_argument("--signals", type=int, help="Number of signals recorded")
    p_done.add_argument("--news", type=int, help="Number of news articles read")
    p_done.add_argument("--filings", type=int, help="Number of SEC filings read")
    p_done.add_argument("--notes", default="")
    p_done.set_defaults(func=cmd_complete_date)

    p_fail = sub.add_parser("fail-date", help="Mark a date as failed")
    p_fail.add_argument("--date", required=True)
    p_fail.add_argument("--reason", required=True)
    p_fail.set_defaults(func=cmd_fail_date)

    p_show = sub.add_parser("show", help="Show full progress JSON")
    p_show.set_defaults(func=cmd_show)

    p_sum = sub.add_parser("summary", help="Show one-line progress summary")
    p_sum.add_argument("--json", action="store_true")
    p_sum.set_defaults(func=cmd_summary)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
