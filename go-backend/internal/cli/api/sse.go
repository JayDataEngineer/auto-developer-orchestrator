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

// SSEEventType constants — must match the backend's pi/types.go
const (
	EventTextDelta        = "text_delta"
	EventThinkingDelta    = "thinking_delta"
	EventToolStart        = "tool_execution_start"
	EventToolEnd          = "tool_execution_end"
	EventApprovalRequest  = "approval_request"
	EventArtifactCreated  = "artifact_created"
	EventArtifactUpdated  = "artifact_updated"
	EventAgentEnd         = "agent_end"
	EventError            = "error"
	EventStateUpdate      = "state_update"
	EventSubagentStart    = "subagent_start"
	EventSubagentEnd      = "subagent_end"
)

// TextDeltaData is the payload for text_delta events.
type TextDeltaData struct {
	Text string `json:"text"`
}

// ToolStartData is the payload for tool_execution_start events.
type ToolStartData struct {
	ToolName string          `json:"toolName"`
	ToolID   string          `json:"toolId"`
	Args     json.RawMessage `json:"args"`
}

// ToolEndData is the payload for tool_execution_end events.
type ToolEndData struct {
	ToolName string `json:"toolName"`
	ToolID   string `json:"toolId"`
	Result   string `json:"result"`
	Error    string `json:"error"`
}

// ApprovalData is the payload for approval_request events.
type ApprovalData struct {
	RequestID string `json:"requestId"`
	Type      string `json:"type"`
	ToolName  string `json:"toolName"`
	ToolArgs  string `json:"toolArgs"`
	Message   string `json:"message"`
	Risk      string `json:"risk"`
}

// AgentEndData is the payload for agent_end events.
type AgentEndData struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// ArtifactData is the payload for artifact events.
type ArtifactData struct {
	ArtifactID string `json:"artifactId"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Content    string `json:"content"`
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
func ParseSSEData(data json.RawMessage, out interface{}) error {
	return json.Unmarshal(data, out)
}
