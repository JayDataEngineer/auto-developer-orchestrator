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

// GetProviders returns all configured providers with full model metadata.
// GET /api/pux/providers
func (h *PuxHandler) GetProviders(w http.ResponseWriter, r *http.Request) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"providers": map[string]interface{}{}})
		return
	}

	settingsPath := filepath.Join(homeDir, ".pi", "agent", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"providers": map[string]interface{}{}})
		return
	}

	var settings struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			API     string `json:"api"`
			APIKey  string `json:"apiKey"`
			Compat  struct {
				SupportsDeveloperRole   bool `json:"supportsDeveloperRole"`
				SupportsReasoningEffort bool `json:"supportsReasoningEffort"`
			} `json:"compat"`
			Models []struct {
				ID            string   `json:"id"`
				Name          string   `json:"name"`
				Reasoning     bool     `json:"reasoning"`
				Input         []string `json:"input"`
				Cost          struct {
					Input      float64 `json:"input"`
					Output     float64 `json:"output"`
					CacheRead  float64 `json:"cacheRead"`
					CacheWrite float64 `json:"cacheWrite"`
				} `json:"cost"`
				ContextWindow int `json:"contextWindow"`
				MaxTokens     int `json:"maxTokens"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"providers": map[string]interface{}{}})
		return
	}

	result := make(map[string]interface{})
	for name, p := range settings.Providers {
		status := "configured"
		if h.providerAvailable(name) {
			status = "available"
		}

		models := make([]map[string]interface{}, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, map[string]interface{}{
				"id":            m.ID,
				"name":          m.Name,
				"reasoning":     m.Reasoning,
				"input":         m.Input,
				"cost":          map[string]float64{"input": m.Cost.Input, "output": m.Cost.Output, "cacheRead": m.Cost.CacheRead, "cacheWrite": m.Cost.CacheWrite},
				"contextWindow": m.ContextWindow,
				"maxTokens":     m.MaxTokens,
			})
		}

		result[name] = map[string]interface{}{
			"baseUrl": p.BaseURL,
			"api":     p.API,
			"status":  status,
			"compat":  map[string]bool{"supportsDeveloperRole": p.Compat.SupportsDeveloperRole, "supportsReasoningEffort": p.Compat.SupportsReasoningEffort},
			"models":  models,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": result})
}

// providerAvailable checks if a provider has a live engine.
func (h *PuxHandler) providerAvailable(name string) bool {
	switch name {
	case "llamacpp":
		return h.llamaEngine != nil
	case "gemini":
		return h.geminiEngine != nil
	case "openrouter":
		return h.openrouterEngine != nil
	case "cluster":
		return h.clusterEngine != nil
	default:
		return false
	}
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

// AddProvider adds or updates a provider in settings.json.
// POST /api/pux/providers
func (h *PuxHandler) AddProvider(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[struct {
		ID       string `json:"id"`
		BaseURL  string `json:"baseUrl"`
		APIKey   string `json:"apiKey"`
		API      string `json:"api"`
		Models   []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextWindow int    `json:"contextWindow"`
			MaxTokens     int    `json:"maxTokens"`
		} `json:"models"`
	}](w, r)
	if !ok {
		return
	}

	if req.ID == "" || req.BaseURL == "" {
		JSONError(w, "id and baseUrl are required", http.StatusBadRequest)
		return
	}
	if len(req.Models) == 0 {
		JSONError(w, "at least one model is required", http.StatusBadRequest)
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		JSONError(w, "cannot determine home directory", http.StatusInternalServerError)
		return
	}

	settingsPath := filepath.Join(homeDir, ".pi", "agent", "settings.json")

	// Read existing settings
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		JSONError(w, "cannot read settings.json", http.StatusInternalServerError)
		return
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		JSONError(w, "invalid settings.json", http.StatusInternalServerError)
		return
	}

	// Get or create providers map
	var providers map[string]json.RawMessage
	if raw, ok := settings["providers"]; ok {
		if err := json.Unmarshal(raw, &providers); err != nil {
			JSONError(w, "invalid providers in settings.json", http.StatusInternalServerError)
			return
		}
	} else {
		providers = make(map[string]json.RawMessage)
	}

	// Build provider entry
	api := req.API
	if api == "" {
		api = "openai-completions"
	}
	modelEntries := make([]map[string]interface{}, 0, len(req.Models))
	for _, m := range req.Models {
		entry := map[string]interface{}{
			"id":            m.ID,
			"name":          m.Name,
			"api":           api,
			"reasoning":     false,
			"input":         []string{"text"},
			"cost":          map[string]float64{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
			"contextWindow": m.ContextWindow,
			"maxTokens":     m.MaxTokens,
		}
		modelEntries = append(modelEntries, entry)
	}

	providerEntry := map[string]interface{}{
		"baseUrl": req.BaseURL,
		"api":     api,
		"apiKey":  req.APIKey,
		"compat": map[string]bool{
			"supportsDeveloperRole":   false,
			"supportsReasoningEffort": false,
		},
		"models": modelEntries,
	}

	providerJSON, _ := json.Marshal(providerEntry)
	providers[req.ID] = providerJSON
	settings["providers"], _ = json.Marshal(providers)

	// Write back
	output, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		JSONError(w, "failed to marshal settings", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(settingsPath, output, 0600); err != nil {
		JSONError(w, "failed to write settings.json", http.StatusInternalServerError)
		return
	}

	h.log.Info("Provider added",
		zap.String("provider", req.ID),
		zap.String("baseUrl", req.BaseURL),
		zap.Int("models", len(req.Models)),
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": req.ID,
	})
}
