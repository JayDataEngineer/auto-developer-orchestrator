package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"path/filepath"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/approval"
	"github.com/auto-developer-orchestrator/backend/internal/browser"
	"github.com/auto-developer-orchestrator/backend/internal/git"
	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/observability"
	"github.com/auto-developer-orchestrator/backend/internal/perms"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/auto-developer-orchestrator/backend/internal/tools/ask"
	"github.com/auto-developer-orchestrator/backend/internal/tools/plan"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// PuxHandler handles Pux agent HTTP endpoints.
type PuxHandler struct {
	db         *storage.Database
	git        *git.GitOps
	github     *GitHubHandler
	log        *zap.Logger
	litellmURL string
	litellmKey string
	toolPerms  *perms.ToolPermissionConfig

	llamaEngine     *llamaeng.LLMClient // primary llama-server engine (local GPU)
	clusterEngine   *llamaeng.LLMClient // Ray cluster LLM (Qwen3.6, always-on remote)
	geminiEngine    *llamaeng.LLMClient // optional Gemini cloud provider
	openrouterEngine *llamaeng.LLMClient // optional OpenRouter cloud provider
	sandboxMgr   *sandbox.Manager
	cuBridge     *ComputerUseBridge // bridges llama executor to CU/X11 handlers
	mcpClient    *mcp.Client        // optional: MCP research server for search/scrape
	mcpMulti     *mcp.MultiClient   // optional: multi-server MCP routing

	visionClient   *browser.VisionClient // local llama.cpp vision (second fallback tier)
	eventStore     *storage.EventStore
	approvalMgr    *approval.Manager // central approval manager for Respond endpoint

	metrics  *observability.Metrics
	langfuse *observability.LangfuseClient

	selectedEngines map[string]*llamaeng.LLMClient // per-agent engine override
}

// NewPuxHandler creates a new Pux handler.
func NewPuxHandler(db *storage.Database, gitOps *git.GitOps, gh *GitHubHandler, logger *zap.Logger) *PuxHandler {
	return &PuxHandler{
		db:            db,
		git:           gitOps,
		github:        gh,
		log:           logger,
		litellmURL:    os.Getenv("LITELLM_PROXY_URL"),
		litellmKey:    os.Getenv("LITELLM_MASTER_KEY"),
		toolPerms:     perms.NewToolPermissionConfig(logger),
		approvalMgr:   approval.NewManager(5 * time.Minute),
		selectedEngines: make(map[string]*llamaeng.LLMClient),
	}
}

// SetLlamaEngine configures the handler for llama-server HTTP inference.
// When set, Prompt() uses the orchestrator + ephemeral sub-agent path.
func (h *PuxHandler) SetLlamaEngine(engine *llamaeng.LLMClient, sandboxMgr *sandbox.Manager, cu *ComputerUseHandler, x11 *X11Handler) {
	h.llamaEngine = engine
	h.sandboxMgr = sandboxMgr
	if cu != nil {
		h.cuBridge = &ComputerUseBridge{CU: cu, X11: x11, Log: h.log}
	}
}

// SetClusterEngine configures the Ray cluster LLM (Qwen3.6, always-on).
func (h *PuxHandler) SetClusterEngine(engine *llamaeng.LLMClient) {
	h.clusterEngine = engine
}

// SetGeminiEngine configures the optional Gemini cloud engine.
func (h *PuxHandler) SetGeminiEngine(engine *llamaeng.LLMClient) {
	h.geminiEngine = engine
}

// SetOpenRouterEngine configures the optional OpenRouter cloud engine.
func (h *PuxHandler) SetOpenRouterEngine(engine *llamaeng.LLMClient) {
	h.openrouterEngine = engine
}

// SetSandboxOnly wires sandbox manager + computer use without a local LLM engine.
// Used in cloud-only mode (OpenRouter, Gemini) when llama-server is off.
func (h *PuxHandler) SetSandboxOnly(sandboxMgr *sandbox.Manager, cu *ComputerUseHandler, x11 *X11Handler) {
	h.sandboxMgr = sandboxMgr
	if cu != nil {
		h.cuBridge = &ComputerUseBridge{CU: cu, X11: x11, Log: h.log}
	}
}

