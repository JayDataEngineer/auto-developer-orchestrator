package handlers

import (
	"encoding/json"
	"net/http"
)

// GitHubUserResponse represents the GitHub user info response
type GitHubUserResponse struct {
	Connected bool            `json:"connected"`
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
		writeJSON(w, http.StatusOK, GitHubUserResponse{Connected: false})
		return
	}

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
	req, ok := decodeReq[struct {
		Token string `json:"token"`
	}](w, r)
	if !ok { return }
	if req.Token == "" {
		JSONError(w, "Token is required", http.StatusBadRequest)
		return
	}

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

	h.tokenStore.Set(req.Token)
	h.logger.Info("GitHub account connected")
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
