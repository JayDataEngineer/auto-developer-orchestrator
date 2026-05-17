package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/adapters"
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
	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
	"github.com/auto-developer-orchestrator/backend/internal/tools/meta"
	plantool "github.com/auto-developer-orchestrator/backend/internal/tools/plan"
	schedulertool "github.com/auto-developer-orchestrator/backend/internal/tools/scheduler"
	"github.com/auto-developer-orchestrator/backend/internal/tools/todo"
	"github.com/auto-developer-orchestrator/backend/internal/tools/orchestration"
	"github.com/auto-developer-orchestrator/backend/internal/perms"
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
	BashExecutor    bash.Executor
	FileOps         file.SandboxFileOps
	DelegateRunner   orchestration.DelegateRunner
	ProviderFactory  orchestration.ProviderFactory  // creates isolated providers per sub-agent (own session/slot)
	Skills           *skills.Store
	ApprovalHandler  hooks.ApprovalHandler     // optional: if set, create_plan requires user approval
	GitExecutor      hooks.GitExecutor         // optional: if set, git checkpoints are created
	ExtraHooks       []core.LoopHook           // optional: add-on hooks (Langfuse, etc.)
	VisionChain      *vision.FallbackChain     // optional: if set, auto-describes images in tool results
	VisualContext    vision.VisualContext      // optional: if set, enables frame-based vision caching
	MCPClient        *mcp.MultiClient          // optional: if set, registers MCP tools (search, analyze_image, etc.)
	ModelResolver    orchestration.ModelResolver // optional: if set, sub-agents can use role-specific models
	ArtifactDB       meta.ArtifactStore         // optional: if set, yield_artifact persists to DB
	Org              *common.OrgManifest        // optional: org manifest for overlay mode
	OrgRoles         map[string]*common.AgentRole // optional: org-specific employee roles
	DBProvider       common.DBProvider          // optional: if set, registers graph/face tools for employees
	LLMProvider      core.LLMProvider           // optional: if set, registers NLP tools for employees
	BrowserProvider browsertools.BrowserProvider // optional: if set, registers browser a11y/cookie/storage tools for employees
	DesktopProvider desktoptools.DesktopProvider // optional: if set, registers desktop screenshot/click/type/key tools for employees
	Subscriber      chan<- core.AgentEvent      // optional: if set, ask_user tool can emit events to TUI
	Scheduler       any                         // optional: *scheduler.Scheduler — passed through to scheduler tool
	ToolPerms       *perms.ToolPermissionConfig // optional: if set, enables per-tool permission checks
}

