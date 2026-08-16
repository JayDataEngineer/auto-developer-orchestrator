"""
Portfolio Risk Intelligence — ATR stops, Kelly sizing, risk checks.

Usage:
  python3 risk.py assess                              # full portfolio risk report
  python3 risk.py stops [--symbols AAPL MSFT]         # ATR stop/target prices
  python3 risk.py size --ticker AAPL --confidence 0.8 --price 180
  python3 risk.py check --ticker AAPL --shares 10 --price 180
  python3 risk.py orders                              # generate stop/target orders
"""

import json
import math
import os
import subprocess
import sys
import argparse
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import paths

RISK_CONFIG_FILE = paths.RISK_CONFIG
MARKET_DATA_FILE = paths.MARKET_DATA_FILE

DEFAULT_CONFIG = {
    "max_position_pct": 0.15,
    "max_portfolio_heat_pct": 0.06,
    "max_sector_pct": 0.40,
    "stop_atr_multiplier": 2.0,
    "target_atr_multiplier": 3.0,
    "trailing_activation_atr": 2.0,
    "trailing_distance_atr": 1.0,
    "min_risk_reward": 1.5,
    "atr_period": 14,
    "min_confidence": 0.6,
}

SECTOR_MAP = {
    "AAPL": "Technology", "MSFT": "Technology", "GOOGL": "Technology",
    "AMZN": "Consumer Cyclical", "NVDA": "Technology", "TSLA": "Consumer Cyclical",
    "META": "Technology",
    "BTC": "Crypto", "ETH": "Crypto", "SOL": "Crypto",
    "SPY": "ETF", "QQQ": "ETF", "VTI": "ETF", "BND": "ETF",
}


# ── Config ────────────────────────────────────────────────────────────


def load_config(path=None):
    """Load risk config, merging file overrides with defaults."""
    path = path or RISK_CONFIG_FILE
    config = DEFAULT_CONFIG.copy()
    if os.path.exists(path):
        try:
            with open(path) as f:
                overrides = json.load(f)
            config.update(overrides)
        except (json.JSONDecodeError, OSError):
            pass
    return config


# ── ATR Calculation ───────────────────────────────────────────────────


def calc_atr(highs, lows, closes, period=14):
    """Pure ATR calculation from price arrays. Returns ATR value or None.

    True Range = max(H-L, |H-prev_C|, |L-prev_C|)
    ATR = SMA of True Range over `period` bars.
    """
    if len(closes) < period + 1:
        return None
    trs = []
    for i in range(1, len(closes)):
        h, l, pc = highs[i], lows[i], closes[i - 1]
        tr = max(h - l, abs(h - pc), abs(l - pc))
        trs.append(tr)
    if len(trs) < period:
        return None
    return sum(trs[-period:]) / period


def kelly_fraction(win_rate, win_loss_ratio):
    """Compute full and half Kelly fractions.

    Kelly formula: f = (W*B - (1-W)) / B
      where W = win_rate, B = win_loss_ratio (avg win / avg loss).
    Half-Kelly is returned because full Kelly is too volatile for live use.

    Returns (0.0, 0.0) for invalid inputs (win_rate <= 0 or ratio <= 0).
    Callers must check for zero and apply a confidence-based fallback.
    """
    if win_rate <= 0 or win_loss_ratio <= 0:
        return 0.0, 0.0
    q = 1 - win_rate
    full = (win_loss_ratio * win_rate - q) / win_loss_ratio
    half = max(0.0, full * 0.5)
    return full, half


def risk_reward_ratio(target_mult, stop_mult):
    """Ratio of take-profit distance to stop-loss distance from ATR multiples.

    Returns 0.0 when stop_mult is zero (degenerate config). Same formula
    cmd_stops uses to decide whether a setup meets min_risk_reward.
    """
    if stop_mult <= 0:
        return 0.0
    return round(target_mult / stop_mult, 2)


def fetch_atr(symbol, period=14):
    """Fetch price history via yfinance and calculate ATR."""
    try:
        import yfinance as yf
        hist = yf.Ticker(symbol).history(period="3mo")
        if hist.empty or len(hist) < period + 1:
            return None
        highs = hist["High"].tolist()
        lows = hist["Low"].tolist()
        closes = hist["Close"].tolist()
        return calc_atr(highs, lows, closes, period)
    except Exception as e:
        print(f"  ATR fetch failed for {symbol}: {e}", file=sys.stderr)
        return None


