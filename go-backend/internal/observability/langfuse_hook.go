package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// LangfuseHook records agent runs, LLM generations, and tool executions
// as Langfuse traces, spans, and generations.
// Implements core.LoopHook — add it to orchestrator.Config.ExtraHooks.
type LangfuseHook struct {
	client    *LangfuseClient
	modelName string
	cfg       TraceConfig

	// per-run state
	trace   *TraceHandle
	startAt time.Time

	// per-turn state (generation tracking)
	turnStart time.Time
	prevModel string

	// per-run invest tracking
	toolsSeen    map[string]bool // tools called during this run
	regimeLabel  string          // last known regime from metrics
	isSimulation bool            // true if running backtest/simulation
}

// NewLangfuseHook creates a new Langfuse tracing hook.
// If client is nil, all methods are no-ops.
func NewLangfuseHook(client *LangfuseClient, modelName string, cfg TraceConfig) *LangfuseHook {
	return &LangfuseHook{
		client:    client,
		modelName: modelName,
		cfg:       cfg,
	}
}

func (h *LangfuseHook) Name() string { return "langfuse" }

func (h *LangfuseHook) OnAgentStart(ctx context.Context, state *core.LoopState) error {
	if h.client == nil || !h.client.Enabled() {
		return nil
	}

	h.startAt = state.StartedAt
	if h.startAt.IsZero() {
		h.startAt = time.Now()
	}

	// Build TraceConfig from hook config + runtime state
	cfg := h.cfg
	// Use project name as session ID for consistent grouping in Langfuse.
	// This lets you see all invest-bot traces in one session timeline.
	if cfg.Project != "" {
		cfg.SessionID = "session-" + cfg.Project
	} else {
		cfg.SessionID = state.SessionID
	}
	if cfg.UserID == "" {
		cfg.UserID = state.SandboxID
	}

	h.client.TraceRun("orchestrator-run", cfg, func(th *TraceHandle) {
		h.trace = th
	})

	h.turnStart = time.Time{}
	h.prevModel = ""
	h.toolsSeen = make(map[string]bool)
	h.regimeLabel = ""
	h.isSimulation = false

	log.Printf("Langfuse trace started: session=%s project=%s", state.SessionID, h.cfg.Project)
	return nil
}

func (h *LangfuseHook) OnBeforeTurn(ctx context.Context, state *core.LoopState) ([]string, error) {
	// Record generation for the PREVIOUS turn (now complete).
	// When OnBeforeTurn fires for round N, state.TurnInputTokens holds
	// the tokens from round N-1 (set by loop.go after that turn's stream).
	if state.Round > 0 && h.trace != nil && !h.turnStart.IsZero() {
		model := h.prevModel
		if model == "" {
			model = h.modelName
		}
		end := time.Now()
		h.trace.Generation(
			fmt.Sprintf("turn-%d", state.Round-1),
			model,
			h.turnStart,
			end,
			state.TurnInputTokens,
			state.TurnOutputTokens,
			state.TurnInputTokens+state.TurnOutputTokens,
		)
	}

	// Start timing THIS turn
	h.turnStart = time.Now()
	h.prevModel = state.TurnModel
	return nil, nil
}

func (h *LangfuseHook) OnAfterToolCall(ctx context.Context, state *core.LoopState, toolName string, args map[string]any, result string, err error) error {
	if h.trace == nil {
		return nil
	}

	end := time.Now()
	start := h.turnStart
	if start.IsZero() {
		start = end.Add(-time.Second)
	}

	output := map[string]any{
		"round": state.Round,
	}
	if err != nil {
		output["error"] = err.Error()
	} else if len(result) > 500 {
		output["result_preview"] = result[:500]
	} else {
		output["result"] = result
	}

	h.trace.Span("tool:"+toolName, start, end, args, output)

	// Track tools seen for workflow classification
	h.toolsSeen[toolName] = true

	// Extract trading signals from execute_trade / signals tool output
	if err == nil && result != "" {
		h.extractSignals(toolName, result)
	}

	// Extract metrics from invest-bot tool results and post as Langfuse scores.
	// If the langfuse_metrics sentinel is present, recordMetricsScores handles everything.
	// Otherwise, fall back to recordPortfolioScores for legacy portfolio data.
	if err == nil && result != "" {
		if !h.recordMetricsScores(toolName, result) {
			h.recordPortfolioScores(toolName, result)
		}
	}

	return nil
}

