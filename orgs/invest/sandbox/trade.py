"""
Alpaca paper trading executor — runs inside the sandbox.
Reads signals from /sandbox/signals.json, executes paper trades
via Alpaca's API, outputs trade summary as JSON.

Usage: python3 trade.py [--signals signals.json]
"""

import json
import sys
import os
import argparse
from datetime import datetime

from alpaca.trading.client import TradingClient
from alpaca.trading.requests import MarketOrderRequest, LimitOrderRequest
from alpaca.trading.enums import OrderSide, TimeInForce, QueryOrderStatus


# Alpaca paper trading credentials — required from env, never hardcoded
API_KEY = os.environ.get("ALPACA_API_KEY")
SECRET_KEY = os.environ.get("ALPACA_SECRET_KEY")

if not API_KEY or not SECRET_KEY:
    print("ERROR: ALPACA_API_KEY and ALPACA_SECRET_KEY must be set in env.", file=sys.stderr)
    sys.exit(2)

SIGNALS_FILE = "/sandbox/signals.json"
MAX_POSITION_PCT = 0.15  # Max 15% of equity in single position
MIN_CONFIDENCE = 0.6


def get_client():
    return TradingClient(API_KEY, SECRET_KEY, paper=True)


def get_current_prices(client, symbols):
    """Get latest trade prices from Alpaca."""
    prices = {}
    for sym in symbols:
        try:
            # Use get_latest_trade for real-time price
            from alpaca.data.live.stock import StockDataStream
            # Fallback: use the trading client's quote endpoint
            quote = client.get_stock_latest_quote(sym)
            if quote:
                prices[sym] = float(quote.ask_price if quote.ask_price else quote.bid_price)
        except Exception:
            pass
    return prices


def get_simple_price(client, symbol):
    """Get a price for a symbol — try multiple approaches."""
    # Try getting from open positions first
    try:
        positions = client.get_all_positions()
        for pos in positions:
            if pos.symbol == symbol:
                return float(pos.current_price)
    except Exception:
        pass

    # Try getting a recent quote
    try:
        quote = client.get_stock_latest_quote(symbol)
        if quote and (quote.ask_price or quote.bid_price):
            mid = (float(quote.ask_price or 0) + float(quote.bid_price or 0)) / 2
            if mid > 0:
                return mid
            return float(quote.ask_price or quote.bid_price)
    except Exception:
        pass

    return None


