#!/usr/bin/env python3
"""
Backtesting / Time-Travel engine for the invest-bot.

Simulates what the AI agent WOULD HAVE done on a given historical date,
then compares against actual price movements afterward.

Usage:
  python3 backtest.py --date 2026-04-01                          # Single date backtest
  python3 backtest.py --start 2026-03-01 --end 2026-04-01         # Date range
  python3 backtest.py --start 2026-01-01 --end 2026-04-01 --step 7  # Weekly snapshots
  python3 backtest.py --date 2026-04-01 --evaluate                # Also evaluate past predictions
  python3 backtest.py --report                                      # Score all saved predictions

How it works:
  1. Fetches market data AS OF the target date (cutoff prices at that point)
  2. Computes technical indicators using only data available on that date
  3. Saves a "snapshot" with the data the agent would have seen
  4. Optionally evaluates previous predictions against actual outcomes
  5. Scores: precision (did buys go up?), recall (did we miss opportunities?)
"""

import json
import argparse
import os
import sys
from datetime import datetime, timedelta

# Third-party deps imported lazily inside functions so --help works even when
# the deps aren't installed (the System A contract test runs --help from a
# bare uv venv). Canonical pattern: stdlib-only top-level imports.
yf = None  # type: ignore[assignment]

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import paths

BACKTEST_DIR = paths.BACKTEST_DIR
SNAPSHOT_FILE = paths.BACKTEST_SNAPSHOT_FILE
PREDICTIONS_FILE = paths.BACKTEST_PREDICTIONS_FILE
SCORES_FILE = paths.BACKTEST_SCORES_FILE

# Default watchlist
DEFAULT_STOCKS = ["AAPL", "MSFT", "GOOGL", "NVDA", "TSLA", "AMZN", "META"]
DEFAULT_CRYPTO = ["bitcoin", "ethereum", "solana"]
EVAL_DAYS = [1, 3, 7, 14, 30]  # Evaluate predictions at these horizons


def _ensure_yfinance():
    """Lazily import yfinance; emit JSON error on missing dep per System A contract."""
    global yf
    if yf is not None:
        return yf
    try:
        import yfinance as _yf
        yf = _yf
        return yf
    except ImportError:
        print(json.dumps({"error": "yfinance not installed. Add to [sandbox].pip_packages."}))
        sys.exit(1)


def ensure_dir():
    os.makedirs(BACKTEST_DIR, exist_ok=True)


def fetch_historical(symbol, end_date, days=120):
    """Fetch price history up to (but NOT after) end_date."""
    _ensure_yfinance()
    start = end_date - timedelta(days=days)
    try:
        ticker = yf.Ticker(symbol)
        hist = ticker.history(start=start.strftime("%Y-%m-%d"),
                              end=end_date.strftime("%Y-%m-%d"))
        if hist.empty:
            return None
        return hist
    except Exception as e:
        return None


def fetch_fundamentals_asof(symbol, date_str):
    """Fetch fundamentals (these don't change much day-to-day, so current is fine)."""
    _ensure_yfinance()
    try:
        ticker = yf.Ticker(symbol)
        info = ticker.info
        result = {}
        for k in ["trailingPE", "forwardPE", "pegRatio", "priceToBook",
                   "profitMargins", "returnOnEquity", "revenueGrowth",
                   "debtToEquity", "recommendationKey", "targetMeanPrice",
                   "beta", "marketCap", "sector", "industry"]:
            v = info.get(k)
            if v is not None:
                result[k] = v
        return result
    except Exception:
        return {}


def calc_rsi(prices, period=14):
    if len(prices) < period + 1:
        return 50.0
    gains, losses = [], []
    for i in range(1, len(prices)):
        d = prices[i] - prices[i - 1]
        gains.append(max(d, 0))
        losses.append(max(-d, 0))
    avg_g = sum(gains[-period:]) / period
    avg_l = sum(losses[-period:]) / period
    if avg_l == 0:
        return 100.0
    return round(100 - 100 / (1 + avg_g / avg_l), 1)


