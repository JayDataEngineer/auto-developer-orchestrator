package pi

import (
	"sync"

	"go.uber.org/zap"
)

// PermissionLevel defines what happens when a tool is invoked.
type PermissionLevel string

const (
	PermAutoApprove      PermissionLevel = "auto"
	PermRequireApproval  PermissionLevel = "confirm"
	PermDeny             PermissionLevel = "deny"
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
	mu     sync.RWMutex
	perms  map[string]ToolPermission
	logger *zap.Logger
}

// NewToolPermissionConfig creates a config with default permissions.
func NewToolPermissionConfig(logger *zap.Logger) *ToolPermissionConfig {
	return &ToolPermissionConfig{
		perms: map[string]ToolPermission{
			"bash":          {Tool: "bash", Level: PermAutoApprove, RiskLevel: "medium"},
			"write":         {Tool: "write", Level: PermAutoApprove, RiskLevel: "low"},
			"edit":          {Tool: "edit", Level: PermAutoApprove, RiskLevel: "low"},
			"delete":        {Tool: "delete", Level: PermRequireApproval, RiskLevel: "high"},
			"git_push":      {Tool: "git_push", Level: PermRequireApproval, RiskLevel: "high"},
			"git_reset":     {Tool: "git_reset", Level: PermRequireApproval, RiskLevel: "high"},
			"web_fetch":     {Tool: "web_fetch", Level: PermAutoApprove, RiskLevel: "low"},
			"computer_use":  {Tool: "computer_use", Level: PermAutoApprove, RiskLevel: "medium"},
		},
		logger: logger,
	}
}

// SetPermission updates a tool's permission level.
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
	defer c.mu.Unlock()

	existing, ok := c.perms[toolName]
	if !ok {
		existing = ToolPermission{Tool: toolName, RiskLevel: "low"}
	}
	existing.Level = level
	existing.Reason = reason
	c.perms[toolName] = existing
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
