package handlers

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/auto-developer-orchestrator/backend/internal/models"
	"go.uber.org/zap"
)

// ProviderInfo describes a known LLM provider for the settings UI.
type ProviderInfo struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	BaseURL string          `json:"baseUrl"`
	HasKey  bool            `json:"hasKey"`
	Models  []ProviderModel `json:"models"`
}

// ProviderModel describes a model offered by a provider.
type ProviderModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// knownProviders defines the providers the UI can configure.
var knownProviders = []ProviderInfo{
	{
		ID: "gemini", Name: "Google Gemini",
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Models: []ProviderModel{
			{ID: "gemini-3-flash-preview", Name: "Gemini 3 Flash Preview"},
			{ID: "gemini-3-flash", Name: "Gemini 3 Flash"},
			{ID: "gemini-3-pro", Name: "Gemini 3 Pro"},
		},
	},
	{
		ID: "openai", Name: "OpenAI",
		BaseURL: "https://api.openai.com/v1",
		Models: []ProviderModel{
			{ID: "gpt-4o", Name: "GPT-4o"},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini"},
			{ID: "o3-mini", Name: "o3 Mini"},
		},
	},
	{
		ID: "anthropic", Name: "Anthropic",
		BaseURL: "https://api.anthropic.com/v1",
		Models: []ProviderModel{
			{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
			{ID: "claude-haiku-4-20250506", Name: "Claude Haiku 4.5"},
		},
	},
	{
		ID: "openrouter", Name: "OpenRouter",
		BaseURL: "https://openrouter.ai/api",
		Models: []ProviderModel{
			{ID: "deepseek/deepseek-v4-flash", Name: "DeepSeek V4 Flash"},
		},
	},
}

// GetModels returns the current main and tool model configuration.
func (h *ConfigHandler) GetModels(w http.ResponseWriter, r *http.Request) {
	if h.modelCfg == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"mainModel": nil, "toolModel": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mainModel": h.modelCfg.MainModel(),
		"toolModel": h.modelCfg.ToolModel(),
	})
}

// SetModels updates the main and/or tool model configuration.
func (h *ConfigHandler) SetModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MainModel *models.ModelEntry `json:"mainModel,omitempty"`
		ToolModel *models.ModelEntry `json:"toolModel,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if h.modelCfg == nil {
		JSONError(w, "Model config not available", http.StatusInternalServerError)
		return
	}
	if req.MainModel != nil && req.MainModel.Provider != "" && req.MainModel.ModelId != "" {
		if err := h.modelCfg.SetMainModel(req.MainModel.Provider, req.MainModel.ModelId); err != nil {
			h.logger.Error("Failed to set main model", zap.Error(err))
			JSONError(w, "Failed to persist main model", http.StatusInternalServerError)
			return
		}
		os.Setenv("TOOL_MODEL", h.modelCfg.ToolModel().ModelId)
	}
	if req.ToolModel != nil && req.ToolModel.Provider != "" && req.ToolModel.ModelId != "" {
		if err := h.modelCfg.SetToolModel(req.ToolModel.Provider, req.ToolModel.ModelId); err != nil {
			h.logger.Error("Failed to set tool model", zap.Error(err))
			JSONError(w, "Failed to persist tool model", http.StatusInternalServerError)
			return
		}
		os.Setenv("TOOL_MODEL", h.modelCfg.ToolModel().ModelId)
	}
	h.logger.Info("Model config updated")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"mainModel": h.modelCfg.MainModel(),
		"toolModel": h.modelCfg.ToolModel(),
	})
}

// GetProviders returns available LLM providers with their configuration status.
func (h *ConfigHandler) GetProviders(w http.ResponseWriter, r *http.Request) {
	savedKeys := h.loadProviderKeys()
	result := make([]ProviderInfo, len(knownProviders))
	for i, p := range knownProviders {
		result[i] = p
		result[i].HasKey = savedKeys[p.ID] != ""
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": result})
}

// SetProviderKey saves an API key for a provider and persists to settings.json.
func (h *ConfigHandler) SetProviderKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	found := false
	for _, p := range knownProviders {
		if p.ID == req.Provider {
			found = true
			break
		}
	}
	if !found {
		JSONError(w, "Unknown provider: "+req.Provider, http.StatusBadRequest)
		return
	}
	if err := h.saveProviderKey(req.Provider, req.APIKey); err != nil {
		h.logger.Error("Failed to save provider key", zap.String("provider", req.Provider), zap.Error(err))
		JSONError(w, "Failed to save API key", http.StatusInternalServerError)
		return
	}
	h.logger.Info("Provider API key saved", zap.String("provider", req.Provider))
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *ConfigHandler) loadProviderKeys() map[string]string {
	keys := make(map[string]string)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return keys
	}
	data, err := os.ReadFile(homeDir + "/.pi/agent/settings.json")
	if err != nil {
		return keys
	}
	var settings struct {
		Providers map[string]struct {
			APIKey string `json:"apiKey"`
		} `json:"providers"`
	}
	if json.Unmarshal(data, &settings) != nil {
		return keys
	}
	for id, p := range settings.Providers {
		if p.APIKey != "" {
			keys[id] = p.APIKey
		}
	}
	return keys
}

func (h *ConfigHandler) saveProviderKey(providerID, apiKey string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := homeDir + "/.pi/agent/settings.json"
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}
	var baseURL string
	for _, p := range knownProviders {
		if p.ID == providerID {
			baseURL = p.BaseURL
			break
		}
	}
	providers, ok := existing["providers"].(map[string]any)
	if !ok {
		providers = make(map[string]any)
		existing["providers"] = providers
	}
	providerEntry, ok := providers[providerID].(map[string]any)
	if !ok {
		providerEntry = make(map[string]any)
		providers[providerID] = providerEntry
	}
	providerEntry["apiKey"] = apiKey
	providerEntry["baseUrl"] = baseURL
	providerEntry["api"] = "openai-completions"
	var models []ProviderModel
	for _, p := range knownProviders {
		if p.ID == providerID {
			models = p.Models
			break
		}
	}
	modelsArr := make([]any, len(models))
	for i, m := range models {
		modelsArr[i] = map[string]any{
			"id": m.ID, "name": m.Name, "api": "openai-completions",
			"reasoning": true, "input": []string{"text", "image"},
			"cost":         map[string]float64{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
			"contextWindow": 0, "maxTokens": 65536,
		}
	}
	providerEntry["models"] = modelsArr
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
