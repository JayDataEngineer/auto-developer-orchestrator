"""
Macro data client — FRED + commodities + FX + yield curve.
Read-only (no execution). Used by regime-analyst and crypto-analyst.

Usage:
    python3 macro.py                            # Full snapshot
    python3 macro.py --series FEDFUNDS DGS10    # Specific FRED series
    python3 macro.py --yield-curve              # Yield curve only
"""

import argparse
import json
import os
import sys
import urllib.request
import urllib.error
from datetime import datetime


FRED_API_KEY = os.environ.get("FRED_API_KEY", "")
FRED_BASE = "https://api.stlouisfed.org/fred/series/observations"


def fetch_fred_series(series_ids, observation_limit=5):
    """Fetch last N observations for each FRED series.

    Returns dict keyed by series_id with values {latest, prev, change, history}.
    """
    if not FRED_API_KEY:
        return {"error": "FRED_API_KEY not set in env (free key at fred.stlouisfed.org)"}

    out = {}
    for sid in series_ids:
        url = (
            f"{FRED_BASE}?series_id={sid}"
            f"&api_key={FRED_API_KEY}"
            f"&file_type=json"
            f"&sort_order=desc"
            f"&limit={observation_limit}"
        )
        try:
            req = urllib.request.Request(url, headers={"User-Agent": "Invest-Research/1.0"})
            with urllib.request.urlopen(req, timeout=15) as resp:
                data = json.loads(resp.read())
            obs = data.get("observations", [])
            if not obs:
                out[sid] = {"error": "no observations"}
                continue

            values = []
            for o in obs:
                try:
                    v = float(o.get("value", "NaN"))
                    if v == v:  # filter NaN
                        values.append({"date": o.get("date"), "value": v})
                except (ValueError, TypeError):
                    continue

            if not values:
                out[sid] = {"error": "all values NaN"}
                continue

            latest = values[0]
            prev = values[1] if len(values) > 1 else values[0]
            change = round(latest["value"] - prev["value"], 4)

            out[sid] = {
                "latest": latest["value"],
                "latest_date": latest["date"],
                "previous": prev["value"],
                "change": change,
                "history": values[:5],
            }
        except Exception as e:
            out[sid] = {"error": str(e)[:200]}

    return out


def yield_curve_analysis(macro_tickers):
    """Compute yield curve spread + inversion flag from Yahoo macro tickers."""
    tnx = macro_tickers.get("^TNX", {}).get("current_price")
    irx = macro_tickers.get("^IRX", {}).get("current_price")
    if tnx is None or irx is None:
        return {"error": "Need ^TNX and ^IRX in macro_tickers"}

    spread = round(tnx - irx, 3)
    return {
        "10y_yield": tnx,
        "13w_yield": irx,
        "spread": spread,
        "inverted": spread < 0,
        "signal": "bearish_yield_curve_inversion" if spread < 0 else "normal_yield_curve",
    }


def macro_regime_snapshot(macro_tickers, fred_data):
    """Aggregate a high-level macro regime snapshot.

    Returns dict with regime label + drivers.
    """
    drivers = []

    # Yield curve
    yca = yield_curve_analysis(macro_tickers)
    if "error" not in yca:
        if yca["inverted"]:
            drivers.append({"factor": "yield_curve_inverted", "direction": "bearish", "weight": 0.25})
        else:
            drivers.append({"factor": "yield_curve_normal", "direction": "bullish", "weight": 0.10})

    # VIX
    vix = macro_tickers.get("^VIX", {}).get("current_price")
    if vix is not None:
        if vix > 30:
            drivers.append({"factor": "vix_high", "value": vix, "direction": "bearish", "weight": 0.25})
        elif vix < 15:
            drivers.append({"factor": "vix_low", "value": vix, "direction": "bullish", "weight": 0.15})
        else:
            drivers.append({"factor": "vix_normal", "value": vix, "direction": "neutral", "weight": 0.05})

    # DXY (dollar strength)
    dxy = macro_tickers.get("DX-Y.NYB", {}).get("current_price")
    if dxy is not None:
        if dxy > 105:
            drivers.append({"factor": "dxy_strong", "value": dxy, "direction": "bearish", "weight": 0.15})
        elif dxy < 100:
            drivers.append({"factor": "dxy_weak", "value": dxy, "direction": "bullish", "weight": 0.10})

    # Gold (risk-off hedge)
    gold = macro_tickers.get("GC=F", {}).get("current_price")
    if gold is not None:
        change = macro_tickers.get("GC=F", {}).get("change_pct", 0)
        if change > 1.0:
            drivers.append({"factor": "gold_rising", "value": gold, "change_pct": change,
                            "direction": "bearish", "weight": 0.10})

    # Oil (inflation proxy)
    oil = macro_tickers.get("CL=F", {}).get("current_price")
    if oil is not None:
        change = macro_tickers.get("CL=F", {}).get("change_pct", 0)
        if change > 3.0:
            drivers.append({"factor": "oil_spike", "value": oil, "change_pct": change,
                            "direction": "bearish", "weight": 0.10})

    # Fed Funds (from FRED if available)
    if "FEDFUNDS" in fred_data and "error" not in fred_data["FEDFUNDS"]:
        ff = fred_data["FEDFUNDS"]["latest"]
        if ff > 5.0:
            drivers.append({"factor": "fed_funds_high", "value": ff, "direction": "bearish", "weight": 0.15})

    # Aggregate regime
    bullish = sum(d["weight"] for d in drivers if d["direction"] == "bullish")
    bearish = sum(d["weight"] for d in drivers if d["direction"] == "bearish")
    net = bullish - bearish

    if net > 0.2:
        regime = "risk-on"
    elif net < -0.2:
        regime = "risk-off"
    else:
        regime = "neutral"

    return {
        "regime": regime,
        "net_score": round(net, 3),
        "bullish_score": round(bullish, 3),
        "bearish_score": round(bearish, 3),
        "drivers": drivers,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--series", nargs="*", default=None, help="Specific FRED series IDs")
    parser.add_argument("--yield-curve", action="store_true", help="Yield curve analysis only")
    parser.add_argument("--observation-limit", type=int, default=5)
    args = parser.parse_args()

    if args.yield_curve:
        # Fetch just the yield tickers via Yahoo
        try:
            sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
            from fetch_data import fetch_yahoo_chart
            macro_tickers = {}
            for sym in ["^TNX", "^IRX"]:
                d = fetch_yahoo_chart(sym, range="5d")
                if "error" not in d:
                    macro_tickers[sym] = {
                        "current_price": d["current_price"],
                        "change_pct": d["change_pct"],
                    }
            print(json.dumps(yield_curve_analysis(macro_tickers), indent=2))
        except Exception as e:
            print(json.dumps({"error": str(e)}, indent=2))
        return

    series_ids = args.series or ["FEDFUNDS", "DGS10", "DGS2", "T10Y2Y",
                                  "CPIAUCSL", "GDP", "UNRATE"]

    print(f"Fetching {len(series_ids)} FRED series...", file=sys.stderr)
    fred_data = fetch_fred_series(series_ids, args.observation_limit)

    print(json.dumps({
        "timestamp": datetime.now().isoformat(),
        "fred": fred_data,
    }, indent=2))


if __name__ == "__main__":
    main()