def fetch_atr_batch(symbols, period=14):
    """Fetch ATR for multiple symbols. Returns {symbol: atr}."""
    result = {}
    for sym in symbols:
        atr = fetch_atr(sym, period)
        if atr is not None:
            result[sym] = atr
    return result


# ── Alpaca Helpers ────────────────────────────────────────────────────


def get_alpaca_client():
    """Get Alpaca TradingClient. Exits with code 2 if no keys configured."""
    import sys
    key = os.environ.get("ALPACA_API_KEY")
    secret = os.environ.get("ALPACA_SECRET_KEY")
    if not key or not secret:
        print("ERROR: ALPACA_API_KEY and ALPACA_SECRET_KEY must be set in env.", file=sys.stderr)
        sys.exit(2)
    from alpaca.trading.client import TradingClient
    return TradingClient(key, secret, paper=True)


def get_portfolio_positions(client):
    """Fetch all open positions. Returns list of dicts."""
    positions = client.get_all_positions()
    result = []
    for p in positions:
        result.append({
            "symbol": p.symbol,
            "shares": int(float(p.qty)),
            "avg_cost": round(float(p.avg_entry_price), 2),
            "current_price": round(float(p.current_price), 2),
            "market_value": round(float(p.market_value), 2),
            "unrealized_pnl": round(float(p.unrealized_pl), 2),
            "unrealized_pnl_pct": round(float(p.unrealized_plpc) * 100, 2),
        })
    return result


def get_account_equity(client):
    """Get current account equity."""
    return float(client.get_account().equity)


# ── Sector Mapping ────────────────────────────────────────────────────


def get_sector(symbol, market_data=None):
    """Get sector for a symbol."""
    if symbol in SECTOR_MAP:
        return SECTOR_MAP[symbol]
    if market_data:
        for item in market_data if isinstance(market_data, list) else []:
            if isinstance(item, dict) and item.get("symbol") == symbol:
                return item.get("sector", "Unknown")
    return "Unknown"


# ── Journal Stats Bridge ──────────────────────────────────────────────


def get_journal_stats(ticker=None):
    """Get accuracy stats from journal.py via subprocess."""
    try:
        cmd = [sys.executable, paths.sibling("journal.py"), "stats"]
        if ticker:
            cmd += ["--ticker", ticker]
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=10)
        if result.returncode != 0:
            return {"accuracy": 0.5, "avg_win_loss_ratio": 1.5}
        # Parse the text output for accuracy
        output = result.stdout
        for line in output.split("\n"):
            if line.startswith("Accuracy:"):
                # "Accuracy: 62.5% (20/32)"
                pct_str = line.split(":")[1].strip().split("%")[0]
                accuracy = float(pct_str) / 100.0
                return {"accuracy": accuracy, "avg_win_loss_ratio": 1.5}
        return {"accuracy": 0.5, "avg_win_loss_ratio": 1.5}
    except Exception:
        return {"accuracy": 0.5, "avg_win_loss_ratio": 1.5}


# ── Stops Command ─────────────────────────────────────────────────────


