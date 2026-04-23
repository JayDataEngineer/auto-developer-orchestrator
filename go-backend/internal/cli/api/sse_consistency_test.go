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
		local    string
		canonical llama.AgentEventType
	}{
		{EventTextDelta, llama.EventTypeTextDelta},
		{EventThinkingDelta, llama.EventTypeThinkingDelta},
		{EventToolStart, llama.EventTypeToolStart},
		{EventToolEnd, llama.EventTypeToolEnd},
		{EventAgentEnd, llama.EventTypeAgentEnd},
		{EventError, llama.EventTypeError},
		{EventStateUpdate, llama.EventTypeStateUpdate},
		{EventApprovalRequest, llama.EventTypeApprovalRequest},
		{EventArtifactCreated, llama.EventTypeArtifactCreated},
		{EventArtifactUpdated, llama.EventTypeArtifactUpdated},
		{EventSubagentStart, llama.EventTypeSubAgentStart},
		{EventSubagentEnd, llama.EventTypeSubAgentEnd},
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
