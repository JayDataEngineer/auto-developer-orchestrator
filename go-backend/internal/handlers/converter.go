package handlers

import (
	"github.com/auto-developer-orchestrator/backend/internal/core"
	llama "github.com/auto-developer-orchestrator/backend/internal/llama"
)

// convertCoreEventToLlama maps a new-style core.AgentEvent to the legacy
// llama.AgentEvent format so the existing SSE streaming works unchanged.
// Both types are type aliases of the same core types, so direct cast works.
func convertCoreEventToLlama(evt core.AgentEvent) llama.AgentEvent {
	return llama.AgentEvent(evt)
}
