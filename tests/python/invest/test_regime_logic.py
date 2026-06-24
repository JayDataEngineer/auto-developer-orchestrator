"""
Pure-logic tests for invest org sandbox scripts (regime.py, signals.py).

These tests verify the MATH of signal fusion, regime classification, and
position sizing — no I/O, no network, no Alpaca. They lock down the
formulas that the agent's risk + execution decisions depend on.

Run: uv run --with pytest python3 -m pytest tests/python/invest/ -v
"""

import os
import sys

import pytest

# Add invest sandbox to sys.path so we can import regime.py + signals.py
INVEST_SANDBOX = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "..", "..", "orgs", "invest", "sandbox")
)
sys.path.insert(0, INVEST_SANDBOX)

import regime  # noqa: E402
import signals  # noqa: E402


# ── compute_sma ──────────────────────────────────────────────────────────────


class TestComputeSma:
    def test_full_window(self):
        assert regime.compute_sma([10, 20, 30, 40, 50], 5) == 30.0

    def test_partial_window_averages_available(self):
        # Fewer points than period → average what we have
        assert regime.compute_sma([10, 20], 5) == 15.0

    def test_empty_returns_zero(self):
        assert regime.compute_sma([], 5) == 0.0

    def test_uses_last_n_points_not_all(self):
        # If we have 10 points and period=3, only last 3 should count
        prices = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
        assert regime.compute_sma(prices, 3) == 9.0  # (8+9+10)/3


# ── compute_roc ──────────────────────────────────────────────────────────────


class TestComputeRoc:
    def test_positive_roc(self):
        # Price went from 100 → 110 over the period → +10%
        prices = [100] * 21
        prices[-1] = 110
        roc = regime.compute_roc(prices, 20)
        assert roc == pytest.approx(10.0, abs=0.01)

    def test_negative_roc(self):
        prices = [100] * 21
        prices[-1] = 90
        roc = regime.compute_roc(prices, 20)
        assert roc == pytest.approx(-10.0, abs=0.01)

    def test_zero_division_returns_zero(self):
        # Start price was 0 → can't divide
        prices = [0] * 21
        prices[-1] = 10
        assert regime.compute_roc(prices, 20) == 0.0

    def test_insufficient_data_returns_zero(self):
        assert regime.compute_roc([1, 2, 3], 20) == 0.0


# ── score_volatility ─────────────────────────────────────────────────────────


class TestScoreVolatility:
    def test_low_vix_is_bullish(self):
        # VIX ≤ 15 → base 90 (calm)
        score, det = regime.score_volatility(12)
        assert score >= 85
        assert det["zone"] == "calm"

    def test_high_vix_is_bearish(self):
        # VIX > 35 → base 5 (fearful)
        score, det = regime.score_volatility(40)
        assert score <= 10
        assert det["zone"] == "fearful"

    def test_rising_vix_lowers_score(self):
        # Same VIX, but rising trend should reduce score
        flat, _ = regime.score_volatility(22, vix_trend=0.0)
        rising, _ = regime.score_volatility(22, vix_trend=2.0)
        assert rising < flat

    def test_falling_vix_raises_score(self):
        flat, _ = regime.score_volatility(22, vix_trend=0.0)
        falling, _ = regime.score_volatility(22, vix_trend=-2.0)
        assert falling > flat

    def test_score_always_in_range(self):
        for vix in [5, 15, 18, 22, 25, 30, 35, 50, 100]:
            for trend in [-10, -5, 0, 5, 10]:
                score, _ = regime.score_volatility(vix, trend)
                assert 0 <= score <= 100


# ── score_breadth ────────────────────────────────────────────────────────────


class TestScoreBreadth:
    def test_all_above_sma_returns_100(self):
        # Construct 50 ascending prices — last > sma50
        tp = {"AAA": list(range(1, 51))}
        score, det = regime.score_breadth(tp)
        assert score == 100.0
        assert det["above_sma50"] == 1

    def test_all_below_sma_returns_0(self):
        # Descending prices — last < sma50
        tp = {"AAA": list(range(50, 0, -1))}
        score, det = regime.score_breadth(tp)
        assert score == 0.0

    def test_empty_returns_50(self):
        score, _ = regime.score_breadth({})
        assert score == 50.0

    def test_mixed_returns_percentage(self):
        # 2 tickers: 1 above, 1 below
        tp = {
            "UP": list(range(1, 51)),    # ascending → above
            "DOWN": list(range(50, 0, -1)),  # descending → below
        }
        score, det = regime.score_breadth(tp)
        assert score == 50.0
        assert det["above_sma50"] == 1
        assert det["total"] == 2


# ── classify_regime ──────────────────────────────────────────────────────────


