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
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
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

	// Channel-based approval manager (decoupled from Pi subprocess)
	approvalMgr *approval.Manager

	// Llama engine — always uses orchestrator + ephemeral sub-agents
	llamaEngine  *llamaeng.HTTPEngine
	geminiEngine *llamaeng.HTTPEngine // optional cloud provider
	sandboxMgr   *sandbox.Manager
	cuBridge     *ComputerUseBridge // bridges llama executor to CU/X11 handlers
	mcpClient    *mcp.Client        // optional: MCP research server for search/scrape

	eventStore     *storage.EventStore

	orchestrators   map[string]*llamaeng.OrchestratorLoop  // key: compositeKey(projectPath, agentId)
	selectedEngines map[string]*llamaeng.HTTPEngine        // per-agent engine override
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
		orchestrators:   make(map[string]*llamaeng.OrchestratorLoop),
		selectedEngines: make(map[string]*llamaeng.HTTPEngine),
	}
}

// SetLlamaEngine configures the handler for llama-server HTTP inference.
// When set, Prompt() uses the orchestrator + ephemeral sub-agent path.
func (h *PuxHandler) SetLlamaEngine(engine *llamaeng.HTTPEngine, sandboxMgr *sandbox.Manager, cu *ComputerUseHandler, x11 *X11Handler) {
	h.llamaEngine = engine
	h.sandboxMgr = sandboxMgr
	if cu != nil {
		h.cuBridge = &ComputerUseBridge{CU: cu, X11: x11, Log: h.log}
	}
}

// SetGeminiEngine configures the optional Gemini cloud engine.
func (h *PuxHandler) SetGeminiEngine(engine *llamaeng.HTTPEngine) {
	h.geminiEngine = engine
}

// SetMCPClient configures the MCP research server client for search/scrape tools.
func (h *PuxHandler) SetMCPClient(client *mcp.Client) {
	h.mcpClient = client
}

// SetEventStore configures the event store for session event persistence.
func (h *PuxHandler) SetEventStore(store *storage.EventStore) {
	h.eventStore = store
}

