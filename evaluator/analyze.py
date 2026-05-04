#!/usr/bin/env python3
"""
Langfuse Analysis CLI — query and analyze observability data.

Usage:
  python3 analyze.py scores [--name NAME] [--from DATE] [--group-by GROUP]
  python3 analyze.py tools [--top N] [--sort-by FIELD] [--tag TAG]
  python3 analyze.py models [--compare] [--last Ns]
  python3 analyze.py sessions [--last Ns] [--min-score N]
  python3 analyze.py export [--format FORMAT] [--output FILE] [--tag TAG]
"""

import argparse
import json
import sys
from datetime import datetime, timedelta, timezone
from metrics_client import LangfuseMetricsClient


def parse_relative_time(s):
    """Parse relative time like '7d', '30d', '24h', '1h'."""
    if not s:
        return None
    s = s.lower().strip()
    if s.endswith("d"):
        return datetime.now(timezone.utc) - timedelta(days=int(s[:-1]))
    elif s.endswith("h"):
        return datetime.now(timezone.utc) - timedelta(hours=int(s[:-1]))
    elif s.endswith("w"):
        return datetime.now(timezone.utc) - timedelta(weeks=int(s[:-1]))
    return datetime.fromisoformat(s.replace("Z", "+00:00"))


def cmd_scores(args):
    client = LangfuseMetricsClient()
    from_date = parse_relative_time(args.from_date) if args.from_date else None
    to_date = parse_relative_time(args.to_date) if args.to_date else None

    agg = client.aggregate_scores(
        score_name=args.name,
        group_by=args.group_by,
        from_date=from_date,
        to_date=to_date,
    )

    if not agg:
        print("No scores found.")
        return

    print(f"Score aggregation: {args.name or 'all scores'} (grouped by {args.group_by})")
    print(f"{'Period':<20} {'Count':>6} {'Avg':>8} {'Min':>8} {'Max':>8}")
    print("-" * 54)
    for period, stats in agg.items():
        print(f"{period:<20} {stats['count']:>6} {stats['avg']:>8.2f} {stats['min']:>8.1f} {stats['max']:>8.1f}")

    # Overall summary
    all_values = [stats["avg"] * stats["count"] for stats in agg.values()]
    all_counts = [stats["count"] for stats in agg.values()]
    if all_counts:
        total = sum(all_counts)
        weighted_avg = sum(all_values) / total if total else 0
        print(f"\nOverall: {total} scores, weighted avg = {weighted_avg:.2f}")


def cmd_tools(args):
    client = LangfuseMetricsClient()
    from_date = parse_relative_time(args.from_date) if args.from_date else None
    to_date = parse_relative_time(args.to_date) if args.to_date else None

    stats = client.tool_usage_stats(from_date=from_date, to_date=to_date)
    if not stats:
        print("No tool usage found.")
        return

    sort_key = "avg_latency_ms" if args.sort_by == "latency" else "count"
    sorted_tools = sorted(stats.items(), key=lambda x: x[1].get(sort_key, 0), reverse=True)

    top = args.top or len(sorted_tools)
    print(f"Tool usage stats (top {top}, sorted by {sort_key}):")
    print(f"{'Tool':<30} {'Count':>6} {'Avg(ms)':>10} {'Total(ms)':>12}")
    print("-" * 60)
    for name, s in sorted_tools[:top]:
        print(f"{name:<30} {s['count']:>6} {s['avg_latency_ms']:>10.1f} {s['total_latency_ms']:>12.1f}")


def cmd_models(args):
    client = LangfuseMetricsClient()
    from_date = parse_relative_time(args.from_date) if args.from_date else None
    to_date = parse_relative_time(args.to_date) if args.to_date else None

    stats = client.model_comparison(from_date=from_date, to_date=to_date)
    if not stats:
        print("No model data found.")
        return

    print("Model comparison:")
    print(f"{'Model':<25} {'Calls':>6} {'In Tokens':>12} {'Out Tokens':>12} {'Avg(ms)':>10}")
    print("-" * 68)
    for model, s in stats.items():
        print(f"{model:<25} {s['count']:>6} {s['total_input_tokens']:>12} {s['total_output_tokens']:>12} {s['avg_latency_ms']:>10.1f}")


