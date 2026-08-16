# NEWS_SENTIMENT

How to gather + score news sentiment via web MCP. This skill is baked into the **news-analyst** role prompt.

## Web MCP Patterns

### Top tool: `web_research_research`
Searches + scrapes top results in one call. Returns title, URL, snippet, AND page content (capped at ~5K chars per result). Default for 90% of queries.

### Single-symbol news scan
```python
web_research_research(
    query="$SYMBOL stock news today earnings upgrade",
    max_results=5,
    depth="quick"  # or "deep" for ~20 results
)
```

### Structured extraction (news schema)
For a specific article URL, `web_research_fetch` returns the full page content — pull the fields from it yourself:
```python
web_research_fetch(url="https://finance.yahoo.com/news/...")
# Fields to extract from the page content: headline, author, date, content summary, category
```

### News by sector/theme
```python
web_research_research(query="semiconductor stocks news TSMC NVDA AMD", max_results=5)
web_research_research(query="AI infrastructure spending datacenter REITs", max_results=5)
web_research_research(query="Fed interest rate decision next FOMC", max_results=3)
```

### Time-filtered news
```python
web_research_search(
    query="$SYMBOL earnings",
    time_filter="day",  # last 24h
    top_k=10
)
```

### Macro / cross-asset
```python
web_research_research(query="Federal Reserve interest rate decision this week", max_results=3)
web_research_research(query="SEC crypto regulation news ETF approval", max_results=3)
web_research_research(query="China GDP trade war impact stocks", max_results=3)
```

## Source Authority Tiers

### Tier 1 (high — primary signal)
- WSJ, Bloomberg, Reuters, FT, The Economist
- SEC filings, company IR pages, official earnings releases
- Federal Reserve, Treasury, BLS statements
- Nature, Science, NEJM (for biotech)

### Tier 2 (medium — confirmation)
- Yahoo Finance, MarketWatch, CNBC, Barron's
- Seeking Alpha, Motley Fool (with skepticism)
- The Information, Axios Pro (industry)

### Tier 3 (low — sentiment only)
- Reddit, StockTwits, X/Twitter, Discord
- Substack newsletters
- Random blogs

**Rule**: For signal-changing claims (earnings miss, fraud, M&A), require 2 Tier 1 sources OR 1 Tier 1 + 2 Tier 2.

## Sentiment Scoring Rubric

For each asset, output a sentiment score from **-1.0** (very bearish) to **+1.0** (very bullish).

| Score Range | Conditions |
|-------------|-----------|
| **+0.8 to +1.0** | Multiple Tier 1 sources reporting bullish catalyst (beat+raise, FDA approval, major partnership, ETF approval) |
| **+0.3 to +0.7** | Generally positive coverage, no red flags. Solid quarter, positive analyst commentary. |
| **-0.3 to +0.3** | Mixed or neutral. Equal bullish + bearish coverage, or no material news. |
| **-0.7 to -0.4** | Generally negative coverage, material concerns. Missed earnings, guidance cut, downgrade. |
| **-1.0 to -0.8** | Multiple Tier 1 sources reporting bearish catalyst (fraud, SEC investigation, going concern, hack for crypto, delisting). |

## Catalyst Categories (high-signal events)

### Earnings (most predictable)
- **Beat + Raise** (revenue + EPS beat + guidance raised): +0.6 to +0.9
- **Beat + Maintain**: +0.2 to +0.4
- **Miss + Maintain**: -0.2 to -0.4
- **Miss + Cut**: -0.6 to -0.9
- **Pre-announcement** (early disclosure of miss): -0.5 to -0.8

### Product / Approval
- FDA approval (Phase 3 / NDA): +0.7 to +0.9
- FDA rejection / CRL: -0.7 to -0.9
- Major product launch with positive reviews: +0.3 to +0.6
- Product recall: -0.3 to -0.6

### M&A / Corporate Actions
- Acquired at premium: +0.5 to +0.8 (for target)
- Acquirer announcement: -0.1 to -0.3 (usually)
- Dividend cut: -0.4 to -0.6
- Dividend initiation: +0.2 to +0.4
- Stock split: 0.0 to +0.2 (cosmetic, sentiment only)
- Buyback announcement: +0.2 to +0.4

### Management / Governance
- CEO departure (unexpected): -0.4 to -0.7
- Activist investor 13D filing: +0.2 to +0.4 (if agitating for value)
- Accounting restatement: -0.5 to -0.8
- SEC investigation: -0.7 to -0.9

### Crypto-specific
- ETF approval (BTC/ETH): +0.5 to +0.8
- ETF rejection: -0.4 to -0.6
- Exchange hack (> $100M): -0.7 to -0.9
- Regulatory crackdown (SEC enforcement): -0.5 to -0.7
- Major partnership (Visa, Mastercard, big bank): +0.3 to +0.5
- Halving (BTC): +0.1 to +0.3 (long-term supply narrative)

## Recency Decay
News value decays. Weight by recency:
| Age | Weight |
|-----|--------|
| < 6 hours | 1.0 |
| 6-24 hours | 0.8 |
| 24-48 hours | 0.5 |
| > 48 hours | 0.2 |

**Rule**: At least 1 source from the last 6 hours for the sentiment score to be high-confidence.

## Conflict Resolution
When sources disagree:
- **2 Tier 1 sources vs 3 Tier 3 sources**: trust Tier 1
- **Recent (today) vs old (last week)**: weight recent
- **Official (IR page) vs third-party (Bloomberg)**: trust official

## Common Pitfalls
- **Headlines vs body**: headline is sensational, body is nuanced. Always read 1-2 paragraphs.
- **Sell-side upgrades are momentum, not fundamental**: banks upgrade after the stock runs up. Don't chase.
- **Pre-market moves ≠ closing direction**: futures spike on news, then fade. Wait for cash open.
- **"According to sources"**: unnamed sources can be wrong. Treat as rumor until confirmed.

## Output Schema

```json
{
  "symbol": "AAPL",
  "asset_class": "stock",
  "sentiment_score": 0.62,
  "confidence": 0.75,
  "key_events": [
    {
      "headline": "Apple Q2 Beats Estimates, Raises Full-Year Guidance",
      "source": "Reuters",
      "tier": 1,
      "age_hours": 4,
      "url": "https://...",
      "category": "earnings_beat_raise"
    },
    {
      "headline": "Apple Announces $90B Buyback",
      "source": "Bloomberg",
      "tier": 1,
      "age_hours": 4,
      "url": "https://...",
      "category": "buyback"
    }
  ],
  "analyst_actions": [
    {"bank": "Morgan Stanley", "action": "upgrade", "from": "Equal-weight", "to": "Overweight", "target": 240}
  ],
  "signal_impact": "reinforces_technical",
  "red_flags": []
}
```
