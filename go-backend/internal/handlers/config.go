package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"go.uber.org/zap"
)

// ConfigHandler handles configuration requests
type ConfigHandler struct {
	logger     *zap.Logger
	config     *Config
	mu         sync.RWMutex
	tokenStore *GitHubTokenStore
}

// Config represents the AI configuration
type Config struct {
	AutoTask            bool     `json:"autoTask"`
	AutoTest            bool     `json:"autoTest"`
	FullAutomationMode  bool     `json:"fullAutomationMode"`
	PostMergeTestGen    bool     `json:"postMergeTestGen"`
	TestGenPrompt       string   `json:"testGenPrompt"`
	TestTypes           TestTypes `json:"testTypes"`
}

// TestTypes represents test type configuration
type TestTypes struct {
	Unit        bool `json:"unit"`
	E2E         bool `json:"e2e"`
	Integration bool `json:"integration"`
	Chaos       bool `json:"chaos"`
	Security    bool `json:"security"`
	Performance bool `json:"performance"`
}

// NewConfigHandler creates a new ConfigHandler
func NewConfigHandler(logger *zap.Logger, tokenStore *GitHubTokenStore) *ConfigHandler {
	return &ConfigHandler{
		logger:     logger,
		tokenStore: tokenStore,
		config: &Config{
			AutoTask:           true,
			AutoTest:           true,
			FullAutomationMode: false,
			PostMergeTestGen:   false,
			TestGenPrompt:      "Generate comprehensive tests for the recent changes, ensuring edge cases are covered.",
			TestTypes: TestTypes{
				Unit:        true,
				E2E:         true,
				Integration: false,
				Chaos:       false,
				Security:    false,
				Performance: false,
			},
		},
	}
}

// GetAI returns the current AI configuration
func (h *ConfigHandler) GetAI(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	writeJSON(w, http.StatusOK, h.config)
}

// SetAI updates the AI configuration
func (h *ConfigHandler) SetAI(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var newConfig Config
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.config = &newConfig

	h.logger.Info("AI Config updated", zap.Any("config", h.config))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"aiConfig": h.config,
	})
}

// GetSystem returns the system configuration
func (h *ConfigHandler) GetSystem(w http.ResponseWriter, r *http.Request) {
	// This would come from database in production
	systemConfig := map[string]string{
		"projectsDir": "/app/projects",
	}

	writeJSON(w, http.StatusOK, systemConfig)
}

// SetSystem updates the system configuration
func (h *ConfigHandler) SetSystem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectsDir string `json:"projectsDir"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Store in database
	h.logger.Info("System config updated", zap.String("projectsDir", req.ProjectsDir))

	systemConfig := map[string]string{
		"projectsDir": req.ProjectsDir,
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"systemConfig": systemConfig,
	})
}

// GitHubUserResponse represents the GitHub user info response
type GitHubUserResponse struct {
	Connected bool          `json:"connected"`
	User      *GitHubUserInfo `json:"user,omitempty"`
}

// GitHubUserInfo represents GitHub user information
type GitHubUserInfo struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// GetGitHubUser returns the current GitHub user info
func (h *ConfigHandler) GetGitHubUser(w http.ResponseWriter, r *http.Request) {
	token := h.tokenStore.Get()
	if token == "" {
		writeJSON(w, http.StatusOK, GitHubUserResponse{
			Connected: false,
		})
		return
	}

	// Call GitHub API to get user info
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		writeJSON(w, http.StatusOK, GitHubUserResponse{Connected: false})
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.tokenStore.Get())
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, GitHubUserResponse{Connected: false})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		writeJSON(w, http.StatusOK, GitHubUserResponse{Connected: false})
		return
	}

	var user struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		writeJSON(w, http.StatusOK, GitHubUserResponse{Connected: false})
		return
	}

	writeJSON(w, http.StatusOK, GitHubUserResponse{
		Connected: true,
		User: &GitHubUserInfo{
			Login:     user.Login,
			Name:      user.Name,
			Email:     user.Email,
			AvatarURL: user.AvatarURL,
		},
	})
}

// ConnectGitHub connects a GitHub account via token
func (h *ConfigHandler) ConnectGitHub(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Token == "" {
		JSONError(w, "Token is required", http.StatusBadRequest)
		return
	}

	// Verify token by calling GitHub API
	gitReq, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		JSONError(w, "Failed to verify token", http.StatusInternalServerError)
		return
	}
	gitReq.Header.Set("Authorization", "Bearer "+req.Token)
	gitReq.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(gitReq)
	if err != nil {
		JSONError(w, "Failed to verify token", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "Invalid GitHub token",
		})
		return
	}

	// Store token
	h.tokenStore.Set(req.Token)

	h.logger.Info("GitHub account connected")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}
