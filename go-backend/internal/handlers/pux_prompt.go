package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/adapters"
	"github.com/auto-developer-orchestrator/backend/internal/agents/orchestrator"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	llama "github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/llm"
	"github.com/auto-developer-orchestrator/backend/internal/observability"
	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
	"go.uber.org/zap"
)

// promptWithOrchestrator handles prompt requests using the orchestrator agent.
// This is the default and only path for /api/pux/prompt.
func (h *PuxHandler) promptWithOrchestrator(w http.ResponseWriter, r *http.Request, req promptRequest, projectPath string) {
	key := compositeAgentKey(projectPath, req.AgentId)

	// Resolve sandbox ID from manager
	sandboxID := filepath.Base(projectPath)
	if h.sandboxMgr != nil {
		if sb := h.sandboxMgr.FindSandboxByProject(projectPath); sb != nil {
			sandboxID = sb.ID
		}
	}

	// Build provider adapter
	engine := h.llamaEngine
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

	// Project memory (MEMORY.md)
	memStore := memory.NewProjectMemory(projectPath)

	// Credential store for secret resolution in tools
	credStore := sensitive.NewStore()
	if ghToken := os.Getenv("GITHUB_TOKEN"); ghToken != "" {
		credStore.Set("github", "token", ghToken)
	}

	// Approval handler via central approval manager (Respond endpoint)
	approvalHandler := &adapters.ApprovalHandler{Mgr: h.approvalMgr}

	cfg := orchestrator.Config{
		ProjectDir:    projectPath,
		SandboxID:     sandboxID,
		ContextSize:   32768,
		MaxToolRounds: 50,
		WorkDir:       "/sandbox",
		BashExecutor:  &bashExec,
		FileOps:       &fileOps,
		MemoryStore:   memStore,
		ApprovalHandler: approvalHandler,
		GitExecutor:     &adapters.GitExecutor{Git: h.git, RepoDir: projectPath},
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
			Message:   truncateStr(req.Message, 200),
			Tags:      observability.ClassifyTags(req.Message),
			Release:   h.langfuse.Release(),
			Env:       h.langfuse.Environment(),
		}
		extraHooks = append(extraHooks, observability.NewLangfuseHook(h.langfuse, modelName, traceCfg))
		h.log.Info("Langfuse tracing hook wired", zap.String("model", modelName), zap.Strings("tags", traceCfg.Tags))
	}
	cfg.ExtraHooks = extraHooks

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

	// Inject project memory prefix into the message
	var memoryPrefix string
	if mem := orch.Memory; mem != nil {
		memoryPrefix = mem.InjectPrefix()
	} else {
		memoryPrefix = memory.ReadMemoryFile(projectPath)
		if memoryPrefix != "" {
			memoryPrefix = "<memory>\n" + memoryPrefix + "\n</memory>\n\n"
		}
	}

	// Set up SSE
	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)

	spawnData, _ := json.Marshal(map[string]string{"agentId": req.AgentId})
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", string(core.EventTypeAgentSpawned), string(spawnData))
	if canFlush {
		flusher.Flush()
	}

	// Save user message
	if h.db != nil {
		if _, err := h.db.SaveUserMessage(r.Context(), req.Project, req.AgentId, req.Message); err != nil {
			h.log.Warn("Failed to save user message", zap.Error(err))
		}
	}

	orchMsg := memoryPrefix + "User request: " + req.Message

	// Event channel
	events := make(chan core.AgentEvent, 256)

	// Detached context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	var loopErr error

	go func() {
		defer close(done)
		defer close(events)
		loopErr = orch.Run(ctx, orchMsg, events)
	}()

	// Stream events
	var assistantText, assistantThinking string
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	llamaEvents := make(chan llama.AgentEvent, 256)
	go func() {
		defer close(llamaEvents)
		for evt := range events {
			switch evt.Type {
			case core.EventTypeTextDelta:
				assistantText += evt.Data.Text
			case core.EventTypeThinkingDelta:
				assistantThinking += evt.Data.Text
			}
			llamaEvents <- convertCoreEventToLlama(evt)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			for evt := range llamaEvents {
				h.writeLlamaSSE(w, evt, canFlush, flusher)
			}
			if h.db != nil {
				if _, err := h.db.SaveAssistantMessage(ctx, req.Project, req.AgentId, assistantText, assistantThinking, "[]"); err != nil {
					h.log.Warn("Failed to save assistant message", zap.Error(err))
				}
			}
			if loopErr != nil {
				h.log.Error("Orchestrator error", zap.Error(loopErr))
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if canFlush {
				flusher.Flush()
			}
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			if canFlush {
				flusher.Flush()
			}
		case evt, ok := <-llamaEvents:
			if !ok {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				if canFlush {
					flusher.Flush()
				}
				return
			}
			keepalive.Reset(15 * time.Second)
			h.writeLlamaSSE(w, evt, canFlush, flusher)
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
