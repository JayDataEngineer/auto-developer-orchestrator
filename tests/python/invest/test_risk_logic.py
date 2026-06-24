"""
Pure-logic tests for invest org sandbox risk.py.

Locks down the math behind every position-sizing and stop-loss decision the
agent makes. No I/O, no Alpaca, no yfinance. If one of these breaks, the
agent's dollar allocations drift silently.

Run: uv run --with pytest python3 -m pytest tests/python/invest/test_risk_logic.py -v
"""

import os
import sys

import pytest

INVEST_SANDBOX = os.path.abspath(
    os.path.join(os.path.dirname(__file__), "..", "..", "..", "orgs", "invest", "sandbox")
)
sys.path.insert(0, INVEST_SANDBOX)

import risk  # noqa: E402


# ── calc_atr ─────────────────────────────────────────────────────────────────


class TestCalcAtr:
    def test_constant_range_uniform(self):
        # Each bar moves exactly $1 (high-low=1, no gaps). ATR over 14 bars = 1.0.
        highs = [11.0] * 15
        lows = [10.0] * 15
        closes = [10.5] * 15
        assert risk.calc_atr(highs, lows, closes, period=14) == pytest.approx(1.0)

    def test_uses_true_range_not_just_high_low(self):
        # Gap-up bar at index 1: prev_close=10, high=15, low=14, close=14.5.
        #   TR for bar 1 = max(15-14, |15-10|, |14-10|) = max(1, 5, 4) = 5
        # Bar 2 inherits the gap (prev_close=14.5, high=11, low=10):
        #   TR = max(1, |11-14.5|, |10-14.5|) = max(1, 3.5, 4.5) = 4.5
        # Remaining 12 bars: prev_close=10.5, high=11, low=10 → TR = 1 each.
        highs = [10.0, 15.0] + [11.0] * 13
        lows = [9.0, 14.0] + [10.0] * 13
        closes = [10.0, 14.5] + [10.5] * 13
        atr = risk.calc_atr(highs, lows, closes, period=14)
        # TRs: [5.0, 4.5, 1.0 × 12] = 21.5 over 14 bars.
        assert atr == pytest.approx(21.5 / 14, abs=0.01)

    def test_insufficient_closes_returns_none(self):
        # Need at least period+1 closes points to compute period TRs.
        assert risk.calc_atr([10, 11], [9, 10], [10, 11], period=14) is None

    def test_exactly_period_plus_one_boundary(self):
        # period+1 points → exactly period TRs. Should return a value, not None.
        highs = [11.0] * 15
        lows = [10.0] * 15
        closes = [10.5] * 15
        result = risk.calc_atr(highs, lows, closes, period=14)
        assert result is not None
        assert result == pytest.approx(1.0)

    def test_custom_period(self):
        # period=3 on a 5-bar series with $1 ranges.
        highs = [11.0] * 5
        lows = [10.0] * 5
        closes = [10.5] * 5
        assert risk.calc_atr(highs, lows, closes, period=3) == pytest.approx(1.0)


# ── kelly_fraction ───────────────────────────────────────────────────────────


class TestKellyFraction:
    def test_known_value_60_15(self):
        # W=0.6, B=1.5: full = (0.9 - 0.4)/1.5 = 0.333, half = 0.167
        full, half = risk.kelly_fraction(0.6, 1.5)
        assert full == pytest.approx(0.333, abs=0.01)
        assert half == pytest.approx(0.167, abs=0.01)

    def test_half_is_exactly_half_of_full(self):
        full, half = risk.kelly_fraction(0.55, 2.0)
        assert half == pytest.approx(full * 0.5)

    def test_zero_win_rate_returns_zero(self):
        # No edge → no bet. Caller must fall back to confidence-based sizing.
        full, half = risk.kelly_fraction(0.0, 1.5)
        assert full == 0.0
        assert half == 0.0

    def test_zero_ratio_returns_zero(self):
        full, half = risk.kelly_fraction(0.6, 0.0)
        assert full == 0.0
        assert half == 0.0

    def test_negative_inputs_return_zero(self):
        # Defensive — journal stats should never be negative, but guard anyway.
        full, half = risk.kelly_fraction(-0.1, 1.5)
        assert (full, half) == (0.0, 0.0)

    def test_losing_strategy_clamped_to_zero(self):
        # W=0.3, B=1.0: full = (0.3 - 0.7)/1 = -0.4 → half clamped to 0.
        full, half = risk.kelly_fraction(0.3, 1.0)
        assert full < 0  # Full can be negative (signals: don't bet)
        assert half == 0.0  # But half-Kelly never goes negative

    def test_high_win_rate_high_ratio(self):
        # W=0.8, B=3.0: full = (2.4 - 0.2)/3 = 0.733, half = 0.367
        full, half = risk.kelly_fraction(0.8, 3.0)
        assert full == pytest.approx(0.733, abs=0.01)
        assert half == pytest.approx(0.367, abs=0.01)


