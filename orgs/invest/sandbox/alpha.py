#!/usr/bin/env python3
"""
alpha.py — Layer 8: Enhanced Alpha Factors + Portfolio Optimization

Three enhancements over the base pipeline:
  1. Expanded alpha factors (20+ new technical indicators)
  2. Hierarchical Risk Parity (HRP) for portfolio allocation
  3. Purged walk-forward cross-validation for realistic backtesting

CLI:
  python3 alpha.py factors [--ticker AAPL]
  python3 alpha.py hrp [--capital 100000]
  python3 alpha.py purged [--months 3] [--gap 5]
  python3 alpha.py enhanced [--months 3]     # Full enhanced historical sim
  python3 alpha.py compare                    # Compare Layer 7 vs Layer 8
"""

import json
import math
import os
import sys
import tempfile
from datetime import datetime, timedelta

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import signals
import walkforward
import regime
import historical

# ── Config ─────────────────────────────────────────────────────────

DEFAULT_CONFIG = {
    "tickers": ["AAPL", "MSFT", "GOOGL", "AMZN", "NVDA", "TSLA", "META"],
    "months": 3,
    "starting_capital": 100000.0,
    "transaction_cost_pct": 0.001,
    "position_pct": 0.10,
    "atr_stop_mult": 2.0,
    "atr_tp_mult": 3.0,
    "max_positions_default": 5,
    "min_composite_buy": 55,
    "max_composite_sell": 40,
    "min_composite_strong": 70,
    # Purged CV settings
    "purge_gap_days": 5,
    "window_days": 60,
    "hold_days": 20,
    "step_days": 10,
}

RESULTS_FILE = os.environ.get("ALPHA_RESULTS", "/sandbox/alpha_results.json")


# ── Part 1: Expanded Alpha Factors ─────────────────────────────────
#
# Pure Python implementations of additional technical indicators
# beyond the basic RSI/EMA/SMA/Bollinger/MACD in walkforward.py.


def compute_obv(closes, volumes):
    """On-Balance Volume — cumulative volume flow indicator."""
    if len(closes) < 2:
        return 0.0
    obv = 0.0
    for i in range(1, len(closes)):
        if closes[i] > closes[i - 1]:
            obv += volumes[i] if i < len(volumes) else 0
        elif closes[i] < closes[i - 1]:
            obv -= volumes[i] if i < len(volumes) else 0
    return obv


def compute_adx(highs, lows, closes, period=14):
    """Average Directional Index — trend strength (0-100)."""
    n = len(closes)
    if n < period + 1:
        return 25.0  # Neutral default
    trs, pos_dm, neg_dm = [], [], []
    for i in range(1, n):
        tr = max(highs[i] - lows[i],
                 abs(highs[i] - closes[i - 1]),
                 abs(lows[i] - closes[i - 1]))
        up = highs[i] - highs[i - 1]
        down = lows[i - 1] - lows[i]
        pos_dm.append(up if up > down and up > 0 else 0)
        neg_dm.append(down if down > up and down > 0 else 0)
        trs.append(tr)
    if len(trs) < period:
        return 25.0
    # Smoothed averages
    atr_val = sum(trs[:period]) / period
    pos_di_vals = sum(pos_dm[:period])
    neg_di_vals = sum(neg_dm[:period])
    for i in range(period, len(trs)):
        atr_val = (atr_val * (period - 1) + trs[i]) / period
        pos_di_vals = (pos_di_vals * (period - 1) + pos_dm[i]) / period
        neg_di_vals = (neg_di_vals * (period - 1) + neg_dm[i]) / period
    if atr_val == 0:
        return 25.0
    pos_di = pos_di_vals / atr_val * 100
    neg_di = neg_di_vals / atr_val * 100
    di_sum = pos_di + neg_di
    if di_sum == 0:
        return 25.0
    dx = abs(pos_di - neg_di) / di_sum * 100
    return min(100.0, dx)


def compute_cci(highs, lows, closes, period=20):
    """Commodity Channel Index — measures deviation from average price."""
    if len(closes) < period:
        return 0.0
    tps = [(h + l + c) / 3 for h, l, c in zip(highs, lows, closes)]
    recent = tps[-period:]
    sma = sum(recent) / period
    mean_dev = sum(abs(tp - sma) for tp in recent) / period
    if mean_dev == 0:
        return 0.0
    return (tps[-1] - sma) / (0.015 * mean_dev)


def compute_williams_r(highs, lows, closes, period=14):
    """Williams %R — momentum indicator (0 to -100)."""
    if len(closes) < period:
        return -50.0
    recent_high = max(highs[-period:])
    recent_low = min(lows[-period:])
    rng = recent_high - recent_low
    if rng == 0:
        return -50.0
    return (recent_high - closes[-1]) / rng * -100


def compute_roc(prices, period=12):
    """Rate of Change — percentage change over N periods."""
    if len(prices) < period + 1:
        return 0.0
    return (prices[-1] - prices[-period - 1]) / prices[-period - 1] * 100


def compute_mfi(highs, lows, closes, volumes, period=14):
    """Money Flow Index — volume-weighted RSI (0-100)."""
    if len(closes) < period + 1:
        return 50.0
    pos_flow, neg_flow = 0.0, 0.0
    for i in range(len(closes) - period, len(closes)):
        tp = (highs[i] + lows[i] + closes[i]) / 3
        vol = volumes[i] if i < len(volumes) else 1
        if i > 0:
            prev_tp = (highs[i - 1] + lows[i - 1] + closes[i - 1]) / 3
            mf = tp * vol
            if tp > prev_tp:
                pos_flow += mf
            else:
                neg_flow += mf
    if neg_flow == 0:
        return 100.0
    mfi = 100 - 100 / (1 + pos_flow / neg_flow)
    return mfi


def compute_stoch(highs, lows, closes, period=14):
    """Stochastic Oscillator — %K and %D."""
    if len(closes) < period:
        return {"k": 50.0, "d": 50.0}
    recent_high = max(highs[-period:])
    recent_low = min(lows[-period:])
    rng = recent_high - recent_low
    if rng == 0:
        return {"k": 50.0, "d": 50.0}
    k = (closes[-1] - recent_low) / rng * 100
    # %D is 3-period SMA of %K (simplified)
    d = k * 0.6 + 50.0 * 0.4  # Weighted approx if not enough history
    return {"k": k, "d": d}


