package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/pi"
	"go.uber.org/zap"
)

// GetModels lists available models.
func (h *PiHandler) GetModels(w http.ResponseWriter, r *http.Request) {
	// If LiteLLM proxy is configured, fetch models directly from it
	if h.litellmURL != "" {
		models, err := h.fetchLiteLLMModels(r.Context())
		if err != nil {
			h.log.Warn("Failed to fetch models from LiteLLM", zap.Error(err))
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"models": []pi.ModelInfo{},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"models": models,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"models": []pi.ModelInfo{},
	})
}

// fetchLiteLLMModels calls LiteLLM's /v1/models endpoint and returns the model list.
func (h *PiHandler) fetchLiteLLMModels(ctx context.Context) ([]pi.ModelInfo, error) {
	url := strings.TrimRight(h.litellmURL, "/") + "/v1/models"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	// Traefik routes by Host header — set it so LiteLLM backend is matched
	req.Host = "litellm.local"
	if h.litellmKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.litellmKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LiteLLM returned status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]pi.ModelInfo, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, pi.ModelInfo{
			Id:   m.ID,
			Name: m.ID,
		})
	}
	return models, nil
}

// setModelRequest is the request body for SetModel.
type setModelRequest struct {
	Project  string `json:"project"`
	Provider string `json:"provider"`
	ModelId  string `json:"modelId"`
	AgentId  string `json:"agentId,omitempty"`
}

// SetModel switches the active model.
func (h *PiHandler) SetModel(w http.ResponseWriter, r *http.Request) {
	var req setModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid request body",
		})
		return
	}

	agentId := req.AgentId
	if agentId == "" {
		agentId = "default"
	}

	projectPath := resolveProjectPath(req.Project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "No active Pi session - send a prompt first",
		})
		return
	}

	if err := client.SetModel(req.Provider, req.ModelId); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Compact triggers context compaction.
func (h *PiHandler) Compact(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	agentId := resolveAgent(r)
	projectPath := resolveProjectPath(project, h.db)
	if projectPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Project not found",
		})
		return
	}

	client := h.pool.GetWithID(projectPath, agentId)
	if client == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "No active Pi session - send a prompt first",
		})
		return
	}

	if err := client.Compact(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
