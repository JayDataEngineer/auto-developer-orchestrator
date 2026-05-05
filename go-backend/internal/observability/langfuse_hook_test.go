package observability

import (
	"encoding/json"
	"testing"
)

// ── parseMetricsJSON tests ──────────────────────────────────────────

func TestParseMetricsJSON_Sentinel(t *testing.T) {
	raw := `{"langfuse_metrics": true, "equity": 100500, "sharpe_ratio": 4.2}`
	data := parseMetricsJSON(raw)
	if data == nil {
		t.Fatal("expected data, got nil")
	}
	if data["equity"].(float64) != 100500 {
		t.Errorf("equity = %v, want 100500", data["equity"])
	}
}

func TestParseMetricsJSON_BashWrapper(t *testing.T) {
	inner := `{"langfuse_metrics": true, "equity": 99000}`
	outer, _ := json.Marshal(map[string]string{"output": inner})
	data := parseMetricsJSON(string(outer))
	if data == nil {
		t.Fatal("expected unwrapped data, got nil")
	}
	if data["equity"].(float64) != 99000 {
		t.Errorf("equity = %v, want 99000", data["equity"])
	}
}

func TestParseMetricsJSON_NoSentinel(t *testing.T) {
	raw := `{"equity": 100000}`
	data := parseMetricsJSON(raw)
	if data != nil {
		t.Error("expected nil for non-sentinel output")
	}
}

// ── parsePortfolioJSON tests ────────────────────────────────────────

func TestParsePortfolioJSON_Direct(t *testing.T) {
	raw := `{"equity": 100500, "cash": 50000}`
	data := parsePortfolioJSON(raw)
	if data == nil {
		t.Fatal("expected data, got nil")
	}
	if data["equity"].(float64) != 100500 {
		t.Errorf("equity = %v, want 100500", data["equity"])
	}
}

func TestParsePortfolioJSON_BashWrapped(t *testing.T) {
	inner := `{"equity": 99000, "total_pnl": -1000}`
	outer, _ := json.Marshal(map[string]string{"output": inner})
	data := parsePortfolioJSON(string(outer))
	if data == nil {
		t.Fatal("expected unwrapped data, got nil")
	}
	if data["equity"].(float64) != 99000 {
		t.Errorf("equity = %v, want 99000", data["equity"])
	}
}

func TestParsePortfolioJSON_PortfolioKey(t *testing.T) {
	raw := `{"portfolio": {"equity": 100000, "positions": []}}`
	data := parsePortfolioJSON(raw)
	if data == nil {
		t.Fatal("expected data with portfolio key, got nil")
	}
}

func TestParsePortfolioJSON_NoPortfolioData(t *testing.T) {
	raw := `{"status": "ok", "message": "hello"}`
	data := parsePortfolioJSON(raw)
	if data != nil {
		t.Error("expected nil for non-portfolio output")
	}
}

// ── extractFloat tests ──────────────────────────────────────────────

func TestExtractFloat(t *testing.T) {
	tests := []struct {
		m    map[string]any
		key  string
		want float64
	}{
		{map[string]any{"x": float64(3.14)}, "x", 3.14},
		{map[string]any{"x": int(42)}, "x", 42},
		{map[string]any{"x": "99.5"}, "x", 99.5},
		{map[string]any{"x": "notanumber"}, "x", 0},
		{map[string]any{}, "x", 0},
	}
	for _, tt := range tests {
		got := extractFloat(tt.m, tt.key)
		if got != tt.want {
			t.Errorf("extractFloat(%v, %q) = %v, want %v", tt.m, tt.key, got, tt.want)
		}
	}
}

// ── toFloat64 tests ─────────────────────────────────────────────────

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input any
		want  float64
		ok    bool
	}{
		{float64(3.14), 3.14, true},
		{int(42), 42, true},
		{"99.5", 99.5, true},
		{"notanumber", 0, false},
		{true, 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		got, ok := toFloat64(tt.input)
		if ok != tt.ok {
			t.Errorf("toFloat64(%v) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("toFloat64(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ── hasMetricsSentinel tests ────────────────────────────────────────

func TestHasMetricsSentinel(t *testing.T) {
	if !hasMetricsSentinel(map[string]any{"langfuse_metrics": true}) {
		t.Error("expected true for sentinel present")
	}
	if hasMetricsSentinel(map[string]any{"langfuse_metrics": false}) {
		t.Error("expected false for sentinel=false")
	}
	if hasMetricsSentinel(map[string]any{"other": true}) {
		t.Error("expected false for missing sentinel")
	}
}

// ── parseAnyJSON tests ──────────────────────────────────────────────

func TestParseAnyJSON_Direct(t *testing.T) {
	raw := `{"symbol": "AAPL", "action": "buy", "confidence": 0.85}`
	data := parseAnyJSON(raw)
	if data == nil {
		t.Fatal("expected data, got nil")
	}
	if data["symbol"].(string) != "AAPL" {
		t.Errorf("symbol = %v, want AAPL", data["symbol"])
	}
}

func TestParseAnyJSON_BashWrapped(t *testing.T) {
	inner := `{"trades": [{"symbol": "TSLA", "side": "buy"}]}`
	outer, _ := json.Marshal(map[string]string{"output": inner})
	data := parseAnyJSON(string(outer))
	if data == nil {
		t.Fatal("expected unwrapped data, got nil")
	}
	trades, ok := data["trades"].([]any)
	if !ok || len(trades) != 1 {
		t.Errorf("trades = %v, want 1-element array", data["trades"])
	}
}

func TestParseAnyJSON_InvalidJSON(t *testing.T) {
	data := parseAnyJSON("not json at all")
	if data != nil {
		t.Error("expected nil for invalid JSON")
	}
}

// ── metricDescriptions coverage test ────────────────────────────────

func TestMetricDescriptions(t *testing.T) {
	required := []string{
		"equity", "total_pnl", "daily_pnl", "position_count",
		"return_pct", "sharpe_ratio", "sortino_ratio", "max_drawdown",
		"win_rate", "profit_factor", "regime_composite",
		"portfolio_heat", "prediction_accuracy",
	}
	for _, name := range required {
		if _, ok := metricDescriptions[name]; !ok {
			t.Errorf("metricDescriptions missing key: %s", name)
		}
	}
}

// ── ClassifyTags tests ──────────────────────────────────────────────

func TestClassifyTags(t *testing.T) {
	tests := []struct {
		input   string
		wantTag string
	}{
		{"What's the AAPL stock price?", "investing"},
		{"Check my portfolio status", "investing"},
		{"Run a backtest on SPY", "investing"},
		{"Fix the login bug in auth.go", "coding"},
		{"Navigate to google.com and scrape", "browser"},
		{"Read the file config.yaml", "file-ops"},
	}
	for _, tt := range tests {
		tags := ClassifyTags(tt.input)
		found := false
		for _, tag := range tags {
			if tag == tt.wantTag {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ClassifyTags(%q) = %v, want tag %q", tt.input, tags, tt.wantTag)
		}
	}
}

func TestClassifyTags_Multiple(t *testing.T) {
	tags := ClassifyTags("Read the stock portfolio file and implement a fix")
	if len(tags) < 2 {
		t.Errorf("ClassifyTags with multi-keyword input got %d tags, want >= 2: %v", len(tags), tags)
	}
}

func TestClassifyTags_Empty(t *testing.T) {
	tags := ClassifyTags("")
	if len(tags) != 1 || tags[0] != "general" {
		t.Errorf("ClassifyTags('') = %v, want [general]", tags)
	}
}
