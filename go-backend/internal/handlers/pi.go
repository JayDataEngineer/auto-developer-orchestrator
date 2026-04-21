package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/approval"
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
	db         *storage.Database
	git        *git.GitOps
	github     *GitHubHandler
	log        *zap.Logger
	litellmURL string
	litellmKey string
	toolPerms  *pi.ToolPermissionConfig

	// Channel-based approval manager (decoupled from Pi subprocess)
	approvalMgr *approval.Manager

	// Llama engine — always uses orchestrator + ephemeral sub-agents
	llamaEngine *llamaeng.HTTPEngine
	sandboxMgr  *sandbox.Manager
	cuBridge    *ComputerUseBridge // bridges llama executor to CU/X11 handlers

	orchestrators map[string]*llamaeng.OrchestratorLoop // key: compositeKey(projectPath, agentId)
}

// NewPiHandler creates a new Pi handler.
func NewPiHandler(db *storage.Database, gitOps *git.GitOps, gh *GitHubHandler, logger *zap.Logger) *PiHandler {
	return &PiHandler{
		db:            db,
		git:           gitOps,
		github:        gh,
		log:           logger,
		litellmURL:    os.Getenv("LITELLM_PROXY_URL"),
		litellmKey:    os.Getenv("LITELLM_MASTER_KEY"),
		toolPerms:     pi.NewToolPermissionConfig(logger),
		approvalMgr:   approval.NewManager(5 * time.Minute),
		orchestrators: make(map[string]*llamaeng.OrchestratorLoop),
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
	approvalData pi.ApprovalRequestData,
	canFlush bool,
	flusher http.Flusher,
) (*pi.ApprovalResponse, string) {
	writeSSE(w, pi.EventApprovalRequest, approvalData, canFlush, flusher)

	// Use the decoupled approval manager
	ch := h.approvalMgr.Register(approvalData.RequestID)
	defer h.approvalMgr.Cleanup(approvalData.RequestID)

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, "channel closed"
		}
		return &resp, ""
	case <-ctx.Done():
		return nil, "context cancelled"
	case <-time.After(h.approvalMgr.Timeout()):
		return nil, "timeout"
	}
}

// RegisterRoutes registers all Pi routes on the given router.
func (h *PiHandler) RegisterRoutes(r chi.Router) {
	r.Post("/prompt", h.Prompt)
	r.Post("/respond", h.Respond)
	r.Get("/tool-permissions", h.GetToolPermissions)
	r.Put("/tool-permissions", h.SetToolPermission)
	r.Get("/history", h.GetHistory)
	r.Delete("/conversation", h.DeleteConversation)
	r.Put("/conversation/rename", h.RenameConversation)
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

	resp := pi.ApprovalResponse{
		Action:  req.Action,
		Message: req.Message,
	}

	// Use the decoupled approval manager (works with both Pi and orchestrator paths)
	if ok := h.approvalMgr.Resolve(req.RequestID, resp); !ok {
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

	// No engine available
	writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
		"success": false,
		"error":   "Agent engine not available. Start llama-server first.",
	})
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

	// Resolve sandbox ID via project path lookup, fall back to basename
	sandboxID := ""
	if h.sandboxMgr != nil {
		if sb := h.sandboxMgr.FindSandboxByProject(projectPath); sb != nil {
			sandboxID = sb.ID
		}
	}
	if sandboxID == "" {
		sandboxID = filepath.Base(projectPath)
	}

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

// GetHistory returns conversation history for a project+agent.
// GET /api/pi/history?project=...&agentId=...&limit=...
func (h *PiHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentID := r.URL.Query().Get("agentId")
	if project == "" {
		JSONError(w, "project query parameter is required", http.StatusBadRequest)
		return
	}
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := fmt.Sscanf(l, "%d", &limit); err != nil || n != 1 {
			limit = 200
		}
	}

	if h.db == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	msgs, err := h.db.GetConversationHistory(r.Context(), project, agentID, limit)
	if err != nil {
		JSONError(w, "Failed to get history", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// DeleteConversation deletes all messages for a project+agent.
// DELETE /api/pi/conversation?project=...&agentId=...
func (h *PiHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentID := r.URL.Query().Get("agentId")
	if project == "" {
		JSONError(w, "project query parameter is required", http.StatusBadRequest)
		return
	}

	if h.db == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	if err := h.db.ClearConversationHistory(r.Context(), project, agentID); err != nil {
		JSONError(w, "Failed to delete conversation", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// RenameConversation sets a custom title for a conversation.
// PUT /api/pi/conversation/rename
func (h *PiHandler) RenameConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		AgentID string `json:"agentId"`
		Title   string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Project == "" || req.Title == "" {
		JSONError(w, "project and title are required", http.StatusBadRequest)
		return
	}

	if h.db == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	if err := h.db.SetConversationTitle(r.Context(), req.Project, req.AgentID, req.Title); err != nil {
		JSONError(w, "Failed to rename conversation", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
