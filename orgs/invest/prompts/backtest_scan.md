You are running a historical backtest scan for the Investment Division.

## Purpose
Time-travel test: fetch a historical market snapshot for a specific date, run the full pipeline against it, record signals, then evaluate past predictions against actual subsequent price action.

## Workflow
1. Load backtest snapshot:
   ```bash
   python3 /sandbox/backtest.py --date {date}
   ```
   This fetches historical data for that date in the same JSON format as a live scan.

2. Delegate to **research-director** with a Lightning-mode prefix:
   "Lightning mode: analyze the historical snapshot from /sandbox/backtest_snapshot.json. Generate signals for that date. Save to /sandbox/signals.json. Yield a brief report."

3. Record each signal for backtest scoring:
   ```bash
   python3 /sandbox/backtest.py --record-signal "SYMBOL,ACTION,CONFIDENCE,{date}"
   ```

4. Evaluate all predictions:
   ```bash
   python3 /sandbox/backtest.py --evaluate
   python3 /sandbox/backtest.py --report
   ```

5. Also run the strategy backtester for context:
   ```bash
   python3 /sandbox/historical.py run --months 3
   python3 /sandbox/historical.py compare
   ```

## Output
Return a backtest report with:
1. **Signals Generated** — what the strategy would have done on {date}
2. **Evaluation** — how correct were past predictions at each horizon (1d, 5d, 21d)
3. **Accuracy Metrics** — overall accuracy, average return, best and worst calls
4. **Strategy Performance** — 3-month walk-forward: total return, Sharpe, win rate vs SPY buy-and-hold
5. **Calibration** — confidence vs realized return correlation (should be positive)
