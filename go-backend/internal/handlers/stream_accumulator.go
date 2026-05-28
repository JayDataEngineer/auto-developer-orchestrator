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
	Args map[string]any `json:"args,omitempty"`
}

// savedSubAgent captures the full execution trace of a delegated sub-agent.
type savedSubAgent struct {
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	ToolCalls []savedToolCall `json:"toolCalls"`
	Thinking  string          `json:"thinking,omitempty"`
	Text      string          `json:"text,omitempty"`
	Result    string          `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// jsonSubAgentToolCall extends savedToolCall with result/error for serialization.
type jsonSubAgentToolCall struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

// jsonSubAgent is the serialization shape for sub-agents (tool calls include results).
type jsonSubAgent struct {
	Name      string                 `json:"name"`
	Status    string                 `json:"status"`
	ToolCalls []jsonSubAgentToolCall `json:"toolCalls"`
	Thinking  string                 `json:"thinking,omitempty"`
	Text      string                 `json:"text,omitempty"`
	Result    string                 `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// jsonToolCall is the serialization shape — extends savedToolCall with optional SubAgent.
type jsonToolCall struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Args     map[string]any `json:"args,omitempty"`
	Result   string        `json:"result,omitempty"`
	Error    string        `json:"error,omitempty"`
	SubAgent *jsonSubAgent `json:"subAgent,omitempty"`
}

// savedToolResult captures a tool result for the legacy tool_executions table.
type savedToolResult struct {
	ToolCallID string
	ToolName   string
	Content    string
}

// delegateCall links a delegate_to savedToolCall with its sub-agent trace.
type delegateCall struct {
	call     *savedToolCall
	subAgent *savedSubAgent
}

// StreamAccumulator processes typed agent events and accumulates state
// for persistence (text, thinking, tool calls with sub-agent hierarchy).
type StreamAccumulator struct {
	mu sync.Mutex

	text        strings.Builder
	thinking    strings.Builder
	toolCalls   []savedToolCall
	toolResults []savedToolResult

	// Sub-agent tracking: agentName → sub-agent trace
	activeSubAgents map[string]*savedSubAgent
	// delegate_to calls awaiting SubAgentStart (most recent first match)
	pendingDelegates []*delegateCall
}

// NewStreamAccumulator creates a ready-to-use accumulator.
func NewStreamAccumulator() *StreamAccumulator {
	return &StreamAccumulator{
		activeSubAgents: make(map[string]*savedSubAgent),
	}
}

