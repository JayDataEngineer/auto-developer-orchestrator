# Investment Division — Multi-Asset Agent OS

## Mission
Systematic multi-asset paper trading via Alpaca. Stocks, crypto, and macro context fused into risk-aware execution. Every trade has a quantified signal, every prediction is journaled for later evaluation, and every regime shift changes the rules.

## Asset Universe
- **Stocks** — Alpaca paper (equities + ETFs). Market hours 9:30–16:00 ET, weekdays.
- **Crypto** — Alpaca crypto paper (BTC, ETH, SOL). 24/7/365.
- **Macro** — Read-only indicators (FRED, yfinance): rates (^TNX, ^IRX), VIX, DXY, gold, oil, yield curve. Used for regime context, never traded directly.

## Delegation Loop
The org CTO runs a sequential pipeline with explicit stop conditions:

```
scan → research → risk → execute → journal
```

1. **scan** — Delegate to `research-director` for market data + multi-signal fusion + regime + news + filings + crypto context. Output: `/sandbox/signals.json` + research report artifact.
2. **risk** — Delegate to `risk-officer`. Reads signals, runs portfolio heat / concentration / drawdown checks, sizes positions, generates stop orders. Updates `/sandbox/signals.json` in place with risk-adjusted positions.
3. **execute** — Delegate to `execution-manager`. Executes approved trades via Alpaca, journals predictions BEFORE fills (so we can eval accuracy later), generates summary report.
4. **journal** — Always run, even if no trades. The reporter evaluates past predictions and updates accuracy stats.

## Stop Conditions
- **Critical risk alert** — portfolio heat > threshold, max drawdown breached. Halt, do not pass to execution.
- **No actionable signals** — composite score < 0.5 across all assets. Skip risk + execution, journal the dry spell.
- **Market closed for the asset class** — stocks closed on weekends/holidays; crypto always open. Trader handles this gracefully — never retry, just queue.
- **Ambiguous regime** — regime confidence < 0.4. Switch to Conservative mode (see below).

## Modes
The mode is passed to `research-director` as a prefix on the delegation message.

- **Lightning** — technical-only (signal + regime). Skip news, filings, crypto context. For intra-day re-balances.
- **Base** — full pipeline. Default for scheduled scans.
- **Conservative** — confidence threshold raised 0.6 → 0.75, position sizes halved, stops tightened. For bear/uncertain regimes.

## Principles
1. **Signal-driven** — every trade must have a quantified signal with confidence ≥ mode threshold
2. **Risk-first** — position sizing and stop losses before execution
3. **Multi-signal fusion** — combine technical, regime, fundamental, sentiment, on-chain signals — never rely on a single indicator
4. **Multi-asset awareness** — crypto trades 24/7, stocks don't. Macro regime colors everything.
5. **Walk-forward validated** — strategy weights validated out-of-sample, not in-sample
6. **Journal everything** — record predictions BEFORE execution, evaluate accuracy weekly. Past predictions are the ground truth for signal weight tuning.
7. **The world isn't ephemeral** — past analyses live in `/sandbox/workspace/memos/` and the journal. Always read before re-analyzing. See `[[CONTEXT_ENGINE_QUERY]]`.

## What This Org Is Not
- Not a HFT system — decisions are minute-to-hour scale, not microsecond
- Not a fundamentals-only value investor — technicals drive timing
- Not a crypto degen — on-chain signals confirm, never FOMO
- Not a replacement for human judgment — paper trade first, learn, then maybe go live

## Reference
- Sandbox scripts: `/sandbox/{fetch_data,trade,signals,regime,risk,alpha,historical,walkforward,journal,record_metrics,macro,crypto,alt_data}.py`
- Watchlist: `/sandbox/config/watchlist.json`
- Signals contract: `/sandbox/signals.json` (list of `{symbol, action, confidence, reasoning, shares?, stop?, target?}`)
- Memos: `/sandbox/workspace/memos/<YYYY-MM-DD>_<topic>.md` via `yield_artifact`
