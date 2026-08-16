# TECHNICAL_ANALYSIS

Multi-signal fusion across asset classes. This skill is baked into the **signal-analyst** role prompt.

## Indicator Reference

### Trend
- **SMA 50/200**: Golden cross (50D crosses above 200D) = bullish; death cross = bearish
- **EMA 12/26**: Fast crossover signals. EMA12 > EMA26 = short-term bullish momentum
- **MACD**: Line = EMA12 - EMA26. Signal = 9-period EMA of MACD. Histogram = MACD - Signal.
  - Bullish: MACD line crosses above signal, histogram expanding positive
  - Bearish: opposite
- **ADX (Average Directional Index)**: Trend strength, not direction
  - ADX > 25: strong trend (combine with +DI/-DI for direction)
  - ADX < 20: no trend, range-bound

### Momentum
- **RSI (14)**: Overbought > 70, oversold < 30
  - Divergence: price makes higher high but RSI makes lower high = bearish reversal
- **Stochastic (14, 3, 3)**: Similar to RSI but bounded differently
  - %K > 80 = overbought, %K < 20 = oversold
  - %K crosses above %D = bullish, below = bearish
- **CCI (20)**: > +100 = overbought, < -100 = oversold. Good for cyclical assets.
- **Williams %R (14)**: -20 to 0 = overbought, -100 to -80 = oversold
- **MFI (14)**: Volume-weighted RSI. Better at detecting divergences that include volume.
- **ROC (12)**: Rate of change. Positive = upward momentum.

### Volatility
- **Bollinger Bands (20, 2σ)**:
  - Squeeze: bands narrow = low vol, breakout imminent
  - Walk: price rides upper/lower band = strong trend continuation
  - Tag + reversal: price tags band then reverses = mean reversion
- **ATR (14)**: Used for position sizing and stop placement (see RISK_MANAGEMENT)

### Volume
- **OBV (On-Balance Volume)**: Cumulative volume, rises when price closes up
  - Divergence: price flat but OBV rising = accumulation (bullish)
- **VWAP**: Volume-weighted average price. Day traders use it as fair value.
  - Above VWAP = bullish intraday, below = bearish
- **CMF (Chaikin Money Flow) (20)**: Money flow multiplier × volume
  - CMF > 0.05 = bullish accumulation, < -0.05 = bearish distribution

## Multi-Signal Fusion

Each signal votes buy/sell/neutral with a weight. Composite score = Σ(weight × vote) / Σ(weight).

### Default weights (tuneable via walkforward)
| Indicator | Weight |
|-----------|--------|
| Trend (SMA + EMA + MACD + ADX) | 0.30 |
| Momentum (RSI + Stoch + CCI + ROC) | 0.25 |
| Volume (OBV + VWAP + CMF) | 0.20 |
| Volatility (BB position) | 0.15 |
| Mean reversion (RSI extremes + BB tags) | 0.10 |

### Scoring rubric
| Composite | Signal | Action |
|-----------|--------|--------|
| > 0.70 | strong_buy | Trade with full size |
| 0.50 - 0.70 | buy | Trade with standard size |
| -0.50 to 0.50 | hold | No action |
| -0.70 to -0.50 | sell | Exit / trim |
| < -0.70 | strong_sell | Exit fully |

## Confidence Calibration
- **High confidence (>0.75)**: 3+ signals agree across categories, trend + momentum + volume all aligned, no major divergence
- **Medium (0.6-0.75)**: 2+ signals agree, one category weak
- **Low (<0.6)**: Mixed signals, divergences present → flag for review

## Walk-Forward Validation
Score-return correlation should be > 0.1. If < 0.1, the weights need tuning:
```
python3 .deepagents/skills/investment-analysis/scripts/walkforward.py optimize
```

## Common Pitfalls
- **Overbought ≠ sell**: In a strong uptrend, RSI can stay > 70 for weeks. ADX > 25 + RSI > 70 = trend, not reversal.
- **Squeeze breakout direction is unknown**: BB squeeze tells you vol is coming, not which way. Wait for breakout.
- **Divergence needs confirmation**: One RSI divergence doesn't reverse a trend. Look for 2+ divergences + structural break.