// recordPortfolioScores parses portfolio data from invest-bot tool results
// and posts them as Langfuse scores (chartable in dashboards).
func (h *LangfuseHook) recordPortfolioScores(toolName, result string) {
	// Only check tools that return portfolio data, or bash that might invoke trade.py
	investTools := map[string]bool{
		"portfolio_status": true, "execute_trade": true,
		"portfolio_ledger": true, "app_portfolio_status": true,
		"app_execute_trade": true, "app_portfolio_ledger": true,
	}
	isInvest := investTools[toolName]
	isBash := toolName == "bash"
	if !isInvest && !isBash {
		return
	}

	// Parse JSON from the result. Bash tool results wrap output as {"output":"..."}.
	// We need to unwrap to get the actual portfolio JSON.
	data := parsePortfolioJSON(result)
	if data == nil {
		return
	}

	// Extract equity (top-level or nested in "portfolio" or "ledger")
	equity := extractFloat(data, "equity")
	if equity == 0 {
		if portfolio, ok := data["portfolio"].(map[string]any); ok {
			equity = extractFloat(portfolio, "equity")
		}
	}
	if equity == 0 {
		if ledger, ok := data["ledger"].(map[string]any); ok {
			equity = extractFloat(ledger, "equity")
		}
	}

	// Extract P&L fields (from ledger snapshot format)
	totalPnl := extractFloat(data, "total_pnl")
	dailyPnl := extractFloat(data, "daily_pnl")
	positionCount := extractFloat(data, "position_count")

	// Post scores for chartable metrics
	if equity > 0 {
		h.trace.Score("equity", equity, "NUMERIC", "Portfolio equity (USD)")
	}
	if totalPnl != 0 {
		h.trace.Score("total_pnl", totalPnl, "NUMERIC", "Cumulative P&L (USD)")
	}
	if dailyPnl != 0 {
		h.trace.Score("daily_pnl", dailyPnl, "NUMERIC", "Daily P&L (USD)")
	}
	if positionCount > 0 {
		h.trace.Score("position_count", positionCount, "NUMERIC", "Number of open positions")
	}
}

// parsePortfolioJSON extracts portfolio data from tool results.
// Handles: raw JSON, {"output":"..."} bash wrapper, nested unwrapping.
func parsePortfolioJSON(raw string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}

	// If top-level has equity or known portfolio keys, return as-is
	if _, hasEquity := data["equity"]; hasEquity {
		return data
	}
	if _, hasPortfolio := data["portfolio"]; hasPortfolio {
		return data
	}
	if _, hasLedger := data["ledger"]; hasLedger {
		return data
	}

	// Try unwrapping: {"output":"..."} or {"stdout":"..."} where the inner string is JSON
	for _, key := range []string{"output", "stdout"} {
		if val, ok := data[key].(string); ok && val != "" {
			var inner map[string]any
			if err := json.Unmarshal([]byte(val), &inner); err == nil {
				// Check if inner has portfolio data
				if _, hasEquity := inner["equity"]; hasEquity {
					return inner
				}
				if _, hasPortfolio := inner["portfolio"]; hasPortfolio {
					return inner
				}
				if _, hasLedger := inner["ledger"]; hasLedger {
					return inner
				}
			}
		}
	}

	return nil
}

// extractFloat safely extracts a float from a map.
func extractFloat(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	}
	return 0
}

// metricDescriptions maps invest-bot metric names to human-readable Langfuse score comments.
var metricDescriptions = map[string]string{
	"equity":                "Portfolio equity (USD)",
	"total_pnl":             "Cumulative P&L (USD)",
	"daily_pnl":             "Daily P&L (USD)",
	"position_count":        "Number of open positions",
	"return_pct":            "Total return (%)",
	"annualized_return_pct": "Annualized return (%)",
	"benchmark_return_pct":  "Benchmark SPY return (%)",
	"excess_return_pct":     "Excess return vs SPY (%)",
	"sharpe_ratio":          "Annualized Sharpe ratio",
	"sortino_ratio":         "Annualized Sortino ratio",
	"profit_factor":         "Gross wins / gross losses",
	"win_rate":              "Win rate (0-1)",
	"max_drawdown":          "Maximum drawdown (USD, negative)",
	"max_drawdown_pct":      "Maximum drawdown (%)",
	"volatility_pct":        "Annualized volatility (%)",
	"regime_composite":      "Market regime composite score (0-100)",
	"regime_confidence":     "Regime detection confidence (0-1)",
	"portfolio_heat":        "Portfolio heat / risk exposure (%)",
	"prediction_accuracy":   "Signal prediction accuracy (0-1)",
	"total_trades":          "Total number of trades",
	"round_trips":           "Number of round-trip trades",
	"avg_win":               "Average winning trade P&L (USD)",
	"avg_loss":              "Average losing trade P&L (USD)",
	"trading_days":          "Number of trading days in period",
}

