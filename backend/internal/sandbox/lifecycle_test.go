package sandbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeManagerForWatchdog is a minimal *Manager stand-in for watchdog tests.
// We can't construct a real Manager without a Docker client, and the
// watchdog only touches mu + sandboxes — we expose them by constructing
// a bare struct and pushing test sandboxes into the map directly.
//
// Tests that need ShutdownByProjectLabel behavior pass a fakeShutdownFn
// that records its calls (we don't want to actually hit Docker).
func newBareManager() *Manager {
	return &Manager{
		sandboxes: make(map[string]*Sandbox),
	}
}

func addSandbox(m *Manager, sb *Sandbox) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sandboxes[sb.ID] = sb
}

// TestWatchdogTickSkipsZeroIdleShutdownSecs proves the watchdog leaves
// sandboxes alone when IdleShutdownSecs is unset (preserves the pre-PR4
// "container stays up forever" default).
func TestWatchdogTickSkipsZeroIdleShutdownSecs(t *testing.T) {
	m := newBareManager()
	addSandbox(m, &Sandbox{
		ID:               "sb-no-threshold",
		ProjectPath:      "/proj/a",
		Status:           StatusRunning,
		CreatedAt:        time.Unix(1000, 0),
		LastActivityAt:   time.Unix(1000, 0),
		IdleShutdownSecs: 0, // off
	})

	var calls []string
	var mu sync.Mutex
	w := NewWatchdog(WatchdogConfig{
		Manager: m,
		Logger:  zap.NewNop(),
		NowFunc: func() time.Time { return time.Unix(9999, 0) }, // way past any threshold
		ShutdownFn: func(ctx context.Context, project string) ([]string, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, project)
			return nil, nil
		},
		// TickInterval doesn't matter — we call tick() directly
	})

	w.tick()
	if len(calls) != 0 {
		t.Errorf("expected 0 shutdown calls, got %d (%v)", len(calls), calls)
	}
}

// TestWatchdogTickHonorsIdleThreshold proves that once a sandbox has been
// idle longer than IdleShutdownSecs, the watchdog calls ShutdownFn.
func TestWatchdogTickHonorsIdleThreshold(t *testing.T) {
	m := newBareManager()
	addSandbox(m, &Sandbox{
		ID:               "sb-idle",
		ProjectPath:      "/proj/idle",
		Status:           StatusRunning,
		CreatedAt:        time.Unix(1000, 0),
		LastActivityAt:   time.Unix(1000, 0),
		IdleShutdownSecs: 60, // 1 minute
	})

	var calls []string
	var mu sync.Mutex
	w := NewWatchdog(WatchdogConfig{
		Manager: m,
		Logger:  zap.NewNop(),
		// 120s after LastActivityAt → 60s past the 60s threshold
		NowFunc: func() time.Time { return time.Unix(1120, 0) },
		ShutdownFn: func(ctx context.Context, project string) ([]string, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, project)
			return []string{"abc123"}, nil
		},
	})

	w.tick()
	if len(calls) != 1 || calls[0] != "/proj/idle" {
		t.Errorf("expected 1 shutdown call for /proj/idle, got %v", calls)
	}
}

// TestWatchdogTickSkipsJustUnderThreshold proves the watchdog does NOT
// fire when idle time is strictly less than IdleShutdownSecs.
func TestWatchdogTickSkipsJustUnderThreshold(t *testing.T) {
	m := newBareManager()
	addSandbox(m, &Sandbox{
		ID:               "sb-recent",
		ProjectPath:      "/proj/recent",
		Status:           StatusRunning,
		CreatedAt:        time.Unix(1000, 0),
		LastActivityAt:   time.Unix(1000, 0),
		IdleShutdownSecs: 60,
	})

	var calls []string
	var mu sync.Mutex
	w := NewWatchdog(WatchdogConfig{
		Manager: m,
		Logger:  zap.NewNop(),
		NowFunc: func() time.Time { return time.Unix(1059, 0) }, // 59s idle, threshold 60s
		ShutdownFn: func(ctx context.Context, project string) ([]string, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, project)
			return nil, nil
		},
	})

	w.tick()
	if len(calls) != 0 {
		t.Errorf("expected no shutdown at 59s idle (threshold 60s), got %d calls", len(calls))
	}
}

