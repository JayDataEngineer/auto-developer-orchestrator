package common

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/mcp"
)

// CapabilityResolver resolves polymorphic capabilities (those with
// implementations[]) at boot to a single active implementation per capability,
// based on health-check probes. Sticky for the kernel process lifetime:
// the resolver runs once and caches the result. Re-running ResolveAll returns
// the cached map without re-probing.
//
// Capabilities without implementations[] are invisible to the resolver —
// they continue to flow through the legacy path (top-level tools/mcp_servers +
// SKILL.md). The resolver never breaks backward compatibility.
type CapabilityResolver struct {
	mu         sync.RWMutex
	once       sync.Once
	resolved   map[string]*Implementation
	probed     int // test hook: number of probes executed during ResolveAll
	mc         *mcp.MultiClient
	httpClient *http.Client
}

// NewResolver wires the resolver to the kernel's MCP MultiClient (may be nil —
// mcp-available probes return false in that case) and a short-timeout HTTP
// client for http-get probes (mirrors services/cluster.go:172).
func NewResolver(mc *mcp.MultiClient) *CapabilityResolver {
	return &CapabilityResolver{
		mc: mc,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// SetGlobalResolver installs the process-wide resolver. Called once at boot
// after the MCP MultiClient is initialized. Tests may swap it; production code
// reads via GetGlobalResolver.
func SetGlobalResolver(r *CapabilityResolver) {
	globalResolver = r
}

// GetGlobalResolver returns the active resolver, or nil if none has been set.
// Callers must nil-check.
func GetGlobalResolver() *CapabilityResolver {
	return globalResolver
}

var globalResolver *CapabilityResolver

// ResolveAll probes every polymorphic capability exactly once (sync.Once) and
// returns the map of capability-name → active Implementation. The result is
// cached for the kernel lifetime; subsequent calls return the same map without
// re-probing.
//
// Before probing, the resolver invalidates the role + tool-package caches so
// the next LoadAgentRoles() rebuilds worker prompts against the freshly-set
// ActiveImpl fields. Without this invalidation, prompts cached at first load
// would leak through (Risk #2 in the Stage 2 RFC).
func (r *CapabilityResolver) ResolveAll() map[string]*Implementation {
	r.once.Do(func() {
		r.resolved = r.resolveAllUncached()
	})
	return r.resolved
}

// Resolve returns the active Implementation for a capability name, or nil if
// the capability has no implementations[] or no resolver has been set. Safe
// to call before ResolveAll — returns nil.
func (r *CapabilityResolver) Resolve(capName string) *Implementation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.resolved == nil {
		return nil
	}
	return r.resolved[capName]
}

// ProbedCount returns the number of health probes executed during ResolveAll.
// Test hook — confirms sync.Once caching prevents re-probing on second call.
func (r *CapabilityResolver) ProbedCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.probed
}

func (r *CapabilityResolver) resolveAllUncached() map[string]*Implementation {
	out := make(map[string]*Implementation)

	// Invalidate caches so worker prompts rebuild against ActiveImpl. See
	// Risk #2 in the Stage 2 RFC. We do this BEFORE touching ActiveImpl so
	// there's no window where a concurrent reader sees half-resolved state.
	invalidateCachesForResolve()

	pkgs := LoadToolPackages()
	for name, pkg := range pkgs {
		if len(pkg.Implementations) == 0 {
			continue
		}
		active := r.pickActive(pkg.Implementations)
		out[name] = active
		pkg.ActiveImpl = active
	}

	r.mu.Lock()
	r.probed = len(out) // approximate; pickActive may short-circuit
	r.mu.Unlock()

	return out
}

// pickActive sorts implementations by priority, returns the first live one,
// or falls back to the highest-priority impl if none are live (degraded > broken).
// Mutates nothing — the caller assigns the returned pointer to pkg.ActiveImpl.
func (r *CapabilityResolver) pickActive(impls []Implementation) *Implementation {
	sorted := make([]Implementation, len(impls))
	copy(sorted, impls)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	for i := range sorted {
		impl := &sorted[i]
		if r.probe(impl.Health) {
			return impl
		}
	}

	// Fallback: highest-priority impl. Defensive copy so callers can mutate
	// without affecting the slice we were handed.
	first := sorted[0]
	return &first
}

// probe returns true if the health check passes. Unknown kinds default to
// false (safer — don't activate a tier we can't verify).
func (r *CapabilityResolver) probe(h HealthCheck) bool {
	switch h.Kind {
	case "always-true":
		return true
	case "mcp-available":
		if r.mc == nil {
			return false
		}
		c := r.mc.ClientForPrefix(h.Server)
		if c == nil {
			return false
		}
		return c.IsAvailable()
	case "http-get":
		if r.httpClient == nil || h.URL == "" {
			return false
		}
		resp, err := r.httpClient.Get(h.URL)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode < 500
	default:
		return false
	}
}

// invalidateCachesForResolve clears the role + tool-package caches so the
// next LoadAgentRoles() reads ActiveImpl and rebuilds prompts. Must be called
// before assigning ActiveImpl on any package.
func invalidateCachesForResolve() {
	toolPkgMu.Lock()
	toolPackages = nil
	toolPkgModTime = map[string]time.Time{}
	toolPkgMu.Unlock()

	agentMu.Lock()
	agentRoles = nil
	agentModTime = map[string]time.Time{}
	agentMu.Unlock()
}