// SetMCPClient configures the MCP research server client for search/scrape tools.
func (h *PuxHandler) SetMCPClient(client *mcp.Client) {
	h.mcpClient = client
}

// SetMCPMulti configures the multi-server MCP client for routing tool calls.
func (h *PuxHandler) SetMCPMulti(multi *mcp.MultiClient) {
	h.mcpMulti = multi
}

// SetVisionClient configures the local vision client (llama.cpp) for fallback vision.
func (h *PuxHandler) SetVisionClient(vc *browser.VisionClient) {
	h.visionClient = vc
}

// SetEventStore configures the event store for session event persistence.
func (h *PuxHandler) SetEventStore(store *storage.EventStore) {
	h.eventStore = store
}

// SetMetrics configures Prometheus metrics recording.
func (h *PuxHandler) SetMetrics(m *observability.Metrics) {
	h.metrics = m
}

// SetLangfuse configures Langfuse tracing.
func (h *PuxHandler) SetLangfuse(lf *observability.LangfuseClient) {
	h.langfuse = lf
}

// RegisterRoutes registers all Pux routes on the given router.
func (h *PuxHandler) RegisterRoutes(r chi.Router) {
	r.Post("/prompt", h.Prompt)
	r.Post("/respond", h.Respond)
	r.Post("/user-response", h.UserResponse)
	r.Post("/plan-response", h.PlanResponse)
	r.Get("/tool-permissions", h.GetToolPermissions)
	r.Put("/tool-permissions", h.SetToolPermission)
	r.Get("/history", h.GetHistory)
	r.Get("/conversations", h.GetConversations)
	r.Delete("/conversation", h.DeleteConversation)
	r.Put("/conversation/rename", h.RenameConversation)
	r.Get("/models", h.GetModels)
	r.Put("/model", h.SetModel)
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
func (h *PuxHandler) Respond(w http.ResponseWriter, r *http.Request) {
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

	resp := llamaeng.ApprovalResponse{
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

// UserResponse handles responses to ask_user questions from the TUI.
// POST /api/pux/user-response
func (h *PuxHandler) UserResponse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuestionID string `json:"questionId"`
		Response   string `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}
	if req.QuestionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "questionId is required",
		})
		return
	}

	if ok := ask.PendingQuestions.Resolve(req.QuestionID, req.Response); !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "No pending question found for this ID (may have timed out)",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// PlanResponse handles responses to create_plan approval requests from the TUI.
// POST /api/pux/plan-response
func (h *PuxHandler) PlanResponse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlanID   string `json:"planId"`
		Action   string `json:"action"`   // "approve", "refine", "cancel"
		Feedback string `json:"feedback"` // optional, for refine
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}
	if req.PlanID == "" || req.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "planId and action are required",
		})
		return
	}
	if req.Action != "approve" && req.Action != "refine" && req.Action != "cancel" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "action must be 'approve', 'refine', or 'cancel'",
		})
		return
	}

	if ok := plan.PendingPlans.Resolve(req.PlanID, plan.PlanResponse{
		Action:   req.Action,
		Feedback: req.Feedback,
	}); !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "No pending plan found for this ID (may have timed out)",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
type promptRequest struct {
	Message       string `json:"message"`
	Project       string `json:"project"`
	AgentId       string `json:"agentId,omitempty"`
	Model         string `json:"model,omitempty"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
	AutoBranch    bool   `json:"autoBranch,omitempty"`
	AutoMerge     bool   `json:"autoMerge,omitempty"`
}

// Prompt sends a coding task to Pux and streams events back via SSE.
func (h *PuxHandler) Prompt(w http.ResponseWriter, r *http.Request) {
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

	// Resolve engine for this prompt.
	key := compositeAgentKey(projectPath, req.AgentId)

	// 1) Per-agent previously selected engine (from PUT /model)
	if selEngine, ok := h.selectedEngines[key]; ok && selEngine.IsLoaded() {
		h.llamaEngine = selEngine
	}

	// 2) Inline model override from request (e.g. CLI --model deepseek/deepseek-v4-flash)
	if req.Model != "" && (h.llamaEngine == nil || h.llamaEngine.ModelName() != req.Model) {
		if eng := h.resolveEngineForModel(req.Model); eng != nil {
			h.selectedEngines[key] = eng
			h.llamaEngine = eng
			h.log.Info("Using model from request", zap.String("model", req.Model))
		}
	}

	// Library-mode path: always use orchestrator + ephemeral sub-agents
	if h.llamaEngine != nil && h.llamaEngine.IsLoaded() {
		h.promptWithOrchestrator(w, r, req, projectPath)
		return
	}

	// Try to bootstrap from settings.json (cloud providers)
	if eng := h.engineFromSettings("openrouter", "deepseek/deepseek-v4-flash"); eng != nil {
		h.llamaEngine = eng
		h.selectedEngines[key] = eng
		h.log.Info("Bootstrapped cloud engine (no local model)", zap.String("model", eng.ModelName()))
		h.promptWithOrchestrator(w, r, req, projectPath)
		return
	}

	// No engine available
	writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
		"success": false,
		"error":   "Agent engine not available. Start llama-server or configure a cloud provider.",
	})
}

