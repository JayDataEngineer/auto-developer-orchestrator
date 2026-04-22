package llama

import (
	"encoding/json"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
)

// ConvertEvent converts a llama AgentEvent to a pi AgentEvent for SSE streaming.
func ConvertEvent(evt AgentEvent) pi.AgentEvent {
	var rpcType string
	switch evt.Type {
	case EventTypeTextDelta:
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
	case EventTypeArtifactCreated:
		rpcType = pi.EventArtifactCreated
	case EventTypeArtifactUpdated:
		rpcType = pi.EventArtifactUpdated
	case EventTypePlanCreated:
		rpcType = pi.EventPlanCreated
	case EventTypePlanUpdated:
		rpcType = pi.EventPlanUpdated
	case EventTypeSubAgentStart:
		rpcType = pi.EventSubAgentStart
	case EventTypeSubAgentEnd:
		rpcType = pi.EventSubAgentEnd
	case EventTypeApprovalRequest:
		rpcType = pi.EventApprovalRequest
	case EventTypeCompactionStart:
		rpcType = pi.RpcEventCompactionStart
	case EventTypeCompactionEnd:
		rpcType = pi.RpcEventCompactionEnd
	default:
		return pi.AgentEvent{Type: "unknown"}
	}

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

	if evt.Type == EventTypeAgentEnd {
		msgs, _ := json.Marshal([]struct {
			Role     string `json:"role"`
			Usage    struct {
				Input  float64 `json:"input"`
				Output float64 `json:"output"`
			} `json:"usage"`
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}{{
			Role: "assistant",
			Usage: struct {
				Input  float64 `json:"input"`
				Output float64 `json:"output"`
			}{Input: evt.Data.Input, Output: evt.Data.Output},
			Provider: "llama-server",
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
