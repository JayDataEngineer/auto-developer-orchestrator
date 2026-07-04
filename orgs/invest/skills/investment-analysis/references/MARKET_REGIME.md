# MARKET_REGIME

4-pillar equities regime detection. This skill is baked into the **regime-analyst** role prompt.

## The 4 Pillars

Each pillar scores 0-100. Weighted sum gives the combined regime score.

### 1. Trend (weight: 0.35)
| Component | Score contribution |
|-----------|-------------------|
| SPY price vs SMA50 | Above = +30, below = -30, near = 0 |
| SPY price vs SMA200 | Above = +40, below = -40 |
| Golden cross (50D > 200D) | +30, death cross = -30 |
| SMA50 slope (rising/falling) | Rising = +10, falling = -10 |

**Trend score**: sum, clamped to 0-100.

### 2. Volatility (weight: 0.25)
| VIX Level | Score |
|-----------|-------|
| < 15 | 95 (low vol, complacent) |
| 15-20 | 80 |
| 20-25 | 60 |
| 25-30 | 40 |
| 30-35 | 20 |
| > 35 | 10 (panic — usually a bottom) |

**VIX trend adjustment**:
- VIX rising (last 5 days): -10
- VIX falling (last 5 days): +10

### 3. Momentum (weight: 0.25)
| Metric | Score |
|--------|-------|
| SPY 20D ROC | ROC > 2% = +30, < -2% = -30 |
| SPY 50D ROC | ROC > 5% = +40, < -5% = -40 |
| Advance/decline ratio (5D avg) | > 1.3 = +30, < 0.7 = -30 |
| New highs vs new lows | > 100 new highs = +20, > 100 new lows = -20 |

### 4. Breadth (weight: 0.15)
| Metric | Score |
|--------|-------|
| % SPX above 200D MA | > 70% = +50, < 30% = -50 |
| % SPX above 50D MA | > 70% = +30, < 30% = -30 |
| Cumulative advance/decline line trend | Rising = +20, falling = -20 |

## Combined Score → Regime

| Score | Regime | Confidence |
|-------|--------|-----------|
| > 70 | Strong bull | > 0.8 |
| 55-70 | Bull | 0.6-0.8 |
| 45-55 | Sideways (bullish lean) | 0.4-0.6 |
| 35-45 | Sideways (bearish lean) | 0.4-0.6 |
| 20-35 | Bear | 0.6-0.8 |
| < 20 | Strong bear | > 0.8 |

**Confidence < 0.4** = uncertain regime → switch to Conservative mode automatically.

## Regime Parameters

| Regime | position_size_mult | stop_atr_mult |
|--------|-------------------|---------------|
| Strong bull | 1.3 | 2.5 |
| Bull | 1.2 | 2.0 |
| Sideways | 1.0 | 1.5 |
| Bear | 0.7 | 1.0 |
| Strong bear | 0.5 | 1.0 |
| Uncertain | 0.5 | 1.0 |

## Combined with Macro Regime

See [[MACRO_ANALYSIS]] for the macro regime. The combined regime is a 2D matrix:

| Equities \ Macro | Risk-On | Neutral | Risk-Off |
|------------------|---------|---------|----------|
| Bull | Strong bull (size up) | Bull (normal) | Late bull (trim) |
| Sideways | Confused (reduce freq) | Sideways (normal) | Risk-off sideways (defensive) |
| Bear | Capitulation (watch bottom) | Bear (normal) | Strong bear (defensive only) |

## Common Patterns
- **V-shaped bottom**: VIX > 35 + RSI < 30 on SPY + breadth washed out (< 20% above 50D) = often a bottom within 5 days
- **Blow-off top**: VIX < 12 + extreme bullish sentiment + narrow leadership (only mega-cap up) = top forming
- **Trend change**: SMA50 slope flips, then golden/death cross follows within 20-50 days

## Output Schema

```json
{
  "equities_regime": {
    "score": 62,
    "regime": "bull",
    "confidence": 0.72,
    "pillars": {
      "trend": {"score": 75, "components": {...}},
      "volatility": {"score": 60, "vix": 18.5, "vix_trend": "rising"},
      "momentum": {"score": 55, "spy_20d_roc": 0.012},
      "breadth": {"score": 50, "pct_above_200d": 0.62}
    }
  },
  "regime_params": {
    "position_size_mult": 1.2,
    "stop_atr_mult": 2.0
  }
}
```
