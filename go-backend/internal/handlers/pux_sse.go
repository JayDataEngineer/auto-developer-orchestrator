package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
)

// sseEvent is a simplified event for the frontend.
type sseEvent struct {
	Type string
	Data interface{}
}

// toolIdCounter generates unique IDs for tool calls when the agent doesn't provide one.
var toolIdCounter int64

func nextToolFallbackId() string {
	n := atomic.AddInt64(&toolIdCounter, 1)
	return fmt.Sprintf("tool-%d", n)
}

// writeSSE sends a single SSE event to the response writer.
func writeSSE(w http.ResponseWriter, eventType string, data interface{}, canFlush bool, flusher http.Flusher) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, string(jsonData))
	if canFlush {
		flusher.Flush()
	}
}

// mapEventToSSE converts a llama agent event to an SSE event for the frontend.
// Consumes llamaeng.AgentEvent directly — no intermediate type conversion needed.
func (h *PuxHandler) mapEventToSSE(event llamaeng.AgentEvent) *sseEvent {
	switch event.Type {
	case llamaeng.EventTypeTextDelta:
		return &sseEvent{
			Type: "text_delta",
			Data: map[string]string{"text": event.Data.Text},
		}

	case llamaeng.EventTypeThinkingDelta:
		return &sseEvent{
			Type: "thinking_delta",
			Data: map[string]string{"text": event.Data.Text},
		}

	case llamaeng.EventTypeToolStart:
		return &sseEvent{
			Type: "tool_execution_start",
			Data: map[string]interface{}{
				"toolName": event.Data.ToolName,
				"args":     event.Data.ToolArgs,
				"toolId":   event.Data.ToolID,
			},
		}

	case llamaeng.EventTypeToolEnd:
		return &sseEvent{
			Type: "tool_execution_end",
			Data: map[string]interface{}{
				"toolName": event.Data.ToolName,
				"toolId":   event.Data.ToolID,
				"result":   event.Data.Result,
				"error":    event.Data.Error,
			},
		}

	case llamaeng.EventTypeToolUpdate:
		return &sseEvent{
			Type: "tool_update",
			Data: map[string]interface{}{
				"toolName": event.Data.ToolName,
				"toolId":   event.Data.ToolID,
				"text":     event.Data.Text,
			},
		}

	case llamaeng.EventTypeAgentStart:
		return &sseEvent{
			Type: "agent_start",
			Data: map[string]interface{}{},
		}

	case llamaeng.EventTypeAgentEnd:
		return &sseEvent{
			Type: "agent_end",
			Data: map[string]interface{}{
				"input":  event.Data.Input,
				"output": event.Data.Output,
				"cache":  event.Data.Cache,
				"model":  event.Data.Model,
			},
		}

	case llamaeng.EventTypeCompactionStart:
		return &sseEvent{
			Type: "compaction_start",
			Data: map[string]interface{}{},
		}

	case llamaeng.EventTypeCompactionEnd:
		return &sseEvent{
			Type: "compaction_end",
			Data: map[string]interface{}{
				"compactedMessages": event.Data.CompactedMessages,
				"keptMessages":      event.Data.KeptMessages,
			},
		}

	case llamaeng.EventTypeError:
		return &sseEvent{
			Type: "error",
			Data: map[string]string{"error": event.Data.Error},
		}

	case llamaeng.EventTypeArtifactCreated:
		return &sseEvent{Type: "artifact_created", Data: event.Data}

	case llamaeng.EventTypeArtifactUpdated:
		return &sseEvent{Type: "artifact_updated", Data: event.Data}

	case llamaeng.EventTypePlanCreated:
		return &sseEvent{Type: "plan_created", Data: event.Data}

	case llamaeng.EventTypePlanUpdated:
		return &sseEvent{Type: "plan_updated", Data: event.Data}

	case llamaeng.EventTypeSubAgentStart:
		return &sseEvent{Type: "subagent_start", Data: event.Data}

	case llamaeng.EventTypeSubAgentEnd:
		return &sseEvent{Type: "subagent_end", Data: event.Data}

	case llamaeng.EventTypeApprovalRequest:
		result, _ := event.Data.Result.(map[string]interface{})
		if result != nil {
			return &sseEvent{Type: "approval_request", Data: result}
		}
		return nil

	default:
		return nil
	}
}