def calc_ema(prices, period):
    if len(prices) < period:
        return prices[-1] if prices else 0
    k = 2 / (period + 1)
    ema = prices[0]
    for p in prices[1:]:
        ema = p * k + ema * (1 - k)
    return round(ema, 2)


def calc_bollinger(prices, period=20):
    if len(prices) < period:
        return {}
    recent = prices[-period:]
    mid = sum(recent) / period
    var = sum((p - mid) ** 2 for p in recent) / period
    std = var ** 0.5
    return {
        "upper": round(mid + std * 2, 2),
        "middle": round(mid, 2),
        "lower": round(mid - std * 2, 2),
    }


def build_snapshot(symbol, end_date):
    """Build a snapshot of market data as the agent would have seen it on end_date."""
    hist = fetch_historical(symbol, end_date, days=150)
    if hist is None or hist.empty:
        return {"symbol": symbol, "error": "No historical data"}

    closes = hist["Close"].tolist()
    volumes = hist["Volume"].tolist()
    highs = hist["High"].tolist()
    lows = hist["Low"].tolist()

    current = closes[-1]
    prev = closes[-2] if len(closes) >= 2 else current
    change_pct = round(((current - prev) / max(prev, 0.01)) * 100, 2)

    prices = closes[-90:]  # Last 90 days of data available on that date

    # Technical indicators (computed only from data up to end_date)
    indicators = {
        "rsi_14": calc_rsi(closes),
        "ema_12": calc_ema(closes, 12),
        "ema_26": calc_ema(closes, 26),
        "sma_20": round(sum(closes[-20:]) / 20, 2) if len(closes) >= 20 else None,
        "sma_50": round(sum(closes[-50:]) / 50, 2) if len(closes) >= 50 else None,
        "bollinger": calc_bollinger(closes),
        "macd": round(calc_ema(closes, 12) - calc_ema(closes, 26), 4),
    }

    # Signal hints (same logic as fetch_data.py)
    signals = []
    rsi = indicators["rsi_14"]
    if rsi > 70:
        signals.append("RSI overbought (>70)")
    elif rsi < 30:
        signals.append("RSI oversold (<30)")

    bb = indicators["bollinger"]
    if bb:
        if current > bb["upper"]:
            signals.append("Price above upper Bollinger Band")
        elif current < bb["lower"]:
            signals.append("Price below lower Bollinger Band")

    if indicators["ema_12"] > indicators["ema_26"]:
        signals.append("EMA12 > EMA26 (bullish)")
    else:
        signals.append("EMA12 < EMA26 (bearish)")

    return {
        "symbol": symbol,
        "current_price": round(current, 2),
        "previous_close": round(prev, 2),
        "change_pct": change_pct,
        "volume": int(volumes[-1]) if volumes else 0,
        "day_range": f"{round(lows[-1], 2)} - {round(highs[-1], 2)}",
        "high_30d": round(max(highs[-30:]), 2) if len(highs) >= 30 else None,
        "low_30d": round(min(lows[-30:]), 2) if len(lows) >= 30 else None,
        "prices_last_30": [round(p, 2) for p in prices[-30:]],
        "indicators": indicators,
        "signals": signals,
    }


def fetch_actual_prices(symbol, start_date, days=35):
    """Fetch actual prices AFTER a prediction date for evaluation."""
    try:
        ticker = yf.Ticker(symbol)
        end = start_date + timedelta(days=days + 5)  # buffer for weekends
        hist = ticker.history(start=start_date.strftime("%Y-%m-%d"),
                              end=min(end, datetime.now()).strftime("%Y-%m-%d"))
        if hist.empty:
            return {}
        closes = hist["Close"].tolist()
        dates = [d.strftime("%Y-%m-%d") for d in hist.index]
        base_price = closes[0]
        result = {}
        for d in EVAL_DAYS:
            if d < len(closes):
                price = closes[d]
                result[f"day_{d}"] = {
                    "price": round(price, 2),
                    "change_pct": round(((price - base_price) / base_price) * 100, 2),
                }
        result["max_price"] = round(max(closes[:min(30, len(closes))]), 2)
        result["min_price"] = round(min(closes[:min(30, len(closes))]), 2)
        return result
    except Exception:
        return {}


