package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	llamaeng "github.com/auto-developer-orchestrator/backend/internal/llama"
	"go.uber.org/zap"
)

// GetModels returns available models from settings.json.
// GET /api/pux/models
func (h *PuxHandler) GetModels(w http.ResponseWriter, r *http.Request) {
	type modelInfo struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Provider      string `json:"provider,omitempty"`
		ContextWindow int    `json:"contextWindow,omitempty"`
	}

	models := []modelInfo{}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		settingsPath := filepath.Join(homeDir, ".pi", "agent", "settings.json")
		if data, err := os.ReadFile(settingsPath); err == nil {
			var settings struct {
				Providers map[string]struct {
					Models []struct {
						ID            string `json:"id"`
						Name          string `json:"name"`
						ContextWindow int    `json:"contextWindow"`
					} `json:"models"`
				} `json:"providers"`
			}
			if json.Unmarshal(data, &settings) == nil {
				for providerName, provider := range settings.Providers {
					for _, m := range provider.Models {
						models = append(models, modelInfo{
							ID:            m.ID,
							Name:          m.Name,
							Provider:      providerName,
							ContextWindow: m.ContextWindow,
						})
					}
				}
			}
		}
	}

	if len(models) == 0 && h.llamaEngine != nil {
		models = append(models, modelInfo{
			ID:       h.llamaEngine.ModelName(),
			Name:     h.llamaEngine.ModelName(),
			Provider: "llamacpp",
		})
	}

	writeJSON(w, http.StatusOK, models)
}

// SetModel switches the active engine for a specific agent.
// PUT /api/pux/model
func (h *PuxHandler) SetModel(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		Project  string `json:"project"`
		Provider string `json:"provider"`
		ModelID  string `json:"modelId"`
		AgentID  string `json:"agentId"`
	}](w, r)
	if !ok { return }

	var engine *llamaeng.LLMClient
	switch {
	case req.ModelID == "gemini-3-flash-preview" && h.geminiEngine != nil:
		engine = h.geminiEngine
	case strings.Contains(req.ModelID, "deepseek") && h.openrouterEngine != nil:
		engine = h.openrouterEngine
	default:
		if req.Provider != "llamacpp" && req.Provider != "" {
			if eng := h.engineFromSettings(req.Provider, req.ModelID); eng != nil {
				engine = eng
			}
		}
		if engine == nil {
			engine = h.llamaEngine
		}
	}

	if engine == nil {
		JSONError(w, "No engine available for the requested model", http.StatusServiceUnavailable)
		return
	}

	projectPath := resolveProjectPath(req.Project, h.db)
	key := compositeAgentKey(projectPath, req.AgentID)
	h.selectedEngines[key] = engine

	h.log.Info("Model switched",
		zap.String("model", req.ModelID),
		zap.String("provider", req.Provider),
		zap.String("agent", req.AgentID),
		zap.String("engine_model", engine.ModelName()),
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"model":   engine.ModelName(),
	})
}

// engineFromSettings reads a provider's apiKey and baseUrl from settings.json
// and creates a temporary LLMClient for it.
func (h *PuxHandler) engineFromSettings(providerID, modelID string) *llamaeng.LLMClient {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(homeDir + "/.pi/agent/settings.json")
	if err != nil {
		return nil
	}

	var settings struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
		} `json:"providers"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return nil
	}

	p, ok := settings.Providers[providerID]
	if !ok || p.APIKey == "" || p.BaseURL == "" {
		return nil
	}

	eng := llamaeng.NewLLMClient(llamaeng.LLMClientConfig{
		BaseURL:   p.BaseURL,
		APIKey:    p.APIKey,
		ModelName: modelID,
		Logger:    h.log,
	})
	if err := eng.LoadModel(); err != nil {
		h.log.Warn("Failed to create engine from settings", zap.String("provider", providerID), zap.Error(err))
		return nil
	}
	return eng
}

// resolveEngineForModel finds the provider for a model ID in settings.json
// and creates an LLMClient. Returns nil if the model is not found.
func (h *PuxHandler) resolveEngineForModel(modelID string) *llamaeng.LLMClient {
	switch {
	case modelID == "gemini-3-flash-preview" && h.geminiEngine != nil:
		return h.geminiEngine
	case strings.Contains(modelID, "deepseek") && h.openrouterEngine != nil:
		return h.openrouterEngine
	case strings.Contains(modelID, "qwen") && h.clusterEngine != nil:
		return h.clusterEngine
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".pi", "agent", "settings.json"))
	if err != nil {
		return nil
	}

	var settings struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			Models  []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return nil
	}

	for providerName, provider := range settings.Providers {
		for _, m := range provider.Models {
			if m.ID == modelID {
				return h.engineFromSettings(providerName, modelID)
			}
		}
	}

	if h.llamaEngine != nil && h.llamaEngine.ModelName() == modelID {
		return h.llamaEngine
	}

	return nil
}