def cmd_stops(config, symbols=None):
    """Calculate ATR-based stop-loss and take-profit prices."""
    client = get_alpaca_client()

    if symbols is None:
        positions = get_portfolio_positions(client)
        symbols = [p["symbol"] for p in positions]

    if not symbols:
        return {"timestamp": datetime.utcnow().isoformat(timespec="seconds"), "stops": {}}

    atrs = fetch_atr_batch(symbols, config["atr_period"])

    # Get entry prices from positions
    entry_prices = {}
    try:
        positions = get_portfolio_positions(client)
        for p in positions:
            entry_prices[p["symbol"]] = p["avg_cost"]
    except Exception:
        pass

    # Get current prices
    current_prices = {}
    try:
        for p in positions:
            current_prices[p["symbol"]] = p["current_price"]
    except Exception:
        pass

    stop_mult = config["stop_atr_multiplier"]
    target_mult = config["target_atr_multiplier"]
    trail_act = config["trailing_activation_atr"]
    trail_dist = config["trailing_distance_atr"]

    stops = {}
    for sym in symbols:
        atr = atrs.get(sym)
        if atr is None:
            stops[sym] = {"error": "ATR unavailable"}
            continue

        entry = entry_prices.get(sym, current_prices.get(sym, 0))
        current = current_prices.get(sym, entry)
        if entry <= 0:
            stops[sym] = {"error": "No price data"}
            continue

        stop_loss = round(entry - stop_mult * atr, 2)
        take_profit = round(entry + target_mult * atr, 2)
        stop_distance = entry - stop_loss
        target_distance = take_profit - entry
        rr_ratio = risk_reward_ratio(target_distance, stop_distance) if stop_distance > 0 else 0

        trailing_activation = round(entry + trail_act * atr, 2)
        trailing_stop = None
        if current > trailing_activation:
            trailing_stop = round(current - trail_dist * atr, 2)

        stops[sym] = {
            "atr": round(atr, 4),
            "atr_pct": round(atr / current * 100, 2) if current else 0,
            "entry_price": entry,
            "current_price": current,
            "stop_loss": stop_loss,
            "take_profit": take_profit,
            "stop_distance_pct": round(stop_distance / entry * 100, 2),
            "target_distance_pct": round(target_distance / entry * 100, 2),
            "risk_reward_ratio": rr_ratio,
            "trailing_activation_price": trailing_activation,
            "trailing_stop_price": trailing_stop,
        }

    return {"timestamp": datetime.utcnow().isoformat(timespec="seconds"), "stops": stops}


# ── Size Command ──────────────────────────────────────────────────────


def cmd_size(ticker, confidence, price, config):
    """Volatility-adjusted position sizing with Kelly criterion."""
    client = get_alpaca_client()
    equity = get_account_equity(client)

    # Get journal stats for Kelly
    jstats = get_journal_stats(ticker)
    win_rate = jstats["accuracy"]
    win_loss_ratio = jstats["avg_win_loss_ratio"]

    # 1. Kelly fraction (half-Kelly for safety)
    if win_rate > 0 and win_loss_ratio > 0:
        _, kelly_half = kelly_fraction(win_rate, win_loss_ratio)
    else:
        kelly_half = confidence * 0.5

    kelly_shares = int(equity * kelly_half / price) if price > 0 else 0

    # 2. Volatility cap
    atr = fetch_atr(ticker, config["atr_period"])
    constraints = []

    if atr and atr > 0:
        # Use ATR as fraction of price for volatility measure
        vol_fraction = atr / price
        # Benchmark: 2% daily vol is "average" — scale inversely
        benchmark_vol = 0.02
        vol_adj_pct = config["max_position_pct"] * (benchmark_vol / vol_fraction)
        vol_adj_pct = min(vol_adj_pct, config["max_position_pct"])
        vol_adj_shares = int(equity * vol_adj_pct / price)
        constraints.append(f"volatility_cap={vol_adj_pct:.3f}")
    else:
        vol_adj_pct = config["max_position_pct"]
        vol_adj_shares = int(equity * vol_adj_pct / price)
        constraints.append("volatility_cap=fallback(no ATR)")

    # 3. Hard cap
    hard_cap_shares = int(equity * config["max_position_pct"] / price)
    constraints.append(f"hard_cap={config['max_position_pct']}")

    # Final = min of all three
    final_shares = max(0, min(kelly_shares, vol_adj_shares, hard_cap_shares))

    # 4. Portfolio heat check
    stop_distance_pct = 0.0
    stop_price = 0.0
    risk_amount = 0.0
    if atr and atr > 0:
        stop_distance_pct = round((config["stop_atr_multiplier"] * atr) / price * 100, 2)
        stop_price = round(price - config["stop_atr_multiplier"] * atr, 2)
        risk_amount = round(final_shares * price * stop_distance_pct / 100, 2)
    else:
        stop_distance_pct = 4.0  # default 4% stop
        stop_price = round(price * 0.96, 2)
        risk_amount = round(final_shares * price * stop_distance_pct / 100, 2)

    # Check if adding this exceeds portfolio heat budget
    heat_budget = equity * config["max_portfolio_heat_pct"]
    try:
        positions = get_portfolio_positions(client)
        existing_heat = sum(
            p["market_value"] * abs(p["unrealized_pnl_pct"]) / 100 * 0.5
            for p in positions
        )
        if existing_heat + risk_amount > heat_budget:
            available_heat = max(0, heat_budget - existing_heat)
            if risk_amount > 0:
                heat_shares = int(available_heat / (price * stop_distance_pct / 100))
                final_shares = min(final_shares, heat_shares)
                risk_amount = round(final_shares * price * stop_distance_pct / 100, 2)
                constraints.append("portfolio_heat_limited")
    except Exception:
        pass

    final_dollar = round(final_shares * price, 2)
    pct_equity = round(final_dollar / equity * 100, 2) if equity > 0 else 0

    return {
        "ticker": ticker,
        "price": price,
        "confidence": confidence,
        "kelly_fraction": round(kelly_half, 4),
        "kelly_shares": kelly_shares,
        "volatility_adjusted_pct": round(vol_adj_pct, 4),
        "vol_adj_shares": vol_adj_shares,
        "final_shares": final_shares,
        "dollar_amount": final_dollar,
        "pct_of_equity": pct_equity,
        "risk_amount": risk_amount,
        "risk_amount_pct": round(risk_amount / equity * 100, 2) if equity > 0 else 0,
        "stop_distance_pct": stop_distance_pct,
        "stop_price": stop_price,
        "constraints_applied": constraints,
    }