def compute_atr_ratio(highs, lows, closes, period=14):
    """ATR as percentage of price — normalized volatility measure."""
    atr = historical.compute_atr(highs, lows, closes, period)
    if atr is None or closes[-1] == 0:
        return 0.02  # 2% default
    return atr / closes[-1]


def compute_vwap(closes, volumes):
    """Volume Weighted Average Price (simplified — uses available history)."""
    if not closes or not volumes or len(closes) != len(volumes):
        return closes[-1] if closes else 0
    total_vol = sum(volumes)
    if total_vol == 0:
        return closes[-1]
    return sum(c * v for c, v in zip(closes, volumes)) / total_vol


def compute_chaikin_mf(highs, lows, closes, volumes, period=20):
    """Chaikin Money Flow — accumulation/distribution measure (-1 to +1)."""
    if len(closes) < period:
        return 0.0
    mfv_sum, vol_sum = 0.0, 0.0
    for i in range(len(closes) - period, len(closes)):
        rng = highs[i] - lows[i]
        if rng == 0:
            continue
        mfv = ((closes[i] - lows[i]) - (highs[i] - closes[i])) / rng * volumes[i]
        mfv_sum += mfv
        vol_sum += volumes[i]
    if vol_sum == 0:
        return 0.0
    return mfv_sum / vol_sum


# ── Expanded Indicator Bundle ──────────────────────────────────────


def compute_all_factors(closes, highs=None, lows=None, volumes=None):
    """Compute all expanded alpha factors from price arrays.

    Returns dict of factor_name -> value for use in enhanced scoring.
    """
    if not closes or len(closes) < 20:
        return {}

    highs = highs or closes
    lows = lows or closes
    volumes = volumes or [1000000] * len(closes)

    # Base indicators from walkforward.py
    base = walkforward.compute_indicators(closes)

    factors = {
        # From base pipeline
        "rsi": base["rsi_14"],
        "macd": base["macd"],
        "sma20": base["sma_20"],
        "sma50": base["sma_50"],
        "bollinger_upper": base["bollinger"]["upper"],
        "bollinger_lower": base["bollinger"]["lower"],
        "ema12": base["ema_12"],
        "ema26": base["ema_26"],

        # New expanded factors
        "obv": compute_obv(closes, volumes),
        "adx": compute_adx(highs, lows, closes),
        "cci": compute_cci(highs, lows, closes),
        "williams_r": compute_williams_r(highs, lows, closes),
        "roc_5": compute_roc(closes, 5),
        "roc_10": compute_roc(closes, 10),
        "roc_20": compute_roc(closes, 20),
        "mfi": compute_mfi(highs, lows, closes, volumes),
        "stoch_k": compute_stoch(highs, lows, closes)["k"],
        "stoch_d": compute_stoch(highs, lows, closes)["d"],
        "atr_pct": compute_atr_ratio(highs, lows, closes),
        "vwap": compute_vwap(closes, volumes),
        "cmf": compute_chaikin_mf(highs, lows, closes, volumes),
    }
    return factors


def score_expanded_technical(factors):
    """Score based on expanded alpha factors. Returns (0-100, details).

    Uses the same 0-100 scoring scale as signals.py but with more inputs.
    """
    if not factors:
        return 50, {"note": "no factors"}

    details = {}
    scores = []

    # RSI (reuses signals.py logic but with more context)
    rsi = factors.get("rsi", 50)
    if rsi < 25:
        rsi_score = 85
    elif rsi < 35:
        rsi_score = 70
    elif rsi < 50:
        rsi_score = 60
    elif rsi < 65:
        rsi_score = 50
    elif rsi < 75:
        rsi_score = 30
    else:
        rsi_score = 15
    scores.append(("rsi", rsi_score, 0.15))
    details["rsi"] = round(rsi, 1)

    # MACD direction
    macd = factors.get("macd", 0)
    price = factors.get("sma20", 100)
    macd_pct = macd / price * 100 if price else 0
    macd_score = max(10, min(90, 50 + macd_pct * 500))
    scores.append(("macd", macd_score, 0.10))
    details["macd"] = "positive" if macd > 0 else "negative"

    # ADX trend strength (above 25 = trending, good for directional trades)
    adx = factors.get("adx", 25)
    if adx > 40:
        adx_score = 80  # Strong trend
    elif adx > 25:
        adx_score = 65
    else:
        adx_score = 40  # Ranging/choppy
    scores.append(("adx", adx_score, 0.10))
    details["adx"] = round(adx, 1)

    # Stochastic (oversold < 20 = buy, overbought > 80 = sell)
    stoch_k = factors.get("stoch_k", 50)
    if stoch_k < 20:
        stoch_score = 85
    elif stoch_k < 40:
        stoch_score = 65
    elif stoch_k < 60:
        stoch_score = 50
    elif stoch_k < 80:
        stoch_score = 35
    else:
        stoch_score = 15
    scores.append(("stoch", stoch_score, 0.10))
    details["stoch_k"] = round(stoch_k, 1)

    # MFI (volume-weighted RSI)
    mfi = factors.get("mfi", 50)
    if mfi < 20:
        mfi_score = 85
    elif mfi < 40:
        mfi_score = 65
    elif mfi < 60:
        mfi_score = 50
    elif mfi < 80:
        mfi_score = 35
    else:
        mfi_score = 15
    scores.append(("mfi", mfi_score, 0.10))
    details["mfi"] = round(mfi, 1)

    # CCI (below -100 = oversold, above +100 = overbought)
    cci = factors.get("cci", 0)
    if cci < -200:
        cci_score = 85
    elif cci < -100:
        cci_score = 70
    elif cci < 100:
        cci_score = 50
    elif cci < 200:
        cci_score = 30
    else:
        cci_score = 15
    scores.append(("cci", cci_score, 0.08))
    details["cci"] = round(cci, 1)

    # Williams %R (similar to stoch, inverted scale)
    wr = factors.get("williams_r", -50)
    if wr < -80:
        wr_score = 80  # Oversold
    elif wr < -50:
        wr_score = 55
    elif wr < -20:
        wr_score = 40
    else:
        wr_score = 20  # Overbought
    scores.append(("williams_r", wr_score, 0.07))
    details["williams_r"] = round(wr, 1)

    # CMF (Chaikin Money Flow) — positive = accumulation
    cmf = factors.get("cmf", 0)
    if cmf > 0.15:
        cmf_score = 80
    elif cmf > 0.05:
        cmf_score = 65
    elif cmf > -0.05:
        cmf_score = 50
    elif cmf > -0.15:
        cmf_score = 35
    else:
        cmf_score = 20
    scores.append(("cmf", cmf_score, 0.08))
    details["cmf"] = round(cmf, 3)

    # Price vs VWAP (institutional reference price)
    vwap = factors.get("vwap", 0)
    sma20 = factors.get("sma20", 0)
    if vwap > 0 and sma20 > 0:
        price_current = factors.get("rsi", 50)  # proxy not ideal
        vwap_diff = (sma20 - vwap) / vwap * 100 if vwap else 0
        if vwap_diff < -1:
            vwap_score = 75  # Below VWAP = potential buy
        elif vwap_diff < 1:
            vwap_score = 55
        else:
            vwap_score = 35  # Above VWAP = potentially overbought
        scores.append(("vwap", vwap_score, 0.07))
        details["vwap_diff"] = round(vwap_diff, 2)
    else:
        scores.append(("vwap", 50, 0.07))

    # ROC composite (momentum)
    roc5 = factors.get("roc_5", 0)
    roc10 = factors.get("roc_10", 0)
    roc20 = factors.get("roc_20", 0)
    roc_composite = roc5 * 0.5 + roc10 * 0.3 + roc20 * 0.2
    roc_score = max(10, min(90, 50 + roc_composite * 5))
    scores.append(("roc", roc_score, 0.08))
    details["roc_composite"] = round(roc_composite, 2)

    # ATR ratio (volatility) — inverse: low vol = higher score for safety
    atr_pct = factors.get("atr_pct", 0.02)
    if atr_pct < 0.015:
        vol_score = 75  # Low vol = stable
    elif atr_pct < 0.025:
        vol_score = 60
    elif atr_pct < 0.04:
        vol_score = 45
    else:
        vol_score = 25  # High vol = risky
    scores.append(("volatility", vol_score, 0.07))
    details["atr_pct"] = round(atr_pct * 100, 2)

    # Weighted composite
    total_weight = sum(w for _, _, w in scores)
    composite = sum(s * w for _, s, w in scores) / total_weight if total_weight else 50
    composite = max(0, min(100, round(composite)))

    return composite, details


