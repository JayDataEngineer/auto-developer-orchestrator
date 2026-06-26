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
	// Clients with a fallback endpoint take a separate probe path that also
	// checks the inactive endpoint for switch opportunities. This is the
	// load-bearing extension: a single-URL client can't self-heal; a
	// fallback-configured client can.
	if client.HasFallback() {
		h.probeOneWithFallback(ctx, prefix, client)
		return
	}
	h.probeOneSimple(ctx, prefix, client)
}

// probeOneSimple is the legacy probe path for clients without a fallback
// endpoint. Behavior is identical to the pre-fallback probeOne: count
// consecutive failures, fire onTierDeath at threshold, restart extension if
// a restarter is wired.
func (h *HealthMonitor) probeOneSimple(ctx context.Context, prefix string, client *Client) {
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

// probeOneWithFallback is the dual-endpoint probe path. Every tick:
//  1. Probe the active endpoint.
//  2. If active is up, no action — same as the simple path.
//  3. If active is down, probe the inactive endpoint (recovery opportunity).
//  4. If inactive is up, switch to it (primary→fallback on primary death;
//     fallback→primary on primary recovery).
//  5. If both are down, increment fail count and fire onTierDeath at threshold.
//
// The fail-count threshold (maxFailures, default 3) applies only to the
// both-down case. A single failed active probe doesn't escalate to
// onTierDeath when fallback absorbs the load — MarkUnavailable is reserved
// for the genuine "neither endpoint works" state.
func (h *HealthMonitor) probeOneWithFallback(ctx context.Context, prefix string, client *Client) {
	active := client.ActiveEndpoint()

	probeCtx, cancel := context.WithTimeout(ctx, h.probeTimeout)
	activeUp := h.probeEndpointURL(probeCtx, client, active)
	cancel()

	if activeUp {
		h.mu.Lock()
		wasFailing := h.failCounts[prefix] > 0 || h.multi.IsUnavailable(prefix)
		h.failCounts[prefix] = 0
		if wasFailing {
			h.logger.Info("MCP health recovered", zap.String("prefix", prefix))
			h.multi.MarkAvailable(prefix)
			_ = h.multi.RefreshTools(ctx)
		}
		h.mu.Unlock()
		return
	}

	// Active is down. Probe the inactive endpoint for a switch opportunity.
	inactive := client.PrimaryEndpoint()
	if active == client.PrimaryEndpoint() {
		inactive = client.FallbackEndpoint()
	}

	probeCtx2, cancel2 := context.WithTimeout(ctx, h.probeTimeout)
	inactiveUp := h.probeEndpointURL(probeCtx2, client, inactive)
	cancel2()

	if inactiveUp {
		reason := "primary down"
		if active == client.FallbackEndpoint() {
			reason = "primary recovered"
		}
		if swErr := client.SwitchEndpoint(inactive, reason); swErr != nil {
			h.logger.Error("MCP health switch failed",
				zap.String("prefix", prefix),
				zap.String("from", active),
				zap.String("to", inactive),
				zap.Error(swErr))
			h.escalateBothDown(ctx, prefix, nil)
			return
		}
		h.mu.Lock()
		h.failCounts[prefix] = 0
		h.multi.MarkAvailable(prefix)
		_ = h.multi.RefreshTools(ctx)
		h.mu.Unlock()
		h.logger.Info("MCP health switched endpoint",
			zap.String("prefix", prefix),
			zap.String("from", active),
			zap.String("to", inactive),
			zap.String("reason", reason))
		return
	}

	// Both down — escalate through the failure-counter path.
	h.escalateBothDown(ctx, prefix, nil)
}

// escalateBothDown advances the failure counter and, at threshold, fires
// onTierDeath + tryRestart. Shared between the simple and fallback paths
// when neither endpoint is reachable. The extra err param is for log context
// — the active probe's error is meaningful even when both are down.
func (h *HealthMonitor) escalateBothDown(ctx context.Context, prefix string, _ error) {
	h.mu.Lock()
	h.failCounts[prefix]++
	h.logger.Warn("MCP health probe failed",
		zap.String("prefix", prefix),
		zap.Int("consecutive_failures", h.failCounts[prefix]))

	if h.failCounts[prefix] < h.maxFailures {
		h.mu.Unlock()
		return
	}

	h.multi.MarkUnavailable(prefix)
	if h.onTierDeath != nil {
		h.logger.Info("MCP health: firing tier-death callback",
			zap.String("prefix", prefix))
		h.mu.Unlock()
		h.onTierDeath(prefix)
		h.mu.Lock()
	}
	h.mu.Unlock()
	if h.restarter != nil {
		h.tryRestart(ctx, prefix)
	}
}

// probeEndpointURL probes a specific URL via a one-shot client. Returns true
// if the URL responded to initialize + tools/list without error. Used by the
// fallback-aware probe path to check both endpoints.
func (h *HealthMonitor) probeEndpointURL(ctx context.Context, client *Client, url string) bool {
	_, err := client.ProbeEndpoint(ctx, url)
	return err == nil
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