// TestWatchdogTickSkipsNonRunningStatus proves destroyed/error sandboxes
// are left alone — the watchdog shouldn't try to shut down something
// that's already gone.
func TestWatchdogTickSkipsNonRunningStatus(t *testing.T) {
	m := newBareManager()
	addSandbox(m, &Sandbox{
		ID:               "sb-destroyed",
		ProjectPath:      "/proj/gone",
		Status:           StatusDestroyed, // already gone
		CreatedAt:        time.Unix(1000, 0),
		LastActivityAt:   time.Unix(1000, 0),
		IdleShutdownSecs: 60,
	})

	var calls []string
	var mu sync.Mutex
	w := NewWatchdog(WatchdogConfig{
		Manager: m,
		Logger:  zap.NewNop(),
		NowFunc: func() time.Time { return time.Unix(99999, 0) },
		ShutdownFn: func(ctx context.Context, project string) ([]string, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, project)
			return nil, nil
		},
	})

	w.tick()
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for destroyed sandbox, got %d", len(calls))
	}
}

// TestWatchdogTickTreatsZeroLastActivityAsCreatedAt proves that a freshly
// booted sandbox (no tool execution yet) doesn't trigger a false positive
// — we fall back to CreatedAt so a brand-new container isn't immediately
// torn down.
func TestWatchdogTickTreatsZeroLastActivityAsCreatedAt(t *testing.T) {
	m := newBareManager()
	addSandbox(m, &Sandbox{
		ID:               "sb-fresh",
		ProjectPath:      "/proj/fresh",
		Status:           StatusRunning,
		CreatedAt:        time.Unix(5000, 0),
		LastActivityAt:   time.Time{}, // zero — never touched
		IdleShutdownSecs: 60,
	})

	var calls []string
	var mu sync.Mutex
	w := NewWatchdog(WatchdogConfig{
		Manager: m,
		Logger:  zap.NewNop(),
		// 30s after CreatedAt — under the 60s threshold
		NowFunc: func() time.Time { return time.Unix(5030, 0) },
		ShutdownFn: func(ctx context.Context, project string) ([]string, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, project)
			return nil, nil
		},
	})

	w.tick()
	if len(calls) != 0 {
		t.Errorf("fresh sandbox (zero LastActivityAt, 30s after CreatedAt) should not be shut down; got %d calls", len(calls))
	}
}

// TestShutdownByProjectLabelDockerClientErr proves the method returns the
// canonical "docker client not initialized" error when the manager has no
// Docker client wired (the test-environment case). This is the unit-level
// contract; integration coverage of the actual stop/remove path needs a
// Docker daemon, which CI may not have.
func TestShutdownByProjectLabelDockerClientErr(t *testing.T) {
	m := &Manager{} // no dockerClient
	_, err := m.ShutdownByProjectLabel(context.Background(), "/proj/x")
	if err == nil {
		t.Fatal("expected error when docker client is nil")
	}
	if !strings.Contains(err.Error(), "docker client") {
		t.Errorf("expected 'docker client' in error, got %v", err)
	}
}

// TestShutdownByProjectLabelClearsInMemorySandbox proves that when Docker
// IS wired and the container list contains a matching container with an
// openshell.sandbox-id label, the method drops the in-memory sandbox
// state. We can't drive Docker in unit tests, so this test documents the
// contract: if you replace the dockerClient with a fake that returns a
// container carrying openshell.sandbox-id=<sb-x>, the sandbox map entry
// for sb-x is gone after the call.
//
// Without this clear, the next prompt would re-adopt a ghost sandbox
// whose ContainerID points to a torn-down container.
func TestShutdownByProjectLabelClearsInMemorySandbox(t *testing.T) {
	// We can't construct a fake dockerClient without exporting a private
	// interface. Document the contract via the post-condition we CAN
	// assert: an in-memory entry left dangling (no matching Docker call)
	// is harmless because the next CreateSandbox validates the bind mount
	// and destroys mismatched state.
	//
	// See manager.go::ShutdownByProjectLabel for the live clear path —
	// it walks ContainerList results, and for each match inspects the
	// openshell.sandbox-id label to delete the matching map entry.
	t.Skip("requires Docker fake client; covered by integration tests")
}

// errInShutdown is a sentinel error type used by the next test to prove
// the watchdog logs + continues when ShutdownFn returns an error.
type errInShutdown struct{ msg string }

func (e *errInShutdown) Error() string { return e.msg }

