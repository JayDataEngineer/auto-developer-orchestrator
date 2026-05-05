#!/usr/bin/env python3
"""
Weekly Correlation Analytics — compute relationships between invest-bot metrics.

Pulls all scores from Langfuse, computes correlations between:
  - Signal confidence vs actual returns
  - Regime composite vs Sharpe ratio
  - Data groundedness vs trade profitability
  - Risk awareness vs max drawdown

Posts summary correlation scores back to a special "analytics" trace.

Usage:
  python3 correlations.py                    # Run weekly analytics
  python3 correlations.py --days 30          # Look back 30 days
  python3 correlations.py --dry-run          # Preview without posting
"""

import argparse
import json
import sys
from datetime import datetime, timedelta, timezone
from statistics import mean, stdev
from metrics_client import LangfuseMetricsClient


def get_score_timeseries(lf: LangfuseMetricsClient, score_name, from_date=None, limit=200):
    """Get a list of (timestamp, value) for a specific score."""
    scores = lf.get_scores(name=score_name, limit=limit)
    if not scores:
        return []

    series = []
    for s in scores:
        if not isinstance(s, dict):
            continue
        val = s.get("value")
        ts = s.get("timestamp", "")
        if val is None or not isinstance(val, (int, float)):
            continue
        if from_date and ts:
            ts_dt = datetime.fromisoformat(ts.replace("Z", "+00:00"))
            if ts_dt < from_date:
                continue
        series.append((ts, float(val)))
    return series


def pearson_correlation(xs, ys):
    """Compute Pearson correlation coefficient between two lists."""
    if len(xs) < 5:
        return None
    n = len(xs)
    if n < 2:
        return None
    mean_x = mean(xs)
    mean_y = mean(ys)
    num = sum((x - mean_x) * (y - mean_y) for x, y in zip(xs, ys))
    try:
        denom_x = sum((x - mean_x) ** 2 for x in xs) ** 0.5
        denom_y = sum((y - mean_y) ** 2 for y in ys) ** 0.5
    except (ValueError, ZeroDivisionError):
        return None
    if denom_x == 0 or denom_y == 0:
        return None
    return num / (denom_x * denom_y)


def compute_metric_stats(values):
    """Compute summary statistics for a list of values."""
    if not values:
        return {"count": 0}
    return {
        "count": len(values),
        "mean": round(mean(values), 4),
        "stdev": round(stdev(values), 4) if len(values) > 1 else 0,
        "min": round(min(values), 4),
        "max": round(max(values), 4),
        "latest": round(values[-1], 4),
    }


def analyze_correlations(lf: LangfuseMetricsClient, days=7):
    """Compute correlations between invest-bot metrics."""
    from_date = datetime.now(timezone.utc) - timedelta(days=days)

    print(f"Analyzing correlations over {days} days...")

    # Fetch key metric series
    metrics_to_fetch = [
        "equity", "sharpe_ratio", "daily_pnl", "return_pct",
        "regime_composite", "portfolio_heat", "prediction_accuracy",
        "win_rate", "data_groundedness", "risk_awareness",
        "reasoning_quality", "signal_confidence",
    ]

    series_data = {}
    for name in metrics_to_fetch:
        data = get_score_timeseries(lf, name, from_date=from_date)
        if data:
            series_data[name] = data
            print(f"  {name}: {len(data)} data points")

    if not series_data:
        print("  No data found for any metric.")
        return {}

    results = {}

    # 1. Summary stats for each metric
    for name, data in series_data.items():
        values = [v for _, v in data]
        results[f"stats_{name}"] = compute_metric_stats(values)

    # 2. Correlation pairs to compute
    correlation_pairs = [
        ("regime_composite", "sharpe_ratio", "regime_vs_sharpe"),
        ("regime_composite", "daily_pnl", "regime_vs_pnl"),
        ("portfolio_heat", "daily_pnl", "heat_vs_pnl"),
        ("prediction_accuracy", "return_pct", "accuracy_vs_return"),
        ("win_rate", "return_pct", "winrate_vs_return"),
    ]

    for x_name, y_name, corr_name in correlation_pairs:
        if x_name in series_data and y_name in series_data:
            # Align by index (approximate — scores may not share exact timestamps)
            x_vals = [v for _, v in series_data[x_name]]
            y_vals = [v for _, v in series_data[y_name]]
            min_len = min(len(x_vals), len(y_vals))
            if min_len >= 5:
                corr = pearson_correlation(x_vals[:min_len], y_vals[:min_len])
                if corr is not None:
                    results[corr_name] = round(corr, 4)
                    direction = "positive" if corr > 0 else "negative"
                    strength = "strong" if abs(corr) > 0.7 else "moderate" if abs(corr) > 0.4 else "weak"
                    print(f"  {corr_name}: {corr:+.4f} ({strength} {direction})")

    # 3. Signal quality vs outcome (if we have judge scores)
    if "data_groundedness" in series_data and "daily_pnl" in series_data:
        x_vals = [v for _, v in series_data["data_groundedness"]]
        y_vals = [v for _, v in series_data["daily_pnl"]]
        min_len = min(len(x_vals), len(y_vals))
        if min_len >= 5:
            corr = pearson_correlation(x_vals[:min_len], y_vals[:min_len])
            if corr is not None:
                results["groundedness_vs_pnl"] = round(corr, 4)
                print(f"  groundedness_vs_pnl: {corr:+.4f}")

    # 4. Model confidence calibration
    if "signal_confidence" in series_data and "daily_pnl" in series_data:
        x_vals = [v for _, v in series_data["signal_confidence"]]
        y_vals = [v for _, v in series_data["daily_pnl"]]
        min_len = min(len(x_vals), len(y_vals))
        if min_len >= 5:
            corr = pearson_correlation(x_vals[:min_len], y_vals[:min_len])
            if corr is not None:
                results["confidence_calibration"] = round(corr, 4)
                print(f"  confidence_calibration: {corr:+.4f}")

    return results


def main():
    parser = argparse.ArgumentParser(description="Invest-Bot Correlation Analytics")
    parser.add_argument("--days", type=int, default=7, help="Lookback window (default: 7)")
    parser.add_argument("--dry-run", action="store_true", help="Preview without posting")
    args = parser.parse_args()

    lf = LangfuseMetricsClient()

    results = analyze_correlations(lf, days=args.days)
    if not results:
        print("\nNo results to post.")
        return

    print(f"\nComputed {len(results)} analytics:")

    # Filter to numeric-only results for posting
    numeric_results = {k: v for k, v in results.items() if isinstance(v, (int, float))}

    if args.dry_run:
        print("\nDry run — would post:")
        for k, v in sorted(numeric_results.items()):
            print(f"  {k}: {v}")
        return

    # Post correlation scores to a synthetic trace
    # We post them as individual scores on the most recent invest trace
    traces = lf.get_traces(tags=["investing"], limit=1)
    if not traces:
        print("\nNo invest traces found to attach analytics to.")
        return

    tid = traces[0].get("id", "")
    print(f"\nPosting {len(numeric_results)} analytics scores to trace {tid[:20]}...")

    for name, value in sorted(numeric_results.items()):
        lf.post_score(tid, f"corr:{name}", value, "NUMERIC", "Weekly correlation analytics")

    print("Done.")


if __name__ == "__main__":
    main()
