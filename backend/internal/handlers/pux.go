package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/agents/orchestrator"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/browser"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	"github.com/auto-developer-orchestrator/backend/internal/git"
	"github.com/auto-developer-orchestrator/backend/internal/hooks"
	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	puxssh "github.com/auto-developer-orchestrator/backend/internal/ssh"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/observability"
	"github.com/auto-developer-orchestrator/backend/internal/perms"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/session"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/auto-developer-orchestrator/backend/internal/vision"
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
	bashRules  *perms.BashRuleStore

	llamaEngine     *llamaeng.LLMClient // primary llama-server engine (local GPU)
	clusterEngine   *llamaeng.LLMClient // Ray cluster LLM (Qwen3.6, always-on remote)
	geminiEngine    *llamaeng.LLMClient // optional Gemini cloud provider
	openrouterEngine *llamaeng.LLMClient // optional OpenRouter cloud provider
	sandboxMgr   *sandbox.Manager
	cuBridge     *ComputerUseBridge // bridges llama executor to CU/X11 handlers
	mcpClient    *mcp.Client        // optional: MCP research server for search/scrape
	mcpMulti     *mcp.MultiClient   // optional: multi-server MCP routing

	visionClient   *browser.VisionClient // local llama.cpp vision (second fallback tier)
	imageServer    *vision.ImageServer   // serves temp images for MCP vision tools
	eventStore     *storage.EventStore
	schedulerTool  any               // schedulertool.Backend — scheduler tool for LLM
	hookBridge     *hooks.SSEHookBridge // SSE hook bridge for TUI interception

	metrics  *observability.Metrics
	langfuse *observability.LangfuseClient

	sshManager  *puxssh.SessionManager // SSH session manager for remote browsing
	sshBrowse   *SshBrowseHandler      // SSH browse HTTP handlers

	selectedEngines map[string]*llamaeng.LLMClient // per-agent engine override
	registry       *AgentRegistry                   // tracks running agents
	activeAgents   sync.Map                         // key="project:agentId" → *orchestrator.Agent for cancel
	taskMgr        *core.TaskManager                // background task manager

	defaultLogic  string // model ID for CTO/orchestrator (logic)
	defaultWorker string // model ID for sub-agents/employees (worker)

	providerRetries int // LLM provider retry count (0 = default 5)
}

// SetProviderRetries configures the LLM provider retry count.
func (h *PuxHandler) SetProviderRetries(n int) { h.providerRetries = n }