# ── risk_reward_ratio ────────────────────────────────────────────────────────


class TestRiskRewardRatio:
    def test_standard_3_to_2(self):
        # Default config: target_atr_multiplier=3, stop_atr_multiplier=2 → 1.5
        assert risk.risk_reward_ratio(3.0, 2.0) == 1.5

    def test_equal_multiples(self):
        assert risk.risk_reward_ratio(2.0, 2.0) == 1.0

    def test_zero_stop_returns_zero(self):
        # Degenerate config — would cause div-by-zero without the guard.
        assert risk.risk_reward_ratio(3.0, 0.0) == 0.0

    def test_negative_stop_returns_zero(self):
        assert risk.risk_reward_ratio(3.0, -1.0) == 0.0

    def test_result_rounded_to_two_decimals(self):
        # 5/3 = 1.666... → rounds to 1.67
        assert risk.risk_reward_ratio(5.0, 3.0) == 1.67


# ── get_sector ───────────────────────────────────────────────────────────────


class TestGetSector:
    def test_known_stock_mapping(self):
        assert risk.get_sector("AAPL") == "Technology"
        assert risk.get_sector("TSLA") == "Consumer Cyclical"
        assert risk.get_sector("BTC") == "Crypto"

    def test_unknown_symbol_no_data_returns_unknown(self):
        assert risk.get_sector("UNKNOWN") == "Unknown"

    def test_falls_back_to_market_data(self):
        # When symbol isn't in SECTOR_MAP, look it up in provided market_data.
        market_data = [
            {"symbol": "PLTR", "sector": "Technology"},
            {"symbol": "COIN", "sector": "Financial"},
        ]
        assert risk.get_sector("PLTR", market_data) == "Technology"
        assert risk.get_sector("COIN", market_data) == "Financial"

    def test_market_data_missing_sector_field(self):
        market_data = [{"symbol": "FOO"}]  # No "sector" key
        assert risk.get_sector("FOO", market_data) == "Unknown"

    def test_market_data_not_a_list(self):
        # Defensive — if someone passes a dict, don't crash.
        assert risk.get_sector("FOO", {"not": "a list"}) == "Unknown"

    def test_market_data_empty_list(self):
        assert risk.get_sector("FOO", []) == "Unknown"


# ── load_config ──────────────────────────────────────────────────────────────


class TestLoadConfig:
    def test_defaults_when_no_file(self, tmp_path, monkeypatch):
        # Point RISK_CONFIG_FILE at a non-existent path → all defaults returned.
        fake_path = str(tmp_path / "nonexistent.json")
        monkeypatch.setattr(risk, "RISK_CONFIG_FILE", fake_path)
        cfg = risk.load_config()
        assert cfg == risk.DEFAULT_CONFIG
        # Spot-check a few critical defaults
        assert cfg["max_position_pct"] == 0.15
        assert cfg["max_portfolio_heat_pct"] == 0.06
        assert cfg["stop_atr_multiplier"] == 2.0

    def test_overrides_merge_with_defaults(self, tmp_path, monkeypatch):
        # File provides a partial override — defaults must fill the gaps.
        cfg_path = tmp_path / "risk.json"
        cfg_path.write_text('{"max_position_pct": 0.25, "atr_period": 21}')
        monkeypatch.setattr(risk, "RISK_CONFIG_FILE", str(cfg_path))

        cfg = risk.load_config()
        assert cfg["max_position_pct"] == 0.25  # overridden
        assert cfg["atr_period"] == 21           # overridden
        assert cfg["max_portfolio_heat_pct"] == 0.06  # default preserved
        assert cfg["target_atr_multiplier"] == 3.0   # default preserved

    def test_corrupt_json_falls_back_to_defaults(self, tmp_path, monkeypatch):
        # Bad JSON must NOT crash — silently fall back.
        cfg_path = tmp_path / "broken.json"
        cfg_path.write_text('{not valid json')
        monkeypatch.setattr(risk, "RISK_CONFIG_FILE", str(cfg_path))

        cfg = risk.load_config()
        assert cfg == risk.DEFAULT_CONFIG