class TestClassifyRegime:
    DEFAULTS = ({"trend": 0.35, "volatility": 0.25, "momentum": 0.25, "breadth": 0.15},
                {"bull": 65, "bear": 35})

    def test_high_composite_is_bull(self):
        weights, thresholds = self.DEFAULTS
        scores = {"trend": 90, "volatility": 90, "momentum": 90, "breadth": 90}
        regime_name, conf, comp = regime.classify_regime(scores, weights, thresholds)
        assert regime_name == "bull"
        assert comp >= 65
        assert conf >= 0.5

    def test_low_composite_is_bear(self):
        weights, thresholds = self.DEFAULTS
        scores = {"trend": 10, "volatility": 10, "momentum": 10, "breadth": 10}
        regime_name, conf, comp = regime.classify_regime(scores, weights, thresholds)
        assert regime_name == "bear"
        assert comp <= 35

    def test_mid_composite_is_sideways(self):
        weights, thresholds = self.DEFAULTS
        scores = {"trend": 50, "volatility": 50, "momentum": 50, "breadth": 50}
        regime_name, _, comp = regime.classify_regime(scores, weights, thresholds)
        assert regime_name == "sideways"
        assert 35 < comp < 65

    def test_bull_confidence_scales_with_distance(self):
        weights, thresholds = self.DEFAULTS
        # Barely bull
        barely = regime.classify_regime(
            {"trend": 70, "volatility": 60, "momentum": 65, "breadth": 65},
            weights, thresholds)
        # Strongly bull
        strong = regime.classify_regime(
            {"trend": 95, "volatility": 95, "momentum": 95, "breadth": 95},
            weights, thresholds)
        assert strong[1] > barely[1]  # strong has higher confidence

    def test_missing_pillars_default_to_50(self):
        # If a pillar is missing, defaults to 50 (neutral)
        weights, thresholds = self.DEFAULTS
        regime_name, _, comp = regime.classify_regime({}, weights, thresholds)
        # Empty scores → all pillars=50 → composite=50 → sideways
        assert regime_name == "sideways"
        assert comp == 50.0


# ── signals.compute_composite + determine_action ────────────────────────────


class TestCompositeAndAction:
    def test_composite_weighted_average(self):
        pillars = {"technical": 80, "fundamental": 60, "sentiment": 40, "momentum": 20}
        weights = {"technical": 0.4, "fundamental": 0.3, "sentiment": 0.2, "momentum": 0.1}
        # 80*0.4 + 60*0.3 + 40*0.2 + 20*0.1 = 32 + 18 + 8 + 2 = 60
        assert signals.compute_composite(pillars, weights) == pytest.approx(60.0, abs=0.01)

    def test_determine_action_strong_buy(self):
        # composite ≥ 70 (strong_buy) AND technical ≥ 65
        action = signals.determine_action(75, 70, {})
        assert action == "strong_buy"

    def test_determine_action_buy(self):
        # composite ≥ 55 but not strong_buy
        action = signals.determine_action(60, 50, {})
        assert action == "buy"

    def test_determine_action_hold(self):
        # composite ≥ 40 but < 55
        action = signals.determine_action(50, 50, {})
        assert action == "hold"

    def test_determine_action_sell(self):
        # composite ≥ 25 but < 40
        action = signals.determine_action(30, 30, {})
        assert action == "sell"

    def test_determine_action_strong_sell(self):
        action = signals.determine_action(10, 10, {})
        assert action == "strong_sell"

    def test_clamp_clamps(self):
        assert signals.clamp(5, 0, 10) == 5
        assert signals.clamp(-1, 0, 10) == 0
        assert signals.clamp(11, 0, 10) == 10

    def test_safe_get_nested(self):
        data = {"a": {"b": {"c": 42}}}
        assert signals.safe_get(data, "a", "b", "c") == 42
        assert signals.safe_get(data, "a", "x", default="missing") == "missing"


# ── score_trend — synthetic price series ─────────────────────────────────────


class TestScoreTrend:
    def test_uptrend_scores_above_neutral(self):
        # 60 ascending prices
        prices = [100 + i * 0.5 for i in range(60)]
        score, _ = regime.score_trend(prices)
        assert score > 50  # bullish

    def test_downtrend_scores_below_neutral(self):
        prices = [100 - i * 0.5 for i in range(60)]
        score, _ = regime.score_trend(prices)
        assert score < 50  # bearish

    def test_short_history_returns_neutral(self):
        score, det = regime.score_trend([100, 101, 102])
        assert score == 50.0
        assert "insufficient" in det["note"].lower()

    def test_score_always_in_range(self):
        for series in [
            [100 + i for i in range(60)],
            [100 - i for i in range(60)],
            [100] * 60,
            [100 + (i % 5) for i in range(60)],
        ]:
            score, _ = regime.score_trend(series)
            assert 0 <= score <= 100


# ── get_regime_params ────────────────────────────────────────────────────────


class TestRegimeParams:
    STRATEGY = {
        "bull":     {"position_size_mult": 1.2, "bias": "long"},
        "bear":     {"position_size_mult": 0.5, "bias": "short"},
        "sideways": {"position_size_mult": 0.8, "bias": "neutral"},
    }

    def test_known_regime_returns_its_params(self):
        params = regime.get_regime_params("bull", self.STRATEGY)
        assert params["bias"] == "long"
        assert params["position_size_mult"] == 1.2

    def test_unknown_regime_falls_back_to_sideways(self):
        params = regime.get_regime_params("nonexistent", self.STRATEGY)
        assert params["bias"] == "neutral"

    def test_returns_copy_not_reference(self):
        params1 = regime.get_regime_params("bull", self.STRATEGY)
        params1["mutated"] = True
        params2 = regime.get_regime_params("bull", self.STRATEGY)
        assert "mutated" not in params2
