package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/adapters"
	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/agents/orchestrator"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/llm"
	"github.com/auto-developer-orchestrator/backend/internal/observability"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/util"
	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
	"github.com/auto-developer-orchestrator/backend/internal/tools/file"
	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
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
				// Docker unavailable — continue in host-only mode.
				// CTO tools use HostBash/HostFileOps (host filesystem).
				// Sub-agent delegation will fail, but CTO-only tasks work fine.
				h.log.Warn("Sandbox creation failed — running in host-only mode",
					zap.Error(err),
					zap.String("project", req.Project))
				sandboxID = "" // no sandbox
			} else {
				sandboxID = sb.ID
				h.log.Info("Auto-created sandbox for prompt",
					zap.String("project", req.Project),
					zap.String("sandbox_id", sb.ID))
			}
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

	// Host executor — CTO reads/writes directly on the host filesystem.
	// Sub-agents with isolated/bridged sandbox tiers keep using the Docker sandbox.
	hostBash := &adapters.HostExecutor{WorkDir: projectPath}
	hostFileOps := &file.SimpleSandboxOps{BasePath: projectPath}

	// Project memory (MEMORY.md — legacy, still supported)
	memStore := memory.NewProjectMemory(projectPath)

	// Folder-based memory (.pux/memory/) — uses host executor so files land on host
	memFolder := memory.NewFolderStore(projectPath, hostBash)

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
		HostBash:      hostBash,
		HostFileOps:   hostFileOps,
		MemoryStore:   memStore,
		MemoryFolder:  memFolder,
		ApprovalHandler: approvalHandler,
		GitExecutor:     nil, // disabled — git auto-commits on session start are unwanted
		ArtifactDB:      h.db,
		TranscriptDB:    h.db,
		Project:         req.Project,
		AgentID:         req.Project + ":" + req.AgentId, // composite key for scratch note persistence
		BrowserProvider: h.cuBridge, // wire accessibility/cookie/storage tools to employees
		DesktopProvider: h.cuBridge, // wire desktop screenshot/click/type/key tools to employees
		ToolPerms:       h.toolPerms, // wire per-tool permission checks
		SandboxOnly:     req.SandboxOnly, // scheduled jobs: restrict to bash/file ops only
		TaskMgr:        h.taskMgr,       // background task support for bash commands
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

	// Build vision fallback chain — always configured as fallback when native vision is
	// unavailable or fails. Priority: phi4 (capable) → MCP Florence-2 (fast) → Native llama.cpp.
	// The VisionAwareExecutor uses native image_url when EngineHasVision=true,
	// and falls back to text description via this chain when false.
	var visionChain *vision.FallbackChain
	engineHasVision := false
	if engine != nil {
		engineHasVision = engine.HasVision()
	}
	{
		var providers []vision.Provider

		// Tier 1: phi4_vision (high quality, good for browser/desktop screenshots)
		if h.mcpMulti != nil && h.mcpMulti.HasTool("phi4_vision") {
			providers = append(providers, vision.NewPhi4Provider(h.mcpMulti, h.imageServer))
		}

		// Tier 2: MCP Florence-2 (fast, structured descriptions)
		if h.mcpMulti != nil && h.mcpMulti.HasTool("analyze_image") {
			providers = append(providers, vision.NewMCPProvider(h.mcpMulti, h.imageServer))
		}

		// Tier 3: Native local llama.cpp vision (flexible, handles complex scenes)
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
				zap.Bool("nativeVision", engineHasVision),
				zap.String("engine", func() string {
					if engine != nil {
						return engine.ModelName()
					}
					return "none"
				}()),
			)
		}
	}
	cfg.VisionChain = visionChain
	cfg.EngineHasVision = engineHasVision
	cfg.MCPClient = h.mcpMulti
	cfg.Subscriber = events // ask_user tool emits to TUI via this channel
	cfg.Scheduler = h.schedulerTool // scheduler tool for LLM

	// Wire TaskManager subscriber so background task events flow to SSE
	h.taskMgr.SetSubscriber(events)

	// Model resolver — lets sub-agents use role-specific models.
	// Special model IDs:
	//   "__logic__" → use the logic default
	//   ""          → let ProviderFactory handle it (worker default → CTO's engine)
	cfg.ModelResolver = func(modelID string) core.LLMProvider {
		if modelID == "__logic__" && h.defaultLogic != "" {
			modelID = h.defaultLogic
		} else if modelID == "" {
			return nil
		}
		eng := h.resolveEngineForModel(modelID)
		if eng == nil {
			return nil
		}
		return llm.NewAdapter(eng, 0)
	}

	// ProviderFactory — creates isolated providers per sub-agent.
	// Each sub-agent gets its own Adapter → own session → own llama-server slot.
	// This prevents KV cache thrashing and enables true concurrent execution.
	// Uses the worker default model if set, falling back to the CTO's engine.
	cfg.ProviderFactory = func() core.LLMProvider {
		if h.defaultWorker != "" {
			if eng := h.resolveEngineForModel(h.defaultWorker); eng != nil {
				return llm.NewAdapter(eng, 0)
			}
		}
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

	h.rehydrateAndStream(w, r, orch, events, req, projectPath, memFolder)
}

// readAgentsMD reads AGENTS.md from the project root (like Claude Code's CLAUDE.md).
// readAgentsMD reads AGENTS.md from the project root (like Claude Code's CLAUDE.md).
// Returns empty string if the file doesn't exist.
func readAgentsMD(projectPath string) string {
	data, err := os.ReadFile(filepath.Join(projectPath, "AGENTS.md"))
	if err != nil {
		return ""
	}
	return string(data)
}
