"""
Prediction Journal — records signals, evaluates accuracy, feeds stats back.

Usage:
  python3 journal.py record --ticker AAPL --action buy --confidence 0.8 --price 280.25 --reasoning "..."
  python3 journal.py record-signals
  python3 journal.py evaluate
  python3 journal.py stats [--ticker AAPL]
  python3 journal.py recent [--limit 10]
  python3 journal.py breakdown
"""

import json
import os
import sys
import argparse
import tempfile
from datetime import datetime, timedelta
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import paths

JOURNAL_FILE = paths.JOURNAL_FILE
SIGNALS_FILE = paths.SIGNALS_FILE
MARKET_DATA_FILE = paths.MARKET_DATA_FILE
MAX_PREDICTIONS = 500
ARCHIVE_FILE = paths.JOURNAL_ARCHIVE

# ── Data I/O ──────────────────────────────────────────────────────────


def load_journal(path=None):
    """Load journal from disk. Returns empty structure on missing/corrupt file."""
    path = path or JOURNAL_FILE
    if not os.path.exists(path):
        return {"version": 1, "predictions": []}
    try:
        with open(path) as f:
            data = json.load(f)
        if "predictions" not in data:
            data["predictions"] = []
        return data
    except (json.JSONDecodeError, OSError):
        return {"version": 1, "predictions": []}


def save_journal(journal, path=None):
    """Atomic write: temp file then rename."""
    path = path or JOURNAL_FILE
    dir_name = os.path.dirname(path) or "."
    os.makedirs(dir_name, exist_ok=True)
    fd, tmp = tempfile.mkstemp(dir=dir_name, suffix=".json")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(journal, f, indent=2, default=str)
        os.replace(tmp, path)
    except Exception:
        if os.path.exists(tmp):
            os.unlink(tmp)
        raise


def archive_if_needed(journal, path=None):
    """Move old predictions to archive when journal exceeds MAX_PREDICTIONS."""
    preds = journal["predictions"]
    if len(preds) <= MAX_PREDICTIONS:
        return journal
    # Keep the most recent MAX_PREDICTIONS
    keep = preds[-MAX_PREDICTIONS:]
    archive_preds = preds[:-MAX_PREDICTIONS]

    # Load existing archive and append
    archive = load_journal(ARCHIVE_FILE)
    archive["predictions"].extend(archive_preds)
    save_journal(archive, ARCHIVE_FILE)

    journal["predictions"] = keep
    return journal


# ── Record ────────────────────────────────────────────────────────────


def make_prediction_id(ticker, action, timestamp_str):
    """Generate a stable ID from ticker+action+date for dedup."""
    date_part = timestamp_str[:10].replace("-", "")
    return f"{date_part}_{ticker}_{action}"


def record_prediction(ticker, action, confidence, price, reasoning,
                      indicators=None, market=None, path=None):
    """Record a single prediction. Returns the prediction ID."""
    journal = load_journal(path)
    now = datetime.utcnow().isoformat(timespec="seconds")
    pred_id = make_prediction_id(ticker, action, now)

    # Dedup: skip if same ticker+action+date already exists
    for p in journal["predictions"]:
        if p["id"] == pred_id:
            print(f"Duplicate: {pred_id} already exists, skipping")
            return pred_id

    prediction = {
        "id": pred_id,
        "timestamp": now,
        "ticker": ticker,
        "action": action,
        "confidence": float(confidence),
        "price": float(price),
        "reasoning": reasoning or "",
        "indicators": indicators or {},
        "market": market or {},
        "evaluations": {"1d": None, "7d": None, "30d": None},
        "outcome": None,
    }

    journal["predictions"].append(prediction)
    journal = archive_if_needed(journal, path)
    save_journal(journal, path)
    print(f"Recorded: {pred_id}")
    return pred_id


