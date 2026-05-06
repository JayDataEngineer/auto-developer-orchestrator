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
	browsertools "github.com/auto-developer-orchestrator/backend/internal/tools/browser"
	desktoptools "github.com/auto-developer-orchestrator/backend/internal/tools/desktop"
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
	BrowserDriver   browsertools.Driver
	DesktopDriver   desktoptools.Driver
	FileOps         file.SandboxFileOps
	DelegateRunner  orchestration.DelegateRunner
	Skills          *skills.Store
	ApprovalHandler  hooks.ApprovalHandler     // optional: if set, create_plan requires user approval
	GitExecutor      hooks.GitExecutor         // optional: if set, git checkpoints are created
	ExtraHooks       []core.LoopHook           // optional: add-on hooks (Langfuse, etc.)
	VisionChain      *vision.FallbackChain     // optional: if set, auto-describes images in tool results
	MCPClient        *mcp.MultiClient          // optional: if set, registers MCP tools (search, analyze_image, etc.)
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

	// Register all tools
	tools := []core.Tool{
		bash.New(cfg.BashExecutor),
		file.NewReadTool(cfg.FileOps),
		file.NewWriteTool(cfg.FileOps),
		file.NewEditTool(cfg.FileOps),
		file.NewGrepTool(cfg.FileOps),
		file.NewGlobTool(cfg.FileOps),
		meta.NewWaitTool(),
		meta.NewYieldArtifactTool(),
	}

	if cfg.BrowserDriver != nil {
		tools = append(tools,
			browsertools.NewNavigateTool(cfg.BrowserDriver),
			browsertools.NewClickTool(cfg.BrowserDriver),
			browsertools.NewTypeTool(cfg.BrowserDriver),
			browsertools.NewReadPageTool(cfg.BrowserDriver),
			browsertools.NewScrollTool(cfg.BrowserDriver),
			browsertools.NewObserveTool(cfg.BrowserDriver),
			browsertools.NewSearchWebTool(cfg.BrowserDriver),
		)
	}

	if cfg.DesktopDriver != nil {
		tools = append(tools,
			desktoptools.NewScreenshotTool(cfg.DesktopDriver),
			desktoptools.NewClickTool(cfg.DesktopDriver),
			desktoptools.NewTypeTool(cfg.DesktopDriver),
			desktoptools.NewKeyTool(cfg.DesktopDriver),
		)
	}

	if cfg.DelegateRunner != nil {
		tools = append(tools,
			orchestration.NewDelegateToTool(cfg.DelegateRunner),
			orchestration.NewDelegateAsyncTool(cfg.DelegateRunner),
			orchestration.NewCollectResultsTool(cfg.DelegateRunner),
			orchestration.NewPlanTool(),
			orchestration.NewSynthesizeTool(),
		)
	}

	if cfg.MemoryStore != nil {
		tools = append(tools, memory.NewTool(cfg.MemoryStore))
	}

	// Register skills (auto-load from standard paths if not provided)
	skillStore := cfg.Skills
	if skillStore == nil {
		home, _ := os.UserHomeDir()
		skillStore = skills.LoadStandard(cfg.ProjectDir, home)
	}
	if skillStore.Count() > 0 {
		tools = append(tools, skills.NewReadSkillTool(skillStore))
		logger.Printf("Skills loaded: %d skills discovered", skillStore.Count())
	}

	// Register MCP tools (search, scrape, analyze_image, etc.) as first-class tools
	if cfg.MCPClient != nil {
		before := len(tools)
		tools = mcptools.RegisterAll(tools, cfg.MCPClient)
		logger.Printf("MCP tools registered: %d tools available", len(tools)-before)
	}

	// Create tool registry (needed by hooks and agent loop)
	toolReg := core.NewToolRegistry(tools)
	toolReg.RegisterCommonAliases()

	// Auto-create ParallelRunner for fan-out delegation if no custom runner provided
	if cfg.DelegateRunner == nil && provider != nil {
		toolSpecs := common.ToOpenAITools(tools)
		pr := orchestration.NewParallelRunner(provider, toolReg, toolSpecs, sess, cfg.ContextSize)
		pr.SetLogger(func(format string, args ...interface{}) {
			logger.Printf("PARALLEL_RUNNER: "+format, args...)
		})
		cfg.DelegateRunner = pr

		// Append delegation tools now that we have a runner
		tools = append(tools,
			orchestration.NewDelegateToTool(pr),
			orchestration.NewDelegateAsyncTool(pr),
			orchestration.NewCollectResultsTool(pr),
			orchestration.NewPlanTool(),
			orchestration.NewSynthesizeTool(),
		)

		// Rebuild tool registry with delegation tools included
		toolReg = core.NewToolRegistry(tools)
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

	systemPrompt := common.BuildOrchestratorPrompt(toolReg.All(), cfg.SandboxID, "", "")
	if skillStore.Count() > 0 {
		systemPrompt += skillStore.FormatAvailableSkills()
	}

	toolSpecs := common.ToOpenAITools(toolReg.All())
	loopCfg := core.AgentLoopConfig{
		SystemPrompt:   systemPrompt,
		MaxToolRounds:  maxRounds,
		MaxTokens:      16384,
		ContextSize:    cfg.ContextSize,
		ThinkingBudget: 4096,
		Tools:          toolSpecs,
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

	// Wrap tool registry with vision-aware executor if chain is provided
	executor := core.ToolExecutor(toolReg)
	if cfg.VisionChain != nil {
		executor = vision.NewVisionAwareExecutor(toolReg, cfg.VisionChain, logger)
		logger.Printf("Vision-aware executor enabled (wrapping tool registry)")
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
