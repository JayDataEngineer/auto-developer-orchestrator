# SEC_FILINGS

How to read SEC EDGAR via web MCP. This skill is baked into the **filings-analyst** role prompt.

## EDGAR URLs

### Company Filing Index
```
https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK={CIK}&type={TYPE}&dateb=&owner=include&count=10
```

- `CIK`: Central Index Key (numeric, e.g., `0000320193` for AAPL). Lookup by ticker:
  ```
  https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&company=aapl&output=atom
  ```
- `TYPE`: `10-K`, `10-Q`, `8-K`, `DEF 14A`, `4` (insider), `S-1` (IPO), `SC 13D` (activist)

### Direct Document URL
```
https://www.sec.gov/Archives/edgar/data/{CIK}/{ACCESSION_NO_NO_DASHES}/{DOC_FILENAME}
```

### Full-Text Search
```
https://efts.sec.gov/LATEST/search-index?q=%22going+concern%22&dateRange=custom&startdt=2026-01-01&enddt=2026-06-19
```

## Web MCP Patterns

### Get list of recent 10-Ks for a company
```python
web_research_fetch(url="https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=0000320193&type=10-K&dateb=&owner=include&count=10")
```

### Read a specific 10-K (full text)
```python
web_research_fetch(url="https://www.sec.gov/Archives/edgar/data/320193/000032019326000010/aapl-20260926.htm")
```

### Search EDGAR full-text for risk language
```python
web_research_fetch(url="https://efts.sec.gov/LATEST/search-index?q=%22going+concern%22&forms=10-K&dateRange=custom&startdt=2026-01-01")
```

### Enumerate a company's full filing history
The armed web_research surface (search/fetch/research) has no crawler or sitemap-mapper — enumerate filing URLs via EDGAR full-text search, then read each document:
```python
web_research_fetch(url="https://efts.sec.gov/LATEST/search-index?q=&forms=10-K&dateRange=custom&startdt=2026-01-01")
# then per filing URL from the results:
web_research_fetch(url="https://www.sec.gov/Archives/edgar/data/<CIK>/<acc-no>/<primary-doc>.htm")
```

### Discover EDGAR URLs in bulk
Same discovery path — EDGAR full-text search per form type (10-K, 10-Q, 8-K, 4), then fetch each hit. There is no domain-mapper in the armed surface:
```python
web_research_fetch(url="https://efts.sec.gov/LATEST/search-index?q=<company>&forms=10-Q&dateRange=custom&startdt=2026-01-01")
```

## Reading Order (per filing type)

### 10-K (annual)
1. **Item 1A Risk Factors** — Ctrl+F for red-flag phrases:
   - "going concern", "material weakness", "restated", "subsequent event"
2. **Item 7 MD&A** (Management Discussion & Analysis) — management's narrative
3. **Item 8 Financial Statements** — focus on:
   - Balance sheet: cash, debt, working capital
   - Income statement: revenue trend, margins
   - Cash flow: OCF vs net income (divergence = quality issue)
   - Footnotes: revenue recognition, segment reporting

### 10-Q (quarterly)
1. **Item 2 MD&A** — quarter-over-quarter changes
2. **Item 3 Quantitative and Qualitative Disclosures About Market Risk**
3. **Item 4 Controls and Procedures** — disclosure of any material weakness
4. **Subsequent events** note (in footnotes)

### 8-K (material event)
Read Items 2.02 (earnings), 5.02 (officer change), 8.01 (other events), 9.01 (financial statements).

### Form 4 (insider trading)
- **Transaction code P**: open market purchase (most bullish)
- **Transaction code S**: open market sale (most bearish)
- **Code A**: grant of equity (neutral)
- **Code M**: exercise of options (slightly bearish if immediately sold)

## CIK Lookup

```
web_research_fetch(url="https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&company=apple&output=atom")
```

Returns XML with CIK. Parse the `<cik>` tag.

Or hardcode common CIKs:
| Ticker | CIK |
|--------|-----|
| AAPL | 0000320193 |
| MSFT | 0000789019 |
| GOOGL | 0001652044 |
| AMZN | 0001018724 |
| META | 0001326801 |
| TSLA | 0001318605 |
| NVDA | 0001045810 |
| BRK.B | 0001067983 |

## Rate Limits & User-Agent
SEC requires a User-Agent header for EDGAR requests. The web MCP server handles this automatically, but if scraping manually:
```
User-Agent: "Invest Research Bot contact@example.com"
```

Rate limit: 10 requests/second per IP. Web MCP handles throttling.

## Caching Strategy
SEC filings don't change after filing. The `alt_data.py` script caches filings by URL+content-hash so re-analyzing the same filing is free. Cache location: `.cache/sec_filings/`.

## Earnings Reports

### Official Press Release (8-K Item 2.02)
```
web_research_research(query="$SYMBOL Q earnings release site:investors.$SYMBOL.com OR site:ir.$SYMBOL.com", max_results=3)
```

### Earnings Call Transcript
```
web_research_research(query="$SYMBOL Q earnings call transcript", max_results=3)
```

### Guidance Update
Look for change in full-year guidance:
- **Raised guidance**: bullish, often +5-10% to stock next day
- **Maintained guidance**: neutral
- **Lowered guidance**: bearish, often -5-15% next day

## Common Pitfalls
- **10-K risk factors are boilerplate** — every company lists the same generic risks. Look for company-specific NEW risks added this year.
- **Non-GAAP adjustments** — companies exclude "one-time" items that recur. Always reconcile to GAAP.
- **Stock-based compensation** — SaaS companies often exclude this. Always add back to get true earnings.
- **Pension adjustments** — interest rate changes inflate/deflate pension expense; look through it.
