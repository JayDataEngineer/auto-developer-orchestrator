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
		data := map[string]interface{}{"text": event.Data.Text}
		if event.Data.AgentName != "" {
			data["agentName"] = event.Data.AgentName
		}
		return &sseEvent{
			Type: "text_delta",
			Data: data,
		}

	case llamaeng.EventTypeThinkingDelta:
		data := map[string]interface{}{"text": event.Data.Text}
		if event.Data.AgentName != "" {
			data["agentName"] = event.Data.AgentName
		}
		return &sseEvent{
			Type: "thinking_delta",
			Data: data,
		}

	case llamaeng.EventTypeToolStart:
		data := map[string]interface{}{
			"toolName": event.Data.ToolName,
			"args":     event.Data.ToolArgs,
			"toolId":   event.Data.ToolID,
		}
		if event.Data.AgentName != "" {
			data["agentName"] = event.Data.AgentName
		}
		return &sseEvent{
			Type: "tool_execution_start",
			Data: data,
		}

	case llamaeng.EventTypeToolEnd:
		data := map[string]interface{}{
			"toolName": event.Data.ToolName,
			"toolId":   event.Data.ToolID,
			"result":   event.Data.Result,
			"error":    event.Data.Error,
		}
		if event.Data.Artifact != nil {
			data["artifact"] = event.Data.Artifact
		}
		if event.Data.ModelContent != "" {
			data["modelContent"] = event.Data.ModelContent
		}
		if event.Data.AgentName != "" {
			data["agentName"] = event.Data.AgentName
		}
		return &sseEvent{
			Type: "tool_execution_end",
			Data: data,
		}

	case llamaeng.EventTypeToolUpdate:
		data := map[string]interface{}{
			"toolName": event.Data.ToolName,
			"toolId":   event.Data.ToolID,
			"text":     event.Data.Text,
		}
		if event.Data.AgentName != "" {
			data["agentName"] = event.Data.AgentName
		}
		return &sseEvent{
			Type: "tool_update",
			Data: data,
		}

	case llamaeng.EventTypeAgentStart:
		return &sseEvent{
			Type: "agent_start",
			Data: map[string]interface{}{},
		}

	case llamaeng.EventTypeAgentEnd:
		data := map[string]interface{}{
			"input":  event.Data.Input,
			"output": event.Data.Output,
			"cache":  event.Data.Cache,
			"model":  event.Data.Model,
		}
		if event.Data.ContextWindow > 0 {
			data["contextWindow"] = event.Data.ContextWindow
		}
		return &sseEvent{
			Type: "agent_end",
			Data: data,
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
				"contextTokens":     event.Data.ContextTokens,
				"contextSize":       event.Data.ContextSize,
				"contextUtil":       event.Data.ContextUtil,
				"compactionType":    event.Data.CompactionType,
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

	case llamaeng.EventTypeDecisionRequest:
		return &sseEvent{
			Type: "decision_request",
			Data: map[string]interface{}{
				"decisionId":   event.Data.ToolArgs["decisionId"],
				"sourceTool":   event.Data.ToolArgs["sourceTool"],
				"title":        event.Data.ToolArgs["title"],
				"description":  event.Data.ToolArgs["description"],
				"hint":         event.Data.ToolArgs["hint"],
				"options":      event.Data.ToolArgs["options"],
				"allowFreeText": event.Data.ToolArgs["allowFreeText"],
				"metadata":     event.Data.ToolArgs["metadata"],
			},
		}

	case llamaeng.EventTypePlanCreated:
		return &sseEvent{
			Type: "plan_created",
			Data: map[string]interface{}{
				"planId":   event.Data.ToolArgs["planId"],
				"name":     event.Data.ToolArgs["name"],
				"content":  event.Data.ToolArgs["content"],
				"filePath": event.Data.ToolArgs["filePath"],
			},
		}

	case llamaeng.EventTypePlanUpdated:
		return &sseEvent{Type: "plan_updated", Data: event.Data}

	case llamaeng.EventTypeSubAgentStart:
		data := map[string]interface{}{
			"agentName": event.Data.AgentName,
			"task":      event.Data.Task,
			"toolName":  event.Data.ToolName,
		}
		if event.Data.TranscriptID != "" {
			data["agentId"] = event.Data.TranscriptID
		}
		return &sseEvent{
			Type: "subagent_start",
			Data: data,
		}

	case llamaeng.EventTypeSubAgentEnd:
		data := map[string]interface{}{
			"agentName": event.Data.AgentName,
			"status":    event.Data.Status,
			"task":      event.Data.Task,
		}
		if event.Data.Text != "" {
			data["result"] = event.Data.Text
		}
		if event.Data.Error != "" {
			data["error"] = event.Data.Error
		}
		if event.Data.TranscriptID != "" {
			data["agentId"] = event.Data.TranscriptID
		}
		return &sseEvent{
			Type: "subagent_end",
			Data: data,
		}

	case llamaeng.EventTypeSource:
		data := map[string]interface{}{
			"sourceType": event.Data.SourceType,
			"id":         event.Data.SourceID,
		}
		if event.Data.SourceURL != "" {
			data["url"] = event.Data.SourceURL
		}
		if event.Data.Text != "" {
			data["title"] = event.Data.Text
		}
		return &sseEvent{
			Type: "source",
			Data: data,
		}

	case llamaeng.EventTypeHookRequest:
		return &sseEvent{
			Type: "hook_request",
			Data: map[string]interface{}{
				"hookId":    event.Data.HookID,
				"hookPoint": event.Data.HookPoint,
				"toolName":  event.Data.ToolName,
				"args":      event.Data.ToolArgs,
				"result":    event.Data.Result,
			},
		}

	case llamaeng.EventTypeStepStart:
		return &sseEvent{
			Type: "step_start",
			Data: map[string]interface{}{
				"round": event.Data.Round,
			},
		}

	case llamaeng.EventTypeStepEnd:
		return &sseEvent{
			Type: "step_end",
			Data: map[string]interface{}{
				"round":    event.Data.Round,
				"decision": event.Data.Decision,
			},
		}

	default:
		return nil
	}
}
