package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ExtensionRestarter is the narrow surface HealthMonitor needs from the
// extension manager: restart by prefix, then learn the new port so it can
// re-register the client. Decoupled as an interface so HealthMonitor tests
// can supply a fake without spinning up real subprocesses.
type ExtensionRestarter interface {
	Restart(ctx context.Context, prefix string) (int, error)
	PortFor(prefix string) int
}

// HealthMonitor pings every registered MCP client on a fixed interval.
// On N consecutive failures it:
//   - marks the prefix unavailable (advisory — CallTool still tries)
//   - fires the optional onTierDeath callback (Phase 3 hot-promotion path —
//     typically calls resolver.Invalidate so the next session picks a fallback)
//   - for extension-backed clients, asks the ExtensionRestarter to restart
//   - on recovery, calls MultiClient.RefreshTools so the tool map is fresh
//
// The monitor owns "is this server alive right now?" Hot-promotion wiring
// is delegated to the onTierDeath callback so this package doesn't depend on
// the resolver.
type HealthMonitor struct {
	multi       *MultiClient
	restarter   ExtensionRestarter
	onTierDeath func(prefix string) // Phase 3: hot-promotion hook. Nil = disabled.
	logger      *zap.Logger

	interval     time.Duration
	probeTimeout time.Duration
	maxFailures  int

	mu         sync.Mutex
	failCounts map[string]int
	cancel     context.CancelFunc
}

// HealthMonitorOption configures a HealthMonitor at construction time.
type HealthMonitorOption func(*HealthMonitor)

// WithHealthInterval overrides the default 60s probe interval.
func WithHealthInterval(d time.Duration) HealthMonitorOption {
	return func(h *HealthMonitor) {
		if d > 0 {
			h.interval = d
		}
	}
}

// WithProbeTimeout overrides the default 5s per-probe timeout.
func WithProbeTimeout(d time.Duration) HealthMonitorOption {
	return func(h *HealthMonitor) {
		if d > 0 {
			h.probeTimeout = d
		}
	}
}

// WithMaxFailures overrides the default 3-consecutive-failure threshold.
func WithMaxFailures(n int) HealthMonitorOption {
	return func(h *HealthMonitor) {
		if n > 0 {
			h.maxFailures = n
		}
	}
}

// WithTierDeathCallback installs a hot-promotion hook (Phase 3). When a prefix
// crosses the maxFailures threshold, the callback fires with the prefix. The
// callback typically calls resolver.CapabilitiesForPrefix + resolver.Invalidate
// so the next agent session picks up the fallback tier. Errors inside the
// callback are the caller's problem — HealthMonitor logs but otherwise ignores
// them. Nil callback = hot-promotion disabled.
func WithTierDeathCallback(fn func(prefix string)) HealthMonitorOption {
	return func(h *HealthMonitor) {
		h.onTierDeath = fn
	}
}

// NewHealthMonitor constructs a monitor. The restarter may be nil — then
// failed clients are flagged unavailable but never restarted (used in tests
// and in deployments that don't use extension subprocesses).
func NewHealthMonitor(multi *MultiClient, restarter ExtensionRestarter, logger *zap.Logger, opts ...HealthMonitorOption) *HealthMonitor {
	if logger == nil {
		logger = zap.NewNop()
	}
	h := &HealthMonitor{
		multi:        multi,
		restarter:    restarter,
		logger:       logger,
		interval:     60 * time.Second,
		probeTimeout: 5 * time.Second,
		maxFailures:  3,
		failCounts:   make(map[string]int),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Start spawns the background probe goroutine. The returned stop function
// cancels the loop and is safe to call multiple times.
func (h *HealthMonitor) Start(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	h.cancel = cancel
	go h.loop(ctx)
	return func() {
		cancel()
		h.mu.Lock()
		h.cancel = nil
		h.mu.Unlock()
	}
}

// Stop cancels the loop. Safe to call from any goroutine.
func (h *HealthMonitor) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
}

func (h *HealthMonitor) loop(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.probeAll(ctx)
		}
	}
}

// probeAll pings every registered prefix and tracks fail counts. Exposed so
// tests can drive a probe cycle synchronously without waiting on the ticker.
func (h *HealthMonitor) probeAll(ctx context.Context) {
	if h.multi == nil {
		return
	}
	for _, prefix := range h.multi.Prefixes() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		h.probeOne(ctx, prefix)
	}
}

func (h *HealthMonitor) probeOne(ctx context.Context, prefix string) {
	client := h.multi.ClientForPrefix(prefix)
	if client == nil {
		return // registered then removed — skip
	}
	probeCtx, cancel := context.WithTimeout(ctx, h.probeTimeout)
	_, err := client.ListTools(probeCtx)
	cancel()

	h.mu.Lock()
	defer h.mu.Unlock()

	if err == nil {
		wasFailing := h.failCounts[prefix] > 0 || h.multi.IsUnavailable(prefix)
		h.failCounts[prefix] = 0
		// Only clear the flag + refresh tools when we actually recovered.
		// Calling RefreshTools on every steady-state success would re-list every
		// client every tick — noisy and O(N²) over time.
		if wasFailing {
			h.logger.Info("MCP health recovered", zap.String("prefix", prefix))
			h.multi.MarkAvailable(prefix)
			_ = h.multi.RefreshTools(ctx)
		}
		return
	}

	h.failCounts[prefix]++
	h.logger.Warn("MCP health probe failed",
		zap.String("prefix", prefix),
		zap.Int("consecutive_failures", h.failCounts[prefix]),
		zap.Error(err))

	if h.failCounts[prefix] >= h.maxFailures {
		h.multi.MarkUnavailable(prefix)
		// Phase 3 hot-promotion: fire the onTierDeath callback first. The
		// callback typically calls resolver.Invalidate which re-probes the
		// capability — if a pre-warmed fallback exists, it becomes the active
		// tier for the next session. The restart path below is the fallback
		// when no resolver is wired OR the prefix isn't tied to a polymorphic
		// capability.
		if h.onTierDeath != nil {
			h.logger.Info("MCP health: firing tier-death callback",
				zap.String("prefix", prefix))
			// Drop the lock for the callback — it may do slow work (resolver
			// re-probes, HTTP health checks) and we don't want to block other
			// prefixes' probes behind it.
			h.mu.Unlock()
			h.onTierDeath(prefix)
			h.mu.Lock()
		}
		if h.restarter != nil {
			h.tryRestart(ctx, prefix)
		}
	}
}

func (h *HealthMonitor) tryRestart(ctx context.Context, prefix string) {
	port, err := h.restarter.Restart(ctx, prefix)
	if err != nil {
		h.logger.Error("MCP health: extension restart declined",
			zap.String("prefix", prefix), zap.Error(err))
		return
	}
	// Build the new endpoint URL from the restarted port and re-register the client.
	// The old client is replaced; RefreshTools on the next successful probe picks
	// up the new tool list.
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
	h.multi.AddClient(prefix, NewClient(prefix, endpoint, h.logger))
	// Reset fail count so we don't immediately re-trigger restart on the next tick.
	h.failCounts[prefix] = 0
	h.logger.Info("MCP health: extension restarted",
		zap.String("prefix", prefix), zap.Int("port", port))
}

// FailCount returns the current consecutive-failure count for a prefix.
// Test helper — not part of the stable surface.
func (h *HealthMonitor) FailCount(prefix string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failCounts[prefix]
}
