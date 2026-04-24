package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/models"
	"github.com/auto-developer-orchestrator/backend/internal/storage"
	"go.uber.org/zap"
)

// ProviderInfo describes a known LLM provider for the settings UI.
type ProviderInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	HasKey  bool   `json:"hasKey"` // true if an API key is saved (key itself never sent to frontend)
	Models  []ProviderModel `json:"models"`
}

// ProviderModel describes a model offered by a provider.
type ProviderModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// knownProviders defines the providers the UI can configure.
// Models are hardcoded here — the user just needs to provide an API key.
var knownProviders = []ProviderInfo{
	{
		ID:      "gemini",
		Name:    "Google Gemini",
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Models: []ProviderModel{
			{ID: "gemini-3-flash-preview", Name: "Gemini 3 Flash Preview"},
			{ID: "gemini-3-flash", Name: "Gemini 3 Flash"},
			{ID: "gemini-3-pro", Name: "Gemini 3 Pro"},
		},
	},
	{
		ID:      "openai",
		Name:    "OpenAI",
		BaseURL: "https://api.openai.com/v1",
		Models: []ProviderModel{
			{ID: "gpt-4o", Name: "GPT-4o"},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini"},
			{ID: "o3-mini", Name: "o3 Mini"},
		},
	},
	{
		ID:      "anthropic",
		Name:    "Anthropic",
		BaseURL: "https://api.anthropic.com/v1",
		Models: []ProviderModel{
			{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
			{ID: "claude-haiku-4-20250506", Name: "Claude Haiku 4.5"},
		},
	},
	{
		ID:      "openrouter",
		Name:    "OpenRouter",
		BaseURL: "https://openrouter.ai/api",
		Models: []ProviderModel{
			{ID: "deepseek/deepseek-v4-flash", Name: "DeepSeek V4 Flash"},
		},
	},
}

// ConfigHandler handles configuration requests
type ConfigHandler struct {
	logger     *zap.Logger
	config     *Config
	mu         sync.RWMutex
	tokenStore *GitHubTokenStore
	modelCfg   *models.ModelConfig
	db         *storage.Database
}

// Config represents the AI configuration
type Config struct {
	AutoTask           bool      `json:"autoTask"`
	AutoTest           bool      `json:"autoTest"`
	FullAutomationMode bool      `json:"fullAutomationMode"`
	PostMergeTestGen   bool      `json:"postMergeTestGen"`
	TestGenPrompt      string    `json:"testGenPrompt"`
	TestTypes          TestTypes `json:"testTypes"`
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
func NewConfigHandler(logger *zap.Logger, tokenStore *GitHubTokenStore, modelCfg *models.ModelConfig, db *storage.Database) *ConfigHandler {
	return &ConfigHandler{
		logger:     logger,
		tokenStore: tokenStore,
		modelCfg:   modelCfg,
		db:         db,
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

// GetModels returns the current main and tool model configuration.
// GET /api/config/models
func (h *ConfigHandler) GetModels(w http.ResponseWriter, r *http.Request) {
	if h.modelCfg == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"mainModel": nil,
			"toolModel": nil,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mainModel": h.modelCfg.MainModel(),
		"toolModel": h.modelCfg.ToolModel(),
	})
}

// SetModels updates the main and/or tool model configuration.
// PUT /api/config/models
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
		// Update TOOL_MODEL env var for new Pi processes
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

	h.logger.Info("Model config updated",
		zap.Any("mainModel", h.modelCfg.MainModel()),
		zap.Any("toolModel", h.modelCfg.ToolModel()),
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"mainModel": h.modelCfg.MainModel(),
		"toolModel": h.modelCfg.ToolModel(),
	})
}

// GetProjectSettings returns per-project settings overrides.
// GET /api/config/project?project=<name>
func (h *ConfigHandler) GetProjectSettings(w http.ResponseWriter, r *http.Request) {
	projectName := r.URL.Query().Get("project")
	if projectName == "" {
		JSONError(w, "project query param required", http.StatusBadRequest)
		return
	}

	projectPath := resolveProjectPath(projectName, h.db)
	if projectPath == "" {
		JSONError(w, "Project not found", http.StatusNotFound)
		return
	}

	ps := models.LoadProjectSettings(projectPath, h.logger)
	writeJSON(w, http.StatusOK, ps)
}

