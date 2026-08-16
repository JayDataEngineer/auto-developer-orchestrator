"""
Multi-Signal Fusion — composite scoring from 4 signal pillars.

Usage:
  python3 signals.py score [--ticker AAPL]
  python3 signals.py rank
  python3 signals.py consensus
  python3 signals.py validate
"""

import json
import math
import os
import sys
import argparse

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import paths

SIGNALS_CONFIG_FILE = paths.SIGNALS_CONFIG
MARKET_DATA_FILE = paths.MARKET_DATA_FILE
SIGNALS_FILE = paths.SIGNALS_FILE
JOURNAL_FILE = paths.JOURNAL_FILE

DEFAULT_CONFIG = {
    "weights": {
        "technical": 0.35,
        "fundamental": 0.30,
        "sentiment": 0.15,
        "momentum": 0.20,
    },
    "action_thresholds": {
        "strong_buy": 70,
        "buy": 55,
        "hold_low": 40,
        "sell": 25,
    },
    "technical_weights": {
        "rsi": 0.30,
        "ma_cross": 0.25,
        "bollinger": 0.20,
        "macd": 0.25,
    },
    "fundamental_weights": {
        "valuation": 0.25,
        "profitability": 0.20,
        "growth": 0.25,
        "balance": 0.15,
        "analyst": 0.15,
    },
    "sector_pe": {
        "Technology": 28.0,
        "Consumer Cyclical": 22.0,
        "Communication Services": 20.0,
        "Healthcare": 18.0,
        "Financial Services": 14.0,
        "default": 20.0,
    },
}


# ── Config ────────────────────────────────────────────────────────────


def load_config(path=None):
    path = path or SIGNALS_CONFIG_FILE
    config = DEFAULT_CONFIG.copy()
    # Deep copy nested dicts
    for k, v in DEFAULT_CONFIG.items():
        if isinstance(v, dict):
            config[k] = v.copy()
    if os.path.exists(path):
        try:
            with open(path) as f:
                overrides = json.load(f)
            for k, v in overrides.items():
                if isinstance(v, dict) and k in config and isinstance(config[k], dict):
                    config[k].update(v)
                else:
                    config[k] = v
        except (json.JSONDecodeError, OSError):
            pass
    return config


# ── Data Loading ──────────────────────────────────────────────────────


def load_market_data(path=None):
    """Load market_data.json. Returns (assets_list, market_context)."""
    path = path or MARKET_DATA_FILE
    if not os.path.exists(path):
        return [], {}
    with open(path) as f:
        raw = json.load(f)
    assets = raw.get("assets", []) if isinstance(raw, dict) else raw
    context = {"SP500": raw.get("SP500"), "NASDAQ": raw.get("NASDAQ"), "VIX": raw.get("VIX")}
    return assets, context


def load_signals(path=None):
    """Load agent's signals.json."""
    path = path or SIGNALS_FILE
    if not os.path.exists(path):
        return []
    with open(path) as f:
        return json.load(f)


def load_journal(path=None):
    """Load journal.json."""
    path = path or JOURNAL_FILE
    if not os.path.exists(path):
        return {"version": 1, "predictions": []}
    with open(path) as f:
        return json.load(f)


# ── Helper ────────────────────────────────────────────────────────────


def clamp(val, lo, hi):
    return max(lo, min(hi, val))


def safe_get(data, *keys, default=None):
    """Safely traverse nested dicts."""
    current = data
    for k in keys:
        if not isinstance(current, dict):
            return default
        current = current.get(k, default)
        if current is None:
            return default
    return current


# ── Technical Score ───────────────────────────────────────────────────


