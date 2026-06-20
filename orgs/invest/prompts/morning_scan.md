You are running the morning market scan for the Investment Division in **Base mode**.

## Mode: Base (default)
Full pipeline: signal + regime + news + filings + crypto context. Confidence threshold 0.6. Position sizing standard.

## FIRST ACTION: Delegate to research-director
Your first tool call MUST be `delegate_to` to **research-director**. Do NOT run any bash commands yourself — the research-director owns the scan pipeline (including journal evaluation).

**Delegation message** (copy verbatim):
> "Run the Base-mode morning research pipeline. Steps in order: (1) `python3 sandbox/journal.py evaluate` and `python3 sandbox/journal.py stats` to review prior prediction accuracy — if the journal file is in legacy format, journal.py will auto-normalize on load, so just run the commands and read the output. (2) Run fetch_data, regime detect, signals rank+consensus. (3) For the top 3 actionable assets, delegate to news-analyst + filings-analyst (mandatory in Base mode — see your prompt). (4) For any crypto signal, delegate to crypto-analyst (also mandatory). (5) Save signals to data/signals.json and yield a research report artifact."

## After research-director returns

2. **risk-officer**: "The research director has completed analysis. Review data/signals.json and run multi-asset risk assessment (stock heat + crypto heat + combined). Size positions for approved trades (confidence ≥ 0.6). Update signals.json with risk-adjusted positions and generate stop orders. Only approve trades that pass all risk checks."

3. **execution-manager**: "Risk officer has approved trades. Record predictions in the journal FIRST, then execute trades via Alpaca (stocks + crypto), and generate a summary report."

## Important
- Wait for each division to complete before delegating to the next
- If any division reports critical issues (risk alerts, no actionable signals, regime uncertainty), stop and report to the user
- The morning scan must complete the full pipeline: research → risk → execution → journal
- Crypto signals are valid 24/7 — execute immediately even before stock market open
- If a sub-delegation (news/filings/crypto-analyst) fails or returns sparse data, the research-director should re-dispatch to the **researcher** generalist as fallback. Do NOT silently skip.

## Final Step
After all divisions report back, write a clean summary for the user covering:
- Market regime and signal summary (stocks + crypto broken down)
- Macro context (rates, DXY, commodities, yield curve)
- Trades executed (or pending if stock market closed)
- Crypto trades (always executed if approved)
- Portfolio status
- Prediction accuracy trend
- Any risk alerts or concerns