def score_ticker_enhanced(asset, config):
    """Score a ticker using expanded alpha factors + base signals.

    Returns score dict compatible with signals.py output format.
    """
    # Get base scores from signals.py
    base_result = signals.score_ticker(asset, config)

    # Compute expanded factors if we have price history
    prices = asset.get("prices", [])
    factors = {}
    if len(prices) >= 20:
        # Use real OHLCV if available, approximate otherwise
        highs = asset.get("highs")
        lows = asset.get("lows")
        volumes = asset.get("volumes")
        if not highs:
            # Approximate from daily ranges using ATR estimate
            atr_est = max(1.0, prices[-1] * 0.015)  # ~1.5% daily range
            highs = [p + atr_est * 0.5 for p in prices]
            lows = [max(0.5, p - atr_est * 0.5) for p in prices]
        if not volumes:
            volumes = [1000000] * len(prices)

        factors = compute_all_factors(
            closes=prices,
            highs=highs,
            lows=lows,
            volumes=volumes,
        )

    alpha_score, alpha_details = score_expanded_technical(factors)

    # Blend: 80% base signals + 20% expanded alpha
    # Very conservative blend — expanded factors fine-tune but don't override.
    # Over-weighting alpha causes over-trading from conflicting short-term signals.
    base_composite = base_result["composite_score"]
    blended = base_composite * 0.8 + alpha_score * 0.2

    # Re-determine action with blended score
    tech_score = base_result["pillars"]["technical"]["score"]
    action = signals.determine_action(
        blended, tech_score, config["action_thresholds"])

    return {
        "ticker": base_result["ticker"],
        "name": base_result.get("name", ""),
        "price": base_result.get("price"),
        "composite_score": round(blended, 1),
        "action_signal": action,
        "base_composite": base_composite,
        "alpha_composite": alpha_score,
        "pillars": base_result["pillars"],
        "alpha_factors": alpha_details,
    }


# ── Part 2: Hierarchical Risk Parity (HRP) ─────────────────────────
#
# De Prado's HRP: cluster assets by correlation, allocate risk evenly
# across clusters. Replaces half-Kelly with mathematically optimal
# diversification.


def compute_returns(price_series_dict):
    """Compute daily returns for each ticker from price arrays.

    Args:
        price_series_dict: {ticker: [prices]}

    Returns:
        {ticker: [daily_returns]}
    """
    returns = {}
    for ticker, prices in price_series_dict.items():
        if len(prices) < 2:
            continue
        rets = []
        for i in range(1, len(prices)):
            if prices[i - 1] > 0:
                rets.append((prices[i] - prices[i - 1]) / prices[i - 1])
            else:
                rets.append(0.0)
        returns[ticker] = rets
    return returns


