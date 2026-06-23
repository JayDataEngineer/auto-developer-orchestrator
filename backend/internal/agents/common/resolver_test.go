package common

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResolverPicksHighestPriorityLive — two impls, both live, must return
// the lower-priority-number one (priority 1 wins over priority 99).
func TestResolverPicksHighestPriorityLive(t *testing.T) {
	r := NewResolver(nil)
	impls := []Implementation{
		{Name: "bash", Priority: 99, Health: HealthCheck{Kind: "always-true"}},
		{Name: "cloud", Priority: 1, Health: HealthCheck{Kind: "always-true"}},
	}
	got := r.pickActive(impls)
	if got.Name != "cloud" {
		t.Errorf("expected cloud (priority 1), got %q", got.Name)
	}
}

// TestResolverFallsBackWhenPrimaryDown — priority-1 mcp-available probe fails,
// priority-99 always-true wins. This is the RFC's primary scenario: cloud MCP
// down → bash-ddg floor.
func TestResolverFallsBackWhenPrimaryDown(t *testing.T) {
	r := NewResolver(nil) // nil MultiClient → mcp-available always returns false
	impls := []Implementation{
		{Name: "cloud", Priority: 1, Health: HealthCheck{Kind: "mcp-available", Server: "web"}},
		{Name: "bash-ddg", Priority: 99, Health: HealthCheck{Kind: "always-true"}},
	}
	got := r.pickActive(impls)
	if got.Name != "bash-ddg" {
		t.Errorf("expected bash-ddg fallback, got %q", got.Name)
	}
}

// TestResolverHealthHttpGet2xx — http-get against a 200 endpoint is true.
func TestResolverHealthHttpGet2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	r := NewResolver(nil)
	got := r.probe(HealthCheck{Kind: "http-get", URL: srv.URL})
	if !got {
		t.Errorf("expected http-get probe true for 200 response")
	}
}

// TestResolverHealthHttpGet5xx — http-get against a 500 endpoint is false.
func TestResolverHealthHttpGet5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	r := NewResolver(nil)
	got := r.probe(HealthCheck{Kind: "http-get", URL: srv.URL})
	if got {
		t.Errorf("expected http-get probe false for 500 response")
	}
}

// TestResolverHealthHttpGetUnreachable — http-get against a closed port is
// false. Catches the panic-on-nil-Response case (resp.Body must be guarded).
func TestResolverHealthHttpGetUnreachable(t *testing.T) {
	r := NewResolver(nil)
	got := r.probe(HealthCheck{Kind: "http-get", URL: "http://127.0.0.1:1/"})
	if got {
		t.Errorf("expected http-get probe false for unreachable URL")
	}
}

// TestResolverAlwaysTrueFloor — a single always-true impl is always live,
// regardless of MultiClient state. This is the floor tier guarantee.
func TestResolverAlwaysTrueFloor(t *testing.T) {
	r := NewResolver(nil)
	impls := []Implementation{
		{Name: "bash-ddg", Priority: 99, Health: HealthCheck{Kind: "always-true"}},
	}
	got := r.pickActive(impls)
	if got.Name != "bash-ddg" {
		t.Errorf("expected always-true to be live, got %q", got.Name)
	}
}

// TestResolverUnknownHealthKindIsFalse — unknown health kind defaults to
// false. Safer to deactivate a tier we can't verify than to activate it blind.
func TestResolverUnknownHealthKindIsFalse(t *testing.T) {
	r := NewResolver(nil)
	if r.probe(HealthCheck{Kind: "totally-made-up"}) {
		t.Error("unknown health kind should return false")
	}
}

// TestResolverAllDownFallsBackToPriority — if every probe fails, the resolver
// still returns the highest-priority impl rather than nil. Degraded > broken.
func TestResolverAllDownFallsBackToPriority(t *testing.T) {
	r := NewResolver(nil)
	impls := []Implementation{
		{Name: "bash-ddg", Priority: 99, Health: HealthCheck{Kind: "mcp-available", Server: "x"}},
		{Name: "cloud", Priority: 1, Health: HealthCheck{Kind: "mcp-available", Server: "y"}},
	}
	got := r.pickActive(impls)
	if got == nil {
		t.Fatal("expected non-nil fallback impl")
	}
	if got.Name != "cloud" {
		t.Errorf("expected fallback to priority-1 cloud, got %q", got.Name)
	}
}