def save_prediction(predictions, pred):
    """Append a prediction to the predictions file."""
    ensure_dir()
    all_preds = []
    if os.path.exists(PREDICTIONS_FILE):
        with open(PREDICTIONS_FILE) as f:
            all_preds = json.load(f)

    # Avoid duplicates
    all_preds = [p for p in all_preds
                 if not (p["symbol"] == pred["symbol"] and p["date"] == pred["date"])]
    all_preds.append(pred)

    with open(PREDICTIONS_FILE, "w") as f:
        json.dump(all_preds, f, indent=2)

    # Auto-track progress: increment signals_recorded for this date.
    _progress_touch(pred["date"], status="ok",
                    signals=sum(1 for p in all_preds if p.get("date") == pred["date"]))
    return len(all_preds)


def evaluate_predictions(predictions):
    """Score all unevaluated predictions against actual outcomes."""
    now = datetime.now()
    scored = []
    for pred in predictions:
        if pred.get("evaluated"):
            scored.append(pred)
            continue

        pred_date = datetime.strptime(pred["date"], "%Y-%m-%d")
        if (now - pred_date).days < 1:
            pred["evaluated"] = False
            pred["reason"] = "Too recent to evaluate"
            scored.append(pred)
            continue

        actuals = fetch_actual_prices(pred["symbol"], pred_date + timedelta(days=1))
        if not actuals:
            pred["evaluated"] = False
            pred["reason"] = "Could not fetch actual prices"
            scored.append(pred)
            continue

        action = pred.get("action", "hold")
        entry_price = pred["current_price"]

        # Score the prediction
        day1 = actuals.get("day_1", {})
        day7 = actuals.get("day_7", {})
        day30 = actuals.get("day_30", {})

        pred["actual"] = actuals
        pred["evaluated"] = True
        pred["evaluated_at"] = now.isoformat()

        if action in ("buy", "strong_buy"):
            # Good if price went up
            if day7.get("change_pct", 0) > 0:
                pred["correct"] = True
                pred["return_pct"] = day7.get("change_pct", 0)
            elif day1.get("change_pct", 0) > 0:
                pred["correct"] = "partial"  # Short-term win, didn't hold
                pred["return_pct"] = day7.get("change_pct", 0)
            else:
                pred["correct"] = False
                pred["return_pct"] = day7.get("change_pct", 0)
        elif action in ("sell", "strong_sell"):
            # Good if price went down after selling
            if day7.get("change_pct", 0) < 0:
                pred["correct"] = True
                pred["return_pct"] = -day7.get("change_pct", 0)  # positive = saved money
            else:
                pred["correct"] = False
                pred["return_pct"] = -day7.get("change_pct", 0)  # negative = missed gains
        else:
            pred["correct"] = "neutral"
            pred["return_pct"] = day7.get("change_pct", 0)

        scored.append(pred)

    return scored


def generate_agent_prompt(snapshot_data):
    """Generate the same prompt the agent gets for a live morning scan.

    Identical format — no dates, no "pretend" language.
    The agent sees raw data and analyzes it the same way it would live.
    """
    assets_text = json.dumps(snapshot_data, indent=2)
    return f"""You are an investment analyst running a market scan.

STEP 1: Analyze the market data below. For each asset, consider:
  - RSI levels (overbought >70, oversold <30)
  - Moving average crossovers (EMA12 vs EMA26)
  - Bollinger Band position
  - Price momentum and volume
  - Fundamentals (PE, growth, margins, analyst targets)

STEP 2: Generate trading signals. For each asset, output:
  - action: strong_buy, buy, hold, sell, strong_sell
  - confidence: 0.0 to 1.0 (only trade if >= 0.6)
  - reasoning: 1-2 sentences

STEP 3: Save signals to data/signals.json as a JSON array:
  [{{"symbol": "AAPL", "action": "buy", "confidence": 0.75, "reasoning": "..."}}]

Market data:
{assets_text}"""


