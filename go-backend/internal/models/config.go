package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

// ModelEntry is a provider + model ID pair.
type ModelEntry struct {
	Provider string `json:"provider"`
	ModelId  string `json:"modelId"`
}

// DefaultModelEntry is the hardcoded fallback when no config exists.
var DefaultModelEntry = ModelEntry{Provider: "llamacpp", ModelId: "gemma-4-26b"}

// settingsFile is the on-disk format for ~/.pi/agent/settings.json.
// Only the fields we care about are defined here.
type settingsFile struct {
	DefaultProvider string     `json:"defaultProvider"`
	DefaultModel    string     `json:"defaultModel"`
	MainModel       *ModelEntry `json:"mainModel,omitempty"`
	ToolModel       *ModelEntry `json:"toolModel,omitempty"`
}

// ModelConfig is a thread-safe, persisted configuration for the two-model system.
// Load it once at startup via LoadModelConfig, then use the getters/setters.
type ModelConfig struct {
	mu     sync.RWMutex
	main   ModelEntry
	tool   ModelEntry
	path   string
	logger *zap.Logger
}

// LoadModelConfig reads ~/.pi/agent/settings.json and returns a ModelConfig.
// If the file doesn't exist or fields are missing, defaults are used.
func LoadModelConfig(logger *zap.Logger) (*ModelConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		logger.Warn("Cannot determine HOME, using defaults", zap.Error(err))
		return newWithDefaults(logger), nil
	}

	path := filepath.Join(homeDir, ".pi", "agent", "settings.json")
	cfg := newWithDefaults(logger)
	cfg.path = path

	data, err := os.ReadFile(path)
	if err != nil {
		logger.Info("No settings.json found, using model defaults", zap.String("path", path))
		return cfg, nil
	}

	var sf settingsFile
	if err := json.Unmarshal(data, &sf); err != nil {
		logger.Warn("Failed to parse settings.json, using defaults", zap.Error(err))
		return cfg, nil
	}

	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	// Main model: explicit field > fallback to defaults
	if sf.MainModel != nil && sf.MainModel.Provider != "" && sf.MainModel.ModelId != "" {
		cfg.main = *sf.MainModel
	} else if sf.DefaultProvider != "" && sf.DefaultModel != "" {
		cfg.main = ModelEntry{Provider: sf.DefaultProvider, ModelId: sf.DefaultModel}
	}

	// Tool model: explicit field > fallback to main model
	if sf.ToolModel != nil && sf.ToolModel.Provider != "" && sf.ToolModel.ModelId != "" {
		cfg.tool = *sf.ToolModel
	} else {
		cfg.tool = cfg.main
	}

	logger.Info("Model config loaded",
		zap.String("mainProvider", cfg.main.Provider),
		zap.String("mainModel", cfg.main.ModelId),
		zap.String("toolProvider", cfg.tool.Provider),
		zap.String("toolModel", cfg.tool.ModelId),
	)

	return cfg, nil
}

func newWithDefaults(logger *zap.Logger) *ModelConfig {
	return &ModelConfig{
		main:   DefaultModelEntry,
		tool:   DefaultModelEntry,
		logger: logger,
	}
}

// MainModel returns the current main (conversation/reasoning) model.
func (c *ModelConfig) MainModel() ModelEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.main
}

// ToolModel returns the current tool (sub-agent/vision/cron) model.
func (c *ModelConfig) ToolModel() ModelEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tool
}

// ProviderForModel returns the provider name for a given model ID.
// Checks if it matches main or tool model, falls back to "llamacpp".
func (c *ModelConfig) ProviderForModel(modelId string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if modelId == c.main.ModelId {
		return c.main.Provider
	}
	if modelId == c.tool.ModelId {
		return c.tool.Provider
	}
	return "llamacpp"
}

// SetMainModel updates the main model and persists to disk.
func (c *ModelConfig) SetMainModel(provider, modelId string) error {
	c.mu.Lock()
	c.main = ModelEntry{Provider: provider, ModelId: modelId}
	c.mu.Unlock()
	return c.persist()
}

// SetToolModel updates the tool model and persists to disk.
func (c *ModelConfig) SetToolModel(provider, modelId string) error {
	c.mu.Lock()
	c.tool = ModelEntry{Provider: provider, ModelId: modelId}
	c.mu.Unlock()
	return c.persist()
}

// persist writes the current config to disk using atomic rename.
func (c *ModelConfig) persist() error {
	if c.path == "" {
		c.logger.Warn("No settings path, cannot persist model config")
		return nil
	}

	c.mu.RLock()
	main := c.main
	tool := c.tool
	c.mu.RUnlock()

	// Read existing file to preserve other fields
	existing := make(map[string]any)
	if data, err := os.ReadFile(c.path); err == nil {
		json.Unmarshal(data, &existing)
	}

	existing["mainModel"] = main
	existing["toolModel"] = tool

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: write to unique temp file, then rename
	tmpPath := c.path + fmt.Sprintf(".tmp.%d", os.Getpid())
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, c.path)
}