// RegisterRoutes registers all Pux routes on the given router.
func (h *PuxHandler) RegisterRoutes(r chi.Router) {
	r.Post("/prompt", h.Prompt)
	r.Post("/respond", h.Respond)
	r.Get("/tool-permissions", h.GetToolPermissions)
	r.Put("/tool-permissions", h.SetToolPermission)
	r.Get("/history", h.GetHistory)
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

// accumulateLlamaText accumulates text/thinking from llama events for DB persistence.
func (h *PuxHandler) accumulateLlamaText(evt llamaeng.AgentEvent, text, thinking *string) {
	switch evt.Type {
	case llamaeng.EventTypeTextDelta:
		*text += evt.Data.Text
	case llamaeng.EventTypeThinkingDelta:
		*thinking += evt.Data.Text
	}
}

// promptWithOrchestrator handles prompt requests using the orchestrator + sub-agent pattern.
// The orchestrator plans and delegates to specialized personas (web, code, desktop).
func (h *PuxHandler) promptWithOrchestrator(w http.ResponseWriter, r *http.Request, req promptRequest, projectPath string) {
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

	// Load project skills (SKILL.md files)
	var skillsBlock string
	skillLoader := llamaeng.NewSkillLoader(projectPath)
	if count, err := skillLoader.Load(); err == nil && count > 0 {
		skillsBlock = skillLoader.SkillsForPrompt() + "\n\n"
		h.log.Debug("Loaded project skills", zap.Int("count", count))
	}

	// Inject project memory into the message if available
	var memoryPrefix string
	if mem := orch.Memory(); mem != nil {
		memoryPrefix = mem.InjectPrefix()
	} else {
		memoryPrefix = llamaeng.ReadMemoryFile(projectPath)
		if memoryPrefix != "" {
			memoryPrefix = "<memory>\n" + memoryPrefix + "\n</memory>\n\n"
		}
	}

	// Set up SSE
	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)

	// Send agent_spawned event
	spawnData, _ := json.Marshal(map[string]string{"agentId": req.AgentId})
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", string(llamaeng.EventTypeAgentSpawned), string(spawnData))
	if canFlush {
		flusher.Flush()
	}

	// Save user message to DB
	if h.db != nil {
		if _, err := h.db.SaveUserMessage(r.Context(), req.Project, req.AgentId, req.Message); err != nil {
			h.log.Warn("Failed to save user message", zap.Error(err))
		}
	}

	// Create event channel — downstream is what SSE reads from
	events := make(chan llamaeng.AgentEvent, 256)

	// Wrap with persistence so non-delta events are saved to SQLite
	sessionID := fmt.Sprintf("%s:%s", req.Project, req.AgentId)
	orchEvents := llamaeng.PersistEvents(r.Context(), h.eventStore, sessionID, events)

	// Run orchestrator in a goroutine with a 3-minute timeout
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	var loopErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(orchEvents)
		// Format message to keep the task front and center for the 26B model
		orchMsg := fmt.Sprintf("%s%sUser request: %s\n\nCreate a plan and delegate each step.", skillsBlock, memoryPrefix, req.Message)
		if orch.Plan() == nil {
			loopErr = orch.Run(ctx, orchMsg, orchEvents)
		} else {
			loopErr = orch.Continue(ctx, orchMsg, orchEvents)
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
func (h *PuxHandler) getOrCreateOrchestrator(key, sandboxID, projectPath string) *llamaeng.OrchestratorLoop {
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
		sandboxExec := &llamaeng.SandboxToolExecutor{
			SandboxID:     sandboxID,
			Manager:       h.sandboxMgr,
			CU:            h.cuBridge,
			Logger:        h.log,
			VisionEnabled: h.cuBridge.CU.VisionClient() != nil,
			MCPClient:     h.mcpClient,
			ApprovalMgr:   (*approvalManagerAdapter)(h.approvalMgr),
		}

		// Wrap with hooks (git checkpoint, auditing, etc.)
		var hooks []llamaeng.ToolHook
		hooks = append(hooks, llamaeng.NewGitCheckpointHook(h.sandboxMgr, sandboxID, h.log))
		baseExecutor = llamaeng.NewHookedExecutor(sandboxExec, hooks, h.log)
	}

	cfg := llamaeng.OrchestratorConfig{
		ProjectDir: projectPath,
		SandboxID:  sandboxID,
		// ContextSize 0 means "use ModelConfig default" (32K)
	}

	// Use the per-agent selected engine, or fall back to the default llama engine
	engine := h.llamaEngine
	if sel, ok := h.selectedEngines[key]; ok {
		engine = sel
	}

	orch, err := llamaeng.NewOrchestratorLoop(engine, baseExecutor, cfg, h.log)
	if err != nil {
		h.log.Error("Failed to create orchestrator", zap.Error(err))
		return nil
	}

	// Wire transcript saver for pre-compaction snapshots
	if h.db != nil {
		orch.SetTranscriptSaver(&dbTranscriptSaver{db: h.db, log: h.log})
	}

	// Wire project memory (MEMORY.md per project)
	orch.SetMemory(llamaeng.NewProjectMemory(projectPath))

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

// approvalManagerAdapter wraps *approval.Manager to satisfy llamaeng.ApprovalManager.
// Both sides now use llamaeng.ApprovalResponse — no conversion needed.
type approvalManagerAdapter approval.Manager

func (a *approvalManagerAdapter) Register(requestID string) <-chan llamaeng.ApprovalResponse {
	return (*approval.Manager)(a).Register(requestID)
}

func (a *approvalManagerAdapter) Resolve(requestID string, resp llamaeng.ApprovalResponse) bool {
	return (*approval.Manager)(a).Resolve(requestID, resp)
}

// dbTranscriptSaver adapts Database to the TranscriptSaver interface.
type dbTranscriptSaver struct {
	db  *storage.Database
	log *zap.Logger
}

func (s *dbTranscriptSaver) SaveTranscript(messagesJSON []byte, reason string, tokenCount int) {
	ctx := context.Background()
	// Use empty session ID — the session ID is tracked at the event store level
	if _, err := s.db.SaveTranscript(ctx, "", "", string(messagesJSON), reason, tokenCount); err != nil {
		s.log.Warn("Failed to save transcript", zap.Error(err))
	}
}

func (a *approvalManagerAdapter) Cleanup(requestID string) {
	(*approval.Manager)(a).Cleanup(requestID)
}

// GetToolPermissions returns all configured tool permissions.
// GET /api/pux/tool-permissions
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
		ID       string `json:"id"`
		Name     string `json:"name"`
		Provider string `json:"provider,omitempty"`
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
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"models"`
				} `json:"providers"`
			}
			if json.Unmarshal(data, &settings) == nil {
				for providerName, provider := range settings.Providers {
					for _, m := range provider.Models {
						models = append(models, modelInfo{
							ID:       m.ID,
							Name:     m.Name,
							Provider: providerName,
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
	var engine *llamaeng.HTTPEngine
	switch {
	case req.ModelID == "gemini-3-flash-preview" && h.geminiEngine != nil:
		engine = h.geminiEngine
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

	// Evict existing orchestrator for this agent so next prompt uses the new engine
	if existing, ok := h.orchestrators[key]; ok {
		existing.Close()
		delete(h.orchestrators, key)
	}

	// Store the engine selection for this agent
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
// and creates a temporary HTTPEngine for it.
func (h *PuxHandler) engineFromSettings(providerID, modelID string) *llamaeng.HTTPEngine {
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

	eng := llamaeng.NewHTTPEngine(llamaeng.HTTPEngineConfig{
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
