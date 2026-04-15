package llama

import (
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
)

// ConvertEvent converts a llama AgentEvent to a pi AgentEvent for SSE streaming.
// This allows the frontend SSE handler to work with both Pi RPC events
// and llama-go library events without modification.
func ConvertEvent(evt AgentEvent) pi.AgentEvent {
	// Convert our event types to Pi's RPC event types
	var rpcType string
	switch evt.Type {
	case EventTypeTextDelta:
		// Pi sends text deltas as message_update with nested assistantMessageEvent
		return pi.AgentEvent{
			Type: "message_update",
			AssistantMessageEvent: &pi.AssistantMessageEvent{
				Type:  "text_delta",
				Delta: evt.Data.Text,
			},
		}
	case EventTypeThinkingDelta:
		return pi.AgentEvent{
			Type: "message_update",
			AssistantMessageEvent: &pi.AssistantMessageEvent{
				Type:  "thinking_delta",
				Delta: evt.Data.Text,
			},
		}
	case EventTypeToolStart:
		rpcType = pi.RpcEventToolStart
	case EventTypeToolEnd:
		rpcType = pi.RpcEventToolEnd
	case EventTypeAgentStart:
		rpcType = pi.RpcEventAgentStart
	case EventTypeAgentEnd:
		rpcType = pi.RpcEventAgentEnd
	case EventTypeError:
		rpcType = pi.RpcEventError
	default:
		return pi.AgentEvent{Type: "unknown"}
	}

	// Build the data payload
	data := pi.AgentEventData{
		ToolName: evt.Data.ToolName,
		ToolArgs: evt.Data.ToolArgs,
		ToolId:   evt.Data.ToolID,
		Result:   evt.Data.Result,
		Error:    evt.Data.Error,
		Model:    evt.Data.Model,
		Input:    evt.Data.Input,
		Output:   evt.Data.Output,
	}

	// For agent_end, include usage in messages field (Pi sends it there)
	if evt.Type == EventTypeAgentEnd {
		msgs, _ := json.Marshal([]struct {
			Role    string `json:"role"`
			Usage   struct {
				Input  float64 `json:"input"`
				Output float64 `json:"output"`
			} `json:"usage"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}{{
			Role:     "assistant",
			Usage:    struct {
				Input  float64 `json:"input"`
				Output float64 `json:"output"`
			}{Input: evt.Data.Input, Output: evt.Data.Output},
			Provider: "llama-go",
			Model:    "gemma-4-26b",
		}})
		return pi.AgentEvent{
			Type:     rpcType,
			Data:     data,
			Messages: msgs,
		}
	}

	return pi.AgentEvent{
		Type: rpcType,
		Data: data,
	}
}
