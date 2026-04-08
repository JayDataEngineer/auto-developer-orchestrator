package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// PiHandler handles Pi agent HTTP endpoints.
type PiHandler struct {
	pool       *pi.PiPool
	db         *storage.Database
	git        *git.GitOps
	github     *GitHubHandler
	log        *zap.Logger
	litellmURL string
	litellmKey string
}

// NewPiHandler creates a new Pi handler.
func NewPiHandler(pool *pi.PiPool, db *storage.Database, gitOps *git.GitOps, gh *GitHubHandler, logger *zap.Logger) *PiHandler {
	return &PiHandler{
		pool:       pool,
		db:         db,
		git:        gitOps,
		github:     gh,
		log:        logger,
		litellmURL: os.Getenv("LITELLM_PROXY_URL"),
		litellmKey: os.Getenv("LITELLM_MASTER_KEY"),
	}
}

// RegisterRoutes registers all Pi routes on the given router.
func (h *PiHandler) RegisterRoutes(r chi.Router) {
	r.Post("/prompt", h.Prompt)
	r.Post("/abort", h.Abort)
	r.Get("/state", h.GetState)
	r.Get("/messages", h.GetMessages)
	r.Get("/models", h.GetModels)
	r.Put("/model", h.SetModel)
	r.Post("/compact", h.Compact)
	r.Get("/sessions", h.ListSessions)
	r.Put("/session", h.SwitchSession)
	r.Get("/active", h.ListActive)
	r.Post("/agent/spawn", h.SpawnAgent)
	r.Post("/agent/destroy", h.DestroyAgent)
	r.Get("/history", h.GetHistory)
	r.Get("/debug/rpc-test", h.DebugRpcTest)
}

// resolveAgent reads ?agentId= from the query string, defaulting to "default".
func resolveAgent(r *http.Request) string {
	aid := r.URL.Query().Get("agentId")
	if aid == "" {
		return "default"
	}
	return aid
}

// promptRequest is the request body for the prompt endpoint.
type promptRequest struct {
	Message       string `json:"message"`
	Project       string `json:"project"`
	AgentId       string `json:"agentId,omitempty"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	AutoBranch    bool   `json:"autoBranch,omitempty"`
	AutoMerge     bool   `json:"autoMerge,omitempty"`
}

// Prompt sends a coding task to Pi and streams events back via SSE.
func (h *PiHandler) Prompt(w http.ResponseWriter, r *http.Request) {
	var req promptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Message is required",
		})
		return
	}

	// Auto-generate agentId if not provided
	if req.AgentId == "" {
		req.AgentId = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
	}

	projectPath := resolveProjectPath(req.Project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client, err := h.pool.GetOrCreateWithID(projectPath, req.AgentId)
	if err != nil {
		h.log.Error("Failed to get Pi client", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": fmt.Sprintf("Failed to start Pi agent: %v", err),
		})
		return
	}

	// Auto-branch if requested
	var autoBranchName string
	if req.AutoBranch && h.git != nil {
		autoBranchName = fmt.Sprintf("pi/task-%d", time.Now().Unix())
		if err := h.git.Checkout(r.Context(), git.CheckoutOptions{
			Dir:       projectPath,
			Branch:    autoBranchName,
			CreateNew: true,
		}); err != nil {
			h.log.Warn("Auto-branch failed (non-fatal)", zap.Error(err), zap.String("branch", autoBranchName))
			autoBranchName = ""
		}
	}

	// Set up SSE
	setSSEHeaders(w)

	flusher, canFlush := w.(http.Flusher)

	// Send agent_spawned event
	spawnData, _ := json.Marshal(map[string]string{"agentId": req.AgentId})
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", pi.EventAgentSpawned, string(spawnData))
	if canFlush {
		flusher.Flush()
	}

	// Send branch_created event if auto-branched
	if autoBranchName != "" {
		data, _ := json.Marshal(map[string]string{"branch": autoBranchName})
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", pi.EventBranchCreated, string(data))
		if canFlush {
			flusher.Flush()
		}
	}

	// Subscribe to events
	subId := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	events := client.Subscribe(subId)
	defer client.Unsubscribe(subId)

	// Save user message to DB
	if h.db != nil {
		if _, err := h.db.SaveUserMessage(r.Context(), req.Project, req.AgentId, req.Message); err != nil {
			h.log.Warn("Failed to save user message", zap.Error(err))
		}
	}

	// Send prompt command
	if err := client.SendPrompt(req.Message, req.Model, req.ThinkingLevel); err != nil {
		h.log.Error("Failed to send prompt to Pi", zap.Error(err))
		JSONError(w, "Failed to send prompt", http.StatusInternalServerError)
		return
	}

	// Stream events to SSE, accumulating assistant response for DB persistence
	ctx := r.Context()
	var assistantText, assistantThinking string
	var assistantToolCalls []json.RawMessage

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			h.log.Info("Pi event received", zap.String("type", event.Type))
			sseEvent := h.mapEventToSSE(event)
			if sseEvent == nil {
				continue
			}
			data, err := json.Marshal(sseEvent.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEvent.Type, string(data))
			if canFlush {
				flusher.Flush()
			}

			// Accumulate assistant response for persistence
			switch sseEvent.Type {
			case "text_delta":
				switch td := sseEvent.Data.(type) {
				case map[string]string:
					assistantText += td["text"]
				case map[string]interface{}:
					if t, ok := td["text"].(string); ok {
						assistantText += t
					}
				}
			case "thinking_delta":
				switch td := sseEvent.Data.(type) {
				case map[string]string:
					assistantThinking += td["text"]
				case map[string]interface{}:
					if t, ok := td["text"].(string); ok {
						assistantThinking += t
					}
				}
			case "tool_execution_start":
				if raw, err := json.Marshal(sseEvent.Data); err == nil {
					assistantToolCalls = append(assistantToolCalls, raw)
				}
			}

			// After agent_end, save assistant message and run post-completion
			if sseEvent.Type == pi.EventAgentEnd {
				if h.db != nil {
					toolCallsJSON := "[]"
					if len(assistantToolCalls) > 0 {
						if raw, err := json.Marshal(assistantToolCalls); err == nil {
							toolCallsJSON = string(raw)
						}
					}
					if _, err := h.db.SaveAssistantMessage(ctx, req.Project, req.AgentId, assistantText, assistantThinking, toolCallsJSON); err != nil {
						h.log.Warn("Failed to save assistant message", zap.Error(err))
					}
				}

				if autoBranchName != "" {
					h.postCompletion(ctx, projectPath, autoBranchName, req.Message, req.AutoMerge, w, canFlush, flusher)
				}
				return
			}
		}
	}
}
