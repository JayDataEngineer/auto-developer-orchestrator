# CRYPTO_ONCHAIN

On-chain metrics, funding rates, exchange flows. This skill is baked into the **crypto-analyst** role prompt.

## Free On-Chain Data Sources

### Blockchain.com (free, no auth)
- Address activity: `https://blockchain.info/rawaddr/{ADDRESS}`
- Latest block: `https://blockchain.info/latestblock`
- Unconfirmed transactions: `https://blockchain.info/unconfirmed-transactions?format=json`

### Blockchair.com (free tier)
- Multi-chain explorer: `https://blockchair.com/bitcoin/transactions`
- Address lookup: `https://api.blockchair.com/bitcoin/dashboards/address/{ADDRESS}`

### Glassnode (free tier, limited)
- Sign up at glassnode.com for free metrics. Available without subscription:
  - Active addresses
  - SOP (Spent Output Profit)
  - Exchange netflow (delayed)

### Binance public API (no auth required for public endpoints)
- Funding rates: `https://fapi.binance.com/fapi/v1/premiumIndex?symbol=BTCUSDT`
- Open interest: `https://fapi.binance.com/fapi/v1/openInterest?symbol=BTCUSDT`
- Order book: `https://api.binance.com/api/v3/depth?symbol=BTCUSDT&limit=100`

### Etherscan API (free with key)
- Gas tracker: `https://api.etherscan.io/api?module=gastracker&action=gasoracle&apikey={KEY}`
- ETH supply: `https://api.etherscan.io/api?module=stats&action=ethsupply&apikey={KEY}`

### Coinglass (free, scrapes funding/OI aggregates)
```python
web_research_fetch(url="https://www.coinglass.com/FundingRate")
web_research_fetch(url="https://www.coinglass.com/OpenInterest")
```

## Web MCP Patterns

### Funding rate snapshot
```python
web_research_fetch(url="https://fapi.binance.com/fapi/v1/premiumIndex?symbol=BTCUSDT")
# Returns: {"symbol":"BTCUSDT","markPrice":...,"lastFundingRate":"0.00010000"...}
```

### Open interest
```python
web_research_fetch(url="https://fapi.binance.com/fapi/v1/openInterest?symbol=BTCUSDT")
```

### Exchange reserve trends (Glassnode public scrapes)
```python
web_research_research(query="Bitcoin exchange reserves outflow inflow this week", max_results=3)
web_research_fetch(url="https://glassnode.com/charts/exchanges/balance")  # may need login
```

### Whale alerts
```python
web_research_search(query="whale alert Bitcoin large transfer exchange last 24h", top_k=5)
```

### ETF flows (BTC/ETH)
```python
web_research_research(query="Bitcoin ETF flows IBIT GBTC last week inflow outflow", max_results=3)
web_research_research(query="Ethereum ETF flows BlackRock Franklin last week", max_results=3)
```

## Metrics Reference

### Active Addresses
- **BTC**: 1M+ daily active addresses = healthy network
- **ETH**: 500K+ daily = healthy
- **Trend matters more than level**: 30-day MA rising = adoption

### Transaction Volume (adjusted)
- USD value of on-chain transactions, excluding internal wallet shuffles
- Rising volume + rising price = bull confirmation
- Rising volume + falling price = distribution

### Exchange Netflow
- **Negative netflow** (outflow): coins leaving exchanges → accumulation → bullish
- **Positive netflow** (inflow): coins moving to exchanges → distribution → bearish
- **3-day MA is more reliable than daily** (smooths noise)

### Funding Rates (perpetual futures)
- **Positive funding** (> 0.01% per 8h): longs pay shorts = bullish leverage, long squeeze risk if funding > 0.05%
- **Negative funding** (< -0.01% per 8h): shorts pay longs = bearish leverage, short squeeze risk
- **Annualized**: multiply 8h rate × 3 × 365

### Open Interest (OI)
- Total outstanding futures contracts
- **Rising OI + rising price** = new longs entering, trend continuation
- **Rising OI + falling price** = new shorts entering, downtrend continuation
- **Falling OI + rising price** = short covering, weaker rally
- **Falling OI + falling price** = long unwinding, weaker selloff