def score_technical(asset, config):
    """Score technical indicators. Returns (score_0_100, details_dict)."""
    ind = asset.get("indicators", {})
    price = asset.get("current_price", 0)
    tw = config["technical_weights"]

    if not ind or price <= 0:
        return 50, {"note": "no indicators"}

    details = {}

    # RSI
    rsi = ind.get("rsi_14")
    if rsi is not None:
        if rsi < 20:
            rsi_score = 25
        elif rsi < 30:
            rsi_score = 45
        elif rsi < 50:
            rsi_score = 80
        elif rsi < 70:
            rsi_score = 60
        elif rsi < 80:
            rsi_score = 30
        else:
            rsi_score = 15
        details["rsi"] = round(rsi, 1)
    else:
        rsi_score = 50

    # MA Cross
    sma20 = ind.get("sma_20", 0)
    sma50 = ind.get("sma_50", 0)
    ema12 = ind.get("ema_12", 0)
    ema26 = ind.get("ema_26", 0)
    ma_score = 0
    if sma20 and price > sma20:
        ma_score += 30
    elif sma20:
        ma_score += 10
    if sma50 and price > sma50:
        ma_score += 30
    elif sma50:
        ma_score += 10
    if ema12 and ema26 and ema12 > ema26:
        ma_score += 40
    elif ema12 and ema26:
        ma_score += 10
    details["sma_cross"] = "bullish" if ema12 > ema26 else "bearish"

    # Bollinger
    bb = ind.get("bollinger", {})
    bb_score = 50
    if bb and bb.get("upper") and bb.get("lower"):
        bb_range = bb["upper"] - bb["lower"]
        if bb_range > 0:
            position = (price - bb["lower"]) / bb_range
            position = clamp(position, 0, 1)
            if position < 0.2:
                bb_score = 80  # near lower = buy
            elif position < 0.5:
                bb_score = 60
            elif position < 0.8:
                bb_score = 40
            else:
                bb_score = 20  # near upper = sell
            details["bb_position"] = round(position, 2)

    # MACD
    macd = ind.get("macd")
    if macd is not None and price > 0:
        macd_pct = macd / price * 100
        macd_score = clamp(50 + macd_pct * 500, 10, 90)
        details["macd"] = "positive" if macd > 0 else "negative"
    else:
        macd_score = 50

    # Weighted average
    score = round(tw["rsi"] * rsi_score + tw["ma_cross"] * ma_score +
                  tw["bollinger"] * bb_score + tw["macd"] * macd_score)
    score = clamp(score, 0, 100)

    return score, details


# ── Fundamental Score ─────────────────────────────────────────────────


def score_fundamental(asset, config):
    """Score fundamental metrics. Returns (score_0_100, details_dict)."""
    fund = asset.get("fundamentals")
    if not fund:
        return 50, {"note": "no fundamentals (likely crypto)"}

    fw = config["fundamental_weights"]
    details = {}

    # Valuation
    val = fund.get("valuation", {})
    pe = val.get("trailingPE")
    fwd_pe = val.get("forwardPE")
    peg = val.get("pegRatio")
    sector = fund.get("sector", "default")
    sector_pe = config["sector_pe"].get(sector, config["sector_pe"]["default"])

    val_score = 50
    if pe and pe > 0:
        pe_ratio = pe / sector_pe
        if pe_ratio < 0.7:
            val_score = 85
        elif pe_ratio < 1.0:
            val_score = 70
        elif pe_ratio < 1.3:
            val_score = 55
        elif pe_ratio < 2.0:
            val_score = 40
        else:
            val_score = 20
        details["pe_vs_sector"] = "cheap" if pe_ratio < 1 else "expensive"
    if peg and peg < 1:
        val_score = min(90, val_score + 10)
        details["peg_bonus"] = True
    if fwd_pe and pe and fwd_pe < pe:
        val_score = min(90, val_score + 5)
    details["trailingPE"] = pe

    # Profitability
    prof = fund.get("profitability", {})
    margins = prof.get("profitMargins", 0)
    op_margins = prof.get("operatingMargins", 0)
    roe = prof.get("returnOnEquity", 0)
    prof_score = 50
    if margins and margins > 0.2:
        prof_score += 15
    if op_margins and op_margins > 0.2:
        prof_score += 15
    if roe and roe > 0.15:
        prof_score += 20
    prof_score = clamp(prof_score, 10, 100)
    details["margins"] = "strong" if margins and margins > 0.15 else "weak"

    # Growth
    growth = fund.get("growth", {})
    rev_g = growth.get("revenueGrowth", 0) or 0
    earn_g = growth.get("earningsGrowth", 0) or 0
    growth_score = 50
    if rev_g > 0:
        growth_score += 10
    if rev_g > 0.1:
        growth_score += 10
    if earn_g > 0:
        growth_score += 10
    if earn_g > 0.15:
        growth_score += 10
    growth_score = clamp(growth_score, 10, 100)
    details["growth"] = "positive" if rev_g > 0 or earn_g > 0 else "negative"

    # Balance sheet
    bs = fund.get("balance_sheet", {})
    de = bs.get("debtToEquity")
    cr = bs.get("currentRatio")
    bal_score = 50
    if de is not None:
        if de < 50:
            bal_score += 20
        elif de < 100:
            bal_score += 10
        elif de > 200:
            bal_score -= 15
    if cr and cr > 1.5:
        bal_score += 10
    bal_score = clamp(bal_score, 10, 100)

    # Analyst consensus
    analysts = fund.get("analysts", {})
    rec = analysts.get("recommendationKey", "hold")
    upside = analysts.get("upside_pct")
    rec_map = {"strong_buy": 100, "buy": 80, "hold": 50, "sell": 30, "strong_sell": 10}
    anal_score = rec_map.get(rec, 50)
    if upside and upside > 15:
        anal_score = min(100, anal_score + 10)
    details["analyst"] = rec
    details["upside_pct"] = upside

    # Weighted average
    score = round(fw["valuation"] * val_score + fw["profitability"] * prof_score +
                  fw["growth"] * growth_score + fw["balance"] * bal_score +
                  fw["analyst"] * anal_score)
    return clamp(score, 0, 100), details


