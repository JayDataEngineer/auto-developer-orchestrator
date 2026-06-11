package autoconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/tools/memory"
)

// MemoryStore adapts memory.Store to the ArtifactStore contract.
// MEMORY.md is a single fixed artifact — List always returns ["memory"].
type MemoryStore struct {
	inner *memory.Store
	path  string
	mu    sync.RWMutex
}

// NewMemoryStore creates an ArtifactStore adapter over the existing memory store.
func NewMemoryStore(inner *memory.Store, projectDir string) *MemoryStore {
	return &MemoryStore{
		inner: inner,
		path:  filepath.Join(projectDir, "MEMORY.md"),
	}
}

// List returns ["memory"] when MEMORY.md exists, empty list otherwise.
func (s *MemoryStore) List(ctx context.Context) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return ListResult(nil, 0), nil
	}
	return ListResult([]string{"memory"}, 1), nil
}

// Get returns the full MEMORY.md content.
func (s *MemoryStore) Get(ctx context.Context, name string) (any, error) {
	if name != "memory" {
		return nil, fmt.Errorf("memory artifact name must be 'memory', got %q", name)
	}

	content := s.inner.Read()
	if content == "" {
		return nil, fmt.Errorf("memory not found (MEMORY.md is empty or missing)")
	}

	return map[string]any{
		"name":    "memory",
		"content": content,
		"lines":   len(strings.Split(content, "\n")),
	}, nil
}

// Put replaces the full MEMORY.md content.
func (s *MemoryStore) Put(ctx context.Context, name string, spec map[string]any) (any, error) {
	if name != "memory" {
		return nil, fmt.Errorf("memory artifact name must be 'memory', got %q", name)
	}

	content, _ := spec["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("spec 'content' is required (markdown string)")
	}

	if err := s.inner.Write(content); err != nil {
		return nil, err
	}

	return TextResult("Memory updated."), nil
}

// Delete clears the MEMORY.md file.
func (s *MemoryStore) Delete(ctx context.Context, name string) error {
	if name != "memory" {
		return fmt.Errorf("memory artifact name must be 'memory', got %q", name)
	}

	os.Remove(s.path)
	return nil
}