def run_backtest(date_str, stocks=None, crypto_ids=None, generate_prompt=True):
    """Run a single-date backtest — build snapshot for agent analysis."""
    target_date = datetime.strptime(date_str, "%Y-%m-%d")
    ensure_dir()

    stocks = stocks or DEFAULT_STOCKS
    crypto_ids = crypto_ids or DEFAULT_CRYPTO

    # Auto-track progress so walks can't silently skip visibility.
    # If walkthrough_progress.json exists, this date is now "in progress".
    _progress_touch(date_str, status="started")

    print(f"Building backtest snapshot for {date_str}...", file=sys.stderr)

    # Stock snapshots
    stock_snapshots = []
    for sym in stocks:
        snap = build_snapshot(sym, target_date)
        if "error" not in snap:
            stock_snapshots.append(snap)
        else:
            print(f"  {sym}: {snap.get('error')}", file=sys.stderr)

    # Save snapshot — date is in filename only, NOT in data (agent shouldn't see it)
    snapshot = {
        "timestamp": datetime.now().isoformat(),
        "assets": stock_snapshots,
    }

    if generate_prompt:
        snapshot["agent_prompt"] = generate_agent_prompt(stock_snapshots)

    path = SNAPSHOT_FILE.format(date=date_str)
    with open(path, "w") as f:
        json.dump(snapshot, f, indent=2)

    # Auto-generate per-date research plan so the walk gets qualitative context
    # even if the agent forgets to invoke historical_research.py explicitly.
    # Best-effort: never let a research-plan failure abort the snapshot.
    try:
        import subprocess
        plan_path = os.path.join(paths.DATA_DIR, f"research_plan_{date_str}.json")
        subprocess.run(
            [sys.executable, os.path.join(paths.SCRIPT_DIR, "historical_research.py"),
             "--date", date_str, "--output", plan_path],
            check=False, capture_output=True, timeout=30,
        )
        if os.path.exists(plan_path):
            snapshot["research_plan_path"] = plan_path
            print(f"Research plan saved: {plan_path}", file=sys.stderr)
    except Exception as e:
        print(f"research plan skipped: {e}", file=sys.stderr)

    print(f"\nSnapshot saved: {path}", file=sys.stderr)
    print(f"Stocks with data: {len(stock_snapshots)}/{len(stocks)}", file=sys.stderr)

    # Quick summary
    for s in stock_snapshots:
        signals_str = ", ".join(s.get("signals", []))
        print(f"  {s['symbol']}: ${s['current_price']} | RSI={s['indicators']['rsi_14']} | {signals_str}",
              file=sys.stderr)

    return snapshot


def _progress_touch(date_str, status="started", signals=None, news=None, filings=None):
    """Best-effort update of walkthrough_progress.json.

    Called automatically from run_backtest / save_prediction / evaluate_predictions
    so the walk leaves a progress trail even if the agent forgets to invoke
    walk_progress.py explicitly. Never raises — progress tracking is a
    side-effect, not a gate.
    """
    try:
        import time
        from pathlib import Path
        pf = Path(paths.WALKTHROUGH_PROGRESS_FILE)
        if not pf.parent.exists():
            pf.parent.mkdir(parents=True, exist_ok=True)
        state = {}
        if pf.exists():
            try:
                state = json.loads(pf.read_text())
            except Exception:
                state = {}
        if not state:
            return  # no walk initialized — don't auto-create
        if status == "started":
            state["current"] = {
                "date": date_str,
                "started_at": datetime.now().isoformat(),
                "started_epoch": time.time(),
            }
        elif status in ("ok", "failed"):
            cur = state.get("current") or {}
            started_epoch = cur.get("started_epoch") or time.time()
            duration_ms = int((time.time() - started_epoch) * 1000)
            entry = {"date": date_str, "status": status, "duration_ms": duration_ms}
            if signals is not None:
                entry["signals_recorded"] = signals
            if news is not None:
                entry["news_articles"] = news
            if filings is not None:
                entry["filings_read"] = filings
            state["completed"] = [c for c in state.get("completed", [])
                                  if c.get("date") != date_str]
            state["completed"].append(entry)
            state["current"] = None
        state["updated_at"] = datetime.now().isoformat()
        tmp = pf.with_suffix(".json.tmp")
        tmp.write_text(json.dumps(state, indent=2))
        tmp.replace(pf)
    except Exception:
        pass


