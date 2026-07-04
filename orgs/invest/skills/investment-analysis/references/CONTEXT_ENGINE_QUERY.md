# CONTEXT_ENGINE_QUERY

**Read this before doing any research.** The world is not ephemeral — earlier pipeline runs have already analyzed data, and you can save time (and avoid duplicate work) by checking what's already done first.

This skill is queryable by the **CTO** via `read_skill`.

## What's Persisted

The investment division writes three kinds of state to disk:

1. **Predictions** (`data/journal.db`) — every signal ever generated, with outcome
2. **Signals** (`data/signals.json`) — current pending/risk-adjusted signals
3. **Memos** (`workspace/memos/`) — research reports, daily summaries

## Setup

```bash
JOURNAL_DB=data/journal.db
MEMOS_DIR=workspace/memos
SIGNALS_FILE=data/signals.json
```

## What's Already Analyzed Today?

Before kicking off a new scan, check:

```bash
# Memos from today
ls -la workspace/memos/ | grep $(date +%Y-%m-%d)

# Last signals.json modification
stat -c "%y" data/signals.json

# Today's predictions in journal
sqlite3 $JOURNAL_DB "SELECT symbol, action, confidence FROM predictions WHERE date(timestamp) = date('now')"
```

## Decision Rules

| Question | If answer is… | Action |
|----------|---------------|--------|
| Has anyone scanned in the last 30 min? | Yes | Skip new scan, reuse signals.json |
| Has anyone scanned in the last 30 min? | No | Run new scan |
| Was there a regime change since last scan? | Yes | Force re-scan regardless of time |
| Did the last scan produce any actionable signals? | No, all sub-0.5 | Skip risk + execution, journal the dry spell |
| Did the last scan produce any actionable signals? | Yes | Run risk + execution |
| Is there a memo from today? | Yes | Read it first, don't duplicate |
| Is there a memo from today? | No | Will be created by research-director |

## Past Predictions (the journal)

The journal is the source of truth for prediction accuracy. Always check it before raising/lowering confidence.

```bash
# Stats from last 30 days
sqlite3 $JOURNAL_DB <<EOF
SELECT
  signal_sources_json,
  COUNT(*) as n,
  AVG(CASE WHEN status = 'won' THEN 1.0 ELSE 0.0 END) as win_rate,
  AVG(confidence) as avg_conf
FROM predictions
WHERE timestamp > datetime('now', '-30 days')
GROUP BY signal_sources_json;
EOF
```

```bash
# Worst-performing symbols (last 90 days)
sqlite3 $JOURNAL_DB <<EOF
SELECT
  symbol,
  COUNT(*) as n,
  AVG(CASE WHEN status = 'won' THEN 1.0 ELSE 0.0 END) as win_rate
FROM predictions
WHERE timestamp > datetime('now', '-90 days')
GROUP BY symbol
HAVING n >= 3
ORDER BY win_rate ASC
LIMIT 10;
EOF
```

## Avoiding Duplicate Analysis

The web MCP server has its own per-domain cache, but it's not perfect. For expensive queries (e.g., SEC filings, on-chain snapshots), the `alt_data.py` script wraps responses:

```bash
# Cache-wrapped web fetch
python3 sandbox/alt_data.py fetch --url "https://www.sec.gov/..." --cache-key "sec_10k_aapl_2025"

# Cache-wrapped research query
python3 sandbox/alt_data.py research --query "AAPL earnings Q2 2026" --cache-key "aapl_news_q2"
```

Cache lives at `.cache/` with content-hash filenames. TTL defaults to 1 hour (configurable via `--ttl`).

## Stop Conditions for Re-analysis

If any of these are true, you MUST re-run the analysis:
1. **Markets moved > 2%** since last scan
2. **News catalyst in last 6 hours** (e.g., FOMC, CPI, earnings, hack)
3. **Regime change** detected (yield curve, DXY, etc.)
4. **Crypto volatility spike** (funding rate flipped sign, OI changed > 20%)

## Memo Format

Memos are markdown files in `workspace/memos/<YYYY-MM-DD>_<topic>.md`:

```markdown
# 2026-06-19 Morning Scan

## Regime
- Equities: Bull (score 62, conf 0.72)
- Macro: Risk-off (DXY 104.2, rising)
- Combined: Late bull

## Signals
| Symbol | Action | Confidence | Composite | Asset |
|--------|--------|-----------|-----------|-------|
| AAPL   | buy    | 0.75      | 0.72      | stock |
| BTC/USD | buy   | 0.68      | 0.65      | crypto |

## Risk Assessment
- Stock heat: 18%
- Crypto heat: 8%
- Combined: 24%
- Status: OK

## Trades Executed
- AAPL: 50 shares @ $220.50, stop $212.00, target $235.00
- BTC/USD: 0.15 BTC @ $68,500, stop $65,000, target $74,000

## Journal
- 2 predictions recorded
- 30-day win rate: 62%
```

## Output Schema

The CTO's context-aware scan decision:

```json
{
  "last_scan_age_minutes": 23,
  "last_scan_signal_count": 4,
  "regime_change_detected": false,
  "market_move_since_scan_pct": 0.3,
  "news_catalyst_last_6h": false,
  "crypto_volatility_spike": false,
  "decision": "skip_rescan_reuse_signals",
  "reason": "Last scan was 23 min ago, no regime change, no catalysts. Reuse data/signals.json.",
  "today_memo_exists": true,
  "today_memo_path": "workspace/memos/2026-06-19_morning.md"
}
```

## Why This Matters

The original failure mode: every scheduled scan ran a full pipeline, re-fetching the same data, re-analyzing the same signals, re-running the same MCP queries. **30% of compute was wasted on duplicate work.**

The fix: query first, work second. Same pattern as `[[CONTEXT_ENGINE_QUERY]]` in deep-research-engine — the world is not ephemeral, so don't pretend it is.

## Related
- [[JOURNAL_PREDICTIONS]] — the prediction journal is the most important persistence layer
- [[MULTI_ASSET_FUSION]] — signal blending uses the journal for calibration
