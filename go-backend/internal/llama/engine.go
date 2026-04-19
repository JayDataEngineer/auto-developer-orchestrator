package llama

import (
	"fmt"
	"sync"
	"time"

	llamago "github.com/tcpipuk/llama-go"
	"go.uber.org/zap"
)

// Engine is a singleton that manages the loaded llama.cpp model.
// It is thread-safe — the Model can be shared across goroutines,
// while Contexts are created per-agent and used from a single goroutine.
type Engine struct {
	mu        sync.RWMutex
	model     *llamago.Model
	modelPath string
	loaded    bool
	loadDur   time.Duration
	logger    *zap.Logger
}

// EngineConfig holds configuration for creating a new Engine.
type EngineConfig struct {
	ModelPath string
	Logger    *zap.Logger
}

// NewEngine creates a new Engine (model not yet loaded).
func NewEngine(cfg EngineConfig) *Engine {
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	return &Engine{
		modelPath: cfg.ModelPath,
		logger:    cfg.Logger,
	}
}

// LoadModel loads the GGUF model file into GPU memory.
// This is expensive (~9 min cold, ~3.6s warm from page cache) and should be called once at startup.
func (e *Engine) LoadModel() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.loaded {
		return fmt.Errorf("model already loaded")
	}

	e.logger.Info("Loading model via llama-go (CGO library mode)",
		zap.String("path", e.modelPath),
	)

	t0 := time.Now()
	model, err := llamago.LoadModel(e.modelPath, llamago.WithGPULayers(-1))
	if err != nil {
		return fmt.Errorf("failed to load model: %w", err)
	}

	e.model = model
	e.loaded = true
	e.loadDur = time.Since(t0)

	e.logger.Info("Model loaded successfully",
		zap.Duration("duration", e.loadDur),
	)

	return nil
}

// NewSession creates a new inference session (Context) for a single agent.
// Each session holds its own KV cache in VRAM.
// The caller must call Session.Close() to release VRAM when done.
func (e *Engine) NewSession(ctxSize int) (*Session, error) {
	e.mu.RLock()
	model := e.model
	e.mu.RUnlock()

	if !e.loaded || model == nil {
		return nil, fmt.Errorf("model not loaded")
	}

	ctx, err := model.NewContext(
		llamago.WithContext(ctxSize),
		llamago.WithPrefixCaching(true),
		llamago.WithBatch(cfg.BatchSize),
		llamago.WithF16Memory(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create context: %w", err)
	}

	return &Session{
		ctx:    ctx,
		engine: e,
		history: []Turn{},
	}, nil
}

// IsLoaded returns whether the model has been loaded.
func (e *Engine) IsLoaded() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.loaded
}

// LoadDuration returns how long the model took to load.
func (e *Engine) LoadDuration() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.loadDur
}

// Close unloads the model and frees GPU memory.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.model != nil {
		e.model.Close()
		e.model = nil
		e.loaded = false
		e.logger.Info("Model unloaded")
	}
	return nil
}
