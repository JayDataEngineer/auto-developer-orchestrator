#!/usr/bin/env python3
"""
regime.py — Layer 6: Market Regime Detection

Classifies the current market regime (bull/bear/sideways) from 4 signal pillars:
trend, volatility, momentum, and breadth. Outputs strategy parameter adjustments
per regime so the agent can adapt its trading style.

No ML — rule-based scoring with configurable thresholds.

CLI:
  python3 regime.py detect              — classify regime with visual report
  python3 regime.py adjust              — output regime params as JSON
  python3 regime.py history [--limit N] — show past regime detections
  python3 regime.py signal --ticker X   — regime-adjusted view for a ticker
"""

import json
import os
import sys
from datetime import datetime

# ── Config ─────────────────────────────────────────────────────────

DEFAULT_CONFIG = {
    "spy_ticker": "SPY",
    "tracked_tickers": ["AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "TSLA", "META"],
    "regime_thresholds": {
        "bull": 65,
        "bear": 35,
    },
    "weights": {
        "trend":      0.35,
        "volatility": 0.25,
        "momentum":   0.25,
        "breadth":    0.15,
    },
    "strategy": {
        "bull": {
            "position_size_mult": 1.2,
            "stop_atr_mult":      2.5,
            "confidence_adj":     0.05,
            "bias":               "long",
            "max_positions":      10,
        },
        "bear": {
            "position_size_mult": 0.5,
            "stop_atr_mult":      1.5,
            "confidence_adj":    -0.05,
            "bias":               "short",
            "max_positions":      3,
        },
        "sideways": {
            "position_size_mult": 0.8,
            "stop_atr_mult":      2.0,
            "confidence_adj":     0.0,
            "bias":               "neutral",
            "max_positions":      5,
        },
    },
}

HISTORY_FILE = os.environ.get("REGIME_HISTORY", "/sandbox/regime_history.json")


# ── Indicator Helpers ──────────────────────────────────────────────


def compute_sma(prices, period):
    """Simple moving average for the last point."""
    if len(prices) < period:
        return sum(prices) / len(prices) if prices else 0.0
    return sum(prices[-period:]) / period


def compute_roc(prices, period):
    """Rate of change (%) over period."""
    if len(prices) < period + 1 or prices[-period - 1] == 0:
        return 0.0
    return (prices[-1] - prices[-period - 1]) / prices[-period - 1] * 100


# ── Regime Scoring Pillars (each 0-100) ────────────────────────────


def score_trend(prices):
    """Score market trend. 0 = bearish, 100 = bullish."""
    score = 50.0
    details = {}

    if len(prices) < 50:
        return 50.0, {"note": "insufficient data"}

    sma50 = compute_sma(prices, 50)

    # Price vs SMA50: ±20 points
    dist50 = (prices[-1] - sma50) / sma50 * 100
    details["price_vs_sma50"] = round(dist50, 2)
    score += max(-20, min(20, dist50 * 4))

    # SMA50 slope over 5 bars: ±15 points
    if len(prices) >= 55:
        sma50_prev = compute_sma(prices[:-5], 50)
        slope = (sma50 - sma50_prev) / sma50_prev * 100
        details["sma50_slope"] = round(slope, 3)
        score += max(-15, min(15, slope * 30))

    # Golden/death cross (SMA50 vs SMA200): ±15 points
    if len(prices) >= 200:
        sma200 = compute_sma(prices, 200)
        cross_dist = (sma50 - sma200) / sma200 * 100
        details["price_vs_sma200"] = round(
            (prices[-1] - sma200) / sma200 * 100, 2)
        score += max(-15, min(15, cross_dist * 5))
        details["golden_cross"] = cross_dist > 0
    else:
        details["golden_cross"] = None

    return round(max(0, min(100, score)), 1), details


def score_volatility(vix, vix_trend=0.0):
    """Score volatility. 100 = calm/bullish, 0 = fearful/bearish."""
    if vix <= 15:
        base = 90
    elif vix <= 18:
        base = 80
    elif vix <= 22:
        base = 65
    elif vix <= 25:
        base = 50
    elif vix <= 30:
        base = 30
    elif vix <= 35:
        base = 15
    else:
        base = 5

    # VIX trend: rising = bearish, falling = bullish (±10)
    base -= max(-10, min(10, vix_trend * 5))

    return round(max(0, min(100, base)), 1), {
        "vix": vix,
        "vix_trend": round(vix_trend, 2),
        "zone": ("calm" if vix < 18 else "normal" if vix < 25
                 else "elevated" if vix < 30 else "fearful"),
    }


def score_momentum(prices):
    """Score price momentum. 0 = declining, 100 = rising."""
    if len(prices) < 51:
        return 50.0, {"note": "insufficient data"}

    roc20 = compute_roc(prices, 20)
    roc50 = compute_roc(prices, 50)

    # 20d ROC → 0-60 (center 30), 50d ROC → 0-40 (center 20)
    score_20 = max(0, min(60, 30 + roc20 * 6))
    score_50 = max(0, min(40, 20 + roc50 * 4))

    return round(max(0, min(100, score_20 + score_50)), 1), {
        "roc_20d": round(roc20, 2),
        "roc_50d": round(roc50, 2),
    }


def score_breadth(ticker_prices):
    """Score market breadth: % of tickers above their SMA50."""
    if not ticker_prices:
        return 50.0, {"note": "no tickers"}

    above, total = 0, 0
    for _ticker, prices in ticker_prices.items():
        if len(prices) < 50:
            continue
        sma50 = compute_sma(prices, 50)
        if prices[-1] > sma50:
            above += 1
        total += 1

    if total == 0:
        return 50.0, {"note": "insufficient data for any ticker"}

    pct = above / total * 100
    return round(pct, 1), {"above_sma50": above, "total": total, "pct": round(pct, 1)}


# ── Regime Classification ─────────────────────────────────────────


def classify_regime(scores, weights, thresholds):
    """Classify regime from pillar scores.

    Returns (regime_str, confidence_0_1, composite_0_100).
    """
    composite = sum(
        weights.get(k, 0.25) * scores.get(k, 50)
        for k in ("trend", "volatility", "momentum", "breadth"))

    bull_t = thresholds.get("bull", 65)
    bear_t = thresholds.get("bear", 35)

    if composite >= bull_t:
        regime = "bull"
    elif composite <= bear_t:
        regime = "bear"
    else:
        regime = "sideways"

    # Confidence scales with distance from decision boundary
    if regime == "bull":
        confidence = min(1.0, (composite - bull_t) / (100 - bull_t) * 2 + 0.5)
    elif regime == "bear":
        confidence = min(1.0, (bear_t - composite) / bear_t * 2 + 0.5)
    else:
        mid = (bull_t + bear_t) / 2
        dist = abs(composite - mid) / ((bull_t - bear_t) / 2)
        confidence = 0.3 + dist * 0.3

    return regime, round(min(1.0, confidence), 2), round(composite, 1)


# ── Strategy Parameters ───────────────────────────────────────────


def get_regime_params(regime, strategy_config):
    """Get strategy parameters for the given regime."""
    return strategy_config.get(regime, strategy_config.get("sideways", {})).copy()


# ── Data Fetching ──────────────────────────────────────────────────


def fetch_spy(period="1y"):
    """Fetch SPY closing prices."""
    import yfinance as yf
    df = yf.Ticker("SPY").history(period=period)
    return df["Close"].tolist() if not df.empty else None


def fetch_ticker_prices(tickers, period="6mo"):
    """Fetch closing prices for multiple tickers."""
    import yfinance as yf
    result = {}
    for t in tickers:
        df = yf.Ticker(t).history(period=period)
        if not df.empty:
            result[t] = df["Close"].tolist()
    return result


def load_vix():
    """Load VIX from market_data.json."""
    path = os.environ.get("MARKET_DATA_FILE", "/sandbox/market_data.json")
    if not os.path.exists(path):
        return 18.0
    with open(path) as f:
        return json.load(f).get("VIX", 18.0)


def estimate_vix_trend():
    """Estimate VIX daily change from recent data."""
    import yfinance as yf
    df = yf.Ticker("^VIX").history(period="5d")
    if df.empty or len(df) < 2:
        return 0.0
    return (df["Close"].iloc[-1] - df["Close"].iloc[0]) / len(df)


# ── History ────────────────────────────────────────────────────────


def save_detection(detection, path=None):
    """Append detection to history file (atomic)."""
    import tempfile
    path = path or HISTORY_FILE
    history = []
    if os.path.exists(path):
        try:
            with open(path) as f:
                history = json.load(f)
        except (json.JSONDecodeError, IOError):
            pass
    history.append(detection)
    history = history[-100:]
    tmp = tempfile.NamedTemporaryFile(
        "w", suffix=".json", delete=False,
        dir=os.path.dirname(path) or ".")
    json.dump(history, tmp, indent=2)
    tmp.close()
    os.replace(tmp.name, path)


def load_history(path=None):
    """Load regime history."""
    path = path or HISTORY_FILE
    if not os.path.exists(path):
        return []
    with open(path) as f:
        return json.load(f)


# ── Full Detection ─────────────────────────────────────────────────


def detect_regime(config):
    """Run full regime detection pipeline. Returns detection dict."""
    spy_prices = fetch_spy()
    vix = load_vix()
    vix_trend = estimate_vix_trend()
    ticker_prices = fetch_ticker_prices(config.get("tracked_tickers", []))

    trend_s, trend_d = score_trend(spy_prices or [])
    vol_s, vol_d = score_volatility(vix, vix_trend)
    mom_s, mom_d = score_momentum(spy_prices or [])
    breadth_s, breadth_d = score_breadth(ticker_prices)

    scores = {"trend": trend_s, "volatility": vol_s,
              "momentum": mom_s, "breadth": breadth_s}
    weights = config.get("weights", DEFAULT_CONFIG["weights"])
    thresholds = config.get("regime_thresholds", DEFAULT_CONFIG["regime_thresholds"])

    regime, confidence, composite = classify_regime(scores, weights, thresholds)
    params = get_regime_params(regime, config.get("strategy", DEFAULT_CONFIG["strategy"]))

    return {
        "timestamp":  datetime.now().isoformat(),
        "regime":     regime,
        "confidence": confidence,
        "composite":  composite,
        "scores":     scores,
        "details": {
            "trend": trend_d, "volatility": vol_d,
            "momentum": mom_d, "breadth": breadth_d,
        },
        "params": params,
    }


# ── CLI Commands ──────────────────────────────────────────────────


def cmd_detect(args, config):
    """Detect and display current market regime."""
    det = detect_regime(config)

    print(f"{'=' * 60}")
    print("MARKET REGIME DETECTION")
    print(f"{'=' * 60}")
    print(f"Regime     : {det['regime'].upper()}")
    print(f"Confidence : {det['confidence']:.0%}")
    print(f"Composite  : {det['composite']:.1f}/100")

    print("\nSignal Pillars:")
    for pillar in ("trend", "volatility", "momentum", "breadth"):
        s = det["scores"][pillar]
        bar = "#" * int(s / 5) + "-" * (20 - int(s / 5))
        print(f"  {pillar:12s} [{bar}] {s:5.1f}")

    d = det["details"]
    if "price_vs_sma50" in d.get("trend", {}):
        print(f"\n  Price vs SMA50: {d['trend']['price_vs_sma50']:+.2f}%")
    if d.get("trend", {}).get("golden_cross") is not None:
        tag = "GOLDEN CROSS" if d["trend"]["golden_cross"] else "DEATH CROSS"
        print(f"  {tag}")
    print(f"  VIX: {d['volatility']['vix']:.1f} ({d['volatility']['zone']})")
    if "roc_20d" in d.get("momentum", {}):
        print(f"  20d ROC: {d['momentum']['roc_20d']:+.2f}%")
        print(f"  50d ROC: {d['momentum']['roc_50d']:+.2f}%")
    if "pct" in d.get("breadth", {}):
        print(f"  Breadth: {d['breadth']['above_sma50']}/{d['breadth']['total']} "
              f"above SMA50 ({d['breadth']['pct']:.0f}%)")

    save_detection(det)
    print(f"\nSaved to {HISTORY_FILE}")


def cmd_adjust(args, config):
    """Output regime-adjusted strategy parameters as JSON."""
    det = detect_regime(config)
    print(json.dumps({
        "regime": det["regime"],
        "confidence": det["confidence"],
        "params": det["params"],
    }, indent=2))


def cmd_history(args, config):
    """Show regime history."""
    history = load_history()
    limit = args.limit or 10
    recent = history[-limit:]
    if not recent:
        print("No regime history. Run 'detect' first.")
        return
    print(f"{'=' * 60}")
    print("REGIME HISTORY")
    print(f"{'=' * 60}")
    print(f"{'Date':20s} {'Regime':10s} {'Conf':>6s} {'Score':>8s}")
    print("-" * 46)
    for h in recent:
        ts = h.get("timestamp", "?")[:19]
        print(f"{ts:20s} {h.get('regime', '?'):10s} "
              f"{h.get('confidence', 0):5.0%} {h.get('composite', 0):8.1f}")


def cmd_signal(args, config):
    """Show regime-adjusted signal for a specific ticker."""
    det = detect_regime(config)
    regime_name = det["regime"]
    params = det["params"]

    print(f"{'=' * 60}")
    print(f"REGIME SIGNAL: {args.ticker}")
    print(f"{'=' * 60}")
    print(f"Regime: {regime_name.upper()} ({det['confidence']:.0%})")
    print(f"\nAdjustments for {args.ticker}:")
    print(f"  Position size  : {params.get('position_size_mult', 1.0):.1f}x")
    print(f"  Stop ATR mult  : {params.get('stop_atr_mult', 2.0):.1f}")
    print(f"  Confidence adj : {params.get('confidence_adj', 0):+.2f}")
    print(f"  Bias           : {params.get('bias', 'neutral')}")
    print(f"  Max positions  : {params.get('max_positions', 5)}")

    hints = {
        "bull": "Favor BUY signals. Wider stops, larger positions.",
        "bear": "Favor SELL signals. Tighter stops, smaller positions.",
        "sideways": "Neutral stance. Standard risk parameters.",
    }
    print(f"\nHint: {hints.get(regime_name, '')}")
    save_detection(det)


# ── Config ─────────────────────────────────────────────────────────


def load_config(path=None):
    """Load config with fallback to defaults (deep-copied)."""
    config = DEFAULT_CONFIG.copy()
    config["strategy"] = {k: v.copy() for k, v in DEFAULT_CONFIG["strategy"].items()}
    config["weights"] = DEFAULT_CONFIG["weights"].copy()
    config["regime_thresholds"] = DEFAULT_CONFIG["regime_thresholds"].copy()

    if path and os.path.exists(path):
        try:
            with open(path) as f:
                override = json.load(f)
            for k, v in override.items():
                if k in ("strategy", "weights", "regime_thresholds") and isinstance(v, dict):
                    config[k].update(v)
                else:
                    config[k] = v
        except (json.JSONDecodeError, IOError):
            pass
    return config


# ── Main ───────────────────────────────────────────────────────────


def main():
    import argparse
    p = argparse.ArgumentParser(description="Market Regime Detection (Layer 6)")
    sub = p.add_subparsers(dest="command")

    sub.add_parser("detect")
    sub.add_parser("adjust")

    hist = sub.add_parser("history")
    hist.add_argument("--limit", type=int, default=10)

    sig = sub.add_parser("signal")
    sig.add_argument("--ticker", required=True)

    args = p.parse_args()
    cfg = load_config("/sandbox/regime_config.json")

    {"detect": cmd_detect, "adjust": cmd_adjust,
     "history": cmd_history, "signal": cmd_signal}.get(
        args.command, lambda *_: p.print_help())(args, cfg)


if __name__ == "__main__":
    main()
