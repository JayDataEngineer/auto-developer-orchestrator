package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"go.uber.org/zap"
)

// ConfigHandler handles configuration requests
type ConfigHandler struct {
	logger *zap.Logger
	config *Config
	mu     sync.RWMutex
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
func NewConfigHandler(logger *zap.Logger) *ConfigHandler {
	return &ConfigHandler{
		logger: logger,
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.config)
}

// SetAI updates the AI configuration
func (h *ConfigHandler) SetAI(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var newConfig Config
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.config = &newConfig

	h.logger.Info("AI Config updated", zap.Any("config", h.config))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(systemConfig)
}

// SetSystem updates the system configuration
func (h *ConfigHandler) SetSystem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectsDir string `json:"projectsDir"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// TODO: Store in database
	h.logger.Info("System config updated", zap.String("projectsDir", req.ProjectsDir))

	systemConfig := map[string]string{
		"projectsDir": req.ProjectsDir,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"systemConfig": systemConfig,
	})
}
