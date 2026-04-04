package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
)

// GitHubHandler proxies requests to the GitHub API
type GitHubHandler struct {
	logger *zap.Logger
	client *http.Client
}

// NewGitHubHandler creates a new GitHub proxy handler
func NewGitHubHandler(logger *zap.Logger) *GitHubHandler {
	return &GitHubHandler{
		logger: logger,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *GitHubHandler) getToken() (string, error) {
	if githubToken != "" {
		return githubToken, nil
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		githubToken = t
		return t, nil
	}
	return "", fmt.Errorf("GitHub not connected")
}

func (h *GitHubHandler) githubGet(url string) ([]byte, int, error) {
	token, err := h.getToken()
	if err != nil {
		return nil, 401, err
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 500, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 502, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func (h *GitHubHandler) githubPost(url string, payload interface{}) ([]byte, int, error) {
	token, err := h.getToken()
	if err != nil {
		return nil, 401, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, 500, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 500, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 502, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// CreatePR creates a pull request on GitHub. Returns the parsed PR data.
func (h *GitHubHandler) CreatePR(owner, repo, title, body, head, base string) (map[string]interface{}, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo)
	payload := map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}

	respBody, status, err := h.githubPost(url, payload)
	if err != nil {
		return nil, fmt.Errorf("GitHub PR creation request failed: %w", err)
	}

	if status != 201 {
		// 422 means PR already exists — try to return the existing one
		if status == 422 {
			return nil, fmt.Errorf("PR already exists for branch %s", head)
		}
		return nil, fmt.Errorf("GitHub PR creation failed (status %d): %s", status, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse PR response: %w", err)
	}

	h.logger.Info("GitHub PR created",
		zap.String("owner", owner),
		zap.String("repo", repo),
		zap.String("head", head),
		zap.Float64("number", result["number"].(float64)),
	)

	return result, nil
}

// MergePR merges a pull request on GitHub. Returns the merge result data.
func (h *GitHubHandler) MergePR(owner, repo string, prNum int, commitMessage string) (map[string]interface{}, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/merge", owner, repo, prNum)
	payload := map[string]string{
		"commit_message": commitMessage,
		"merge_method":   "squash",
	}

	respBody, status, err := h.githubPut(url, payload)
	if err != nil {
		return nil, fmt.Errorf("GitHub PR merge request failed: %w", err)
	}

	if status != 200 {
		return nil, fmt.Errorf("GitHub PR merge failed (status %d): %s", status, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// 200 response may have non-JSON body for merge
		return map[string]interface{}{"merged": true}, nil
	}

	h.logger.Info("GitHub PR merged",
		zap.String("owner", owner),
		zap.String("repo", repo),
		zap.Int("pr", prNum),
	)

	return result, nil
}

// githubPut sends a PUT request to the GitHub API.
func (h *GitHubHandler) githubPut(url string, payload interface{}) ([]byte, int, error) {
	token, err := h.getToken()
	if err != nil {
		return nil, 401, err
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, 500, err
	}

	req, err := http.NewRequest("PUT", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 500, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, 502, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// parseOwnerRepo extracts owner and repo from a git remote URL.
// Supports: https://github.com/owner/repo.git, git@github.com:owner/repo.git
func parseOwnerRepo(remoteURL string) (owner, repo string, err error) {
	// HTTPS: https://github.com/owner/repo.git
	httpsRe := regexp.MustCompile(`github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$`)
	matches := httpsRe.FindStringSubmatch(remoteURL)
	if len(matches) == 3 {
		return matches[1], matches[2], nil
	}
	return "", "", fmt.Errorf("cannot parse owner/repo from remote URL: %s", remoteURL)
}

// GetRepos returns the authenticated user's GitHub repositories.
func (h *GitHubHandler) GetRepos(w http.ResponseWriter, r *http.Request) {
	// Fetch repos sorted by recently updated
	body, status, err := h.githubGet("https://api.github.com/user/repos?sort=updated&per_page=100&type=all")
	if err != nil || status != 200 {
		h.logger.Error("GitHub repos fetch failed", zap.Error(err), zap.Int("status", status))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
		})
		return
	}

	var rawRepos []interface{}
	json.Unmarshal(body, &rawRepos)

	// Extract only the fields the frontend needs
	type repo struct {
		Name        string `json:"name"`
		FullName    string `json:"full_name"`
		HtmlUrl     string `json:"html_url"`
		Description string `json:"description"`
		Private     bool   `json:"private"`
		UpdatedAt   string `json:"updated_at"`
	}

	repos := make([]repo, 0, len(rawRepos))
	for _, raw := range rawRepos {
		if m, ok := raw.(map[string]interface{}); ok {
			repos = append(repos, repo{
				Name:        strVal(m["name"]),
				FullName:    strVal(m["full_name"]),
				HtmlUrl:     strVal(m["html_url"]),
				Description: strVal(m["description"]),
				Private:     m["private"] == true,
				UpdatedAt:   strVal(m["updated_at"]),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected": true,
		"repos":     repos,
	})
}

func strVal(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// GetPRs returns open pull requests for a repo
func (h *GitHubHandler) GetPRs(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo required", http.StatusBadRequest)
		return
	}

	body, status, err := h.githubGet(fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/pulls?state=open&per_page=20",
		owner, repo))
	if err != nil {
		h.logger.Error("GitHub PRs fetch failed", zap.Error(err))
		http.Error(w, err.Error(), status)
		return
	}

	var prs []interface{}
	json.Unmarshal(body, &prs)
	if prs == nil {
		prs = []interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"prs": prs,
	})
}

// GetStats returns repo stats (stars, issues, forks)
func (h *GitHubHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo required", http.StatusBadRequest)
		return
	}

	body, status, err := h.githubGet(fmt.Sprintf(
		"https://api.github.com/repos/%s/%s", owner, repo))
	if err != nil {
		h.logger.Error("GitHub stats fetch failed", zap.Error(err))
		http.Error(w, err.Error(), status)
		return
	}

	var data map[string]interface{}
	json.Unmarshal(body, &data)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"stats": map[string]interface{}{
			"stars":    data["stargazers_count"],
			"issues":   data["open_issues_count"],
			"forks":    data["forks_count"],
			"language": data["language"],
		},
	})
}

// GetBranches returns branches for a repo
func (h *GitHubHandler) GetBranches(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo required", http.StatusBadRequest)
		return
	}

	body, status, err := h.githubGet(fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/branches?per_page=30",
		owner, repo))
	if err != nil {
		h.logger.Error("GitHub branches fetch failed", zap.Error(err))
		http.Error(w, err.Error(), status)
		return
	}

	var branches []interface{}
	json.Unmarshal(body, &branches)
	if branches == nil {
		branches = []interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"branches": branches,
	})
}