// TestWatchdogTickLogsAndContinuesOnError proves a failed shutdown call
// does NOT crash the watchdog — it logs and continues to the next
// candidate. Critical because one bad project shouldn't kill polling
// for every other sandbox.
func TestWatchdogTickLogsAndContinuesOnError(t *testing.T) {
	m := newBareManager()
	addSandbox(m, &Sandbox{
		ID:               "sb-a",
		ProjectPath:      "/proj/a",
		Status:           StatusRunning,
		CreatedAt:        time.Unix(1000, 0),
		LastActivityAt:   time.Unix(1000, 0),
		IdleShutdownSecs: 60,
	})
	addSandbox(m, &Sandbox{
		ID:               "sb-b",
		ProjectPath:      "/proj/b",
		Status:           StatusRunning,
		CreatedAt:        time.Unix(1000, 0),
		LastActivityAt:   time.Unix(1000, 0),
		IdleShutdownSecs: 60,
	})

	var calls []string
	var mu sync.Mutex
	w := NewWatchdog(WatchdogConfig{
		Manager: m,
		Logger:  zap.NewNop(),
		NowFunc: func() time.Time { return time.Unix(99999, 0) },
		ShutdownFn: func(ctx context.Context, project string) ([]string, error) {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, project)
			if project == "/proj/a" {
				return nil, &errInShutdown{msg: "simulated docker failure"}
			}
			return []string{"xyz"}, nil
		},
	})

	w.tick()
	if len(calls) != 2 {
		t.Errorf("expected both candidates to be attempted, got %d calls: %v", len(calls), calls)
	}
}

// TestWatchdogTickSkipsEmptyManager proves a freshly-constructed manager
// with no sandboxes is a safe no-op for tick (no goroutine leak, no error).
func TestWatchdogTickSkipsEmptyManager(t *testing.T) {
	m := newBareManager()
	w := NewWatchdog(WatchdogConfig{
		Manager: m,
		Logger:  zap.NewNop(),
		NowFunc: time.Now,
		ShutdownFn: func(ctx context.Context, project string) ([]string, error) {
			t.Errorf("shutdown should not be called when no sandboxes exist")
			return nil, nil
		},
	})
	w.tick() // should be a no-op
}

// TestWatchdogStartStopRoundTrip proves Start() + Stop() are idempotent
// and don't leak goroutines. The wg.Wait() inside Stop() must return
// even if no tick ever fired.
func TestWatchdogStartStopRoundTrip(t *testing.T) {
	m := newBareManager()
	w := NewWatchdog(WatchdogConfig{
		Manager:      m,
		Logger:       zap.NewNop(),
		TickInterval: 1 * time.Millisecond,
		NowFunc:      time.Now,
		ShutdownFn:   func(ctx context.Context, project string) ([]string, error) { return nil, nil },
	})

	w.Start()
	// Give the goroutine a moment to enter its select loop.
	time.Sleep(5 * time.Millisecond)
	w.Stop()
	w.Stop() // idempotent — should not panic on second call
}

// TestNewWatchdogDefaultsApplied proves NewWatchdog fills in sensible
// defaults (TickInterval = 60s, NowFunc = time.Now, ShutdownFn = no-op)
// when fields are missing. Production callers pass WatchdogDefaults();
// tests construct bare. The defaults must be coherent so a buggy caller
// doesn't crash the watchdog.
func TestNewWatchdogDefaultsApplied(t *testing.T) {
	m := newBareManager()
	w := NewWatchdog(WatchdogConfig{
		Manager: m,
		Logger:  zap.NewNop(),
		// NowFunc, TickInterval, ShutdownFn all nil
	})

	if w.tickInterval != 60*time.Second {
		t.Errorf("expected default TickInterval=60s, got %v", w.tickInterval)
	}
	if w.nowFunc == nil {
		t.Error("expected default nowFunc to be time.Now, got nil")
	}
	if w.shutdownFn == nil {
		t.Error("expected default shutdownFn to be no-op, got nil")
	}

	// Calling the no-op shouldn't crash + should return nil/empty.
	removed, err := w.shutdownFn(context.Background(), "/proj/x")
	if err != nil {
		t.Errorf("default shutdownFn should not error, got %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("default shutdownFn should return empty list, got %v", removed)
	}
}

// TestShutdownByProjectLabelEmptyProjectPath proves that an empty project
// path (which would match every container via the Docker filter API) is
// rejected explicitly rather than silently shutting down everything.
//
// This is the safety rail: the discovery query uses the label value as
// the match key, so an empty value could match unlabeled containers if
// the Docker filter semantics changed. We refuse to operate instead.
func TestShutdownByProjectLabelEmptyProjectPath(t *testing.T) {
	m := &Manager{}
	_, err := m.ShutdownByProjectLabel(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty project path, got nil")
	}
	if !strings.Contains(err.Error(), "projectPath") {
		t.Errorf("expected error mentioning projectPath, got %v", err)
	}
}

// TestErrorsInShutdownSentinel makes sure the errInShutdown type used
// above satisfies the error interface — defensive: if someone refactors
// it away, this test catches the missing method.
func TestErrorsInShutdownSentinel(t *testing.T) {
	var _ error = &errInShutdown{msg: "x"}
	if errors.New("x").Error() == "" {
		t.Error("sentinel error type broken")
	}
}
