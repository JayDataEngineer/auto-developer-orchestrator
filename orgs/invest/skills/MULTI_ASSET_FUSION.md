# MULTI_ASSET_FUSION

How to blend signals across stocks, crypto, and macro context. This skill is queryable by the **CTO** via `read_skill`.

## The Problem
Each specialist returns a confidence score for their asset class. But asset classes are correlated — a crypto crash drags tech stocks; a stock market sell-off shakes crypto. Naive averaging of confidences over-allocates to correlated risks.

## Asset Class Profiles

| Class | Vol (annualized σ) | Correlation to SPX | Hours |
|-------|-------------------|-------------------|-------|
| Stocks (SPX) | ~15% | 1.0 | 9:30-16:00 ET weekdays |
| Crypto (BTC) | ~60-80% | 0.3-0.5 (rising in risk-off) | 24/7/365 |
| Macro tickers | varies | various | Futures ~23h/day weekdays |

## Cross-Asset Signal Rules

### Rule 1: Macro regime dampens or amplifies
- **Risk-off macro regime**: reduce all risk-asset confidences by 0.1
- **Risk-on macro regime**: leave as-is or +0.05
- **Mixed**: leave as-is

### Rule 2: Correlation discount for portfolio heat
When multiple signals would result in correlated positions, discount combined confidence:

```python
def combined_confidence(signals, correlation_matrix):
    raw = sum(s.confidence * s.weight for s in signals)
    # Discount by average pairwise correlation
    avg_corr = mean(correlation_matrix[s1.asset][s2.asset]
                    for s1, s2 in pairs(signals) if s1.asset != s2.asset)
    return raw * (1 - 0.3 * max(0, avg_corr - 0.3))
```

So if stocks + crypto are both bullish and their correlation is 0.6, you take a 10% discount on the combined confidence.

### Rule 3: Asset-class-specific sanity checks
- **Stock earnings within 5 days**: halve confidence, widen stop
- **Crypto hack/exploit in last 7 days**: cap confidence at 0.3
- **FOMC meeting within 48 hours**: reduce all risk-asset confidences by 0.15
- **CPI print day**: same as FOMC

### Rule 4: Funding cost penalty (crypto only)
For crypto longs in negative-funding regimes, no penalty. For positive-funding regimes, reduce effective confidence by funding cost / expected return:

```python
def funding_adjusted_confidence(confidence, funding_rate_8h, holding_period_days):
    # Annualized funding cost
    annual_funding = funding_rate_8h * 3 * 365
    expected_annual_return = 0.30  # 30% baseline crypto expected return
    penalty = annual_funding / expected_annual_return
    return confidence * (1 - min(penalty, 0.3))
```

### Rule 5: Don't cross-correlate trades
If you're long TSLA (consumer disc), don't also long GM and F — they're all auto cycle. Pick the strongest, skip the rest.

## Asset Allocation Targets (paper portfolio)

Default target for a $100K paper account:

| Bucket | Target % | Notes |
|--------|---------|-------|
| Cash | 10-20% | Dry powder for opportunities |
| Stocks (large cap) | 40-60% | Core holdings |
| Stocks (growth/spec) | 10-20% | Higher-conviction tactical |
| Crypto | 5-15% | Capped — high vol |
| Hedges (if used) | 0-5% | VIX calls, put spreads |

**Hard caps**:
- Single stock: 15% (10% in bear regime)
- Single crypto: 10% (5% in bear regime)
- Single sector: 30%
- Crypto total: 20% (15% in bear regime)

## Conflict Resolution

When signals disagree across asset classes:

| Pattern | Action |
|---------|--------|
| Stocks bullish + crypto bearish + macro risk-on | Trust stocks (macro tailwind); skip crypto |
| Stocks bearish + crypto bullish + macro risk-off | Skip both — macro dominates |
| Stocks bullish + crypto bullish + macro risk-on | Full allocation, both asset classes |
| Stocks bearish + crypto bearish + macro risk-off | Heavy cash; defensive only |

## Rebalancing Triggers
- **Position drift > 20% from target** (e.g., 10% target grows to 12%): trim back
- **Asset class drift > 5% from target** (e.g., crypto target 10% grows to 16%): rebalance
- **Stop-loss hit**: exit immediately, journal the loss
- **Thesis broken**: exit even if stop not hit; journal why

## Output Schema

The CTO's final signal blending output:

```json
{
  "scan_id": "2026-06-19_morning",
  "mode": "Base",
  "regime_context": {
    "equities": "bull",
    "macro": "risk-off",
    "combined": "late bull",
    "confidence": 0.65
  },
  "signals": [
    {
      "symbol": "AAPL",
      "asset_class": "stock",
      "action": "buy",
      "raw_confidence": 0.78,
      "macro_adjusted_confidence": 0.68,
      "correlation_discount": 0.03,
      "final_confidence": 0.65,
      "reasoning": "..."
    },
    {
      "symbol": "BTC/USD",
      "asset_class": "crypto",
      "action": "buy",
      "raw_confidence": 0.70,
      "macro_adjusted_confidence": 0.60,
      "funding_penalty": 0.02,
      "final_confidence": 0.58,
      "reasoning": "..."
    }
  ]
}
```