// SetProjectSettings updates per-project settings overrides.
// PUT /api/config/project?project=<name>
func (h *ConfigHandler) SetProjectSettings(w http.ResponseWriter, r *http.Request) {
	projectName := r.URL.Query().Get("project")
	if projectName == "" {
		JSONError(w, "project query param required", http.StatusBadRequest)
		return
	}

	projectPath := resolveProjectPath(projectName, h.db)
	if projectPath == "" {
		JSONError(w, "Project not found", http.StatusNotFound)
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ps := models.LoadProjectSettings(projectPath, h.logger)
	if err := ps.Update(updates); err != nil {
		h.logger.Error("Failed to save project settings", zap.Error(err))
		JSONError(w, "Failed to save settings", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"settings": ps,
	})
}

// GetProviders returns available LLM providers with their configuration status.
// API keys are never sent to the frontend — only a boolean "hasKey" flag.
// GET /api/config/providers
func (h *ConfigHandler) GetProviders(w http.ResponseWriter, r *http.Request) {
	// Read saved API keys from settings.json to check which are configured
	savedKeys := h.loadProviderKeys()

	result := make([]ProviderInfo, len(knownProviders))
	for i, p := range knownProviders {
		result[i] = p
		result[i].HasKey = savedKeys[p.ID] != ""
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": result,
	})
}

// SetProviderKey saves an API key for a provider and persists to settings.json.
// PUT /api/config/providers
func (h *ConfigHandler) SetProviderKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"` // e.g. "gemini", "openai", "anthropic"
		APIKey   string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate the provider is known
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

	// Save to settings.json
	if err := h.saveProviderKey(req.Provider, req.APIKey); err != nil {
		h.logger.Error("Failed to save provider key", zap.String("provider", req.Provider), zap.Error(err))
		JSONError(w, "Failed to save API key", http.StatusInternalServerError)
		return
	}

	h.logger.Info("Provider API key saved", zap.String("provider", req.Provider))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

// loadProviderKeys reads API keys from ~/.pi/agent/settings.json providers section.
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

// saveProviderKey updates a provider's API key in ~/.pi/agent/settings.json.
// If the provider doesn't exist yet, it creates it with the appropriate baseUrl.
func (h *ConfigHandler) saveProviderKey(providerID, apiKey string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	path := homeDir + "/.pi/agent/settings.json"

	// Read existing
	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &existing)
	}

	// Find base URL for this provider
	var baseURL string
	for _, p := range knownProviders {
		if p.ID == providerID {
			baseURL = p.BaseURL
			break
		}
	}

	// Ensure providers map exists
	providers, ok := existing["providers"].(map[string]any)
	if !ok {
		providers = make(map[string]any)
		existing["providers"] = providers
	}

	// Update or create the provider entry
	providerEntry, ok := providers[providerID].(map[string]any)
	if !ok {
		providerEntry = make(map[string]any)
		providers[providerID] = providerEntry
	}
	providerEntry["apiKey"] = apiKey
	providerEntry["baseUrl"] = baseURL
	providerEntry["api"] = "openai-completions"

	// Build models array for this provider
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
			"id":           m.ID,
			"name":         m.Name,
			"api":          "openai-completions",
			"reasoning":    true,
			"input":        []string{"text", "image"},
			"cost":         map[string]float64{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
			"contextWindow": 1048576,
			"maxTokens":    65536,
		}
	}
	providerEntry["models"] = modelsArr

	// Write back
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600) // 0600: only owner can read (contains API keys)
}

// AgentConfigDTO is the JSON representation of the tuning knobs exposed to the frontend.
// Only includes user-adjustable fields from llama.ModelConfig.
type AgentConfigDTO struct {
	// Context
	DefaultContextSize  int `json:"defaultContextSize"`
	SubAgentContextSize int `json:"subAgentContextSize"`

	// Generation
	MaxTokens   int     `json:"maxTokens"`
	Temperature float32 `json:"temperature"`
	TopP        float32 `json:"topP"`
	TopK        int     `json:"topK"`

	// Agent loop
	ThinkingBudgetTokens int `json:"thinkingBudgetTokens"`
	DefaultMaxToolRounds int `json:"defaultMaxToolRounds"`
	BrowserMaxToolRounds int `json:"browserMaxToolRounds"`
	ToolExecTimeoutSec   int `json:"toolExecTimeoutSec"`
	ToolResultMaxChars   int `json:"toolResultMaxChars"`

	// Compaction
	MicroCompactThreshold float64 `json:"microCompactThreshold"`
	FullCompactThreshold  float64 `json:"fullCompactThreshold"`

	// VRAM
	MaxConcurrentAgents int `json:"maxConcurrentAgents"`

	// Plan-first workflow
	PlanApprovalEnabled bool `json:"planApprovalEnabled"`
}

