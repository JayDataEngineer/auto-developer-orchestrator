package autoconfig

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/profiles"
	"gopkg.in/yaml.v3"
)

// ProfileStore wraps profiles.Store to implement ArtifactStore.
// This brings the existing app profile system under the auto-config contract.
type ProfileStore struct {
	store *profiles.Store
}

// NewProfileStore creates an ArtifactStore adapter over the existing profiles.Store.
func NewProfileStore(store *profiles.Store) *ProfileStore {
	return &ProfileStore{store: store}
}

// List returns all profile names.
func (s *ProfileStore) List(ctx context.Context) (any, error) {
	names, err := s.store.List()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return ListResult(nil, 0), nil
	}
	return ListResult(names, len(names)), nil
}

// Get returns a single profile's details.
func (s *ProfileStore) Get(ctx context.Context, name string) (any, error) {
	prof, err := s.store.Load(name)
	if err != nil {
		return nil, err
	}

	var actionNames []string
	for name := range prof.Actions {
		actionNames = append(actionNames, name)
	}

	return map[string]any{
		"name":    prof.App,
		"type":    prof.Type,
		"actions": len(prof.Actions),
		"detail":  fmt.Sprintf("Profile %q (type: %s) with %d actions: %s", prof.App, prof.Type, len(prof.Actions), strings.Join(actionNames, ", ")),
	}, nil
}

// Put creates or replaces a profile from YAML content.
// spec must contain "content" key with YAML string.
func (s *ProfileStore) Put(ctx context.Context, name string, spec map[string]any) (any, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}

	content, _ := spec["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("spec 'content' is required (YAML string)")
	}

	var prof profiles.Profile
	if err := yaml.Unmarshal([]byte(content), &prof); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	if prof.App == "" {
		prof.App = name
	}

	if err := s.store.Save(name, &prof); err != nil {
		return nil, err
	}

	return TextResult(fmt.Sprintf("Profile %q saved with %d actions.", name, len(prof.Actions))), nil
}

// Delete removes a profile.
func (s *ProfileStore) Delete(ctx context.Context, name string) error {
	return s.store.Delete(name)
}
