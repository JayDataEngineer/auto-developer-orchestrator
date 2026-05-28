package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/auto-developer-orchestrator/backend/internal/agents"
	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/adapters"
	"github.com/auto-developer-orchestrator/backend/internal/checkpoint"
	"github.com/auto-developer-orchestrator/backend/internal/autoconfig"
	ctxpkg "github.com/auto-developer-orchestrator/backend/internal/context"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/hooks"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/profiles"
	"github.com/auto-developer-orchestrator/backend/internal/session"
	"github.com/auto-developer-orchestrator/backend/internal/skills"
	appprofile "github.com/auto-developer-orchestrator/backend/internal/tools/appprofile"
	browsertools "github.com/auto-developer-orchestrator/backend/internal/tools/browser"
	desktoptools "github.com/auto-developer-orchestrator/backend/internal/tools/desktop"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	asktool "github.com/auto-developer-orchestrator/backend/internal/tools/ask"
	"github.com/auto-developer-orchestrator/backend/internal/tools/file"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
	"github.com/auto-developer-orchestrator/backend/internal/tools/graph"
	mcptools "github.com/auto-developer-orchestrator/backend/internal/tools/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/tools/eval"
	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
	"github.com/auto-developer-orchestrator/backend/internal/tools/python"
	"github.com/auto-developer-orchestrator/backend/internal/tools/meta"
	_ "github.com/auto-developer-orchestrator/backend/internal/tools/plan" // plan tool: removed from CTO, kept for re-enable
	schedulertool "github.com/auto-developer-orchestrator/backend/internal/tools/scheduler"
	"github.com/auto-developer-orchestrator/backend/internal/tools/todo"
	"github.com/auto-developer-orchestrator/backend/internal/tools/orchestration"
	"github.com/auto-developer-orchestrator/backend/internal/perms"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"github.com/auto-developer-orchestrator/backend/internal/vision"
)

// 	Config holds configuration for the orchestrator agent.
type Config struct {
	ProjectDir      string
	SandboxID       string
	ContextSize     int
	MaxToolRounds   int
	SessionPath     string
	WorkDir         string
	MemoryStore     *memory.Store
	MemoryFolder    *memory.FolderStore
	BashExecutor    bash.Executor   // sandbox executor — used by sub-agents (isolated/bridged)
	FileOps         file.SandboxFileOps // sandbox file ops — used by sub-agents
	HostBash        bash.Executor   // host executor — CTO reads/writes directly on host
	HostFileOps     file.SandboxFileOps // host file ops — CTO file tools
	DelegateRunner   orchestration.DelegateRunner
	ProviderFactory  orchestration.ProviderFactory  // creates isolated providers per sub-agent (own session/slot)
	Skills           *skills.Store
	ApprovalHandler  hooks.ApprovalHandler     // optional: if set, create_plan requires user approval
	GitExecutor      hooks.GitExecutor         // optional: if set, git checkpoints are created
	ExtraHooks       []core.LoopHook           // optional: add-on hooks (Langfuse, etc.)
	VisionChain      *vision.FallbackChain     // optional: if set, fallback text description for non-vision models
	EngineHasVision  bool                      // true if the LLM supports native image_url
	VisualContext    vision.VisualContext      // optional: if set, enables frame-based vision caching
	MCPClient        *mcp.MultiClient          // optional: if set, registers MCP tools (search, analyze_image, etc.)
	ModelResolver    orchestration.ModelResolver // optional: if set, sub-agents can use role-specific models
	ArtifactDB       meta.ArtifactStore         // optional: if set, yield_artifact persists to DB
	TranscriptDB     *storage.Database          // optional: if set, sub-agent messages persist for transcript retrieval
	Project          string                     // project name for transcript DB storage
	AgentID          string                     // agent identifier for scratch note persistence (e.g., "default" or composite key)
	Org              *common.OrgManifest        // optional: org manifest for overlay mode
	OrgRoles         map[string]*common.AgentRole // optional: org-specific employee roles
	DBProvider       common.DBProvider          // optional: if set, registers graph/face tools for employees
	LLMProvider      core.LLMProvider           // optional: if set, registers NLP tools for employees
	BrowserProvider browsertools.BrowserProvider // optional: if set, registers browser a11y/cookie/storage tools for employees
	DesktopProvider desktoptools.DesktopProvider // optional: if set, registers desktop screenshot/click/type/key tools for employees
	Subscriber      chan<- core.AgentEvent      // optional: if set, ask_user tool can emit events to TUI
	Scheduler       any                         // optional: *scheduler.Scheduler — passed through to scheduler tool
	ToolPerms       *perms.ToolPermissionConfig // optional: if set, enables per-tool permission checks
	BashRules       *perms.BashRuleStore        // optional: if set, enables user-defined bash command rules
	SandboxOnly     bool                        // optional: if true, only bash + file tools available (no delegation, MCP, browser, etc.)
	TaskMgr         *core.TaskManager           // optional: if set, bash tool supports run_in_background + task_output
}

