package pi

// ToolSpec defines a tool's metadata
type ToolSpec struct {
	Name        string
	Description string
	Category    ToolCategory
	IsReadOnly  bool
}

// DefaultTools is the registry of all known Pi tools
var DefaultTools = []ToolSpec{
	{
		Name:       "read",
		Category:   CategoryRead,
		IsReadOnly: true,
	},
	{
		Name:       "write",
		Category:   CategoryWrite,
		IsReadOnly: false,
	},
	{
		Name:       "edit",
		Category:   CategoryWrite,
		IsReadOnly: false,
	},
	{
		Name:       "bash",
		Category:   CategoryExecute,
		IsReadOnly: false,
	},
	{
		Name:       "grep",
		Category:   CategoryRead,
		IsReadOnly: true,
	},
	{
		Name:       "find",
		Category:   CategoryRead,
		IsReadOnly: true,
	},
	{
		Name:       "ls",
		Category:   CategoryRead,
		IsReadOnly: true,
	},
	{
		Name:       "compact",
		Category:   CategoryExecute,
		IsReadOnly: false,
	},
	{
		Name:       "set_model",
		Category:   CategoryExecute,
		IsReadOnly: false,
	},
}

// SubAgentToolAllowlists defines which tools each sub-agent type can use
var SubAgentToolAllowlists = map[SubAgentType][]string{
	SubAgentExplore: {
		"read", "grep", "find", "ls", "bash",
	},
	SubAgentCode: {
		"read", "write", "edit", "bash", "grep", "find", "ls", "compact", "set_model",
	},
	SubAgentWeb: {
		"bash", "read", "write",
	},
	SubAgentComputerUse: {
		"bash", "read", "write",
	},
}

// GetToolSpec returns the spec for a tool by name
func GetToolSpec(name string) *ToolSpec {
	for _, spec := range DefaultTools {
		if spec.Name == name {
			return &spec
		}
	}
	return nil
}

// GetToolCategory returns the category for a tool name
func GetToolCategory(name string) ToolCategory {
	spec := GetToolSpec(name)
	if spec == nil {
		return CategoryExecute // default to execute for unknown tools
	}
	return spec.Category
}

// IsToolReadOnly returns true if the tool is read-only
func IsToolReadOnly(name string) bool {
	spec := GetToolSpec(name)
	if spec == nil {
		return false
	}
	return spec.IsReadOnly
}

// IsToolAllowed checks if a tool is allowed for the given sub-agent type
func IsToolAllowed(toolName string, subAgentType SubAgentType) bool {
	allowlist, ok := SubAgentToolAllowlists[subAgentType]
	if !ok {
		return true // no allowlist means all tools allowed
	}

	for _, allowed := range allowlist {
		if allowed == toolName {
			return true
		}
	}
	return false
}
