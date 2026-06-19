"""
Aggregate invest-bot metrics into a flat JSON blob for Langfuse scoring.

Collects from: regime.py, risk.py, journal.py, data/ledger.json
Computes: Sharpe ratio, max drawdown, return %, win rate, profit factor

Output: JSON with "langfuse_metrics": true sentinel at top level.
The orchestrator's Langfuse hook detects this sentinel and posts each
numeric key as a chartable score.

Modes:
  live        (default) Collect from live Alpaca + current regime + journal
  simulation  Read from historical.py's output (historical_results.json)
              This pulls Sharpe, Sortino, drawdown, win rate, profit factor,
              return vs SPY from the history walk simulation.

Usage:
  python3 record_metrics.py                        # live mode
  python3 record_metrics.py --mode simulation      # from history walk
  python3 record_metrics.py --data-dir /path       # custom data dir
"""

import json
import math
import os
import subprocess
import sys
from datetime import datetime
from pathlib import Path

# Allow importing sibling modules when run from sandbox
SCRIPT_DIR = Path(__file__).parent
sys.path.insert(0, str(SCRIPT_DIR))


def load_ledger(data_dir):
    """Load portfolio ledger (equity snapshots over time)."""
    path = Path(data_dir) / "ledger.json"
    if not path.exists():
        return None
    with open(path) as f:
        return json.load(f)


def get_regime_metrics():
    """Import regime module and detect current market regime."""
    try:
        import regime
        config = regime.load_config()
        det = regime.detect_regime(config)
        return {
            "regime_composite": det.get("composite", 0),
            "regime_label": det.get("regime", "unknown"),
            "regime_confidence": det.get("confidence", 0),
        }
    except Exception as e:
        return {"_error": str(e)}


def get_risk_metrics():
    """Call risk.py assess via subprocess (it outputs JSON to stdout)."""
    try:
        result = subprocess.run(
            [sys.executable, str(SCRIPT_DIR / "risk.py"), "assess"],
            capture_output=True, text=True, timeout=30,
        )
        data = json.loads(result.stdout.strip())
        return {
            "portfolio_heat": data.get("portfolio_heat_pct", 0),
            "equity": data.get("equity", 0),
            "cash": data.get("cash", 0),
            "position_count": len(data.get("positions", [])),
        }
    except Exception as e:
        return {"_error": str(e)}


def get_journal_metrics():
    """Import journal module and compute prediction accuracy."""
    try:
        import journal
        data = journal.load_journal()
        stats = journal.compute_stats(data)
        accuracy = stats.get("accuracy", 0) / 100.0  # normalize to 0-1
        return {
            "prediction_accuracy": accuracy,
            "total_predictions": stats.get("total", 0),
            "evaluated": stats.get("evaluated", 0),
            "correct": stats.get("correct", 0),
            "wrong": stats.get("wrong", 0),
        }
    except Exception as e:
        return {"_error": str(e)}


def compute_derived_metrics(ledger):
    """Compute Sharpe, drawdown, return from ledger equity curve."""
    if not ledger or not ledger.get("snapshots"):
        return {}

    snapshots = ledger["snapshots"]
    starting = ledger.get("starting_equity", 100000)
    latest = snapshots[-1]
    equity = latest.get("equity", 0)

    metrics = {
        "equity": equity,
        "total_pnl": latest.get("total_pnl", equity - starting),
        "daily_pnl": latest.get("daily_pnl", 0),
        "return_pct": round((equity - starting) / starting * 100, 4) if starting else 0,
    }

    # Need at least 10 snapshots for meaningful Sharpe/drawdown
    equities = [s["equity"] for s in snapshots if "equity" in s]
    if len(equities) >= 10:
        # Daily returns
        returns = []
        for i in range(1, len(equities)):
            if equities[i - 1] > 0:
                returns.append((equities[i] - equities[i - 1]) / equities[i - 1])

        if len(returns) >= 5:
            mean_r = sum(returns) / len(returns)
            variance = sum((r - mean_r) ** 2 for r in returns) / len(returns)
            std_r = math.sqrt(variance) if variance > 0 else 0

            # Annualized Sharpe (252 trading days)
            if std_r > 0:
                metrics["sharpe_ratio"] = round(mean_r / std_r * math.sqrt(252), 4)
            else:
                metrics["sharpe_ratio"] = 0

        # Max drawdown
        peak = equities[0]
        max_dd = 0
        max_dd_pct = 0
        for eq in equities:
            if eq > peak:
                peak = eq
            dd = eq - peak
            dd_pct = dd / peak if peak > 0 else 0
            if dd < max_dd:
                max_dd = dd
                max_dd_pct = dd_pct

        metrics["max_drawdown"] = round(max_dd, 2)
        metrics["max_drawdown_pct"] = round(max_dd_pct * 100, 4)
    else:
        metrics["sharpe_ratio"] = 0
        metrics["max_drawdown"] = 0
        metrics["max_drawdown_pct"] = 0

    return metrics


def compute_win_profit(journal_metrics):
    """Compute win rate and profit factor from journal stats."""
    correct = journal_metrics.get("correct", 0)
    wrong = journal_metrics.get("wrong", 0)
    total = correct + wrong

    if total == 0:
        return {"win_rate": 0, "profit_factor": 0}

    win_rate = correct / total
    # Profit factor proxy: use accuracy ratio (no trade-level P&L available)
    if wrong > 0:
        profit_factor = correct / wrong
    else:
        profit_factor = 99.99 if correct > 0 else 0

    return {
        "win_rate": round(win_rate, 4),
        "profit_factor": round(profit_factor, 2),
    }