// TestResolverCachesAcrossCalls — ResolveAll is sync.Once; calling twice
// must not re-probe. Verified via ProbedCount staying constant.
func TestResolverCachesAcrossCalls(t *testing.T) {
	r := NewResolver(nil)
	// Stub: inject a polymorphic capability manually so ResolveAll has work
	// to do. We bypass the package cache because that's orthogonal to the
	// caching-under-test here.
	r.once.Do(func() {
		r.resolved = map[string]*Implementation{
			"fake": {Name: "stub", Priority: 1, Health: HealthCheck{Kind: "always-true"}},
		}
		r.probed = 999
	})
	out1 := r.ResolveAll()
	out2 := r.ResolveAll()
	if len(out1) != len(out2) {
		t.Errorf("ResolveAll returned different sizes across calls: %d vs %d", len(out1), len(out2))
	}
	if r.ProbedCount() != 999 {
		t.Errorf("sync.Once violated: probed count changed across ResolveAll calls")
	}
}

// TestGlobalResolverNilSafe — GetGlobalResolver returns nil when no resolver
// has been set. Callers must nil-check; we verify the default is nil so they
// don't accidentally rely on a zero-value non-nil resolver.
func TestGlobalResolverNilSafe(t *testing.T) {
	// Save and restore the global state so other tests aren't polluted.
	prev := globalResolver
	t.Cleanup(func() { globalResolver = prev })
	globalResolver = nil
	if r := GetGlobalResolver(); r != nil {
		t.Errorf("expected nil global resolver by default, got %v", r)
	}
	SetGlobalResolver(NewResolver(nil))
	if r := GetGlobalResolver(); r == nil {
		t.Error("expected non-nil global resolver after SetGlobalResolver")
	}
}

// TestResolveReturnsNilBeforeResolveAll — calling Resolve(name) before
// ResolveAll must return nil, not panic. (BuildWorkerPrompt hits this path
// when a resolver is set but hasn't been called yet.)
func TestResolveReturnsNilBeforeResolveAll(t *testing.T) {
	r := NewResolver(nil)
	if got := r.Resolve("anything"); got != nil {
		t.Errorf("expected nil before ResolveAll, got %v", got)
	}
}

