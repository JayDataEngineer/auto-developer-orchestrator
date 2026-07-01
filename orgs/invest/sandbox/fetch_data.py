"""
Market data fetcher — runs inside the sandbox.
Fetches stock + crypto prices, technical indicators, and FUNDAMENTALS.

Default behavior: writes JSON to data/market_data.json (auto-discovered via paths.py).
Progress messages go to stderr, JSON goes to the file.

Usage:
  python3 fetch_data.py                                     # writes to data/market_data.json
  python3 fetch_data.py --output /tmp/market.json           # custom output path
  python3 fetch_data.py --stdout                            # print JSON to stdout (legacy)
"""

import json
import sys
import os
import argparse
import tempfile
import urllib.request
import urllib.error
from datetime import datetime, timedelta

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import paths  # noqa: E402

# Third-party deps imported lazily so --help works in a bare venv (System A contract).
yf = None  # type: ignore[assignment]


def _ensure_yfinance():
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


def fetch_yahoo_chart(symbol: str, range: str = "3mo", interval: str = "1d"):
    """Fetch price history from Yahoo Finance chart API (no auth needed)."""
    url = (
        f"https://query1.finance.yahoo.com/v8/finance/chart/{symbol}"
        f"?range={range}&interval={interval}&includePrePost=false"
    )
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            data = json.loads(resp.read())
        result = data.get("chart", {}).get("result", [{}])[0]
        meta = result.get("meta", {})
        timestamps = result.get("timestamp", [])
        quotes = result.get("indicators", {}).get("quote", [{}])[0]

        closes = [c for c in quotes.get("close", []) if c is not None]
        volumes = [v for v in quotes.get("volume", []) if v is not None]

        current_price = meta.get("regularMarketPrice") or (closes[-1] if closes else 0)
        prev_close = meta.get("previousClose") or (closes[-2] if len(closes) >= 2 else current_price)
        change_pct = round(
            ((current_price - prev_close) / max(prev_close, 0.01)) * 100, 2
        ) if prev_close else 0

        return {
            "symbol": symbol,
            "name": meta.get("shortName", symbol),
            "currency": meta.get("currency", "USD"),
            "current_price": current_price,
            "previous_close": prev_close,
            "change_pct": change_pct,
            "volume": volumes[-1] if volumes else 0,
            "high_52w": meta.get("fiftyTwoWeekHigh", 0),
            "low_52w": meta.get("fiftyTwoWeekLow", 0),
            "prices": closes[-90:],  # last 90 days
            "timestamps": timestamps[-90:],
        }
    except Exception as e:
        return {"symbol": symbol, "error": str(e)}