# ── Check Command ─────────────────────────────────────────────────────


def cmd_check(ticker, shares, price, config):
    """Pre-trade risk gate."""
    client = get_alpaca_client()
    equity = get_account_equity(client)
    proposed_value = shares * price
    proposed_pct = proposed_value / equity if equity > 0 else 0

    approved = True
    reasons = []
    warnings = []

    # 1. Position size check
    if proposed_pct > config["max_position_pct"]:
        approved = False
        reasons.append(f"Position {proposed_pct:.1%} exceeds max {config['max_position_pct']:.0%}")

    # 2. Portfolio heat check
    atr = fetch_atr(ticker, config["atr_period"])
    stop_dist = (config["stop_atr_multiplier"] * atr / price) if atr and price > 0 else 0.04
    new_risk = proposed_value * stop_dist

    try:
        positions = get_portfolio_positions(client)
        existing_risk = sum(
            p["market_value"] * abs(p["unrealized_pnl_pct"]) / 100 * 0.5
            for p in positions
        )
        total_heat = existing_risk + new_risk
        heat_limit = equity * config["max_portfolio_heat_pct"]
        if total_heat > heat_limit:
            approved = False
            reasons.append(f"Portfolio heat {total_heat:.0f} exceeds budget {heat_limit:.0f}")
    except Exception:
        warnings.append("Could not calculate portfolio heat")

    # 3. Sector concentration
    sector = get_sector(ticker)
    try:
        positions = get_portfolio_positions(client)
        sector_value = proposed_value
        for p in positions:
            if get_sector(p["symbol"]) == sector:
                sector_value += p["market_value"]
        sector_pct = sector_value / equity if equity > 0 else 0
        if sector_pct > config["max_sector_pct"]:
            approved = False
            reasons.append(f"Sector {sector} at {sector_pct:.1%} exceeds {config['max_sector_pct']:.0%}")
    except Exception:
        warnings.append("Could not check sector concentration")

    # 4. Risk/reward check
    if atr and atr > 0:
        rr_ratio = config["target_atr_multiplier"] / config["stop_atr_multiplier"]
        if rr_ratio < config["min_risk_reward"]:
            approved = False
            reasons.append(f"Risk/reward {rr_ratio:.1f} below minimum {config['min_risk_reward']}")

    # Calculate max allowed shares
    max_shares = int(equity * config["max_position_pct"] / price) if price > 0 else 0
    adjustments = {
        "max_allowed_shares": max_shares,
        "max_allowed_pct": config["max_position_pct"],
    }

    return {
        "ticker": ticker,
        "proposed_shares": shares,
        "proposed_price": price,
        "proposed_value": round(proposed_value, 2),
        "proposed_pct": round(proposed_pct * 100, 2),
        "approved": approved,
        "reasons": reasons,
        "warnings": warnings,
        "adjustments": adjustments,
    }


