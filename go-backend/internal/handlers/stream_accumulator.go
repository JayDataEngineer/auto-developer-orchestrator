package handlers

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// savedToolCall captures tool call metadata for persistence.
type savedToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

// savedToolResult captures a tool result for persistence.
type savedToolResult struct {
	ToolCallID string
	ToolName   string
	Content    string
}

// StreamAccumulator processes typed agent events and accumulates state
// for persistence (text, thinking, tool calls, tool results).
type StreamAccumulator struct {
	mu sync.Mutex

	text        strings.Builder
	thinking    strings.Builder
	toolCalls   []savedToolCall
	toolResults []savedToolResult
}

// NewStreamAccumulator creates a ready-to-use accumulator.
func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{}
}

// ProcessEvent updates internal state from a single agent event.
func (a *StreamAccumulator) ProcessEvent(evt core.AgentEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch p := evt.Data.(type) {
	case core.TextDelta:
		a.text.WriteString(p.Text)

	case core.ThinkingDelta:
		a.thinking.WriteString(p.Text)

	case core.ErrorEventData:
		if p.Error != "" {
			if a.text.Len() > 0 {
				a.text.WriteString("\n\n")
			}
			a.text.WriteString("Error: ")
			a.text.WriteString(p.Error)
		}

	case core.ToolStart:
		a.toolCalls = append(a.toolCalls, savedToolCall{
			ID:   p.ToolID,
			Name: p.ToolName,
			Args: p.ToolArgs,
		})

	case core.ToolEnd:
		var resultContent string
		if p.Result != nil {
			if b, err := json.Marshal(p.Result); err == nil {
				resultContent = string(b)
			}
		}
		if p.Error != "" {
			if resultContent != "" {
				resultContent += "\n"
			}
			resultContent += "Error: " + p.Error
		}
		a.toolResults = append(a.toolResults, savedToolResult{
			ToolCallID: p.ToolID,
			ToolName:   p.ToolName,
			Content:    resultContent,
		})
	}
}

// Text returns accumulated assistant text.
func (a *StreamAccumulator) Text() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.text.String()
}

// Thinking returns accumulated thinking text.
func (a *StreamAccumulator) Thinking() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.thinking.String()
}

// ToolCallsJSON returns accumulated tool calls as a JSON array.
// Returns "[]" if no tool calls were recorded.
func (a *StreamAccumulator) ToolCallsJSON() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.toolCalls) == 0 {
		return "[]"
	}
	b, err := json.Marshal(a.toolCalls)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ToolResults returns accumulated tool results.
func (a *StreamAccumulator) ToolResults() []savedToolResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]savedToolResult, len(a.toolResults))
	copy(out, a.toolResults)
	return out
}