### Stablecoin Supply Ratio (SSR)
- BTC market cap / stablecoin supply
- High SSR = stablecoin buying power low (less fuel for rally)
- Low SSR = stablecoin buying power high (fuel for rally)

### MVRV Z-Score
- Market Value / Realized Value, normalized
- > 7 = market top historically
- < 0.1 = market bottom historically

### SOP (Spent Output Profit)
- > 1: coins being sold at profit
- < 1: coins being sold at loss (capitulation)

## Composite On-Chain Score

```python
def onchain_score(metrics):
    score = 0

    # Exchange flow (-0.3 to +0.3)
    if metrics.exchange_netflow_3d < -5000:  # BTC outflow > 5000
        score += 0.3
    elif metrics.exchange_netflow_3d > 5000:  # inflow
        score -= 0.3

    # Active addresses trend (-0.2 to +0.2)
    if metrics.active_addresses_30d_trend > 0.05:
        score += 0.2
    elif metrics.active_addresses_30d_trend < -0.05:
        score -= 0.2

    # Funding (-0.2 to +0.2)
    # Negative funding = contrarian bullish for longs
    if metrics.funding_8h < -0.0001:
        score += 0.2
    elif metrics.funding_8h > 0.0005:
        score -= 0.15  # overleveraged longs
    elif metrics.funding_8h > 0.001:
        score -= 0.25  # squeeze risk

    # MVRV (-0.3 to +0.3)
    if metrics.mvrv_zscore < 0.1:
        score += 0.3  # historically a bottom
    elif metrics.mvrv_zscore > 7:
        score -= 0.3  # historically a top

    return max(-1.0, min(1.0, score))
```

## Squeeze Detection
Long squeeze setup:
- Funding rate > 0.05% per 8h (overleveraged longs)
- OI at multi-week high
- Price extended (RSI > 70, far above VWAP)
- Catalyst: negative news can trigger cascade

Short squeeze setup:
- Funding rate < -0.01% per 8h (overleveraged shorts)
- OI at multi-week high
- Price compressed (BB squeeze)
- Catalyst: positive news can trigger cascade

## Multi-Asset Context
- **DXY inverse correlation** (-0.4 historical): strong dollar = crypto bearish
- **10Y yield inverse**: rising yields = crypto bearish (risk-off)
- **Fed balance sheet positive**: liquidity expansion = crypto bullish
- **VIX inverse**: VIX > 30 = panic selling across risk assets including crypto
- **Tech stocks (NDX)**: positive correlation (~0.4-0.5 with BTC)

## Pitfalls
- **Stale data**: on-chain data has 10-60 min lag. Use price action for real-time.
- **Whale wallets aren't always whales**: exchanges, custodians, ETFs hold large balances.
- **Funding rates vary by exchange**: Binance is the reference; others can be off.
- **"Hack" headlines may be fake**: verify with Tier 1 sources before pricing in.
- **On-chain ≠ fundamental**: speculative frenzy shows up as "adoption" on-chain.

## Output Schema

```json
{
  "symbol": "BTC/USD",
  "onchain_score": 0.45,
  "confidence": 0.70,
  "metrics": {
    "active_addresses_24h": 1050000,
    "active_addresses_30d_trend": 0.04,
    "exchange_netflow_3d_btc": -3200,
    "funding_8h_binance": 0.00007,
    "funding_annualized": 0.077,
    "open_interest_usd": 28_500_000_000,
    "oi_trend_7d": "rising",
    "mvrv_zscore": 1.8,
    "stablecoin_supply_ratio": 8.4,
    "sop_ratio": 1.12
  },
  "whale_alerts": [
    {"size_usd": 250_000_000, "direction": "exchange_outflow", "time": "2026-06-19T14:23Z"}
  ],
  "etf_flows_7d_usd": 480_000_000,
  "squeeze_setup": "none",
  "signal_impact": "confirms_technical_bullish"
}
```
