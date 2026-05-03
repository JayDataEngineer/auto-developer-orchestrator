package llama

// ── Extension API ──────────────────────────────────────────────────────────
// Implements the pi-mono extension pattern: extensions register tools, commands,
// event hooks, and resource paths. Designed to work across CLI, TUI, and Web UI
// via the shared Go backend (DRY constraint).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExtensionTool is a tool registered by an extension.
type ExtensionTool struct {
	Name        string                 // unique tool name (prefixed with ext_)
	Label       string                 // human-readable label
	Description string                 // LLM description
	Schema      map[string]interface{} // JSON Schema for parameters
	Handler     func(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// Extension represents a loaded extension with its registrations.
type Extension struct {
	Name     string
	Path     string
	Tools    []ExtensionTool
	Enabled  bool
}

// ExtensionRegistry manages loaded extensions.
type ExtensionRegistry struct {
	extensions []*Extension
	toolIndex  map[string]*ExtensionTool  // tool name → tool
}

// NewExtensionRegistry creates an empty registry.
func NewExtensionRegistry() *ExtensionRegistry {
	return &ExtensionRegistry{
		toolIndex: make(map[string]*ExtensionTool),
	}
}

// LoadFromDir discovers extensions in a directory.
// Each extension is a subdirectory containing extension.json:
//
//	{
//	  "name": "hello-world",
//	  "description": "Example extension",
//	  "tools": [
//	    {
//	      "name": "greet",
//	      "description": "Greet a person",
//	      "parameters": { "type": "object", "properties": { "name": { "type": "string" } }, "required": ["name"] },
//	      "command": "echo Hello, $NAME!"  // simple shell-based tools
//	    }
//	  ]
//	}
func (r *ExtensionRegistry) LoadFromDir(dir string) (int, error) {
	if dir == "" {
		return 0, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return 0, nil // dir doesn't exist — not an error
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		configPath := filepath.Join(abs, entry.Name(), "extension.json")
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue // skip directories without extension.json
		}

		var cfg struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Enabled     bool   `json:"enabled"`
			Tools       []struct {
				Name        string                 `json:"name"`
				Description string                 `json:"description"`
				Parameters  map[string]interface{} `json:"parameters"`
				Command     string                 `json:"command"`
			} `json:"tools"`
		}
		cfg.Enabled = true // default enabled

		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}

		ext := &Extension{
			Name:    cfg.Name,
			Path:    filepath.Join(abs, entry.Name()),
			Enabled: cfg.Enabled,
		}

		// Register tools (shell-based: expand $PARAM_NAME in command)
		for _, t := range cfg.Tools {
			toolName := "ext_" + cfg.Name + "_" + t.Name
			cmdTemplate := t.Command
			paramsSchema := t.Parameters
			if paramsSchema == nil {
				paramsSchema = map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				}
			}

			tool := &ExtensionTool{
				Name:        toolName,
				Label:       t.Name,
				Description: t.Description,
				Schema:      paramsSchema,
				Handler:     r.makeShellHandler(cmdTemplate),
			}
			ext.Tools = append(ext.Tools, *tool)
			r.toolIndex[toolName] = tool

			// Register as a tool in the global registry
			RegisterExtensionTool(tool)
		}

		r.extensions = append(r.extensions, ext)
		count++
	}

	return count, nil
}

// makeShellHandler creates a handler that expands $VAR references and executes the command.
func (r *ExtensionRegistry) makeShellHandler(template string) func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		cmd := template
		for k, v := range args {
			cmd = strings.ReplaceAll(cmd, "$"+strings.ToUpper(k), fmt.Sprint(v))
			cmd = strings.ReplaceAll(cmd, "${"+k+"}", fmt.Sprint(v))
		}
		return map[string]interface{}{"command": cmd}, nil
	}
}

// GetTool returns a registered extension tool by name.
func (r *ExtensionRegistry) GetTool(name string) *ExtensionTool {
	return r.toolIndex[name]
}

// Extensions returns all loaded extensions.
func (r *ExtensionRegistry) Extensions() []*Extension {
	return r.extensions
}

// Count returns the number of loaded extensions.
func (r *ExtensionRegistry) Count() int {
	return len(r.extensions)
}

// RegisterExtensionTool adds an extension tool to the global tool registry.
func RegisterExtensionTool(tool *ExtensionTool) {
	schemaJSON, _ := json.Marshal(tool.Schema)
	spec := ToolSpec{
		Name:             tool.Name,
		Category:         CategoryMeta, // extension tools are meta-category
		Description:      tool.Description,
		Schema:           string(schemaJSON),
		ParametersSchema: string(schemaJSON),
		Returns:          "Executes the extension tool and returns its result.",
	}
	allTools = append(allTools, spec)
	toolIndex[tool.Name] = &spec
}
