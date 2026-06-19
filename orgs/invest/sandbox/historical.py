#!/usr/bin/env python3
"""
historical.py — Layer 7: Historical Multi-Day Simulation

Replays the full 6-layer pipeline across N trading days.
Two modes:
  - Rule-based (default): signals.py drives all decisions
  - AI mode (--ai): LLM generates signals from daily snapshots

Simulates: scoring → regime detection → position sizing → stop management → P&L

CLI:
  python3 historical.py run [--months 3] [--tickers AAPL,MSFT,...] [--ai] [--api URL]
  python3 historical.py report
  python3 historical.py compare
"""

import json
import math
import os
import sys
import tempfile
from datetime import datetime, timedelta

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import signals
import regime
import walkforward

# ── Config ─────────────────────────────────────────────────────────

DEFAULT_CONFIG = {
    "starting_capital": 100000.0,
    "transaction_cost_pct": 0.001,  # 0.1%
    "min_composite_buy": 55,
    "max_composite_sell": 40,
    "min_composite_strong": 70,
    "max_positions_default": 5,
    "atr_stop_mult": 2.0,
    "atr_tp_mult": 3.0,
    "position_pct": 0.10,  # 10% of equity per position
    "tickers": ["AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "TSLA", "META"],
    "months": 3,
}

RESULTS_FILE = os.environ.get(
    "HISTORICAL_RESULTS", "/sandbox/historical_results.json")

AI_DEFAULT_API = "http://localhost:8001"
AI_MODEL = os.environ.get("AI_MODEL", "gemma-4-26b")
AI_PROMPT = """Analyze these stocks and return a JSON array with signals.

Actions: strong_buy, buy, hold, sell, strong_sell. Confidence: 0.0-1.0, trade only if >= 0.6.
Regime: {regime} ({bias} bias, {size_mult}x sizing). Consider RSI, MA crossovers, momentum.

{data}

Return ONLY a JSON array:
[{{"symbol":"AAPL","action":"buy","confidence":0.75,"reasoning":"..."}}]"""


# ── Data Helpers ────────────────────────────────────────────────────


def fetch_all_history(tickers, months):
    """Fetch OHLCV for all tickers + SPY. Returns {ticker: hist_dict}.

    Fetches extra history (6 months min) for regime detection even on short sims.
    """
    import yfinance as yf
    fetch_months = max(months, 6)  # Always fetch 6mo for regime detection
    period = f"{fetch_months}mo"
    result = {}
    all_tickers = list(tickers) + ["SPY"]
    for t in all_tickers:
        df = yf.Ticker(t).history(period=period)
        if df.empty:
            continue
        result[t] = {
            "dates":  [d.strftime("%Y-%m-%d") for d in df.index],
            "close":  df["Close"].tolist(),
            "high":   df["High"].tolist(),
            "low":    df["Low"].tolist(),
            "volume": df["Volume"].tolist(),
        }
    return result


def slice_asof(data, target_date):
    """Return data dict with only bars on or before target_date.

    Returns a new dict with same keys, truncated arrays.
    """
    dates = data.get("dates", [])
    cut = 0
    for i, d in enumerate(dates):
        if d <= target_date:
            cut = i + 1
        else:
            break
    if cut == 0:
        return None
    return {
        "dates":  dates[:cut],
        "close":  data["close"][:cut],
        "high":   data["high"][:cut],
        "low":    data["low"][:cut],
        "volume": data["volume"][:cut],
    }


# ── Portfolio State ────────────────────────────────────────────────


def init_portfolio(capital):
    """Create empty portfolio."""
    return {
        "cash": capital,
        "positions": {},
        "trades": [],
        "equity_curve": [],
        "regime_history": [],
    }


def portfolio_value(portfolio, current_prices):
    """Mark-to-market: cash + sum of positions at current prices."""
    val = portfolio["cash"]
    for ticker, pos in portfolio["positions"].items():
        price = current_prices.get(ticker, pos["entry_price"])
        val += pos["shares"] * price
    return val


# ── Simulation Core ────────────────────────────────────────────────


def compute_atr(highs, lows, closes, period=14):
    """ATR from price arrays."""
    if len(closes) < period + 1:
        return None
    trs = []
    for i in range(1, len(closes)):
        tr = max(
            highs[i] - lows[i],
            abs(highs[i] - closes[i - 1]),
            abs(lows[i] - closes[i - 1]),
        )
        trs.append(tr)
    if len(trs) < period:
        return sum(trs) / len(trs) if trs else None
    return sum(trs[-period:]) / period


def position_size(equity, price, regime_params, config):
    """Calculate shares to buy based on equity and regime."""
    pct = config.get("position_pct", 0.10)
    mult = regime_params.get("position_size_mult", 1.0)
    dollar = equity * pct * mult
    if price <= 0:
        return 0
    return max(1, int(dollar / price))


def execute_signal(portfolio, ticker, action, price, date, regime_params, config):
    """Execute a buy or sell signal."""
    cost_pct = config.get("transaction_cost_pct", 0.001)

    if action in ("strong_buy", "buy"):
        if ticker in portfolio["positions"]:
            return  # Already holding
        equity = portfolio_value(portfolio, {ticker: price})
        shares = position_size(equity, price, regime_params, config)
        if shares <= 0:
            return
        # Check max positions
        max_pos = regime_params.get("max_positions", config.get("max_positions_default", 5))
        if len(portfolio["positions"]) >= max_pos:
            return
        cost = shares * price * (1 + cost_pct)
        if cost > portfolio["cash"]:
            shares = int(portfolio["cash"] / (price * (1 + cost_pct)))
            if shares <= 0:
                return
            cost = shares * price * (1 + cost_pct)
        atr = regime_params.get("stop_atr_mult", config.get("atr_stop_mult", 2.0))
        tp_mult = config.get("atr_tp_mult", 3.0)
        portfolio["positions"][ticker] = {
            "shares": shares,
            "entry_price": price,
            "entry_date": date,
        }
        portfolio["cash"] -= cost
        portfolio["trades"].append({
            "date": date, "ticker": ticker, "action": "buy",
            "shares": shares, "price": price, "pnl": 0,
        })

    elif action in ("strong_sell", "sell"):
        if ticker not in portfolio["positions"]:
            return
        pos = portfolio["positions"][ticker]
        proceeds = pos["shares"] * price * (1 - cost_pct)
        pnl = (price - pos["entry_price"]) * pos["shares"] - (
            pos["shares"] * price * cost_pct * 2)
        portfolio["cash"] += proceeds
        portfolio["trades"].append({
            "date": date, "ticker": ticker, "action": "sell",
            "shares": pos["shares"], "price": round(price, 2),
            "pnl": round(pnl, 2), "reason": "signal",
        })
        del portfolio["positions"][ticker]


def check_stops(portfolio, current_prices, date, config):
    """Check stop-loss and take-profit for all positions."""
    cost_pct = config.get("transaction_cost_pct", 0.001)
    stop_mult = config.get("atr_stop_mult", 2.0)
    tp_mult = config.get("atr_tp_mult", 3.0)

    to_close = []
    for ticker, pos in portfolio["positions"].items():
        price = current_prices.get(ticker)
        if price is None:
            continue
        entry = pos["entry_price"]
        change_pct = (price - entry) / entry * 100 if entry else 0

        # Simple percentage-based stops
        stop_pct = stop_mult * 2.5  # ~5% default stop
        tp_pct = tp_mult * 2.5     # ~7.5% default take-profit

        if change_pct <= -stop_pct:
            to_close.append((ticker, "stop_loss"))
        elif change_pct >= tp_pct:
            to_close.append((ticker, "take_profit"))

    for ticker, reason in to_close:
        pos = portfolio["positions"][ticker]
        price = current_prices[ticker]
        proceeds = pos["shares"] * price * (1 - cost_pct)
        pnl = (price - pos["entry_price"]) * pos["shares"]
        portfolio["cash"] += proceeds
        portfolio["trades"].append({
            "date": date, "ticker": ticker, "action": "sell",
            "shares": pos["shares"], "price": round(price, 2),
            "pnl": round(pnl, 2), "reason": reason,
        })
        del portfolio["positions"][ticker]


def simulate_day(portfolio, date, all_data, regime_params, config):
    """Run one simulation day: score tickers + execute + check stops."""
    tickers = [t for t in config.get("tickers", []) if t in all_data]
    spy_data = all_data.get("SPY")
    current_prices = {}

    # Get current prices
    for ticker in tickers:
        sl = slice_asof(all_data[ticker], date)
        if sl and sl["close"]:
            current_prices[ticker] = sl["close"][-1]

    if spy_data:
        spy_sl = slice_asof(spy_data, date)
        if spy_sl and spy_sl["close"]:
            current_prices["SPY"] = spy_sl["close"][-1]

    # Check stops first (before new signals)
    check_stops(portfolio, current_prices, date, config)

    # Score each ticker
    for ticker in tickers:
        sl = slice_asof(all_data[ticker], date)
        if not sl or len(sl["close"]) < 20:
            continue
        prices = sl["close"]
        volumes = sl["volume"]
        price = prices[-1]
        indicators = walkforward.compute_indicators(prices)

        # Build asset dict for signals.py
        asset = {
            "symbol": ticker,
            "current_price": price,
            "previous_close": prices[-2] if len(prices) > 1 else price,
            "change_pct": ((prices[-1] - prices[-2]) / prices[-2] * 100)
                          if len(prices) > 1 else 0.0,
            "volume": volumes[-1] if volumes else 0,
            "prices": prices,
            "indicators": indicators,
            "_market_vix": 18.0,  # Approximate for historical
        }

        # Score
        scored = signals.score_ticker(asset, signals.DEFAULT_CONFIG)
        action = scored["action_signal"]
        composite = scored["composite_score"]

        # Apply regime filter
        bias = regime_params.get("bias", "neutral")
        min_buy = config.get("min_composite_buy", 55)
        max_sell = config.get("max_composite_sell", 40)

        # Regime: suppress weak signals against the regime
        if bias == "short" and action in ("buy", "strong_buy") and composite < 70:
            action = "hold"
        elif bias == "long" and action in ("sell", "strong_sell") and composite > 30:
            action = "hold"

        # Only trade above confidence threshold
        if action in ("buy", "strong_buy") and composite < min_buy:
            action = "hold"
        elif action in ("sell", "strong_sell") and composite > max_sell:
            action = "hold"

        execute_signal(
            portfolio, ticker, action, price, date, regime_params, config)


# ── AI Signal Generation ────────────────────────────────────────────


def build_snapshot_for_ai(date, all_data, tickers, regime_name, regime_params):
    """Build a concise market summary for the LLM."""
    lines = []
    for ticker in tickers:
        sl = slice_asof(all_data.get(ticker, {}), date)
        if not sl or len(sl["close"]) < 20:
            continue
        prices = sl["close"]
        price = prices[-1]
        change = ((prices[-1] - prices[-2]) / prices[-2] * 100
                  ) if len(prices) > 1 else 0
        indicators = walkforward.compute_indicators(prices)
        rsi = indicators["rsi_14"]
        ema12, ema26 = indicators["ema_12"], indicators["ema_26"]
        bb = indicators["bollinger"]

        # Get fundamentals from signals if available
        asset = {
            "symbol": ticker, "current_price": price,
            "previous_close": prices[-2] if len(prices) > 1 else price,
            "change_pct": change, "volume": sl["volume"][-1] if sl["volume"] else 0,
            "prices": prices, "indicators": indicators, "_market_vix": 18.0,
        }
        scored = signals.score_ticker(asset, signals.DEFAULT_CONFIG)

        lines.append(
            f"{ticker}: ${price:.2f} ({change:+.2f}%) | "
            f"RSI={rsi:.1f} | EMA12={ema12:.2f} EMA26={ema26:.2f} | "
            f"BB: {bb.get('lower', 0):.1f}-{bb.get('upper', 0):.1f} | "
            f"Composite={scored['composite_score']:.1f} ({scored['action_signal']})"
        )

    # Also add regime summary
    spy_sl = slice_asof(all_data.get("SPY", {}), date)
    spy_info = ""
    if spy_sl and spy_sl["close"]:
        spy_price = spy_sl["close"][-1]
        spy_change = 0
        if len(spy_sl["close"]) > 1:
            spy_change = ((spy_sl["close"][-1] - spy_sl["close"][-2])
                          / spy_sl["close"][-2] * 100)
        spy_info = f"\nSPY: ${spy_price:.2f} ({spy_change:+.2f}%)\n"
        spy_info += f"Regime: {regime_name.upper()}\n"

    return spy_info + "\n".join(lines)


def call_llm(prompt, api_url=None, model=None):
    """Call the OpenAI-compatible chat completions API."""
    import urllib.request
    api_url = api_url or AI_DEFAULT_API
    model = model or AI_MODEL
    url = f"{api_url}/v1/chat/completions"
    payload = json.dumps({
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0.3,
        "max_tokens": 2048,
    }).encode()
    req = urllib.request.Request(url, data=payload, headers={
        "Content-Type": "application/json",
    })
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            data = json.loads(resp.read())
        msg = data["choices"][0]["message"]
        content = msg.get("content", "")
        # If model used thinking and content is empty, extract from reasoning
        if not content.strip() and msg.get("reasoning_content"):
            reasoning = msg["reasoning_content"]
            # Look for JSON array in the reasoning
            start = reasoning.find("[")
            end = reasoning.rfind("]")
            if start != -1 and end > start:
                content = reasoning[start:end + 1]
        return content if content.strip() else None
    except Exception as e:
        print(f"  LLM error: {e}", file=sys.stderr)
        return None


def parse_ai_signals(text):
    """Extract JSON signal array from LLM response."""
    if not text:
        return []
    # Try to find JSON array in the response
    text = text.strip()
    # Strip markdown code fences
    if text.startswith("```"):
        lines = text.split("\n")
        text = "\n".join(lines[1:])
        if text.endswith("```"):
            text = text[:-3]
        text = text.strip()

    # Find the JSON array
    start = text.find("[")
    end = text.rfind("]")
    if start == -1 or end == -1 or end <= start:
        return []

    try:
        parsed = json.loads(text[start:end + 1])
        if isinstance(parsed, list):
            return parsed
    except json.JSONDecodeError:
        # Try fixing common issues
        chunk = text[start:end + 1]
        # Remove trailing commas before ] or }
        import re
        chunk = re.sub(r',\s*([}\]])', r'\1', chunk)
        try:
            parsed = json.loads(chunk)
            if isinstance(parsed, list):
                return parsed
        except json.JSONDecodeError:
            pass

    return []


def simulate_day_ai(portfolio, date, all_data, regime_name, regime_params,
                    config, api_url=None):
    """Run one simulation day using AI-generated signals."""
    tickers = [t for t in config.get("tickers", []) if t in all_data]
    current_prices = {}

    for ticker in tickers:
        sl = slice_asof(all_data[ticker], date)
        if sl and sl["close"]:
            current_prices[ticker] = sl["close"][-1]

    # Check stops first
    check_stops(portfolio, current_prices, date, config)

    # Build snapshot and call LLM
    snapshot = build_snapshot_for_ai(
        date, all_data, tickers, regime_name, regime_params)
    prompt = AI_PROMPT.format(
        regime=regime_name,
        bias=regime_params.get("bias", "neutral"),
        size_mult=regime_params.get("position_size_mult", 1.0),
        data=snapshot,
    )

    response = call_llm(prompt, api_url=api_url)
    ai_signals = parse_ai_signals(response)

    if not ai_signals:
        # Fallback: no trades if LLM fails
        return

    # Build lookup from AI signals
    signal_map = {}
    for s in ai_signals:
        sym = s.get("symbol", s.get("ticker", "")).upper()
        action = s.get("action", "hold").lower()
        conf = float(s.get("confidence", 0))
        signal_map[sym] = {"action": action, "confidence": conf}

    # Execute AI signals
    bias = regime_params.get("bias", "neutral")
    for ticker in tickers:
        price = current_prices.get(ticker)
        if price is None:
            continue

        sig = signal_map.get(ticker)
        if not sig:
            continue

        action = sig["action"]
        conf = sig["confidence"]

        # Apply regime filter
        if bias == "short" and action in ("buy", "strong_buy") and conf < 0.8:
            action = "hold"
        elif bias == "long" and action in ("sell", "strong_sell") and conf > 0.3:
            action = "hold"

        # Confidence threshold
        if action in ("buy", "strong_buy") and conf < 0.6:
            action = "hold"
        elif action in ("sell", "strong_sell") and conf < 0.6:
            action = "hold"

        execute_signal(
            portfolio, ticker, action, price, date, regime_params, config)


# ── Full Simulation ────────────────────────────────────────────────


def run_simulation(config, use_ai=False, api_url=None):
    """Run full multi-day simulation. Returns (portfolio, report)."""
    months = config.get("months", 3)
    tickers = config.get("tickers", DEFAULT_CONFIG["tickers"])
    capital = config.get("starting_capital", 100000.0)
    mode_str = "AI" if use_ai else "rule-based"
    step_fn = simulate_day_ai if use_ai else simulate_day

    print(f"Fetching {months} months of data for {len(tickers)} tickers + SPY...",
          file=sys.stderr)
    all_data = fetch_all_history(tickers, months)

    spy_data = all_data.get("SPY")
    if not spy_data:
        print("ERROR: No SPY data fetched", file=sys.stderr)
        return None, None

    # Get trading dates from SPY (only the last N months)
    all_dates = spy_data["dates"]
    cutoff = datetime.now() - timedelta(days=months * 31)
    cutoff_str = cutoff.strftime("%Y-%m-%d")
    trading_dates = [d for d in all_dates if d >= cutoff_str]
    if not trading_dates:
        trading_dates = all_dates

    portfolio = init_portfolio(capital)
    regime_config = regime.DEFAULT_CONFIG
    regime_thresholds = regime_config.get("regime_thresholds",
                                           regime.DEFAULT_CONFIG["regime_thresholds"])
    regime_weights = regime_config.get("weights", regime.DEFAULT_CONFIG["weights"])
    strategy_config = regime_config.get("strategy", regime.DEFAULT_CONFIG["strategy"])

    print(f"Simulating {len(trading_dates)} trading days ({mode_str})...",
          file=sys.stderr)

    for i, date in enumerate(trading_dates):
        # Regime detection
        spy_sl = slice_asof(spy_data, date)
        regime_params = regime.get_regime_params("sideways", strategy_config)
        if spy_sl and len(spy_sl["close"]) >= 50:
            spy_prices = spy_sl["close"]
            trend_s, _ = regime.score_trend(spy_prices)
            vol_s, _ = regime.score_volatility(18.0, 0.0)  # Approximate VIX
            mom_s, _ = regime.score_momentum(spy_prices)

            # Breadth: check how many tickers above SMA50
            ticker_prices = {}
            for t in tickers:
                tsl = slice_asof(all_data.get(t, {}), date)
                if tsl and len(tsl["close"]) >= 50:
                    ticker_prices[t] = tsl["close"]
            breadth_s, _ = regime.score_breadth(ticker_prices)

            scores = {
                "trend": trend_s, "volatility": vol_s,
                "momentum": mom_s, "breadth": breadth_s,
            }
            regime_name, _, composite = regime.classify_regime(
                scores, regime_weights, regime_thresholds)
            regime_params = regime.get_regime_params(regime_name, strategy_config)

            portfolio["regime_history"].append({
                "date": date, "regime": regime_name,
                "composite": composite,
            })
        else:
            regime_name = "sideways"

        # Simulate this day
        if use_ai:
            simulate_day_ai(portfolio, date, all_data, regime_name,
                            regime_params, config, api_url=api_url)
        else:
            simulate_day(portfolio, date, all_data, regime_params, config)

        # Record equity
        current_prices = {}
        for ticker in tickers:
            sl = slice_asof(all_data.get(ticker, {}), date)
            if sl and sl["close"]:
                current_prices[ticker] = sl["close"][-1]
        if spy_sl and spy_sl["close"]:
            current_prices["SPY"] = spy_sl["close"][-1]

        eq = portfolio_value(portfolio, current_prices)
        portfolio["equity_curve"].append({"date": date, "equity": round(eq, 2)})

        if (i + 1) % 20 == 0:
            print(f"  Day {i+1}/{len(trading_dates)}: equity=${eq:,.0f} "
                  f"positions={len(portfolio['positions'])} "
                  f"regime={regime_name}",
                  file=sys.stderr)

    # Close all remaining positions at last prices
    last_date = trading_dates[-1] if trading_dates else ""
    last_prices = {}
    for ticker in tickers:
        sl = slice_asof(all_data.get(ticker, {}), last_date)
        if sl and sl["close"]:
            last_prices[ticker] = sl["close"][-1]

    for ticker in list(portfolio["positions"].keys()):
        price = last_prices.get(ticker)
        if price:
            pos = portfolio["positions"][ticker]
            cost_pct = config.get("transaction_cost_pct", 0.001)
            proceeds = pos["shares"] * price * (1 - cost_pct)
            pnl = (price - pos["entry_price"]) * pos["shares"]
            portfolio["cash"] += proceeds
            portfolio["trades"].append({
                "date": last_date, "ticker": ticker, "action": "sell",
                "shares": pos["shares"], "price": round(price, 2),
                "pnl": round(pnl, 2), "reason": "liquidation",
            })
            del portfolio["positions"][ticker]

    report = generate_report(portfolio, all_data, config)
    return portfolio, report


# ── Performance Analysis ───────────────────────────────────────────


def compute_performance(equity_curve):
    """Compute return, Sharpe, max DD from equity curve."""
    if len(equity_curve) < 2:
        return {
            "total_return_pct": 0.0,
            "annualized_return_pct": 0.0,
            "max_drawdown_pct": 0.0,
            "sharpe_ratio": 0.0,
            "sortino_ratio": 0.0,
            "volatility_annualized_pct": 0.0,
        }

    equities = [e["equity"] for e in equity_curve]
    initial = equities[0]
    final = equities[-1]
    total_return = (final - initial) / initial * 100 if initial else 0

    # Daily returns
    daily_rets = []
    for i in range(1, len(equities)):
        if equities[i - 1] > 0:
            daily_rets.append(
                (equities[i] - equities[i - 1]) / equities[i - 1])
        else:
            daily_rets.append(0.0)

    n = len(daily_rets)
    mean_r = sum(daily_rets) / n if n else 0
    var_r = sum((r - mean_r) ** 2 for r in daily_rets) / (n - 1) if n > 1 else 0
    std_r = math.sqrt(var_r) if var_r > 0 else 0

    # Annualize (252 trading days)
    ann_return = ((1 + mean_r) ** 252 - 1) * 100 if mean_r else 0
    ann_vol = std_r * math.sqrt(252) * 100 if std_r else 0
    sharpe = (mean_r / std_r) * math.sqrt(252) if std_r > 0 else 0

    # Sortino (downside deviation only)
    neg_rets = [r for r in daily_rets if r < 0]
    down_var = sum(r ** 2 for r in neg_rets) / len(neg_rets) if neg_rets else 0
    down_std = math.sqrt(down_var) if down_var > 0 else 0
    sortino = (mean_r / down_std) * math.sqrt(252) if down_std > 0 else 0

    # Max drawdown
    peak = equities[0]
    mdd = 0.0
    for eq in equities:
        peak = max(peak, eq)
        dd = (peak - eq) / peak * 100 if peak else 0
        mdd = max(mdd, dd)

    return {
        "total_return_pct": round(total_return, 2),
        "annualized_return_pct": round(ann_return, 2),
        "max_drawdown_pct": round(mdd, 2),
        "sharpe_ratio": round(sharpe, 2),
        "sortino_ratio": round(sortino, 2),
        "volatility_annualized_pct": round(ann_vol, 2),
    }


def generate_report(portfolio, all_data, config):
    """Generate full performance report."""
    curve = portfolio["equity_curve"]
    trades = portfolio["trades"]
    perf = compute_performance(curve)

    # Trade stats
    buy_sells = [t for t in trades if t["action"] == "sell"]
    wins = [t for t in buy_sells if t.get("pnl", 0) > 0]
    losses = [t for t in buy_sells if t.get("pnl", 0) <= 0]
    total_pnl = sum(t.get("pnl", 0) for t in buy_sells)

    win_rate = len(wins) / len(buy_sells) * 100 if buy_sells else 0
    avg_win = (sum(t["pnl"] for t in wins) / len(wins)) if wins else 0
    avg_loss = (sum(t["pnl"] for t in losses) / len(losses)) if losses else 0

    gross_profit = sum(t["pnl"] for t in wins) if wins else 0
    gross_loss = abs(sum(t["pnl"] for t in losses)) if losses else 0
    profit_factor = gross_profit / gross_loss if gross_loss > 0 else 0

    # Benchmark (SPY buy-and-hold)
    benchmark_return = 0.0
    spy_data = all_data.get("SPY", {})
    spy_closes = spy_data.get("close", [])
    if len(spy_closes) >= 2:
        benchmark_return = round(
            (spy_closes[-1] - spy_closes[0]) / spy_closes[0] * 100, 2)

    # Regime distribution
    regime_dist = {}
    for r in portfolio.get("regime_history", []):
        reg = r.get("regime", "unknown")
        regime_dist[reg] = regime_dist.get(reg, 0) + 1

    # Per-ticker stats
    by_ticker = {}
    for t in buy_sells:
        tick = t["ticker"]
        by_ticker.setdefault(tick, {"trades": 0, "wins": 0, "pnl": 0.0})
        by_ticker[tick]["trades"] += 1
        by_ticker[tick]["pnl"] += t.get("pnl", 0)
        if t.get("pnl", 0) > 0:
            by_ticker[tick]["wins"] += 1
    for tick in by_ticker:
        d = by_ticker[tick]
        d["win_rate"] = round(d["wins"] / d["trades"] * 100, 1) if d["trades"] else 0

    dates = [e["date"] for e in curve]
    return {
        "period": {
            "start": dates[0] if dates else "",
            "end": dates[-1] if dates else "",
            "trading_days": len(curve),
        },
        "returns": {
            "total_return_pct": perf["total_return_pct"],
            "annualized_return_pct": perf["annualized_return_pct"],
            "benchmark_return_pct": benchmark_return,
            "excess_return_pct": round(
                perf["total_return_pct"] - benchmark_return, 2),
        },
        "risk": {
            "max_drawdown_pct": perf["max_drawdown_pct"],
            "sharpe_ratio": perf["sharpe_ratio"],
            "sortino_ratio": perf["sortino_ratio"],
            "volatility_annualized_pct": perf["volatility_annualized_pct"],
        },
        "trades": {
            "total_trades": len(trades),
            "round_trips": len(buy_sells),
            "win_rate_pct": round(win_rate, 1),
            "avg_win": round(avg_win, 2),
            "avg_loss": round(avg_loss, 2),
            "profit_factor": round(profit_factor, 2),
            "total_pnl": round(total_pnl, 2),
        },
        "regime_distribution": regime_dist,
        "by_ticker": by_ticker,
    }


# ── Save/Load ──────────────────────────────────────────────────────


def save_results(report, path=None):
    """Save report atomically."""
    path = path or RESULTS_FILE
    report["generated_at"] = datetime.now().isoformat()
    tmp = tempfile.NamedTemporaryFile(
        "w", suffix=".json", delete=False,
        dir=os.path.dirname(path) or ".")
    json.dump(report, tmp, indent=2)
    tmp.close()
    os.replace(tmp.name, path)


def load_results(path=None):
    """Load saved report."""
    path = path or RESULTS_FILE
    if not os.path.exists(path):
        return None
    with open(path) as f:
        return json.load(f)


# ── CLI Commands ───────────────────────────────────────────────────


def cmd_run(args, config):
    """Run full historical simulation."""
    months = args.months or config.get("months", 3)
    config["months"] = months
    if args.tickers:
        config["tickers"] = args.tickers.split(",")
    use_ai = getattr(args, "ai", False)
    api_url = getattr(args, "api", None)

    portfolio, report = run_simulation(config, use_ai=use_ai, api_url=api_url)
    if report is None:
        print("Simulation failed.")
        return

    save_results(report)

    # Display
    print(f"\n{'=' * 60}")
    print("HISTORICAL SIMULATION RESULTS")
    print(f"{'=' * 60}")
    p = report["period"]
    print(f"Period: {p['start']} to {p['end']} ({p['trading_days']} days)")

    r = report["returns"]
    print(f"\nReturns:")
    print(f"  Total:          {r['total_return_pct']:+.2f}%")
    print(f"  Annualized:     {r['annualized_return_pct']:+.2f}%")
    print(f"  Benchmark (SPY): {r['benchmark_return_pct']:+.2f}%")
    print(f"  Excess:         {r['excess_return_pct']:+.2f}%")

    rk = report["risk"]
    print(f"\nRisk:")
    print(f"  Max Drawdown:   {rk['max_drawdown_pct']:.2f}%")
    print(f"  Sharpe Ratio:   {rk['sharpe_ratio']:.2f}")
    print(f"  Sortino Ratio:  {rk['sortino_ratio']:.2f}")

    t = report["trades"]
    print(f"\nTrades:")
    print(f"  Total:          {t['total_trades']} ({t['round_trips']} round-trips)")
    print(f"  Win Rate:       {t['win_rate_pct']:.1f}%")
    print(f"  Avg Win:        ${t['avg_win']:+.2f}")
    print(f"  Avg Loss:       ${t['avg_loss']:+.2f}")
    print(f"  Profit Factor:  {t['profit_factor']:.2f}")
    print(f"  Total P&L:      ${t['total_pnl']:+,.2f}")

    rd = report["regime_distribution"]
    print(f"\nRegime Distribution:")
    for reg, days in sorted(rd.items(), key=lambda x: -x[1]):
        print(f"  {reg}: {days} days")

    bt = report["by_ticker"]
    if bt:
        print(f"\nBy Ticker:")
        print(f"  {'Ticker':8s} {'Trades':>6s} {'Wins':>5s} {'P&L':>10s}")
        for tick, d in sorted(bt.items(), key=lambda x: -x[1]["pnl"]):
            print(f"  {tick:8s} {d['trades']:6d} {d['win_rate']:4.0f}% "
                  f"${d['pnl']:+10,.2f}")

    print(f"\nSaved to {RESULTS_FILE}")


def cmd_report(args, config):
    """Display saved report."""
    report = load_results()
    if not report:
        print("No results. Run 'historical.py run' first.")
        return
    cmd_run.__wrapped__(args, config) if hasattr(cmd_run, '__wrapped__') else None
    # Reuse display logic
    p = report["period"]
    print(f"Period: {p['start']} to {p['end']} ({p['trading_days']} days)")
    r = report["returns"]
    print(f"Return: {r['total_return_pct']:+.2f}% (SPY: {r['benchmark_return_pct']:+.2f}%)")
    rk = report["risk"]
    print(f"Sharpe: {rk['sharpe_ratio']:.2f} | MDD: {rk['max_drawdown_pct']:.2f}%")
    t = report["trades"]
    print(f"Trades: {t['round_trips']} | Win: {t['win_rate_pct']:.1f}% | P&L: ${t['total_pnl']:+,.2f}")
    print(f"Generated: {report.get('generated_at', '?')}")


def cmd_compare(args, config):
    """Compare strategy results side-by-side."""
    report = load_results()
    if not report:
        print("No results to compare. Run 'historical.py run' first.")
        return
    r = report["returns"]
    print(f"{'=' * 50}")
    print(f"  Strategy vs Buy-and-Hold SPY")
    print(f"{'=' * 50}")
    print(f"  Strategy:  {r['total_return_pct']:+.2f}%")
    print(f"  SPY:       {r['benchmark_return_pct']:+.2f}%")
    print(f"  Excess:    {r['excess_return_pct']:+.2f}%")
    print(f"  Sharpe:    {report['risk']['sharpe_ratio']:.2f}")
    print(f"  Win Rate:  {report['trades']['win_rate_pct']:.1f}%")
    print(f"  PF:        {report['trades']['profit_factor']:.2f}")


# ── Main ───────────────────────────────────────────────────────────


def main():
    import argparse
    p = argparse.ArgumentParser(
        description="Historical Multi-Day Simulation (Layer 7)")
    sub = p.add_subparsers(dest="command")

    run_p = sub.add_parser("run")
    run_p.add_argument("--months", type=int, default=3)
    run_p.add_argument("--tickers", default=None)
    run_p.add_argument("--ai", action="store_true",
                       help="Use LLM for signal generation")
    run_p.add_argument("--api", default=None,
                       help="LLM API URL (default: http://localhost:8001)")

    sub.add_parser("report")
    sub.add_parser("compare")

    args = p.parse_args()
    config = DEFAULT_CONFIG.copy()

    {"run": cmd_run, "report": cmd_report,
     "compare": cmd_compare}.get(
        args.command, lambda *_: p.print_help())(args, config)


if __name__ == "__main__":
    main()