// TestPrefixesFor — verifies the reverse-index key derivation. MCP tiers use
// their MCPServers list; extension tiers use the <cap>-<tier> PreWarmer
// convention; bash tiers have no prefix (HealthMonitor never sees them).
func TestPrefixesFor(t *testing.T) {
	cases := []struct {
		name    string
		capName string
		impl    *Implementation
		want    []string
	}{
		{
			name:    "mcp tier uses MCPServers",
			capName: "research",
			impl:    &Implementation{Name: "cloud", Type: "mcp", MCPServers: []string{"web", "media"}},
			want:    []string{"web", "media"},
		},
		{
			name:    "extension tier uses cap-tier convention",
			capName: "research",
			impl:    &Implementation{Name: "self-hosted", Type: "extension", Source: "git+https://example/x"},
			want:    []string{"research-self-hosted"},
		},
		{
			name:    "extension tier without source has no prefix",
			capName: "research",
			impl:    &Implementation{Name: "stub", Type: "extension"},
			want:    nil,
		},
		{
			name:    "bash tier has no prefix",
			capName: "research",
			impl:    &Implementation{Name: "bash-ddg", Type: "bash"},
			want:    nil,
		},
		{
			name:    "nil impl",
			capName: "research",
			impl:    nil,
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := prefixesFor(tc.capName, tc.impl)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// seedResolverForTest bypasses ResolveAll (which calls invalidateCachesForResolve
// and fights the test's stubbed toolPackages cache) by directly populating
// r.resolved + r.prefixToCaps. Returns a cleanup that nils them out.
func seedResolverForTest(t *testing.T, r *CapabilityResolver, resolved map[string]*Implementation) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolved = resolved
	r.prefixToCaps = make(map[string][]string)
	for capName, impl := range resolved {
		for _, pfx := range prefixesFor(capName, impl) {
			r.prefixToCaps[pfx] = append(r.prefixToCaps[pfx], capName)
		}
	}
	t.Cleanup(func() {
		r.mu.Lock()
		r.resolved = nil
		r.prefixToCaps = nil
		r.mu.Unlock()
	})
}

// TestInvalidateReResolves — when the active impl dies and a fallback becomes
// live, Invalidate(capName) returns the new impl and updates the cache.
//
// Uses invalidateOneLocked directly (bypasses LoadToolPackages so we don't
// fight the package cache). The "cloud tier died" simulation: change the
// pkg's implementation[0] health to a failing probe, then re-run invalidate.
func TestInvalidateReResolves(t *testing.T) {
	r := NewResolver(nil)

	pkg := &ToolPackage{
		Name: "research",
		Implementations: []Implementation{
			{Name: "cloud", Priority: 1, Health: HealthCheck{Kind: "always-true"}},
			{Name: "bash", Priority: 99, Health: HealthCheck{Kind: "always-true"}},
		},
	}
	seedResolverForTest(t, r, map[string]*Implementation{
		"research": &pkg.Implementations[0],
	})

	if got := r.Resolve("research"); got == nil || got.Name != "cloud" {
		t.Fatalf("initial Resolve = %v, want cloud", got)
	}

	// Simulate "cloud died" — its always-true probe becomes a failing mcp probe.
	pkg.Implementations[0].Health = HealthCheck{Kind: "mcp-available", Server: "nonexistent"}

	r.mu.Lock()
	fresh := r.invalidateOneLocked("research", pkg)
	r.mu.Unlock()

	if fresh == nil {
		t.Fatal("invalidateOneLocked returned nil")
	}
	if fresh.Name != "bash" {
		t.Errorf("expected invalidate to pick bash fallback, got %q", fresh.Name)
	}
	if got := r.Resolve("research"); got == nil || got.Name != "bash" {
		t.Errorf("Resolve after invalidate = %v, want bash", got)
	}
}

// TestInvalidateNoOpWhenUnchanged — if pickActive returns the same tier,
// Invalidate returns the previous impl without bumping probed or firing
// cache invalidation. Confirms the short-circuit path.
func TestInvalidateNoOpWhenUnchanged(t *testing.T) {
	r := NewResolver(nil)

	pkg := &ToolPackage{
		Name: "research",
		Implementations: []Implementation{
			{Name: "cloud", Priority: 1, Health: HealthCheck{Kind: "always-true"}},
		},
	}
	seedResolverForTest(t, r, map[string]*Implementation{
		"research": &pkg.Implementations[0],
	})

	before := r.ProbedCount()

	r.mu.Lock()
	fresh := r.invalidateOneLocked("research", pkg)
	r.mu.Unlock()

	if fresh == nil || fresh.Name != "cloud" {
		t.Errorf("expected invalidate to return cloud, got %v", fresh)
	}
	if r.ProbedCount() != before {
		t.Errorf("ProbedCount changed on no-op invalidate: %d → %d", before, r.ProbedCount())
	}
}

// TestCapabilitiesForPrefix — after seeding the resolver, the reverse index
// maps MCP prefixes back to capability names. Used by HealthMonitor's
// hot-promotion path.
func TestCapabilitiesForPrefix(t *testing.T) {
	r := NewResolver(nil)

	cloud := &Implementation{Name: "cloud", Priority: 1, Type: "mcp", MCPServers: []string{"web"}, Health: HealthCheck{Kind: "always-true"}}
	seedResolverForTest(t, r, map[string]*Implementation{"research": cloud})

	caps := r.CapabilitiesForPrefix("web")
	if len(caps) != 1 || caps[0] != "research" {
		t.Errorf("CapabilitiesForPrefix(web) = %v, want [research]", caps)
	}
	if got := r.CapabilitiesForPrefix("nonexistent"); len(got) != 0 {
		t.Errorf("CapabilitiesForPrefix(nonexistent) = %v, want []", got)
	}
}

// TestInvalidateUpdatesReverseIndex — when Invalidate swaps the active impl,
// the reverse index must drop the old prefix and add the new one. Otherwise
// HealthMonitor would invalidate the wrong capability on the next failure.
func TestInvalidateUpdatesReverseIndex(t *testing.T) {
	r := NewResolver(nil)

	pkg := &ToolPackage{
		Name: "research",
		Implementations: []Implementation{
			{Name: "cloud", Priority: 1, Type: "mcp", MCPServers: []string{"web"}, Health: HealthCheck{Kind: "always-true"}},
			{Name: "self-hosted", Priority: 99, Type: "extension", Source: "git+https://example/x", Health: HealthCheck{Kind: "always-true"}},
		},
	}
	seedResolverForTest(t, r, map[string]*Implementation{
		"research": &pkg.Implementations[0],
	})

	// Sanity: initial reverse index points at web.
	if caps := r.CapabilitiesForPrefix("web"); len(caps) != 1 || caps[0] != "research" {
		t.Fatalf("initial CapabilitiesForPrefix(web) = %v, want [research]", caps)
	}

	// Kill the cloud tier.
	pkg.Implementations[0].Health = HealthCheck{Kind: "mcp-available", Server: "nonexistent"}

	r.mu.Lock()
	fresh := r.invalidateOneLocked("research", pkg)
	r.mu.Unlock()

	if fresh == nil || fresh.Name != "self-hosted" {
		t.Fatalf("invalidate = %v, want self-hosted", fresh)
	}

	// Reverse index: web → [] (removed), research-self-hosted → [research] (added).
	if caps := r.CapabilitiesForPrefix("web"); len(caps) != 0 {
		t.Errorf("after invalidate, CapabilitiesForPrefix(web) = %v, want []", caps)
	}
	if caps := r.CapabilitiesForPrefix("research-self-hosted"); len(caps) != 1 || caps[0] != "research" {
		t.Errorf("after invalidate, CapabilitiesForPrefix(research-self-hosted) = %v, want [research]", caps)
	}
}
