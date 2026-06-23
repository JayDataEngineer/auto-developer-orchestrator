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
// ResolveAll runs once via sync.Once and caches the result.
//
// Invalidate(capName) is the Phase 3 hot-promotion path: HealthMonitor calls
// it when the active tier's prefix fails N consecutive probes. Invalidate
// re-probes just that capability and swaps the cached entry atomically. Worker
// prompts rebuild on the next LoadAgentRoles (invalidateCachesForResolve fires
// inside Invalidate).
//
// Capabilities without implementations[] are invisible to the resolver —
// they continue to flow through the legacy path (top-level tools/mcp_servers +
// SKILL.md). The resolver never breaks backward compatibility.
type CapabilityResolver struct {
	mu            sync.RWMutex
	once          sync.Once
	resolved      map[string]*Implementation
	prefixToCaps  map[string][]string // reverse index: MCP prefix → capability names that use it as active impl
	probed        int                 // test hook: number of probes executed during ResolveAll + Invalidate
	mc            *mcp.MultiClient
	httpClient    *http.Client
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
		r.resolved, r.prefixToCaps = r.resolveAllUncached()
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

// CapabilitiesForPrefix returns the capability names whose active impl uses
// the given MCP prefix. Used by HealthMonitor's hot-promotion path: when a
// prefix dies, it looks up which capabilities to invalidate.
//
// Returns nil if no capability uses the prefix, or if the resolver hasn't
// run yet. Safe to call before ResolveAll.
func (r *CapabilityResolver) CapabilitiesForPrefix(prefix string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.prefixToCaps[prefix]
}

// loadToolPackagesForTest, when non-nil, overrides LoadToolPackages inside
// Invalidate. Test seam so resolver tests don't need real capability.yaml
// files on disk. Production code leaves this nil.
var loadToolPackagesForTest func() map[string]*ToolPackage

// Invalidate re-probes ONE capability and swaps its cached active impl.
// Called by HealthMonitor when the active tier's prefix fails N consecutive
// probes — picks the next live tier (typically a pre-warmed self-hosted one)
// and rebuilds worker prompts on next LoadAgentRoles.
//
// Safe to call when the active impl hasn't actually changed — returns the
// (possibly same) impl without firing cache invalidation in that case.
//
// Holds the write lock for the duration of the re-probe. Invalidate is rare
// (tier death, ~once per 10min when something is actually wrong), so blocking
// Resolve callers for a few seconds is acceptable. Returns nil if the resolver
// hasn't run yet or the capability has no implementations[].
func (r *CapabilityResolver) Invalidate(capName string) *Implementation {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.resolved == nil {
		return nil
	}

	loader := LoadToolPackages
	if loadToolPackagesForTest != nil {
		loader = loadToolPackagesForTest
	}
	pkgs := loader()
	pkg, ok := pkgs[capName]
	if !ok || len(pkg.Implementations) == 0 {
		return nil
	}

	return r.invalidateOneLocked(capName, pkg)
}

// invalidateOneLocked is the testable core of Invalidate: re-probes one
// capability against the given pkg and swaps the cached entry. Caller MUST
// hold r.mu. Separated from Invalidate so tests can verify the swap logic
// without going through LoadToolPackages (which clears the package cache via
// invalidateCachesForResolve, fighting test stubs).
func (r *CapabilityResolver) invalidateOneLocked(capName string, pkg *ToolPackage) *Implementation {
	previous := r.resolved[capName]
	fresh := r.pickActive(pkg.Implementations)
	if previous != nil && fresh != nil && previous.Name == fresh.Name {
		// Same tier re-selected. Don't churn the cache or fire prompt rebuilds.
		return previous
	}

	r.removePrefixIndexLocked(capName, previous)
	r.resolved[capName] = fresh
	pkg.ActiveImpl = fresh
	r.addPrefixIndexLocked(capName, fresh)
	r.probed++

	invalidateCachesForResolve()

	return fresh
}

// removePrefixIndexLocked removes all reverse-index entries pointing at capName.
// Caller must hold r.mu.
func (r *CapabilityResolver) removePrefixIndexLocked(capName string, impl *Implementation) {
	if impl == nil || r.prefixToCaps == nil {
		return
	}
	for _, pfx := range prefixesFor(capName, impl) {
		caps := r.prefixToCaps[pfx]
		out := caps[:0]
		for _, c := range caps {
			if c != capName {
				out = append(out, c)
			}
		}
		if len(out) == 0 {
			delete(r.prefixToCaps, pfx)
		} else {
			r.prefixToCaps[pfx] = out
		}
	}
}

// addPrefixIndexLocked adds reverse-index entries for capName → impl's prefixes.
// Caller must hold r.mu.
func (r *CapabilityResolver) addPrefixIndexLocked(capName string, impl *Implementation) {
	if impl == nil {
		return
	}
	if r.prefixToCaps == nil {
		r.prefixToCaps = make(map[string][]string)
	}
	for _, pfx := range prefixesFor(capName, impl) {
		r.prefixToCaps[pfx] = append(r.prefixToCaps[pfx], capName)
	}
}

// ProbedCount returns the number of health probes executed during ResolveAll +
// Invalidate. Test hook.
func (r *CapabilityResolver) ProbedCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.probed
}

func (r *CapabilityResolver) resolveAllUncached() (map[string]*Implementation, map[string][]string) {
	out := make(map[string]*Implementation)
	prefixToCaps := make(map[string][]string)

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
		for _, pfx := range prefixesFor(name, active) {
			prefixToCaps[pfx] = append(prefixToCaps[pfx], name)
		}
	}

	r.mu.Lock()
	r.probed = len(out) // approximate; pickActive may short-circuit
	r.mu.Unlock()

	return out, prefixToCaps
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

// prefixesFor returns the MCP prefixes an Implementation exposes. The reverse
// index uses these so HealthMonitor can map a dying prefix back to a capability.
//
// For MCP tiers, the prefixes are impl.MCPServers (could be several — e.g., a
// research tier that wires both "web" and "media").
// For extension (self-hosted) tiers, the prefix follows the PreWarmer
// convention: "<capName>-<impl.Name>".
// Bash and HTTP tiers have no MCP prefix — they don't appear in the reverse
// index because HealthMonitor never sees them fail.
func prefixesFor(capName string, impl *Implementation) []string {
	if impl == nil {
		return nil
	}
	if len(impl.MCPServers) > 0 {
		out := make([]string, len(impl.MCPServers))
		copy(out, impl.MCPServers)
		return out
	}
	if impl.Type == "extension" && impl.Source != "" {
		return []string{capName + "-" + impl.Name}
	}
	return nil
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
