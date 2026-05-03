package coder

import (
	"context"
	"fmt"
	"log"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/hooks"
	"github.com/auto-developer-orchestrator/backend/internal/session"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	"github.com/auto-developer-orchestrator/backend/internal/tools/file"
	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
	"github.com/auto-developer-orchestrator/backend/internal/tools/meta"
)

// Config holds configuration for the coder agent.
type Config struct {
	ProjectDir    string
	SandboxID     string
	ContextSize   int            // 0 = default (32K)
	MaxToolRounds int            // 0 = unlimited
	SessionPath   string
	WorkDir       string // working directory for file ops (equivalent to sandbox workspace path)
	MemoryStore   *memory.Store
	BashExecutor  bash.Executor     // required: executor for bash commands
	FileOps       file.SandboxFileOps // required: file operations in sandbox
}

// Agent is a minimal coding agent (pi-mono style).
// Tools: bash, file_read, file_write, file_edit, file_grep, file_glob, yield_artifact
// This is the equivalent of pi-mono's 4-7 tool set.
type Agent struct {
	Loop       *core.AgentLoop
	Session    *session.SessionTree
	Memory     *memory.Store
	config     Config
	logger     *log.Logger
}

// New creates a new coder agent.
func New(provider core.LLMProvider, cfg Config) (*Agent, error) {
	if cfg.SessionPath == "" {
		cfg.SessionPath = fmt.Sprintf("%s/.pux/sessions/coder-%s.jsonl", cfg.ProjectDir, cfg.SandboxID)
	}
	if cfg.MaxToolRounds == 0 {
		cfg.MaxToolRounds = 15
	}
	logger := log.Default()

	// Create session tree
	sess, err := session.New(cfg.SessionPath, cfg.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("coder: failed to create session: %w", err)
	}

	// Collect tools, then register in one shot
	var toolList []core.Tool
	toolList = append(toolList, bash.New(cfg.BashExecutor), meta.NewYieldArtifactTool())
	if cfg.FileOps != nil {
		toolList = append(toolList,
			file.NewReadTool(cfg.FileOps),
			file.NewWriteTool(cfg.FileOps),
			file.NewEditTool(cfg.FileOps),
			file.NewGrepTool(cfg.FileOps),
			file.NewGlobTool(cfg.FileOps),
		)
	}
	if cfg.MemoryStore != nil {
		toolList = append(toolList, memory.NewTool(cfg.MemoryStore))
	}
	toolReg := common.RegisterTools(toolList...)

	// Build hooks
	compactionHook := hooks.NewCompactionHook(sess, 0.55, 0.75, 4)
	goalNudgeHook := hooks.NewGoalNudgeHook(cfg.MaxToolRounds)
	journalHook := hooks.NewJournalCheckpointHook(sess)

	loopHooks := []core.LoopHook{compactionHook, goalNudgeHook, journalHook}

	// Build system prompt
	systemPrompt := common.BuildCoderPrompt(toolReg.All(), cfg.SandboxID)

	// Create agent loop
	loopCfg := core.AgentLoopConfig{
		SystemPrompt:   systemPrompt,
		MaxToolRounds:  cfg.MaxToolRounds,
		MaxTokens:      4096,
		ContextSize:    cfg.ContextSize,
		ThinkingBudget: 2048,
		Tools:          common.ToOpenAITools(toolReg.All()),
		Opts: core.GenerateOptions{
			MaxTokens:   4096,
			Temperature: 0.4,
			TopP:        0.95,
			TopK:        20,
		},
		Hooks:      loopHooks,
		ProjectDir: cfg.ProjectDir,
		SandboxID:  cfg.SandboxID,
	}

	loop := core.NewAgentLoop(provider, toolReg, sess, loopCfg)
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
