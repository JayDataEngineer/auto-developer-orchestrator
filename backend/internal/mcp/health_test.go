package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeRestarter records calls to Restart so tests can assert the health
// monitor asked for a restart after N failures. Port returns whatever was
// last set; zero-value means "no opinion."
type fakeRestarter struct {
	mu            sync.Mutex
	restartCalls  []string
	portToReturn  int
	portCalls     []string
	onRestart     func() // optional; fires inside the lock when Restart is called
}

func (f *fakeRestarter) Restart(_ context.Context, prefix string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restartCalls = append(f.restartCalls, prefix)
	if f.onRestart != nil {
		f.onRestart()
	}
	return f.portToReturn, nil
}

func (f *fakeRestarter) PortFor(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.portCalls = append(f.portCalls, prefix)
	return f.portToReturn
}

func (f *fakeRestarter) RestartCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.restartCalls)
}

// mcpServerStub answers initialize + tools/list with a fixed tool list.
// Closing the server makes subsequent calls fail — used to simulate a
// crashed MCP server.
func mcpServerStub(t *testing.T, toolName string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "test-session")
			_ = json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{"tools":{}},"serverInfo":{"name":"stub","version":"0"}}`),
			})
		case "notifications/initialized":
		case "tools/list":
			_ = json.NewEncoder(w).Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: json.RawMessage(`{"tools":[{"name":"` + toolName + `","description":"stub"}]}`),
			})
		default:
			http.Error(w, "not implemented", http.StatusBadRequest)
		}
	}))
}

// TestHealthMonitor_PingSuccessNoAction: one healthy probe should not
// increment fail count or trigger restart.
func TestHealthMonitor_PingSuccessNoAction(t *testing.T) {
	server := mcpServerStub(t, "stub_tool")
	defer server.Close()

	multi := NewMultiClient(zap.NewNop())
	multi.AddClient("stub", NewClient("stub", server.URL, nil))
	if err := multi.InitializeAll(context.Background()); err != nil {
		t.Fatalf("InitializeAll: %v", err)
	}

	restarter := &fakeRestarter{portToReturn: 9999}
	mon := NewHealthMonitor(multi, restarter, zap.NewNop(),
		WithHealthInterval(time.Hour), // disable ticker; we drive probes manually
		WithMaxFailures(3),
	)
	mon.probeAll(context.Background())

	if mon.FailCount("stub") != 0 {
		t.Errorf("fail count = %d, want 0", mon.FailCount("stub"))
	}
	if restarter.RestartCount() != 0 {
		t.Errorf("restart called %d times, want 0", restarter.RestartCount())
	}
	if multi.IsUnavailable("stub") {
		t.Errorf("stub marked unavailable after successful probe")
	}
}

// TestHealthMonitor_ThreeFailuresTriggerRestart: closing the server makes
// probes fail; after maxFailures consecutive failures, the monitor should
// call Restart and reset the fail count.
func TestHealthMonitor_ThreeFailuresTriggerRestart(t *testing.T) {
	server := mcpServerStub(t, "stub_tool")

	multi := NewMultiClient(zap.NewNop())
	multi.AddClient("stub", NewClient("stub", server.URL, nil))
	if err := multi.InitializeAll(context.Background()); err != nil {
		t.Fatalf("InitializeAll: %v", err)
	}

	restarter := &fakeRestarter{portToReturn: 8888}
	mon := NewHealthMonitor(multi, restarter, zap.NewNop(),
		WithHealthInterval(time.Hour),
		WithMaxFailures(3),
		WithProbeTimeout(500*time.Millisecond),
	)

	// Kill the server so probes fail.
	server.Close()

	for i := 1; i <= 3; i++ {
		mon.probeAll(context.Background())
		if got := mon.FailCount("stub"); got != i && i < 3 {
			t.Errorf("after probe %d: fail count = %d, want %d", i, got, i)
		}
	}

	if restarter.RestartCount() != 1 {
		t.Errorf("restart calls = %d, want 1", restarter.RestartCount())
	}
	if mon.FailCount("stub") != 0 {
		t.Errorf("fail count after restart = %d, want 0 (reset)", mon.FailCount("stub"))
	}
}

// TestHealthMonitor_RecoveryCallsRefreshTools: if a previously-failing probe
// succeeds, the monitor should clear the unavailable flag and refresh tools.
func TestHealthMonitor_RecoveryCallsRefreshTools(t *testing.T) {
	server := mcpServerStub(t, "stub_tool")

	multi := NewMultiClient(zap.NewNop())
	multi.AddClient("stub", NewClient("stub", server.URL, nil))
	if err := multi.InitializeAll(context.Background()); err != nil {
		t.Fatalf("InitializeAll: %v", err)
	}

	mon := NewHealthMonitor(multi, nil, zap.NewNop(),
		WithHealthInterval(time.Hour),
		WithMaxFailures(3),
		WithProbeTimeout(500*time.Millisecond),
	)

	// Mark the client unavailable manually, then probe — success should clear it.
	multi.MarkUnavailable("stub")
	if !multi.IsUnavailable("stub") {
		t.Fatalf("precondition: stub should be unavailable")
	}

	mon.probeAll(context.Background())

	if mon.FailCount("stub") != 0 {
		t.Errorf("fail count after successful probe = %d, want 0", mon.FailCount("stub"))
	}
	if multi.IsUnavailable("stub") {
		t.Errorf("stub still marked unavailable after successful probe")
	}
}

// TestHealthMonitor_RestartDisabledNoOp: when Restart returns ErrRestartDisabled
// (simulated via Restart=="no" policy at the manager layer), the monitor logs
// but does not loop-restart. We simulate this by returning a zero port from
// the fake restarter; the monitor treats that as "restart declined."
func TestHealthMonitor_NoRestarterIsSafe(t *testing.T) {
	server := mcpServerStub(t, "stub_tool")

	multi := NewMultiClient(zap.NewNop())
	multi.AddClient("stub", NewClient("stub", server.URL, nil))
	_ = multi.InitializeAll(context.Background())

	mon := NewHealthMonitor(multi, nil, zap.NewNop(), // nil restarter
		WithHealthInterval(time.Hour),
		WithMaxFailures(2),
		WithProbeTimeout(500*time.Millisecond),
	)

	server.Close()

	for i := 0; i < 4; i++ {
		mon.probeAll(context.Background())
	}

	// No panic, no restart. Client should be flagged unavailable.
	if !multi.IsUnavailable("stub") {
		t.Errorf("stub should be marked unavailable after 4 failures")
	}
}

// TestHealthMonitor_StopIsIdempotent: calling Stop multiple times after Start
// must not panic.
func TestHealthMonitor_StopIsIdempotent(t *testing.T) {
	multi := NewMultiClient(zap.NewNop())
	mon := NewHealthMonitor(multi, nil, zap.NewNop(),
		WithHealthInterval(time.Hour),
	)
	stop := mon.Start(context.Background())
	stop()
	mon.Stop() // idempotent — must not panic
}

// TestHealthMonitor_ProbeTimeout: a probe that hangs past the probe timeout
// counts as a failure.
func TestHealthMonitor_ProbeTimeout(t *testing.T) {
	var atomicCalls int32
	hangServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&atomicCalls, 1)
		time.Sleep(2 * time.Second) // longer than probe timeout
	}))
	defer hangServer.Close()

	multi := NewMultiClient(zap.NewNop())
	multi.AddClient("hang", NewClient("hang", hangServer.URL, nil))
	_ = multi.InitializeAll(context.Background())

	mon := NewHealthMonitor(multi, nil, zap.NewNop(),
		WithHealthInterval(time.Hour),
		WithMaxFailures(5),
		WithProbeTimeout(100*time.Millisecond),
	)

	start := time.Now()
	mon.probeAll(context.Background())
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Errorf("probe took %v; should have timed out near 100ms", elapsed)
	}
	if mon.FailCount("hang") != 1 {
		t.Errorf("fail count = %d, want 1 (timeout counts as failure)", mon.FailCount("hang"))
	}
}

// keep import used even if future test drops atomic helper
var _ = atomic.AddInt32

// TestHealthMonitor_TierDeathCallbackFires: after maxFailures consecutive
// failures, the onTierDeath hook (Phase 3 hot-promotion) fires exactly once
// with the dying prefix. Verifies the wiring without depending on a real
// resolver — the test stubs the callback to record what it saw.
func TestHealthMonitor_TierDeathCallbackFires(t *testing.T) {
	server := mcpServerStub(t, "stub_tool")

	multi := NewMultiClient(zap.NewNop())
	multi.AddClient("stub", NewClient("stub", server.URL, nil))
	if err := multi.InitializeAll(context.Background()); err != nil {
		t.Fatalf("InitializeAll: %v", err)
	}

	var fired []string
	var firedMu sync.Mutex
	mon := NewHealthMonitor(multi, nil, zap.NewNop(),
		WithHealthInterval(time.Hour),
		WithMaxFailures(3),
		WithProbeTimeout(500*time.Millisecond),
		WithTierDeathCallback(func(prefix string) {
			firedMu.Lock()
			fired = append(fired, prefix)
			firedMu.Unlock()
		}),
	)

	server.Close()

	for i := 1; i <= 3; i++ {
		mon.probeAll(context.Background())
	}

	firedMu.Lock()
	gotFired := append([]string(nil), fired...)
	firedMu.Unlock()

	if len(gotFired) != 1 {
		t.Errorf("onTierDeath fired %d times, want 1 (one maxFailures crossing)", len(gotFired))
	}
	if len(gotFired) > 0 && gotFired[0] != "stub" {
		t.Errorf("onTierDeath prefix = %q, want stub", gotFired[0])
	}
}

// TestHealthMonitor_TierDeathFiresBeforeRestart: confirms the ordering —
// onTierDeath fires BEFORE the restart path. If both are wired, hot-promotion
// gets first crack. App wiring uses this to invalidate the resolver before
// the extension manager spawns a new subprocess.
func TestHealthMonitor_TierDeathFiresBeforeRestart(t *testing.T) {
	server := mcpServerStub(t, "stub_tool")

	multi := NewMultiClient(zap.NewNop())
	multi.AddClient("stub", NewClient("stub", server.URL, nil))
	if err := multi.InitializeAll(context.Background()); err != nil {
		t.Fatalf("InitializeAll: %v", err)
	}

	var order []string
	var orderMu sync.Mutex
	restarter := &fakeRestarter{
		portToReturn: 9999,
		onRestart: func() {
			orderMu.Lock()
			order = append(order, "restart")
			orderMu.Unlock()
		},
	}
	mon := NewHealthMonitor(multi, restarter, zap.NewNop(),
		WithHealthInterval(time.Hour),
		WithMaxFailures(3),
		WithProbeTimeout(500*time.Millisecond),
		WithTierDeathCallback(func(prefix string) {
			orderMu.Lock()
			order = append(order, "tier-death:"+prefix)
			orderMu.Unlock()
		}),
	)

	server.Close()

	for i := 1; i <= 3; i++ {
		mon.probeAll(context.Background())
	}

	orderMu.Lock()
	defer orderMu.Unlock()
	if len(order) != 2 {
		t.Fatalf("expected 2 callbacks (tier-death + restart), got %d: %v", len(order), order)
	}
	if order[0] != "tier-death:stub" {
		t.Errorf("first callback = %q, want tier-death:stub", order[0])
	}
	if order[1] != "restart" {
		t.Errorf("second callback = %q, want restart", order[1])
	}
}