// recordMetricsScores detects the langfuse_metrics sentinel in bash tool results
// and posts all numeric top-level keys as Langfuse scores.
// Returns true if the sentinel was found (meaning this handles all scoring).
func (h *LangfuseHook) recordMetricsScores(toolName, result string) bool {
	if toolName != "bash" {
		return false
	}

	data := parseMetricsJSON(result)
	if data == nil {
		return false
	}

	for key, val := range data {
		// Skip sentinel, meta, and string values (like regime_label)
		if key == "langfuse_metrics" || strings.HasPrefix(key, "_") {
			continue
		}
		fval, ok := toFloat64(val)
		if !ok {
			continue
		}
		comment := metricDescriptions[key]
		if comment == "" {
			comment = key
		}
		h.trace.Score(key, fval, "NUMERIC", comment)
	}

	// Track regime label for metadata enrichment
	h.updateRegimeContext(data)

	// Post simulation flag
	if data["simulation_mode"] == true {
		h.isSimulation = true
		h.trace.Score("is_simulation", 1, "BOOLEAN", "True for backtest/simulation, false for live")
	} else {
		h.trace.Score("is_simulation", 0, "BOOLEAN", "True for backtest/simulation, false for live")
	}
	return true
}

// parseMetricsJSON detects invest-bot metrics output from bash tool results.
// Returns the parsed map only if it contains the "langfuse_metrics": true sentinel.
func parseMetricsJSON(raw string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}

	if hasMetricsSentinel(data) {
		return data
	}

	// Try unwrapping {"output":"..."} bash wrapper
	for _, key := range []string{"output", "stdout"} {
		if val, ok := data[key].(string); ok && val != "" {
			var inner map[string]any
			if err := json.Unmarshal([]byte(val), &inner); err == nil {
				if hasMetricsSentinel(inner) {
					return inner
				}
			}
		}
	}

	return nil
}

func hasMetricsSentinel(m map[string]any) bool {
	v, ok := m["langfuse_metrics"]
	return ok && v == true
}

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// ── Workflow classification ────────────────────────────────────────

// classifyWorkflow determines the invest-bot workflow type from tools called.
// Posted as a CATEGORICAL score so dashboards can filter by workflow type.
func (h *LangfuseHook) classifyWorkflow() string {
	switch {
	case h.toolsSeen["execute_trade"] || h.toolsSeen["app_execute_trade"]:
		return "trade_execution"
	case h.toolsSeen["backtest"] || h.toolsSeen["app_backtest"] || h.isSimulation:
		return "backtest"
	case h.toolsSeen["metrics"] || h.toolsSeen["historical_metrics"]:
		return "metrics_collection"
	case h.toolsSeen["market_snapshot"] || h.toolsSeen["app_market_snapshot"]:
		return "market_scan"
	case h.toolsSeen["portfolio_status"] || h.toolsSeen["app_portfolio_status"] ||
		h.toolsSeen["portfolio_ledger"] || h.toolsSeen["app_portfolio_ledger"]:
		return "portfolio_review"
	case h.toolsSeen["bash"] && h.isSimulation:
		return "backtest"
	default:
		return "general"
	}
}

// ── Signal extraction ──────────────────────────────────────────────

// extractSignals parses trading signal data (ticker, action, confidence)
// from execute_trade and bash tool output. Posts per-signal scores for
// correlating model confidence with actual trade outcomes.
func (h *LangfuseHook) extractSignals(toolName, result string) {
	investTools := map[string]bool{
		"execute_trade": true, "app_execute_trade": true,
	}
	if !investTools[toolName] && toolName != "bash" {
		return
	}

	data := parseAnyJSON(result)
	if data == nil {
		return
	}

	// Check for signals array (from signals.py or trade.py output)
	if signals, ok := data["signals"].([]any); ok {
		for _, s := range signals {
			if sig, ok := s.(map[string]any); ok {
				h.scoreSignal(sig)
			}
		}
	}

	// Check for single signal or executed trade
	if _, hasSymbol := data["symbol"]; hasSymbol {
		h.scoreSignal(data)
	}

	// Check for trades array (from trade.py --status)
	if trades, ok := data["trades"].([]any); ok {
		for _, t := range trades {
			if trade, ok := t.(map[string]any); ok {
				h.scoreSignal(trade)
			}
		}
	}

	// Track simulation mode from metrics output
	if data["simulation_mode"] == true {
		h.isSimulation = true
		h.trace.Score("is_simulation", 1, "BOOLEAN", "True for backtest/simulation, false for live")
	}
}

