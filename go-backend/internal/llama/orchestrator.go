package llama

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// OrchestratorLoop manages the top-level planning and delegation loop.
// It owns the orchestrator's AgentLoop (KV session) and the ArtifactRegistry.
// Sub-agents are created on demand, run synchronously, and freed after yielding.
type OrchestratorLoop struct {
	engine    ChatProvider
	artifacts *ArtifactRegistry
	executor  *OrchestratorExecutor
	loop      *AgentLoop
	cfg       PersonaConfig
	logger    *zap.Logger
	saver     TranscriptSaver

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	plan    *Plan
}

// OrchestratorConfig holds configuration for the orchestrator.
type OrchestratorConfig struct {
	ProjectDir  string
	SandboxID   string
	ContextSize int // 0 = use ModelConfig default (32K)
}

// NewOrchestratorLoop creates a new orchestrator with a fresh KV session.
func NewOrchestratorLoop(
	engine ChatProvider,
	baseExecutor ToolExecutor, // SandboxToolExecutor for sub-agent tool dispatch
	ocfg OrchestratorConfig,
	logger *zap.Logger,
) (*OrchestratorLoop, error) {
	if !engine.IsLoaded() {
		return nil, fmt.Errorf("engine model not loaded")
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	personaCfg := PersonaConfig{
		ProjectDir: ocfg.ProjectDir,
		SandboxID:  ocfg.SandboxID,
	}

	artifacts := NewArtifactRegistry()
	persona := NewOrchestratorPersona(personaCfg)

	ctxSize := ocfg.ContextSize
	if ctxSize == 0 {
		ctxSize = cfg.DefaultContextSize
	}

	orchestratorExec := &OrchestratorExecutor{
		engine:       engine,
		artifacts:    artifacts,
		personaCfg:   personaCfg,
		baseExecutor: baseExecutor,
		logger:       logger,
	}

	loopCfg := AgentLoopConfig{
		SystemPrompt:  persona.SystemPrompt,
		MaxToolRounds: persona.MaxToolRounds,
		MaxTokens:     persona.MaxTokens,
		ContextSize:   ctxSize,
		Tools:         PersonaOpenAITools(PersonaOrchestrator),
		Compaction:    DefaultCompactionConfig(),
		Opts: GenerateOptions{
			MaxTokens:   persona.MaxTokens,
			Temperature: persona.Temperature,
			TopP:        cfg.TopP,
			TopK:        cfg.TopK,
		},
	}

	loop, err := NewAgentLoop(engine, orchestratorExec, loopCfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestrator loop: %w", err)
	}

	orchestratorExec.subscriber = nil // set in Run/Continue

	return &OrchestratorLoop{
		engine:    engine,
		artifacts: artifacts,
		executor:  orchestratorExec,
		loop:      loop,
		cfg:       personaCfg,
		logger:    logger,
	}, nil
}

// SetTranscriptSaver configures the transcript saver for pre-compaction snapshots.
// Propagates to the orchestrator's own loop. Sub-agents get it via their own creation.
func (o *OrchestratorLoop) SetTranscriptSaver(saver TranscriptSaver) {
	o.saver = saver
	if o.loop != nil {
		o.loop.SetTranscriptSaver(saver)
	}
}

// SetMemory configures the project memory for persistent MEMORY.md storage.
func (o *OrchestratorLoop) SetMemory(memory *ProjectMemory) {
	if o.executor != nil {
		o.executor.memory = memory
	}
}

// Memory returns the project memory instance.
func (o *OrchestratorLoop) Memory() *ProjectMemory {
	if o.executor != nil {
		return o.executor.memory
	}
	return nil
}

// SetApprovalManager configures the approval manager for plan approval.
func (o *OrchestratorLoop) SetApprovalManager(mgr ApprovalManager) {
	if o.executor != nil {
		o.executor.approvalMgr = mgr
	}
}

// Run starts the orchestrator for a user prompt. Blocks until complete.
// Events (including sub-agent events) are emitted to the subscriber channel.
func (o *OrchestratorLoop) Run(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator already running")
	}
	o.running = true
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		o.running = false
		o.mu.Unlock()
	}()

	o.executor.subscriber = subscriber
	return o.loop.Run(ctx, userMsg, subscriber)
}

// Continue sends a follow-up message to the orchestrator session.
func (o *OrchestratorLoop) Continue(ctx context.Context, userMsg string, subscriber chan<- AgentEvent) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator already running")
	}
	o.running = true
	o.mu.Unlock()

	defer func() {
		o.mu.Lock()
		o.running = false
		o.mu.Unlock()
	}()

	o.executor.subscriber = subscriber
	return o.loop.Continue(ctx, userMsg, subscriber)
}

// Close releases the orchestrator's KV session.
func (o *OrchestratorLoop) Close() error {
	if o.cancel != nil {
		o.cancel()
	}
	return o.loop.Close()
}

// Artifacts returns the artifact registry for inspection.
func (o *OrchestratorLoop) Artifacts() *ArtifactRegistry {
	return o.artifacts
}

// Plan returns the current execution plan (nil if not yet created).
func (o *OrchestratorLoop) Plan() *Plan {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.plan
}

// IsRunning returns whether the orchestrator is currently active.
func (o *OrchestratorLoop) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.running
}
