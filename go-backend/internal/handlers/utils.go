package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/storage"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// setSSEHeaders sets the standard Server-Sent Events headers.
func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

// resolveProjectPath resolves a project name to its filesystem directory path.
// It checks custom projects from the DB first, then falls back to PROJECT_ROOT/<project>.
// Returns empty string if no valid directory is found.
func resolveProjectPath(project string, db *storage.Database) string {
	if project == "" {
		return ""
	}

	// If project is an absolute path that exists, use it directly
	if filepath.IsAbs(project) {
		if info, err := os.Stat(project); err == nil && info.IsDir() {
			return project
		}
	}

	// Check custom projects from DB first
	if db != nil {
		ctx := context.Background()
		customProjects, err := db.GetCustomProjects(ctx)
		if err == nil {
			for _, p := range customProjects {
				if p.Name == project {
					// Skip non-filesystem paths (e.g. jules:// URLs)
					if strings.Contains(p.Path, "://") {
						break
					}
					if info, err := os.Stat(p.Path); err == nil && info.IsDir() {
						return p.Path
					}
					break
				}
			}
		}
	}

	// Fall back to PROJECT_ROOT/<project> and PROJECT_ROOT/projects/<project>
	projectsDir := os.Getenv("PROJECT_ROOT")
	if projectsDir == "" {
		projectsDir = "/app/projects"
	}

	// Only check subdirectories — never return PROJECT_ROOT itself
	// (prevents "deep-research-test" from resolving to the ADO root)
	candidate := filepath.Join(projectsDir, "projects", project)
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate
	}

	// Also check PROJECT_ROOT/<project> (but skip if it equals PROJECT_ROOT)
	candidate = filepath.Join(projectsDir, project)
	if candidate != projectsDir {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	return ""
}

// GitHubTokenStore provides thread-safe storage for the GitHub API token.
type GitHubTokenStore struct {
	mu    sync.RWMutex
	token string
}

// NewGitHubTokenStore creates a new token store, initialized from the
// GITHUB_TOKEN environment variable if set.
func NewGitHubTokenStore() *GitHubTokenStore {
	return &GitHubTokenStore{
		token: os.Getenv("GITHUB_TOKEN"),
	}
}

// Get returns the current GitHub token. Returns empty string if not set.
func (s *GitHubTokenStore) Get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.token
}

// Set updates the GitHub token and persists it to the environment.
func (s *GitHubTokenStore) Set(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
	os.Setenv("GITHUB_TOKEN", token)
}

// parseOwnerRepo extracts owner and repo from a git remote URL.
// Supports: https://github.com/owner/repo.git, git@github.com:owner/repo.git
func parseOwnerRepo(remoteURL string) (owner, repo string, err error) {
	re := regexp.MustCompile(`github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$`)
	matches := re.FindStringSubmatch(remoteURL)
	if len(matches) == 3 {
		return matches[1], matches[2], nil
	}
	return "", "", fmt.Errorf("cannot parse owner/repo from remote URL: %s", remoteURL)
}
