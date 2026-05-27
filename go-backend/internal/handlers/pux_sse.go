package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
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
// Sensitive data (API keys, tokens, passwords) is scrubbed from text-bearing events.
func writeSSE(w http.ResponseWriter, eventType string, data interface{}, canFlush bool, flusher http.Flusher) {
	// Scrub secrets from events that carry user-visible text
	if sensitive.ShouldScrubEvent(eventType) {
		if m, ok := data.(map[string]any); ok {
			data = sensitive.ScrubMap(m)
		}
	}

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
// Uses type assertions on EventPayload to extract typed data.
func mapEventToSSE(event llamaeng.AgentEvent) *sseEvent {
	// Extract the core payload — event.Data is a core.EventPayload
	payload := event.Data

	switch event.Type {
	case llamaeng.EventTypeTextDelta:
		p := payload.(llamaeng.TextDelta)
		data := map[string]interface{}{"text": p.Text}
		if p.AgentName != "" {
			data["agentName"] = p.AgentName
		}
		return &sseEvent{Type: "text_delta", Data: data}

	case llamaeng.EventTypeThinkingDelta:
		p := payload.(llamaeng.ThinkingDelta)
		data := map[string]interface{}{"text": p.Text}
		if p.AgentName != "" {
			data["agentName"] = p.AgentName
		}
		return &sseEvent{Type: "thinking_delta", Data: data}

	case llamaeng.EventTypeToolStart:
		p := payload.(llamaeng.ToolStart)
		data := map[string]interface{}{
			"toolName": p.ToolName,
			"args":     p.ToolArgs,
			"toolId":   p.ToolID,
		}
		if p.AgentName != "" {
			data["agentName"] = p.AgentName
		}
		return &sseEvent{Type: "tool_execution_start", Data: data}

	case llamaeng.EventTypeToolEnd:
		p := payload.(llamaeng.ToolEnd)
		data := map[string]interface{}{
			"toolName": p.ToolName,
			"toolId":   p.ToolID,
			"result":   p.Result,
			"error":    p.Error,
		}
		if p.AgentName != "" {
			data["agentName"] = p.AgentName
		}
		if p.Artifact != nil {
			data["artifact"] = p.Artifact
		}
		if p.ModelContent != "" {
			data["modelContent"] = p.ModelContent
		}
		return &sseEvent{Type: "tool_execution_end", Data: data}

	case llamaeng.EventTypeToolUpdate:
		p := payload.(llamaeng.ToolUpdate)
		data := map[string]interface{}{
			"toolName": p.ToolName,
			"toolId":   p.ToolID,
			"text":     p.Text,
		}
		return &sseEvent{Type: "tool_update", Data: data}

	case llamaeng.EventTypeAgentStart:
		return &sseEvent{Type: "agent_start", Data: map[string]interface{}{}}

	case llamaeng.EventTypeAgentEnd:
		p := payload.(llamaeng.AgentEndData)
		data := map[string]interface{}{
			"input":  p.Input,
			"output": p.Output,
			"cache":  p.Cache,
			"model":  p.Model,
		}
		if p.ContextWindow > 0 {
			data["contextWindow"] = p.ContextWindow
		}
		return &sseEvent{Type: "agent_end", Data: data}

	case llamaeng.EventTypeCompactionStart:
		return &sseEvent{Type: "compaction_start", Data: map[string]interface{}{}}

	case llamaeng.EventTypeCompactionEnd:
		p := payload.(llamaeng.CompactionEndData)
		data := map[string]interface{}{
			"compactedMessages": p.CompactedMessages,
			"keptMessages":      p.KeptMessages,
			"contextTokens":     p.ContextTokens,
			"contextSize":       p.ContextSize,
			"contextUtil":       p.ContextUtil,
		}
		return &sseEvent{Type: "compaction_end", Data: data}

	case llamaeng.EventTypeError:
		p := payload.(llamaeng.ErrorEventData)
		return &sseEvent{Type: "error", Data: map[string]string{"error": p.Error}}

	case llamaeng.EventTypeArtifactCreated, llamaeng.EventTypeArtifactUpdated:
		// Artifact events pass through payload as-is
		return &sseEvent{Type: string(event.Type), Data: payload}

	case llamaeng.EventTypePlanCreated, llamaeng.EventTypePlanUpdated:
		// Plan events pass through payload as-is
		return &sseEvent{Type: string(event.Type), Data: payload}

	case llamaeng.EventTypeDecisionRequest:
		return &sseEvent{Type: "decision_request", Data: payload}

	case llamaeng.EventTypeSubAgentStart:
		p := payload.(llamaeng.SubAgentStartData)
		data := map[string]interface{}{
			"agentName": p.AgentName,
			"task":      p.Task,
		}
		if p.TranscriptID != "" {
			data["agentId"] = p.TranscriptID
		}
		return &sseEvent{Type: "subagent_start", Data: data}

	case llamaeng.EventTypeSubAgentEnd:
		p := payload.(llamaeng.SubAgentEndData)
		data := map[string]interface{}{
			"agentName": p.AgentName,
			"status":    p.Status,
			"task":      p.Task,
		}
		if p.Result != "" {
			data["result"] = p.Result
		}
		if p.Error != "" {
			data["error"] = p.Error
		}
		if p.TranscriptID != "" {
			data["agentId"] = p.TranscriptID
		}
		return &sseEvent{Type: "subagent_end", Data: data}

	case llamaeng.EventTypeSource:
		p := payload.(llamaeng.SourceEventData)
		data := map[string]interface{}{
			"sourceType": p.SourceType,
			"id":         p.SourceID,
		}
		if p.SourceURL != "" {
			data["url"] = p.SourceURL
		}
		return &sseEvent{Type: "source", Data: data}

	case llamaeng.EventTypeHookRequest:
		p := payload.(llamaeng.HookRequestData)
		data := map[string]interface{}{
			"hookId":    p.HookID,
			"hookPoint": p.HookPoint,
			"message":   p.Message,
		}
		return &sseEvent{Type: "hook_request", Data: data}

	case llamaeng.EventTypeStepStart:
		p := payload.(llamaeng.StepStartData)
		return &sseEvent{Type: "step_start", Data: map[string]interface{}{"round": p.Round}}

	case llamaeng.EventTypeStepEnd:
		p := payload.(llamaeng.StepEndData)
		return &sseEvent{Type: "step_end", Data: map[string]interface{}{"round": p.Round, "decision": p.Decision}}

	default:
		return nil
	}
}