// writeLlamaSSE converts a llama engine event to SSE and writes it.
func (h *PuxHandler) writeLlamaSSE(w http.ResponseWriter, evt llamaeng.AgentEvent, canFlush bool, flusher http.Flusher) {
	sseEvt := h.mapEventToSSE(evt)
	if sseEvt == nil {
		return
	}

	// Ensure tool events have IDs
	if sseEvt.Type == "tool_execution_start" {
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

// compositeAgentKey builds a key from projectPath and agentId.
func compositeAgentKey(projectPath, agentId string) string {
	return projectPath + "\x00" + agentId
}

// GetToolPermissions returns all configured tool permissions.
func (h *PuxHandler) GetToolPermissions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.toolPerms.AllPermissions())
}

// SetToolPermission updates a single tool's permission level.
// PUT /api/pux/tool-permissions
func (h *PuxHandler) SetToolPermission(w http.ResponseWriter, r *http.Request) {
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

	h.toolPerms.SetPermission(req.Tool, perms.PermissionLevel(req.Level), req.Reason)
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
// GET /api/pux/history?project=...&agentId=...&limit=...
func (h *PuxHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
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

// GetConversations returns a summary list of all conversations.
// GET /api/pux/conversations
func (h *PuxHandler) GetConversations(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	project := r.URL.Query().Get("project")
	summaries, err := h.db.GetConversationSummaries(r.Context())
	if err != nil {
		JSONError(w, "Failed to get conversations", http.StatusInternalServerError)
		return
	}
	if summaries == nil {
		summaries = []storage.ConversationSummary{}
	}
	// Filter by project if specified
	if project != "" {
		filtered := make([]storage.ConversationSummary, 0, len(summaries))
		for _, s := range summaries {
			if s.Project == project {
				filtered = append(filtered, s)
			}
		}
		summaries = filtered
	}
	writeJSON(w, http.StatusOK, summaries)
}

// DeleteConversation deletes all messages for a project+agent.
// DELETE /api/pux/conversation?project=...&agentId=...
func (h *PuxHandler) DeleteConversation(w http.ResponseWriter, r *http.Request) {
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
// PUT /api/pux/conversation/rename
func (h *PuxHandler) RenameConversation(w http.ResponseWriter, r *http.Request) {
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

// GetModels returns available models from settings.json.
// GET /api/pux/models
func (h *PuxHandler) GetModels(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Provider      string `json:"provider,omitempty"`
		ContextWindow int    `json:"contextWindow,omitempty"`
	}

	models := []modelInfo{}

	// Read settings.json to discover all providers/models
	homeDir, err := os.UserHomeDir()
	if err == nil {
		settingsPath := filepath.Join(homeDir, ".pi", "agent", "settings.json")
		if data, err := os.ReadFile(settingsPath); err == nil {
			var settings struct {
				Providers map[string]struct {
					Models []struct {
						ID            string `json:"id"`
						Name          string `json:"name"`
						ContextWindow int    `json:"contextWindow"`
					} `json:"models"`
				} `json:"providers"`
			}
			if json.Unmarshal(data, &settings) == nil {
				for providerName, provider := range settings.Providers {
					for _, m := range provider.Models {
						models = append(models, modelInfo{
							ID:            m.ID,
							Name:          m.Name,
							Provider:      providerName,
							ContextWindow: m.ContextWindow,
						})
					}
				}
			}
		}
	}

	// If no models found from settings, add the engine's current model
	if len(models) == 0 && h.llamaEngine != nil {
		models = append(models, modelInfo{
			ID:       h.llamaEngine.ModelName(),
			Name:     h.llamaEngine.ModelName(),
			Provider: "llamacpp",
		})
	}

	writeJSON(w, http.StatusOK, models)
}

// SetModel switches the active engine for a specific agent.
// PUT /api/pux/model
func (h *PuxHandler) SetModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project  string `json:"project"`
		Provider string `json:"provider"`
		ModelID  string `json:"modelId"`
		AgentID  string `json:"agentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Resolve which engine to use based on provider/model
	var engine *llamaeng.LLMClient
	switch {
	case req.ModelID == "gemini-3-flash-preview" && h.geminiEngine != nil:
		engine = h.geminiEngine
	case strings.Contains(req.ModelID, "deepseek") && h.openrouterEngine != nil:
		engine = h.openrouterEngine
	default:
		// For cloud models, try to create an engine from settings.json
		if req.Provider != "llamacpp" && req.Provider != "" {
			if eng := h.engineFromSettings(req.Provider, req.ModelID); eng != nil {
				engine = eng
			}
		}
		// Fall back to local llama engine
		if engine == nil {
			engine = h.llamaEngine
		}
	}

	if engine == nil {
		JSONError(w, "No engine available for the requested model", http.StatusServiceUnavailable)
		return
	}

	// Resolve project path to build the orchestrator key
	projectPath := resolveProjectPath(req.Project, h.db)
	key := compositeAgentKey(projectPath, req.AgentID)

	// Store the engine selection for this agent (next prompt uses it)
	h.selectedEngines[key] = engine

	h.log.Info("Model switched",
		zap.String("model", req.ModelID),
		zap.String("provider", req.Provider),
		zap.String("agent", req.AgentID),
		zap.String("engine_model", engine.ModelName()),
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"model":   engine.ModelName(),
	})
}

// engineFromSettings reads a provider's apiKey and baseUrl from settings.json
// and creates a temporary LLMClient for it.
func (h *PuxHandler) engineFromSettings(providerID, modelID string) *llamaeng.LLMClient {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(homeDir + "/.pi/agent/settings.json")
	if err != nil {
		return nil
	}

	var settings struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
		} `json:"providers"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return nil
	}

	p, ok := settings.Providers[providerID]
	if !ok || p.APIKey == "" || p.BaseURL == "" {
		return nil
	}

	eng := llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
		BaseURL:   p.BaseURL,
		APIKey:    p.APIKey,
		ModelName: modelID,
		Logger:    h.log,
	})
	if err := eng.LoadModel(); err != nil {
		h.log.Warn("Failed to create engine from settings", zap.String("provider", providerID), zap.Error(err))
		return nil
	}
	return eng
}

// resolveEngineForModel finds the provider for a model ID in settings.json
// and creates an LLMClient. Returns nil if the model is not found.
func (h *PuxHandler) resolveEngineForModel(modelID string) *llamaeng.LLMClient {
	// Check pre-built engines first
	switch {
	case modelID == "gemini-3-flash-preview" && h.geminiEngine != nil:
		return h.geminiEngine
	case strings.Contains(modelID, "deepseek") && h.openrouterEngine != nil:
		return h.openrouterEngine
	case strings.Contains(modelID, "qwen") && h.clusterEngine != nil:
		return h.clusterEngine
	}

	// Scan settings.json for matching model
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".pi", "agent", "settings.json"))
	if err != nil {
		return nil
	}

	var settings struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return nil
	}

	for providerName, provider := range settings.Providers {
		for _, m := range provider.Models {
			if m.ID == modelID {
				return h.engineFromSettings(providerName, modelID)
			}
		}
	}

	// Try as local model name (llamacpp)
	if h.llamaEngine != nil && h.llamaEngine.ModelName() == modelID {
		return h.llamaEngine
	}

	return nil
}