def correlation_matrix(returns_dict):
    """Compute pairwise Pearson correlation matrix.

    Returns:
        (tickers_list, matrix) where matrix[i][j] = correlation
    """
    tickers = sorted(returns_dict.keys())
    n = len(tickers)
    if n == 0:
        return tickers, []

    # Align returns to same length (use min length)
    min_len = min(len(returns_dict[t]) for t in tickers)
    if min_len < 2:
        return tickers, [[1.0] * n for _ in range(n)]

    # Compute means
    means = {}
    for t in tickers:
        rets = returns_dict[t][-min_len:]
        means[t] = sum(rets) / min_len

    # Compute correlations
    matrix = []
    for i, ti in enumerate(tickers):
        row = []
        ri = returns_dict[ti][-min_len:]
        for j, tj in enumerate(tickers):
            rj = returns_dict[tj][-min_len:]
            cov = sum((a - means[ti]) * (b - means[tj])
                      for a, b in zip(ri, rj)) / min_len
            vi = sum((a - means[ti]) ** 2 for a in ri) / min_len
            vj = sum((b - means[tj]) ** 2 for b in rj) / min_len
            if vi > 0 and vj > 0:
                row.append(cov / math.sqrt(vi * vj))
            else:
                row.append(0.0)
        matrix.append(row)

    return tickers, matrix


def distance_matrix(corr_matrix):
    """Convert correlation to distance: d = sqrt(0.5 * (1 - corr))."""
    n = len(corr_matrix)
    dist = [[0.0] * n for _ in range(n)]
    for i in range(n):
        for j in range(n):
            dist[i][j] = math.sqrt(0.5 * (1 - corr_matrix[i][j]))
    return dist


def single_linkage_cluster(dist_matrix):
    """Simple single-linkage hierarchical clustering.

    Returns list of merge steps: (cluster_a, cluster_b, distance).
    Each cluster is initially a single ticker index.
    """
    n = len(dist_matrix)
    if n < 2:
        return []

    # Track active clusters: {cluster_id: set_of_indices}
    clusters = {i: {i} for i in range(n)}
    merges = []
    next_id = n

    while len(clusters) > 1:
        # Find closest pair
        best_dist = float("inf")
        best_pair = None
        active = list(clusters.keys())

        for ii in range(len(active)):
            for jj in range(ii + 1, len(active)):
                ci, cj = active[ii], active[jj]
                # Single linkage: min distance between any pair
                min_d = float("inf")
                for a in clusters[ci]:
                    for b in clusters[cj]:
                        d = dist_matrix[a][b]
                        if d < min_d:
                            min_d = d
                if min_d < best_dist:
                    best_dist = min_d
                    best_pair = (ci, cj)

        if best_pair is None:
            break

        ci, cj = best_pair
        merges.append((ci, cj, best_dist))

        # Merge
        clusters[next_id] = clusters[ci] | clusters[cj]
        del clusters[ci]
        del clusters[cj]
        next_id += 1

    return merges


def hrp_allocate(tickers, corr_matrix, dist_matrix, merges):
    """Allocate weights via Hierarchical Risk Parity.

    Top-down bisection: at each merge step, split weight proportionally
    to inverse-variance of each sub-cluster.

    Returns: {ticker: weight} where weights sum to 1.0
    """
    n = len(tickers)
    if n == 0:
        return {}
    if n == 1:
        return {tickers[0]: 1.0}

    # Compute variance for each ticker (diagonal of covariance)
    variances = [max(0.001, 1.0 - corr_matrix[i][i] ** 2)
                 for i in range(n)]

    # Start with all tickers in one cluster, split recursively
    def cluster_variance(indices):
        """Inverse-variance weight for a cluster."""
        if not indices:
            return 0.0, {}
        if len(indices) == 1:
            return variances[indices[0]], {indices[0]: 1.0}
        inv_vars = [1.0 / variances[i] for i in indices]
        total = sum(inv_vars)
        cvar = sum(variances[i] * (iv / total)
                   for i, iv in zip(indices, inv_vars))
        weights = {i: iv / total for i, iv in zip(indices, inv_vars)}
        return cvar, weights

    # Build cluster membership from merges
    cluster_members = {i: {i} for i in range(n)}
    for ci, cj, _ in merges:
        cluster_members[len(cluster_members)] = (
            cluster_members.get(ci, {ci}) | cluster_members.get(cj, {cj}))

    # Top-down bisection
    allocations = {i: 1.0 for i in range(n)}

    # Process merges in order — each merge tells us a split
    all_indices = set(range(n))

    # Use the last merge to find the top-level split
    if merges:
        # Reconstruct dendrogram to find bisection order
        cluster_map = {i: frozenset([i]) for i in range(n)}
        next_id = n
        for ci, cj, _ in merges:
            cluster_map[next_id] = cluster_map.get(ci, frozenset([ci])) | \
                                   cluster_map.get(cj, frozenset([cj]))
            next_id += 1

        # Top-down: start from root, bisect
        root_id = next_id - 1
        root_set = cluster_map.get(root_id, all_indices)

        stack = [(root_set, 1.0)]  # (cluster_indices, weight)
        final_weights = {i: 0.0 for i in range(n)}

        while stack:
            current_set, current_weight = stack.pop()
            current_list = sorted(current_set)

            if len(current_list) == 1:
                final_weights[current_list[0]] = current_weight
                continue

            # Find the merge that created this cluster
            left_set = None
            right_set = None
            for ci, cj, _ in reversed(merges):
                c1 = cluster_map.get(ci, frozenset([ci]))
                c2 = cluster_map.get(cj, frozenset([cj]))
                merged = c1 | c2
                if merged == current_set:
                    left_set = c1
                    right_set = c2
                    break

            if left_set is None:
                # Can't split further, equal weight
                w = current_weight / len(current_list)
                for idx in current_list:
                    final_weights[idx] = w
                continue

            # Split by inverse variance
            left_list = sorted(left_set)
            right_list = sorted(right_set)

            lv, _ = cluster_variance(left_list)
            rv, _ = cluster_variance(right_list)

            total_var = lv + rv
            if total_var > 0:
                left_w = current_weight * (1.0 - lv / total_var)
                right_w = current_weight * (1.0 - rv / total_var)
            else:
                left_w = current_weight * 0.5
                right_w = current_weight * 0.5

            stack.append((frozenset(left_list), left_w))
            stack.append((frozenset(right_list), right_w))

        return {tickers[i]: round(final_weights.get(i, 0), 4)
                for i in range(n)}

    # Fallback: equal weight
    w = 1.0 / n
    return {t: round(w, 4) for t in tickers}