# ── Sentiment Score ───────────────────────────────────────────────────


def score_sentiment(asset, config):
    """Score market sentiment. Returns (score_0_100, details_dict)."""
    fund = asset.get("fundamentals", {})
    if not fund:
        return 50, {"note": "no data"}

    details = {}

    # Analyst upside
    analysts = fund.get("analysts", {})
    upside = analysts.get("upside_pct")
    if upside is not None:
        if upside > 20:
            upside_score = 90
        elif upside > 10:
            upside_score = 70
        elif upside > 0:
            upside_score = 50
        else:
            upside_score = 25
        details["upside"] = round(upside, 1)
    else:
        upside_score = 50

    # Institutional holding
    ownership = fund.get("ownership", {})
    inst = ownership.get("heldPercentInstitutions")
    if inst is not None:
        if inst > 0.7:
            inst_score = 75
        elif inst > 0.5:
            inst_score = 60
        elif inst > 0.3:
            inst_score = 50
        else:
            inst_score = 35
    else:
        inst_score = 50

    # Short interest
    short = ownership.get("shortPercentOfFloat")
    if short is not None:
        if short < 0.02:
            short_score = 75
        elif short < 0.05:
            short_score = 55
        elif short < 0.10:
            short_score = 35
        else:
            short_score = 15
    else:
        short_score = 50

    # Average
    avg_score = (upside_score + inst_score + short_score) / 3

    # VIX multiplier
    vix = asset.get("_market_vix")
    if vix is not None:
        if vix < 18:
            mult = 1.0
            details["vix"] = "calm"
        elif vix < 25:
            mult = 0.9
            details["vix"] = "moderate"
        elif vix < 35:
            mult = 0.7
            details["vix"] = "fearful"
        else:
            mult = 0.5
            details["vix"] = "extreme_fear"
        avg_score *= mult
    else:
        details["vix"] = "unknown"

    return clamp(round(avg_score), 0, 100), details


# ── Momentum Score ────────────────────────────────────────────────────


