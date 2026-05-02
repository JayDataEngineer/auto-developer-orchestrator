package llama

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/manifest"
	"go.uber.org/zap"
)

// AppToolRegistration stores the info needed to dispatch an app tool call.
type AppToolRegistration struct {
	ProjectName string // project that owns this tool
	ToolName    string // original name from manifest (e.g., "deep_research")
	Handler     string // template (e.g., "python -m dre research {query}")
	Description string
}

// appToolNames stores dynamically-registered app tool names.
var appToolNames []string

// appToolRegistry maps "app_<name>" → registration for dispatch.
var appToolRegistry = map[string]*AppToolRegistration{}

// RegisterAppTools registers app-sourced tools into the global tool registry.
// Each tool is prefixed with "app_" and given a schema derived from the handler template.
func RegisterAppTools(tools []AppToolRegistration) []string {
	var names []string
	for _, t := range tools {
		registeredName := "app_" + t.ToolName

		// Generate schema from {param} tokens in handler
		params := parseHandlerParams(t.Handler)
		schema := buildAppToolSchema(params)
		example := buildAppToolExample(params)

		spec := ToolSpec{
			Name:             registeredName,
			Category:         CategoryExecution,
			Description:      fmt.Sprintf("[App: %s] %s", t.ProjectName, t.Description),
			Schema:           example,
			ParametersSchema: schema,
			Returns:          "Returns the stdout output from the command.",
		}

		allTools = append(allTools, spec)
		toolIndex[spec.Name] = &allTools[len(allTools)-1]
		appToolRegistry[registeredName] = &AppToolRegistration{
			ProjectName: t.ProjectName,
			ToolName:    t.ToolName,
			Handler:     t.Handler,
			Description: t.Description,
		}
		names = append(names, registeredName)
	}

	appToolNames = append(appToolNames, names...)
	return names
}

// UnregisterAppTools removes all app tools for a given project from the registry.
func UnregisterAppTools(projectName string) {
	// Remove from appToolRegistry and collect names to drop
	var toRemove []string
	for name, reg := range appToolRegistry {
		if reg.ProjectName == projectName {
			toRemove = append(toRemove, name)
			delete(appToolRegistry, name)
		}
	}

	// Filter allTools
	filtered := allTools[:0]
	for _, t := range allTools {
		keep := true
		for _, rm := range toRemove {
			if t.Name == rm {
				keep = false
				delete(toolIndex, rm)
				break
			}
		}
		if keep {
			filtered = append(filtered, t)
		}
	}
	allTools = filtered

	// Filter appToolNames
	newNames := appToolNames[:0]
	for _, n := range appToolNames {
		found := false
		for _, rm := range toRemove {
			if n == rm {
				found = true
				break
			}
		}
		if !found {
			newNames = append(newNames, n)
		}
	}
	appToolNames = newNames
}

// AppToolNames returns the list of dynamically registered app tool names.
func AppToolNames() []string {
	return appToolNames
}

// LookupAppTool returns the registration for an app tool, or nil.
func LookupAppTool(name string) *AppToolRegistration {
	return appToolRegistry[name]
}

// AppToolReference generates a reference section describing app tools.
// Goes in the orchestrator's system prompt so it can call them directly.
func AppToolReference() string {
	if len(appToolNames) == 0 {
		return ""
	}

	// Sort for deterministic output
	sorted := make([]string, len(appToolNames))
	copy(sorted, appToolNames)
	sort.Strings(sorted)

	var b strings.Builder
	for _, name := range sorted {
		reg := appToolRegistry[name]
		if reg == nil {
			continue
		}
		params := parseHandlerParams(reg.Handler)
		paramStr := strings.Join(params, ", ")
		fmt.Fprintf(&b, "%s(%s) — %s\n  Runs: %s\n", name, paramStr, reg.Description, reg.Handler)
	}
	return b.String()
}

// resolveHandlerTemplate substitutes {param} tokens with values from args.
func resolveHandlerTemplate(handler string, args map[string]interface{}) string {
	params := parseHandlerParams(handler)
	result := handler
	for _, p := range params {
		val := ""
		if v, ok := args[p]; ok {
			val = fmt.Sprintf("%v", v)
		}
		result = strings.ReplaceAll(result, "{"+p+"}", val)
	}
	return result
}

// paramRe matches {param_name} tokens in handler templates.
var paramRe = regexp.MustCompile(`\{(\w+)\}`)

// parseHandlerParams extracts {param} token names from a handler template.
func parseHandlerParams(handler string) []string {
	matches := paramRe.FindAllStringSubmatch(handler, -1)
	seen := map[string]bool{}
	var params []string
	for _, m := range matches {
		name := m[1]
		if !seen[name] {
			seen[name] = true
			params = append(params, name)
		}
	}
	return params
}

// buildAppToolSchema generates a JSON Schema string from handler parameters.
func buildAppToolSchema(params []string) string {
	if len(params) == 0 {
		return `{"type":"object","properties":{}}`
	}

	props := make(map[string]interface{})
	required := make([]string, len(params))
	for i, p := range params {
		props[p] = map[string]string{"type": "string"}
		required[i] = p
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}

	b, _ := json.Marshal(schema)
	return string(b)
}

// buildAppToolExample generates a JSON example string from handler parameters.
func buildAppToolExample(params []string) string {
	if len(params) == 0 {
		return "{}"
	}

	example := make(map[string]string)
	for _, p := range params {
		example[p] = "..."
	}

	b, _ := json.Marshal(example)
	return string(b)
}

// AppToolRegisterer implements the handlers.ToolRegisterer interface.
type AppToolRegisterer struct {
	logger *zap.Logger
}

// NewAppToolRegisterer creates a new AppToolRegisterer.
func NewAppToolRegisterer(logger *zap.Logger) *AppToolRegisterer {
	return &AppToolRegisterer{logger: logger}
}

// RegisterFromManifest converts manifest ToolDefs into AppToolRegistrations and registers them.
func (r *AppToolRegisterer) RegisterFromManifest(projectName, projectDir string, tools []manifest.ToolDef) []string {
	var regs []AppToolRegistration
	for _, t := range tools {
		regs = append(regs, AppToolRegistration{
			ProjectName: projectName,
			ToolName:    t.Name,
			Handler:     t.Handler,
			Description: t.Description,
		})
	}
	names := RegisterAppTools(regs)
	r.logger.Info("Registered app tools from manifest",
		zap.String("project", projectName),
		zap.Strings("tools", names),
	)
	return names
}

// UnregisterFromManifest removes all app tools for a project.
func (r *AppToolRegisterer) UnregisterFromManifest(projectName string) {
	UnregisterAppTools(projectName)
	r.logger.Info("Unregistered app tools for project", zap.String("project", projectName))
}