// GetActivity returns recent events for a repo
func (h *GitHubHandler) GetActivity(w http.ResponseWriter, r *http.Request) {
	owner := r.URL.Query().Get("owner")
	repo := r.URL.Query().Get("repo")
	if owner == "" || repo == "" {
		http.Error(w, "owner and repo required", http.StatusBadRequest)
		return
	}

	body, status, err := h.githubGet(fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/events?per_page=30",
		owner, repo))
	if err != nil {
		h.logger.Error("GitHub activity fetch failed", zap.Error(err))
		http.Error(w, err.Error(), status)
		return
	}

	var events []interface{}
	json.Unmarshal(body, &events)
	if events == nil {
		events = []interface{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
	})
}

// GetAllRepoActivity returns events across all the user's repos
func (h *GitHubHandler) GetAllRepoActivity(w http.ResponseWriter, r *http.Request) {
	token, err := h.getToken()
	if err != nil {
		http.Error(w, "GitHub not connected", http.StatusUnauthorized)
		return
	}

	// Get user's received events
	req, err := http.NewRequest("GET", "https://api.github.com/users/"+r.URL.Query().Get("owner")+"/events?per_page=30", nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.client.Do(req)
	if err != nil {
		h.logger.Error("GitHub user events fetch failed", zap.Error(err))
		http.Error(w, "Failed to fetch events", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Filter to only repos we care about if repos param is provided
	reposParam := r.URL.Query().Get("repos")
	if reposParam != "" {
		repoSet := map[string]bool{}
		for _, r := range strings.Split(reposParam, ",") {
			repoSet[strings.TrimSpace(r)] = true
		}

		var allEvents []interface{}
		json.Unmarshal(body, &allEvents)

		var filtered []interface{}
		for _, ev := range allEvents {
			if m, ok := ev.(map[string]interface{}); ok {
				if repo, ok := m["repo"].(map[string]interface{}); ok {
					if name, ok := repo["name"].(string); ok {
						if repoSet[name] {
							filtered = append(filtered, ev)
						}
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"events": filtered})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}
