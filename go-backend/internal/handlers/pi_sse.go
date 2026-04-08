package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
)

// sseEvent is a simplified event for the frontend.
type sseEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
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

// mapEventToSSE converts a Pi RPC event to an SSE event for the frontend.
// Pi's RPC protocol sends message_update events with nested assistantMessageEvent
// containing the actual text deltas in its "delta" field.
func (h *PiHandler) mapEventToSSE(event pi.AgentEvent) *sseEvent {
	switch event.Type {
	case "message_update":
		// Pi sends message updates with nested assistantMessageEvent
		if event.AssistantMessageEvent != nil {
			ame := event.AssistantMessageEvent
			switch ame.Type {
			case "text_delta":
				return &sseEvent{
					Type: pi.EventTextDelta,
					Data: map[string]string{"text": ame.Delta},
				}
			case "text_end":
				// Text complete — extract usage from partial if available
				return nil // Frontend accumulates deltas, no action needed
			}
		}
		return nil

	case "message_start":
		// Extract model info from assistant messages
		return nil // Frontend doesn't need message_start

	case "message_end":
		return nil // Frontend handles via text_delta accumulation

	case pi.RpcEventToolStart:
		return &sseEvent{
			Type: pi.EventToolStart,
			Data: map[string]interface{}{
				"toolName": event.Data.ToolName,
				"args":     event.Data.ToolArgs,
				"toolId":   event.Data.ToolId,
			},
		}
	case pi.RpcEventToolEnd:
		return &sseEvent{
			Type: pi.EventToolEnd,
			Data: map[string]interface{}{
				"toolName": event.Data.ToolName,
				"toolId":   event.Data.ToolId,
				"result":   event.Data.Result,
				"error":    event.Data.Error,
			},
		}
	case pi.RpcEventAgentStart:
		return &sseEvent{
			Type: pi.EventAgentStart,
			Data: map[string]interface{}{},
		}
	case pi.RpcEventAgentEnd:
		// Extract usage from the messages field
		data := map[string]interface{}{}
		if len(event.Messages) > 0 {
			// Parse the last assistant message for usage
			var msgs []struct {
				Role string `json:"role"`
				Usage struct {
					Input     float64 `json:"input"`
					Output    float64 `json:"output"`
					CacheRead float64 `json:"cacheRead"`
				} `json:"usage"`
				API      string `json:"api"`
				Provider string `json:"provider"`
				Model    string `json:"model"`
			}
			if json.Unmarshal(event.Messages, &msgs) == nil {
				for i := len(msgs) - 1; i >= 0; i-- {
					if msgs[i].Role == "assistant" {
						data["input"] = msgs[i].Usage.Input
						data["output"] = msgs[i].Usage.Output
						data["cache"] = msgs[i].Usage.CacheRead
						data["model"] = msgs[i].Provider + "/" + msgs[i].Model
						break
					}
				}
			}
		}
		return &sseEvent{
			Type: pi.EventAgentEnd,
			Data: data,
		}
	case pi.RpcEventCompactionStart:
		return &sseEvent{
			Type: pi.EventCompactionStart,
			Data: map[string]interface{}{},
		}
	case pi.RpcEventCompactionEnd:
		return &sseEvent{
			Type: pi.EventCompactionEnd,
			Data: map[string]interface{}{
				"compactedMessages": event.Data.CompactedMessages,
				"keptMessages":      event.Data.KeptMessages,
			},
		}
	case pi.RpcEventError:
		return &sseEvent{
			Type: pi.EventError,
			Data: map[string]string{"error": event.Data.Error},
		}
	case pi.RpcEventResponse:
		return &sseEvent{
			Type: pi.EventStateUpdate,
			Data: map[string]interface{}{
				"model":  event.Data.Model,
				"input":  event.Data.Input,
				"output": event.Data.Output,
				"cache":  event.Data.Cache,
			},
		}
	default:
		return nil
	}
}
