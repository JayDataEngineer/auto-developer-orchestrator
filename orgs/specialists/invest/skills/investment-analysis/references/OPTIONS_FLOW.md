# OPTIONS_FLOW

Unusual options activity + dark pool flow via web MCP. This skill is queryable by the **CTO** via `read_skill` for ad-hoc analysis.

## What It Tells You
Options flow reveals **where smart money (or large risk-takers) is positioning**. A sudden huge call buy on a stock often precedes a move up; same for puts and moves down. Dark pool flow reveals large institutional trades happening off-exchange.

## Free Sources

### unusualwhales.com (free tier)
- News + flow summaries
- Scraped via web MCP:
```python
mcp__web__research(query="site:unusualwhales.com $SYMBOL options flow", max_results=3)
```

### barchart.com (free tier, limited)
- Unusual options activity
```python
mcp__web__scrape(url="https://www.barchart.com/options/unusual-activity/stocks")
```

### Market Chameleon (free, requires registration)
- Volatility, expected moves
```python
mcp__web__scrape(url="https://marketchameleon.com/Overview/$SYMBOL/OptionActivity/")
```

### Yahoo Finance (free)
- Options chain with OI + volume
```python
mcp__web__scrape(url="https://finance.yahoo.com/quote/$SYMBOL/options")
```

### Finviz (free)
- News + dark pool hints in headlines
```python
mcp__web__scrape(url="https://finviz.com/quote.ashx?t=$SYMBOL")
```

### Quiver Quant (free, dark pool + flow)
- Trades > $100K
```python
mcp__web__scrape(url="https://quiverquant.com/source/darkpool")
```

## Web MCP Patterns

### Recent unusual activity
```python
mcp__web__research(
    query="$SYMBOL unusual options activity call put today",
    max_results=5,
    time_filter="day"
)
```

### Earnings options positioning
```python
mcp__web__research(
    query="$SYMBOL options implied move earnings next week",
    max_results=3
)
```

### Dark pool prints
```python
mcp__web__research(
    query="$SYMBOL dark pool prints last 5 days size",
    max_results=3
)
```

## Reading Options Flow

### Order Size Matters
| Size (premium) | Interpretation |
|----------------|---------------|
| < $25K | Retail noise — ignore |
| $25K - $100K | Small institutional — minor signal |
| $100K - $500K | **Unusual** — meaningful |
| $500K - $2M | **Very unusual** — high signal |
| > $2M | **Whale** — extreme signal |

### Flow Types
- **Sweep**: order routed across multiple exchanges quickly. High urgency = strong conviction.
- **Block**: large single order at one exchange. Often a hedge, not directional.
- **Spread**: combination (call spread, put spread). Lower conviction.
- **Rolled**: closing one position and opening another (rolling). Neutral directional.

### Call vs Put
- **Calls bought (ask side)**: bullish bet
- **Puts bought (ask side)**: bearish bet
- **Calls sold (bid side)**: neutral-to-bearish (covered call, short call)
- **Puts sold (bid side)**: neutral-to-bullish (cash-secured put, short put)

### IV Rank Context
Options flow signals matter more when IV Rank is **low** (cheaper premium = smart money expecting a move).

- IV Rank < 30: Flow is cheap to replicate → more meaningful signal
- IV Rank > 70: Flow is expensive → mostly noise from earnings/FOMC

## Dark Pool Flow

Dark pools are private exchanges where institutions trade large blocks without moving the public price.

- **Dark pool buy prints** at a premium to current price = bullish (institutions willing to pay up)
- **Dark pool sell prints** at a discount = bearish
- **Volume surge** (much larger than typical) = "smart money positioning"

## Common Patterns

### Bullish Setup: "Call Sweep"
- Large call sweep, premium > $500K
- IV Rank < 50
- OI doubles or triples on the strike
- Stock near support level

### Bearish Setup: "Put Wall"
- Large put purchase, premium > $500K
- Strike well below current price
- OI builds at that strike
- Catalyst: pending earnings, regulatory event

### Gamma Squeeze
- Heavy call buying → market makers short calls → must buy underlying to hedge → price rises → more calls → feedback loop
- Requires: low float + high call OI + bullish catalyst

### Earnings Pin
- Max pain theory: stock tends to gravitate to the strike where most options expire worthless
- Useful for predicting earnings day close

## How to Use in Signals

Options flow should be a **confirmation overlay** on technical + fundamental signals, not a primary signal:

```python
def options_flow_adjustment(flow_data, base_signal):
    """Adjust confidence based on options flow confirmation."""
    if flow_data is None:
        return base_signal  # neutral

    # Bullish flow confirms bullish signal
    if flow_data.bullish_premium_24h > 1_000_000 and base_signal.action == "buy":
        return base_signal.adjust_confidence(+0.10)

    # Bearish flow confirms bearish signal
    if flow_data.bearish_premium_24h > 1_000_000 and base_signal.action == "sell":
        return base_signal.adjust_confidence(+0.10)

    # Conflicting flow → reduce confidence
    if (flow_data.bullish_premium_24h > 500_000 and base_signal.action == "sell") or \
       (flow_data.bearish_premium_24h > 500_000 and base_signal.action == "buy"):
        return base_signal.adjust_confidence(-0.15)

    return base_signal
```

## Pitfalls
- **Hedges**: large put buys are often hedge for long stock, not bearish bet
- **Market makers**: their flow is mechanical, not directional
- **Wrong direction**: a "call sweep" can be closing a short call position (bullish close, not bullish open)
- **Old news**: flow from yesterday is already priced in
- **Small caps**: easily manipulated by single whale

## Output Schema

```json
{
  "symbol": "AAPL",
  "options_flow_summary": {
    "bullish_premium_24h": 1_850_000,
    "bearish_premium_24h": 420_000,
    "largest_trades": [
      {"type": "call_sweep", "strike": 230, "expiry": "2026-07-19", "premium": 850_000, "side": "ask"},
      {"type": "put_block", "strike": 200, "expiry": "2026-06-30", "premium": 380_000, "side": "ask"}
    ],
    "iv_rank": 42,
    "implied_move_30d_pct": 5.2,
    "put_call_ratio": 0.68
  },
  "dark_pool_summary": {
    "net_flow_5d_usd": 12_400_000,
    "largest_print": {"size": 850_000, "price_pct_vs_close": 0.4, "side": "buy"}
  },
  "gamma_setup": "neutral",
  "signal_impact": "confirms_bullish"
}
```
