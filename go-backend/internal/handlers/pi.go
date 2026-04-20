package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/git"
	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
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
	toolPerms  *pi.ToolPermissionConfig

	// Library mode — always uses orchestrator + ephemeral sub-agents
	llamaEngine *llamaeng.HTTPEngine
	sandboxMgr  *sandbox.Manager
	cuBridge    *ComputerUseBridge // bridges llama executor to CU/X11 handlers

	orchestrators map[string]*llamaeng.OrchestratorLoop // key: compositeKey(projectPath, agentId)
}

// NewPiHandler creates a new Pi handler.
func NewPiHandler(pool *pi.PiPool, db *storage.Database, gitOps *git.GitOps, gh *GitHubHandler, logger *zap.Logger) *PiHandler {
	return &PiHandler{
		pool:            pool,
		db:              db,
		git:             gitOps,
		github:          gh,
		log:             logger,
		litellmURL:      os.Getenv("LITELLM_PROXY_URL"),
		litellmKey:      os.Getenv("LITELLM_MASTER_KEY"),
		toolPerms:       pi.NewToolPermissionConfig(logger),
		orchestrators:   make(map[string]*llamaeng.OrchestratorLoop),
	}
}

// SetLlamaEngine configures the handler for llama-server HTTP inference.
// When set, Prompt() uses the orchestrator + ephemeral sub-agent path.
func (h *PiHandler) SetLlamaEngine(engine *llamaeng.HTTPEngine, sandboxMgr *sandbox.Manager, cu *ComputerUseHandler, x11 *X11Handler) {
	h.llamaEngine = engine
	h.sandboxMgr = sandboxMgr
	if cu != nil {
		h.cuBridge = &ComputerUseBridge{CU: cu, X11: x11, Log: h.log}
	}
}

