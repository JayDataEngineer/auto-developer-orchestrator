package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

// ProjectSettings holds per-project overrides for agent behavior.
// Settings cascade: project overrides > global defaults.
type ProjectSettings struct {
	mu     sync.RWMutex
	path   string // path to the settings file on disk
	logger *zap.Logger

	// Model overrides (empty = use global default)
	MainModel *ModelEntry `json:"mainModel,omitempty"`
	ToolModel *ModelEntry `json:"toolModel,omitempty"`

	// Agent behavior overrides
	ThinkingLevel    string `json:"thinkingLevel,omitempty"`    // "low", "medium", "high"
	MaxTurns         int    `json:"maxTurns,omitempty"`         // 0 = unlimited
	AutoBranch       bool   `json:"autoBranch"`
	AutoMerge        bool   `json:"autoMerge"`
	SystemPromptAddon string `json:"systemPromptAddon,omitempty"` // appended to the default prompt

	// Tool permission overrides for this project
	ToolPermissionOverrides []ToolPermissionOverride `json:"toolPermissionOverrides,omitempty"`
}

// ToolPermissionOverride is a per-project tool permission override.
type ToolPermissionOverride struct {
	Tool   string `json:"tool"`
	Level  string `json:"level"` // "auto", "confirm", "deny"
	Reason string `json:"reason,omitempty"`
}

// DefaultProjectSettings returns a blank settings with no overrides.
func DefaultProjectSettings(logger *zap.Logger) *ProjectSettings {
	return &ProjectSettings{
		logger: logger,
	}
}

// LoadProjectSettings reads per-project settings from <projectPath>/.pi/settings.json.
// Returns default settings if no file exists.
func LoadProjectSettings(projectPath string, logger *zap.Logger) *ProjectSettings {
	settingsFile := filepath.Join(projectPath, ".pi", "settings.json")
	ps := &ProjectSettings{
		path:   settingsFile,
		logger: logger,
	}

	data, err := os.ReadFile(settingsFile)
	if err != nil {
		return ps // no file, use defaults
	}

	if err := json.Unmarshal(data, ps); err != nil {
		logger.Warn("Failed to parse project settings.json", zap.String("path", settingsFile), zap.Error(err))
		return ps
	}

	logger.Info("Loaded project settings",
		zap.String("project", filepath.Base(projectPath)),
		zap.Bool("hasMainModel", ps.MainModel != nil),
		zap.Bool("hasToolModel", ps.ToolModel != nil),
	)
	return ps
}

// ResolveMainModel returns the effective main model (project override > global).
func (ps *ProjectSettings) ResolveMainModel(global ModelEntry) ModelEntry {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.MainModel != nil && ps.MainModel.Provider != "" && ps.MainModel.ModelId != "" {
		return *ps.MainModel
	}
	return global
}

// ResolveToolModel returns the effective tool model (project override > global).
func (ps *ProjectSettings) ResolveToolModel(global ModelEntry) ModelEntry {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.ToolModel != nil && ps.ToolModel.Provider != "" && ps.ToolModel.ModelId != "" {
		return *ps.ToolModel
	}
	return global
}

// ResolveThinkingLevel returns the effective thinking level (project override > provided default).
func (ps *ProjectSettings) ResolveThinkingLevel(fallback string) string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if ps.ThinkingLevel != "" {
		return ps.ThinkingLevel
	}
	return fallback
}

// Save persists the current settings to disk.
func (ps *ProjectSettings) Save() error {
	if ps.path == "" {
		return nil
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	return ps.saveLocked()
}

// saveLocked writes settings to disk. Must be called with ps.mu held.
func (ps *ProjectSettings) saveLocked() error {
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(ps.path), 0755); err != nil {
		return err
	}

	return os.WriteFile(ps.path, append(data, '\n'), 0644)
}

// Update applies a partial update and persists.
func (ps *ProjectSettings) Update(updates map[string]interface{}) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if v, ok := updates["mainModel"]; ok {
		if v == nil {
			ps.MainModel = nil
		} else {
			var entry ModelEntry
			if raw, err := json.Marshal(v); err == nil {
				json.Unmarshal(raw, &entry)
				if entry.Provider != "" && entry.ModelId != "" {
					ps.MainModel = &entry
				}
			}
		}
	}
	if v, ok := updates["toolModel"]; ok {
		if v == nil {
			ps.ToolModel = nil
		} else {
			var entry ModelEntry
			if raw, err := json.Marshal(v); err == nil {
				json.Unmarshal(raw, &entry)
				if entry.Provider != "" && entry.ModelId != "" {
					ps.ToolModel = &entry
				}
			}
		}
	}
	if v, ok := updates["thinkingLevel"].(string); ok {
		ps.ThinkingLevel = v
	}
	if v, ok := updates["maxTurns"].(float64); ok {
		ps.MaxTurns = int(v)
	}
	if v, ok := updates["autoBranch"].(bool); ok {
		ps.AutoBranch = v
	}
	if v, ok := updates["autoMerge"].(bool); ok {
		ps.AutoMerge = v
	}
	if v, ok := updates["systemPromptAddon"].(string); ok {
		ps.SystemPromptAddon = v
	}

	return ps.saveLocked()
}