// GetAgent returns the current agent tuning configuration.
// GET /api/config/agent
func (h *ConfigHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	cfg := llama.GetModelConfig()
	writeJSON(w, http.StatusOK, AgentConfigDTO{
		DefaultContextSize:    cfg.DefaultContextSize,
		SubAgentContextSize:   cfg.SubAgentContextSize,
		MaxTokens:             cfg.MaxTokens,
		Temperature:           cfg.Temperature,
		TopP:                  cfg.TopP,
		TopK:                  cfg.TopK,
		ThinkingBudgetTokens:  cfg.ThinkingBudgetTokens,
		DefaultMaxToolRounds:  cfg.DefaultMaxToolRounds,
		BrowserMaxToolRounds:  cfg.BrowserMaxToolRounds,
		ToolExecTimeoutSec:    cfg.ToolExecTimeoutSec,
		ToolResultMaxChars:    cfg.ToolResultMaxChars,
		MicroCompactThreshold: cfg.MicroCompactThreshold,
		FullCompactThreshold:  cfg.FullCompactThreshold,
		MaxConcurrentAgents:   cfg.MaxConcurrentAgents,
		PlanApprovalEnabled:   cfg.PlanApprovalEnabled,
	})
}

// SetAgent updates the agent tuning configuration.
// PUT /api/config/agent
func (h *ConfigHandler) SetAgent(w http.ResponseWriter, r *http.Request) {
	var req AgentConfigDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	cfg := llama.GetModelConfig()

	// Apply only non-zero fields (partial update)
	if req.DefaultContextSize > 0 {
		cfg.DefaultContextSize = req.DefaultContextSize
	}
	if req.SubAgentContextSize > 0 {
		cfg.SubAgentContextSize = req.SubAgentContextSize
	}
	if req.MaxTokens > 0 {
		cfg.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		cfg.Temperature = req.Temperature
	}
	if req.TopP > 0 {
		cfg.TopP = req.TopP
	}
	if req.TopK > 0 {
		cfg.TopK = req.TopK
	}
	if req.ThinkingBudgetTokens >= 0 {
		cfg.ThinkingBudgetTokens = req.ThinkingBudgetTokens
	}
	if req.DefaultMaxToolRounds > 0 {
		cfg.DefaultMaxToolRounds = req.DefaultMaxToolRounds
	}
	if req.BrowserMaxToolRounds > 0 {
		cfg.BrowserMaxToolRounds = req.BrowserMaxToolRounds
	}
	if req.ToolExecTimeoutSec > 0 {
		cfg.ToolExecTimeoutSec = req.ToolExecTimeoutSec
	}
	if req.ToolResultMaxChars > 0 {
		cfg.ToolResultMaxChars = req.ToolResultMaxChars
	}
	if req.MicroCompactThreshold > 0 {
		cfg.MicroCompactThreshold = req.MicroCompactThreshold
	}
	if req.FullCompactThreshold > 0 {
		cfg.FullCompactThreshold = req.FullCompactThreshold
	}
	if req.MaxConcurrentAgents > 0 {
		cfg.MaxConcurrentAgents = req.MaxConcurrentAgents
	}
	cfg.PlanApprovalEnabled = req.PlanApprovalEnabled

	llama.SetModelConfig(cfg)

	h.logger.Info("Agent config updated",
		zap.Float32("temperature", cfg.Temperature),
		zap.Int("thinkingBudget", cfg.ThinkingBudgetTokens),
		zap.Int("maxTokens", cfg.MaxTokens),
		zap.Int("maxToolRounds", cfg.DefaultMaxToolRounds),
		zap.Bool("planApprovalEnabled", cfg.PlanApprovalEnabled),
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"config": AgentConfigDTO{
			DefaultContextSize:    cfg.DefaultContextSize,
			SubAgentContextSize:   cfg.SubAgentContextSize,
			MaxTokens:             cfg.MaxTokens,
			Temperature:           cfg.Temperature,
			TopP:                  cfg.TopP,
			TopK:                  cfg.TopK,
			ThinkingBudgetTokens:  cfg.ThinkingBudgetTokens,
			DefaultMaxToolRounds:  cfg.DefaultMaxToolRounds,
			BrowserMaxToolRounds:  cfg.BrowserMaxToolRounds,
			ToolExecTimeoutSec:    cfg.ToolExecTimeoutSec,
			ToolResultMaxChars:    cfg.ToolResultMaxChars,
			MicroCompactThreshold: cfg.MicroCompactThreshold,
			FullCompactThreshold:  cfg.FullCompactThreshold,
			MaxConcurrentAgents:   cfg.MaxConcurrentAgents,
			PlanApprovalEnabled:   cfg.PlanApprovalEnabled,
		},
	})
}