// NewPuxHandler creates a new Pux handler.
func NewPuxHandler(db *storage.Database, gitOps *git.GitOps, gh *GitHubHandler, logger *zap.Logger) *PuxHandler {
	tp := perms.NewToolPermissionConfig(logger)
	// Load persisted permission overrides
	permPath := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "tool_permissions.json")
	if err := tp.Load(permPath); err != nil {
		logger.Warn("Failed to load tool permissions", zap.Error(err))
	}

	br := perms.NewBashRuleStore(logger)
	rulesPath := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "bash_command_rules.json")
	if err := br.Load(rulesPath); err != nil {
		logger.Warn("Failed to load bash command rules", zap.Error(err))
	}

	return &PuxHandler{
		db:            db,
		git:           gitOps,
		github:        gh,
		log:           logger,
		litellmURL:    os.Getenv("LITELLM_PROXY_URL"),
		litellmKey:    os.Getenv("LITELLM_MASTER_KEY"),
		toolPerms:     tp,
		bashRules:     br,
		selectedEngines: make(map[string]*llamaeng.LLMClient),
		registry:       NewAgentRegistry(),
		taskMgr:        core.NewTaskManager(filepath.Join(os.Getenv("HOME"), ".pi", "agent", "task-results")),
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

// SetSchedulerTool wires the scheduler tool backend for LLM access.
func (h *PuxHandler) SetSchedulerTool(backend any) {
	h.schedulerTool = backend
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

// SetSSHManager wires the SSH session manager for remote filesystem browsing.
func (h *PuxHandler) SetSSHManager(mgr *puxssh.SessionManager) {
	h.sshManager = mgr
	h.sshBrowse = NewSshBrowseHandler(mgr, h.log)
}

// CloseSSH closes all SSH sessions on shutdown.
func (h *PuxHandler) CloseSSH() {
	if h.sshManager != nil {
		h.sshManager.CloseAll()
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

// SetImageServer configures the temp image file server for MCP vision tools.
func (h *PuxHandler) SetImageServer(is *vision.ImageServer) {
	h.imageServer = is
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
	r.Post("/decision", h.Decision)
	r.Get("/tool-permissions", h.GetToolPermissions)
	r.Put("/tool-permissions", h.SetToolPermission)
	r.Get("/bash-rules", h.GetBashRules)
	r.Post("/bash-rules", h.AddBashRule)
	r.Delete("/bash-rules/{id}", h.RemoveBashRule)
	r.Get("/bash-system-rules", h.GetBashSystemRules)
	r.Get("/history", h.GetHistory)
	r.Get("/conversations", h.GetConversations)
	r.Get("/agent-status", h.GetAgentStatus)
	r.Post("/cancel", h.CancelAgent)
	r.Delete("/conversation", h.DeleteConversation)
	r.Put("/conversation/rename", h.RenameConversation)
	r.Put("/conversation/mark-read", h.MarkRead)
	r.Get("/models", h.GetModels)
	r.Get("/providers", h.GetProviders)
	r.Post("/providers", h.AddProvider)
	r.Put("/model", h.SetModel)
	r.Post("/compact", h.Compact)
	r.Post("/hook-response", h.HookResponse)
	r.Get("/tree", h.GetTree)
	r.Post("/fork", h.Fork)
	r.Get("/files", h.GetProjectFiles)
	r.Get("/file", h.GetProjectFile)
	r.Get("/project-info", h.GetProjectInfo)
	r.Put("/file", h.SaveProjectFile)
	r.Post("/file/create", h.CreateProjectFile)
	r.Post("/file/move", h.MoveProjectFile)
	r.Delete("/file", h.DeleteProjectFile)
	r.Post("/file/restore", h.RestoreProjectFile)
	r.Get("/git/diff", h.GetGitDiff)
	r.Post("/feedback", h.Feedback)
	r.Post("/suggestions", h.Suggestions)
	r.Get("/mcp-servers", h.GetMCPServers)
	r.Post("/mcp-servers", h.AddMCPServer)
	r.Delete("/mcp-servers", h.RemoveMCPServer)
	r.Get("/defaults", h.GetDefaults)
	r.Put("/defaults", h.SetDefaults)

	// File checkpoint management
	r.Get("/checkpoints", h.ListCheckpoints)
	r.Get("/checkpoints/{id}/files", h.GetCheckpointFiles)
	r.Post("/checkpoints/{id}/restore", h.RestoreCheckpoint)
	r.Get("/checkpoints/file-history", h.GetFileHistory)
	r.Post("/checkpoints/file-restore", h.RestoreFileVersion)

	// Session rewind
	r.Get("/rewind", h.GetRewindCheckpoints)
	r.Post("/rewind", h.RewindSession)

	// Background task management
	r.Post("/tasks/{taskID}/background", h.BackgroundTask)
	r.Get("/tasks", h.ListTasks)
	r.Get("/tasks/{taskID}", h.GetTaskStatus)

	// SSH remote browse
	if h.sshBrowse != nil {
		r.Post("/ssh/connect", h.sshBrowse.SshConnect)
		r.Post("/ssh/browse", h.sshBrowse.SshBrowse)
		r.Post("/ssh/disconnect", h.sshBrowse.SshDisconnect)
		r.Post("/ssh/trust-host", h.sshBrowse.SshTrustHost)
		r.Post("/ssh/mkdir", h.sshBrowse.SshMkdir)
	}
}

// GetAgentStatus returns running agent status.
// GET /api/pux/agent-status?project=...&agentId=... (single)
// GET /api/pux/agent-status (all running)
func (h *PuxHandler) GetAgentStatus(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentID := r.URL.Query().Get("agentId")
	if project != "" && agentID != "" {
		if h.registry.IsRunning(project, agentID) {
			running := h.registry.GetAllRunning()
			for _, e := range running {
				if e.Project == project && e.AgentID == agentID {
					writeJSON(w, http.StatusOK, e)
					return
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "idle"})
		return
	}
	writeJSON(w, http.StatusOK, h.registry.GetAllRunning())
}

// CancelAgent aborts a running agent by cancelling its context.
func (h *PuxHandler) CancelAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		AgentId string `json:"agentId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	key := req.Project + ":" + req.AgentId
	val, ok := h.activeAgents.Load(key)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not running"})
		return
	}
	agent, ok := val.(*orchestrator.Agent)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid agent type"})
		return
	}
	agent.Loop().Abort()
	h.activeAgents.Delete(key)
	h.log.Info("Agent cancelled", zap.String("key", key))
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

type promptRequest struct {
	Message       string   `json:"message"`
	Images        []string `json:"images,omitempty"`
	Project       string   `json:"project"`
	Org           string   `json:"org,omitempty"`
	AgentId       string   `json:"agentId,omitempty"`
	Model         string   `json:"model,omitempty"`
	ThinkingLevel string   `json:"thinkingLevel,omitempty"`
	AutoBranch    bool     `json:"autoBranch,omitempty"`
	AutoMerge     bool     `json:"autoMerge,omitempty"`
	SandboxOnly   bool     `json:"sandboxOnly,omitempty"`
}

// Prompt sends a coding task to Pux and streams events back via SSE.
func (h *PuxHandler) Prompt(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[promptRequest](w, r)
	if !ok { return }

	if req.Message == "" {
		JSONError(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Auto-generate agentId if not provided
	if req.AgentId == "" {
		req.AgentId = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
	}

	projectRes, projectOk := resolveProjectFull(req.Project, h.db)
	if !projectOk {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"success": false, "error": "Project not found",
		})
		return
	}
	projectPath := projectRes.Path

	// Load defaults from settings.json on first call
	if h.defaultLogic == "" && h.defaultWorker == "" {
		logic, worker := readDefaults()
		h.defaultLogic = logic
		h.defaultWorker = worker
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
		} else {
			h.log.Warn("Model not found in any provider, falling back to current engine",
				zap.String("model", req.Model))
		}
	}

	// 3) Use logic default — always preferred over selectedEngines/local/cluster.
	// Not stored in selectedEngines so default changes take effect on next prompt.
	if h.defaultLogic != "" {
		if eng := h.resolveEngineForModel(h.defaultLogic); eng != nil {
			h.llamaEngine = eng
		}
	}

	// Library-mode path: always use orchestrator + ephemeral sub-agents
	if h.llamaEngine != nil && h.llamaEngine.IsLoaded() {
		h.promptWithOrchestrator(w, r, *req, projectPath, projectRes.SSHInfo)
		return
	}

	// Try to bootstrap from settings.json (cloud providers)
	if eng := h.engineFromSettings("openrouter", "deepseek/deepseek-v4-flash", 0); eng != nil {
		h.llamaEngine = eng
		h.selectedEngines[key] = eng
		h.promptWithOrchestrator(w, r, *req, projectPath, projectRes.SSHInfo)
		return
	}

	// No engine available
	errMsg := "Agent engine not available. Start llama-server or configure a cloud provider."
	// Persist error as assistant message so it survives session reload
	if h.db != nil {
		h.db.SaveAssistantMessage(r.Context(), req.Project, req.AgentId, "Error: "+errMsg, "", "[]")
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"success": false,
		"error":   errMsg,
	})
}

// writeLlamaSSE converts a llama engine event to SSE and writes it.
func (h *PuxHandler) writeLlamaSSE(w http.ResponseWriter, evt llamaeng.AgentEvent, canFlush bool, flusher http.Flusher) {
	sseEvt := mapEventToSSE(evt)
	if sseEvt == nil {
		return
	}

	// Ensure tool events have IDs
	if sseEvt.Type == "tool_execution_start" {
		if dataMap, ok := sseEvt.Data.(map[string]any); ok {
			tid, _ := dataMap["toolId"].(string)
			if tid == "" {
				dataMap["toolId"] = nextToolFallbackId()
			}
		}
	}

	writeSSE(w, sseEvt.Type, sseEvt.Data, canFlush, flusher)
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
	req, ok := decodeReq[struct {
		Tool   string `json:"tool"`
		Level  string `json:"level"` // "auto", "confirm", "deny"
		Reason string `json:"reason,omitempty"`
	}](w, r)
	if !ok { return }
	if req.Tool == "" || req.Level == "" {
		JSONError(w, "tool and level are required", http.StatusBadRequest)
		return
	}

	h.toolPerms.SetPermission(req.Tool, perms.PermissionLevel(req.Level), req.Reason)
	h.log.Info("Tool permission updated",
		zap.String("tool", req.Tool),
		zap.String("level", req.Level),
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"tool":    req.Tool,
		"level":   req.Level,
	})
}

// GetBashRules returns all user-defined bash command rules.
// GET /api/pux/bash-rules
func (h *PuxHandler) GetBashRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.bashRules.AllRules())
}

// AddBashRule creates a new user-defined bash command rule.
// POST /api/pux/bash-rules
func (h *PuxHandler) AddBashRule(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Pattern string `json:"pattern"`
		Level   string `json:"level"`
	}](w, r)
	if !ok {
		return
	}
	if req.Pattern == "" || req.Level == "" {
		JSONError(w, "pattern and level are required", http.StatusBadRequest)
		return
	}

	rule, err := h.bashRules.AddRule(req.Pattern, perms.PermissionLevel(req.Level))
	if err != nil {
		JSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.log.Info("Bash command rule added", zap.String("pattern", req.Pattern), zap.String("level", req.Level))
	writeJSON(w, http.StatusOK, rule)
}

// RemoveBashRule deletes a user-defined bash command rule by ID.
// DELETE /api/pux/bash-rules/{id}
func (h *PuxHandler) RemoveBashRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		JSONError(w, "rule id is required", http.StatusBadRequest)
		return
	}
	if !h.bashRules.RemoveRule(id) {
		JSONError(w, "rule not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// GetBashSystemRules returns a summary of built-in system bash command rules.
// GET /api/pux/bash-system-rules
func (h *PuxHandler) GetBashSystemRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, bash.GetSystemRulesSummary())
}

// Compact triggers a manual context compaction for the given agent session.
// Performs LLM summarization on the session JSONL file, replacing old messages
// with a structured summary. Falls back to micro-compact (tool result truncation)
// when no LLM engine is available.
func (h *PuxHandler) Compact(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		AgentID string `json:"agentId"`
	}](w, r)
	if !ok { return }
	if req.Project == "" {
		req.Project = "default"
	}
	if req.AgentID == "" {
		req.AgentID = "default"
	}

	// Run compaction on the JSONL session file
	result := h.compactSession(r.Context(), req.Project, req.AgentID)

	// Also trim the database for consistency
	if h.db != nil {
		if _, err := h.db.CompactSession(r.Context(), req.Project, req.AgentID); err != nil {
			h.log.Warn("db compact failed (non-fatal)", zap.String("project", req.Project), zap.Error(err))
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// SetHookBridge sets the SSE hook bridge for TUI interception.
func (h *PuxHandler) SetHookBridge(bridge *hooks.SSEHookBridge) {
	h.hookBridge = bridge
}

// HookResponse handles POST /api/pux/hook-response — the TUI's response to a hook_request.
func (h *PuxHandler) HookResponse(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		HookID string         `json:"hookId"`
		Action string         `json:"action"` // "allow", "block", "modify"
		Data   map[string]any `json:"data,omitempty"`
		Reason string         `json:"reason,omitempty"`
	}](w, r)
	if !ok { return }
	if req.HookID == "" || req.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hookId and action are required"})
		return
	}

	if h.hookBridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no hook bridge active"})
		return
	}

	resp := hooks.HookResponse{
		Action:       req.Action,
		ModifiedData: req.Data,
		Reason:       req.Reason,
	}
	if ok := h.hookBridge.Respond(req.HookID, resp); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no pending hook with that ID"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetMCPServers handles GET /api/pux/mcp-servers — returns registered MCP server info.
func (h *PuxHandler) GetMCPServers(w http.ResponseWriter, r *http.Request) {
	if h.mcpMulti == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, h.mcpMulti.ServersInfo())
}

// GetTree handles GET /api/pux/tree — returns the session tree for navigation.
func (h *PuxHandler) GetTree(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentID := r.URL.Query().Get("agentId")
	if project == "" {
		project = "default"
	}
	if agentID == "" {
		agentID = "default"
	}

	// Load existing session file
	sessionPath := fmt.Sprintf("%s/.pux/sessions/%s.jsonl", project, agentID)
	tree, err := session.Load(sessionPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	defer tree.Close()

	// Serialize tree nodes
	root := tree.GetTree()
	nodes := serializeTree(root)

	writeJSON(w, http.StatusOK, map[string]any{
		"sessionId":   tree.ID(),
		"currentNode": tree.GetCurrentNode(),
		"nodes":       nodes,
	})
}

// Fork handles POST /api/pux/fork — forks the session at a given node.
func (h *PuxHandler) Fork(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project string `json:"project"`
		AgentID string `json:"agentId"`
		NodeID  string `json:"nodeId"`
		Label   string `json:"label,omitempty"`
	}](w, r)
	if !ok { return }
	if req.Project == "" {
		req.Project = "default"
	}
	if req.AgentID == "" {
		req.AgentID = "default"
	}
	if req.NodeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "nodeId is required"})
		return
	}

	sessionPath := fmt.Sprintf("%s/.pux/sessions/%s.jsonl", req.Project, req.AgentID)
	tree, err := session.Load(sessionPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	defer tree.Close()

	forked, err := tree.Fork(req.NodeID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer forked.Close()

	forkPath := forked.(*session.SessionTree).FilePath()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"forkPath": forkPath,
		"forkId":   forked.ID(),
	})
}

// serializeTree converts a TreeNode tree into a flat list for JSON serialization.
func serializeTree(node *core.TreeNode) []map[string]any {
	if node == nil {
		return nil
	}
	var result []map[string]any

	var walk func(n *core.TreeNode)
	walk = func(n *core.TreeNode) {
		entry := map[string]any{
			"id":        n.Entry.ID,
			"parentId":  n.Entry.ParentID,
			"type":      string(n.Entry.Type),
			"timestamp": n.Entry.Timestamp,
			"label":     n.Entry.Label,
		}
		// Extract message preview
		if n.Entry.Data != nil {
			var msg core.Message
			if json.Unmarshal(n.Entry.Data, &msg) == nil {
				preview := msg.Content
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				entry["role"] = msg.Role
				entry["preview"] = preview
			}
		}
		result = append(result, entry)
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(node)
	return result
}

// Feedback accepts user feedback (positive/negative) on assistant messages.
// Stores it in the event store for observability (Langfuse, metrics).
func (h *PuxHandler) Feedback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MessageID string `json:"messageId"`
		Type      string `json:"type"` // "positive" or "negative"
		Role      string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.MessageID == "" || (req.Type != "positive" && req.Type != "negative") {
		http.Error(w, "messageId and type (positive/negative) required", http.StatusBadRequest)
		return
	}

	h.log.Info("feedback received",
		zap.String("messageId", req.MessageID),
		zap.String("type", req.Type),
		zap.String("role", req.Role),
	)

	// TODO: wire to event store, metrics, and Langfuse when those APIs stabilize

	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// Suggestions returns contextual follow-up suggestions based on recent messages.
// Falls back to defaults when no conversation context is available.
func (h *PuxHandler) Suggestions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Build contextual suggestions from the last assistant message
	suggestions := h.generateSuggestions(req.Messages)

	writeJSON(w, http.StatusOK, map[string]any{
		"suggestions": suggestions,
	})
}

func (h *PuxHandler) generateSuggestions(messages []map[string]any) []string {
	// Look at the last assistant message for context
	var lastAssistantContent string
	for i := len(messages) - 1; i >= 0; i-- {
		if role, ok := messages[i]["role"].(string); ok && role == "assistant" {
			if content, ok := messages[i]["content"].(string); ok {
				lastAssistantContent = content
				break
			}
		}
	}

	// Context-aware suggestions based on what the assistant just did
	if strings.Contains(strings.ToLower(lastAssistantContent), "error") || strings.Contains(strings.ToLower(lastAssistantContent), "failed") {
		return []string{
			"What went wrong? Can you fix it?",
			"Show me the error details",
			"Try a different approach",
		}
	}
	if strings.Contains(strings.ToLower(lastAssistantContent), "test") {
		return []string{
			"Show me the test results",
			"Run the failing tests again",
			"Fix the test failures",
		}
	}
	if strings.Contains(strings.ToLower(lastAssistantContent), "file") || strings.Contains(strings.ToLower(lastAssistantContent), "wrote") {
		return []string{
			"Show me what changed",
			"Run the tests",
			"Review the diff",
		}
	}

	// Default contextual suggestions
	return []string{
		"What's next?",
		"Show me what changed",
		"Run the tests",
	}
}

// BackgroundTask sends a foreground task to the background.
// POST /api/pux/tasks/{taskID}/background
func (h *PuxHandler) BackgroundTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "missing taskID"})
		return
	}

	if err := h.taskMgr.Background(taskID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"taskId":  taskID,
		"status":  "backgrounded",
	})
}

// ListTasks returns all background tasks.
// GET /api/pux/tasks
func (h *PuxHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := h.taskMgr.List()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	})
}

// GetTaskStatus returns the status of a specific task.
// GET /api/pux/tasks/{taskID}
func (h *PuxHandler) GetTaskStatus(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if taskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"error": "missing taskID"})
		return
	}

	status, err := h.taskMgr.Status(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, status)
}
