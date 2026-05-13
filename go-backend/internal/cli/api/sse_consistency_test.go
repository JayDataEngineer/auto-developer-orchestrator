package api

import (
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/llama"
)

// TestSSEConsistency verifies that the CLI's local SSE event constants
// match the canonical llama.AgentEventType constants. This prevents drift
// between the client and server event types.
func TestSSEConsistency(t *testing.T) {
	tests := []struct {
		local     string
		canonical llama.AgentEventType
	}{
		{EventTextDelta, llama.EventTypeTextDelta},
		{EventThinkingDelta, llama.EventTypeThinkingDelta},
		{EventToolStart, llama.EventTypeToolStart},
		{EventToolEnd, llama.EventTypeToolEnd},
		{EventToolUpdate, llama.EventTypeToolUpdate},
		{EventAgentStart, llama.EventTypeAgentStart},
		{EventAgentEnd, llama.EventTypeAgentEnd},
		{EventAgentSpawned, llama.EventTypeAgentSpawned},
		{EventError, llama.EventTypeError},
		{EventCompactionStart, llama.EventTypeCompactionStart},
		{EventCompactionEnd, llama.EventTypeCompactionEnd},
		{EventApprovalRequest, llama.EventTypeApprovalRequest},
		{EventArtifactCreated, llama.EventTypeArtifactCreated},
		{EventArtifactUpdated, llama.EventTypeArtifactUpdated},
		{EventPlanCreated, llama.EventTypePlanCreated},
		{EventPlanUpdated, llama.EventTypePlanUpdated},
		{EventSubagentStart, llama.EventTypeSubAgentStart},
		{EventSubagentEnd, llama.EventTypeSubAgentEnd},
		{EventGrindAttempt, llama.EventTypeGrindAttempt},
		{EventGrindVerify, llama.EventTypeGrindVerify},
		{EventGrindEnd, llama.EventTypeGrindEnd},
		{EventHookRequest, llama.EventTypeHookRequest},
		{EventStepStart, llama.EventTypeStepStart},
		{EventStepEnd, llama.EventTypeStepEnd},
	}

	for _, tt := range tests {
		if tt.local != string(tt.canonical) {
			t.Errorf("constant mismatch: api.%s = %q, llama.%s = %q",
				nameOf(tt.local), tt.local,
				nameOf(string(tt.canonical)), string(tt.canonical),
			)
		}
	}
}

// nameOf returns a readable name for the constant value.
func nameOf(v string) string {
	return v // The constant values are self-descriptive
}