# ── Assess Command ────────────────────────────────────────────────────


def cmd_assess(config):
    """Full portfolio risk assessment with alerts."""
    client = get_alpaca_client()
    equity = get_account_equity(client)
    account = client.get_account()
    cash = float(account.cash)

    try:
        positions = get_portfolio_positions(client)
    except Exception as e:
        return {"error": str(e), "equity": equity}

    if not positions:
        return {
            "timestamp": datetime.utcnow().isoformat(timespec="seconds"),
            "equity": equity,
            "cash": round(cash, 2),
            "total_exposure_pct": 0,
            "positions": [],
            "portfolio_heat_pct": 0,
            "sector_concentration": {},
            "alerts": ["No open positions"],
        }

    # Fetch ATR for all positions
    symbols = [p["symbol"] for p in positions]
    atrs = fetch_atr_batch(symbols, config["atr_period"])

    total_exposure = 0
    portfolio_heat = 0
    sector_weights = {}
    position_reports = []
    alerts = []

    for p in positions:
        sym = p["symbol"]
        mv = p["market_value"]
        weight = mv / equity if equity > 0 else 0
        total_exposure += mv
        sector = get_sector(sym)
        sector_weights[sector] = sector_weights.get(sector, 0) + weight

        atr = atrs.get(sym)
        stop_loss = 0
        take_profit = 0
        rr_ratio = 0
        dist_to_stop_pct = 0
        position_risk = 0

        if atr and atr > 0:
            stop_loss = round(p["avg_cost"] - config["stop_atr_multiplier"] * atr, 2)
            take_profit = round(p["avg_cost"] + config["target_atr_multiplier"] * atr, 2)
            stop_dist = p["avg_cost"] - stop_loss
            target_dist = take_profit - p["avg_cost"]
            rr_ratio = round(target_dist / stop_dist, 2) if stop_dist > 0 else 0
            dist_to_stop_pct = round((p["current_price"] - stop_loss) / p["current_price"] * 100, 2) if p["current_price"] > 0 else 0
            position_risk = mv * stop_dist / p["avg_cost"] if p["avg_cost"] > 0 else 0
            portfolio_heat += position_risk

        position_reports.append({
            "symbol": sym,
            "shares": p["shares"],
            "avg_cost": p["avg_cost"],
            "current_price": p["current_price"],
            "market_value": mv,
            "pnl_pct": p["unrealized_pnl_pct"],
            "weight_pct": round(weight * 100, 2),
            "sector": sector,
            "stop_loss": stop_loss,
            "take_profit": take_profit,
            "risk_reward_ratio": rr_ratio,
            "distance_to_stop_pct": dist_to_stop_pct,
            "position_risk": round(position_risk, 2),
        })

        # Alerts
        if weight > config["max_position_pct"]:
            alerts.append(f"{sym}: position {weight:.1%} exceeds {config['max_position_pct']:.0%} limit")
        if atr and p["current_price"] <= stop_loss + atr * 0.5:
            alerts.append(f"{sym}: within 0.5 ATR of stop-loss ({stop_loss})")
        if atr and rr_ratio < config["min_risk_reward"]:
            alerts.append(f"{sym}: risk/reward {rr_ratio} below {config['min_risk_reward']}")

    # Portfolio-level alerts
    heat_pct = portfolio_heat / equity if equity > 0 else 0
    if heat_pct > config["max_portfolio_heat_pct"]:
        alerts.append(f"Portfolio heat {heat_pct:.1%} exceeds {config['max_portfolio_heat_pct']:.0%} budget")

    for sector, pct in sector_weights.items():
        if pct > config["max_sector_pct"]:
            alerts.append(f"Sector {sector}: {pct:.1%} exceeds {config['max_sector_pct']:.0%} limit")

    return {
        "timestamp": datetime.utcnow().isoformat(timespec="seconds"),
        "equity": equity,
        "cash": round(cash, 2),
        "total_exposure_pct": round(total_exposure / equity * 100, 2) if equity > 0 else 0,
        "portfolio_heat_pct": round(heat_pct * 100, 2),
        "sector_concentration": {k: round(v * 100, 2) for k, v in sector_weights.items()},
        "positions": position_reports,
        "alerts": alerts,
    }


