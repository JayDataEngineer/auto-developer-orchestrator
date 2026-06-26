package handlers

import (
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
)

// TestMapEventToSSEAllKnownEvents exercises the mapper for every event type
// in core/event_types.go. **When you add a new event type to event_types.go,
// add a row here AND a case to mapEventToSSE.** The mapper's `default: return
// nil` silently drops unknown event types — this test exists to catch that
// trap forever. (See feedback_multiagent_harness.md / feedback_safeguard_router
// for prior incidents where an event was wired through the bus but never
// reached the SSE subscriber because the mapper dropped it.)
func TestMapEventToSSEAllKnownEvents(t *testing.T) {
	cases := []struct {
		name      string
		eventType core.AgentEventType
		payload   core.EventPayload
		wantType  string
	}{
		{
			name:      "text_delta",
			eventType: core.EventTypeTextDelta,
			payload:   core.TextDelta{Text: "hello", AgentName: "cto"},
			wantType:  "text_delta",
		},
		{
			name:      "thinking_delta",
			eventType: core.EventTypeThinkingDelta,
			payload:   core.ThinkingDelta{Text: "hmm", AgentName: "cto"},
			wantType:  "thinking_delta",
		},
		{
			name:      "tool_start",
			eventType: core.EventTypeToolStart,
			payload:   core.ToolStart{ToolName: "bash", ToolID: "t1"},
			wantType:  "tool_execution_start",
		},
		{
			name:      "tool_end",
			eventType: core.EventTypeToolEnd,
			payload:   core.ToolEnd{ToolName: "bash", ToolID: "t1", Result: "ok"},
			wantType:  "tool_execution_end",
		},
		{
			name:      "tool_update",
			eventType: core.EventTypeToolUpdate,
			payload:   core.ToolUpdate{ToolID: "t1", Text: "..."},
			wantType:  "tool_update",
		},
		{
			name:      "agent_start",
			eventType: core.EventTypeAgentStart,
			payload:   core.AgentStartData{},
			wantType:  "agent_start",
		},
		{
			name:      "agent_end",
			eventType: core.EventTypeAgentEnd,
			payload:   core.AgentEndData{Model: "gpt-4"},
			wantType:  "agent_end",
		},
		{
			name:      "error",
			eventType: core.EventTypeError,
			payload:   core.ErrorEventData{Error: "boom"},
			wantType:  "error",
		},
		{
			name:      "artifact_created",
			eventType: core.EventTypeArtifactCreated,
			payload:   core.ArtifactCreatedData{Path: "/x"},
			wantType:  "artifact_created",
		},
		{
			name:      "plan_created",
			eventType: core.EventTypePlanCreated,
			payload:   core.PlanCreatedData{ID: "p1"},
			wantType:  "plan_created",
		},
		{
			name:      "subagent_start",
			eventType: core.EventTypeSubAgentStart,
			payload:   core.SubAgentStartData{AgentName: "alice"},
			wantType:  "subagent_start",
		},
		{
			name:      "subagent_end",
			eventType: core.EventTypeSubAgentEnd,
			payload:   core.SubAgentEndData{AgentName: "alice", Status: "ok"},
			wantType:  "subagent_end",
		},
		{
			name:      "compaction_start",
			eventType: core.EventTypeCompactionStart,
			payload:   core.CompactionStartData{},
			wantType:  "compaction_start",
		},
		{
			name:      "compaction_end",
			eventType: core.EventTypeCompactionEnd,
			payload:   core.CompactionEndData{CompactedMessages: 5},
			wantType:  "compaction_end",
		},
		{
			name:      "context_update",
			eventType: core.EventTypeContextUpdate,
			payload:   core.ContextUpdateData{ContextTokens: 100},
			wantType:  "context_update",
		},
		{
			name:      "step_start",
			eventType: core.EventTypeStepStart,
			payload:   core.StepStartData{Round: 1},
			wantType:  "step_start",
		},
		{
			name:      "step_end",
			eventType: core.EventTypeStepEnd,
			payload:   core.StepEndData{Round: 1},
			wantType:  "step_end",
		},
		{
			name:      "source",
			eventType: core.EventTypeSource,
			payload:   core.SourceEventData{SourceURL: "https://example.com"},
			wantType:  "source",
		},
		{
			name:      "hook_request",
			eventType: core.EventTypeHookRequest,
			payload:   core.HookRequestData{HookID: "h1"},
			wantType:  "hook_request",
		},
		{
			name:      "decision_request",
			eventType: core.EventTypeDecisionRequest,
			payload:   core.DecisionRequestData{ID: "d1"},
			wantType:  "decision_request",
		},
		{
			name:      "mouse_action",
			eventType: core.EventTypeMouseAction,
			payload:   core.MouseActionData{NormX: 0.5, NormY: 0.5, Action: "click"},
			wantType:  "mouse_action",
		},
		{
			name:      "provider_retry",
			eventType: core.EventTypeProviderRetry,
			payload:   core.ProviderRetryData{Attempt: 1, MaxRetry: 5, Error: "timeout"},
			wantType:  "provider_retry",
		},
		// --- PR5/PR6 events: the actual reason this test exists ---
		{
			name:      "safeguard_fallback",
			eventType: core.EventTypeSafeguardFallback,
			payload: core.SafeguardFallbackData{
				PatternID:     "destructive-shell",
				MatchedText:   "rm -rf /",
				OriginalModel: "gpt-4",
				FallbackModel: "gpt-4",
			},
			wantType: "safeguard_fallback",
		},
		{
			name:      "resource_conflict",
			eventType: core.EventTypeResourceConflict,
			payload: core.ResourceConflictData{
				Path:   "/workspace/a.txt",
				AgentA: "alice",
				AgentB: "bob",
			},
			wantType: "resource_conflict",
		},
		{
			name:      "agent_message",
			eventType: core.EventTypeAgentMessage,
			payload: core.AgentMessageData{
				FromAgent: "alice",
				ToAgent:   "bob",
				Content:   "hi",
			},
			wantType: "agent_message",
		},
		{
			name:      "agent_status",
			eventType: core.EventTypeAgentStatus,
			payload: core.AgentStatusData{
				AgentID: "alice",
				State:   "idle",
			},
			wantType: "agent_status",
		},
		{
			name:      "mcp_endpoint_changed",
			eventType: core.EventTypeMCPEndpointChanged,
			payload: core.MCPEndpointChangedData{
				Prefix: "media",
				From:   "http://localhost:8101",
				To:     "http://fallback.example.com/mcp",
				Reason: "primary down",
			},
			wantType: "mcp_endpoint_changed",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := llamaeng.AgentEvent{Type: c.eventType, Data: c.payload}
			got := mapEventToSSE(ev)
			if got == nil {
				t.Fatalf("mapper returned nil for %s — event type not handled "+
					"in pux_sse.go mapEventToSSE (default: return nil trap)",
					c.eventType)
			}
			if got.Type != c.wantType {
				t.Errorf("wire type = %q, want %q", got.Type, c.wantType)
			}
			if got.Data == nil {
				t.Errorf("wire data is nil for %s", c.eventType)
			}
		})
	}
}

