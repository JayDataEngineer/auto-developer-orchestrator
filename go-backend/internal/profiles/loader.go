package profiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Store manages interaction profiles. Thread-safe.
// Profiles are loaded from two locations (project overrides global):
//   - Global: ~/.pux/profiles/<name>.yaml
//   - Project: <project>/profiles/<name>.yaml
type Store struct {
	mu       sync.RWMutex
	global   string // ~/.pux/profiles
	project  string // <project>/profiles
	cache    map[string]*cachedProfile
}

type cachedProfile struct {
	profile *Profile
	modTime int64
}

// NewStore creates a profile store with global + optional project directories.
func NewStore(projectDir string) *Store {
	home, _ := os.UserHomeDir()
	global := filepath.Join(home, ".pux", "profiles")

	s := &Store{
		global: global,
		cache:  make(map[string]*cachedProfile),
	}

	if projectDir != "" {
		s.project = filepath.Join(projectDir, "profiles")
	}

	return s
}

// Load reads a profile by name. Project-level overrides global.
// Results are cached with modTime-based invalidation.
func (s *Store) Load(name string) (*Profile, error) {
	// Sanitize name — no path traversal
	name = filepath.Base(name)
	if strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid profile name: %s", name)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try project-level first, then global
	paths := s.resolvePaths(name)
	for _, p := range paths {
		prof, err := s.loadFromPath(p)
		if err == nil {
			return prof, nil
		}
	}

	return nil, fmt.Errorf("profile %q not found (searched %v)", name, paths)
}

// Save writes a profile. Goes to project dir if set, otherwise global.
func (s *Store) Save(name string, prof *Profile) error {
	name = filepath.Base(name)
	if strings.Contains(name, "..") {
		return fmt.Errorf("invalid profile name: %s", name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.project
	if dir == "" {
		dir = s.global
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create profiles dir: %w", err)
	}

	prof.App = name // ensure consistency

	data, err := yaml.Marshal(prof)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}

	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}

	// Invalidate cache
	delete(s.cache, path)

	return nil
}

// List returns all available profile names (deduplicated, project overrides global).
func (s *Store) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	var names []string

	for _, dir := range []string{s.global, s.project} {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".yaml")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}

	return names, nil
}

// Delete removes a profile. Only deletes from project dir (won't touch global).
func (s *Store) Delete(name string) error {
	name = filepath.Base(name)
	if s.project == "" {
		return fmt.Errorf("cannot delete global profiles without project context")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.project, name+".yaml")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	delete(s.cache, path)
	return nil
}

// ResolveAction returns the resolved action for a profile + action name.
// It also validates that required params are present.
func (s *Store) ResolveAction(profileName, actionName string, params map[string]any) (*Action, error) {
	prof, err := s.Load(profileName)
	if err != nil {
		return nil, err
	}

	action, ok := prof.Actions[actionName]
	if !ok {
		// List available actions for helpful error
		available := make([]string, 0, len(prof.Actions))
		for k := range prof.Actions {
			available = append(available, k)
		}
		return nil, fmt.Errorf("action %q not found in profile %q — available: %v",
			actionName, profileName, available)
	}

	// Validate required params
	for paramName := range action.Params {
		if _, ok := params[paramName]; !ok {
			// Check if any step references this param
			needsParam := false
			for _, step := range action.Steps {
				if strings.Contains(step.Key, "{"+paramName+"}") ||
					strings.Contains(step.Type, "{"+paramName+"}") ||
					strings.Contains(step.Shortcut, "{"+paramName+"}") {
					needsParam = true
					break
				}
			}
			if strings.Contains(action.Key, "{"+paramName+"}") ||
				strings.Contains(action.Type, "{"+paramName+"}") {
				needsParam = true
			}
			if needsParam {
				return nil, fmt.Errorf("missing required param %q for action %q", paramName, actionName)
			}
		}
	}

	return &action, nil
}

// resolvePaths returns the search paths for a profile (project first, then global).
func (s *Store) resolvePaths(name string) []string {
	var paths []string
	if s.project != "" {
		paths = append(paths, filepath.Join(s.project, name+".yaml"))
	}
	paths = append(paths, filepath.Join(s.global, name+".yaml"))
	return paths
}

// loadFromPath loads and caches a profile from a file path.
func (s *Store) loadFromPath(path string) (*Profile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	modTime := info.ModTime().UnixMilli()

	// Check cache
	if cached, ok := s.cache[path]; ok && cached.modTime == modTime {
		return cached.profile, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var prof Profile
	if err := yaml.Unmarshal(data, &prof); err != nil {
		return nil, fmt.Errorf("parse profile %s: %w", path, err)
	}

	if prof.App == "" {
		prof.App = strings.TrimSuffix(filepath.Base(path), ".yaml")
	}

	// Cache it
	s.cache[path] = &cachedProfile{profile: &prof, modTime: modTime}

	return &prof, nil
}