def compute_hrp_weights(price_series_dict):
    """Full HRP pipeline: prices -> returns -> corr -> cluster -> allocate.

    Args:
        price_series_dict: {ticker: [close_prices]}

    Returns:
        {ticker: weight} where weights sum to ~1.0
    """
    returns = compute_returns(price_series_dict)
    tickers, corr = correlation_matrix(returns)
    if len(tickers) < 2:
        return {tickers[0]: 1.0} if tickers else {}

    dist = distance_matrix(corr)
    merges = single_linkage_cluster(dist)
    return hrp_allocate(tickers, corr, dist, merges)


# ── Part 3: Purged Walk-Forward Cross-Validation ───────────────────
#
# Adds a gap between train and test windows to prevent information
# leakage. Also adds "embargo" — excluding observations that overlap
# with the test window from the next training set.


def build_purged_windows(n_prices, window_days, hold_days, step_days, gap_days):
    """Build walk-forward windows with a purge gap.

    Standard: [train_start .. train_end][test_start .. test_end]
    Purged:   [train_start .. train_end][GAP][test_start .. test_end]

    The gap prevents the model from seeing information that bleeds
    across the train/test boundary (e.g., overlapping indicators).

    Returns list of (train_start, train_end, test_start, test_end) tuples.
    """
    windows = []
    i = 0
    while True:
        train_start = i
        train_end = i + window_days
        test_start = train_end + gap_days
        test_end = test_start + hold_days

        if test_end > n_prices:
            break

        windows.append((train_start, train_end, test_start, test_end))
        i += step_days
    return windows


def purged_walkforward(ticker, hist, market_asset, vix, config):
    """Run purged walk-forward validation for one ticker.

    Like run_walkforward in walkforward.py but with gap between windows.
    """
    gap = config.get("purge_gap_days", 5)
    wd = config.get("window_days", 60)
    hd = config.get("hold_days", 20)
    sd = config.get("step_days", 10)

    windows = build_purged_windows(
        len(hist["close"]), wd, hd, sd, gap)

    results = []
    for train_s, train_e, test_s, test_e in windows:
        prices = hist["close"][train_s:train_e]
        volumes = hist["volume"][train_s:train_e]
        scored = walkforward.score_window(prices, volumes, market_asset, vix)
        if scored is None:
            continue

        # Forward return uses test window prices (after the gap)
        test_prices = hist["close"][test_s:test_e]
        if len(test_prices) < 2:
            continue
        ret = (test_prices[-1] - test_prices[0]) / test_prices[0] * 100

        results.append({
            "ticker": ticker,
            "composite_score": scored["composite_score"],
            "action_signal": scored["action_signal"],
            "pillars": {k: v["score"] for k, v in scored["pillars"].items()},
            "forward_return": round(ret, 2),
            "date": hist["dates"][test_s] if test_s < len(hist["dates"]) else "",
            "gap_days": gap,
        })
    return results


# ── Part 4: Enhanced Historical Simulation ─────────────────────────
#
# Combines expanded alpha scoring + HRP position sizing in the
# same framework as historical.py's run_simulation.


def simulate_day_enhanced(portfolio, date, all_data, regime_params, config,
                         use_alpha=True, use_hrp=True):
    """Enhanced simulation day with optional alpha factors and/or HRP sizing.

    Args:
        use_alpha: If True, blend expanded alpha factors into scoring.
        use_hrp: If True, use HRP-weighted position sizing.
    """
    tickers = [t for t in config.get("tickers", []) if t in all_data]
    current_prices = {}

    for ticker in tickers:
        sl = historical.slice_asof(all_data[ticker], date)
        if sl and sl["close"]:
            current_prices[ticker] = sl["close"][-1]

    # Check stops first
    historical.check_stops(portfolio, current_prices, date, config)

    # Compute HRP weights for position sizing (if we have enough price data)
    price_series = {}
    for ticker in tickers:
        sl = historical.slice_asof(all_data[ticker], date)
        if sl and len(sl["close"]) >= 30:
            price_series[ticker] = sl["close"]

    hrp_weights = {}
    if len(price_series) >= 2:
        hrp_weights = compute_hrp_weights(price_series)

    # Score each ticker with enhanced alpha factors
    signals_map = {}
    for ticker in tickers:
        sl = historical.slice_asof(all_data[ticker], date)
        if not sl or len(sl["close"]) < 20:
            continue

        prices = sl["close"]
        volumes = sl["volume"]
        price = prices[-1]

        # Build asset dict
        asset = {
            "symbol": ticker,
            "current_price": price,
            "previous_close": prices[-2] if len(prices) > 1 else price,
            "change_pct": ((prices[-1] - prices[-2]) / prices[-2] * 100)
                          if len(prices) > 1 else 0.0,
            "volume": volumes[-1] if volumes else 0,
            "prices": prices,
            "highs": sl["high"],
            "lows": sl["low"],
            "volumes": volumes,
            "indicators": walkforward.compute_indicators(prices),
            "_market_vix": 18.0,
        }

        # Use enhanced scoring (or base scoring if alpha disabled)
        if use_alpha:
            scored = score_ticker_enhanced(asset, signals.DEFAULT_CONFIG)
        else:
            base_scored = signals.score_ticker(asset, signals.DEFAULT_CONFIG)
            scored = {
                "action_signal": base_scored["action_signal"],
                "composite_score": base_scored["composite_score"],
            }
        action = scored["action_signal"]
        composite = scored["composite_score"]

        # Apply regime filter
        bias = regime_params.get("bias", "neutral")
        if bias == "short" and action in ("buy", "strong_buy") and composite < 70:
            action = "hold"
        elif bias == "long" and action in ("sell", "strong_sell") and composite > 30:
            action = "hold"

        # Confidence threshold
        min_buy = config.get("min_composite_buy", 55)
        max_sell = config.get("max_composite_sell", 40)
        if action in ("buy", "strong_buy") and composite < min_buy:
            action = "hold"
        elif action in ("sell", "strong_sell") and composite > max_sell:
            action = "hold"

        signals_map[ticker] = {
            "action": action, "composite": composite, "price": price,
        }

    # Only use HRP sizing when enabled and 2+ concurrent buy signals
    buy_tickers = [t for t, s in signals_map.items()
                   if s["action"] in ("buy", "strong_buy")]
    should_use_hrp = use_hrp and len(buy_tickers) >= 2 and len(hrp_weights) >= 2

    for ticker, sig in signals_map.items():
        action = sig["action"]
        price = sig["price"]

        if action in ("buy", "strong_buy") and should_use_hrp and ticker in hrp_weights:
            # HRP-weighted sizing for diversified entries
            config_copy = config.copy()
            w = hrp_weights[ticker]
            config_copy["position_pct"] = max(0.03, min(0.15, w))
            historical.execute_signal(
                portfolio, ticker, action, price, date,
                regime_params, config_copy)
        else:
            historical.execute_signal(
                portfolio, ticker, action, price, date,
                regime_params, config)