def record_from_signals(path=None):
    """Record predictions from signals.json + market_data.json."""
    journal = load_journal(path)

    # Load signals
    if not os.path.exists(SIGNALS_FILE):
        print("No signals.json found")
        return
    with open(SIGNALS_FILE) as f:
        signals = json.load(f)

    # Load market data for indicator snapshots
    market_data = {}
    if os.path.exists(MARKET_DATA_FILE):
        with open(MARKET_DATA_FILE) as f:
            raw = json.load(f)
        # Index by symbol for quick lookup
        for item in raw if isinstance(raw, list) else raw.get("assets", []):
            if isinstance(item, dict) and "symbol" in item:
                market_data[item["symbol"]] = item

    # Extract market context (SPY, VIX)
    market_ctx = {}
    spy = market_data.get("SPY", {})
    if spy:
        market_ctx["sp500"] = spy.get("current_price")
    for key in ("^VIX", "VIX"):
        vix = market_data.get(key, {})
        if vix:
            market_ctx["vix"] = vix.get("current_price")

    now = datetime.utcnow().isoformat(timespec="seconds")
    added = 0

    for sig in signals:
        ticker = sig.get("symbol", sig.get("ticker", ""))
        action = sig.get("action", "hold")
        confidence = sig.get("confidence", 0.5)
        reasoning = sig.get("reasoning", "")
        price = 0.0

        # Get price from market data
        md = market_data.get(ticker, {})
        if md:
            price = md.get("current_price", 0.0)

        # Extract indicators for this ticker
        indicators = {}
        for k in ("rsi", "sma20", "sma50", "ema12", "ema26",
                   "bb_upper", "bb_lower", "bb_middle",
                   "macd", "macd_signal", "volume"):
            if k in md:
                indicators[k] = md[k]
            elif "indicators" in md and k in md["indicators"]:
                indicators[k] = md["indicators"][k]

        pred_id = make_prediction_id(ticker, action, now)

        # Dedup
        existing_ids = {p["id"] for p in journal["predictions"]}
        if pred_id in existing_ids:
            print(f"Duplicate: {pred_id}, skipping")
            continue

        prediction = {
            "id": pred_id,
            "timestamp": now,
            "ticker": ticker,
            "action": action,
            "confidence": float(confidence),
            "price": float(price),
            "reasoning": reasoning,
            "indicators": indicators,
            "market": market_ctx,
            "evaluations": {"1d": None, "7d": None, "30d": None},
            "outcome": None,
        }

        journal["predictions"].append(prediction)
        added += 1
        print(f"Recorded: {pred_id}")

    if added > 0:
        journal = archive_if_needed(journal, path)
        save_journal(journal, path)
    print(f"\nTotal new predictions: {added}")


# ── Evaluate ──────────────────────────────────────────────────────────


def fetch_price_yfinance(ticker):
    """Fetch current price via yfinance. Returns None on failure."""
    try:
        import yfinance as yf
        t = yf.Ticker(ticker)
        hist = t.history(period="1d")
        if hist.empty:
            return None
        return float(hist["Close"].iloc[-1])
    except Exception as e:
        print(f"  Price fetch failed for {ticker}: {e}", file=sys.stderr)
        return None


def fetch_price_alpaca(ticker):
    """Fetch current price via Alpaca (if available in sandbox)."""
    try:
        from alpaca.trading.client import TradingClient
        API_KEY = os.environ.get("ALPACA_API_KEY")
        SECRET = os.environ.get("ALPACA_SECRET_KEY")
        if not API_KEY or not SECRET:
            return None  # no keys → fall through to yfinance
        client = TradingClient(API_KEY, SECRET, paper=True)
        quote = client.get_stock_latest_quote(ticker)
        if quote:
            ask = float(quote.ask_price or 0)
            bid = float(quote.bid_price or 0)
            if ask + bid > 0:
                return (ask + bid) / 2
    except Exception:
        pass
    return None


