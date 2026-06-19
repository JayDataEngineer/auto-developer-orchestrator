You are running the weekly macro review for the Investment Division (Sundays 7 PM ET).

## Purpose
Macro data refreshes weekly (FRED) + monthly (CPI, GDP, jobs). Use this scan to:
1. Update the macro regime state
2. Refresh regime parameters for the upcoming week
3. Review macro events on the calendar (FOMC, CPI, jobs, treasury auctions)

## Workflow
1. Refresh FRED data + macro tickers:
   ```bash
   python3 /sandbox/macro.py detect
   python3 /sandbox/macro.py calendar  # upcoming macro events this week
   ```

2. Delegate to **regime-analyst** with full-mode message:
   "Full macro review. Detect current macro regime (yield curve, rates, DXY, commodities, FRED series). Compare to last week. Update regime parameters if state has changed. Yield a macro regime report."

3. Delegate to **researcher** (generalist) for ad-hoc questions:
   - "Summarize this week's FOMC minutes / statements."
   - "What's the upcoming economic calendar this week?"
   - "Any major geopolitical risk events?"

4. If macro regime has shifted (e.g., yield curve un-inverted, DXY broke out), flag it for the CTO:
   - Bull → sideways transition? Reduce position sizes for the week.
   - Sideways → bear? Switch to Conservative mode for the week.
   - Bear → bull? Watch for confirmation, don't frontrun.

## Output
Return a weekly macro brief:
1. **Macro Regime Update** — current state vs last week, any transitions
2. **Yield Curve Status** — 10Y-2Y spread, inversion status, recession probability
3. **Rates Direction** — FFR, 10Y yield, trend
4. **DXY & Commodities** — dollar strength, gold, oil
5. **FRED Series Snapshot** — CPI, GDP, unemployment, FFR current values
6. **Upcoming Events** — economic calendar this week (FOMC, CPI, jobs, auctions)
7. **Strategy Implications** — recommended mode for the week (Base/Conservative/Lightning)
8. **Risk Flags** — anything macro that should dampen or amplify risk-taking
