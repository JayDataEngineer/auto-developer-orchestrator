package hooks

import (
	"fmt"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// HookFactory creates a LoopHook given contextual dependencies.
// Not all hooks need all parameters — use what you need, ignore the rest.
type HookFactory func(dep HookDeps) (core.LoopHook, error)

// HookDeps provides the contextual dependencies a hook factory might need.
// Passed to the factory at hook creation time — only use what you need.
type HookDeps struct {
	SessionID  string // session ID for checkpoint isolation
	ProjectDir string // project root for file checkpointing
	HomeDir    string // user home dir for checkpoint storage
	// Raise browser — call to raise the browser window on VNC
	RaiseBrowserFunc func() error
	// Git executor — if set, git checkpoint hook can commit
	GitExecutor GitExecutor
	// Journal session tree — if set, journal checkpoint hook can save node state
	// (type is interface{} to avoid importing session package)
	JournalTree any
}

// globalRegistry is the singleton hook registry.
var globalRegistry = struct {
	mu      sync.RWMutex
	factory map[string]HookFactory
}{
	factory: make(map[string]HookFactory),
}

// RegisterHook registers a named hook factory. Call from init() or package setup.
// Panics on duplicate registration (catches init-order bugs early).
func RegisterHook(name string, factory HookFactory) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	if _, exists := globalRegistry.factory[name]; exists {
		panic(fmt.Sprintf("hooks: duplicate registration for %q", name))
	}
	globalRegistry.factory[name] = factory
}

// ResolveHooks resolves a list of hook names to concrete LoopHook instances.
// Returns an error if any name is not registered.
func ResolveHooks(names []string, dep HookDeps) ([]core.LoopHook, error) {
	if len(names) == 0 {
		return nil, nil
	}
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	var result []core.LoopHook
	for _, name := range names {
		factory, ok := globalRegistry.factory[name]
		if !ok {
			return nil, fmt.Errorf("hooks: unknown hook %q (registered: %v)", name, registeredNames())
		}
		hook, err := factory(dep)
		if err != nil {
			return nil, fmt.Errorf("hooks: failed to create %q: %w", name, err)
		}
		result = append(result, hook)
	}
	return result, nil
}

// registeredNames returns the names of all registered hooks (for error messages).
func registeredNames() []string {
	names := make([]string, 0, len(globalRegistry.factory))
	for name := range globalRegistry.factory {
		names = append(names, name)
	}
	return names
}