def fetch_fundamentals(symbol: str):
    """Fetch comprehensive fundamentals from Yahoo Finance via yfinance.

    Returns: valuation, profitability, growth, balance sheet, cash flow,
    analyst consensus, and quarterly income statement.
    """
    _ensure_yfinance()
    try:
        ticker = yf.Ticker(symbol)
        info = ticker.info

        def _safe(key, default=None):
            v = info.get(key, default)
            if isinstance(v, (int, float)):
                return v
            return default

        # --- Valuation ---
        valuation = {}
        for k in ["marketCap", "enterpriseValue", "trailingPE", "forwardPE",
                   "pegRatio", "priceToBook", "enterpriseToRevenue",
                   "enterpriseToEbitda", "trailingEps", "forwardEps",
                   "bookValue", "dividendYield", "payoutRatio"]:
            v = _safe(k)
            if v is not None:
                valuation[k] = v

        # --- Profitability ---
        profitability = {}
        for k in ["profitMargins", "operatingMargins", "returnOnEquity",
                   "returnOnAssets", "grossProfits"]:
            v = _safe(k)
            if v is not None:
                profitability[k] = v

        # --- Growth ---
        growth = {}
        for k in ["revenueGrowth", "earningsGrowth",
                   "earningsQuarterlyGrowth", "revenueQuarterlyGrowth"]:
            v = _safe(k)
            if v is not None:
                growth[k] = v

        # --- Balance sheet ---
        balance_sheet = {}
        for k in ["totalDebt", "totalCash", "debtToEquity", "currentRatio",
                   "quickRatio", "freeCashflow", "operatingCashflow",
                   "totalRevenue", "netIncomeToCommon"]:
            v = _safe(k)
            if v is not None:
                balance_sheet[k] = v

        # --- Analyst consensus ---
        analysts = {}
        for k in ["recommendationKey", "numberOfAnalystOpinions",
                   "targetMeanPrice", "targetHighPrice", "targetLowPrice"]:
            v = _safe(k)
            if v is not None:
                analysts[k] = v
        # Add upside % if we have target and current price
        target = _safe("targetMeanPrice")
        if target and info.get("currentPrice"):
            analysts["upside_pct"] = round(
                ((target - info["currentPrice"]) / info["currentPrice"]) * 100, 2
            )

        # --- Ownership / risk ---
        ownership = {}
        for k in ["heldPercentInsiders", "heldPercentInstitutions",
                   "shortRatio", "shortPercentOfFloat", "beta"]:
            v = _safe(k)
            if v is not None:
                ownership[k] = v

        # --- Sector/Industry ---
        sector = info.get("sector", "")
        industry = info.get("industry", "")

        # --- Quarterly income (last 4 quarters, compact) ---
        quarterly_income = []
        try:
            inc = ticker.quarterly_income_stmt
            if inc is not None and not inc.empty:
                for col in list(inc.columns)[:4]:
                    q = {"quarter": str(col.date())}
                    has_data = False
                    for row in ["Total Revenue", "Gross Profit", "Operating Income",
                                "Net Income", "Diluted EPS", "Research And Development",
                                "Cost Of Revenue"]:
                        val = inc.loc[row, col] if row in inc.index else None
                        if val is not None:
                            try:
                                fv = float(val)
                                if fv == fv:  # not NaN
                                    q[row] = fv
                                    has_data = True
                            except (ValueError, TypeError):
                                pass
                    if has_data:
                        quarterly_income.append(q)
        except Exception:
            pass

        # --- Annual revenue/earnings (last 3 years) ---
        annual = []
        try:
            inc_y = ticker.income_stmt
            if inc_y is not None and not inc_y.empty:
                for col in list(inc_y.columns)[:3]:
                    y = {"year": str(col.year) if hasattr(col, 'year') else str(col)}
                    for row in ["Total Revenue", "Net Income", "Gross Profit"]:
                        val = inc_y.loc[row, col] if row in inc_y.index else None
                        if val is not None:
                            y[row] = float(val)
                    annual.append(y)
        except Exception:
            pass

        return {
            "sector": sector,
            "industry": industry,
            "valuation": valuation,
            "profitability": profitability,
            "growth": growth,
            "balance_sheet": balance_sheet,
            "analysts": analysts,
            "ownership": ownership,
            "quarterly_income": quarterly_income,
            "annual": annual,
        }
    except Exception as e:
        return {"error": str(e)}


def fetch_coingecko(coin_id: str, days: int = 90):
    """Fetch crypto price history from CoinGecko (free, no auth)."""
    url = (
        f"https://api.coingecko.com/api/v3/coins/{coin_id}/market_chart"
        f"?vs_currency=usd&days={days}&interval=daily"
    )
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urllib.request.urlopen(req, timeout=15) as resp:
            data = json.loads(resp.read())

        prices = [p[1] for p in data.get("prices", [])]
        volumes = [v[1] for v in data.get("total_volumes", [])]

        current = prices[-1] if prices else 0
        prev = prices[-2] if len(prices) >= 2 else current

        return {
            "symbol": coin_id.upper(),
            "name": coin_id.capitalize(),
            "currency": "USD",
            "current_price": round(current, 2),
            "previous_close": round(prev, 2),
            "change_pct": round(((current - prev) / max(prev, 0.01)) * 100, 2),
            "volume": volumes[-1] if volumes else 0,
            "prices": [round(p, 2) for p in prices[-90:]],
        }
    except Exception as e:
        return {"symbol": coin_id, "error": str(e)}