def score_momentum(asset, config):
    """Score price momentum. Returns (score_0_100, details_dict)."""
    price = asset.get("current_price", 0)
    ind = asset.get("indicators", {})
    prices = asset.get("prices", [])
    details = {}

    if price <= 0:
        return 50, {"note": "no price data"}

    # Price vs SMAs
    sma20 = ind.get("sma_20", 0)
    sma50 = ind.get("sma_50", 0)
    sma_score = 50
    if sma20:
        pct_above = (price - sma20) / sma20 * 100
        sma_score += clamp(pct_above * 5, -30, 30)
        details["above_sma20"] = price > sma20
    if sma50:
        pct_above = (price - sma50) / sma50 * 100
        sma_score += clamp(pct_above * 3, -20, 20)
        details["above_sma50"] = price > sma50
    sma_score = clamp(sma_score, 0, 100)

    # Rate of change
    roc_score = 50
    if len(prices) >= 6:
        roc5 = (prices[-1] - prices[-6]) / prices[-6] * 100
        details["price_change_5d"] = round(roc5, 2)
        roc_score += clamp(roc5 * 5, -30, 30)
    if len(prices) >= 21:
        roc20 = (prices[-1] - prices[-21]) / prices[-21] * 100
        details["price_change_20d"] = round(roc20, 2)
        roc_score += clamp(roc20 * 2, -20, 20)
    roc_score = clamp(roc_score, 0, 100)

    # Volume trend
    vol_score = 50
    current_vol = asset.get("volume", 0)
    if current_vol and len(prices) >= 10:
        avg_vol_est = current_vol  # rough: use current vs itself
        details["volume_trend"] = "normal"
    elif current_vol:
        details["volume_trend"] = "normal"
    else:
        details["volume_trend"] = "unknown"

    # Average
    score = round((sma_score + roc_score + vol_score) / 3)
    return clamp(score, 0, 100), details


# ── Composite and Action ──────────────────────────────────────────────


def compute_composite(pillars, weights):
    """Weighted sum of pillar scores."""
    return sum(pillars[k] * weights.get(k, 0.25) for k in pillars)


def determine_action(composite, technical_score, thresholds):
    """Map composite + technical score to action signal."""
    if composite >= thresholds.get("strong_buy", 70) and technical_score >= 65:
        return "strong_buy"
    elif composite >= thresholds.get("buy", 55):
        return "buy"
    elif composite >= thresholds.get("hold_low", 40):
        return "hold"
    elif composite >= thresholds.get("sell", 25):
        return "sell"
    else:
        return "strong_sell"


def score_ticker(asset, config):
    """Score a single asset. Returns full score dict."""
    tech_score, tech_details = score_technical(asset, config)
    fund_score, fund_details = score_fundamental(asset, config)
    sent_score, sent_details = score_sentiment(asset, config)
    mom_score, mom_details = score_momentum(asset, config)

    pillars = {
        "technical": tech_score,
        "fundamental": fund_score,
        "sentiment": sent_score,
        "momentum": mom_score,
    }
    weights = config["weights"]
    composite = compute_composite(pillars, weights)
    action = determine_action(composite, tech_score, config["action_thresholds"])

    return {
        "ticker": asset.get("symbol", "UNKNOWN"),
        "name": asset.get("name", ""),
        "price": asset.get("current_price"),
        "composite_score": round(composite, 1),
        "action_signal": action,
        "pillars": {
            "technical": {"score": tech_score, "details": tech_details},
            "fundamental": {"score": fund_score, "details": fund_details},
            "sentiment": {"score": sent_score, "details": sent_details},
            "momentum": {"score": mom_score, "details": mom_details},
        },
    }


# ── CLI Commands ──────────────────────────────────────────────────────


def cmd_score(args, config):
    assets, context = load_market_data()
    vix = context.get("VIX")
    results = []
    for asset in assets:
        if not isinstance(asset, dict) or asset.get("error"):
            continue
        asset["_market_vix"] = vix
        if args.ticker and asset.get("symbol") != args.ticker:
            continue
        results.append(score_ticker(asset, config))

    if args.ticker and len(results) == 1:
        print(json.dumps(results[0], indent=2))
    else:
        ranked = sorted(results, key=lambda x: x["composite_score"], reverse=True)
        print(json.dumps(ranked, indent=2))


def cmd_rank(args, config):
    assets, context = load_market_data()
    vix = context.get("VIX")
    results = []
    for asset in assets:
        if not isinstance(asset, dict) or asset.get("error"):
            continue
        asset["_market_vix"] = vix
        results.append(score_ticker(asset, config))

    ranked = sorted(results, key=lambda x: x["composite_score"], reverse=True)

    print("=== Signal Rankings ===")
    print(f"{'Rank':<5} {'Ticker':<8} {'Composite':>9} {'Tech':>5} {'Fund':>5} "
          f"{'Sent':>5} {'Mom':>5} Action")
    print("-" * 60)
    for i, r in enumerate(ranked, 1):
        p = r["pillars"]
        print(f"{i:<5} {r['ticker']:<8} {r['composite_score']:>9.1f} "
              f"{p['technical']['score']:>5} {p['fundamental']['score']:>5} "
              f"{p['sentiment']['score']:>5} {p['momentum']['score']:>5} "
              f"{r['action_signal'].upper()}")