def fetch_price(ticker):
    """Try Alpaca first, then yfinance."""
    price = fetch_price_alpaca(ticker)
    if price:
        return price
    return fetch_price_yfinance(ticker)


def determine_outcome(action, change_pct):
    """Determine if a prediction was correct based on action and price change.

    buy/strong_buy: correct if price went up
    sell/strong_sell: correct if price went down
    hold: correct if price stayed within +/- 2%
    """
    if action in ("buy", "strong_buy"):
        return change_pct > 0
    elif action in ("sell", "strong_sell"):
        return change_pct < 0
    elif action == "hold":
        return abs(change_pct) <= 2.0
    return None


def evaluate_predictions(path=None):
    """Check unevaluated predictions and fetch prices to evaluate."""
    journal = load_journal(path)
    now = datetime.utcnow()
    evaluated = 0
    errors = 0

    for pred in journal["predictions"]:
        ts = datetime.fromisoformat(pred["timestamp"].replace("Z", "+00:00").replace("+00:00", ""))
        age_days = (now - ts).days

        for horizon, days in [("1d", 1), ("7d", 7), ("30d", 30)]:
            if pred["evaluations"].get(horizon) is not None:
                continue
            if age_days < days:
                continue

            # Fetch current price
            price = fetch_price(pred["ticker"])
            if price is None:
                errors += 1
                continue

            entry_price = pred["price"]
            if entry_price <= 0:
                continue

            change_pct = round(((price - entry_price) / entry_price) * 100, 2)

            pred["evaluations"][horizon] = {
                "timestamp": now.isoformat(timespec="seconds"),
                "price": price,
                "change_pct": change_pct,
            }
            evaluated += 1
            print(f"  {pred['id']} {horizon}: ${entry_price:.2f} → ${price:.2f} ({change_pct:+.2f}%)")

        # Update overall outcome from the longest available evaluation
        if pred["outcome"] is None:
            for horizon in ("30d", "7d", "1d"):
                ev = pred["evaluations"].get(horizon)
                if ev is not None:
                    outcome = determine_outcome(pred["action"], ev["change_pct"])
                    if outcome is not None:
                        pred["outcome"] = outcome
                    break

    save_journal(journal, path)
    print(f"\nEvaluated: {evaluated} | Errors: {errors}")
    return evaluated


# ── Stats ─────────────────────────────────────────────────────────────


def compute_stats(journal, ticker_filter=None):
    """Compute accuracy statistics."""
    preds = journal["predictions"]

    if ticker_filter:
        preds = [p for p in preds if p["ticker"] == ticker_filter]

    total = len(preds)
    evaluated = [p for p in preds if p["outcome"] is not None]
    pending = total - len(evaluated)

    correct = sum(1 for p in evaluated if p["outcome"] is True)
    wrong = sum(1 for p in evaluated if p["outcome"] is False)
    accuracy = (correct / len(evaluated) * 100) if evaluated else 0

    avg_confidence = (sum(p["confidence"] for p in preds) / total) if total else 0

    # By action
    by_action = {}
    for p in evaluated:
        act = p["action"]
        if act not in by_action:
            by_action[act] = {"correct": 0, "total": 0}
        by_action[act]["total"] += 1
        if p["outcome"]:
            by_action[act]["correct"] += 1

    # By ticker
    by_ticker = {}
    for p in evaluated:
        t = p["ticker"]
        if t not in by_ticker:
            by_ticker[t] = {"correct": 0, "total": 0}
        by_ticker[t]["total"] += 1
        if p["outcome"]:
            by_ticker[t]["correct"] += 1

    # Recent trend (last 7 days vs prior)
    now = datetime.utcnow()
    week_ago = now - timedelta(days=7)
    recent = []
    prior = []
    for p in evaluated:
        ts = datetime.fromisoformat(p["timestamp"].replace("Z", "+00:00").replace("+00:00", ""))
        if ts >= week_ago:
            recent.append(p)
        else:
            prior.append(p)

    recent_acc = (sum(1 for p in recent if p["outcome"]) / len(recent) * 100) if recent else 0
    prior_acc = (sum(1 for p in prior if p["outcome"]) / len(prior) * 100) if prior else 0

    if recent_acc > prior_acc:
        trend = "improving"
    elif recent_acc < prior_acc:
        trend = "declining"
    else:
        trend = "stable"

    return {
        "total": total,
        "evaluated": len(evaluated),
        "pending": pending,
        "correct": correct,
        "wrong": wrong,
        "accuracy": round(accuracy, 1),
        "avg_confidence": round(avg_confidence, 2),
        "by_action": by_action,
        "by_ticker": by_ticker,
        "recent_accuracy": round(recent_acc, 1),
        "prior_accuracy": round(prior_acc, 1),
        "trend": trend,
    }


