package perms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"go.uber.org/zap"
)

// PermissionLevel defines what happens when a tool is invoked.
type PermissionLevel string

const (
	PermAutoApprove     PermissionLevel = "auto"
	PermRequireApproval PermissionLevel = "confirm"
	PermDeny            PermissionLevel = "deny"
)

// ToolPermission defines the permission level for a specific tool.
type ToolPermission struct {
	Tool      string          `json:"tool"`
	Level     PermissionLevel `json:"level"`
	Reason    string          `json:"reason,omitempty"`
	RiskLevel string          `json:"risk,omitempty"`
}

// ToolPermissionConfig manages per-tool permission settings.
type ToolPermissionConfig struct {
	mu       sync.RWMutex
	perms    map[string]ToolPermission
	filePath string // path to persistence file
	logger   *zap.Logger
}

// NewToolPermissionConfig creates a config with default permissions.
func NewToolPermissionConfig(logger *zap.Logger) *ToolPermissionConfig {
	return &ToolPermissionConfig{
		perms: map[string]ToolPermission{
			"bash":           {Tool: "bash", Level: PermAutoApprove, RiskLevel: "medium"},
			"file_read":      {Tool: "file_read", Level: PermAutoApprove, RiskLevel: "low"},
			"file_write":     {Tool: "file_write", Level: PermAutoApprove, RiskLevel: "medium"},
			"file_edit":      {Tool: "file_edit", Level: PermAutoApprove, RiskLevel: "medium"},
			"file_grep":      {Tool: "file_grep", Level: PermAutoApprove, RiskLevel: "low"},
			"file_glob":      {Tool: "file_glob", Level: PermAutoApprove, RiskLevel: "low"},
			"delegate_to":    {Tool: "delegate_to", Level: PermAutoApprove, RiskLevel: "medium"},
			"delegate_async": {Tool: "delegate_async", Level: PermAutoApprove, RiskLevel: "medium"},
			"memory":         {Tool: "memory", Level: PermAutoApprove, RiskLevel: "low"},
			"create_plan":    {Tool: "create_plan", Level: PermAutoApprove, RiskLevel: "low"},
		},
		logger: logger,
	}
}

// SetPermission updates a tool's permission level. Auto-saves if persistence is configured.
func (c *ToolPermissionConfig) SetPermission(toolName string, level PermissionLevel, reason string) {
	switch level {
	case PermAutoApprove, PermRequireApproval, PermDeny:
	default:
		c.logger.Warn("Invalid permission level, ignoring",
			zap.String("tool", toolName),
			zap.String("level", string(level)),
		)
		return
	}

	c.mu.Lock()
	existing, ok := c.perms[toolName]
	if !ok {
		existing = ToolPermission{Tool: toolName, RiskLevel: "low"}
	}
	existing.Level = level
	existing.Reason = reason
	c.perms[toolName] = existing
	savePath := c.filePath
	c.mu.Unlock()

	// Auto-save if persistence path is set
	if savePath != "" {
		_ = c.Save(savePath)
	}
}

// AllPermissions returns a snapshot of all configured permissions.
func (c *ToolPermissionConfig) AllPermissions() map[string]ToolPermission {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]ToolPermission, len(c.perms))
	for k, v := range c.perms {
		result[k] = v
	}
	return result
}

// SetFilePath enables auto-save to the given path on every SetPermission call.
func (c *ToolPermissionConfig) SetFilePath(path string) {
	c.mu.Lock()
	c.filePath = path
	c.mu.Unlock()
}

// Load reads permission overrides from a JSON file and merges them into the defaults.
// Unknown/invalid levels are skipped. Missing file is not an error.
func (c *ToolPermissionConfig) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var overrides map[string]ToolPermission
	if err := json.Unmarshal(data, &overrides); err != nil {
		return err
	}

	c.mu.Lock()
	for name, p := range overrides {
		switch p.Level {
		case PermAutoApprove, PermRequireApproval, PermDeny:
			p.Tool = name
			c.perms[name] = p
		default:
			c.logger.Warn("Skipping invalid permission level in config",
				zap.String("tool", name),
				zap.String("level", string(p.Level)),
			)
		}
	}
	c.filePath = path
	c.mu.Unlock()

	c.logger.Debug("Loaded tool permissions", zap.String("path", path), zap.Int("count", len(overrides)))
	return nil
}

// Save writes the current permissions to a JSON file.
func (c *ToolPermissionConfig) Save(path string) error {
	c.mu.RLock()
	data, err := json.MarshalIndent(c.perms, "", "  ")
	c.mu.RUnlock()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// ModelConfigProvider resolves the provider for a given model ID.
var ModelConfigProvider func(modelId string) string = func(_ string) string { return "llamacpp" }