def run_backtest_range(start_str, end_str, step=7, stocks=None):
    """Run backtests across a date range."""
    start = datetime.strptime(start_str, "%Y-%m-%d")
    end = datetime.strptime(end_str, "%Y-%m-%d")
    dates = []
    current = start
    while current <= end:
        # Skip weekends
        if current.weekday() < 5:
            dates.append(current.strftime("%Y-%m-%d"))
        current += timedelta(days=step)

    print(f"Running backtest for {len(dates)} dates ({start_str} to {end_str}, every {step} days)",
          file=sys.stderr)

    results = []
    for d in dates:
        print(f"\n--- {d} ---", file=sys.stderr)
        snap = run_backtest(d, stocks=stocks)
        results.append({"date": d, "stocks_count": len(snap.get("stocks", []))})

    return results


def score_report():
    """Generate a score report from all saved predictions."""
    if not os.path.exists(PREDICTIONS_FILE):
        print(json.dumps({"error": "No predictions file found. Run backtest with --evaluate first."}))
        return

    with open(PREDICTIONS_FILE) as f:
        preds = json.load(f)

    evaluated = [p for p in preds if p.get("evaluated")]
    pending = [p for p in preds if not p.get("evaluated")]

    # Stats
    correct = [p for p in evaluated if p.get("correct") is True]
    wrong = [p for p in evaluated if p.get("correct") is False]
    partial = [p for p in evaluated if p.get("correct") == "partial"]
    neutral = [p for p in evaluated if p.get("correct") == "neutral"]

    buy_preds = [p for p in evaluated if p.get("action") in ("buy", "strong_buy")]
    sell_preds = [p for p in evaluated if p.get("action") in ("sell", "strong_sell")]

    avg_return = 0
    if evaluated:
        returns = [p.get("return_pct", 0) for p in evaluated if isinstance(p.get("return_pct"), (int, float))]
        avg_return = round(sum(returns) / len(returns), 2) if returns else 0

    report = {
        "summary": {
            "total_predictions": len(preds),
            "evaluated": len(evaluated),
            "pending": len(pending),
            "correct": len(correct),
            "wrong": len(wrong),
            "partial": len(partial),
            "neutral": len(neutral),
            "accuracy_pct": round(len(correct) / max(len(correct) + len(wrong), 1) * 100, 1),
            "avg_return_pct": avg_return,
            "buy_signals": len(buy_preds),
            "sell_signals": len(sell_preds),
        },
        "by_symbol": {},
        "worst_predictions": sorted(
            [p for p in evaluated if isinstance(p.get("return_pct"), (int, float))],
            key=lambda x: x.get("return_pct", 0)
        )[:5],
        "best_predictions": sorted(
            [p for p in evaluated if isinstance(p.get("return_pct"), (int, float))],
            key=lambda x: x.get("return_pct", 0), reverse=True
        )[:5],
    }

    # Per-symbol stats
    symbols = set(p["symbol"] for p in evaluated)
    for sym in symbols:
        sym_preds = [p for p in evaluated if p["symbol"] == sym]
        sym_correct = len([p for p in sym_preds if p.get("correct") is True])
        sym_returns = [p.get("return_pct", 0) for p in sym_preds
                       if isinstance(p.get("return_pct"), (int, float))]
        report["by_symbol"][sym] = {
            "predictions": len(sym_preds),
            "correct": sym_correct,
            "accuracy_pct": round(sym_correct / max(len(sym_preds), 1) * 100, 1),
            "avg_return_pct": round(sum(sym_returns) / len(sym_returns), 2) if sym_returns else 0,
        }

    print(json.dumps(report, indent=2))


