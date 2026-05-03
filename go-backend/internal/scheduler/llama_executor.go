package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/manifest"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// LlamaExecutor runs scheduled jobs using the llama HTTP engine directly.
// This replaces both IsolatedExecutor (Pi subprocess) and SchedulerPromptSender
// (Pi pool) — no Pi subprocess needed.
type LlamaExecutor struct {
	engine      *llamaeng.LLMClient
	sandboxMgr  *sandbox.Manager
	projectRoot string
	logger      *zap.Logger
	mcpMulti    *mcp.MultiClient
}

// NewLlamaExecutor creates a scheduler executor backed by the llama engine.
func NewLlamaExecutor(engine *llamaeng.LLMClient, sandboxMgr *sandbox.Manager, projectRoot string, logger *zap.Logger) *LlamaExecutor {
	return &LlamaExecutor{
		engine:      engine,
		sandboxMgr:  sandboxMgr,
		projectRoot: projectRoot,
		logger:      logger,
	}
}

// SetMCPMulti injects the MCP multi-client so scheduled jobs can use MCP tools.
func (e *LlamaExecutor) SetMCPMulti(multi *mcp.MultiClient) {
	e.mcpMulti = multi
}

// Execute runs a job using the llama engine. Creates a fresh OrchestratorLoop
// per execution, collects text output, then closes it to free VRAM.
const defaultSandboxID = "scheduler-default"

func (e *LlamaExecutor) Execute(ctx context.Context, jobID, jobName, projectPath, message, model string, timeoutSec int) *JobResult {
	start := time.Now()
	if timeoutSec <= 0 {
		timeoutSec = 600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// When no project is specified, use a default sandbox so the agent can
	// still run bash commands, write files, etc.
	sandboxID := filepath.Base(projectPath)
	if sandboxID == "" || sandboxID == "." {
		sandboxID = defaultSandboxID
		if projectPath == "" {
			projectPath = "/tmp/scheduler-default"
		}
		e.ensureDefaultSandbox(ctx)
	}

	// Load skills from standard discovery paths (pi-mono standard)
	var skills *llamaeng.SkillStore
	var baseExecutor llamaeng.ToolExecutor
	if e.sandboxMgr != nil {
		// Auto-initialize sandbox from manifest if needed (pip, files, env)
		e.initSandboxFromManifest(ctx, projectPath, sandboxID)

		skills = llamaeng.LoadStandardSkills(projectPath, "")
		baseExecutor = &llamaeng.SandboxToolExecutor{
			SandboxID: sandboxID,
			Manager:   e.sandboxMgr,
			MCPMulti:  e.mcpMulti,
			Logger:    e.logger,
			Skills:    skills,
		}
	}

	// Create a fresh orchestrator loop for this job
	orch, err := llamaeng.NewOrchestratorLoop(e.engine, baseExecutor, llamaeng.OrchestratorConfig{
		ProjectDir: projectPath,
		SandboxID:  sandboxID,
		Skills:     skills,
	}, e.logger)
	if err != nil {
		return &JobResult{
			Error:      fmt.Sprintf("failed to create orchestrator: %v", err),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	defer orch.Close()

	// Subscribe to events and collect text output
	events := make(chan llamaeng.AgentEvent, 256)
	var output strings.Builder

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range events {
			if evt.Type == llamaeng.EventTypeTextDelta {
				output.WriteString(evt.Data.Text)
			}
			if evt.Type == llamaeng.EventTypeAgentEnd {
				return
			}
		}
	}()

	// Run the orchestrator
	orchMsg := fmt.Sprintf("Scheduled task: %s\n\n%s", jobName, message)
	loopErr := orch.Run(ctx, orchMsg, events)

	// Wait for event collection to finish
	<-done

	if loopErr != nil && loopErr != context.DeadlineExceeded {
		return &JobResult{
			Output:     output.String(),
			Error:      loopErr.Error(),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}

	result := &JobResult{
		Output:     output.String(),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.Error = fmt.Sprintf("job execution timed out after %ds", timeoutSec)
	}
	return result
}

// ensureDefaultSandbox creates a default sandbox if it doesn't already exist.
// This gives projectless tasks a place to run bash commands.
func (e *LlamaExecutor) ensureDefaultSandbox(ctx context.Context) {
	if e.sandboxMgr == nil {
		return
	}
	if sb := e.sandboxMgr.FindSandboxByProject(defaultSandboxID); sb != nil {
		return // already exists
	}
	_, err := e.sandboxMgr.CreateSandbox(ctx, sandbox.SandboxOptions{
		ID: defaultSandboxID,
	})
	if err != nil {
		e.logger.Warn("Failed to create default sandbox for projectless task", zap.Error(err))
	}
}

// resolveProjectPath resolves a project name to an absolute path.
func (e *LlamaExecutor) resolveProjectPath(project string) string {
	if project == "" {
		return ""
	}
	candidate := filepath.Join(e.projectRoot, project)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}
	if info, err := os.Stat(project); err == nil && info.IsDir() {
		return project
	}
	return ""
}

// initSandboxFromManifest checks if the project has a pux.yaml with sandbox config
// and auto-initializes the sandbox (upload files, pip install, write .env).
// This is idempotent — safe to call multiple times.
func (e *LlamaExecutor) initSandboxFromManifest(ctx context.Context, projectPath, sandboxID string) {
	if e.sandboxMgr == nil || projectPath == "" {
		return
	}

	mf, err := manifest.LoadManifest(projectPath)
	if err != nil || mf == nil || mf.Sandbox == nil {
		return
	}

	sb := e.sandboxMgr.FindSandboxByProject(sandboxID)
	if sb == nil {
		return
	}

	cfg := mf.Sandbox
	initCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	// Upload init files
	for _, relPath := range cfg.InitFiles {
		localPath := filepath.Join(projectPath, relPath)
		sandboxPath := filepath.Join("/sandbox", filepath.Base(relPath))
		if err := e.sandboxMgr.CopyToSandbox(initCtx, sb.ID, localPath, sandboxPath); err != nil {
			e.logger.Warn("auto-init: failed to upload file",
				zap.String("file", relPath), zap.Error(err))
		}
	}

	// Install pip packages
	if len(cfg.PipPackages) > 0 {
		if err := e.sandboxMgr.PipInstall(initCtx, sb.ID, cfg.PipPackages); err != nil {
			e.logger.Warn("auto-init: pip install failed", zap.Error(err))
		} else {
			e.logger.Info("auto-init: pip packages installed",
				zap.Strings("packages", cfg.PipPackages))
		}
	}

	// Write .env file
	if len(cfg.Env) > 0 {
		if err := e.sandboxMgr.WriteEnvFile(initCtx, sb.ID, cfg.Env); err != nil {
			e.logger.Warn("auto-init: write .env failed", zap.Error(err))
		}
	}
}
