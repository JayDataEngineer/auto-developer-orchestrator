# RISK_MANAGEMENT

Portfolio heat, position sizing, sector limits. This skill is baked into the **risk-analyst** and **position-sizer** role prompts.

## Position Sizing

### Kelly Criterion (theoretical optimal)
```
f* = (bp - q) / b
```
Where:
- `f*` = fraction of capital to risk
- `b` = odds (reward/risk ratio, e.g., 2:1 = 2)
- `p` = probability of winning (your confidence)
- `q` = probability of losing (1 - p)

**Half-Kelly** is standard practice (volatility reduction):
```
position_size = f* / 2
```

### ATR-Based Sizing (practical)
Most common method:
```python
def atr_position_size(account_equity, entry_price, atr_14, risk_per_trade_pct=0.02):
    """
    Risk 2% of account per trade. Stop at 1.5x ATR below entry.
    """
    stop_distance = atr_14 * 1.5
    risk_dollars = account_equity * risk_per_trade_pct
    shares = risk_dollars / stop_distance
    position_value = shares * entry_price
    position_pct = position_value / account_equity

    # Cap at 15% of equity for stocks
    if position_pct > 0.15:
        shares = (account_equity * 0.15) / entry_price

    return shares, stop_distance
```

### Risk Per Trade by Asset Class
| Asset Class | Risk per Trade | Max Position |
|-------------|---------------|--------------|
| Stocks (large cap) | 1-2% | 15% of equity |
| Stocks (small cap) | 0.5-1% | 8% of equity |
| Crypto (BTC, ETH) | 1-2% | 10% of equity |
| Crypto (alt coins) | 0.5% | 5% of equity |
| Options | 0.5-1% | 3% of equity |

## Portfolio Heat
**Portfolio heat** = sum of risk across all open positions / total equity.

```python
def portfolio_heat(positions, account_equity):
    total_risk = 0
    for pos in positions:
        stop_distance_pct = abs(pos.entry_price - pos.stop_price) / pos.entry_price
        position_risk = pos.shares * pos.entry_price * stop_distance_pct
        total_risk += position_risk
    return total_risk / account_equity
```

### Heat Thresholds
| Asset Class | Warning | Critical |
|-------------|---------|----------|
| Stocks (heat) | > 30% | > 50% |
| Crypto (heat) | > 15% | > 25% |
| Combined | > 40% | > 60% |

**Action at critical**: halt new trades, reduce existing.

### Combined Heat with Correlation Discount
Stocks + crypto are correlated in risk-off. Combined heat is not just sum:
```python
def combined_heat(stock_heat, crypto_heat, correlation=0.4):
    # Effective combined heat = sqrt(sum_of_squares + 2*correlation*stock_heat*crypto_heat)
    return (stock_heat**2 + crypto_heat**2 + 2*correlation*stock_heat*crypto_heat) ** 0.5
```

So 30% stock heat + 15% crypto heat with 0.4 correlation = 38% combined, not 45%.

## Sector Concentration

| Sector | Target Weight | Max |
|--------|--------------|-----|
| Technology | 30% | 40% |
| Healthcare | 15% | 25% |
| Financials | 15% | 25% |
| Consumer Disc | 12% | 20% |
| Consumer Staples | 8% | 15% |
| Energy | 5% | 12% |
| Industrials | 8% | 15% |
| Utilities | 5% | 10% |
| Materials | 5% | 10% |
| REITs | 5% | 10% |
| Communication | 10% | 18% |

**Single stock within sector**: max 50% of sector weight.

## Drawdown Management

| DD Level | Action |
|----------|--------|
| 0% - 5% | Normal |
| 5% - 10% | Yellow — reduce new positions by 50% |
| 10% - 15% | Orange — halt new positions, review |
| 15% - 20% | Red — exit 50% of risk positions |
| > 20% | Crisis — go to cash, deep review |

## Stop-Loss Rules

### Initial Stop
- **Stocks**: 1.5x ATR(14) below entry for swing trades; tighter for day trades
- **Crypto**: 2.0x ATR(14) — wider because vol is ~3x
- **Macro**: N/A (not traded)

### Trailing Stop
- Move stop up as position moves in your favor
- **Chandelier exit**: stop at 3x ATR from highest high since entry
- **Parabolic SAR**: tighter but more whipsaws

### Time-Based Stop
If position hasn't moved in your favor within N days, exit:
- Day trade: end of day
- Swing trade: 5-10 days
- Position trade: 30 days

### Volatility Stop
If realized vol expands > 50% above entry vol, exit (regime change).

## Take-Profit Rules

### Scale-Out
- Sell 1/3 at 1R (1x risk)
- Sell 1/3 at 2R
- Sell 1/3 at 3R or trailing stop

### Run-the-Winners (trend-following)
- No fixed take-profit
- Trail the stop, let the market take you out
- Better for trends, worse in chop

### Fixed Target
- Exit at 2R or 3R
- Settles for "good enough" return
- Better for chop, worse in trends

## Regime Adjustment

| Regime | position_size_mult | stop_atr_mult | Take-profit |
|--------|-------------------|---------------|-------------|
| Strong bull | 1.3 | 2.5 | Run winners |
| Bull | 1.2 | 2.0 | Run winners |
| Sideways | 1.0 | 1.5 | Fixed 2R |
| Bear | 0.7 | 1.0 | Fixed 1.5R |
| Strong bear | 0.5 | 1.0 | Fixed 1R (cut quickly) |
| Uncertain | 0.5 | 1.0 | Fixed 1.5R |

## Conservative Mode
When user invokes Conservative mode (or regime confidence < 0.4):
- Halve all position sizes
- Raise confidence threshold 0.6 → 0.75
- Tighten stops (use 1.0x ATR instead of regime default)
- Cut all targets in half

## Pre-Trade Checklist

Before approving any trade:
1. ✅ Confidence ≥ mode threshold (0.6 Base / 0.75 Conservative)
2. ✅ Position size within single-name limit
3. ✅ Combined heat ≤ 60% (or 40% Conservative)
4. ✅ Sector concentration ≤ 30%
5. ✅ Asset class concentration ≤ 20% crypto
6. ✅ Stop-loss set (1.5x ATR stocks, 2.0x crypto)
7. ✅ Take-profit target set (1.5-3R)
8. ✅ No earnings within 5 days (or sized for it)
9. ✅ Funding cost acceptable (crypto)
10. ✅ Thesis clear (1 sentence)

If any fails → reject with reason.

## Output Schema

```json
{
  "portfolio_heat": {
    "stock_heat": 0.18,
    "crypto_heat": 0.08,
    "combined_heat": 0.24,
    "status": "OK"
  },
  "concentration": {
    "sectors": {
      "Technology": 0.32,
      "Healthcare": 0.15,
      "Financials": 0.12
    },
    "asset_classes": {
      "stocks": 0.65,
      "crypto": 0.12,
      "cash": 0.23
    }
  },
  "drawdown": {
    "current_from_peak": 0.03,
    "max_30d": 0.05,
    "status": "Normal"
  },
  "approved_for_trading": true
}
```
