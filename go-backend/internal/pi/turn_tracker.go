package pi

import (
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TurnTracker tracks tool calls within a single agent turn
type TurnTracker struct {
	mu              sync.Mutex
	turnID          string
	pendingTools    map[string]*PendingToolCall
	completedTools  []ToolCallRecord
	logger          *zap.Logger
}

// PendingToolCall tracks a tool call waiting for its result
type PendingToolCall struct {
	ToolName string
	Input    map[string]interface{}
	CalledAt int64 // unix ms
}

// ToolCallRecord is the completed record
type ToolCallRecord struct {
	ToolName  string
	Input     map[string]interface{}
	Output    string
	IsError   bool
	DurationMs int64
}

// NewTurnTracker creates a new turn tracker
func NewTurnTracker(logger *zap.Logger) *TurnTracker {
	return &TurnTracker{
		pendingTools: make(map[string]*PendingToolCall),
		logger:       logger,
	}
}

// Reset clears the tracker for a new turn
func (tt *TurnTracker) Reset(turnID string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tt.turnID = turnID
	tt.pendingTools = make(map[string]*PendingToolCall)
	tt.completedTools = nil
}

// TrackPending records a tool call that's about to execute
func (tt *TurnTracker) TrackPending(toolName string, input map[string]interface{}) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tt.pendingTools[toolName] = &PendingToolCall{
		ToolName: toolName,
		Input:    input,
		CalledAt: timeNowMs(),
	}

	tt.logger.Debug("tool call pending",
		zap.String("turn", tt.turnID),
		zap.String("tool", toolName),
	)
}

// MarkCompleted records a tool call result
func (tt *TurnTracker) MarkCompleted(toolName string, output string, isError bool) *ToolCallRecord {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	pending, exists := tt.pendingTools[toolName]
	var duration int64
	if exists {
		duration = timeNowMs() - pending.CalledAt
		delete(tt.pendingTools, toolName)
	}

	record := &ToolCallRecord{
		ToolName:   toolName,
		Input:      pending.Input,
		Output:     output,
		IsError:    isError,
		DurationMs: duration,
	}

	tt.completedTools = append(tt.completedTools, *record)

	tt.logger.Debug("tool call completed",
		zap.String("turn", tt.turnID),
		zap.String("tool", toolName),
		zap.Bool("error", isError),
		zap.Int64("duration_ms", duration),
	)

	return record
}

// HasPending returns true if there are still pending tool calls
func (tt *TurnTracker) HasPending() bool {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return len(tt.pendingTools) > 0
}

// PendingCount returns the number of pending tool calls
func (tt *TurnTracker) PendingCount() int {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return len(tt.pendingTools)
}

// CompletedCount returns the number of completed tool calls
func (tt *TurnTracker) CompletedCount() int {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return len(tt.completedTools)
}

// AllToolsInTurn returns all tool calls (pending + completed) for the current turn
func (tt *TurnTracker) AllToolsInTurn() ([]PendingToolCall, []ToolCallRecord) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	pending := make([]PendingToolCall, 0, len(tt.pendingTools))
	for _, p := range tt.pendingTools {
		pending = append(pending, *p)
	}

	completed := make([]ToolCallRecord, len(tt.completedTools))
	copy(completed, tt.completedTools)

	return pending, completed
}

// SummaryJSON returns a JSON summary of the turn
func (tt *TurnTracker) SummaryJSON() string {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	summary := map[string]interface{}{
		"turn_id":          tt.turnID,
		"completed_tools": len(tt.completedTools),
		"pending_tools":    len(tt.pendingTools),
	}

	data, _ := json.Marshal(summary)
	return string(data)
}

func timeNowMs() int64 {
	return time.Now().UnixMilli()
}