def print_stats(stats):
    """Print formatted stats to stdout."""
    print("=== Prediction Journal ===")
    print(f"Total: {stats['total']} | Evaluated: {stats['evaluated']} | Pending: {stats['pending']}")
    print(f"Accuracy: {stats['accuracy']}% ({stats['correct']}/{stats['evaluated']}) | Avg confidence: {stats['avg_confidence']}")

    if stats["by_action"]:
        parts = []
        for act, data in sorted(stats["by_action"].items()):
            acc = (data["correct"] / data["total"] * 100) if data["total"] else 0
            parts.append(f"{act} {acc:.0f}% ({data['correct']}/{data['total']})")
        print(f"\nBy action: {' | '.join(parts)}")

    if stats["recent_accuracy"] > 0 or stats["prior_accuracy"] > 0:
        print(f"Recent trend: {stats['trend']} (last 7d: {stats['recent_accuracy']}% vs prior: {stats['prior_accuracy']}%)")

    if stats["by_ticker"]:
        top = sorted(stats["by_ticker"].items(),
                     key=lambda x: x[1]["correct"] / max(x[1]["total"], 1),
                     reverse=True)[:5]
        parts = []
        for t, data in top:
            acc = (data["correct"] / data["total"] * 100) if data["total"] else 0
            parts.append(f"{t} {acc:.0f}%")
        print(f"\nTop tickers: {' | '.join(parts)}")


# ── Recent ────────────────────────────────────────────────────────────


def get_recent(journal, limit=10):
    """Get the N most recent predictions."""
    preds = journal["predictions"][-limit:]
    preds.reverse()
    return preds


def print_recent(preds):
    """Print recent predictions as a table."""
    if not preds:
        print("No predictions in journal")
        return

    print(f"{'ID':<25} {'Ticker':<8} {'Action':<12} {'Conf':>5} {'Price':>10} {'Outcome':<8} Reasoning")
    print("-" * 95)
    for p in preds:
        outcome = "?"
        if p["outcome"] is True:
            outcome = "CORRECT"
        elif p["outcome"] is False:
            outcome = "WRONG"
        print(f"{p['id']:<25} {p['ticker']:<8} {p['action']:<12} {p['confidence']:>5.2f} "
              f"${p['price']:>9.2f} {outcome:<8} {p['reasoning'][:40]}")


# ── Breakdown ─────────────────────────────────────────────────────────


