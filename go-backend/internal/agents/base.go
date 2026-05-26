package agents

import (
	"context"
	"log"
	"time"

	ctxpkg "github.com/auto-developer-orchestrator/backend/internal/context"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/hooks"
	"github.com/auto-developer-orchestrator/backend/internal/perms"
)

// BaseConfig holds configuration common to all agents (orchestrator and sub-agents).
type BaseConfig struct {
	Provider        core.LLMProvider
	Session         core.Session
	SystemPrompt    string
	ToolSpecs       []core.OpenAITool
	Executor        core.ToolExecutor
	MaxToolRounds   int
	MaxTokens       int
	ContextSize     int
	ThinkingBudget  int
	ProjectDir      string
	SandboxID       string
	GenerateOptions core.GenerateOptions

	// ScratchStore is shared across the agent hierarchy.
	// The same instance is used by the CTO and all sub-agents.
	ScratchStore *ctxpkg.ScratchStore

	// Permission checking (shared DecisionRegistry across all agents).
	PermDecisions *core.DecisionRegistry
	ToolPerms     *perms.ToolPermissionConfig

	// Extension points.
	ExtraHooks           []core.LoopHook
	ToolResultProcessor  func(ctx context.Context, toolName, toolCallID, result string, toolArgs map[string]any) string
	Logger               *log.Logger
}

// BaseAgent is the common agent foundation for both the CTO orchestrator
// and sub-agents. It wires up the core AgentLoop with hooks that every
// agent should have: scratch pad re-injection, goal nudges, cycle detection,
// and permission checking.
type BaseAgent struct {
	loop     *core.AgentLoop
	Session  core.Session
	Provider core.LLMProvider
	Scratch  *ctxpkg.ScratchStore

	config BaseConfig
	logger *log.Logger
}

// NewBaseAgent creates a BaseAgent with common hooks wired in.
func NewBaseAgent(cfg BaseConfig) *BaseAgent {
	maxRounds := cfg.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 50
	}

	var hks []core.LoopHook

	hks = append(hks, hooks.NewGoalNudgeHook(maxRounds))

	if cfg.ScratchStore != nil {
		hks = append(hks, hooks.NewScratchpadHook(cfg.ScratchStore))
	}

	if cfg.ToolPerms != nil && cfg.PermDecisions != nil {
		hks = append(hks, hooks.NewPermissionHook(cfg.ToolPerms, cfg.PermDecisions, nil))
	}

	hks = append(hks, cfg.ExtraHooks...)

	opts := cfg.GenerateOptions
	if opts.MaxTokens == 0 {
		opts.MaxTokens = cfg.MaxTokens
		if opts.MaxTokens == 0 {
			opts.MaxTokens = 8192
		}
	}
	if opts.Temperature == 0 {
		opts.Temperature = 0.7
	}
	if opts.TopP == 0 {
		opts.TopP = 0.95
	}

	loopCfg := core.AgentLoopConfig{
		SystemPrompt:        cfg.SystemPrompt,
		MaxToolRounds:       maxRounds,
		MaxTokens:           cfg.MaxTokens,
		ContextSize:         cfg.ContextSize,
		ThinkingBudget:      cfg.ThinkingBudget,
		Tools:               cfg.ToolSpecs,
		Opts:                opts,
		Hooks:               hks,
		ProjectDir:          cfg.ProjectDir,
		SandboxID:           cfg.SandboxID,
		ToolResultProcessor: cfg.ToolResultProcessor,
	}

	// Wire ToolMetadata lookup if executor is a ToolRegistry
	if reg, ok := cfg.Executor.(*core.ToolRegistry); ok {
		loopCfg.ToolTimeoutHint = func(name string) time.Duration {
			if t := reg.Get(name); t != nil {
				if meta, ok := t.(core.ToolMetadata); ok {
					return meta.TimeoutHint()
				}
			}
			return 0
		}
	}

	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	ba := &BaseAgent{
		Session:  cfg.Session,
		Provider: cfg.Provider,
		Scratch:  cfg.ScratchStore,
		config:   cfg,
		logger:   logger,
	}
	ba.loop = core.NewAgentLoop(cfg.Provider, cfg.Executor, cfg.Session, loopCfg)
	ba.loop.SetLogger(logger)
	return ba
}

// Loop returns the underlying AgentLoop.
func (a *BaseAgent) Loop() *core.AgentLoop { return a.loop }

// Close releases the agent loop resources.
func (a *BaseAgent) Close() {
	if a.loop != nil {
		a.loop.Close()
	}
}
