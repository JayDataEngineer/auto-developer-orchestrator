package handlers

import (
	"net/http"

	"github.com/auto-developer-orchestrator/backend/internal/llama"
	"github.com/auto-developer-orchestrator/backend/internal/models"
	"go.uber.org/zap"
)

// GetProjectSettings returns per-project settings overrides.
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
	updates, ok := decodeReq[map[string]interface{}](w, r)
	if !ok { return }
	ps := models.LoadProjectSettings(projectPath, h.logger)
	if err := ps.Update(*updates); err != nil {
		h.logger.Error("Failed to save project settings", zap.Error(err))
		JSONError(w, "Failed to save settings", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "settings": ps})
}

// AgentConfigDTO is the JSON representation of the tuning knobs exposed to the frontend.
type AgentConfigDTO struct {
	DefaultContextSize    int     `json:"defaultContextSize"`
	SubAgentContextSize   int     `json:"subAgentContextSize"`
	MaxTokens             int     `json:"maxTokens"`
	Temperature           float32 `json:"temperature"`
	TopP                  float32 `json:"topP"`
	TopK                  int     `json:"topK"`
	ThinkingBudgetTokens  int     `json:"thinkingBudgetTokens"`
	DefaultMaxToolRounds  int     `json:"defaultMaxToolRounds"`
	BrowserMaxToolRounds  int     `json:"browserMaxToolRounds"`
	ToolExecTimeoutSec    int     `json:"toolExecTimeoutSec"`
	ToolResultMaxChars    int     `json:"toolResultMaxChars"`
	MicroCompactThreshold float64 `json:"microCompactThreshold"`
	FullCompactThreshold  float64 `json:"fullCompactThreshold"`
	MaxConcurrentAgents   int     `json:"maxConcurrentAgents"`
	PlanApprovalEnabled   bool    `json:"planApprovalEnabled"`
}

// GetAgent returns the current agent tuning configuration.
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
func (h *ConfigHandler) SetAgent(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeReq[AgentConfigDTO](w, r)
	if !ok { return }
	cfg := llama.GetModelConfig()
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
	h.logger.Info("Agent config updated")

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