def load_historical_results():
    """Load historical.py simulation results (from history walk)."""
    # Check env var first, then data/ dir, then sandbox/ dir
    candidates = [
        os.environ.get("HISTORICAL_RESULTS", ""),
        str(Path(SCRIPT_DIR).parent / "data" / "historical_results.json"),
        str(SCRIPT_DIR / "historical_results.json"),
    ]
    for path in candidates:
        if path and os.path.exists(path):
            with open(path) as f:
                return json.load(f)
    return None


def extract_simulation_metrics(report):
    """Flatten historical.py report into Langfuse metrics format."""
    if not report:
        return None

    metrics = {"langfuse_metrics": True, "timestamp": datetime.utcnow().isoformat(timespec="seconds") + "Z"}

    # Returns
    ret = report.get("returns", {})
    metrics["return_pct"] = ret.get("total_return_pct", 0)
    metrics["annualized_return_pct"] = ret.get("annualized_return_pct", 0)
    metrics["benchmark_return_pct"] = ret.get("benchmark_return_pct", 0)
    metrics["excess_return_pct"] = ret.get("excess_return_pct", 0)

    # Risk
    risk = report.get("risk", {})
    metrics["sharpe_ratio"] = risk.get("sharpe_ratio", 0)
    metrics["sortino_ratio"] = risk.get("sortino_ratio", 0)
    metrics["max_drawdown_pct"] = risk.get("max_drawdown_pct", 0)
    metrics["volatility_pct"] = risk.get("volatility_annualized_pct", 0)

    # Trades
    trades = report.get("trades", {})
    metrics["win_rate"] = trades.get("win_rate_pct", 0) / 100.0
    metrics["profit_factor"] = trades.get("profit_factor", 0)
    metrics["total_pnl"] = trades.get("total_pnl", 0)
    metrics["total_trades"] = trades.get("total_trades", 0)
    metrics["round_trips"] = trades.get("round_trips", 0)
    metrics["avg_win"] = trades.get("avg_win", 0)
    metrics["avg_loss"] = trades.get("avg_loss", 0)

    # Period
    period = report.get("period", {})
    metrics["trading_days"] = period.get("trading_days", 0)

    # Mode tag
    metrics["simulation_mode"] = True

    metrics["_meta"] = {
        "source": "record_metrics.py (simulation)",
        "version": 1,
        "period": f"{period.get('start', '')} to {period.get('end', '')}",
        "errors": [],
    }

    return metrics


def main():
    data_dir = os.environ.get("DATA_DIR", str(Path(SCRIPT_DIR).parent / "data"))
    mode = "live"

    # Parse args
    args = sys.argv[1:]
    if "--data-dir" in args:
        idx = args.index("--data-dir")
        if idx + 1 < len(args):
            data_dir = args[idx + 1]
    if "--mode" in args:
        idx = args.index("--mode")
        if idx + 1 < len(args):
            mode = args[idx + 1]

    # Simulation mode: read from historical_results.json
    if mode == "simulation":
        report = load_historical_results()
        metrics = extract_simulation_metrics(report)
        if metrics is None:
            print(json.dumps({
                "langfuse_metrics": True,
                "_meta": {"source": "record_metrics.py", "version": 1,
                          "errors": ["No historical_results.json found. Run historical.py first."]},
            }))
            return
        print(json.dumps(metrics, indent=2))
        return

    # Live mode (default)
    metrics = {
        "langfuse_metrics": True,
        "timestamp": datetime.utcnow().isoformat(timespec="seconds") + "Z",
    }
    errors = []

    # 1. Derived metrics from ledger (Sharpe, drawdown, return)
    ledger = load_ledger(data_dir)
    derived = compute_derived_metrics(ledger)
    metrics.update(derived)

    # 2. Regime metrics (import module)
    regime = get_regime_metrics()
    if "_error" in regime:
        errors.append(f"regime: {regime['_error']}")
    else:
        metrics["regime_composite"] = regime.get("regime_composite", 0)
        metrics["regime_label"] = regime.get("regime_label", "unknown")

    # 3. Risk metrics (subprocess — outputs JSON)
    risk = get_risk_metrics()
    if "_error" in risk:
        errors.append(f"risk: {risk['_error']}")
    else:
        metrics["portfolio_heat"] = risk.get("portfolio_heat", 0)
        # Only override equity/position_count if not already set from ledger
        if "equity" not in metrics or metrics.get("equity", 0) == 0:
            metrics["equity"] = risk.get("equity", 0)
        if "position_count" not in metrics:
            metrics["position_count"] = risk.get("position_count", 0)

    # 4. Journal metrics (import module)
    journal = get_journal_metrics()
    if "_error" in journal:
        errors.append(f"journal: {journal['_error']}")
    else:
        metrics["prediction_accuracy"] = journal.get("prediction_accuracy", 0)
        # Win rate and profit factor from journal
        wp = compute_win_profit(journal)
        metrics["win_rate"] = wp["win_rate"]
        metrics["profit_factor"] = wp["profit_factor"]

    metrics["_meta"] = {
        "source": "record_metrics.py",
        "version": 1,
        "errors": errors,
    }

    print(json.dumps(metrics, indent=2))


if __name__ == "__main__":
    main()
