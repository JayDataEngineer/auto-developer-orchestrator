package autoconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"gopkg.in/yaml.v3"
)

// WorkerStore manages worker YAML artifacts on disk.
// It implements ArtifactStore for the worker domain.
// Two variants: persistent (project workers dir) and JIT (session-scoped).
type WorkerStore struct {
	mu       sync.RWMutex
	baseDir  string
	jit      bool // true = session-scoped, cleaned up on Close
}

// Worker YAML format is defined once in common.RoleConfig — autoconfig
// reuses that struct so new fields (delegates_to, hooks, division, etc.)
// are automatically available everywhere. No second struct to keep in sync.
type workerConfig = common.RoleConfig

// NewWorkerStore creates a persistent worker store.
// Workers are written to baseDir as <name>.yaml files.
func NewWorkerStore(baseDir string) *WorkerStore {
	return &WorkerStore{baseDir: baseDir}
}

// NewJITWorkerStore creates a session-scoped worker store.
// Workers are written to sessionDir/workers/ and cleaned up on Cleanup().
func NewJITWorkerStore(sessionDir string) *WorkerStore {
	return &WorkerStore{
		baseDir: filepath.Join(sessionDir, "workers"),
		jit:     true,
	}
}

// IsJIT returns whether this is a session-scoped store.
func (s *WorkerStore) IsJIT() bool { return s.jit }

// List returns all worker names in the store directory.
func (s *WorkerStore) List(ctx context.Context) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names, err := s.listNames()
	if err != nil {
		return nil, err
	}
	return ListResult(names, len(names)), nil
}

// Get returns a single worker's details.
func (s *WorkerStore) Get(ctx context.Context, name string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path, err := SafePath(s.baseDir, name, ".yaml")
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("worker %q not found", name)
		}
		return nil, fmt.Errorf("read worker: %w", err)
	}

	var wc workerConfig
	if err := yaml.Unmarshal(data, &wc); err != nil {
		return nil, fmt.Errorf("parse worker: %w", err)
	}

	return map[string]any{
		"name":         name,
		"hint":         wc.Hint,
		"persona":      wc.Persona,
		"description":  wc.Description,
		"capabilities": wc.Capabilities,
		"imports":      wc.Imports,
		"tools":        wc.Tools,
		"mcp_servers":  wc.MCPServers,
		"max_rounds":   wc.MaxRounds,
		"temperature":  wc.Temperature,
		"model":        wc.Model,
		"sandbox":      wc.Sandbox,
		"division":     wc.Division,
		"delegates_to": wc.DelegatesTo,
		"hooks":        wc.Hooks,
		"jit":          s.jit,
	}, nil
}

// Put creates or replaces a worker. Validates capabilities before writing.
// The spec map keys correspond to workerConfig fields.
func (s *WorkerStore) Put(ctx context.Context, name string, spec map[string]any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ValidateName(name); err != nil {
		return nil, err
	}

	// Contract 3.5: kernel workers are immutable — reject creates/updates that collide.
	if common.KernelWorkerNames()[name] {
		return nil, fmt.Errorf("worker %q is a kernel worker and cannot be modified", name)
	}

	// Parse spec into workerConfig
	wc, err := specToWorkerConfig(spec)
	if err != nil {
		return nil, err
	}

	// Validate capabilities exist
	if len(wc.Capabilities) > 0 {
		if err := validateCapabilities(wc.Capabilities); err != nil {
			return nil, err
		}
	}

	// Validate sandbox value
	if wc.Sandbox != "" && wc.Sandbox != "isolated" && wc.Sandbox != "bridged" && wc.Sandbox != "native" {
		return nil, fmt.Errorf("invalid sandbox %q: must be isolated, bridged, or native", wc.Sandbox)
	}

	// Ensure directory exists
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return nil, fmt.Errorf("create workers dir: %w", err)
	}

	// Marshal and write
	data, err := yaml.Marshal(wc)
	if err != nil {
		return nil, fmt.Errorf("marshal worker: %w", err)
	}

	path := filepath.Join(s.baseDir, name+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return nil, fmt.Errorf("write worker: %w", err)
	}

	return TextResult(fmt.Sprintf("Worker %q created with capabilities: %s", name, strings.Join(wc.Capabilities, ", "))), nil
}

// Delete removes a worker YAML file.
func (s *WorkerStore) Delete(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ValidateName(name); err != nil {
		return err
	}

	// Contract 3.5: kernel workers are immutable.
	if common.KernelWorkerNames()[name] {
		return fmt.Errorf("worker %q is a kernel worker and cannot be deleted", name)
	}

	path := filepath.Join(s.baseDir, name+".yaml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete worker: %w", err)
	}
	return nil
}

// Cleanup removes all JIT workers. Call on session end.
func (s *WorkerStore) Cleanup() error {
	if !s.jit {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.baseDir)
}

// Dir returns the base directory for this store.
func (s *WorkerStore) Dir() string { return s.baseDir }

func (s *WorkerStore) listNames() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read workers dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(names)
	return names, nil
}

// validateCapabilities checks that all named capabilities exist.
func validateCapabilities(caps []string) error {
	pkgs := common.LoadToolPackages()
	var unknown []string
	var available []string
	for name := range pkgs {
		available = append(available, name)
	}
	sort.Strings(available)

	for _, cap := range caps {
		if _, ok := pkgs[cap]; !ok {
			unknown = append(unknown, cap)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown capabilities: %s. Available: %s", strings.Join(unknown, ", "), strings.Join(available, ", "))
	}
	return nil
}

// specToWorkerConfig converts a tool args map to workerConfig.
func specToWorkerConfig(spec map[string]any) (*workerConfig, error) {
	wc := &workerConfig{}

	if v, ok := spec["hint"].(string); ok {
		wc.Hint = v
	}

	if v, ok := spec["persona"].(string); ok {
		wc.Persona = v
	}
	// Description is accepted as a fallback identity — some workers are
	// authored by agents that copy the legacy role format (description +
	// prompt.md). If neither is provided we cannot identify the worker.
	if v, ok := spec["description"].(string); ok {
		wc.Description = v
	}
	if wc.Persona == "" && wc.Description == "" {
		return nil, fmt.Errorf("persona or description is required")
	}

	if v, ok := spec["capabilities"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				wc.Capabilities = append(wc.Capabilities, s)
			}
		}
	}
	// imports is an alias for capabilities (legacy role format). Merge in.
	if v, ok := spec["imports"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				wc.Imports = append(wc.Imports, s)
			}
		}
	}

	if v, ok := spec["tools"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				wc.Tools = append(wc.Tools, s)
			}
		}
	}

	if v, ok := spec["mcp_servers"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				wc.MCPServers = append(wc.MCPServers, s)
			}
		}
	}

	if v, ok := spec["max_rounds"].(float64); ok && v > 0 {
		wc.MaxRounds = int(v)
	}
	if wc.MaxRounds == 0 {
		wc.MaxRounds = 15
	}

	if v, ok := spec["temperature"].(float64); ok {
		wc.Temperature = v
	}
	if wc.Temperature == 0 {
		wc.Temperature = 0.4
	}

	if v, ok := spec["model"].(string); ok {
		wc.Model = v
	}

	if v, ok := spec["sandbox"].(string); ok {
		wc.Sandbox = v
	}

	if v, ok := spec["division"].(string); ok {
		wc.Division = v
	}

	if v, ok := spec["delegates_to"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				wc.DelegatesTo = append(wc.DelegatesTo, s)
			}
		}
	}

	if v, ok := spec["hooks"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				wc.Hooks = append(wc.Hooks, s)
			}
		}
	}

	return wc, nil
}
