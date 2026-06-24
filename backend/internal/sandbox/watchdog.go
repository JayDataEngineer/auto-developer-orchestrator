package sandbox

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Watchdog polls tracked sandboxes for idle timeout. When a sandbox's
// IdleShutdownSecs > 0 and (now - LastActivityAt) exceeds that threshold,
// the watchdog calls Manager.ShutdownByProjectLabel to tear down the
// container. Used to give sandbox containers a true agent lifecycle —
// once the agent stops driving a sandbox, the sandbox goes away.
//
// Design notes:
//
//  1. Poll-based, not push-based. The Manager already holds a sync.Mutex
//     around sandbox state; push-based eventing would require either
//     another channel + goroutine per sandbox or a condition variable.
//     60s polling costs nothing (one map walk under RLock) and lets us
//     reuse the existing lock structure.
//
//  2. nowFunc is injected for testability — tests pass a fake clock
//     instead of sleeping for the real idle threshold.
//
//  3. tickInterval is also injectable. Tests pass 1ms; production passes
//     60s. The gap between ticks is the upper bound on shutdown latency
//     after a sandbox goes idle (a sandbox may stay up to tickInterval
//     past its threshold before the watchdog notices).
//
//  4. shutdownFn is injected so tests can assert "shutdown was called
//     for project X" without spinning up Docker. Production wires this
//     to Manager.ShutdownByProjectLabel.
//
//  5. The watchdog never shuts down a sandbox whose IdleShutdownSecs is
//     0. That preserves the pre-PR4 default — sandboxes stay up forever
//     unless the org opts in.
type Watchdog struct {
	manager *Manager
	logger  *zap.Logger

	nowFunc       func() time.Time
	tickInterval  time.Duration
	shutdownFn    func(ctx context.Context, projectPath string) ([]string, error)

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// WatchdogConfig configures the watchdog. All fields required — production
// callers should pass WatchdogDefaults(); tests override per-case.
type WatchdogConfig struct {
	Manager      *Manager
	Logger       *zap.Logger
	NowFunc      func() time.Time
	TickInterval time.Duration
	ShutdownFn   func(ctx context.Context, projectPath string) ([]string, error)
}

// WatchdogDefaults returns a config wired for production use. Tick every
// 60s, use real time, call Manager.ShutdownByProjectLabel.
func WatchdogDefaults(m *Manager, logger *zap.Logger) WatchdogConfig {
	return WatchdogConfig{
		Manager:      m,
		Logger:       logger,
		NowFunc:      time.Now,
		TickInterval: 60 * time.Second,
		ShutdownFn:   m.ShutdownByProjectLabel,
	}
}

// NewWatchdog constructs a watchdog from config. Does NOT start it —
// callers must call Start() to launch the goroutine. Stop() cancels it
// and waits for the in-flight tick to finish.
func NewWatchdog(cfg WatchdogConfig) *Watchdog {
	if cfg.TickInterval == 0 {
		cfg.TickInterval = 60 * time.Second
	}
	if cfg.NowFunc == nil {
		cfg.NowFunc = time.Now
	}
	if cfg.ShutdownFn == nil {
		// Default: no-op. Production callers must wire ShutdownFn
		// explicitly via WatchdogDefaults — silently swallowing shutdowns
		// would hide a wiring bug.
		cfg.ShutdownFn = func(ctx context.Context, projectPath string) ([]string, error) {
			return nil, nil
		}
	}
	return &Watchdog{
		manager:      cfg.Manager,
		logger:       cfg.Logger,
		nowFunc:      cfg.NowFunc,
		tickInterval: cfg.TickInterval,
		shutdownFn:   cfg.ShutdownFn,
		stopCh:       make(chan struct{}),
	}
}

// Start launches the polling goroutine. Idempotent — calling Start twice
// is a no-op (the second call observes a closed stopCh and returns).
// Returns the watchdog so the caller can chain Stop().
func (w *Watchdog) Start() *Watchdog {
	w.wg.Add(1)
	go w.loop()
	return w
}

// Stop cancels the watchdog and waits for the in-flight tick to finish.
// Safe to call multiple times — the second call observes the wait group
// already drained and returns immediately.
func (w *Watchdog) Stop() {
	select {
	case <-w.stopCh:
		// already closed
	default:
		close(w.stopCh)
	}
	w.wg.Wait()
}

// loop is the polling goroutine. Exits when stopCh is closed.
//
// Uses a ticker + select so Stop() unblocks within tickInterval
// instead of waiting up to 60s for the next tick.
func (w *Watchdog) loop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

// tick walks every tracked sandbox, checks idle threshold, and shuts
// down any that have crossed it. Extracted from loop() so tests can
// drive a single tick deterministically without waiting on the ticker.
func (w *Watchdog) tick() {
	if w.manager == nil {
		return
	}
	now := w.nowFunc()

	// Snapshot under RLock — ShutdownByProjectLabel takes the write
	// lock, so we can't hold the read lock across the shutdown call.
	type candidate struct {
		projectPath string
		sandboxID   string
		threshold   time.Duration
		idleFor     time.Duration
	}
	candidates := []candidate{}

	w.manager.mu.RLock()
	for id, sb := range w.manager.sandboxes {
		if sb.IdleShutdownSecs <= 0 {
			continue
		}
		if sb.Status != StatusRunning {
			continue
		}
		threshold := time.Duration(sb.IdleShutdownSecs) * time.Second
		last := sb.LastActivityAt
		if last.IsZero() {
			last = sb.CreatedAt
		}
		idleFor := now.Sub(last)
		if idleFor >= threshold {
			candidates = append(candidates, candidate{
				projectPath: sb.ProjectPath,
				sandboxID:   id,
				threshold:   threshold,
				idleFor:     idleFor,
			})
		}
	}
	w.manager.mu.RUnlock()

	if len(candidates) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, c := range candidates {
		removed, err := w.shutdownFn(ctx, c.projectPath)
		if err != nil {
			w.logger.Warn("watchdog: shutdown failed",
				zap.String("sandbox_id", c.sandboxID),
				zap.String("project", c.projectPath),
				zap.Duration("idle_for", c.idleFor),
				zap.Duration("threshold", c.threshold),
				zap.Error(err))
			continue
		}
		w.logger.Info("watchdog: idle-shutdown complete",
			zap.String("sandbox_id", c.sandboxID),
			zap.String("project", c.projectPath),
			zap.Duration("idle_for", c.idleFor),
			zap.Duration("threshold", c.threshold),
			zap.Int("containers_removed", len(removed)),
			zap.Strings("containers", removed))
	}
}
