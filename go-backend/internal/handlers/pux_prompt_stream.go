package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/agents/orchestrator"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	llama "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
	"github.com/auto-developer-orchestrator/backend/internal/tools/plan"
	"go.uber.org/zap"
)

// rehydrateAndStream handles session rehydration, SSE streaming, and result persistence.
// Called after the orchestrator is fully configured and ready.
func (h *PuxHandler) rehydrateAndStream(
	w http.ResponseWriter,
	r *http.Request,
	orch *orchestrator.Agent,
	events chan core.AgentEvent,
	req promptRequest,
	projectPath string,
	memFolder *memory.FolderStore,
) {
	// Register agent as running
	h.registry.Start(req.Project, req.AgentId)
	defer h.registry.Stop(req.Project, req.AgentId)

	// Rehydrate session tree from SQL history
	var lastUserContent string
	if h.db != nil {
		history, err := h.db.GetConversationHistory(r.Context(), req.Project, req.AgentId, 200)
		if err != nil {
			h.log.Warn("Failed to load conversation history", zap.Error(err))
		} else {
			deduped := 0
			for _, stored := range history {
				msg := core.Message{Role: stored.Role}
				switch stored.Role {
				case "user":
					msg.Content = stored.Content
					if stored.Content == lastUserContent {
						deduped++
						continue
					}
					lastUserContent = stored.Content
				case "assistant":
					msg.Content = stored.Text
					msg.ReasoningContent = stored.Thinking
					if stored.ToolCalls != "" && stored.ToolCalls != "[]" && stored.ToolCalls != "[streaming]" {
						var tcs []core.ToolCallResponse
						if err := json.Unmarshal([]byte(stored.ToolCalls), &tcs); err == nil {
							msg.ToolCalls = tcs
						}
					}
					if stored.Text == "" && stored.Thinking == "" && (stored.ToolCalls == "" || stored.ToolCalls == "[]" || stored.ToolCalls == "[streaming]") {
						continue
					}
					lastUserContent = ""
				case "tool":
					msg.Content = stored.Content
					msg.ToolCallID = stored.ToolCallID
					msg.Name = stored.ToolName
					lastUserContent = ""
				default:
					continue
				}
				if err := orch.Session.AppendMessage(msg); err != nil {
					h.log.Warn("Failed to append history message", zap.Error(err))
				}
			}
			if deduped > 0 {
				h.log.Info("Deduped history messages",
					zap.String("agentId", req.AgentId),
					zap.Int("removed", deduped))
			}
		}
	}

	// Auto-title from first user message
	if h.db != nil && lastUserContent == "" {
		title := req.Message
		if len(title) > 60 {
			title = title[:60] + "..."
		}
		if err := h.db.SetConversationTitle(r.Context(), req.Project, req.AgentId, title); err != nil {
			h.log.Warn("Failed to set auto-title", zap.Error(err))
		}
	}

	// Inject project memory prefix
	var memoryPrefix string
	if memFolder != nil {
		memoryPrefix = memFolder.InjectPrefix()
	}
	if memoryPrefix == "" {
		if mem := orch.Memory; mem != nil {
			memoryPrefix = mem.InjectPrefix()
		} else {
			memoryPrefix = memory.ReadMemoryFile(projectPath)
			if memoryPrefix != "" {
				memoryPrefix = "<memory>\n" + memoryPrefix + "\n</memory>\n\n"
			}
		}
	}

	// Read AGENTS.md
	var agentsMDPrefix string
	if agentsContent := readAgentsMD(projectPath); agentsContent != "" {
		agentsMDPrefix = "<agents-md>\n" + agentsContent + "\n</agents-md>\n\n"
	}

	// Set up SSE
	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)
	writeSSE(w, string(core.EventTypeAgentSpawned), map[string]string{"agentId": req.AgentId}, canFlush, flusher)

	// Save user message
	if h.db != nil {
		if lastUserContent != req.Message {
			if _, err := h.db.SaveUserMessage(r.Context(), req.Project, req.AgentId, req.Message); err != nil {
				h.log.Warn("Failed to save user message", zap.Error(err))
			}
		} else {
			h.log.Info("Skipping duplicate user message save",
				zap.String("agentId", req.AgentId))
		}
		if err := h.db.SetConversationStatus(r.Context(), req.Project, req.AgentId, "processing"); err != nil {
			h.log.Warn("Failed to set conversation status to processing", zap.Error(err))
		}
	}

	// Build final message
	planPrefix := plan.InjectActivePlan(projectPath)
	orchMsg := agentsMDPrefix + memoryPrefix + planPrefix + "User request: " + req.Message

	var images []core.ContentImage
	for _, dataURL := range req.Images {
		images = append(images, core.ContentImage{DataURL: dataURL})
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	done := make(chan struct{})
	var loopErr error

	go func() {
		defer close(done)
		defer close(events)
		if len(images) > 0 {
			loopErr = orch.RunWithImages(ctx, orchMsg, images, events)
		} else {
			loopErr = orch.Run(ctx, orchMsg, events)
		}
	}()

	// Stream events
	accum := NewStreamAccumulator()
	streamer := NewEventStreamer(w)

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	saveTicker := time.NewTicker(5 * time.Second)
	defer saveTicker.Stop()

	savePartialResults := func() {
		if h.db == nil {
			return
		}
		assistantText := accum.Text()
		assistantThinking := accum.Thinking()
		toolCallsJSON := accum.ToolCallsJSON()
		if err := h.db.FinalizeStreamingMessage(r.Context(), req.Project, req.AgentId, assistantText, assistantThinking, toolCallsJSON); err != nil {
			h.log.Warn("Failed to finalize streaming message, falling back to insert", zap.Error(err))
			if _, fallbackErr := h.db.SaveAssistantMessage(r.Context(), req.Project, req.AgentId, assistantText, assistantThinking, toolCallsJSON); fallbackErr != nil {
				h.log.Warn("Failed to save assistant message", zap.Error(fallbackErr))
			}
		}
		for _, tr := range accum.ToolResults() {
			if _, err := h.db.SaveToolResult(r.Context(), req.Project, req.AgentId, tr.ToolCallID, tr.ToolName, tr.Content); err != nil {
				h.log.Warn("Failed to save tool result", zap.String("tool", tr.ToolName), zap.Error(err))
			}
		}
		if err := h.db.SetConversationStatus(r.Context(), req.Project, req.AgentId, "unread"); err != nil {
			h.log.Warn("Failed to set conversation status to unread", zap.Error(err))
		}
	}

	llamaEvents := make(chan llama.AgentEvent, 256)
	go func() {
		defer close(llamaEvents)
		for evt := range events {
			accum.ProcessEvent(evt)
			llamaEvents <- convertCoreEventToLlama(evt)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			h.log.Info("Client disconnected, saving partial results",
				zap.String("agentId", req.AgentId),
				zap.String("project", req.Project))
			go func() {
				for range llamaEvents {
				}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				h.log.Warn("Orchestrator didn't finish after client disconnect, saving partial",
					zap.String("agentId", req.AgentId))
			}
			savePartialResults()
			return

		case <-done:
			for evt := range llamaEvents {
				streamer.WriteEvent(evt)
			}
			savePartialResults()
			if loopErr != nil {
				h.log.Error("Orchestrator error", zap.Error(loopErr))
			}
			streamer.WriteDone()
			return
		case <-keepalive.C:
			streamer.WriteKeepalive()
		case evt, ok := <-llamaEvents:
			if !ok {
				streamer.WriteDone()
				return
			}
			keepalive.Reset(15 * time.Second)
			streamer.WriteEvent(evt)
			h.registry.Bump(req.Project, req.AgentId)
		case <-saveTicker.C:
			if h.db != nil && (accum.Text() != "" || accum.Thinking() != "") {
				if err := h.db.SaveStreamingMessage(r.Context(), req.Project, req.AgentId, accum.Text(), accum.Thinking()); err != nil {
					h.log.Warn("Failed to save streaming message", zap.Error(err))
				}
			}
		}
	}
}