def compute_breakdown(journal):
    """Accuracy breakdown by action, confidence bucket, and ticker."""
    evaluated = [p for p in journal["predictions"] if p["outcome"] is not None]

    if not evaluated:
        print("No evaluated predictions to break down")
        return

    # By action
    print("=== By Action ===")
    by_action = {}
    for p in evaluated:
        act = p["action"]
        if act not in by_action:
            by_action[act] = {"correct": 0, "total": 0, "conf_sum": 0.0}
        by_action[act]["total"] += 1
        by_action[act]["conf_sum"] += p["confidence"]
        if p["outcome"]:
            by_action[act]["correct"] += 1

    for act, data in sorted(by_action.items(), key=lambda x: x[1]["total"], reverse=True):
        acc = (data["correct"] / data["total"] * 100) if data["total"] else 0
        avg_conf = data["conf_sum"] / data["total"] if data["total"] else 0
        print(f"  {act:<12} {acc:>6.1f}% ({data['correct']}/{data['total']}) avg_conf={avg_conf:.2f}")

    # By confidence bucket
    print("\n=== By Confidence Bucket ===")
    buckets = {"0.0-0.3": (0.0, 0.3), "0.3-0.5": (0.3, 0.5),
               "0.5-0.7": (0.5, 0.7), "0.7-0.85": (0.7, 0.85),
               "0.85-1.0": (0.85, 1.01)}
    for label, (lo, hi) in buckets.items():
        bucket_preds = [p for p in evaluated if lo <= p["confidence"] < hi]
        if not bucket_preds:
            continue
        correct = sum(1 for p in bucket_preds if p["outcome"])
        acc = (correct / len(bucket_preds) * 100) if bucket_preds else 0
        print(f"  {label:<10} {acc:>6.1f}% ({correct}/{len(bucket_preds)})")

    # By ticker
    print("\n=== By Ticker (min 2 evaluated) ===")
    by_ticker = {}
    for p in evaluated:
        t = p["ticker"]
        if t not in by_ticker:
            by_ticker[t] = {"correct": 0, "total": 0}
        by_ticker[t]["total"] += 1
        if p["outcome"]:
            by_ticker[t]["correct"] += 1

    for t, data in sorted(by_ticker.items(),
                          key=lambda x: x[1]["correct"] / max(x[1]["total"], 1),
                          reverse=True):
        if data["total"] < 2:
            continue
        acc = (data["correct"] / data["total"] * 100) if data["total"] else 0
        print(f"  {t:<8} {acc:>6.1f}% ({data['correct']}/{data['total']})")


# ── CLI ───────────────────────────────────────────────────────────────


def main():
    parser = argparse.ArgumentParser(description="Prediction Journal")
    sub = parser.add_subparsers(dest="command")

    # record
    rec = sub.add_parser("record", help="Record a single prediction")
    rec.add_argument("--ticker", required=True)
    rec.add_argument("--action", required=True, choices=["buy", "strong_buy", "sell", "strong_sell", "hold"])
    rec.add_argument("--confidence", type=float, required=True)
    rec.add_argument("--price", type=float, required=True)
    rec.add_argument("--reasoning", default="")

    # record-signals
    sub.add_parser("record-signals", help="Record predictions from signals.json + market_data.json")

    # evaluate
    sub.add_parser("evaluate", help="Evaluate past predictions against actual prices")

    # stats
    st = sub.add_parser("stats", help="Show accuracy statistics")
    st.add_argument("--ticker", default=None, help="Filter by ticker")

    # recent
    rc = sub.add_parser("recent", help="Show recent predictions")
    rc.add_argument("--limit", type=int, default=10)

    # breakdown
    sub.add_parser("breakdown", help="Show accuracy breakdown by action/confidence/ticker")

    args = parser.parse_args()

    if args.command == "record":
        record_prediction(args.ticker, args.action, args.confidence,
                          args.price, args.reasoning)

    elif args.command == "record-signals":
        record_from_signals()

    elif args.command == "evaluate":
        evaluate_predictions()

    elif args.command == "stats":
        journal = load_journal()
        stats = compute_stats(journal, ticker_filter=args.ticker)
        print_stats(stats)

    elif args.command == "recent":
        journal = load_journal()
        preds = get_recent(journal, limit=args.limit)
        print_recent(preds)

    elif args.command == "breakdown":
        journal = load_journal()
        compute_breakdown(journal)

    else:
        parser.print_help()


if __name__ == "__main__":
    main()
