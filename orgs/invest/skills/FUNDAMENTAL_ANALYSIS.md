# FUNDAMENTAL_ANALYSIS

How to read SEC filings and fundamental data. This skill is baked into the **filings-analyst** role prompt.

## The 5-Dimension Rubric

Score each dimension 0-100, weight as shown, sum for the fundamental score.

### 1. Valuation (20%)
| Metric | What it tells you | Good | Bad |
|--------|-------------------|------|-----|
| PE (trailing) | Price per dollar of current earnings | < sector avg | > sector avg |
| Forward PE | Price per dollar of forward earnings | < trailing PE = growing | > trailing PE = decelerating |
| PEG | PE / earnings growth rate | < 1.0 (undervalued) | > 2.0 (expensive) |
| EV/EBITDA | Enterprise value / cash earnings | < 10 | > 15 |
| Price/Book | Price / net asset value | < 3 (capital-intensive ok) | > 10 (brand-driven) |

**Red flag**: PE > 100 with no growth narrative = speculative.

### 2. Profitability (20%)
| Metric | Good | Bad |
|--------|------|-----|
| Gross margin | > 40% (software) / > 20% (most) | < 10% (commodity) |
| Operating margin | > 20% | < 5% |
| Net margin | > 15% | < 5% |
| ROE | > 15% | < 8% |
| ROIC | > WACC + 5% | < WACC |

**Trend matters more than level**: 15% margin growing to 18% is bullish; 25% margin falling to 22% is bearish.

### 3. Growth (25%) — most heavily weighted
| Metric | Good | Bad |
|--------|------|-----|
| Revenue growth YoY | > 20% | < 5% (mature) / negative |
| Revenue growth QoQ | > 5% | < 0% |
| EPS growth YoY | > 25% | < 5% |
| FCF growth | > 15% | flat / declining |
| Guidance raise | Beat + raise (sequential) | Cut guidance |

**The "Rule of 40"** (SaaS): revenue growth % + FCF margin % should be > 40.

### 4. Balance Sheet (20%)
| Metric | Good | Bad |
|--------|------|-----|
| Debt/Equity | < 1.0 | > 2.5 |
| Current ratio | > 1.5 | < 1.0 |
| Cash position | > 6 months opex | burning cash |
| Interest coverage | > 5x EBIT | < 2x EBIT |
| FCF / Debt | > 20% | < 5% |

**Red flag**: "Going concern" language in 10-K = automatic cap at 30.
**Red flag**: Debt covenant violation disclosed = cap at 25.

### 5. Capital Return (15%)
| Signal | Bullish | Bearish |
|--------|---------|---------|
| Buyback trend | Increasing, well-timed | Suspended or poorly timed (at all-time highs) |
| Dividend | Growing, low payout ratio (<50%) | Cut, high payout (>80%) |
| Insider buying | Cluster buying (3+ in 90 days) | Cluster selling |

## Where to Find the Data

### SEC EDGAR (primary source)
- **10-K**: Annual report. Read Item 1A (risk factors), Item 7 (MD&A), Item 8 (financials).
- **10-Q**: Quarterly report. Focus on YoY changes and any "subsequent events".
- **8-K**: Material event. Filed within 4 days of events like CEO change, M&A, restatement.
- **DEF 14A**: Proxy statement. Executive comp + board composition.
- **Form 4**: Insider trading. Filed within 2 days of transaction.

### Earnings Reports
- **8-K with earnings press release** (Item 2.02): the official results
- **Earnings call transcript**: forward guidance, Q&A reveals sentiment

### Analyst Notes (secondary)
- **Upgrade/downgrade**: tier 1 only (Goldman, Morgan Stanley, JPM)
- **Price target changes**: direction matters more than level
- **Estimate revisions**: trend in EPS estimates is more predictive than the print itself

## Reading a 10-K (10-minute scan)

1. **Item 1A Risk Factors** (5 min) — Ctrl+F for these phrases:
   - "going concern" → fundamental cap 30
   - "material weakness" → cap at 40, investigate
   - "restated" → cap at 35
   - "cybersecurity incident" → case-by-case
   - "loss of key customer" → reduce growth score by 20
2. **Item 7 MD&A** (3 min) — management's narrative. Look for honesty about challenges.
3. **Item 8 Financials** (2 min) — balance sheet, cash flow statement.

## Common Manipulation Patterns
- **Channel stuffing**: receivables growing faster than revenue = fake sales
- **Capitalizing opex**: D&A growing much faster than revenue = hiding expenses
- **Stock-based comp**: SaaS companies often exclude this from "non-GAAP" earnings — always add it back
- **Off-balance-sheet**: "variable interest entities" = hidden debt

## Output Schema

For each ticker, the filings-analyst returns:

```json
{
  "symbol": "AAPL",
  "fundamental_score": 78,
  "dimensions": {
    "valuation": {"score": 75, "notes": "PE 28 vs sector 22, but PEG 1.1 with 25% growth"},
    "profitability": {"score": 92, "notes": "30% net margin, ROE 150% (cash heavy)"},
    "growth": {"score": 70, "notes": "Revenue +11% YoY, slowing but stable"},
    "balance_sheet": {"score": 88, "notes": "$160B cash, debt/equity 1.5 but serviceable"},
    "capital_return": {"score": 65, "notes": "$90B buyback authorized, div growing 5%/yr"}
  },
  "latest_filings": {
    "10-K": "2025-11-01 — no red flags",
    "10-Q": "2026-02-15 — beat+raise",
    "8-K": "2026-03-01 — CEO succession announced"
  },
  "analyst_consensus": {
    "avg_target": 245,
    "current_price": 220,
    "upside_pct": 11.4,
    "recent_actions": "2 upgrades, 0 downgrades last 30d"
  },
  "insider_activity": {
    "net_90d": "-15M (selling)",
    "cluster_signal": "none"
  },
  "red_flags": []
}
```