# ── Orders Command ────────────────────────────────────────────────────


def cmd_orders(config):
    """Generate stop-loss and take-profit orders for positions missing them."""
    client = get_alpaca_client()

    try:
        positions = get_portfolio_positions(client)
    except Exception as e:
        return {"error": str(e), "orders": [], "skipped": []}

    if not positions:
        return {"timestamp": datetime.utcnow().isoformat(timespec="seconds"), "orders": [], "skipped": []}

    # Get existing open orders to skip positions that already have stops
    try:
        from alpaca.trading.enums import QueryOrderStatus
        open_orders = client.get_orders(filter={"status": QueryOrderStatus.OPEN})
        symbols_with_stops = set()
        for o in open_orders:
            symbols_with_stops.add(o.symbol)
    except Exception:
        symbols_with_stops = set()

    symbols = [p["symbol"] for p in positions]
    atrs = fetch_atr_batch(symbols, config["atr_period"])

    orders = []
    skipped = []

    for p in positions:
        sym = p["symbol"]

        if sym in symbols_with_stops:
            skipped.append({"symbol": sym, "reason": "existing open order"})
            continue

        atr = atrs.get(sym)
        if not atr or atr <= 0:
            skipped.append({"symbol": sym, "reason": "ATR unavailable"})
            continue

        entry = p["avg_cost"]
        stop_price = round(entry - config["stop_atr_multiplier"] * atr, 2)
        target_price = round(entry + config["target_atr_multiplier"] * atr, 2)

        # Stop-loss order
        orders.append({
            "symbol": sym,
            "qty": p["shares"],
            "side": "sell",
            "type": "stop_limit",
            "stop_price": stop_price,
            "limit_price": round(stop_price - 0.05, 2),
            "time_in_force": "gtc",
            "reason": f"stop-loss: entry={entry}, ATR={atr:.2f}, stop={stop_price}",
        })

        # Take-profit order
        orders.append({
            "symbol": sym,
            "qty": p["shares"],
            "side": "sell",
            "type": "limit",
            "limit_price": target_price,
            "time_in_force": "gtc",
            "reason": f"take-profit: entry={entry}, ATR={atr:.2f}, target={target_price}",
        })

    return {
        "timestamp": datetime.utcnow().isoformat(timespec="seconds"),
        "orders": orders,
        "skipped": skipped,
    }


# ── CLI ───────────────────────────────────────────────────────────────


def main():
    parser = argparse.ArgumentParser(description="Portfolio Risk Intelligence")
    sub = parser.add_subparsers(dest="command")

    sub.add_parser("assess", help="Full portfolio risk assessment")

    stops_p = sub.add_parser("stops", help="Calculate ATR-based stops/targets")
    stops_p.add_argument("--symbols", nargs="+", help="Specific symbols (default: all positions)")

    size_p = sub.add_parser("size", help="Volatility-adjusted position sizing")
    size_p.add_argument("--ticker", required=True)
    size_p.add_argument("--confidence", type=float, required=True)
    size_p.add_argument("--price", type=float, required=True)

    check_p = sub.add_parser("check", help="Pre-trade risk check")
    check_p.add_argument("--ticker", required=True)
    check_p.add_argument("--shares", type=int, required=True)
    check_p.add_argument("--price", type=float, required=True)

    sub.add_parser("orders", help="Generate stop-loss/take-profit orders")

    args = parser.parse_args()
    config = load_config()

    if args.command == "assess":
        result = cmd_assess(config)
    elif args.command == "stops":
        result = cmd_stops(config, symbols=args.symbols)
    elif args.command == "size":
        result = cmd_size(args.ticker, args.confidence, args.price, config)
    elif args.command == "check":
        result = cmd_check(args.ticker, args.shares, args.price, config)
    elif args.command == "orders":
        result = cmd_orders(config)
    else:
        parser.print_help()
        return

    print(json.dumps(result, indent=2, default=str))


if __name__ == "__main__":
    main()
