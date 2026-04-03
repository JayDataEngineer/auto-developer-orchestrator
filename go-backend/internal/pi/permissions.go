package pi

import (
	"path/filepath"
	"strings"
)

// PermissionMode defines the access level for tool execution.
// Modeled after Claude Code's ReadOnly/WorkspaceWrite/DangerFullAccess.
type PermissionMode string

const (
	// PermReadOnly — can only read files and run read-only commands.
	PermReadOnly PermissionMode = "readonly"

	// PermWorkspaceWrite — can read/write within the project directory.
	PermWorkspaceWrite PermissionMode = "workspace_write"

	// PermDangerFullAccess — unrestricted (used cautiously for computer_use).
	PermDangerFullAccess PermissionMode = "danger_full_access"
)

// ToolCategory classifies a tool or command by its impact level.
type ToolCategory string

const (
	CategoryRead        ToolCategory = "read"        // file reading, grep, glob, browsing
	CategoryWrite       ToolCategory = "write"       // file creation/modification
	CategoryExecute     ToolCategory = "execute"     // bash commands
	CategoryDestructive ToolCategory = "destructive" // rm -rf, git reset --hard, force push
	CategoryBrowser     ToolCategory = "browser"     // web navigation/interaction
	CategorySandbox     ToolCategory = "sandbox"     // sandbox management
)

// PermissionModeAllows returns true if the given mode permits the tool category.
func PermissionModeAllows(mode PermissionMode, category ToolCategory) bool {
	switch mode {
	case PermReadOnly:
		return category == CategoryRead
	case PermWorkspaceWrite:
		return category == CategoryRead || category == CategoryWrite || category == CategoryExecute
	case PermDangerFullAccess:
		return true
	default:
		return false
	}
}

// Sub-agent default permission modes per type.
var defaultSubAgentPermissions = map[SubAgentType]PermissionMode{
	SubAgentCode:        PermWorkspaceWrite,
	SubAgentExplore:     PermReadOnly,
	SubAgentWeb:         PermReadOnly, // can browse but not modify files
	SubAgentComputerUse: PermDangerFullAccess,
}

// DefaultPermissionForSubAgent returns the permission mode for a sub-agent type.
func DefaultPermissionForSubAgent(t SubAgentType) PermissionMode {
	if m, ok := defaultSubAgentPermissions[t]; ok {
		return m
	}
	return PermReadOnly
}

// PermissionContext holds the runtime permission state for an agent.
type PermissionContext struct {
	Mode        PermissionMode
	ProjectDir  string
	DenyNames   []string // exact tool names to deny
	DenyPrefixes []string // tool name prefixes to deny
}

// NewPermissionContext creates a permission context for the given mode and project.
func NewPermissionContext(mode PermissionMode, projectDir string) *PermissionContext {
	return &PermissionContext{
		Mode:       mode,
		ProjectDir: projectDir,
	}
}

// Allows checks if a tool/action with the given category and name is permitted.
func (pc *PermissionContext) Allows(category ToolCategory, toolName string) bool {
	// Check deny list first (deny takes precedence)
	if pc.isDenied(toolName) {
		return false
	}
	return PermissionModeAllows(pc.Mode, category)
}

// isDenied checks the deny lists for the given tool name.
func (pc *PermissionContext) isDenied(toolName string) bool {
	lower := strings.ToLower(toolName)
	for _, name := range pc.DenyNames {
		if strings.ToLower(name) == lower {
			return true
		}
	}
	for _, prefix := range pc.DenyPrefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// IsPathAllowed checks if a file path is within the project directory.
func (pc *PermissionContext) IsPathAllowed(path string) bool {
	if pc.ProjectDir == "" {
		return true // no restriction if no project dir
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	projectAbs, err := filepath.Abs(pc.ProjectDir)
	if err != nil {
		return false
	}
	// Allow paths within the project directory
	rel, err := filepath.Rel(projectAbs, abs)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

// RequiresConfirmation returns true if the action should prompt the user before proceeding.
func (pc *PermissionContext) RequiresConfirmation(category ToolCategory) bool {
	switch pc.Mode {
	case PermReadOnly:
		// No confirmation needed — can't do anything dangerous
		return false
	case PermWorkspaceWrite:
		// Confirm destructive operations
		return category == CategoryDestructive
	case PermDangerFullAccess:
		// Confirm destructive operations even in full access mode
		return category == CategoryDestructive
	default:
		return true
	}
}