// waitForApproval sends an approval SSE event and blocks until the user responds,
// the context is cancelled, or the timeout expires.
// Returns the approval response, or nil if the context timed out.
func (h *PiHandler) waitForApproval(
	ctx context.Context,
	w http.ResponseWriter,
	client *pi.PiClient,
	approvalData pi.ApprovalRequestData,
	canFlush bool,
	flusher http.Flusher,
) (*pi.ApprovalResponse, string) {
	writeSSE(w, pi.EventApprovalRequest, approvalData, canFlush, flusher)

	ch := client.RegisterApproval(approvalData.RequestID)
	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, "channel closed"
		}
		return &resp, ""
	case <-ctx.Done():
		return nil, "context cancelled"
	case <-time.After(5 * time.Minute):
		client.Steer("APPROVAL_TIMEOUT: do NOT proceed. No response received.")
		return nil, "timeout"
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
	r.Get("/tool-permissions", h.GetToolPermissions)
	r.Put("/tool-permissions", h.SetToolPermission)
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

	// Library-mode path: always use orchestrator + ephemeral sub-agents
	if h.llamaEngine != nil && h.llamaEngine.IsLoaded() {
		h.promptWithOrchestrator(w, r, req, projectPath)
		return
	}

	client, err := h.pool.GetOrCreateWithID(projectPath, req.AgentId)
	if req.Model != "" {
		// Re-fetch with fingerprint to validate settings haven't changed
		fp := pi.ComputeFingerprint(req.Model, "")
		client, err = h.pool.GetOrCreateWithFingerprint(projectPath, req.AgentId, fp)
	}
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

			// Intercept tool invocations that need approval before forwarding to SSE
			if sseEvent.Type == pi.EventToolStart && !approvalTriggered {
				if dataMap, ok := sseEvent.Data.(map[string]interface{}); ok {
					toolName, _ := dataMap["toolName"].(string)
					args, _ := dataMap["args"].(map[string]interface{})
					if args == nil {
						args = map[string]interface{}{}
					}

					needsApproval, riskLevel, reason := h.toolPerms.ShouldApprove(toolName, args)
					if needsApproval {
						requestID := fmt.Sprintf("req-%d", time.Now().UnixMilli())
						approvalTriggered = true

						resp, status := h.waitForApproval(ctx, w, client, pi.ApprovalRequestData{
							RequestID: requestID,
							Type:      "tool_confirm",
							ToolName:  toolName,
							ToolArgs:  args,
							Message:   reason + ": " + toolName,
							Risk:      riskLevel,
						}, canFlush, flusher)

						if status == "context cancelled" {
							return
						}
						if resp == nil {
							continue // timeout
						}
						if resp.Action == "approve" {
							client.Steer("APPROVED: " + resp.Message)
						} else {
							client.Steer("DENIED: do NOT execute " + toolName + ". Reason: " + resp.Message)
							continue
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

					resp, status := h.waitForApproval(ctx, w, client, pi.ApprovalRequestData{
						RequestID: requestID,
						Type:      "plan",
						Message:   msg,
						Risk:      "high",
					}, canFlush, flusher)
					if status == "context cancelled" {
						return
					}
					if resp != nil {
						if resp.Action == "approve" {
							client.Steer(fmt.Sprintf("APPROVED by user: %s. Proceed with the planned action.", resp.Message))
						} else {
							client.Steer(fmt.Sprintf("DENIED by user: %s. Do NOT proceed with this action.", resp.Message))
						}
					}
				} else if idx := strings.Index(assistantText, "??QUESTION:"); idx >= 0 {
					approvalTriggered = true
					question := strings.TrimSpace(assistantText[idx+len("??QUESTION:"):])
					if question == "" {
						question = "Question asked"
					}
					requestID := fmt.Sprintf("req-%d", time.Now().UnixMilli())

					resp, status := h.waitForApproval(ctx, w, client, pi.ApprovalRequestData{
						RequestID: requestID,
						Type:      "question",
						Message:   question,
						Risk:      "low",
					}, canFlush, flusher)
					if status == "context cancelled" {
						return
					}
					if resp != nil {
						client.Steer(fmt.Sprintf("USER ANSWER: %s", resp.Message))
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

// writeLlamaSSE converts a llama engine event to SSE and writes it.
func (h *PiHandler) writeLlamaSSE(w http.ResponseWriter, evt llamaeng.AgentEvent, canFlush bool, flusher http.Flusher) {
	piEvent := llamaeng.ConvertEvent(evt)
	sseEvt := h.mapEventToSSE(piEvent)
	if sseEvt == nil {
		return
	}

	// Ensure tool events have IDs
	if sseEvt.Type == pi.EventToolStart {
		if dataMap, ok := sseEvt.Data.(map[string]interface{}); ok {
			tid, _ := dataMap["toolId"].(string)
			if tid == "" {
				dataMap["toolId"] = nextToolFallbackId()
			}
		}
	}

	data, err := json.Marshal(sseEvt.Data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", sseEvt.Type, string(data))
	if canFlush {
		flusher.Flush()
	}
}

// accumulateLlamaText accumulates text/thinking from llama events for DB persistence.
func (h *PiHandler) accumulateLlamaText(evt llamaeng.AgentEvent, text, thinking *string) {
	switch evt.Type {
	case llamaeng.EventTypeTextDelta:
		*text += evt.Data.Text
	case llamaeng.EventTypeThinkingDelta:
		*thinking += evt.Data.Text
	}
}

// promptWithOrchestrator handles prompt requests using the orchestrator + sub-agent pattern.
// The orchestrator plans and delegates to specialized personas (web, code, desktop).
func (h *PiHandler) promptWithOrchestrator(w http.ResponseWriter, r *http.Request, req promptRequest, projectPath string) {
	key := compositeAgentKey(projectPath, req.AgentId)
	sandboxID := filepath.Base(projectPath)

	orch := h.getOrCreateOrchestrator(key, sandboxID, projectPath)
	if orch == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"success": false,
			"error":   "Failed to create orchestrator (VRAM full or model not loaded). Try again later.",
		})
		return
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

	// Save user message to DB
	if h.db != nil {
		if _, err := h.db.SaveUserMessage(r.Context(), req.Project, req.AgentId, req.Message); err != nil {
			h.log.Warn("Failed to save user message", zap.Error(err))
		}
	}

	// Create event channel
	events := make(chan llamaeng.AgentEvent, 256)

	// Run orchestrator in a goroutine with a 3-minute timeout
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	var loopErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(events)
		// Format message to keep the task front and center for the 26B model
		orchMsg := fmt.Sprintf("User request: %s\n\nCreate a plan and delegate each step.", req.Message)
		if orch.Plan() == nil {
			loopErr = orch.Run(ctx, orchMsg, events)
		} else {
			loopErr = orch.Continue(ctx, orchMsg, events)
		}
	}()

	// Stream events to SSE
	var assistantText, assistantThinking string
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			// Drain remaining events
			for evt := range events {
				h.writeLlamaSSE(w, evt, canFlush, flusher)
				h.accumulateLlamaText(evt, &assistantText, &assistantThinking)
			}
			if h.db != nil {
				if _, err := h.db.SaveAssistantMessage(ctx, req.Project, req.AgentId, assistantText, assistantThinking, "[]"); err != nil {
					h.log.Warn("Failed to save assistant message", zap.Error(err))
				}
			}
			if loopErr != nil {
				h.log.Error("Orchestrator error", zap.Error(loopErr))
			}
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			if canFlush {
				flusher.Flush()
			}
		case evt, ok := <-events:
			if !ok {
				return
			}
			keepalive.Reset(15 * time.Second)
			h.writeLlamaSSE(w, evt, canFlush, flusher)
			h.accumulateLlamaText(evt, &assistantText, &assistantThinking)
		}
	}
}

// getOrCreateOrchestrator returns an existing orchestrator or creates a new one.
// Evicts ALL previous orchestrators (even running ones) to free VRAM.
func (h *PiHandler) getOrCreateOrchestrator(key, sandboxID, projectPath string) *llamaeng.OrchestratorLoop {
	if orch, ok := h.orchestrators[key]; ok {
		return orch
	}

	// Evict ALL previous orchestrators (running or idle) to free VRAM
	for k, orch := range h.orchestrators {
		h.log.Info("Evicting orchestrator", zap.String("evictKey", k), zap.Bool("wasRunning", orch.IsRunning()))
		orch.Close()
		delete(h.orchestrators, k)
	}

	// Build base executor for sub-agents
	var baseExecutor llamaeng.ToolExecutor
	if h.sandboxMgr != nil {
		baseExecutor = &llamaeng.SandboxToolExecutor{
			SandboxID:     sandboxID,
			Manager:       h.sandboxMgr,
			CU:            h.cuBridge,
			Logger:        h.log,
			VisionEnabled: h.cuBridge.CU.VisionClient() != nil,
		}
	}

	cfg := llamaeng.OrchestratorConfig{
		ProjectDir: projectPath,
		SandboxID:  sandboxID,
		// ContextSize 0 means "use ModelConfig default" (32K)
	}

	orch, err := llamaeng.NewOrchestratorLoop(h.llamaEngine, baseExecutor, cfg, h.log)
	if err != nil {
		h.log.Error("Failed to create orchestrator", zap.Error(err))
		return nil
	}

	h.orchestrators[key] = orch
	h.log.Info("Created new orchestrator loop",
		zap.String("key", key),
		zap.String("sandbox", sandboxID),
	)
	return orch
}



// compositeAgentKey builds a key from projectPath and agentId for the llama loops map.
func compositeAgentKey(projectPath, agentId string) string {
	return projectPath + "\x00" + agentId
}

// GetToolPermissions returns all configured tool permissions.
// GET /api/pi/tool-permissions
func (h *PiHandler) GetToolPermissions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.toolPerms.AllPermissions())
}

// SetToolPermission updates a single tool's permission level.
// PUT /api/pi/tool-permissions
func (h *PiHandler) SetToolPermission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tool   string `json:"tool"`
		Level  string `json:"level"` // "auto", "confirm", "deny"
		Reason string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Tool == "" || req.Level == "" {
		JSONError(w, "tool and level are required", http.StatusBadRequest)
		return
	}

	h.toolPerms.SetPermission(req.Tool, pi.PermissionLevel(req.Level), req.Reason)
	h.log.Info("Tool permission updated",
		zap.String("tool", req.Tool),
		zap.String("level", req.Level),
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"tool":    req.Tool,
		"level":   req.Level,
	})
}
