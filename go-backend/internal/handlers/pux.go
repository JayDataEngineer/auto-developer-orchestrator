package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	r.Post("/compact", h.Compact)
}

// resolveAgent reads ?agentId= from the query string, defaulting to "default".
func resolveAgent(r *http.Request) string {
	aid := r.URL.Query().Get("agentId")
	if aid == "" {
		return "default"
	}
	return aid
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

// Compact triggers a manual context compaction for the given agent session.
// This is a lightweight operation — it clears old tool results (micro-compact).
// Full LLM-based compaction happens automatically via the CompactionHook during the agent loop.
func (h *PuxHandler) Compact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		AgentID string `json:"agentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Project == "" {
		req.Project = "default"
	}
	if req.AgentID == "" {
		req.AgentID = "default"
	}

	// Micro-compact: trim old messages from the database
	// The session tree handles full compaction internally during the agent loop
	compacted, err := h.db.CompactSession(r.Context(), req.Project, req.AgentID)
	if err != nil {
		h.log.Warn("manual compact failed", zap.String("project", req.Project), zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "ok",
		"compactedMessages": compacted,
	})
}
