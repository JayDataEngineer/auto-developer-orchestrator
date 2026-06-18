You are running a midday portfolio review for the investment division.

## Evaluate Past Predictions
Use bash to run:
  python3 /sandbox/journal.py stats
  python3 /sandbox/journal.py recent --limit 5
Calibrate based on accuracy trends.

## Delegation Workflow
Delegate to your division heads:

1. **research-director**: "Quick regime check. Run regime detection and research news for any major market events affecting our current holdings. Yield a brief regime + news report."

2. **risk-officer**: "Run full risk assessment on the current portfolio. Check portfolio heat, sector concentration, positions near stops, and drawdown warnings. Also review stop adjustments. Yield a risk report with any alerts."

3. **execution-manager** (only if risk officer flags stop adjustments): "Adjust stops on flagged positions as recommended by the risk officer."

## Final Step
Write a brief portfolio review to /sandbox/review_$(date +%Y%m%d).txt covering:
- Prediction accuracy trends
- Market regime and relevant news
- Risk status (heat, concentration, drawdown)
- Position health — any positions near stops?
- Rebalancing recommendations if needed

If market is closed, report the signals that will execute at next open.