// scoreSignal posts per-signal scores: direction and confidence.
func (h *LangfuseHook) scoreSignal(sig map[string]any) {
	symbol, _ := sig["symbol"].(string)
	if symbol == "" {
		return
	}
	symbol = strings.ToUpper(symbol)

	// Action/direction
	action := ""
	for _, key := range []string{"action", "side", "direction", "signal"} {
		if v, ok := sig[key].(string); ok && v != "" {
			action = strings.ToLower(v)
			break
		}
	}
	if action == "" {
		if side, ok := sig["side"].(string); ok {
			action = strings.ToLower(side)
		}
	}

	// Normalize action to canonical directions
	direction := "unknown"
	switch {
	case strings.Contains(action, "buy") || strings.Contains(action, "long"):
		direction = "buy"
	case strings.Contains(action, "sell") || strings.Contains(action, "short"):
		direction = "sell"
	case strings.Contains(action, "hold"):
		direction = "hold"
	}

	if direction != "unknown" {
		h.trace.Score("signal_direction:"+symbol, 0, "CATEGORICAL", "Trade signal direction for "+symbol)
		// Store direction in comment since CATEGORICAL value must be a string
		// We use a NUMERIC proxy: 1=buy, -1=sell, 0=hold
		dirVal := 0.0
		switch direction {
		case "buy":
			dirVal = 1
		case "sell":
			dirVal = -1
		}
		h.trace.Score("signal:"+symbol, dirVal, "NUMERIC", "Signal: "+direction)
	}

	// Confidence
	confidence := extractFloat(sig, "confidence")
	if confidence > 0 {
		// Normalize to 0-1 if on 0-100 scale
		if confidence > 1 {
			confidence = confidence / 100
		}
		h.trace.Score("signal_confidence:"+symbol, confidence, "NUMERIC", "Model confidence for "+symbol)
	}
}

// ── Regime metadata ────────────────────────────────────────────────

// updateRegimeContext extracts regime label from metrics output and
// enriches trace metadata for dashboard filtering by market regime.
func (h *LangfuseHook) updateRegimeContext(data map[string]any) {
	if label, ok := data["regime_label"].(string); ok && label != "" {
		h.regimeLabel = label
	}
}

// parseAnyJSON tries to parse JSON, unwrapping {"output":"..."} if needed.
func parseAnyJSON(raw string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil
	}

	// Try unwrapping bash wrapper
	for _, key := range []string{"output", "stdout"} {
		if val, ok := data[key].(string); ok && val != "" {
			var inner map[string]any
			if err := json.Unmarshal([]byte(val), &inner); err == nil {
				return inner
			}
		}
	}
	return data
}

func (h *LangfuseHook) OnAgentEnd(ctx context.Context, state *core.LoopState) error {
	if h.trace == nil {
		return nil
	}

	// Record the final turn's generation
	if !h.turnStart.IsZero() {
		model := h.prevModel
		if model == "" {
			model = h.modelName
		}
		h.trace.Generation(
			fmt.Sprintf("turn-%d-final", state.Round),
			model,
			h.turnStart,
			time.Now(),
			state.TurnInputTokens,
			state.TurnOutputTokens,
			state.TurnInputTokens+state.TurnOutputTokens,
		)
	}

	// Post workflow type classification for invest-bot traces
	if len(h.toolsSeen) > 0 {
		workflow := h.classifyWorkflow()
		// Use NUMERIC proxy for workflow type (CATEGORICAL value must be string in API)
		// 1=trade, 2=scan, 3=backtest, 4=metrics, 5=review, 0=general
		workflowVal := 0.0
		switch workflow {
		case "trade_execution":
			workflowVal = 1
		case "market_scan":
			workflowVal = 2
		case "backtest":
			workflowVal = 3
		case "metrics_collection":
			workflowVal = 4
		case "portfolio_review":
			workflowVal = 5
		}
		h.trace.Score("workflow_type", workflowVal, "NUMERIC", "Workflow: "+workflow)
	}

	// Post regime as metadata for dashboard filtering
	if h.regimeLabel != "" {
		h.trace.Score("regime", 0, "CATEGORICAL", "Market regime: "+h.regimeLabel)
	}

	// Flush all buffered events
	h.client.Flush()

	log.Printf("Langfuse trace closed: session=%s rounds=%d tokens_in=%d tokens_out=%d",
		state.SessionID, state.Round, state.TotalInputTokens, state.TotalOutputTokens)
	return nil
}
