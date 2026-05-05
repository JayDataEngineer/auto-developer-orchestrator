package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
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

	// Extract portfolio metrics from invest-bot tool results and post as scores
	if err == nil && result != "" {
		h.recordPortfolioScores(toolName, result)
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

	// Flush all buffered events
	h.client.Flush()

	log.Printf("Langfuse trace closed: session=%s rounds=%d tokens_in=%d tokens_out=%d",
		state.SessionID, state.Round, state.TotalInputTokens, state.TotalOutputTokens)
	return nil
}