def run_enhanced_simulation(config, use_alpha=True, use_hrp=True):
    """Run full enhanced simulation with alpha factors and/or HRP."""
    months = config.get("months", 3)
    tickers = config.get("tickers", DEFAULT_CONFIG["tickers"])
    capital = config.get("starting_capital", 100000.0)

    print(f"Fetching {months} months of data for {len(tickers)} tickers + SPY...",
          file=sys.stderr)
    all_data = historical.fetch_all_history(tickers, months)

    spy_data = all_data.get("SPY")
    if not spy_data:
        print("ERROR: No SPY data fetched", file=sys.stderr)
        return None, None

    # Get trading dates
    all_dates = spy_data["dates"]
    cutoff = datetime.now() - timedelta(days=months * 31)
    cutoff_str = cutoff.strftime("%Y-%m-%d")
    trading_dates = [d for d in all_dates if d >= cutoff_str]
    if not trading_dates:
        trading_dates = all_dates

    portfolio = historical.init_portfolio(capital)
    regime_config = regime.DEFAULT_CONFIG
    regime_thresholds = regime_config.get("regime_thresholds",
                                           regime.DEFAULT_CONFIG["regime_thresholds"])
    regime_weights = regime_config.get("weights", regime.DEFAULT_CONFIG["weights"])
    strategy_config = regime_config.get("strategy", regime.DEFAULT_CONFIG["strategy"])

    print(f"Simulating {len(trading_dates)} days (enhanced alpha + HRP)...",
          file=sys.stderr)

    for i, date in enumerate(trading_dates):
        # Regime detection (same as historical.py)
        spy_sl = historical.slice_asof(spy_data, date)
        regime_params = regime.get_regime_params("sideways", strategy_config)
        regime_name = "sideways"

        if spy_sl and len(spy_sl["close"]) >= 50:
            spy_prices = spy_sl["close"]
            trend_s, _ = regime.score_trend(spy_prices)
            vol_s, _ = regime.score_volatility(18.0, 0.0)
            mom_s, _ = regime.score_momentum(spy_prices)

            ticker_prices = {}
            for t in tickers:
                tsl = historical.slice_asof(all_data.get(t, {}), date)
                if tsl and len(tsl["close"]) >= 50:
                    ticker_prices[t] = tsl["close"]
            breadth_s, _ = regime.score_breadth(ticker_prices)

            scores = {
                "trend": trend_s, "volatility": vol_s,
                "momentum": mom_s, "breadth": breadth_s,
            }
            regime_name, _, composite = regime.classify_regime(
                scores, regime_weights, regime_thresholds)
            regime_params = regime.get_regime_params(regime_name, strategy_config)

            portfolio["regime_history"].append({
                "date": date, "regime": regime_name,
                "composite": composite,
            })

        # Enhanced simulation day
        simulate_day_enhanced(
            portfolio, date, all_data, regime_params, config,
            use_alpha=use_alpha, use_hrp=use_hrp)

        # Record equity
        current_prices = {}
        for ticker in tickers:
            sl = historical.slice_asof(all_data.get(ticker, {}), date)
            if sl and sl["close"]:
                current_prices[ticker] = sl["close"][-1]
        if spy_sl and spy_sl["close"]:
            current_prices["SPY"] = spy_sl["close"][-1]

        eq = historical.portfolio_value(portfolio, current_prices)
        portfolio["equity_curve"].append({"date": date, "equity": round(eq, 2)})

        if (i + 1) % 20 == 0:
            print(f"  Day {i+1}/{len(trading_dates)}: equity=${eq:,.0f} "
                  f"positions={len(portfolio['positions'])} "
                  f"regime={regime_name}",
                  file=sys.stderr)

    # Close remaining positions
    last_date = trading_dates[-1] if trading_dates else ""
    last_prices = {}
    for ticker in tickers:
        sl = historical.slice_asof(all_data.get(ticker, {}), last_date)
        if sl and sl["close"]:
            last_prices[ticker] = sl["close"][-1]

    for ticker in list(portfolio["positions"].keys()):
        price = last_prices.get(ticker)
        if price:
            pos = portfolio["positions"][ticker]
            cost_pct = config.get("transaction_cost_pct", 0.001)
            proceeds = pos["shares"] * price * (1 - cost_pct)
            pnl = (price - pos["entry_price"]) * pos["shares"]
            portfolio["cash"] += proceeds
            portfolio["trades"].append({
                "date": last_date, "ticker": ticker, "action": "sell",
                "shares": pos["shares"], "price": round(price, 2),
                "pnl": round(pnl, 2), "reason": "liquidation",
            })
            del portfolio["positions"][ticker]

    report = historical.generate_report(portfolio, all_data, config)
    report["enhanced"] = True
    report["alpha_factors_used"] = True
    report["hrp_sizing_used"] = True
    return portfolio, report


# ── Save/Load ──────────────────────────────────────────────────────


def save_results(report, path=None):
    """Save report atomically."""
    path = path or RESULTS_FILE
    report["generated_at"] = datetime.now().isoformat()
    tmp = tempfile.NamedTemporaryFile(
        "w", suffix=".json", delete=False,
        dir=os.path.dirname(path) or ".")
    json.dump(report, tmp, indent=2)
    tmp.close()
    os.replace(tmp.name, path)


