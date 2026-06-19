# MACRO_ANALYSIS

Macro regime detection — yield curve, rates, DXY, commodities, FRED series. This skill is baked into the **regime-analyst** role prompt.

## Data Sources

### FRED API (primary — free, key required)
Get a free API key at https://fred.stlouisfed.org/docs/api/api_key.html. Set `FRED_API_KEY` env var.

### yfinance Macro Tickers (no auth)
| Ticker | What |
|--------|------|
| `^TNX` | 10-year Treasury yield (%) |
| `^IRX` | 13-week T-bill yield (%) |
| `^TYX` | 30-year Treasury yield (%) |
| `^FVX` | 5-year Treasury yield (%) |
| `^VIX` | Volatility index |
| `DX-Y.NYB` | US Dollar Index |
| `GC=F` | Gold futures |
| `CL=F` | Crude oil WTI futures |
| `SI=F` | Silver futures |
| `HG=F` | Copper futures |
| `ZW=F` | Wheat futures |
| `ZN=F` | 10-year Treasury note futures |

### FRED Series IDs
| Series | What |
|--------|------|
| `FEDFUNDS` | Federal Funds Effective Rate |
| `DGS10` | 10-Year Treasury Constant Maturity Rate |
| `DGS2` | 2-Year Treasury Constant Maturity Rate |
| `DGS30` | 30-Year Treasury Constant Maturity Rate |
| `T10YIE` | 10-Year Breakeven Inflation Rate |
| `CPIAUCSL` | Consumer Price Index (All Urban) |
| `CORESTICKM159SFRBATL` | Sticky-Price Core CPI |
| `GDP` | Real Gross Domestic Product |
| `GDPC1` | Real GDP (chained 2017 dollars) |
| `UNRATE` | Civilian Unemployment Rate |
| `PAYEMS` | Total Nonfarm Payrolls |
| `UMCSENT` | University of Michigan Consumer Sentiment |
| `WALCL` | Federal Reserve Total Assets (balance sheet) |
| `M2SL` | M2 Money Stock |

## Macro Regime Detection

### 1. Yield Curve State
```
spread_10y_2y = DGS10 - DGS2
spread_10y_3m = DGS10 - DGS3M (more recession-predictive per Fed research)
```

| Spread State | Signal |
|--------------|--------|
| > 100 bps | Normal, healthy growth |
| 0 to 100 bps | Flattening, late cycle |
| -50 to 0 bps | Inverted, recession within 12-18 months |
| < -50 bps | Deeply inverted, strong recession signal |
| **Re-steepening after inversion** | Recession starting or imminent |

**Note**: Yield curve inversions have preceded every US recession since 1955 (except one). The un-inversion (re-steepening) is often the actual recession signal.

### 2. Rates Direction
| State | Signal |
|-------|--------|
| FFR cuts + 10Y falling | Easing cycle = bullish for risk assets (eventually) |
| FFR hikes + 10Y rising | Tightening cycle = bearish for risk assets |
| FFR high + 10Y stable | Holding pattern = mixed |
| FFR cuts + 10Y rising | Growth scare + inflation = bearish |

### 3. Dollar (DXY)
| Level | Impact |
|-------|--------|
| < 95 | Weak dollar — bullish for commodities, emerging markets, crypto |
| 95-100 | Normal |
| 100-105 | Strong dollar — pressure on commodities, crypto, intl equities |
| > 105 | Very strong dollar — risk-off for non-US assets |

**DXY trend matters more than level**: rising DXY is generally risk-off.

### 4. Commodities
- **Gold (GC=F)**: Safe haven. Rising gold = risk-off hedge.
- **Oil (CL=F)**: Inflation proxy + growth indicator. Rising oil = inflation pressure or growth.
- **Copper (HG=F)**: "Dr. Copper" — industrial demand proxy. Rising copper = economic growth.

### 5. Fed Balance Sheet (WALCL)
- **Growing (QE)**: Liquidity injection, bullish risk assets
- **Shrinking (QT)**: Liquidity drain, bearish risk assets

## Macro Regime Composite

| Factor | Risk-On Score | Risk-Off Score |
|--------|---------------|----------------|
| Yield curve | > 100 bps | < 0 bps |
| Rates trend | Cutting cycle | Hiking cycle |
| DXY trend | Falling | Rising |
| Gold trend | Falling (no hedge demand) | Rising |
| Oil stability | Stable | Spiking (>20% in 30d) |
| Fed balance sheet | Growing | Shrinking |

**Composite**:
- 5-6 risk-on factors = **risk-on regime**
- 3-4 = **mixed**
- 0-2 = **risk-off regime**

## Crypto-Specific Macro
Crypto is highly sensitive to:
- **DXY** (inverse correlation, ~ -0.4 historically)
- **10Y yield** (inverse correlation)
- **Fed balance sheet** (positive correlation, liquidity-driven)
- **VIX** (inverse correlation in panic sell-offs)

Strong DXY + rising 10Y + shrinking Fed balance sheet = bearish crypto macro headwind.

## Output Schema

```json
{
  "macro_regime": {
    "state": "risk-off",
    "confidence": 0.68,
    "factors": {
      "yield_curve": {
        "spread_10y_2y": -0.45,
        "state": "inverted",
        "trend": "re-steepening"
      },
      "rates": {
        "ffr": 5.25,
        "ten_year": 4.35,
        "state": "holding",
        "trend": "stable"
      },
      "dxy": {
        "value": 104.2,
        "trend": "rising",
        "state": "strong"
      },
      "commodities": {
        "gold": 2050,
        "oil": 78,
        "copper": 3.85
      },
      "fed_balance_sheet": {
        "value_t": 7.4e12,
        "yoy_change_pct": -10.5
      }
    },
    "factors_risk_on": 2,
    "factors_risk_off": 4,
    "crypto_headwind": true,
    "recession_probability_12m": 0.65
  }
}
```

## Common Patterns
- **Soft landing**: Yield curve un-inverts without recession = bullish for risk assets
- **No landing**: Curve stays inverted, growth continues = mixed (rates stay higher for longer)
- **Hard landing**: Un-inversion + rapidly rising unemployment = bearish