def execute_signals(client, signals):
    """Execute trading signals via Alpaca paper trading API."""
    executed = []
    account = client.get_account()
    equity = float(account.equity)
    cash = float(account.cash)

    # Get current positions
    positions = {p.symbol: p for p in client.get_all_positions()}

    for sig in signals:
        symbol = sig["symbol"]
        action = sig.get("action", "hold")
        confidence = sig.get("confidence", 0.5)

        if action == "hold" or confidence < MIN_CONFIDENCE:
            executed.append({**sig, "executed": False, "reason": f"{'Hold signal' if action == 'hold' else f'Low confidence ({confidence})'}"})
            continue

        try:
            if action in ("buy", "strong_buy"):
                # Position sizing
                size_pct = min(confidence * MAX_POSITION_PCT, MAX_POSITION_PCT)
                budget = equity * size_pct

                # Get price
                price = get_simple_price(client, symbol)
                if not price:
                    executed.append({**sig, "executed": False, "reason": "Could not get price"})
                    continue

                shares = int(budget / price)
                if shares <= 0:
                    executed.append({**sig, "executed": False, "reason": f"Insufficient budget ({budget:.0f} / {price:.2f})"})
                    continue

                # Check if we have enough cash
                cost = shares * price
                if cost > cash:
                    shares = int(cash / price)
                    if shares <= 0:
                        executed.append({**sig, "executed": False, "reason": "Insufficient cash"})
                        continue

                # Place market buy order
                order = client.submit_order(
                    MarketOrderRequest(
                        symbol=symbol,
                        qty=shares,
                        side=OrderSide.BUY,
                        time_in_force=TimeInForce.DAY,
                    )
                )

                cash -= cost
                executed.append({
                    **sig,
                    "executed": True,
                    "order_id": str(order.id),
                    "shares": shares,
                    "estimated_price": round(price, 2),
                    "estimated_cost": round(cost, 2),
                    "status": order.status.value if hasattr(order.status, 'value') else str(order.status),
                })

            elif action in ("sell", "strong_sell"):
                # Check existing position
                pos = positions.get(symbol)
                if not pos:
                    executed.append({**sig, "executed": False, "reason": "No position to sell"})
                    continue

                sell_qty = int(float(pos.qty))
                if action == "sell":
                    sell_qty = max(1, sell_qty // 2)  # Sell half

                order = client.submit_order(
                    MarketOrderRequest(
                        symbol=symbol,
                        qty=sell_qty,
                        side=OrderSide.SELL,
                        time_in_force=TimeInForce.DAY,
                    )
                )

                price = float(pos.current_price)
                proceeds = sell_qty * price
                pnl = (price - float(pos.avg_entry_price)) * sell_qty

                executed.append({
                    **sig,
                    "executed": True,
                    "order_id": str(order.id),
                    "shares": sell_qty,
                    "price": round(price, 2),
                    "proceeds": round(proceeds, 2),
                    "pnl": round(pnl, 2),
                    "status": order.status.value if hasattr(order.status, 'value') else str(order.status),
                })

        except Exception as e:
            executed.append({**sig, "executed": False, "reason": str(e)[:200]})

    return executed


def get_portfolio_summary(client):
    """Get current portfolio state from Alpaca."""
    account = client.get_account()
    positions = client.get_all_positions()

    pos_list = []
    for p in positions:
        pos_list.append({
            "symbol": p.symbol,
            "shares": int(float(p.qty)),
            "avg_cost": round(float(p.avg_entry_price), 2),
            "current_price": round(float(p.current_price), 2),
            "market_value": round(float(p.market_value), 2),
            "unrealized_pnl": round(float(p.unrealized_pl), 2),
            "unrealized_pnl_pct": round(float(p.unrealized_plpc) * 100, 2),
        })

    return {
        "equity": round(float(account.equity), 2),
        "cash": round(float(account.cash), 2),
        "buying_power": round(float(account.buying_power), 2),
        "positions": pos_list,
        "position_count": len(pos_list),
    }


# ── Portfolio Ledger ──────────────────────────────────────────────────

LEDGER_FILE = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data", "ledger.json")
STARTING_EQUITY = 100000.0  # Alpaca paper accounts start at $100k


def load_ledger():
    """Load the portfolio ledger (creates empty if missing)."""
    if os.path.exists(LEDGER_FILE):
        with open(LEDGER_FILE) as f:
            return json.load(f)
    return {"starting_equity": STARTING_EQUITY, "snapshots": []}


def save_ledger(ledger):
    """Save the portfolio ledger."""
    os.makedirs(os.path.dirname(LEDGER_FILE), exist_ok=True)
    with open(LEDGER_FILE, "w") as f:
        json.dump(ledger, f, indent=2)


def record_snapshot(client, trades=None):
    """Take a portfolio snapshot and append to the ledger."""
    portfolio = get_portfolio_summary(client)
    ledger = load_ledger()

    equity = portfolio["equity"]
    starting = ledger["starting_equity"]
    total_pnl = round(equity - starting, 2)
    total_pnl_pct = round((total_pnl / starting) * 100, 2) if starting > 0 else 0

    # Calculate daily change from last snapshot
    daily_pnl = 0
    daily_pnl_pct = 0
    if ledger["snapshots"]:
        prev_equity = ledger["snapshots"][-1]["equity"]
        daily_pnl = round(equity - prev_equity, 2)
        daily_pnl_pct = round((daily_pnl / prev_equity) * 100, 2) if prev_equity > 0 else 0

    snapshot = {
        "timestamp": datetime.now().isoformat(),
        "equity": equity,
        "cash": portfolio["cash"],
        "buying_power": portfolio["buying_power"],
        "position_count": portfolio["position_count"],
        "positions": portfolio["positions"],
        "daily_pnl": daily_pnl,
        "daily_pnl_pct": daily_pnl_pct,
        "total_pnl": total_pnl,
        "total_pnl_pct": total_pnl_pct,
    }
    if trades:
        snapshot["trades_executed"] = len([t for t in trades if t.get("executed")])
        snapshot["trades_skipped"] = len([t for t in trades if not t.get("executed")])

    ledger["snapshots"].append(snapshot)
    save_ledger(ledger)
    return snapshot


def print_ledger_summary(ledger):
    """Print a human-readable money trail."""
    snaps = ledger["snapshots"]
    if not snaps:
        print("No portfolio snapshots recorded yet.")
        return

    starting = ledger["starting_equity"]
    latest = snaps[-1]

    print(f"\n{'='*55}")
    print(f"  PORTFOLIO LEDGER — {latest['timestamp'][:10]}")
    print(f"{'='*55}")
    print(f"  Starting equity:  ${starting:>12,.2f}")
    print(f"  Current equity:   ${latest['equity']:>12,.2f}")
    print(f"  Cash:             ${latest['cash']:>12,.2f}")
    print(f"  Positions:        {latest['position_count']:>12}")
    print(f"  Total P&L:        ${latest['total_pnl']:>12,.2f} ({latest['total_pnl_pct']:+.2f}%)")
    print(f"  Daily change:     ${latest['daily_pnl']:>12,.2f} ({latest['daily_pnl_pct']:+.2f}%)")

    # Show positions
    if latest.get("positions"):
        print(f"\n  {'Symbol':<8} {'Shares':>6} {'AvgCost':>10} {'Price':>10} {'Value':>10} {'P&L':>10}")
        print(f"  {'-'*54}")
        for p in latest["positions"]:
            print(f"  {p['symbol']:<8} {p['shares']:>6} ${p['avg_cost']:>8.2f} ${p['current_price']:>8.2f} ${p['market_value']:>8.2f} ${p['unrealized_pnl']:>+8.2f}")

    # Show history (last 10 snapshots)
    if len(snaps) > 1:
        print(f"\n  --- History (last {min(len(snaps), 10)} snapshots) ---")
        print(f"  {'Date':<12} {'Equity':>12} {'Daily P&L':>12} {'Total P&L':>12}")
        print(f"  {'-'*48}")
        for s in snaps[-10:]:
            date = s["timestamp"][:10]
            print(f"  {date:<12} ${s['equity']:>10,.2f} ${s['daily_pnl']:>+10,.2f} ${s['total_pnl']:>+10,.2f}")

    print(f"{'='*55}\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--signals", default=SIGNALS_FILE)
    parser.add_argument("--status", action="store_true", help="Just show portfolio status")
    parser.add_argument("--ledger", action="store_true", help="Show portfolio ledger with P&L history")
    parser.add_argument("--snapshot", action="store_true", help="Record a portfolio snapshot to the ledger")
    args = parser.parse_args()

    client = get_client()

    # Ledger mode — show money trail
    if args.ledger:
        ledger = load_ledger()
        if not ledger["snapshots"]:
            # Take a fresh snapshot
            record_snapshot(client)
            ledger = load_ledger()
        print_ledger_summary(ledger)
        return

    # Snapshot mode — just record to ledger
    if args.snapshot:
        snap = record_snapshot(client)
        print(json.dumps(snap, indent=2))
        return

    # Status-only mode
    if args.status:
        summary = get_portfolio_summary(client)
        print(json.dumps(summary, indent=2))
        return

    # Verify connection and check market status
    account = client.get_account()
    print(f"Account: {account.status} | Cash: ${account.cash} | Equity: ${account.equity}", file=sys.stderr)

    clock = client.get_clock()
    if not clock.is_open:
        print(f"Market closed — next open: {clock.next_open}. Saving signals for next session.", file=sys.stderr)
        summary = get_portfolio_summary(client)
        summary["market_open"] = False
        summary["next_open"] = str(clock.next_open)
        print(json.dumps(summary, indent=2))
        # Still record the snapshot
        record_snapshot(client)
        return

    # Load signals
    if not os.path.exists(args.signals):
        print("No signals file found — showing portfolio status only", file=sys.stderr)
        summary = get_portfolio_summary(client)
        print(json.dumps(summary, indent=2))
        record_snapshot(client)
        return

    with open(args.signals) as f:
        signals = json.load(f)

    if not signals:
        print("Empty signals — no trades", file=sys.stderr)
        summary = get_portfolio_summary(client)
        print(json.dumps(summary, indent=2))
        record_snapshot(client)
        return

    # Execute trades
    executed = execute_signals(client, signals)

    # Get final portfolio state and record to ledger
    portfolio = get_portfolio_summary(client)
    snap = record_snapshot(client, trades=executed)

    summary = {
        "timestamp": datetime.now().isoformat(),
        "trades_executed": len([t for t in executed if t.get("executed")]),
        "trades_skipped": len([t for t in executed if not t.get("executed")]),
        "trades": executed,
        "portfolio": portfolio,
        "ledger": snap,
    }

    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    main()