def cmd_consensus(args, config):
    assets, context = load_market_data()
    vix = context.get("VIX")

    scored = {}
    for asset in assets:
        if not isinstance(asset, dict) or asset.get("error"):
            continue
        asset["_market_vix"] = vix
        r = score_ticker(asset, config)
        scored[r["ticker"]] = r

    agent_signals = {}
    for sig in load_signals():
        agent_signals[sig.get("symbol")] = sig

    agreements = 0
    divergences = []
    total = 0

    for ticker, my_score in scored.items():
        agent_sig = agent_signals.get(ticker)
        if not agent_sig:
            continue
        total += 1

        def normalize(a):
            if a in ("strong_buy", "buy"):
                return "buy"
            if a in ("strong_sell", "sell"):
                return "sell"
            return "hold"

        agent_action = agent_sig.get("action", "hold")
        my_action = my_score["action_signal"]

        if normalize(agent_action) == normalize(my_action):
            agreements += 1
        else:
            divergences.append({
                "ticker": ticker,
                "composite_score": my_score["composite_score"],
                "signal_action": my_action,
                "agent_action": agent_action,
                "agent_confidence": agent_sig.get("confidence"),
                "agent_reasoning": agent_sig.get("reasoning", "")[:80],
            })

    result = {
        "agreement_rate": round(agreements / total * 100, 1) if total else 0,
        "agreements": agreements,
        "total_compared": total,
        "divergences": divergences,
    }
    print(json.dumps(result, indent=2))

    if divergences:
        print("\nDivergences:", file=sys.stderr)
        for d in divergences:
            print(f"  {d['ticker']}: signal={d['signal_action']} vs "
                  f"agent={d['agent_action']} (score={d['composite_score']})",
                  file=sys.stderr)


def cmd_validate(args, config):
    journal = load_journal()
    predictions = journal.get("predictions", [])
    evaluated = [p for p in predictions if p.get("outcome") is not None]

    if not evaluated:
        print(json.dumps({"note": "No evaluated predictions to validate"}))
        return

    buckets = {
        "high_conf_0.8+": [],
        "medium_conf_0.6-0.8": [],
        "low_conf_below_0.6": [],
    }
    for p in evaluated:
        c = p.get("confidence", 0.5)
        if c >= 0.8:
            buckets["high_conf_0.8+"].append(p)
        elif c >= 0.6:
            buckets["medium_conf_0.6-0.8"].append(p)
        else:
            buckets["low_conf_below_0.6"].append(p)

    result = {}
    for label, preds in buckets.items():
        if not preds:
            continue
        correct = sum(1 for p in preds if p["outcome"])
        result[label] = {
            "total": len(preds),
            "correct": correct,
            "accuracy": round(correct / len(preds) * 100, 1),
        }

    by_action = {}
    for p in evaluated:
        action = p.get("action", "unknown")
        if action not in by_action:
            by_action[action] = {"correct": 0, "total": 0}
        by_action[action]["total"] += 1
        if p["outcome"]:
            by_action[action]["correct"] += 1
    result["by_action"] = by_action

    print(json.dumps(result, indent=2))


# ── CLI ───────────────────────────────────────────────────────────────


def main():
    parser = argparse.ArgumentParser(description="Multi-Signal Fusion")
    sub = parser.add_subparsers(dest="command")

    score_p = sub.add_parser("score", help="Score tickers")
    score_p.add_argument("--ticker", default=None)

    sub.add_parser("rank", help="Ranked table of all tickers")
    sub.add_parser("consensus", help="Compare with agent signals")
    sub.add_parser("validate", help="Backtest scores vs outcomes")

    args = parser.parse_args()
    config = load_config()

    if args.command == "score":
        cmd_score(args, config)
    elif args.command == "rank":
        cmd_rank(args, config)
    elif args.command == "consensus":
        cmd_consensus(args, config)
    elif args.command == "validate":
        cmd_validate(args, config)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
