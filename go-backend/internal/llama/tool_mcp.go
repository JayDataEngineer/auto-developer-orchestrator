package llama

import (
	"encoding/json"
	"fmt"
	"strings"
)

// mcpToolNames stores the dynamically-registered MCP tool names.
// Populated by RegisterMCPTools at startup when the MCP server is available.
var mcpToolNames []string

// MCPToolRegistration holds the data needed to register an MCP tool as a first-class tool.
type MCPToolRegistration struct {
	MCPName       string // original MCP tool name (e.g., "research")
	Description   string // from MCP tools/list
	InputSchema   string // full JSON Schema from MCP tools/list
	SchemaExample string // generated example JSON for system prompt
}

// RegisterMCPTools registers MCP-sourced tools into the global tool registry.
// Each tool is prefixed with "mcp_" and given the full parameter schema from the MCP server.
// Returns the list of registered tool names.
func RegisterMCPTools(tools []MCPToolRegistration) []string {
	var names []string
	for _, t := range tools {
		spec := ToolSpec{
			Name:             "mcp_" + t.MCPName,
			Category:         CategoryBrowser,
			Description:      t.Description,
			Schema:           t.SchemaExample,
			ParametersSchema: t.InputSchema,
			Returns:          "Returns the result from the MCP research server.",
		}

		allTools = append(allTools, spec)
		// Point index at the newly appended entry
		toolIndex[spec.Name] = &allTools[len(allTools)-1]
		names = append(names, spec.Name)
	}

	mcpToolNames = names
	return names
}

// MCPToolNames returns the list of dynamically registered MCP tool names.
func MCPToolNames() []string {
	return mcpToolNames
}

// MCPToolReference generates a reference section describing MCP tools and their parameters.
// This goes in the orchestrator's system prompt so it can write accurate delegate_to instructions.
// These tools are NOT in the orchestrator's callable tool list — only sub-agents get them.
func MCPToolReference() string {
	if len(mcpToolNames) == 0 {
		return ""
	}
	var b strings.Builder
	for _, name := range mcpToolNames {
		spec := LookupTool(name)
		if spec == nil {
			continue
		}
		// Extract key parameter names from the schema for quick reference
		params := extractParamSummary(spec.ParametersSchema)
		mcpName := strings.TrimPrefix(name, "mcp_")
		fmt.Fprintf(&b, "mcp_%s(%s) — %s\n", mcpName, params, spec.Description)
	}
	return b.String()
}

// extractParamSummary pulls required + notable optional params from a JSON Schema string.
func extractParamSummary(schema string) string {
	var s struct {
		Properties map[string]struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Enum        []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(schema), &s); err != nil {
		return "..."
	}

	// Show required params, then notable optional ones
	var parts []string
	for _, k := range s.Required {
		parts = append(parts, k)
	}
	// Add a few notable optional params
	for k, v := range s.Properties {
		if !containsStr(parts, k) && (strings.Contains(v.Description, "max") ||
			strings.Contains(v.Description, "pattern") ||
			strings.Contains(v.Description, "strategy") ||
			strings.Contains(v.Description, "depth") ||
			strings.Contains(v.Description, "limit")) {
			parts = append(parts, k+"?")
		}
	}

	if len(parts) == 0 {
		return "..."
	}
	return strings.Join(parts, ", ")
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// SchemaToExample generates a minimal JSON example string from a JSON Schema.
// Used as the Schema field in ToolSpec for system prompt formatting.
func SchemaToExample(schema string) string {
	if schema == "" {
		return "{}"
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal([]byte(schema), &s); err != nil {
		return "{}"
	}

	example := make(map[string]interface{})
	for _, k := range s.Required {
		example[k] = schemaValueExample(s.Properties[k])
	}

	b, _ := json.Marshal(example)
	return string(b)
}

func schemaValueExample(prop json.RawMessage) interface{} {
	var p struct {
		Type    string   `json:"type"`
		Enum    []string `json:"enum"`
		Default any      `json:"default"`
	}
	json.Unmarshal(prop, &p)
	if len(p.Enum) > 0 {
		return p.Enum[0]
	}
	if p.Default != nil {
		return p.Default
	}
	switch p.Type {
	case "string":
		return "..."
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return false
	case "array":
		return []string{}
	case "object":
		return map[string]interface{}{}
	default:
		return "..."
	}
}