// ProcessEvent updates internal state from a single agent event.
// Events with AgentName != "" are routed to the active sub-agent.
// CTO events (AgentName == "") are handled normally.
func (a *StreamAccumulator) ProcessEvent(evt core.AgentEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch p := evt.Data.(type) {
	case core.TextDelta:
		if p.AgentName != "" {
			if sa, ok := a.activeSubAgents[p.AgentName]; ok {
				sa.Text += p.Text
			}
		} else {
			a.text.WriteString(p.Text)
		}

	case core.ThinkingDelta:
		if p.AgentName != "" {
			if sa, ok := a.activeSubAgents[p.AgentName]; ok {
				sa.Thinking += p.Text
			}
		} else {
			a.thinking.WriteString(p.Text)
		}

	case core.ErrorEventData:
		if p.Error != "" {
			if a.text.Len() > 0 {
				a.text.WriteString("\n\n")
			}
			a.text.WriteString("Error: ")
			a.text.WriteString(p.Error)
		}

	case core.ToolStart:
		if p.AgentName != "" {
			// Sub-agent tool call — append to active sub-agent
			if sa, ok := a.activeSubAgents[p.AgentName]; ok {
				sa.ToolCalls = append(sa.ToolCalls, savedToolCall{
					ID:   p.ToolID,
					Name: p.ToolName,
					Args: p.ToolArgs,
				})
			}
		} else {
			// CTO tool call
			call := savedToolCall{
				ID:   p.ToolID,
				Name: p.ToolName,
				Args: p.ToolArgs,
			}
			a.toolCalls = append(a.toolCalls, call)

			// Track delegate_to / delegate_async for sub-agent nesting
			if p.ToolName == "delegate_to" || p.ToolName == "delegate_async" {
				ptr := &a.toolCalls[len(a.toolCalls)-1]
				a.pendingDelegates = append(a.pendingDelegates, &delegateCall{call: ptr})
			}
		}

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

		// Always append to toolResults for legacy SaveToolResult compat
		a.toolResults = append(a.toolResults, savedToolResult{
			ToolCallID: p.ToolID,
			ToolName:   p.ToolName,
			Content:    resultContent,
		})

	case core.SubAgentStartData:
		// Find the most recent pending delegate and link it
		for i := len(a.pendingDelegates) - 1; i >= 0; i-- {
			dc := a.pendingDelegates[i]
			if dc.subAgent == nil {
				sa := &savedSubAgent{
					Name:   p.AgentName,
					Status: "running",
				}
				dc.subAgent = sa
				a.activeSubAgents[p.AgentName] = sa
				break
			}
		}

	case core.SubAgentEndData:
		if sa, ok := a.activeSubAgents[p.AgentName]; ok {
			sa.Status = p.Status
			if sa.Status == "" {
				if p.Error != "" {
					sa.Status = "error"
				} else {
					sa.Status = "completed"
				}
			}
			sa.Result = p.Result
			sa.Error = p.Error
			delete(a.activeSubAgents, p.AgentName)
		}
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

// ToolCallsJSON returns accumulated tool calls as a hierarchical JSON array.
// Delegate tools get a nested subAgent field with the full sub-agent trace.
// Sub-agent tool calls include their results, resolved from the toolResults array.
func (a *StreamAccumulator) ToolCallsJSON() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.toolCalls) == 0 {
		return "[]"
	}

	// Build toolCallID → result content lookup
	resultByID := make(map[string]string, len(a.toolResults))
	for _, tr := range a.toolResults {
		resultByID[tr.ToolCallID] = tr.Content
	}

	// Build delegate lookup: toolCall ID → delegateCall
	delegateByID := make(map[string]*delegateCall, len(a.pendingDelegates))
	for _, dc := range a.pendingDelegates {
		if dc.call != nil {
			delegateByID[dc.call.ID] = dc
		}
	}

	// Build set of sub-agent tool call IDs (to distinguish from CTO results)
	subToolIDs := make(map[string]bool)
	for _, dc := range a.pendingDelegates {
		if dc.subAgent != nil {
			for _, tc := range dc.subAgent.ToolCalls {
				subToolIDs[tc.ID] = true
			}
		}
	}

	out := make([]jsonToolCall, 0, len(a.toolCalls))
	for _, tc := range a.toolCalls {
		jtc := jsonToolCall{
			ID:   tc.ID,
			Name: tc.Name,
			Args: tc.Args,
		}

		// Attach CTO tool result (skip sub-agent tool IDs)
		if content, ok := resultByID[tc.ID]; ok && !subToolIDs[tc.ID] {
			jtc.Result = content
		}

		// Attach sub-agent trace for delegate tools
		if dc, ok := delegateByID[tc.ID]; ok && dc.subAgent != nil {
			sa := dc.subAgent
			jsa := &jsonSubAgent{
				Name:     sa.Name,
				Status:   sa.Status,
				Thinking: sa.Thinking,
				Text:     sa.Text,
				Result:   sa.Result,
				Error:    sa.Error,
			}
			// Build tool calls with results
			jsa.ToolCalls = make([]jsonSubAgentToolCall, len(sa.ToolCalls))
			for i, stc := range sa.ToolCalls {
				jtc2 := jsonSubAgentToolCall{
					ID:   stc.ID,
					Name: stc.Name,
					Args: stc.Args,
				}
				if content, ok := resultByID[stc.ID]; ok {
					jtc2.Result = content
				}
				jsa.ToolCalls[i] = jtc2
			}
			jtc.SubAgent = jsa
		}

		out = append(out, jtc)
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ToolResults returns accumulated tool results (legacy path for SaveToolResult).
func (a *StreamAccumulator) ToolResults() []savedToolResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]savedToolResult, len(a.toolResults))
	copy(out, a.toolResults)
	return out
}