def calc_rsi(prices, period=14):
    """Calculate RSI from price list."""
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
    """Exponential moving average."""
    if len(prices) < period:
        return prices[-1] if prices else 0
    k = 2 / (period + 1)
    ema = prices[0]
    for p in prices[1:]:
        ema = p * k + ema * (1 - k)
    return round(ema, 2)


def calc_bollinger(prices, period=20, num_std=2):
    """Bollinger Bands."""
    if len(prices) < period:
        return {}
    recent = prices[-period:]
    mid = sum(recent) / period
    var = sum((p - mid) ** 2 for p in recent) / period
    std = var ** 0.5
    return {
        "upper": round(mid + std * num_std, 2),
        "middle": round(mid, 2),
        "lower": round(mid - std * num_std, 2),
    }


def add_indicators(asset):
    """Add technical indicators to an asset dict."""
    prices = asset.get("prices", [])
    if not prices or len(prices) < 5:
        asset["indicators"] = {}
        return asset

    asset["indicators"] = {
        "rsi_14": calc_rsi(prices),
        "ema_12": calc_ema(prices, 12),
        "ema_26": calc_ema(prices, 26),
        "sma_20": round(sum(prices[-20:]) / min(len(prices), 20), 2),
        "sma_50": round(sum(prices[-50:]) / min(len(prices), 50), 2) if len(prices) >= 20 else None,
        "bollinger": calc_bollinger(prices),
    }

    # MACD approximation
    ema12 = calc_ema(prices, 12)
    ema26 = calc_ema(prices, 26)
    asset["indicators"]["macd"] = round(ema12 - ema26, 4)

    # Trend signals
    rsi = asset["indicators"]["rsi_14"]
    current = asset["current_price"]
    bb = asset["indicators"]["bollinger"]

    signals = []
    if rsi > 70:
        signals.append("RSI overbought (>70)")
    elif rsi < 30:
        signals.append("RSI oversold (<30)")

    if bb and current > bb.get("upper", 999999):
        signals.append("Price above upper Bollinger Band")
    elif bb and current < bb.get("lower", 0):
        signals.append("Price below lower Bollinger Band")

    if len(prices) >= 20:
        sma20 = asset["indicators"]["sma_20"]
        if current > sma20:
            signals.append("Price above SMA20 (bullish)")
        else:
            signals.append("Price below SMA20 (bearish)")

    if ema12 > ema26:
        signals.append("EMA12 > EMA26 (bullish crossover)")
    else:
        signals.append("EMA12 < EMA26 (bearish crossover)")

    asset["signals"] = signals
    return asset


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[1] if __doc__ else None)
    parser.add_argument("--watchlist", default=None, help="Path to watchlist.json (default: built-in multi-asset list)")
    parser.add_argument("--history-days", type=int, default=90)
    parser.add_argument("--skip-macro", action="store_true", help="Skip macro tickers (FRED + commodities)")
    parser.add_argument("--crypto-only", action="store_true",
                        help="Fetch only crypto assets (stocks + macro skipped)")
    parser.add_argument("--stocks-only", action="store_true",
                        help="Fetch only stocks (crypto + macro skipped)")
    parser.add_argument("--output", default=None,
                        help=f"Write JSON to this path (default: {paths.MARKET_DATA_FILE})")
    parser.add_argument("--stdout", action="store_true",
                        help="Print JSON to stdout instead of writing to file (legacy mode)")
    args = parser.parse_args()

    if args.crypto_only and args.stocks_only:
        parser.error("--crypto-only and --stocks-only are mutually exclusive")

    # Default watchlist — multi-asset
    stocks = ["AAPL", "MSFT", "GOOGL", "NVDA", "TSLA", "AMZN", "META"]
    crypto = ["bitcoin", "ethereum", "solana"]
    macro_tickers = ["^TNX", "^IRX", "^VIX", "DX-Y.NYB", "GC=F", "CL=F"]
    fred_series = []

    if args.watchlist:
        with open(args.watchlist) as f:
            wl = json.load(f)
        watchlist = wl.get("watchlist", wl)  # support both formats
        stocks = [s["symbol"] for s in watchlist.get("stocks", []) if s.get("enabled")]
        crypto = [c["id"] for c in watchlist.get("crypto", []) if c.get("enabled")]
        macro_raw = watchlist.get("macro_tickers", [])
        macro_tickers = [m["symbol"] if isinstance(m, dict) else m for m in macro_raw]
        fred_raw = watchlist.get("fred_series", [])
        fred_series = [f["id"] if isinstance(f, dict) else f for f in fred_raw]

    range_str = f"{args.history_days}d"
    results = {"timestamp": datetime.now().isoformat(), "assets": [], "macro": {}}

    # Mode flags short-circuit unrelated asset classes
    do_stocks = not args.crypto_only
    do_crypto = not args.stocks_only
    do_macro = not args.skip_macro and not args.crypto_only and not args.stocks_only

    scope_parts = []
    if do_stocks:
        scope_parts.append(f"{len(stocks)} stocks")
    if do_crypto:
        scope_parts.append(f"{len(crypto)} crypto")
    if do_macro:
        scope_parts.append(f"{len(macro_tickers)} macro tickers")
    print(f"Fetching {' + '.join(scope_parts)}...", file=sys.stderr)

    if do_stocks:
        for sym in stocks:
            data = fetch_yahoo_chart(sym, range=range_str)
            if "error" not in data:
                add_indicators(data)
                data["asset_class"] = "stock"
                fund = fetch_fundamentals(sym)
                if "error" not in fund:
                    data["fundamentals"] = fund
            results["assets"].append(data)

    if do_crypto:
        for coin in crypto:
            data = fetch_coingecko(coin, days=args.history_days)
            if "error" not in data:
                add_indicators(data)
                data["asset_class"] = "crypto"
            results["assets"].append(data)

    # Market indices + macro tickers
    if do_macro:
        print("Fetching macro tickers + indices...", file=sys.stderr)
        for ticker in macro_tickers + ["^GSPC", "^IXIC", "^VIX"]:
            idx_data = fetch_yahoo_chart(ticker, range="5d")
            if "error" not in idx_data:
                results["macro"][ticker] = {
                    "current_price": idx_data["current_price"],
                    "change_pct": idx_data["change_pct"],
                    "previous_close": idx_data["previous_close"],
                }

        # FRED series (if API key set)
        if fred_series:
            try:
                sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
                from macro import fetch_fred_series  # type: ignore
                fred_data = fetch_fred_series(fred_series)
                results["macro"]["fred"] = fred_data
            except Exception as e:
                print(f"FRED fetch skipped: {e}", file=sys.stderr)

        # Yield curve spread
        if "^TNX" in results["macro"] and "^IRX" in results["macro"]:
            tnx = results["macro"]["^TNX"]["current_price"]
            irx = results["macro"]["^IRX"]["current_price"]
            results["macro"]["yield_curve_2y_10y"] = round(tnx - irx, 3)
            results["macro"]["yield_curve_inverted"] = tnx < irx

    # Summary stats
    errors = [a for a in results["assets"] if "error" in a]
    ok = [a for a in results["assets"] if "error" not in a]
    print(f"Fetched {len(ok)} assets, {len(errors)} errors, {len(results['macro'])} macro indicators", file=sys.stderr)

    # Strip raw prices to keep output compact (keep last 30 for indicators)
    for a in ok:
        a["prices"] = a.get("prices", [])[-30:]

    payload = json.dumps(results, indent=2)

    # Default: write to file (atomic). --stdout forces legacy behavior.
    if args.stdout:
        print(payload)
        return

    output_path = args.output or paths.MARKET_DATA_FILE
    out_dir = os.path.dirname(output_path) or "."
    os.makedirs(out_dir, exist_ok=True)

    tmp = tempfile.NamedTemporaryFile(
        "w", suffix=".json", delete=False, dir=out_dir)
    try:
        tmp.write(payload)
        tmp.close()
        os.replace(tmp.name, output_path)
    except Exception:
        if os.path.exists(tmp.name):
            os.unlink(tmp.name)
        raise

    print(f"Wrote {len(payload):,} bytes → {output_path}", file=sys.stderr)
    print(f"OK: {len(ok)} assets + {len(results['macro'])} macro entries", file=sys.stderr)


if __name__ == "__main__":
    main()
