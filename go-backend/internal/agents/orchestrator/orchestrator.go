package orchestrator

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/hooks"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/session"
	"github.com/auto-developer-orchestrator/backend/internal/skills"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	"github.com/auto-developer-orchestrator/backend/internal/tools/file"
	mcptools "github.com/auto-developer-orchestrator/backend/internal/tools/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
	"github.com/auto-developer-orchestrator/backend/internal/tools/meta"
	"github.com/auto-developer-orchestrator/backend/internal/tools/orchestration"
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
	DelegateRunner  orchestration.DelegateRunner
	Skills          *skills.Store
	ApprovalHandler  hooks.ApprovalHandler     // optional: if set, create_plan requires user approval
	GitExecutor      hooks.GitExecutor         // optional: if set, git checkpoints are created
	ExtraHooks       []core.LoopHook           // optional: add-on hooks (Langfuse, etc.)
	VisionChain      *vision.FallbackChain     // optional: if set, auto-describes images in tool results
	MCPClient        *mcp.MultiClient          // optional: if set, registers MCP tools (search, analyze_image, etc.)
	ModelResolver    orchestration.ModelResolver // optional: if set, sub-agents can use role-specific models
	ArtifactDB       meta.ArtifactStore         // optional: if set, yield_artifact persists to DB
	Org              *common.OrgManifest        // optional: org manifest for overlay mode
	OrgRoles         map[string]*common.AgentRole // optional: org-specific employee roles
}

// Agent is the full orchestrator agent with all tools.
type Agent struct {
	Loop    *core.AgentLoop
	Session *session.SessionTree
	Memory  *memory.Store
	config  Config
	logger  *log.Logger
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

	if cfg.MemoryStore != nil {
		ctoTools = append(ctoTools, memory.NewTool(cfg.MemoryStore))
	}

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

	// ── Employee tools: NOT registered on the CTO ──
	// These are collected into allTools for sub-agent toolSpecs only.
	employeeTools := []core.Tool{}

	if cfg.MCPClient != nil {
		employeeTools = mcptools.RegisterAll(employeeTools, cfg.MCPClient)
		logger.Printf("MCP tools loaded for employees: %d tools", len(employeeTools))
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
	if cfg.DelegateRunner != nil {
		runner = cfg.DelegateRunner
	}

	if runner == nil && provider != nil {
		pr := orchestration.NewParallelRunner(provider, allToolReg, allToolSpecs, sess, cfg.ContextSize, cfg.ModelResolver)
		pr.SetLogger(func(format string, args ...interface{}) {
			logger.Printf("PARALLEL_RUNNER: "+format, args...)
		})
		runner = pr
	}

	if runner != nil {
		ctoTools = append(ctoTools,
			orchestration.NewDelegateToTool(runner, mcpResolver),
			orchestration.NewDelegateAsyncTool(runner, mcpResolver),
			orchestration.NewCollectResultsTool(runner),
			orchestration.NewPlanTool(),
			orchestration.NewSynthesizeTool(),
		)
		ctoToolReg = core.NewToolRegistry(ctoTools)
		ctoToolReg.RegisterCommonAliases()
	}

	maxRounds := cfg.MaxToolRounds
	if maxRounds == 0 {
		maxRounds = 50
	}

	compactionHook := hooks.NewCompactionHook(sess, 0.55, 0.75, 4)
	goalNudgeHook := hooks.NewGoalNudgeHook(maxRounds)
	journalHook := hooks.NewJournalCheckpointHook(sess)
	loopHooks := []core.LoopHook{compactionHook, goalNudgeHook, journalHook}

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

	// Add extra hooks from add-ons (Langfuse, etc.)
	loopHooks = append(loopHooks, cfg.ExtraHooks...)

	var skillsStr string
	if skillStore.Count() > 0 {
		skillsStr = skillStore.FormatAvailableSkills()
	}
	systemPrompt := common.BuildOrchestratorPromptWithOrg(ctoToolReg.All(), cfg.SandboxID, "", skillsStr, cfg.Org, cfg.OrgRoles)

	ctoToolSpecs := common.ToOpenAITools(ctoToolReg.All())
	loopCfg := core.AgentLoopConfig{
		SystemPrompt:   systemPrompt,
		MaxToolRounds:  maxRounds,
		MaxTokens:      16384,
		ContextSize:    cfg.ContextSize,
		ThinkingBudget: 4096,
		Tools:          ctoToolSpecs,
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
		executor = vision.NewVisionAwareExecutor(ctoToolReg, cfg.VisionChain, logger)
		logger.Printf("Vision-aware executor enabled (wrapping CTO tool registry)")
	}

	loop := core.NewAgentLoop(provider, executor, sess, loopCfg)
	loop.SetLogger(logger)

	return &Agent{
		Loop:    loop,
		Session: sess,
		Memory:  cfg.MemoryStore,
		config:  cfg,
		logger:  logger,
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
	return a.Loop.Close()
}

// IsRunning returns whether the agent is currently active.
func (a *Agent) IsRunning() bool {
	return a.Loop.IsRunning()
}
