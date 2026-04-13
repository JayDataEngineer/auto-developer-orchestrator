package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
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
	r.Post("/respond", h.Respond)
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
	r.Delete("/conversation", h.DeleteConversation)
	r.Put("/conversation/rename", h.RenameConversation)
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

// respondRequest is the request body for the approval response endpoint.
type respondRequest struct {
	Project   string `json:"project"`
	AgentId   string `json:"agentId"`
	RequestID string `json:"requestId"`
	Action    string `json:"action"` // "approve", "deny", "answer"
	Message   string `json:"message,omitempty"`
}

// Respond handles user approval/denial responses for pending agent approvals.
func (h *PiHandler) Respond(w http.ResponseWriter, r *http.Request) {
	var req respondRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	if req.RequestID == "" || req.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "requestId and action are required",
		})
		return
	}

	projectPath := resolveProjectPath(req.Project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, req.AgentId)
	if client == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Agent not found",
		})
		return
	}

	resp := pi.ApprovalResponse{
		Action:  req.Action,
		Message: req.Message,
	}

	if ok := client.ResolveApproval(req.RequestID, resp); !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "No pending approval found for this request",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
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
	var approvalTriggered bool
	var lastToolStartID string // Track tool start ID for matching with end events

	// Keepalive ticker — sends a comment every 15s to prevent client/proxy timeouts
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			// SSE comment as keepalive (clients ignore lines starting with ":")
			fmt.Fprintf(w, ": keepalive\n\n")
			if canFlush {
				flusher.Flush()
			}
		case event, ok := <-events:
			if !ok {
				return
			}
			// Reset keepalive timer on every real event
			keepalive.Reset(15 * time.Second)
			h.log.Info("Pi event received", zap.String("type", event.Type))
			sseEvent := h.mapEventToSSE(event)
			if sseEvent == nil {
				continue
			}

			// Track tool start IDs so end events can be matched when Pi doesn't provide IDs
			if sseEvent.Type == pi.EventToolStart {
				if dataMap, ok := sseEvent.Data.(map[string]interface{}); ok {
					tid, _ := dataMap["toolId"].(string)
					if tid == "" {
						tid = nextToolFallbackId()
						dataMap["toolId"] = tid
					}
					lastToolStartID = tid
				}
			}
			if sseEvent.Type == pi.EventToolEnd {
				if dataMap, ok := sseEvent.Data.(map[string]interface{}); ok {
					tid, _ := dataMap["toolId"].(string)
					if tid == "" && lastToolStartID != "" {
						dataMap["toolId"] = lastToolStartID
					}
				}
			}

			// Intercept risky bash commands for approval before forwarding to SSE
			if sseEvent.Type == pi.EventToolStart && !approvalTriggered {
				if dataMap, ok := sseEvent.Data.(map[string]interface{}); ok {
					if toolName, _ := dataMap["toolName"].(string); toolName == "bash" {
						if args, ok := dataMap["args"].(map[string]interface{}); ok {
							if cmd, _ := args["command"].(string); cmd != "" {
								if risky, reason := pi.IsRiskyBashCommand(cmd); risky {
									requestID := fmt.Sprintf("req-%d", time.Now().UnixMilli())
									approvalTriggered = true

									writeSSE(w, pi.EventApprovalRequest, pi.ApprovalRequestData{
										RequestID: requestID,
										Type:      "tool_confirm",
										ToolName:  "bash",
										ToolArgs:  map[string]interface{}{"command": cmd},
										Message:   reason + ": " + cmd,
										Risk:      "high",
									}, canFlush, flusher)

									ch := client.RegisterApproval(requestID)
									select {
									case resp, ok := <-ch:
										if !ok {
											return
										}
										if resp.Action == "approve" {
											client.Steer("APPROVED: " + resp.Message)
										} else {
											client.Steer("DENIED: do NOT execute: " + cmd + ". Reason: " + resp.Message)
											continue // skip forwarding the tool event
										}
									case <-ctx.Done():
										return
									case <-time.After(5 * time.Minute):
										client.Steer("APPROVAL_TIMEOUT: do NOT execute: " + cmd)
										continue
									}
								}
							}
						}
					}
				}
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

			// Check for approval/question markers in accumulated text
			if !approvalTriggered {
				if idx := strings.Index(assistantText, "??APPROVAL:"); idx >= 0 {
					approvalTriggered = true
					msg := strings.TrimSpace(assistantText[idx+len("??APPROVAL:"):])
					if msg == "" {
						msg = "Plan approval requested"
					}
					requestID := fmt.Sprintf("req-%d", time.Now().UnixMilli())

					writeSSE(w, pi.EventApprovalRequest, pi.ApprovalRequestData{
						RequestID: requestID,
						Type:      "plan",
						Message:   msg,
						Risk:      "high",
					}, canFlush, flusher)

					ch := client.RegisterApproval(requestID)
					select {
					case resp, ok := <-ch:
						if !ok {
							return
						}
						if resp.Action == "approve" {
							client.Steer(fmt.Sprintf("APPROVED by user: %s. Proceed with the planned action.", resp.Message))
						} else {
							client.Steer(fmt.Sprintf("DENIED by user: %s. Do NOT proceed with this action.", resp.Message))
						}
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Minute):
						client.Steer("APPROVAL_TIMEOUT: No response. Do NOT proceed.")
					}
				} else if idx := strings.Index(assistantText, "??QUESTION:"); idx >= 0 {
					approvalTriggered = true
					question := strings.TrimSpace(assistantText[idx+len("??QUESTION:"):])
					if question == "" {
						question = "Question asked"
					}
					requestID := fmt.Sprintf("req-%d", time.Now().UnixMilli())

					writeSSE(w, pi.EventQuestionAsked, pi.ApprovalRequestData{
						RequestID: requestID,
						Type:      "question",
						Message:   question,
						Risk:      "low",
					}, canFlush, flusher)

					ch := client.RegisterApproval(requestID)
					select {
					case resp, ok := <-ch:
						if !ok {
							return
						}
						client.Steer(fmt.Sprintf("USER ANSWER: %s", resp.Message))
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Minute):
						client.Steer("QUESTION_TIMEOUT: No response. Use your best judgment.")
					}
				}
			}

			// After agent_end, save assistant message and run post-completion
			if sseEvent.Type == pi.EventAgentEnd {
				// Extract thinking from agent_end messages if streaming didn't capture it
				if assistantThinking == "" && len(event.Messages) > 0 {
					var msgs []struct {
						Role    string `json:"role"`
						Content []struct {
							Type     string `json:"type"`
							Thinking string `json:"thinking"`
							Text     string `json:"text"`
						} `json:"content"`
					}
					if json.Unmarshal(event.Messages, &msgs) == nil {
						for i := len(msgs) - 1; i >= 0; i-- {
							if msgs[i].Role == "assistant" {
								for _, block := range msgs[i].Content {
									if block.Type == "thinking" && block.Thinking != "" {
										assistantThinking += block.Thinking
									}
									// Also grab text from content blocks if streaming missed it
									if block.Type == "text" && block.Text != "" && assistantText == "" {
										assistantText += block.Text
									}
								}
								break
							}
						}
					}
				}
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
