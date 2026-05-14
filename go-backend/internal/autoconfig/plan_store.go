package autoconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// PlanStore manages execution plan files on disk via the ArtifactStore contract.
// Plans are stored as .pux/plans/<name>.md files.
type PlanStore struct {
	mu     sync.RWMutex
	dir    string // .pux/plans/ directory
}

// NewPlanStore creates a plan store rooted at the given directory.
// The directory is created on first write if it doesn't exist.
func NewPlanStore(plansDir string) *PlanStore {
	return &PlanStore{dir: plansDir}
}

// List returns all plan names.
func (s *PlanStore) List(ctx context.Context) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names, err := s.listNames()
	if err != nil {
		return nil, err
	}
	return ListResult(names, len(names)), nil
}

// Get returns a single plan's content.
func (s *PlanStore) Get(ctx context.Context, name string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path, err := SafePath(s.dir, name, ".md")
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("plan %q not found", name)
		}
		return nil, fmt.Errorf("read plan: %w", err)
	}

	return map[string]any{
		"name":    name,
		"content": string(data),
	}, nil
}

// Put creates or replaces a plan.
// spec must have "content" key with the plan body (markdown string).
func (s *PlanStore) Put(ctx context.Context, name string, spec map[string]any) (any, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}

	content, _ := spec["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("spec 'content' is required (markdown string)")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return nil, fmt.Errorf("create plans dir: %w", err)
	}

	path := filepath.Join(s.dir, name+".md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("write plan: %w", err)
	}

	return TextResult(fmt.Sprintf("Plan %q saved.", name)), nil
}

// Delete removes a plan.
func (s *PlanStore) Delete(ctx context.Context, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, name+".md")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plan %q not found", name)
		}
		return fmt.Errorf("delete plan: %w", err)
	}
	return nil
}

func (s *PlanStore) listNames() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read plans dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names, nil
}
