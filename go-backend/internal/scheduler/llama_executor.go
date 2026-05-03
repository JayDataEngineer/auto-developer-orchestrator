package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/adapters"
	"github.com/auto-developer-orchestrator/backend/internal/agents/orchestrator"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/llm"
	"github.com/auto-developer-orchestrator/backend/internal/manifest"
	"github.com/auto-developer-orchestrator/backend/internal/mcp"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"github.com/auto-developer-orchestrator/backend/internal/skills"
	"go.uber.org/zap"
)

// LlamaExecutor runs scheduled jobs using the llama HTTP engine and new architecture.
type LlamaExecutor struct {
	engine      *llamaeng.LLMClient
	sandboxMgr  *sandbox.Manager
	projectRoot string
	logger      *zap.Logger
	mcpMulti    *mcp.MultiClient
}

func NewLlamaExecutor(engine *llamaeng.LLMClient, sandboxMgr *sandbox.Manager, projectRoot string, logger *zap.Logger) *LlamaExecutor {
	return &LlamaExecutor{
		engine:      engine,
		sandboxMgr:  sandboxMgr,
		projectRoot: projectRoot,
		logger:      logger,
	}
}

func (e *LlamaExecutor) SetMCPMulti(multi *mcp.MultiClient) {
	e.mcpMulti = multi
}

const defaultSandboxID = "scheduler-default"

func (e *LlamaExecutor) Execute(ctx context.Context, jobID, jobName, projectPath, message, model string, timeoutSec int) *JobResult {
	start := time.Now()
	if timeoutSec <= 0 {
		timeoutSec = 600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	sandboxID := filepath.Base(projectPath)
	if sandboxID == "" || sandboxID == "." {
		sandboxID = defaultSandboxID
		if projectPath == "" {
			projectPath = "/tmp/scheduler-default"
		}
		e.ensureDefaultSandbox(ctx)
	}

	// Auto-initialize sandbox from manifest if needed
	e.initSandboxFromManifest(ctx, projectPath, sandboxID)

	// Build provider adapter from llama engine
	provider := llm.NewAdapter(e.engine, 0)
	defer provider.Close()

	// Build sandbox adapters (shared with handlers)
	var bashExec adapters.BashExecutor
	var fileOps adapters.FileOps
	if e.sandboxMgr != nil {
		bashExec = adapters.BashExecutor{Mgr: e.sandboxMgr, SandboxID: sandboxID}
		fileOps = adapters.FileOps{Mgr: e.sandboxMgr, SandboxID: sandboxID}
	}

	// Load skills from standard paths
	home, _ := os.UserHomeDir()
	skillStore := skills.LoadStandard(projectPath, home)

	// Create orchestrator with new architecture
	orch, err := orchestrator.New(provider, orchestrator.Config{
		ProjectDir:    projectPath,
		SandboxID:     sandboxID,
		ContextSize:   32768,
		MaxToolRounds: 50,
		WorkDir:       "/sandbox",
		BashExecutor:  &bashExec,
		FileOps:       &fileOps,
		Skills:        skillStore,
		GitExecutor:   &adapters.GitExecutor{},
	})
	if err != nil {
		return &JobResult{
			Error:      fmt.Sprintf("failed to create orchestrator: %v", err),
			DurationMs: time.Since(start).Milliseconds(),
		}
	}
	defer orch.Close()

	// Subscribe to events and collect text output
	events := make(chan core.AgentEvent, 256)
	var output strings.Builder

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range events {
			if evt.Type == core.EventTypeTextDelta {
				output.WriteString(evt.Data.Text)
			}
			if evt.Type == core.EventTypeAgentEnd {
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

func (e *LlamaExecutor) ensureDefaultSandbox(ctx context.Context) {
	if e.sandboxMgr == nil {
		return
	}
	if sb := e.sandboxMgr.FindSandboxByProject(defaultSandboxID); sb != nil {
		return
	}
	_, err := e.sandboxMgr.CreateSandbox(ctx, sandbox.SandboxOptions{
		ID: defaultSandboxID,
	})
	if err != nil {
		e.logger.Warn("Failed to create default sandbox for projectless task", zap.Error(err))
	}
}

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

	for _, relPath := range cfg.InitFiles {
		localPath := filepath.Join(projectPath, relPath)
		sandboxPath := filepath.Join("/sandbox", filepath.Base(relPath))
		if err := e.sandboxMgr.CopyToSandbox(initCtx, sb.ID, localPath, sandboxPath); err != nil {
			e.logger.Warn("auto-init: failed to upload file",
				zap.String("file", relPath), zap.Error(err))
		}
	}

	if len(cfg.PipPackages) > 0 {
		if err := e.sandboxMgr.PipInstall(initCtx, sb.ID, cfg.PipPackages); err != nil {
			e.logger.Warn("auto-init: pip install failed", zap.Error(err))
		} else {
			e.logger.Info("auto-init: pip packages installed",
				zap.Strings("packages", cfg.PipPackages))
		}
	}

	if len(cfg.Env) > 0 {
		if err := e.sandboxMgr.WriteEnvFile(initCtx, sb.ID, cfg.Env); err != nil {
			e.logger.Warn("auto-init: write .env failed", zap.Error(err))
		}
	}
}
