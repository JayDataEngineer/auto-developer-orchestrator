package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// LlamaExecutor runs scheduled jobs using the llama HTTP engine directly.
// This replaces both IsolatedExecutor (Pi subprocess) and SchedulerPromptSender
// (Pi pool) — no Pi subprocess needed.
type LlamaExecutor struct {
	engine      *llamaeng.HTTPEngine
	sandboxMgr  *sandbox.Manager
	projectRoot string
	logger      *zap.Logger
}

// NewLlamaExecutor creates a scheduler executor backed by the llama engine.
func NewLlamaExecutor(engine *llamaeng.HTTPEngine, sandboxMgr *sandbox.Manager, projectRoot string, logger *zap.Logger) *LlamaExecutor {
	return &LlamaExecutor{
		engine:      engine,
		sandboxMgr:  sandboxMgr,
		projectRoot: projectRoot,
		logger:      logger,
	}
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

	// Build base executor for tool dispatch
	var baseExecutor llamaeng.ToolExecutor
	if e.sandboxMgr != nil {
		baseExecutor = &llamaeng.SandboxToolExecutor{
			SandboxID: sandboxID,
			Manager:   e.sandboxMgr,
			// CU bridge not needed for most scheduled jobs (coding tasks)
			// If needed, it can be injected via LlamaExecutor fields
			Logger: e.logger,
		}
	}

	// Create a fresh orchestrator loop for this job
	orch, err := llamaeng.NewOrchestratorLoop(e.engine, baseExecutor, llamaeng.OrchestratorConfig{
		ProjectDir: projectPath,
		SandboxID:  sandboxID,
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
