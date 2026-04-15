package pi

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
	// PermAutoApprove means the tool runs without asking the user.
	PermAutoApprove PermissionLevel = "auto"
	// PermRequireApproval means the tool pauses for user confirmation.
	PermRequireApproval PermissionLevel = "confirm"
	// PermDeny means the tool is blocked entirely.
	PermDeny PermissionLevel = "deny"
)

// ToolPermission defines the permission level for a specific tool.
type ToolPermission struct {
	Tool      string          `json:"tool"`
	Level     PermissionLevel `json:"level"`
	Reason    string          `json:"reason,omitempty"`
	RiskLevel string          `json:"risk,omitempty"` // "low", "medium", "high"
}

// ToolPermissionConfig manages per-tool permission settings.
// Settings cascade: per-project overrides > global defaults > built-in risk rules.
type ToolPermissionConfig struct {
	mu      sync.RWMutex
	perms   map[string]ToolPermission // keyed by tool name
	logger  *zap.Logger
	project string // project path this config is scoped to (empty = global)
}

// DefaultToolPermissions returns the built-in permission rules.
// Tools not listed here default to auto-approve.
func DefaultToolPermissions() map[string]ToolPermission {
	return map[string]ToolPermission{
		"bash": {
			Tool:      "bash",
			Level:     PermAutoApprove, // individual commands are checked via IsRiskyBashCommand
			RiskLevel: "medium",
		},
		"write": {
			Tool:      "write",
			Level:     PermAutoApprove,
			RiskLevel: "low",
		},
		"edit": {
			Tool:      "edit",
			Level:     PermAutoApprove,
			RiskLevel: "low",
		},
		"delete": {
			Tool:      "delete",
			Level:     PermRequireApproval,
			RiskLevel: "high",
		},
		"git_push": {
			Tool:      "git_push",
			Level:     PermRequireApproval,
			RiskLevel: "high",
		},
		"git_reset": {
			Tool:      "git_reset",
			Level:     PermRequireApproval,
			RiskLevel: "high",
		},
		"web_fetch": {
			Tool:      "web_fetch",
			Level:     PermAutoApprove,
			RiskLevel: "low",
		},
		"computer_use": {
			Tool:      "computer_use",
			Level:     PermAutoApprove,
			RiskLevel: "medium",
		},
	}
}

// NewToolPermissionConfig creates a config with default permissions.
func NewToolPermissionConfig(logger *zap.Logger) *ToolPermissionConfig {
	cfg := &ToolPermissionConfig{
		perms:  DefaultToolPermissions(),
		logger: logger,
	}
	return cfg
}

// LoadProjectOverrides loads per-project permission overrides from
// <project>/.pi/tool-permissions.json if it exists.
func (c *ToolPermissionConfig) LoadProjectOverrides(projectPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.project = projectPath
	permFile := filepath.Join(projectPath, ".pi", "tool-permissions.json")

	data, err := os.ReadFile(permFile)
	if err != nil {
		return // no overrides file, use defaults
	}

	var overrides []ToolPermission
	if err := json.Unmarshal(data, &overrides); err != nil {
		c.logger.Warn("Failed to parse tool-permissions.json", zap.Error(err))
		return
	}

	for _, o := range overrides {
		c.perms[o.Tool] = o
	}

	c.logger.Info("Loaded project tool permission overrides",
		zap.String("project", filepath.Base(projectPath)),
		zap.Int("overrides", len(overrides)),
	)
}

// GetPermission returns the permission level for a tool.
func (c *ToolPermissionConfig) GetPermission(toolName string) ToolPermission {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if p, ok := c.perms[toolName]; ok {
		return p
	}
	return ToolPermission{Tool: toolName, Level: PermAutoApprove, RiskLevel: "low"}
}

// SetPermission updates a tool's permission level.
// Invalid levels are silently ignored.
func (c *ToolPermissionConfig) SetPermission(toolName string, level PermissionLevel, reason string) {
	switch level {
	case PermAutoApprove, PermRequireApproval, PermDeny:
		// valid
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

// ShouldApprove returns true if the tool invocation needs user approval.
// This replaces the old IsRiskyBashCommand check — it handles all tools.
func (c *ToolPermissionConfig) ShouldApprove(toolName string, args map[string]interface{}) (bool, string, string) {
	perm := c.GetPermission(toolName)

	switch perm.Level {
	case PermDeny:
		return false, perm.RiskLevel, "Tool '" + toolName + "' is denied: " + perm.Reason
	case PermRequireApproval:
		return true, perm.RiskLevel, perm.Reason
	case PermAutoApprove:
		// For bash, still check individual command risk
		if toolName == "bash" {
			if cmd, ok := args["command"].(string); ok {
				if risky, reason := IsRiskyBashCommand(cmd); risky {
					return true, "high", reason
				}
			}
		}
		return false, "", ""
	default:
		return false, "", ""
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
