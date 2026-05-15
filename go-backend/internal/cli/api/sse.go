package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// SSEEvent represents a parsed Server-Sent Event.
type SSEEvent struct {
	Type string
	Data json.RawMessage
}

// SSEEventType constants — must match CONTRACT.md event names
const (
	EventTextDelta       = "text_delta"
	EventThinkingDelta   = "thinking_delta"
	EventToolStart       = "tool_execution_start"
	EventToolEnd         = "tool_execution_end"
	EventToolUpdate      = "tool_update"
	EventAgentStart      = "agent_start"
	EventAgentEnd        = "agent_end"
	EventAgentSpawned    = "agent_spawned"
	EventError           = "error"
	EventCompactionStart = "compaction_start"
	EventCompactionEnd   = "compaction_end"
	EventApprovalRequest  = "approval_request"  // legacy
	EventUserQuestion     = "user_question"     // legacy
	EventDecisionRequest  = "decision_request"  // unified HITL
	EventArtifactCreated = "artifact_created"
	EventArtifactUpdated = "artifact_updated"
	EventPlanCreated     = "plan_created"
	EventPlanUpdated     = "plan_updated"
	EventSubagentStart   = "subagent_start"
	EventSubagentEnd     = "subagent_end"
	EventHookRequest     = "hook_request"
	EventStepStart       = "step_start"
	EventStepEnd         = "step_end"
)

// TextDeltaData is the payload for text_delta events.
type TextDeltaData struct {
	Text      string `json:"text"`
	AgentName string `json:"agentName,omitempty"`
}

// ThinkingDeltaData is the payload for thinking_delta events.
type ThinkingDeltaData struct {
	Text      string `json:"text"`
	AgentName string `json:"agentName,omitempty"`
}

// ToolStartData is the payload for tool_execution_start events.
type ToolStartData struct {
	ToolName  string          `json:"toolName"`
	ToolID    string          `json:"toolId"`
	Args      json.RawMessage `json:"args"`
	AgentName string          `json:"agentName,omitempty"`
}

// ToolEndData is the payload for tool_execution_end events.
type ToolEndData struct {
	ToolName  string          `json:"toolName"`
	ToolID    string          `json:"toolId"`
	Result    json.RawMessage `json:"result"`
	Error     string          `json:"error,omitempty"`
	AgentName string          `json:"agentName,omitempty"`
}

// ToolUpdateData is the payload for tool_update events.
type ToolUpdateData struct {
	ToolName  string `json:"toolName"`
	ToolID    string `json:"toolId"`
	Text      string `json:"text"`
	AgentName string `json:"agentName,omitempty"`
}

// AgentEndData is the payload for agent_end events.
type AgentEndData struct {
	Input         int     `json:"input"`
	Output        int     `json:"output"`
	Cache         int     `json:"cache"`
	Model         string  `json:"model"`
	ContextWindow int     `json:"contextWindow,omitempty"`
}

// SubagentStartData is the payload for subagent_start events.
type SubagentStartData struct {
	AgentName string `json:"agentName"`
	Task      string `json:"task"`
	ToolName  string `json:"toolName,omitempty"`
}

// SubagentEndData is the payload for subagent_end events.
type SubagentEndData struct {
	AgentName string `json:"agentName"`
	Status    string `json:"status"`
	Task      string `json:"task"`
	Error     string `json:"error,omitempty"`
}

// CompactionEndData is the payload for compaction_end events.
type CompactionEndData struct {
	CompactedMessages int     `json:"compactedMessages"`
	KeptMessages      int     `json:"keptMessages"`
	ContextTokens     int     `json:"contextTokens"`
	ContextSize       int     `json:"contextSize"`
	ContextUtil       float64 `json:"contextUtil"`
	CompactionType    string  `json:"compactionType"`
}

// ApprovalData is the payload for approval_request events.
type ApprovalData struct {
	RequestID  string `json:"requestId"`
	Title      string `json:"title,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	ToolArgs   string `json:"toolArgs,omitempty"`
	Message    string `json:"message,omitempty"`
	Risk       string `json:"risk,omitempty"`
}

// UserQuestionData is the payload for user_question events.
type UserQuestionData struct {
	QuestionID    string   `json:"questionId"`
	Question      string   `json:"question"`
	Options       []string `json:"options,omitempty"`
	AllowFreeText bool     `json:"allowFreeText,omitempty"`
	Default       string   `json:"default,omitempty"`
}

// ArtifactData is the payload for artifact events.
type ArtifactData struct {
	ArtifactID string `json:"artifactId"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Content    string `json:"content"`
}

// PlanCreatedData is the payload for plan_created events.
type PlanCreatedData struct {
	PlanID   string `json:"planId"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	FilePath string `json:"filePath"`
}

// HookRequestData is the payload for hook_request events.
type HookRequestData struct {
	HookID    string         `json:"hookId"`
	HookPoint string         `json:"hookPoint"`
	ToolName  string         `json:"toolName"`
	Args      map[string]any `json:"args"`
}

// StreamSSE reads SSE events from a response body and sends them to a channel.
// Blocks until the body is fully read or an error occurs.
func StreamSSE(body io.Reader, ch chan<- SSEEvent) error {
	defer close(ch)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	for scanner.Scan() {
		line := scanner.Text()

		// Empty line = end of event
		if line == "" {
			eventType = ""
			continue
		}

		// Parse event type
		if len(line) > 6 && line[:6] == "event:" {
			eventType = line[6:]
			if len(eventType) > 0 && eventType[0] == ' ' {
				eventType = eventType[1:]
			}
			continue
		}

		// Parse data
		if len(line) > 5 && line[:5] == "data:" {
			data := line[5:]
			if data[0] == ' ' {
				data = data[1:]
			}
			if eventType != "" && data != "" {
				ch <- SSEEvent{
					Type: eventType,
					Data: json.RawMessage(data),
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("SSE read error: %w", err)
	}
	return nil
}

// ParseSSEData unmarshals SSE event data into a struct.
func ParseSSEData(data json.RawMessage, out any) error {
	return json.Unmarshal(data, out)
}