// TestMapEventToSSENilPayload asserts the nil-payload guard at the top of
// mapEventToSSE.
func TestMapEventToSSENilPayload(t *testing.T) {
	got := mapEventToSSE(llamaeng.AgentEvent{
		Type: core.EventTypeTextDelta,
		Data: nil,
	})
	if got != nil {
		t.Errorf("nil payload should return nil, got %+v", got)
	}
}

// TestMapEventToSSEPR5Fields asserts the safeguard_fallback payload fields
// survive the mapper intact. This is the SSE contract the frontend banner
// depends on.
func TestMapEventToSSEPR5Fields(t *testing.T) {
	ev := llamaeng.AgentEvent{
		Type: core.EventTypeSafeguardFallback,
		Data: core.SafeguardFallbackData{
			PatternID:     "destructive-shell",
			Description:   "Recursive delete at filesystem root",
			MatchedText:   "rm -rf /",
			OriginalModel: "gpt-4",
			FallbackModel: "gpt-4",
			AgentName:     "cto",
			ToolName:      "bash",
		},
	}
	got := mapEventToSSE(ev)
	if got == nil {
		t.Fatal("mapper returned nil for safeguard_fallback")
	}
	if got.Type != "safeguard_fallback" {
		t.Errorf("Type = %q, want safeguard_fallback", got.Type)
	}
	// Payload passes through as the typed struct; the wire serializer will
	// marshal its JSON tags. Verify the struct round-trips with expected fields.
	data, ok := got.Data.(core.SafeguardFallbackData)
	if !ok {
		t.Fatalf("Data type = %T, want core.SafeguardFallbackData", got.Data)
	}
	if data.PatternID != "destructive-shell" {
		t.Errorf("PatternID = %q", data.PatternID)
	}
	if data.AgentName != "cto" {
		t.Errorf("AgentName = %q", data.AgentName)
	}
}

// TestMapEventToSSEPR6Fields asserts the agent_message + resource_conflict
// payloads round-trip with the fields the frontend panel needs.
func TestMapEventToSSEPR6Fields(t *testing.T) {
	t.Run("agent_message", func(t *testing.T) {
		ev := llamaeng.AgentEvent{
			Type: core.EventTypeAgentMessage,
			Data: core.AgentMessageData{
				FromAgent: "alice",
				ToAgent:   "bob",
				Content:   "found the bug",
			},
		}
		got := mapEventToSSE(ev)
		if got == nil || got.Type != "agent_message" {
			t.Fatalf("got = %+v", got)
		}
		data, ok := got.Data.(core.AgentMessageData)
		if !ok {
			t.Fatalf("Data type = %T", got.Data)
		}
		if data.FromAgent != "alice" || data.ToAgent != "bob" || data.Content != "found the bug" {
			t.Errorf("payload round-trip lost fields: %+v", data)
		}
	})

	t.Run("resource_conflict", func(t *testing.T) {
		ev := llamaeng.AgentEvent{
			Type: core.EventTypeResourceConflict,
			Data: core.ResourceConflictData{
				Path:   "/workspace/main.go",
				AgentA: "alice",
				AgentB: "bob",
			},
		}
		got := mapEventToSSE(ev)
		if got == nil || got.Type != "resource_conflict" {
			t.Fatalf("got = %+v", got)
		}
		data, ok := got.Data.(core.ResourceConflictData)
		if !ok {
			t.Fatalf("Data type = %T", got.Data)
		}
		if data.Path != "/workspace/main.go" || data.AgentA != "alice" || data.AgentB != "bob" {
			t.Errorf("payload round-trip lost fields: %+v", data)
		}
	})
}
