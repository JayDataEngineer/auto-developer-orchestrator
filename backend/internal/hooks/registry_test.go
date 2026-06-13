package hooks

import (
	"errors"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// resetRegistry swaps the global registry for a fresh one and restores it on
// cleanup. The registry is a package singleton, so tests must isolate state.
func resetRegistry(t *testing.T) {
	t.Helper()
	saved := globalRegistry.factory
	globalRegistry.factory = make(map[string]HookFactory)
	t.Cleanup(func() { globalRegistry.factory = saved })
}

// namedHook is a NoopHook that reports a chosen name.
type namedHook struct {
	core.NoopHook
	name string
}

func (n *namedHook) Name() string { return n.name }

// newNamedHook returns a named hook factory result (hook + nil error) so it can
// be returned directly from HookFactory closures.
func newNamedHook(name string) (core.LoopHook, error) {
	return &namedHook{name: name}, nil
}

func TestRegisterHook_AndResolve(t *testing.T) {
	resetRegistry(t)
	RegisterHook("alpha", func(dep HookDeps) (core.LoopHook, error) {
		if dep.SessionID != "sess-1" {
			t.Errorf("factory received SessionID = %q, want %q", dep.SessionID, "sess-1")
		}
		return newNamedHook("alpha")
	})

	hooks, err := ResolveHooks([]string{"alpha"}, HookDeps{SessionID: "sess-1"})
	if err != nil {
		t.Fatalf("ResolveHooks: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Name() != "alpha" {
		t.Errorf("hook Name = %q, want %q", hooks[0].Name(), "alpha")
	}
}

func TestResolveHooks_EmptyReturnsNil(t *testing.T) {
	resetRegistry(t)
	got, err := ResolveHooks(nil, HookDeps{})
	if err != nil {
		t.Fatalf("expected nil error for empty names, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil slice for empty names, got %v", got)
	}
}

func TestResolveHooks_UnknownNameListsRegistered(t *testing.T) {
	resetRegistry(t)
	RegisterHook("known", func(HookDeps) (core.LoopHook, error) { return newNamedHook("known") })

	_, err := ResolveHooks([]string{"missing"}, HookDeps{})
	if err == nil {
		t.Fatal("expected error for unknown hook, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention unknown name %q: %v", "missing", err)
	}
	if !strings.Contains(err.Error(), "known") {
		t.Errorf("error should list registered hooks: %v", err)
	}
}

func TestResolveHooks_FactoryErrorWraps(t *testing.T) {
	resetRegistry(t)
	factoryErr := errors.New("boom")
	RegisterHook("broken", func(HookDeps) (core.LoopHook, error) {
		return nil, factoryErr
	})

	_, err := ResolveHooks([]string{"broken"}, HookDeps{})
	if err == nil {
		t.Fatal("expected wrapped factory error, got nil")
	}
	if !errors.Is(err, factoryErr) {
		t.Errorf("expected error to wrap factory error, got %v", err)
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("wrapped error should mention hook name: %v", err)
	}
}

func TestRegisterHook_DuplicatePanics(t *testing.T) {
	resetRegistry(t)
	RegisterHook("dup", func(HookDeps) (core.LoopHook, error) { return newNamedHook("dup") })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration, got none")
		}
	}()
	RegisterHook("dup", func(HookDeps) (core.LoopHook, error) { return newNamedHook("dup") })
}

func TestAvailableHookNames(t *testing.T) {
	resetRegistry(t)
	RegisterHook("c", func(HookDeps) (core.LoopHook, error) { return newNamedHook("c") })
	RegisterHook("a", func(HookDeps) (core.LoopHook, error) { return newNamedHook("a") })
	RegisterHook("b", func(HookDeps) (core.LoopHook, error) { return newNamedHook("b") })

	names := AvailableHookNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d (%v)", len(names), names)
	}
	// Names are map keys (unordered); verify membership.
	want := map[string]bool{"a": true, "b": true, "c": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected name %q in %v", n, names)
		}
	}
}

func TestResolveHooks_ResolvesMultipleInOrder(t *testing.T) {
	resetRegistry(t)
	RegisterHook("first", func(HookDeps) (core.LoopHook, error) { return newNamedHook("first") })
	RegisterHook("second", func(HookDeps) (core.LoopHook, error) { return newNamedHook("second") })

	got, err := ResolveHooks([]string{"first", "second"}, HookDeps{})
	if err != nil {
		t.Fatalf("ResolveHooks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(got))
	}
	if got[0].Name() != "first" || got[1].Name() != "second" {
		t.Errorf("order = [%s, %s], want [first, second]", got[0].Name(), got[1].Name())
	}
}
