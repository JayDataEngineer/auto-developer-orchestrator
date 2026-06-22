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