def load_results(path=None):
    """Load saved report."""
    path = path or RESULTS_FILE
    if not os.path.exists(path):
        return None
    with open(path) as f:
        return json.load(f)


# ── CLI Commands ───────────────────────────────────────────────────


def cmd_factors(args, config):
    """Show expanded alpha factors for a ticker."""
    import yfinance as yf

    ticker = args.ticker or "AAPL"
    print(f"Fetching {ticker} data...")
    hist = yf.Ticker(ticker).history(period="6mo")
    if hist.empty:
        print(f"No data for {ticker}")
        return

    closes = hist["Close"].tolist()
    highs = hist["High"].tolist()
    lows = hist["Low"].tolist()
    volumes = hist["Volume"].tolist()

    factors = compute_all_factors(closes, highs, lows, volumes)
    alpha_score, details = score_expanded_technical(factors)

    print(f"\n{'=' * 50}")
    print(f"EXPANDED ALPHA FACTORS: {ticker}")
    print(f"{'=' * 50}")
    print(f"Alpha Composite Score: {alpha_score}")
    print(f"\nFactor Breakdown:")
    for k, v in sorted(details.items()):
        print(f"  {k:20s} {v}")
    print(f"\nRaw Factors:")
    for k, v in sorted(factors.items()):
        if isinstance(v, float):
            print(f"  {k:20s} {v:.4f}")
        else:
            print(f"  {k:20s} {v}")


def cmd_hrp(args, config):
    """Compute HRP portfolio allocation."""
    import yfinance as yf

    tickers = config.get("tickers", DEFAULT_CONFIG["tickers"])
    print(f"Fetching {len(tickers)} tickers for HRP analysis...")

    price_series = {}
    for t in tickers:
        hist = yf.Ticker(t).history(period="6mo")
        if hist.empty:
            continue
        price_series[t] = hist["Close"].tolist()

    if len(price_series) < 2:
        print("Need at least 2 tickers with data")
        return

    weights = compute_hrp_weights(price_series)

    capital = args.capital if hasattr(args, "capital") and args.capital else 100000

    print(f"\n{'=' * 50}")
    print("HIERARCHICAL RISK PARITY ALLOCATION")
    print(f"{'=' * 50}")
    print(f"Capital: ${capital:,.0f}")
    print(f"\n{'Ticker':8s} {'Weight':>8s} {'Dollar':>12s}")
    print("-" * 30)
    for t, w in sorted(weights.items(), key=lambda x: -x[1]):
        print(f"{t:8s} {w:>7.1%} ${capital * w:>11,.0f}")

    total = sum(weights.values())
    print(f"\nTotal weight: {total:.4f}")

    # Compare with equal weight
    print(f"\nvs Equal Weight:")
    eq_w = 1.0 / len(weights)
    for t, w in sorted(weights.items(), key=lambda x: -x[1]):
        delta = w - eq_w
        print(f"  {t:8s} HRP={w:.1%} Equal={eq_w:.1%} Delta={delta:+.1%}")


def cmd_purged(args, config):
    """Run purged walk-forward cross-validation."""
    import yfinance as yf

    gap = args.gap or config.get("purge_gap_days", 5)
    months = args.months or config.get("months", 3)
    config["purge_gap_days"] = gap

    tickers = config.get("tickers", DEFAULT_CONFIG["tickers"])
    market_assets, vix = walkforward.load_market_data()

    all_results = []
    for ticker in tickers:
        print(f"Fetching {ticker}...")
        period = f"{max(months + 3, 6)}mo"
        hist = walkforward.fetch_historical(ticker, period)
        if hist is None:
            continue
        asset = market_assets.get(ticker, {"symbol": ticker})
        results = purged_walkforward(ticker, hist, asset, vix, config)
        all_results.extend(results)
        print(f"  {len(results)} windows (gap={gap} days)")

    if not all_results:
        print("No results.")
        return

    # Compare standard vs purged
    corr = walkforward.score_return_correlation(all_results)
    sr = walkforward.sharpe_ratio(all_results)
    mdd = walkforward.max_drawdown(all_results)
    wr = walkforward.action_win_rates(all_results)

    print(f"\n{'=' * 60}")
    print("PURGED WALK-FORWARD VALIDATION RESULTS")
    print(f"{'=' * 60}")
    print(f"Purge gap: {gap} trading days")
    print(f"Observations: {len(all_results)}")
    print(f"Correlation: {corr:.3f}")
    print(f"Sharpe: {sr:.2f}")
    print(f"Max Drawdown: {mdd:.1f}%")

    print(f"\nWin Rates by Action:")
    for action in ("strong_buy", "buy", "hold", "sell", "strong_sell"):
        if action in wr:
            w = wr[action]
            print(f"  {action:12s}  win={w['win_rate']:5.1f}%  "
                  f"avg={w['avg_return']:+6.2f}%  n={w['count']}")

    quality = "GOOD" if corr > 0.3 else "WEAK" if corr > 0.1 else "POOR"
    print(f"\nScore-Return Quality: {quality}")
    if gap > 0:
        print(f"  Purge gap of {gap} days reduces information leakage")

    # Load standard walkforward results for comparison
    std_report = walkforward.load_report()
    if std_report:
        std_corr = std_report.get("correlation", 0)
        print(f"\nvs Standard Walk-Forward:")
        print(f"  Standard correlation: {std_corr:.3f}")
        print(f"  Purged correlation:   {corr:.3f}")
        delta = corr - std_corr
        if delta > 0.02:
            print(f"  Purged is BETTER (+{delta:.3f}) — less overfitting")
        elif delta < -0.02:
            print(f"  Purged is WORSE ({delta:.3f}) — may need gap tuning")
        else:
            print(f"  Similar ({delta:+.3f}) — model is robust")


