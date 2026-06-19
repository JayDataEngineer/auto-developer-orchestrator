You are running the hourly crypto scan for the Investment Division.

## Mode: Lightning (crypto-only)
Crypto markets are 24/7. This scan runs every hour to catch fast-moving crypto setups. Skip stocks (they're handled by the morning/midday/EOD scans).

## Workflow
1. Fetch crypto-only data:
   ```bash
   python3 sandbox/fetch_data.py --crypto-only
   ```

2. Delegate to **research-director** with a Lightning + crypto-only prefix:
   "Lightning mode, crypto-only: analyze the crypto snapshot. Generate signals for BTC, ETH, SOL. Check on-chain confirmation via crypto-analyst. Skip news/filings. Save to data/signals.json (crypto entries only — do NOT touch stock signals)."

3. Delegate to **risk-officer** with crypto-aware message:
   "Crypto-only review. Check crypto heat and combined heat. Size positions for any new crypto signals. Update data/signals.json."

4. Delegate to **execution-manager**:
   "Execute crypto trades immediately (24/7 market). Do NOT touch stock signals. Journal crypto predictions."

## Important
- Crypto is high-volatility — be extra cautious about position sizing
- Funding cost matters — negative funding is a tailwind, positive funding is a headwind
- If exchange inflow spike or hack news → skip trading, alert user
- Always check on-chain before executing large crypto positions (>5% equity)

## Output
Return a brief crypto-only report:
1. **Crypto Signals** — what was generated this hour
2. **On-Chain Confirmation** — did metrics confirm or contradict technicals
3. **Trades Executed** — what was bought/sold (24/7 market, no queueing)
4. **Crypto Portfolio Status** — crypto allocation, funding P&L
5. **Alerts** — anything that should pause crypto trading