def main():
    parser = argparse.ArgumentParser(description="Invest-bot backtest / time-travel engine")
    parser.add_argument("--date", help="Single date to backtest (YYYY-MM-DD)")
    parser.add_argument("--start", help="Start date for range backtest")
    parser.add_argument("--end", help="End date for range backtest")
    parser.add_argument("--step", type=int, default=7, help="Days between snapshots (default: 7)")
    parser.add_argument("--evaluate", action="store_true", help="Evaluate past predictions against actuals")
    parser.add_argument("--report", action="store_true", help="Generate score report")
    parser.add_argument("--stocks", nargs="+", help="Override default stock list")
    parser.add_argument("--record-signal", help="Record a manual signal: symbol,action,confidence,date")
    args = parser.parse_args()

    if args.report:
        # First evaluate any pending predictions
        if os.path.exists(PREDICTIONS_FILE):
            with open(PREDICTIONS_FILE) as f:
                preds = json.load(f)
            scored = evaluate_predictions(preds)
            with open(PREDICTIONS_FILE, "w") as f:
                json.dump(scored, f, indent=2)
        score_report()
        return

    if args.record_signal:
        # Record a signal from agent analysis: "AAPL,buy,0.75,2026-04-01"
        parts = args.record_signal.split(",")
        if len(parts) < 3:
            print(json.dumps({"error": "Format: symbol,action,confidence[,date]"}))
            return
        symbol, action = parts[0], parts[1]
        confidence = float(parts[2])
        date = parts[3] if len(parts) > 3 else datetime.now().strftime("%Y-%m-%d")

        # Get the price at that date
        target = datetime.strptime(date, "%Y-%m-%d")
        hist = fetch_historical(symbol, target, days=5)
        price = round(hist["Close"].tolist()[-1], 2) if hist is not None and not hist.empty else 0

        pred = {
            "symbol": symbol,
            "action": action,
            "confidence": confidence,
            "current_price": price,
            "date": date,
            "recorded_at": datetime.now().isoformat(),
        }
        count = save_prediction([], pred)
        print(json.dumps({"recorded": pred, "total_predictions": count}, indent=2))
        return

    if args.evaluate:
        if not os.path.exists(PREDICTIONS_FILE):
            print(json.dumps({"error": "No predictions to evaluate"}))
            return
        with open(PREDICTIONS_FILE) as f:
            preds = json.load(f)
        scored = evaluate_predictions(preds)
        with open(PREDICTIONS_FILE, "w") as f:
            json.dump(scored, f, indent=2)

        evaluated = [p for p in scored if p.get("evaluated")]
        correct = len([p for p in evaluated if p.get("correct") is True])
        wrong = len([p for p in evaluated if p.get("correct") is False])
        print(f"\nEvaluated {len(evaluated)} predictions: {correct} correct, {wrong} wrong",
              file=sys.stderr)
        return

    if args.date:
        snapshot = run_backtest(args.date, stocks=args.stocks)
        # Also print the agent prompt to stdout
        if "agent_prompt" in snapshot:
            print("\n" + "=" * 60, file=sys.stderr)
            print("AGENT PROMPT (feed this to the AI):", file=sys.stderr)
            print("=" * 60, file=sys.stderr)
            print(snapshot["agent_prompt"], file=sys.stderr)
        # Print JSON snapshot to stdout
        print(json.dumps(snapshot, indent=2))
        return

    if args.start and args.end:
        results = run_backtest_range(args.start, args.end, step=args.step, stocks=args.stocks)
        print(json.dumps(results, indent=2))
        return

    parser.print_help()


if __name__ == "__main__":
    main()
