#!/usr/bin/env python3
"""
walkforward.py — Layer 5: Walk-Forward Validation

Validates that the multi-signal scoring system (Layer 4) predicts future returns,
and finds optimal pillar weights through walk-forward grid search.

Uses signals.py scoring functions + yfinance historical data.
No ML — grid search over weight combinations.

CLI:
  python3 walkforward.py backtest [--ticker AAPL] [--period 1y]
  python3 walkforward.py optimize [--period 1y]
  python3 walkforward.py compare
  python3 walkforward.py report
"""

import json
import math
import os
import sys
from datetime import datetime
from itertools import product

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import paths
import signals

# ── Config ─────────────────────────────────────────────────────────

DEFAULT_CONFIG = {
    "window_days": 60,
    "hold_days": 20,
    "step_days": 10,
    "period": "1y",
    "tickers": ["AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "TSLA", "META"],
    "weight_grid": {
        "technical":    [0.25, 0.30, 0.35, 0.40],
        "fundamental":  [0.25, 0.30, 0.35],
        "sentiment":    [0.10, 0.15, 0.20],
        "momentum":     [0.15, 0.20, 0.25],
    },
}

VALIDATION_FILE = paths.WALKFORWARD_REPORT


# ── Indicator Computation ──────────────────────────────────────────


def compute_rsi(prices, period=14):
    """Compute RSI from price series."""
    if len(prices) < period + 1:
        return 50.0
    gains, losses = [], []
    for i in range(1, len(prices)):
        change = prices[i] - prices[i - 1]
        gains.append(max(0.0, change))
        losses.append(max(0.0, -change))
    avg_gain = sum(gains[:period]) / period
    avg_loss = sum(losses[:period]) / period
    for i in range(period, len(gains)):
        avg_gain = (avg_gain * (period - 1) + gains[i]) / period
        avg_loss = (avg_loss * (period - 1) + losses[i]) / period
    if avg_loss == 0:
        return 100.0
    rs = avg_gain / avg_loss
    return 100.0 - (100.0 / (1.0 + rs))


def compute_ema(prices, period):
    """Compute EMA value for the last point."""
    if len(prices) < period:
        return sum(prices) / len(prices) if prices else 0.0
    multiplier = 2.0 / (period + 1)
    ema = sum(prices[:period]) / period
    for price in prices[period:]:
        ema = (price - ema) * multiplier + ema
    return ema


def compute_sma(prices, period):
    """Compute SMA value for the last point."""
    if len(prices) < period:
        return sum(prices) / len(prices) if prices else 0.0
    return sum(prices[-period:]) / period


def compute_bollinger(prices, period=20):
    """Compute Bollinger Bands (upper, middle, lower)."""
    sma = compute_sma(prices, period)
    subset = prices[-period:] if len(prices) >= period else prices
    if len(subset) < 2:
        return {"upper": sma * 1.02, "middle": sma, "lower": sma * 0.98}
    std = math.sqrt(sum((p - sma) ** 2 for p in subset) / len(subset))
    return {"upper": sma + 2 * std, "middle": sma, "lower": sma - 2 * std}


def compute_macd(prices):
    """MACD line = EMA12 − EMA26."""
    return compute_ema(prices, 12) - compute_ema(prices, 26)


def compute_indicators(prices):
    """Compute all technical indicators from a price series."""
    return {
        "rsi_14":     compute_rsi(prices),
        "ema_12":     compute_ema(prices, 12),
        "ema_26":     compute_ema(prices, 26),
        "sma_20":     compute_sma(prices, 20),
        "sma_50":     compute_sma(prices, 50),
        "bollinger":  compute_bollinger(prices),
        "macd":       compute_macd(prices),
    }


# ── Historical Data ────────────────────────────────────────────────


def fetch_historical(ticker, period="1y"):
    """Fetch historical OHLCV via yfinance."""
    import yfinance as yf
    df = yf.Ticker(ticker).history(period=period)
    if df.empty:
        return None
    return {
        "dates":  [d.strftime("%Y-%m-%d") for d in df.index],
        "close":  df["Close"].tolist(),
        "high":   df["High"].tolist(),
        "low":    df["Low"].tolist(),
        "volume": df["Volume"].tolist(),
    }


def load_market_data():
    """Load current market data for fundamentals + VIX."""
    path = paths.MARKET_DATA_FILE
    if not os.path.exists(path):
        return {}, 18.0
    with open(path) as f:
        data = json.load(f)
    return {a["symbol"]: a for a in data.get("assets", [])}, data.get("VIX", 18.0)


# ── Walk-Forward Engine ────────────────────────────────────────────


def build_windows(n_prices, window_days, hold_days, step_days):
    """Return list of (train_start, train_end, test_end) index tuples."""
    windows = []
    i = 0
    while i + window_days + hold_days <= n_prices:
        windows.append((i, i + window_days, i + window_days + hold_days))
        i += step_days
    return windows


def score_window(prices, volumes, market_asset, vix):
    """Score one ticker for one window using signals.py scoring."""
    if len(prices) < 20:
        return None
    indicators = compute_indicators(prices)
    asset = {
        "symbol":         market_asset.get("symbol", "UNK"),
        "current_price":  prices[-1],
        "previous_close": prices[-2] if len(prices) > 1 else prices[-1],
        "change_pct":     ((prices[-1] - prices[-2]) / prices[-2] * 100)
                          if len(prices) > 1 else 0.0,
        "volume":    volumes[-1] if volumes else 0,
        "prices":    prices,
        "indicators": indicators,
        "_market_vix": vix,
    }
    if "fundamentals" in market_asset:
        asset["fundamentals"] = market_asset["fundamentals"]
    return signals.score_ticker(asset, signals.DEFAULT_CONFIG)


def forward_return(close_prices, train_end, test_end):
    """Return % from end-of-train price to end-of-test price."""
    entry = close_prices[train_end - 1]
    exit_idx = min(test_end - 1, len(close_prices) - 1)
    exit_p = close_prices[exit_idx]
    return ((exit_p - entry) / entry * 100.0) if entry else 0.0


def run_walkforward(ticker, hist, market_asset, vix, config):
    """Run walk-forward for one ticker. Returns list of result dicts."""
    wd = config.get("window_days", 60)
    hd = config.get("hold_days", 20)
    sd = config.get("step_days", 10)
    windows = build_windows(len(hist["close"]), wd, hd, sd)
    results = []
    for train_s, train_e, test_e in windows:
        prices = hist["close"][train_s:train_e]
        volumes = hist["volume"][train_s:train_e]
        scored = score_window(prices, volumes, market_asset, vix)
        if scored is None:
            continue
        ret = forward_return(hist["close"], train_e, test_e)
        results.append({
            "ticker":          ticker,
            "composite_score": scored["composite_score"],
            "action_signal":   scored["action_signal"],
            "pillars":         {k: v["score"] for k, v in scored["pillars"].items()},
            "forward_return":  round(ret, 2),
            "date":            hist["dates"][train_e - 1]
                               if train_e - 1 < len(hist["dates"]) else "",
        })
    return results


# ── Analysis ───────────────────────────────────────────────────────


def score_return_correlation(results):
    """Pearson correlation between composite_score and forward_return."""
    if len(results) < 3:
        return 0.0
    xs = [r["composite_score"] for r in results]
    ys = [r["forward_return"] for r in results]
    n = len(xs)
    mx, my = sum(xs) / n, sum(ys) / n
    cov = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    vx = sum((x - mx) ** 2 for x in xs)
    vy = sum((y - my) ** 2 for y in ys)
    if vx == 0 or vy == 0:
        return 0.0
    return cov / math.sqrt(vx * vy)


def action_win_rates(results):
    """Win rate and avg return grouped by action_signal."""
    buckets = {}
    for r in results:
        a = r["action_signal"]
        buckets.setdefault(a, {"wins": 0, "total": 0, "returns": []})
        buckets[a]["total"] += 1
        buckets[a]["returns"].append(r["forward_return"])
        win = False
        if a in ("strong_buy", "buy"):
            win = r["forward_return"] > 0
        elif a in ("strong_sell", "sell"):
            win = r["forward_return"] < 0
        elif a == "hold":
            win = abs(r["forward_return"]) < 2
        if win:
            buckets[a]["wins"] += 1
    return {
        a: {
            "win_rate":   round(b["wins"] / b["total"] * 100, 1),
            "avg_return": round(sum(b["returns"]) / len(b["returns"]), 2),
            "count":      b["total"],
        }
        for a, b in buckets.items()
    }


def sharpe_ratio(results, windows_per_year=12):
    """Annualised Sharpe ratio of forward returns."""
    if len(results) < 2:
        return 0.0
    rets = [r["forward_return"] for r in results]
    n = len(rets)
    mean_r = sum(rets) / n
    var_r = sum((r - mean_r) ** 2 for r in rets) / (n - 1)
    if var_r == 0:
        return 0.0
    return (mean_r / math.sqrt(var_r)) * math.sqrt(windows_per_year)


def max_drawdown(results):
    """Max drawdown from a cumulative return series."""
    if not results:
        return 0.0
    cumul, peak, mdd = 100.0, 100.0, 0.0
    for r in results:
        cumul *= 1 + r["forward_return"] / 100
        peak = max(peak, cumul)
        mdd = max(mdd, (peak - cumul) / peak * 100)
    return round(mdd, 2)


# ── Weight Optimization ────────────────────────────────────────────


def generate_weight_combos(grid):
    """Generate valid weight dicts where sum ≈ 1.0 (±0.05)."""
    keys = list(grid.keys())
    combos = []
    for vals in product(*(grid[k] for k in keys)):
        if abs(sum(vals) - 1.0) < 0.05:
            combos.append(dict(zip(keys, vals)))
    return combos


def optimize_weights(results, grid):
    """Find pillar weights that maximise score-return correlation.

    Returns (best_weights_dict | None, best_correlation).
    """
    combos = generate_weight_combos(grid)
    if not combos or len(results) < 3:
        return None, 0.0
    pillars = ("technical", "fundamental", "sentiment", "momentum")
    pillar_scores = [{p: r["pillars"].get(p, 50) for p in pillars} for r in results]
    fwd_rets = [r["forward_return"] for r in results]
    n = len(fwd_rets)

    best_corr, best_w = -2.0, None
    for w in combos:
        composites = [sum(w.get(p, 0) * ps.get(p, 50) for p in pillars)
                      for ps in pillar_scores]
        mc = sum(composites) / n
        mr = sum(fwd_rets) / n
        cov = sum((c - mc) * (r - mr) for c, r in zip(composites, fwd_rets))
        vc = sum((c - mc) ** 2 for c in composites)
        vr = sum((r - mr) ** 2 for r in fwd_rets)
        if vc == 0 or vr == 0:
            continue
        corr = cov / math.sqrt(vc * vr)
        if corr > best_corr:
            best_corr, best_w = corr, w
    return best_w, best_corr


# ── Report ─────────────────────────────────────────────────────────


def generate_report(results, current_weights, best_weights, best_corr):
    """Build structured validation report dict."""
    report = {
        "timestamp":            datetime.now().isoformat(),
        "total_observations":   len(results),
        "correlation":          round(score_return_correlation(results), 3),
        "sharpe":               round(sharpe_ratio(results), 2),
        "max_drawdown":         max_drawdown(results),
        "win_rates":            action_win_rates(results),
        "current_weights":      current_weights,
        "optimized_weights":    best_weights,
        "optimized_correlation": round(best_corr, 3),
        "weight_changes":       {},
    }
    if best_weights and current_weights:
        for k in current_weights:
            if k in best_weights:
                report["weight_changes"][k] = round(
                    best_weights[k] - current_weights[k], 3)
    return report


def save_report(report, path=None):
    """Atomic write of report JSON."""
    import tempfile
    path = path or VALIDATION_FILE
    tmp = tempfile.NamedTemporaryFile(
        "w", suffix=".json", delete=False,
        dir=os.path.dirname(path) or ".")
    json.dump(report, tmp, indent=2)
    tmp.close()
    os.replace(tmp.name, path)


def load_report(path=None):
    """Load last report, or None."""
    path = path or VALIDATION_FILE
    if not os.path.exists(path):
        return None
    with open(path) as f:
        return json.load(f)


# ── CLI ────────────────────────────────────────────────────────────


def cmd_backtest(args, config):
    """Run walk-forward backtest and print results."""
    tickers = args.ticker.split(",") if args.ticker else config.get("tickers", [])
    period = args.period or config.get("period", "1y")
    market_assets, vix = load_market_data()
    all_results = []

    for ticker in tickers:
        print(f"Fetching {ticker} ...")
        hist = fetch_historical(ticker, period)
        if hist is None:
            print(f"  No data for {ticker}, skipping")
            continue
        asset = market_assets.get(ticker, {"symbol": ticker})
        results = run_walkforward(ticker, hist, asset, vix, config)
        all_results.extend(results)
        print(f"  {len(results)} windows")

    if not all_results:
        print("No results.")
        return

    corr = score_return_correlation(all_results)
    sr = sharpe_ratio(all_results)
    mdd = max_drawdown(all_results)
    wr = action_win_rates(all_results)

    print(f"\n{'=' * 60}")
    print("WALK-FORWARD BACKTEST RESULTS")
    print(f"{'=' * 60}")
    print(f"Observations : {len(all_results)}")
    print(f"Correlation  : {corr:.3f}")
    print(f"Sharpe       : {sr:.2f}")
    print(f"Max Drawdown : {mdd:.1f}%")

    print("\nWin Rates by Action:")
    for action in ("strong_buy", "buy", "hold", "sell", "strong_sell"):
        if action in wr:
            w = wr[action]
            print(f"  {action:12s}  win={w['win_rate']:5.1f}%  "
                  f"avg={w['avg_return']:+6.2f}%  n={w['count']}")

    quality = "GOOD" if corr > 0.3 else "WEAK" if corr > 0.1 else "POOR"
    print(f"\nScore-Return Quality: {quality}")
    if corr < 0.1:
        print("  WARNING: Scores do not reliably predict returns.")

    report = generate_report(
        all_results, signals.DEFAULT_CONFIG["weights"], None, 0.0)
    save_report(report)
    print(f"\nSaved to {VALIDATION_FILE}")


def cmd_optimize(args, config):
    """Find optimal pillar weights via walk-forward grid search."""
    tickers = config.get("tickers", [])
    period = args.period or config.get("period", "1y")
    market_assets, vix = load_market_data()
    all_results = []

    for ticker in tickers:
        print(f"Fetching {ticker} ...")
        hist = fetch_historical(ticker, period)
        if hist is None:
            continue
        asset = market_assets.get(ticker, {"symbol": ticker})
        all_results.extend(run_walkforward(ticker, hist, asset, vix, config))

    if len(all_results) < 5:
        print("Insufficient data for optimization.")
        return

    grid = config.get("weight_grid", DEFAULT_CONFIG["weight_grid"])
    combos = generate_weight_combos(grid)
    print(f"Optimising over {len(all_results)} observations, "
          f"{len(combos)} combos ...")

    current_w = signals.DEFAULT_CONFIG["weights"]
    best_w, best_corr = optimize_weights(all_results, grid)
    current_corr = score_return_correlation(all_results)

    print(f"\n{'=' * 60}")
    print("WEIGHT OPTIMIZATION RESULTS")
    print(f"{'=' * 60}")
    print(f"Current correlation : {current_corr:.3f}")
    print(f"Optimized correlation: {best_corr:.3f}")
    print(f"\n{'Pillar':12s} {'Current':>8s} {'Optimal':>8s} {'Delta':>8s}")
    print("-" * 38)
    if best_w:
        for p in ("technical", "fundamental", "sentiment", "momentum"):
            c = current_w.get(p, 0)
            o = best_w.get(p, 0)
            print(f"{p:12s} {c:8.2f} {o:8.2f} {o - c:+8.3f}")

    report = generate_report(all_results, current_w, best_w, best_corr)
    save_report(report)
    if best_w:
        print(f"\nApply via signals_config.json:")
        print(json.dumps({"weights": best_w}, indent=2))


def cmd_compare(args, config):
    """Compare current vs optimized weights."""
    report = load_report()
    if not report or not report.get("optimized_weights"):
        print("Run 'optimize' first.")
        return
    cur = report["current_weights"]
    opt = report["optimized_weights"]
    print(f"\n{'Pillar':12s} {'Current':>8s} {'Optimized':>10s} {'Delta':>8s}")
    print("-" * 40)
    for p in ("technical", "fundamental", "sentiment", "momentum"):
        c, o = cur.get(p, 0), opt.get(p, 0)
        print(f"{p:12s} {c:8.2f} {o:10.2f} {o - c:+8.3f}")
    print(f"\nCorrelation: {report.get('correlation', 0):.3f} -> "
          f"{report.get('optimized_correlation', 0):.3f}")


def cmd_report(args, config):
    """Print latest validation report."""
    report = load_report()
    if not report:
        print("No report. Run 'backtest' or 'optimize' first.")
        return
    print(f"{'=' * 60}")
    print("WALK-FORWARD VALIDATION REPORT")
    print(f"{'=' * 60}")
    print(f"Generated    : {report.get('timestamp', '?')}")
    print(f"Observations : {report.get('total_observations', 0)}")
    print(f"Correlation  : {report.get('correlation', 0):.3f}")
    print(f"Sharpe       : {report.get('sharpe', 0):.2f}")
    print(f"Max Drawdown : {report.get('max_drawdown', 0):.1f}%")
    wr = report.get("win_rates", {})
    if wr:
        print("\nWin Rates:")
        for a in ("strong_buy", "buy", "hold", "sell", "strong_sell"):
            if a in wr:
                w = wr[a]
                print(f"  {a:12s}  win={w['win_rate']:5.1f}%  "
                      f"avg={w['avg_return']:+6.2f}%  n={w['count']}")
    ch = report.get("weight_changes", {})
    if ch:
        print("\nRecommended Changes:")
        for k, v in ch.items():
            print(f"  {k}: {v:+.3f}")


def load_config(path=None):
    """Load config with fallback to defaults."""
    config = DEFAULT_CONFIG.copy()
    if path and os.path.exists(path):
        try:
            with open(path) as f:
                config.update(json.load(f))
        except (json.JSONDecodeError, IOError):
            pass
    return config


def main():
    import argparse
    p = argparse.ArgumentParser(description="Walk-Forward Validation (Layer 5)")
    sub = p.add_subparsers(dest="command")

    bt = sub.add_parser("backtest")
    bt.add_argument("--ticker", help="Comma-separated tickers")
    bt.add_argument("--period", default="1y")

    opt = sub.add_parser("optimize")
    opt.add_argument("--period", default="1y")

    sub.add_parser("compare")
    sub.add_parser("report")

    args = p.parse_args()
    cfg = load_config(paths.WALKFORWARD_CONFIG)
    {"backtest": cmd_backtest, "optimize": cmd_optimize,
     "compare": cmd_compare, "report": cmd_report}.get(
        args.command, lambda *_: p.print_help())(args, cfg)


if __name__ == "__main__":
    main()
