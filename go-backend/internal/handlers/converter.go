package handlers

import (
	"github.com/auto-developer-orchestrator/backend/internal/core"
	llama "github.com/auto-developer-orchestrator/backend/internal/llama"
)

// convertCoreEventToLlama maps a new-style core.AgentEvent to the legacy
// llama.AgentEvent format so the existing SSE streaming works unchanged.
func convertCoreEventToLlama(evt core.AgentEvent) llama.AgentEvent {
	return llama.AgentEvent{
		Type: llama.AgentEventType(evt.Type),
		Data: llama.AgentEventData{
			Text:              evt.Data.Text,
			ToolName:          evt.Data.ToolName,
			ToolArgs:          evt.Data.ToolArgs,
			ToolID:            evt.Data.ToolID,
			Result:            evt.Data.Result,
			Error:             evt.Data.Error,
			Input:             evt.Data.Input,
			Output:            evt.Data.Output,
			Model:             evt.Data.Model,
			CompactedMessages: evt.Data.CompactedMessages,
			KeptMessages:      evt.Data.KeptMessages,
		},
	}
}