// Agent is the full orchestrator agent with all tools.
type Agent struct {
	*agents.BaseAgent
	Memory   *memory.Store
	config   Config
	logger   *log.Logger
	jitStore *autoconfig.WorkerStore          // session-scoped workers, cleaned up on Close
	runner   *orchestration.ParallelRunner    // nil if external DelegateRunner provided
}

// newBashTool creates a bash tool for the CTO using the host executor.
// The CTO runs commands directly on the host machine — not inside the sandbox.
func newBashTool(cfg Config) core.Tool {
	exec := cfg.HostBash
	if exec == nil {
		exec = cfg.BashExecutor // fallback to sandbox if no host executor
	}
	if cfg.TaskMgr != nil {
		return bash.NewWithTaskManager(exec, cfg.TaskMgr, cfg.ProjectDir)
	}
	return bash.New(exec)
}

// New creates a new orchestrator agent.
func New(provider core.LLMProvider, cfg Config) (*Agent, error) {
	if cfg.SessionPath == "" {
		cfg.SessionPath = fmt.Sprintf("%s/.pux/sessions/orch-%s.jsonl", cfg.ProjectDir, cfg.SandboxID)
	}
	logger := log.Default()

	sess, err := session.New(cfg.SessionPath, cfg.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: failed to create session: %w", err)
	}

	// ── Tool split: CTO tools vs Employee tools ──
	// The CTO (orchestrator) only gets delegation + minimal tools.
	// Browser, desktop, and MCP tools go ONLY to employees via delegate_to.
	// This forces the model to delegate instead of doing work itself.

	// Build media describer from MCP client (nil if MCP unavailable)
	var mediaDescriber file.MediaDescriber
	if cfg.MCPClient != nil {
		mediaDescriber = &file.MCPMediaDescriber{Client: cfg.MCPClient}
	}

	// CTO uses host file ops — reads/writes directly on the host filesystem.
	// Falls back to sandbox ops if no host file ops are configured.
	hostFileOps := cfg.HostFileOps
	if hostFileOps == nil {
		hostFileOps = cfg.FileOps
	}

	ctoTools := []core.Tool{
		newBashTool(cfg),
	}

	// Shared tracker for concurrent modification detection between read and edit
	tracker := file.NewFileReadTracker()
	ctoTools = append(ctoTools, file.NewReadToolWithTracker(hostFileOps, mediaDescriber, tracker))

	// task_output tool — available when TaskManager is wired in
	if cfg.TaskMgr != nil {
		ctoTools = append(ctoTools, bash.NewTaskOutputTool(cfg.TaskMgr))
	}

	ctoTools = append(ctoTools,
		file.NewWriteTool(hostFileOps),
		file.NewEditToolWithTracker(hostFileOps, tracker),
		file.NewGrepTool(hostFileOps),
		file.NewGlobTool(hostFileOps),
		meta.NewWaitTool(),
		meta.NewYieldArtifactToolWithDB(cfg.ArtifactDB, cfg.ProjectDir, cfg.SandboxID),
	)

	// ── Sandbox-only mode: strict isolation ──
	// When enabled, the agent can only run bash + file ops inside its sandbox.
	// No delegation, no MCP, no browser, no desktop, no memory, no skills.
	if cfg.SandboxOnly {
		logger.Printf("SANDBOX-ONLY mode: only bash + file tools available")

		scratchStore := ctxpkg.NewPersistentScratchStore(cfg.TranscriptDB, cfg.AgentID)
		ctoTools = append(ctoTools,
			ctxpkg.NewScratchWriteTool(scratchStore),
			ctxpkg.NewScratchReadTool(scratchStore),
			ctxpkg.NewScratchClearTool(scratchStore),
		)

		ctoToolReg := core.NewToolRegistry(ctoTools)
		ctoToolReg.RegisterCommonAliases()
		ctoToolSpecs := common.ToOpenAITools(ctoToolReg.All())

		maxRounds := cfg.MaxToolRounds
		if maxRounds == 0 {
			maxRounds = 50
		}

		// Build a minimal system prompt — no employee roster, no skills
		systemPrompt := common.BuildOrchestratorPromptV2(ctoToolReg.All(), cfg.SandboxID, "", "", nil, nil)

		baseAgent := agents.NewBaseAgent(agents.BaseConfig{
			Provider:        provider,
			Session:         sess,
			SystemPrompt:    systemPrompt,
			ToolSpecs:       ctoToolSpecs,
			Executor:        core.ToolExecutor(ctoToolReg),
			MaxToolRounds:   maxRounds,
			MaxTokens:       16384,
			ContextSize:     cfg.ContextSize,
			ProjectDir:      cfg.ProjectDir,
			SandboxID:       cfg.SandboxID,
			GenerateOptions: core.GenerateOptions{MaxTokens: 16384, Temperature: 0.7, TopP: 0.95, TopK: 20},
			ScratchStore:    scratchStore,
			PermDecisions:   core.GlobalDecisions,
			ToolPerms:       cfg.ToolPerms,
			BashRules:       cfg.BashRules,
			Logger:          logger,
		})

		return &Agent{
			BaseAgent: baseAgent,
			Memory:    cfg.MemoryStore,
			config:    cfg,
			logger:    logger,
		}, nil
	}

	// Register ask_user tool (Contract 3: no subscriber injection — uses context)
	if cfg.Subscriber != nil {
		ctoTools = append(ctoTools, asktool.NewAskUserTool())

		// Plan tool removed from CTO — delegation-first means no planning step.
		// create_plan was a bottleneck: model calls it, waits for approval,
		// then does the work itself instead of delegating.
		// The plan store is kept for backward compat if re-enabled later.
		_ = filepath.Join(cfg.ProjectDir, ".pux", "plans")
	}

	if cfg.MemoryStore != nil {
		ctoTools = append(ctoTools, memory.NewTool(cfg.MemoryStore))
	}

	// Folder-based memory tool (replaces single-file MEMORY.md)
	if cfg.MemoryFolder != nil {
		ctoTools = append(ctoTools, memory.NewFolderTool(cfg.MemoryFolder))
	}

	// Scheduler tool — via autoconfig contract (ArtifactStore)
	if cfg.Scheduler != nil {
		if backend, ok := cfg.Scheduler.(schedulertool.Backend); ok {
			scheduleStore := autoconfig.NewScheduleStore(backend)
			ctoTools = append(ctoTools, autoconfig.NewScheduleTool(scheduleStore))
		}
	}

	// Todo list — always available (created internally)
	todoStore := todo.NewStore()
	ctoTools = append(ctoTools, todo.NewTool(todoStore))

	// JS eval tool — sandboxed runtime for deterministic transforms
	ctoTools = append(ctoTools, eval.NewEvalTool())

	// Python tool — subprocess execution with timeout
	ctoTools = append(ctoTools, python.NewPythonTool())

	// Register skills (auto-load from standard paths if not provided)
	skillStore := cfg.Skills
	if skillStore == nil {
		home, _ := os.UserHomeDir()
		skillStore = skills.LoadStandard(cfg.ProjectDir, home)
	}
	// Merge org-scoped skills on top of kernel skills
	if cfg.Org != nil {
		if orgSkillsDir := cfg.Org.SkillsDirPath(); orgSkillsDir != "" {
			if skillStore == nil {
				skillStore = skills.NewStore()
			}
			loaded := skillStore.LoadFromDirs([]string{orgSkillsDir})
			if loaded > 0 {
				logger.Printf("Org skills loaded: %d from %s", loaded, orgSkillsDir)
			}
		}
	}
	if skillStore != nil && skillStore.Count() > 0 {
		ctoTools = append(ctoTools, skills.NewReadSkillTool(skillStore))
		logger.Printf("Skills loaded: %d skills discovered", skillStore.Count())
	}

	// ── Context management add-on ──
	ctxConfig := ctxpkg.DefaultConfig()
	ctxConfig.ContextSize = cfg.ContextSize
	if ctxConfig.ContextSize <= 0 {
		ctxConfig.ContextSize = 32768
	}
	ctxConfig.SpillDir = filepath.Join(cfg.ProjectDir, ".pux", "spill", sess.ID())
	ctxConfig.LLMProvider = provider // enables LLM-powered summarization
	// Use the worker model for compaction summaries — cheaper than the CTO model.
	if cfg.ProviderFactory != nil {
		if compactProvider := cfg.ProviderFactory(); compactProvider != nil {
			ctxConfig.CompactProvider = compactProvider
		}
	}
	ctxMgr := ctxpkg.Factory(sess, ctxConfig)

	// Subdirectory hint tracker — discovers AGENTS.md/CLAUDE.md/.cursorrules
	// from directories the agent navigates into via tool calls.
	var hintTracker *ctxpkg.SubdirectoryHintTracker
	if cfg.ProjectDir != "" {
		hintTracker = ctxpkg.NewSubdirectoryHintTracker(cfg.ProjectDir)
	}

	// Scratch store: persist to DB when available so notes survive session resume
	scratchAgentID := cfg.AgentID
	if scratchAgentID == "" && cfg.Project != "" {
		scratchAgentID = cfg.Project + ":default"
	}
	var scratchStore *ctxpkg.ScratchStore
	if cfg.TranscriptDB != nil && scratchAgentID != "" {
		scratchStore = ctxpkg.NewPersistentScratchStore(cfg.TranscriptDB, scratchAgentID)
	} else {
		scratchStore = ctxpkg.NewScratchStore()
	}
	ctoTools = append(ctoTools,
		ctxpkg.NewScratchWriteTool(scratchStore),
		ctxpkg.NewScratchReadTool(scratchStore),
		ctxpkg.NewScratchClearTool(scratchStore),
		ctxpkg.NewLoadSpilledTool(ctxMgr),
		ctxpkg.NewSummarizeTool(ctxMgr, sess, provider),
		ctxpkg.NewContextStatusTool(ctxMgr),
	)

	// ── Employee tools: NOT registered on the CTO ──
	// These are collected into allTools for sub-agent toolSpecs only.
	employeeTools := []core.Tool{}

	if cfg.MCPClient != nil {
		employeeTools = mcptools.RegisterAll(employeeTools, cfg.MCPClient)
		logger.Printf("MCP tools loaded for employees: %d tools", len(employeeTools))
	}

	if cfg.DBProvider != nil {
		employeeTools = graph.RegisterAll(employeeTools, cfg.DBProvider)
		logger.Printf("Graph tools loaded for employees: %d tools", len(employeeTools))
	}

	if cfg.BrowserProvider != nil {
		sandboxIDFn := func() string { return cfg.SandboxID }
		before := len(employeeTools)
		employeeTools = browsertools.RegisterBrowserTools(employeeTools, cfg.BrowserProvider, sandboxIDFn)
		logger.Printf("Browser tools loaded for employees: %d tools (browse_to + a11y/cookie/storage)", len(employeeTools)-before)
	}

	// Application profiles — shared between CTO (manage_profile) and employees (app_profile)
	profileStore := profiles.NewStore(cfg.ProjectDir)
	profileAdapter := autoconfig.NewProfileStore(profileStore)
	ctoTools = append(ctoTools, autoconfig.NewProfileTool(profileAdapter))
	logger.Printf("Profile management tool loaded for CTO: manage_profile")

	if cfg.DesktopProvider != nil {
		sandboxIDFn := func() string { return cfg.SandboxID }
		employeeTools = desktoptools.RegisterDesktopTools(employeeTools, cfg.DesktopProvider, sandboxIDFn)
		logger.Printf("Desktop tools loaded for employees: 5 tools")

		// Application profiles — semantic interaction layer on top of desktop tools
		employeeTools = appprofile.RegisterAll(employeeTools, profileStore, cfg.DesktopProvider, sandboxIDFn)
		logger.Printf("App profile tools loaded for employees: app_interact, app_profile")
	}

	// Build MCP server resolver for role-based delegation
	var mcpResolver orchestration.MCPResolver
	if cfg.MCPClient != nil {
		mcpResolver = func(prefix string) []string {
			return cfg.MCPClient.ServerToolNames(prefix)
		}
	}

	// All tools = CTO tools + employee tools (for sub-agent toolSpecs)
	allTools := append(ctoTools, employeeTools...)
	allToolSpecs := common.ToOpenAITools(allTools)

	// Create executor that has access to ALL tools (sub-agents need them)
	allToolReg := core.NewToolRegistry(allTools)
	allToolReg.RegisterCommonAliases()

	// CTO tool registry only has delegation + minimal tools
	// BUT the executor behind delegate_to uses allToolReg
	ctoToolReg := core.NewToolRegistry(ctoTools)
	ctoToolReg.RegisterCommonAliases()

	// Add delegation tools to CTO
	var runner orchestration.DelegateRunner
	var pr *orchestration.ParallelRunner
	if cfg.DelegateRunner != nil {
		runner = cfg.DelegateRunner
	}

	if runner == nil && provider != nil {
		// Summarize long sub-agent results before returning to CTO.
		// Uses the worker model (via ProviderFactory) for cheaper summarization.
		summarizerProvider := provider
		if cfg.ProviderFactory != nil {
			if wp := cfg.ProviderFactory(); wp != nil {
				summarizerProvider = wp
			}
		}

		// Wire git-based change tracking for delegate_to / delegate_continue / delegate_revert
		// Uses host executor — git operates on the host project directory
		var snapshotter orchestration.Snapshotter
		gitExec := cfg.HostBash
		if gitExec == nil {
			gitExec = cfg.BashExecutor
		}
		if gitExec != nil {
			snapshotter = orchestration.NewGitSnapshotter(gitExec)
		}

		// Auto-director: raise Chrome window for VNC visibility when browser agent starts
		var raiseBrowser func(ctx context.Context)
		if cfg.BashExecutor != nil && cfg.SandboxID != "" {
			bashExec := cfg.BashExecutor
			raiseBrowser = func(ctx context.Context) {
				_, _ = bashExec.Exec(ctx, "DISPLAY=:99 wmctrl -a 'Google Chrome' 2>/dev/null || true")
			}
		}

		// Per-role sandbox tier: native roles use host executor, others use sandbox.
		// When there's no sandbox (Docker down), ALL tiers fall through to host executor.
		// For SSH projects, HostBash/HostFileOps are SSH-backed — use them directly.
		execFactory := func(tier string) core.ToolExecutor {
			if tier == "native" || cfg.SandboxID == "" {
				// Use configured host executor (SSH for remote projects, HostExecutor for local)
				hostExec := cfg.HostBash
				if hostExec == nil {
					hostExec = &adapters.HostExecutor{WorkDir: cfg.ProjectDir}
				}
				hostFileOps := cfg.HostFileOps
				if hostFileOps == nil {
					hostFileOps = &file.SimpleSandboxOps{BasePath: cfg.ProjectDir}
				}
				nativeTracker := file.NewFileReadTracker()
				hostReg := core.NewToolRegistry([]core.Tool{
					bash.New(hostExec),
					file.NewReadToolWithTracker(hostFileOps, mediaDescriber, nativeTracker),
					file.NewWriteTool(hostFileOps),
					file.NewEditToolWithTracker(hostFileOps, nativeTracker),
					file.NewGrepTool(hostFileOps),
					file.NewGlobTool(hostFileOps),
					python.NewPythonTool(python.WithWorkDir(cfg.ProjectDir)),
					eval.NewEvalTool(),
				})
				hostReg.RegisterCommonAliases()
				return hostReg
			}
			// isolated + bridged both use the sandbox executor
			return allToolReg
		}

		var visionLogger func(string, ...interface{})
		if cfg.VisualContext != nil {
			visionLogger = func(format string, args ...interface{}) {
				logger.Printf("SUB_VISION: "+format, args...)
			}
		}

		home, _ := os.UserHomeDir()

		pr = orchestration.NewParallelRunner(orchestration.RunnerConfig{
			ProviderFactory:    cfg.ProviderFactory,
			ToolSpecs:          allToolSpecs,
			Executor:           allToolReg,
			BaseSession:        sess,
			ContextSize:        cfg.ContextSize,
			ModelResolver:      cfg.ModelResolver,
			Logger:             func(format string, args ...interface{}) { logger.Printf("PARALLEL_RUNNER: "+format, args...) },
			OrchestratorFactory: makeOrchestratorFactory(provider, cfg),
			ProjectDir:         cfg.ProjectDir,
			Depth:              0,
			Snapshotter:        snapshotter,
			ExecutorFactory:    execFactory,
			VisualContext:      cfg.VisualContext,
			VisionChain:        cfg.VisionChain,
			NativeVision:       cfg.EngineHasVision,
			VisionLogger:       visionLogger,
			RaiseBrowserFunc:   raiseBrowser,
			TaskMgr:            cfg.TaskMgr,
			ScratchStore:       scratchStore,
			DB:                 cfg.TranscriptDB,
			Project:            cfg.Project,
			PermDecisions:      core.GlobalDecisions,
			ToolPerms:          cfg.ToolPerms,
			BashRules:          cfg.BashRules,
			Summarizer: func(ctx context.Context, text string, targetChars int) (string, error) {
				return ctxpkg.SummarizeText(ctx, summarizerProvider, text, targetChars)
			},
			HookDeps: hooks.HookDeps{
				ProjectDir:  cfg.ProjectDir,
				HomeDir:     home,
				GitExecutor: cfg.GitExecutor,
			},
		})
		runner = pr
	}

	// JIT worker store — session-scoped workers, cleaned up on Close
	sessionDir := filepath.Dir(cfg.SessionPath)
	var jitStore *autoconfig.WorkerStore

	if runner != nil {
		// Worker management tool — lets the CTO compose workers from capabilities
		// Persistent store: workers written to project dir
		projectWorkersDir := filepath.Join(cfg.ProjectDir, "workers")
		persistentStore := autoconfig.NewWorkerStore(projectWorkersDir)
		// JIT store: session-scoped workers, cleaned up on session end
		jitStore = autoconfig.NewJITWorkerStore(sessionDir)

		ctoTools = append(ctoTools, autoconfig.NewWorkerTool(persistentStore, jitStore))
		logger.Printf("Worker management tool loaded (persistent: %s, JIT: %s)", projectWorkersDir, jitStore.Dir())

		// Dynamic role provider — merges kernel + org + JIT workers on each call
		roleProvider := func() map[string]*common.AgentRole {
			roles := common.LoadAgentRoles()
			// Merge org roles
			for name, role := range cfg.OrgRoles {
				if _, isKernel := roles[name]; !isKernel {
					roles[name] = role
				}
			}
			// Merge JIT workers from session dir
			jitRoles := common.LoadWorkersFrom(jitStore.Dir())
			for name, role := range jitRoles {
				roles[name] = role
			}
			return roles
		}
		nameProvider := func() []string {
			return common.AgentNames(cfg.OrgRoles)
		}

		// Wire role providers into ParallelRunner for scoped delegation
		if pr != nil {
			pr.SetRoleProviders(roleProvider, mcpResolver)
		}

		ctoTools = append(ctoTools,
			orchestration.NewDelegateToToolDynamic(runner, mcpResolver, roleProvider, nameProvider),
			orchestration.NewDelegateAsyncToolDynamic(runner, mcpResolver, roleProvider, nameProvider),
			orchestration.NewDelegateContinueTool(runner),
			orchestration.NewDelegateRevertTool(runner),
			orchestration.NewCollectResultsTool(runner),
			orchestration.NewSynthesizeTool(),
		)
		ctoToolReg = core.NewToolRegistry(ctoTools)
		ctoToolReg.RegisterCommonAliases()
	}

	maxRounds := cfg.MaxToolRounds
	if maxRounds == 0 {
		maxRounds = 50
	}

	// CTO-specific hooks (common hooks — scratchpad, goal nudge, permission — are wired by BaseAgent)
	var ctoHooks []core.LoopHook
	ctoHooks = append(ctoHooks, hooks.NewJournalCheckpointHook(sess))

	if cfg.GitExecutor != nil {
		ctoHooks = append(ctoHooks, hooks.NewGitCheckpointHook(cfg.GitExecutor))
		logger.Printf("Git checkpoint hook enabled")
	}

	if cfg.ApprovalHandler != nil {
		ctoHooks = append(ctoHooks, hooks.NewApprovalHook(cfg.ApprovalHandler, true, 0))
		logger.Printf("Approval hook enabled (plan-only mode)")
	}

	// File checkpoint hook — auto-backups before file writes/edits/destructive bash
	home2, _ := os.UserHomeDir()
	cpManager := checkpoint.NewManager(sess.ID(), cfg.ProjectDir,
		filepath.Join(home2, ".pi", "agent", "checkpoints", sess.ID()))
	ctoHooks = append(ctoHooks, hooks.NewFileCheckpointHook(cpManager))
	logger.Printf("File checkpoint hook enabled (session %s)", sess.ID())

	ctoHooks = append(ctoHooks, cfg.ExtraHooks...)

	var skillsStr string
	if skillStore.Count() > 0 {
		skillsStr = skillStore.FormatAvailableSkills()
	}
	systemPrompt := common.BuildOrchestratorPromptV2(ctoToolReg.All(), cfg.SandboxID, "", skillsStr, cfg.Org, cfg.OrgRoles)

	ctoToolSpecs := common.ToOpenAITools(ctoToolReg.All())

	toolResultProcessor := func(procCtx context.Context, toolName, toolCallID, result string, toolArgs map[string]any) string {
		processed, err := ctxMgr.ProcessToolResult(procCtx, toolName, toolCallID, result)
		if err != nil {
			return truncate.Tail(result, truncate.FileMaxLines, truncate.BashMaxChars).Content
		}
		// Enrich with subdirectory context hints (AGENTS.md, CLAUDE.md, .cursorrules)
		if hintTracker != nil {
			if hints := hintTracker.CheckToolCall(toolName, toolArgs); hints != "" {
				processed += hints
			}
		}
		return processed
	}

	// Main loop executor uses ctoToolReg (has delegate_to, bash, file tools)
	// Sub-agents use allToolReg via ParallelRunner (has everything)
	// Always wrap with VisionAwareExecutor — it handles both native vision
	// (extracts image_url, strips base64 from text) and fallback (describes via chain).
	executor := core.ToolExecutor(ctoToolReg)
	vExec := vision.NewVisionAwareExecutor(ctoToolReg, cfg.VisionChain, logger)
	vExec.SetNativeVision(cfg.EngineHasVision)
	if cfg.VisualContext != nil {
		vExec.SetVisualContext(cfg.VisualContext)
		logger.Printf("Vision-aware executor enabled (native=%v, frame caching)", cfg.EngineHasVision)
	} else {
		logger.Printf("Vision-aware executor enabled (native=%v)", cfg.EngineHasVision)
	}
	executor = vExec

	baseAgent := agents.NewBaseAgent(agents.BaseConfig{
		Provider:        provider,
		Session:         sess,
		SystemPrompt:    systemPrompt,
		ToolSpecs:       ctoToolSpecs,
		Executor:        executor,
		MaxToolRounds:   maxRounds,
		MaxTokens:       16384,
		ContextSize:     cfg.ContextSize,
		ThinkingBudget:  4096,
		ProjectDir:      cfg.ProjectDir,
		SandboxID:       cfg.SandboxID,
		GenerateOptions: core.GenerateOptions{MaxTokens: 16384, Temperature: 0.7, TopP: 0.95, TopK: 20},
		ScratchStore:    scratchStore,
		PermDecisions:   core.GlobalDecisions,
		ToolPerms:       cfg.ToolPerms,
		BashRules:       cfg.BashRules,
		ExtraHooks:      ctoHooks,
		ToolResultProcessor: toolResultProcessor,
		Logger:          logger,
	})

	return &Agent{
		BaseAgent: baseAgent,
		Memory:    cfg.MemoryStore,
		config:    cfg,
		logger:    logger,
		jitStore:  jitStore,
		runner:    pr,
	}, nil
}