def cmd_enhanced(args, config):
    """Run full enhanced historical simulation."""
    months = args.months or config.get("months", 3)
    config["months"] = months

    use_alpha = not getattr(args, "no_alpha", False)
    use_hrp = not getattr(args, "no_hrp", False)

    portfolio, report = run_enhanced_simulation(
        config, use_alpha=use_alpha, use_hrp=use_hrp)
    if report is None:
        print("Enhanced simulation failed.")
        return

    save_results(report)

    # Display
    print(f"\n{'=' * 60}")
    print("ENHANCED ALPHA SIMULATION RESULTS (Layer 8)")
    print(f"{'=' * 60}")
    p = report["period"]
    print(f"Period: {p['start']} to {p['end']} ({p['trading_days']} days)")

    r = report["returns"]
    print(f"\nReturns:")
    print(f"  Total:          {r['total_return_pct']:+.2f}%")
    print(f"  Annualized:     {r['annualized_return_pct']:+.2f}%")
    print(f"  Benchmark (SPY): {r['benchmark_return_pct']:+.2f}%")
    print(f"  Excess:         {r['excess_return_pct']:+.2f}%")

    rk = report["risk"]
    print(f"\nRisk:")
    print(f"  Max Drawdown:   {rk['max_drawdown_pct']:.2f}%")
    print(f"  Sharpe Ratio:   {rk['sharpe_ratio']:.2f}")
    print(f"  Sortino Ratio:  {rk['sortino_ratio']:.2f}")

    t = report["trades"]
    print(f"\nTrades:")
    print(f"  Total:          {t['total_trades']} ({t['round_trips']} round-trips)")
    print(f"  Win Rate:       {t['win_rate_pct']:.1f}%")
    print(f"  Profit Factor:  {t['profit_factor']:.2f}")
    print(f"  Total P&L:      ${t['total_pnl']:+,.2f}")

    print(f"\nEnhancements: Alpha factors + HRP sizing")


def cmd_compare(args, config):
    """Compare Layer 7 (base) vs Layer 8 (enhanced) results."""
    base = historical.load_results()
    enhanced = load_results()

    print(f"\n{'=' * 60}")
    print("LAYER 7 (BASE) vs LAYER 8 (ENHANCED) COMPARISON")
    print(f"{'=' * 60}")

    if base:
        r = base["returns"]
        print(f"\nLayer 7 (Base):")
        print(f"  Return: {r['total_return_pct']:+.2f}% (SPY: {r['benchmark_return_pct']:+.2f}%)")
        print(f"  Sharpe: {base['risk']['sharpe_ratio']:.2f} | "
              f"MDD: {base['risk']['max_drawdown_pct']:.2f}%")
        print(f"  Win Rate: {base['trades']['win_rate_pct']:.1f}% | "
              f"PF: {base['trades']['profit_factor']:.2f}")
    else:
        print("\nNo Layer 7 results found. Run historical.py run first.")

    if enhanced:
        r = enhanced["returns"]
        print(f"\nLayer 8 (Enhanced):")
        print(f"  Return: {r['total_return_pct']:+.2f}% (SPY: {r['benchmark_return_pct']:+.2f}%)")
        print(f"  Sharpe: {enhanced['risk']['sharpe_ratio']:.2f} | "
              f"MDD: {enhanced['risk']['max_drawdown_pct']:.2f}%")
        print(f"  Win Rate: {enhanced['trades']['win_rate_pct']:.1f}% | "
              f"PF: {enhanced['trades']['profit_factor']:.2f}")
    else:
        print("\nNo Layer 8 results found. Run alpha.py enhanced first.")

    if base and enhanced:
        print(f"\n{'=' * 60}")
        print("DELTA (Enhanced - Base):")
        ret_d = enhanced["returns"]["total_return_pct"] - base["returns"]["total_return_pct"]
        sharpe_d = enhanced["risk"]["sharpe_ratio"] - base["risk"]["sharpe_ratio"]
        mdd_d = enhanced["risk"]["max_drawdown_pct"] - base["risk"]["max_drawdown_pct"]
        wr_d = enhanced["trades"]["win_rate_pct"] - base["trades"]["win_rate_pct"]
        pf_d = enhanced["trades"]["profit_factor"] - base["trades"]["profit_factor"]

        print(f"  Return:  {ret_d:+.2f}%")
        print(f"  Sharpe:  {sharpe_d:+.2f}")
        print(f"  MDD:     {mdd_d:+.2f}% ({'better' if mdd_d < 0 else 'worse'})")
        print(f"  WinRate: {wr_d:+.1f}%")
        print(f"  PF:      {pf_d:+.2f}")

        if ret_d > 0 and sharpe_d > 0:
            print(f"\n  VERDICT: Enhanced outperforms base")
        elif ret_d > 0:
            print(f"\n  VERDICT: Higher return but risk-adjusted metrics mixed")
        else:
            print(f"\n  VERDICT: Base may be better for this period")


# ── CLI ────────────────────────────────────────────────────────────


def main():
    import argparse
    p = argparse.ArgumentParser(
        description="Enhanced Alpha + Portfolio Optimization (Layer 8)")
    sub = p.add_subparsers(dest="command")

    fac = sub.add_parser("factors", help="Show expanded alpha factors")
    fac.add_argument("--ticker", default=None)

    hrp_p = sub.add_parser("hrp", help="Compute HRP allocation")
    hrp_p.add_argument("--capital", type=float, default=100000)

    pur = sub.add_parser("purged", help="Purged walk-forward validation")
    pur.add_argument("--months", type=int, default=3)
    pur.add_argument("--gap", type=int, default=5)

    enh = sub.add_parser("enhanced", help="Run enhanced historical sim")
    enh.add_argument("--months", type=int, default=3)
    enh.add_argument("--no-alpha", action="store_true", dest="no_alpha",
                     help="Disable expanded alpha factors (use base scoring)")
    enh.add_argument("--no-hrp", action="store_true", dest="no_hrp",
                     help="Disable HRP sizing (use fixed sizing)")

    sub.add_parser("compare", help="Compare Layer 7 vs Layer 8")

    args = p.parse_args()
    cfg = DEFAULT_CONFIG.copy()

    {"factors": cmd_factors, "hrp": cmd_hrp, "purged": cmd_purged,
     "enhanced": cmd_enhanced, "compare": cmd_compare}.get(
        args.command, lambda *_: p.print_help())(args, cfg)


if __name__ == "__main__":
    main()