// Agent is the full orchestrator agent with all tools.
type Agent struct {
	Loop     *core.AgentLoop
	Session  *session.SessionTree
	Memory   *memory.Store
	config   Config
	logger   *log.Logger
	jitStore *autoconfig.WorkerStore          // session-scoped workers, cleaned up on Close
	runner   *orchestration.ParallelRunner    // nil if external DelegateRunner provided
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

	ctoTools := []core.Tool{
		bash.New(cfg.BashExecutor),
		file.NewReadTool(cfg.FileOps),
		file.NewWriteTool(cfg.FileOps),
		file.NewEditTool(cfg.FileOps),
		file.NewGrepTool(cfg.FileOps),
		file.NewGlobTool(cfg.FileOps),
		meta.NewWaitTool(),
		meta.NewYieldArtifactToolWithDB(cfg.ArtifactDB, cfg.ProjectDir, cfg.SandboxID),
	}

	// Register ask_user tool (Contract 3: no subscriber injection — uses context)
	if cfg.Subscriber != nil {
		ctoTools = append(ctoTools, asktool.NewAskUserTool())

		// Plan tool uses PlanStore (ArtifactStore contract) for file I/O
		plansDir := filepath.Join(cfg.ProjectDir, ".pux", "plans")
		planStore := autoconfig.NewPlanStore(plansDir)
		ctoTools = append(ctoTools, plantool.NewPlanTool(cfg.ProjectDir, planStore))
	}

	if cfg.MemoryStore != nil {
		ctoTools = append(ctoTools, memory.NewTool(cfg.MemoryStore))
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

	// Register skills (auto-load from standard paths if not provided)
	skillStore := cfg.Skills
	if skillStore == nil {
		home, _ := os.UserHomeDir()
		skillStore = skills.LoadStandard(cfg.ProjectDir, home)
	}
	if skillStore.Count() > 0 {
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
	ctxMgr := ctxpkg.Factory(sess, ctxConfig)

	scratchStore := ctxpkg.NewScratchStore()
	ctoTools = append(ctoTools,
		ctxpkg.NewScratchWriteTool(scratchStore),
		ctxpkg.NewScratchReadTool(scratchStore),
		ctxpkg.NewScratchClearTool(scratchStore),
		ctxpkg.NewLoadSpilledTool(ctxMgr),
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
		employeeTools = browsertools.RegisterBrowserTools(employeeTools, cfg.BrowserProvider, sandboxIDFn)
		logger.Printf("Browser a11y/cookie/storage tools loaded for employees: 8 tools")
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
		pr = orchestration.NewParallelRunner(allToolReg, allToolSpecs, sess, cfg.ContextSize, cfg.ModelResolver)
		pr.SetProviderFactory(cfg.ProviderFactory)
		pr.SetLogger(func(format string, args ...interface{}) {
			logger.Printf("PARALLEL_RUNNER: "+format, args...)
		})
		pr.SetProjectDir(cfg.ProjectDir)
		pr.SetDepth(0)
		pr.SetOrchestratorFactory(makeOrchestratorFactory(provider, cfg))
		// Subscriber is now injected via context (Contract 3.4 compliance).
		// No need to call SetSubscriber — subscriberFromCtx() extracts it from ctx.
		// Wire visual context for sub-agent vision caching
		if cfg.VisualContext != nil && cfg.VisionChain != nil {
			pr.SetVisualContext(cfg.VisualContext, cfg.VisionChain, func(format string, args ...interface{}) {
				logger.Printf("SUB_VISION: "+format, args...)
			})
		}
		// Wire git-based change tracking for delegate_to / delegate_continue / delegate_revert
		if cfg.BashExecutor != nil {
			pr.SetSnapshotter(orchestration.NewGitSnapshotter(cfg.BashExecutor))
		}

		// Auto-director: raise Chrome window for VNC visibility when browser agent starts
		if cfg.BashExecutor != nil && cfg.SandboxID != "" {
			bashExec := cfg.BashExecutor
			pr.SetRaiseBrowserFunc(func(ctx context.Context) {
				_, _ = bashExec.Exec(ctx, "DISPLAY=:99 wmctrl -a 'Google Chrome' 2>/dev/null || true")
			})
		}
		// Per-role sandbox tier: native roles use host executor, others use sandbox
		pr.SetExecutorFactory(func(tier string) core.ToolExecutor {
			if tier == "native" {
				hostExec := &adapters.HostExecutor{WorkDir: cfg.ProjectDir}
				hostFileOps := &file.SimpleSandboxOps{BasePath: cfg.ProjectDir}
				nativeReg := core.NewToolRegistry([]core.Tool{
					bash.New(hostExec),
					file.NewReadTool(hostFileOps),
					file.NewWriteTool(hostFileOps),
					file.NewEditTool(hostFileOps),
					file.NewGrepTool(hostFileOps),
					file.NewGlobTool(hostFileOps),
				})
				nativeReg.RegisterCommonAliases()
				return nativeReg
			}
			// isolated + bridged both use the sandbox executor
			return allToolReg
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

	// Context manager handles compaction now — no more CompactionHook
	goalNudgeHook := hooks.NewGoalNudgeHook(maxRounds)
	journalHook := hooks.NewJournalCheckpointHook(sess)
	scratchpadHook := hooks.NewScratchpadHook(scratchStore)
	loopHooks := []core.LoopHook{goalNudgeHook, journalHook, scratchpadHook}

	// Add git checkpoint hook if executor provided
	if cfg.GitExecutor != nil {
		gitHook := hooks.NewGitCheckpointHook(cfg.GitExecutor)
		loopHooks = append(loopHooks, gitHook)
		logger.Printf("Git checkpoint hook enabled")
	}

	// Add approval hook if handler provided
	if cfg.ApprovalHandler != nil {
		approvalHook := hooks.NewApprovalHook(cfg.ApprovalHandler, true, 0) // plan-only, default timeout
		loopHooks = append(loopHooks, approvalHook)
		logger.Printf("Approval hook enabled (plan-only mode)")
	}

	// Add permission hook if tool permissions configured
	if cfg.ToolPerms != nil {
		permHook := hooks.NewPermissionHook(cfg.ToolPerms, core.GlobalDecisions, nil)
		loopHooks = append(loopHooks, permHook)
		logger.Printf("Permission hook enabled (%d tools configured)", len(cfg.ToolPerms.AllPermissions()))
	}

	// Add extra hooks from add-ons (Langfuse, etc.)
	loopHooks = append(loopHooks, cfg.ExtraHooks...)

	// Todo hook — injects todo state before each model call
	todoHook := hooks.NewTodoHook(todoStore)
	loopHooks = append(loopHooks, todoHook)

	var skillsStr string
	if skillStore.Count() > 0 {
		skillsStr = skillStore.FormatAvailableSkills()
	}
	systemPrompt := common.BuildOrchestratorPromptV2(ctoToolReg.All(), cfg.SandboxID, "", skillsStr, cfg.Org, cfg.OrgRoles)

	ctoToolSpecs := common.ToOpenAITools(ctoToolReg.All())
	loopCfg := core.AgentLoopConfig{
		SystemPrompt:   systemPrompt,
		MaxToolRounds:  maxRounds,
		MaxTokens:      16384,
		ContextSize:    cfg.ContextSize,
		ThinkingBudget: 4096,
		Tools:          ctoToolSpecs,
		ToolResultProcessor: func(procCtx context.Context, toolName, toolCallID, result string) string {
			processed, err := ctxMgr.ProcessToolResult(procCtx, toolName, toolCallID, result)
			if err != nil {
				// Fallback: use line-aware truncation instead of blind slice
				return truncate.Tail(result, truncate.FileMaxLines, truncate.BashMaxChars).Content
			}
			return processed
		},
		Opts: core.GenerateOptions{
			MaxTokens:   16384,
			Temperature: 0.7,
			TopP:        0.95,
			TopK:        20,
		},
		Hooks:      loopHooks,
		ProjectDir: cfg.ProjectDir,
		SandboxID:  cfg.SandboxID,
	}

	// Main loop executor uses ctoToolReg (has delegate_to, bash, file tools)
	// Sub-agents use allToolReg via ParallelRunner (has everything)
	executor := core.ToolExecutor(ctoToolReg)
	if cfg.VisionChain != nil {
		vExec := vision.NewVisionAwareExecutor(ctoToolReg, cfg.VisionChain, logger)
		if cfg.VisualContext != nil {
			vExec.SetVisualContext(cfg.VisualContext)
			logger.Printf("Vision-aware executor enabled with frame caching (wrapping CTO tool registry)")
		} else {
			logger.Printf("Vision-aware executor enabled (wrapping CTO tool registry)")
		}
		executor = vExec
	}

	loop := core.NewAgentLoop(provider, executor, sess, loopCfg)
	loop.SetLogger(logger)

	return &Agent{
		Loop:     loop,
		Session:  sess,
		Memory:   cfg.MemoryStore,
		config:   cfg,
		logger:   logger,
		jitStore: jitStore,
		runner:   pr,
	}, nil
}

// Run executes the agent with a user message.
func (a *Agent) Run(ctx context.Context, userMsg string, subscriber chan<- core.AgentEvent) error {
	return a.Loop.Run(ctx, userMsg, subscriber)
}

// Continue sends a follow-up message.
func (a *Agent) Continue(ctx context.Context, userMsg string, subscriber chan<- core.AgentEvent) error {
	return a.Loop.Continue(ctx, userMsg, subscriber)
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
	return a.Loop.Close()
}

// IsRunning returns whether the agent is currently active.
func (a *Agent) IsRunning() bool {
	return a.Loop.IsRunning()
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
			MemoryStore:   parentCfg.MemoryStore,
			Org:           org,
			OrgRoles:      orgRoles,
			MCPClient:     parentCfg.MCPClient,
			VisionChain:   parentCfg.VisionChain,
			VisualContext: parentCfg.VisualContext,
			ProviderFactory: parentCfg.ProviderFactory,
			ModelResolver: parentCfg.ModelResolver,
			ArtifactDB:    parentCfg.ArtifactDB,
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