// Run executes the agent with a user message.
func (a *Agent) Run(ctx context.Context, userMsg string, subscriber chan<- core.AgentEvent) error {
	return a.Loop().RunWithImages(ctx, userMsg, nil, subscriber)
}

// RunWithImages executes the agent with a multimodal user message (text + images).
func (a *Agent) RunWithImages(ctx context.Context, userMsg string, images []core.ContentImage, subscriber chan<- core.AgentEvent) error {
	return a.Loop().RunWithImages(ctx, userMsg, images, subscriber)
}

// Continue sends a follow-up message.
func (a *Agent) Continue(ctx context.Context, userMsg string, subscriber chan<- core.AgentEvent) error {
	return a.Loop().Continue(ctx, userMsg, subscriber)
}

// Close releases resources.
func (a *Agent) Close() error {
	// Clean up live sub-agents (providers kept alive for delegate_continue)
	if a.runner != nil {
		a.runner.Close()
	}
	// Clean up session-scoped JIT workers
	if a.jitStore != nil {
		_ = a.jitStore.Cleanup()
	}
	a.BaseAgent.Close()
	return nil
}

// IsRunning returns whether the agent is currently active.
func (a *Agent) IsRunning() bool {
	return a.Loop().IsRunning()
}

// makeOrchestratorFactory creates a factory for recursive delegation.
// The factory captures the parent's infrastructure and creates child orchestrators
// pointing at division sub-directories.
func makeOrchestratorFactory(provider core.LLMProvider, parentCfg Config) orchestration.OrchestratorFactory {
	return func(ctx context.Context, subCfg orchestration.SubOrchestratorConfig) (orchestration.SubOrchestrator, error) {
		// Load org manifest from the division directory
		org := common.LoadOrgManifest(subCfg.DivisionPath)
		if org == nil {
			return nil, fmt.Errorf("no pux.yaml found in division %q", subCfg.DivisionPath)
		}

		// Load division-specific roles
		var orgRoles map[string]*common.AgentRole
		if org.RolesDir() != "" {
			orgRoles = common.LoadAgentRolesFrom(org.RolesDir())
		}

		// Resolve provider (model override or inherit from parent)
		subProvider := provider
		if subCfg.ModelID != "" && parentCfg.ModelResolver != nil {
			if resolved := parentCfg.ModelResolver(subCfg.ModelID); resolved != nil {
				subProvider = resolved
			}
		}

		// Build sub-orchestrator config — reuses parent infrastructure
		cfg := Config{
			ProjectDir:    subCfg.DivisionPath,
			SandboxID:     parentCfg.SandboxID,
			ContextSize:   parentCfg.ContextSize,
			MaxToolRounds: 50,
			WorkDir:       parentCfg.WorkDir,
			BashExecutor:  parentCfg.BashExecutor,
			FileOps:       parentCfg.FileOps,
			HostBash:      parentCfg.HostBash,
			HostFileOps:   parentCfg.HostFileOps,
			MemoryStore:   parentCfg.MemoryStore,
			Org:           org,
			OrgRoles:      orgRoles,
			MCPClient:     parentCfg.MCPClient,
			VisionChain:   parentCfg.VisionChain,
			VisualContext: parentCfg.VisualContext,
			ProviderFactory: parentCfg.ProviderFactory,
			ModelResolver: parentCfg.ModelResolver,
			ArtifactDB:    parentCfg.ArtifactDB,
			TranscriptDB:  parentCfg.TranscriptDB,
			Project:       parentCfg.Project,
			ExtraHooks:    parentCfg.ExtraHooks,
			DBProvider:    parentCfg.DBProvider,
			LLMProvider:   parentCfg.LLMProvider,
			DesktopProvider: parentCfg.DesktopProvider,
		}

		subOrch, err := New(subProvider, cfg)
		if err != nil {
			return nil, err
		}

		return &subOrchestratorAdapter{agent: subOrch}, nil
	}
}

// subOrchestratorAdapter wraps orchestrator.Agent to implement SubOrchestrator.
type subOrchestratorAdapter struct {
	agent *Agent
}

func (a *subOrchestratorAdapter) Run(ctx context.Context, userMsg string, subscriber chan<- core.AgentEvent) error {
	return a.agent.Run(ctx, userMsg, subscriber)
}

func (a *subOrchestratorAdapter) Close() error {
	return a.agent.Close()
}
