# JOURNAL_PREDICTIONS

How to record + evaluate predictions. This skill is baked into the **reporter** role prompt.

## Why Journal
**"The world isn't ephemeral."** Every prediction must be recorded BEFORE execution so we can:
1. Measure accuracy over time
2. Identify which signal types are working
3. Tune confidence thresholds
4. Catch systematic biases (always too bullish? too bearish?)

Without a journal, you can't tell if a strategy is good or just lucky.

## Prediction Schema

Every prediction is a JSON record:

```json
{
  "id": "pred_2026-06-19_AAPL_001",
  "timestamp": "2026-06-19T14:30:00Z",
  "scan_id": "2026-06-19_morning",
  "symbol": "AAPL",
  "asset_class": "stock",
  "action": "buy",
  "confidence": 0.75,
  "entry_price": 220.50,
  "stop_price": 212.00,
  "target_price": 235.00,
  "expected_move": {
    "direction": "up",
    "magnitude_pct": 6.6,
    "horizon_days": 10
  },
  "thesis": "Q2 beat + raise + $90B buyback. Above SMA50, MACD bullish cross, MACD histogram expanding.",
  "signal_sources": ["technical", "fundamental", "news"],
  "regime_context": {
    "equities": "bull",
    "macro": "risk-off",
    "mode": "Base"
  },
  "status": "open",
  "outcome": null
}
```

## Status Lifecycle
- `open` — just recorded, awaiting outcome
- `won` — closed at target (or above for buys, below for sells)
- `lost` — closed at stop
- `expired` — horizon passed without hitting target or stop
- `closed_manual` — closed for any other reason (thesis broken, rebalance)

## Evaluation Horizons
- **1 day**: short-term momentum confirmation
- **5 days**: swing trade horizon
- **21 days (1 month)**: standard evaluation
- **63 days (1 quarter)**: fundamental thesis check

## Stats Tracked

### Per Signal Type
- **Technical-only**: signals generated without news/filings/crypto confirmation
- **Multi-signal fusion**: technical + fundamental + sentiment
- **News-driven**: signals where news was the primary catalyst
- **Crypto-onchain**: signals where on-chain was the primary catalyst

### Per Asset Class
- **Stocks**: large cap vs small cap
- **Crypto**: BTC/ETH vs altcoins

### Metrics
- **Win rate**: % of predictions that hit target
- **Average R-multiple**: avg return / risk
- **Profit factor**: gross profit / gross loss
- **Calibration**: actual win rate vs predicted confidence
  - Well-calibrated: 0.7 confidence → ~70% win rate
  - Overconfident: 0.7 confidence → 50% win rate (lower threshold)
  - Underconfident: 0.7 confidence → 85% win rate (raise threshold)

## Calibration Curve

Plot predicted confidence (x) vs actual win rate (y). Perfect calibration = 45° line.

| Predicted confidence | Actual win rate | Action |
|----------------------|-----------------|--------|
| 0.50 | 0.50 | Perfect |
| 0.50 | 0.35 | Lower confidence (overconfident) |
| 0.50 | 0.65 | Raise confidence (underconfident) |
| 0.70 | 0.40 | **Big problem** — signals are noise at this confidence |

## Monthly Review

First Monday of each month, the reporter runs:
1. **30-day accuracy report** — win rate, profit factor, R-multiple per signal type
2. **Calibration curve** — plot predicted vs actual
3. **Worst performers** — top 3 signal types with worst accuracy
4. **Best performers** — top 3 signal types with best accuracy
5. **Recommendation** — tune weights, raise/lower thresholds

## The `journal.py` Script

Located at `plugins/investment-analysis/skills/investment-analysis/scripts/journal.py`. CLI:

```bash
# Record today's signals as predictions
python3 plugins/investment-analysis/skills/investment-analysis/scripts/journal.py record-signals

# Evaluate all open predictions whose horizon has passed
python3 plugins/investment-analysis/skills/investment-analysis/scripts/journal.py evaluate

# Show stats (last 30 days)
python3 plugins/investment-analysis/skills/investment-analysis/scripts/journal.py stats

# Calibration curve
python3 plugins/investment-analysis/skills/investment-analysis/scripts/journal.py calibration

# Per-signal-type breakdown
python3 plugins/investment-analysis/skills/investment-analysis/scripts/journal.py by-type

# Per-asset-class breakdown
python3 plugins/investment-analysis/skills/investment-analysis/scripts/journal.py by-asset

# Generate monthly review
python3 plugins/investment-analysis/skills/investment-analysis/scripts/journal.py monthly-review
```

## Storage
Predictions stored in SQLite at `data/journal.db`:
```sql
CREATE TABLE predictions (
    id TEXT PRIMARY KEY,
    timestamp TEXT,
    scan_id TEXT,
    symbol TEXT,
    asset_class TEXT,
    action TEXT,
    confidence REAL,
    entry_price REAL,
    stop_price REAL,
    target_price REAL,
    expected_move_json TEXT,
    thesis TEXT,
    signal_sources_json TEXT,
    regime_context_json TEXT,
    status TEXT,
    outcome_json TEXT,
    closed_at TEXT,
    close_reason TEXT
);

CREATE INDEX idx_predictions_symbol ON predictions(symbol);
CREATE INDEX idx_predictions_status ON predictions(status);
CREATE INDEX idx_predictions_timestamp ON predictions(timestamp);
```

## Anti-Patterns to Avoid

1. **Hindsight bias**: don't revise predictions after the fact. The journal is immutable once status != open.
2. **Survivorship bias**: count ALL predictions, including ones that didn't execute (e.g., market closed).
3. **Cherry-picking**: don't selectively show winners. Full transparency.
4. **Confusing "won" with "good"**: a winning trade with bad thesis is luck, not skill.
5. **Ignoring losses**: losing trades have MORE signal than winning trades — they reveal blind spots.

## Output Schema (Reporter's summary)

```json
{
  "today_predictions": [
    {"id": "pred_2026-06-19_AAPL_001", "symbol": "AAPL", "action": "buy", "confidence": 0.75},
    {"id": "pred_2026-06-19_BTC-USD_001", "symbol": "BTC/USD", "action": "buy", "confidence": 0.68}
  ],
  "evaluation_30d": {
    "total_predictions": 47,
    "win_rate": 0.62,
    "avg_r_multiple": 1.4,
    "profit_factor": 2.1,
    "by_signal_type": {
      "technical_only": {"count": 18, "win_rate": 0.50, "avg_r": 0.8},
      "multi_signal": {"count": 22, "win_rate": 0.73, "avg_r": 1.8},
      "news_driven": {"count": 7, "win_rate": 0.57, "avg_r": 1.1}
    },
    "by_asset_class": {
      "stocks": {"count": 35, "win_rate": 0.66, "avg_r": 1.5},
      "crypto": {"count": 12, "win_rate": 0.50, "avg_r": 1.1}
    }
  },
  "calibration": {
    "0.5_confidence_actual": 0.48,
    "0.6_confidence_actual": 0.55,
    "0.7_confidence_actual": 0.68,
    "0.8_confidence_actual": 0.82,
    "verdict": "well_calibrated"
  },
  "yellow_flags": [
    "Crypto win rate at 50% — consider raising crypto threshold to 0.65"
  ],
  "monthly_review_next": "2026-07-06"
}
```