def cmd_sessions(args):
    client = LangfuseMetricsClient()
    from_date = parse_relative_time(args.from_date) if args.from_date else None
    to_date = parse_relative_time(args.to_date) if args.to_date else None

    sessions = client.sessions_summary(
        from_date=from_date,
        to_date=to_date,
        min_avg_score=args.min_score,
    )
    if not sessions:
        print("No sessions found.")
        return

    print(f"Sessions ({len(sessions)} total):")
    print(f"{'Session ID':<40} {'Traces':>7} {'Avg Score':>10} {'Tags':<20}")
    print("-" * 80)
    for sid, s in sorted(sessions.items(), key=lambda x: x[1].get("avg_score", 0) or 0, reverse=True):
        score_str = f"{s['avg_score']:.2f}" if s['avg_score'] is not None else "N/A"
        tags_str = ", ".join(s['tags'][:3])
        print(f"{sid[:40]:<40} {s['trace_count']:>7} {score_str:>10} {tags_str:<20}")


def cmd_export(args):
    client = LangfuseMetricsClient()
    from_date = parse_relative_time(args.from_date) if args.from_date else None
    to_date = parse_relative_time(args.to_date) if args.to_date else None

    tags = [args.tag] if args.tag else None
    fmt = args.format or "json"
    output = args.output or f"traces_export.{fmt}"

    traces = client.export_traces(
        output_format=fmt,
        output_file=output,
        tags=tags,
        from_date=from_date,
        to_date=to_date,
    )
    print(f"Exported {len(traces)} traces to {output} (format: {fmt})")


def main():
    parser = argparse.ArgumentParser(description="Langfuse Analysis CLI")
    sub = parser.add_subparsers(dest="command")

    # scores
    p_scores = sub.add_parser("scores", help="Aggregate score analytics")
    p_scores.add_argument("--name", help="Score name to filter (e.g., response_quality)")
    p_scores.add_argument("--from", dest="from_date", help="From date (ISO or relative like 7d)")
    p_scores.add_argument("--to", dest="to_date", help="To date (ISO or relative)")
    p_scores.add_argument("--group-by", default="day", choices=["hour", "day", "week", "month"])

    # tools
    p_tools = sub.add_parser("tools", help="Tool usage stats")
    p_tools.add_argument("--top", type=int, default=15, help="Top N tools")
    p_tools.add_argument("--sort-by", default="count", choices=["count", "latency"])
    p_tools.add_argument("--from", dest="from_date")
    p_tools.add_argument("--to", dest="to_date")

    # models
    p_models = sub.add_parser("models", help="Model comparison")
    p_models.add_argument("--from", dest="from_date")
    p_models.add_argument("--to", dest="to_date")

    # sessions
    p_sessions = sub.add_parser("sessions", help="Session analytics")
    p_sessions.add_argument("--from", dest="from_date")
    p_sessions.add_argument("--to", dest="to_date")
    p_sessions.add_argument("--min-score", type=float, help="Filter sessions below this avg score")

    # export
    p_export = sub.add_parser("export", help="Export trace data")
    p_export.add_argument("--format", choices=["json", "csv"], default="json")
    p_export.add_argument("--output", help="Output file path")
    p_export.add_argument("--tag", help="Filter by tag")
    p_export.add_argument("--from", dest="from_date")
    p_export.add_argument("--to", dest="to_date")

    args = parser.parse_args()
    commands = {
        "scores": cmd_scores,
        "tools": cmd_tools,
        "models": cmd_models,
        "sessions": cmd_sessions,
        "export": cmd_export,
    }
    if args.command in commands:
        commands[args.command](args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
