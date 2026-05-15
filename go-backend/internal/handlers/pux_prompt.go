package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/adapters"
	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/agents/orchestrator"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	llama "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/llm"
	"github.com/auto-developer-orchestrator/backend/internal/observability"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/util"
	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
	"github.com/auto-developer-orchestrator/backend/internal/tools/plan"
	"github.com/auto-developer-orchestrator/backend/internal/vision"
	"go.uber.org/zap"
)

// streamerVisualContext adapts a ComputerUseHandler + sandbox ID to the
// vision.VisualContext interface. It reads the streamer's last frame change score.
type streamerVisualContext struct {
	cu        *ComputerUseHandler
	sandboxID string
}

func (vc *streamerVisualContext) LastChangeScore() float64 {
	vc.cu.mu.RLock()
	client, ok := vc.cu.clients[vc.sandboxID]
	vc.cu.mu.RUnlock()
	if !ok {
		return -1
	}
	streamer := client.GetStreamer()
	if streamer == nil {
		return -1
	}
	frame := streamer.LastFrame()
	if frame == nil {
		return -1
	}
	return frame.ChangeScore
}

// promptWithOrchestrator handles prompt requests using the orchestrator agent.
// This is the default and only path for /api/pux/prompt.
func (h *PuxHandler) promptWithOrchestrator(w http.ResponseWriter, r *http.Request, req promptRequest, projectPath string) {
	key := compositeAgentKey(projectPath, req.AgentId)

	// Resolve sandbox — find existing or auto-create
	// Sanitize sandbox ID: Docker requires [a-zA-Z0-9][a-zA-Z0-9_.-]
	sandboxID := strings.ReplaceAll(req.Project, "/", "-")
	sandboxID = strings.ReplaceAll(sandboxID, "_", "-")
	sandboxID = strings.Trim(sandboxID, "-")
	if h.sandboxMgr != nil {
		if sb := h.sandboxMgr.FindSandboxByProject(projectPath); sb != nil {
			sandboxID = sb.ID
		} else {
			// No sandbox for this project — auto-create one
			sb, err := h.sandboxMgr.CreateSandbox(r.Context(), sandbox.SandboxOptions{
				ID:          sandboxID,
				ProjectPath: projectPath,
				InitialMode: sandbox.ModeBrowser,
			})
			if err != nil {
				// Fail fast: no sandbox = every tool call will fail.
				// Return error immediately instead of proceeding to broken execution.
				h.log.Error("Failed to auto-create sandbox — cannot execute tools", zap.Error(err))
				setSSEHeaders(w)
				flusher, canFlush := w.(http.Flusher)
				writeSSE(w, "error", map[string]string{"error": fmt.Sprintf("Sandbox unavailable: %s. Start Docker or run 'task dev' first.", err)}, canFlush, flusher)
				writeSSE(w, "done", map[string]bool{"done": true}, canFlush, flusher)
				return
			}
			sandboxID = sb.ID
			h.log.Info("Auto-created sandbox for prompt",
				zap.String("project", req.Project),
				zap.String("sandbox_id", sb.ID))
		}
	}

	// Build provider adapter — priority chain: local → cluster → gemini → openrouter
	engine := h.llamaEngine
	if engine == nil {
		engine = h.clusterEngine
	}
	if sel, ok := h.selectedEngines[key]; ok {
		engine = sel
	}
	provider := llm.NewAdapter(engine, 0)
	defer provider.Close()

	// Build infrastructure adapters (shared with scheduler)
	var bashExec adapters.BashExecutor
	var fileOps adapters.FileOps
	if h.sandboxMgr != nil {
		bashExec = adapters.BashExecutor{Mgr: h.sandboxMgr, SandboxID: sandboxID}
		fileOps = adapters.FileOps{Mgr: h.sandboxMgr, SandboxID: sandboxID}
	}

	// Project memory (MEMORY.md)
	memStore := memory.NewProjectMemory(projectPath)

	// Credential store for secret resolution in tools
	credStore := sensitive.NewStore()
	if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
		credStore.Set("github", "token", ghToken)
	}

	// Approval handler via unified decision registry (Decision endpoint)
	approvalHandler := &adapters.ApprovalHandler{Registry: core.GlobalDecisions}

	// Use per-agentId session path for history continuity.
	// Sessions live under ~/.pi/agent/sessions/ (always user-writable),
	// not inside the project directory (which may have restricted perms).
	home, _ := os.UserHomeDir()
	sessionDir := filepath.Join(home, ".pi", "agent", "sessions", sandboxID)
	sessionPath := filepath.Join(sessionDir, req.AgentId+".jsonl")

	// Create event channel early so the ask_user tool can emit events to the TUI
	events := make(chan core.AgentEvent, 256)

	cfg := orchestrator.Config{
		ProjectDir:    projectPath,
		SandboxID:     sandboxID,
		SessionPath:   sessionPath,
		ContextSize:   32768,
		MaxToolRounds: 50,
		WorkDir:       "/sandbox",
		BashExecutor:  &bashExec,
		FileOps:       &fileOps,
		MemoryStore:   memStore,
		ApprovalHandler: approvalHandler,
		GitExecutor:     nil, // disabled — git auto-commits on session start are unwanted
		ArtifactDB:      h.db,
		BrowserProvider: h.cuBridge, // wire accessibility/cookie/storage tools to employees
		DesktopProvider: h.cuBridge, // wire desktop screenshot/click/type/key tools to employees
	}

	// Wire visual context for frame-based vision caching (skips vision API when page hasn't changed)
	if h.cuBridge != nil && sandboxID != "" {
		cfg.VisualContext = &streamerVisualContext{cu: h.cuBridge.CU, sandboxID: sandboxID}
	}

	// Detect org mode — explicit org path takes priority, then check project dir
	orgPath := ""
	if req.Org != "" {
		orgPath = req.Org
	} else if org := common.LoadOrgManifest(projectPath); org != nil {
		orgPath = projectPath
		_ = orgPath // loaded below
	}

	if orgPath != "" {
		org := common.LoadOrgManifest(orgPath)
		if org != nil {
			h.log.Info("Org mode detected",
				zap.String("org", org.Name),
				zap.String("orgPath", orgPath),
				zap.String("projectPath", projectPath),
			)
			cfg.Org = org

			// Merge org tool packages into kernel cache BEFORE loading roles,
			// so that role imports like tech_noir_art, godot, comfyui resolve
			if org.ToolPkgsDir() != "" {
				common.MergeToolPackages(org.ToolPkgsDir())
				h.log.Info("Org tool packages merged", zap.String("dir", org.ToolPkgsDir()))
			}

			if org.RolesDir() != "" {
				cfg.OrgRoles = common.LoadAgentRolesFrom(org.RolesDir())
				h.log.Info("Org roles loaded", zap.Int("count", len(cfg.OrgRoles)))
			}
			// Create DBProvider from org databases config (Neo4j, Postgres, CompreFace)
			if len(org.Databases) > 0 {
				dbProvider := common.NewOrgDBProvider(org.Databases)
				cfg.DBProvider = dbProvider
				h.log.Info("DBProvider created from org config",
					zap.Int("databases", len(org.Databases)))
			}
		}
	}

	// Wire add-on hooks (Langfuse tracing, etc.)
	var extraHooks []core.LoopHook
	if h.langfuse != nil && h.langfuse.Enabled() {
		modelName := ""
		if engine != nil {
			modelName = engine.ModelName()
		}
		traceCfg := observability.TraceConfig{
			UserID:    req.AgentId,
			Project:   req.Project,
			ModelName: modelName,
			SandboxID: sandboxID,
			Message:   util.Truncate(req.Message, 200),
			Tags:      observability.ClassifyTags(req.Message),
			Release:   h.langfuse.Release(),
			Env:       h.langfuse.Environment(),
		}
		extraHooks = append(extraHooks, observability.NewLangfuseHook(h.langfuse, modelName, traceCfg))
		h.log.Info("Langfuse tracing hook wired", zap.String("model", modelName), zap.Strings("tags", traceCfg.Tags))
	}
	cfg.ExtraHooks = extraHooks

	// Build vision fallback chain — used when the engine lacks native vision.
	// Priority: MCP (Florence-2 on cluster, fast) → Native (llama.cpp/LLM vision, flexible).
	// When the LLM can see images (mmproj/Gemini), skip to avoid redundant descriptions.
	var visionChain *vision.FallbackChain
	engineHasVision := false
	if engine != nil {
		engineHasVision = engine.HasVision()
	}
	if !engineHasVision {
		var providers []vision.Provider

		// Tier 1: MCP media analysis server (Florence-2, fast structured descriptions)
		if h.mcpMulti != nil && h.mcpMulti.HasTool("analyze_image") {
			providers = append(providers, vision.NewMCPProvider(h.mcpMulti))
		}

		// Tier 2: Native local llama.cpp vision (flexible, handles complex scenes)
		if h.visionClient != nil {
			vc := h.visionClient
			providers = append(providers, vision.NewNativeProvider(vision.NativeProviderOpt{
				DescribeFunc: func(ctx context.Context, b64, mimeType, prompt string) (string, error) {
					imgBytes, err := base64.StdEncoding.DecodeString(b64)
					if err != nil {
						return "", fmt.Errorf("decode base64: %w", err)
					}
					return vc.DescribeImage(ctx, imgBytes, prompt, mimeType)
				},
				HealthCheck: func(ctx context.Context) bool {
					return vc.CheckHealth(ctx)
				},
			}))
		}

		if len(providers) > 0 {
			visionChain = vision.NewFallbackChain(providers...)
			h.log.Info("Vision fallback chain configured",
				zap.Int("providers", len(providers)),
				zap.String("engine", engine.ModelName()),
			)
		}
	} else if engineHasVision {
		h.log.Debug("LLM has native vision, skipping fallback chain", zap.String("engine", engine.ModelName()))
	}
	cfg.VisionChain = visionChain
	cfg.MCPClient = h.mcpMulti
	cfg.Subscriber = events // ask_user tool emits to TUI via this channel
	cfg.Scheduler = h.schedulerTool // scheduler tool for LLM

	// Model resolver — lets sub-agents use role-specific models
	cfg.ModelResolver = func(modelID string) core.LLMProvider {
		eng := h.resolveEngineForModel(modelID)
		if eng == nil {
			return nil
		}
		return llm.NewAdapter(eng, 0)
	}

	// ProviderFactory — creates isolated providers per sub-agent.
	// Each sub-agent gets its own Adapter → own session → own llama-server slot.
	// This prevents KV cache thrashing and enables true concurrent execution.
	cfg.ProviderFactory = func() core.LLMProvider {
		return llm.NewAdapter(engine, 0)
	}

	orch, err := orchestrator.New(provider, cfg)
	if err != nil {
		h.log.Error("Failed to create orchestrator", zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   "Failed to create orchestrator: " + err.Error(),
		})
		return
	}
	defer orch.Close()

	// Rehydrate session tree from SQL history for context continuity
	var historyLen int
	if h.db != nil {
		history, err := h.db.GetConversationHistory(r.Context(), req.Project, req.AgentId, 200)
		if err != nil {
			h.log.Warn("Failed to load conversation history", zap.Error(err))
		} else {
			historyLen = len(history)
			for _, stored := range history {
				msg := core.Message{Role: stored.Role}
				switch stored.Role {
				case "user":
					msg.Content = stored.Content
				case "assistant":
					msg.Content = stored.Text
					msg.ReasoningContent = stored.Thinking
				}
				if err := orch.Session.AppendMessage(msg); err != nil {
					h.log.Warn("Failed to append history message", zap.Error(err))
				}
			}
			if historyLen > 0 {
				h.log.Info("Session rehydrated from SQL history",
					zap.String("agentId", req.AgentId),
					zap.Int("messages", historyLen))
			}
		}
	}

	// Auto-title conversation from first user message
	if h.db != nil && historyLen == 0 {
		title := req.Message
		if len(title) > 60 {
			title = title[:60] + "..."
		}
		if err := h.db.SetConversationTitle(r.Context(), req.Project, req.AgentId, title); err != nil {
			h.log.Warn("Failed to set auto-title", zap.Error(err))
		}
	}

	// Inject project memory prefix into the message
	var memoryPrefix string
	if mem := orch.Memory; mem != nil {
		memoryPrefix = mem.InjectPrefix()
	} else {
		memoryPrefix = memory.ReadMemoryFile(projectPath)
		if memoryPrefix != "" {
			memoryPrefix = "<memory>\n" + memoryPrefix + "\n</memory>\n\n"
		}
	}

	// Read AGENTS.md from project root (like Claude Code's CLAUDE.md)
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
		if _, err := h.db.SaveUserMessage(r.Context(), req.Project, req.AgentId, req.Message); err != nil {
			h.log.Warn("Failed to save user message", zap.Error(err))
		}
	}

	// Inject active plan if one exists (survives context compaction)
	planPrefix := plan.InjectActivePlan(projectPath)

	orchMsg := agentsMDPrefix + memoryPrefix + planPrefix + "User request: " + req.Message

	// Event channel (created earlier in function, passed to orchestrator Config)

	// Detached context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	var loopErr error

	go func() {
		defer close(done)
		defer close(events)
		loopErr = orch.Run(ctx, orchMsg, events)
	}()

	// Stream events
	var assistantText, assistantThinking string
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	llamaEvents := make(chan llama.AgentEvent, 256)
	go func() {
		defer close(llamaEvents)
		for evt := range events {
			switch evt.Type {
			case core.EventTypeTextDelta:
				assistantText += evt.Data.Text
			case core.EventTypeThinkingDelta:
				assistantThinking += evt.Data.Text
			}
			llamaEvents <- convertCoreEventToLlama(evt)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			for evt := range llamaEvents {
				h.writeLlamaSSE(w, evt, canFlush, flusher)
			}
			if h.db != nil {
				if _, err := h.db.SaveAssistantMessage(ctx, req.Project, req.AgentId, assistantText, assistantThinking, "[]"); err != nil {
					h.log.Warn("Failed to save assistant message", zap.Error(err))
				}
			}
			if loopErr != nil {
				h.log.Error("Orchestrator error", zap.Error(loopErr))
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if canFlush {
				flusher.Flush()
			}
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			if canFlush {
				flusher.Flush()
			}
		case evt, ok := <-llamaEvents:
			if !ok {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				if canFlush {
					flusher.Flush()
				}
				return
			}
			keepalive.Reset(15 * time.Second)
			h.writeLlamaSSE(w, evt, canFlush, flusher)
		}
	}
}

// readAgentsMD reads AGENTS.md from the project root (like Claude Code's CLAUDE.md).
// Returns empty string if the file doesn't exist.
func readAgentsMD(projectPath string) string {
	data, err := os.ReadFile(filepath.Join(projectPath, "AGENTS.md"))
	if err != nil {
		return ""
	}
	return string(data)
}
