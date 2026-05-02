package llama

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const customToolsDir = "/sandbox/persist/tools"
const manifestFile = "manifest.json"

// CustomToolManifest is the persisted tool registry.
type CustomToolManifest struct {
	Tools []CustomToolEntry `json:"tools"`
}

// CustomToolEntry describes a single custom tool.
type CustomToolEntry struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Language    string            `json:"language"`
	ArgsSchema  map[string]string `json:"args_schema"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	LastUsedAt  string            `json:"last_used_at,omitempty"`
	SuccessCount int              `json:"success_count"`
	FailCount    int              `json:"fail_count"`
}

// toolNameRe validates custom tool names.
var toolNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// readManifest reads the tool manifest from the sandbox.
func (e *SandboxToolExecutor) readManifest(ctx context.Context, sandboxID string) (*CustomToolManifest, error) {
	output, err := e.Manager.ExecInSandbox(ctx, sandboxID, []string{"cat", filepath.Join(customToolsDir, manifestFile)})
	if err != nil {
		// No manifest yet — return empty
		return &CustomToolManifest{}, nil
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return &CustomToolManifest{}, nil
	}
	var manifest CustomToolManifest
	if err := json.Unmarshal([]byte(output), &manifest); err != nil {
		return nil, fmt.Errorf("corrupt manifest.json: %w", err)
	}
	return &manifest, nil
}

// writeManifest writes the tool manifest to the sandbox.
func (e *SandboxToolExecutor) writeManifest(ctx context.Context, sandboxID string, manifest *CustomToolManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	// Ensure directory exists
	e.Manager.ExecInSandbox(ctx, sandboxID, []string{"mkdir", "-p", customToolsDir})
	// Write manifest
	writeCmd := fmt.Sprintf("cat > %s << 'MANIFEST_EOF'\n%s\nMANIFEST_EOF",
		filepath.Join(customToolsDir, manifestFile), string(data))
	_, err = e.Manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", writeCmd})
	return err
}

// executeCreateTool creates a new custom tool.
func (e *SandboxToolExecutor) executeCreateTool(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	script, _ := args["script"].(string)
	language, _ := args["language"].(string)
	argsSchemaStr, _ := args["args_schema"].(string)

	if name == "" || description == "" || script == "" {
		return nil, fmt.Errorf("missing required fields: name, description, script")
	}
	if !toolNameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid tool name %q: must be lowercase, start with letter, only underscores/letters/digits, 2-64 chars", name)
	}
	if language == "" {
		language = "python"
	}

	// Parse args schema
	argsSchema := make(map[string]string)
	if argsSchemaStr != "" {
		for _, part := range strings.Split(argsSchemaStr, ",") {
			kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
			if len(kv) == 2 {
				argsSchema[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			} else {
				argsSchema[strings.TrimSpace(kv[0])] = "string"
			}
		}
	}

	// Ensure tools directory
	e.Manager.ExecInSandbox(ctx, sandboxID, []string{"mkdir", "-p", customToolsDir})

	// Determine script filename
	var filename string
	switch language {
	case "python":
		filename = name + ".py"
	case "bash":
		filename = name + ".sh"
	case "node":
		filename = name + ".js"
	default:
		filename = name + ".py"
		language = "python"
	}

	// Write the script
	scriptPath := filepath.Join(customToolsDir, filename)
	writeCmd := fmt.Sprintf("cat > %s << 'TOOL_SCRIPT_EOF'\n%s\nTOOL_SCRIPT_EOF", scriptPath, script)
	if _, err := e.Manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", writeCmd}); err != nil {
		return nil, fmt.Errorf("failed to write script: %w", err)
	}

	// Make executable
	e.Manager.ExecInSandbox(ctx, sandboxID, []string{"chmod", "+x", scriptPath})

	// Update manifest
	manifest, err := e.readManifest(ctx, sandboxID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	entry := CustomToolEntry{
		Name:        name,
		Description: description,
		Language:    language,
		ArgsSchema:  argsSchema,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Update existing or append
	found := false
	for i, t := range manifest.Tools {
		if t.Name == name {
			entry.CreatedAt = t.CreatedAt // preserve original creation time
			entry.SuccessCount = t.SuccessCount
			entry.FailCount = t.FailCount
			manifest.Tools[i] = entry
			found = true
			break
		}
	}
	if !found {
		manifest.Tools = append(manifest.Tools, entry)
	}

	if err := e.writeManifest(ctx, sandboxID, manifest); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	verb := "created"
	if found {
		verb = "updated"
	}
	return map[string]interface{}{
		"status":   verb,
		"name":     name,
		"script":   scriptPath,
		"language": language,
		"message":  fmt.Sprintf("Tool %q %s. Use run_tool({\"name\": %q, \"args\": {...}}) to execute it.", name, verb, name),
	}, nil
}

// executeListTools lists all custom tools.
func (e *SandboxToolExecutor) executeListTools(ctx context.Context, sandboxID string) (interface{}, error) {
	manifest, err := e.readManifest(ctx, sandboxID)
	if err != nil {
		return nil, err
	}

	if len(manifest.Tools) == 0 {
		return map[string]interface{}{
			"tools":   []CustomToolEntry{},
			"message": "No custom tools created yet. Use create_tool to make one.",
		}, nil
	}

	// Sort by name
	sort.Slice(manifest.Tools, func(i, j int) bool {
		return manifest.Tools[i].Name < manifest.Tools[j].Name
	})

	return map[string]interface{}{
		"tools":  manifest.Tools,
		"count":  len(manifest.Tools),
	}, nil
}

// executeRunTool runs a custom tool by name.
func (e *SandboxToolExecutor) executeRunTool(ctx context.Context, sandboxID string, args map[string]interface{}) (interface{}, error) {
	name, _ := args["name"].(string)
	toolArgs, _ := args["args"].(map[string]interface{})

	if name == "" {
		return nil, fmt.Errorf("missing 'name' argument")
	}

	// Load manifest
	manifest, err := e.readManifest(ctx, sandboxID)
	if err != nil {
		return nil, err
	}

	var entry *CustomToolEntry
	for i := range manifest.Tools {
		if manifest.Tools[i].Name == name {
			entry = &manifest.Tools[i]
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("custom tool %q not found. Use list_tools to see available tools.", name)
	}

	// Determine script path
	var filename string
	switch entry.Language {
	case "python":
		filename = name + ".py"
	case "bash":
		filename = name + ".sh"
	case "node":
		filename = name + ".js"
	default:
		filename = name + ".py"
	}
	scriptPath := filepath.Join(customToolsDir, filename)

	// Build the command with args as environment variables
	var cmdParts []string
	for k, v := range toolArgs {
		cmdParts = append(cmdParts, fmt.Sprintf("%s=%v", strings.ToUpper(k), v))
	}
	envPrefix := ""
	if len(cmdParts) > 0 {
		envPrefix = strings.Join(cmdParts, " ") + " "
	}

	// Build the execution command
	var execCmd string
	switch entry.Language {
	case "python":
		execCmd = fmt.Sprintf("%spython3 %s", envPrefix, scriptPath)
	case "bash":
		execCmd = fmt.Sprintf("%sbash %s", envPrefix, scriptPath)
	case "node":
		execCmd = fmt.Sprintf("%snode %s", envPrefix, scriptPath)
	}

	// Also write args as a temp JSON file for scripts that prefer structured input
	if len(toolArgs) > 0 {
		argsJSON, _ := json.Marshal(toolArgs)
		argsWriteCmd := fmt.Sprintf("cat > /tmp/tool_args_%s.json << 'ARGS_EOF'\n%s\nARGS_EOF", name, string(argsJSON))
		e.Manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", argsWriteCmd})
	}

	// Execute
	output, err := e.Manager.ExecInSandbox(ctx, sandboxID, []string{"bash", "-c", execCmd})

	// Update stats
	now := time.Now().UTC().Format(time.RFC3339)
	entry.LastUsedAt = now
	if err != nil {
		entry.FailCount++
	} else {
		entry.SuccessCount++
	}
	e.writeManifest(ctx, sandboxID, manifest)

	if err != nil {
		return map[string]interface{}{
			"output": output,
			"error":  err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"output": output,
		"tool":   name,
	}, nil
}

// CustomToolsReference generates a reference section for the orchestrator prompt
// listing any custom tools in the manifest.
func CustomToolsReference(manifest *CustomToolManifest) string {
	if manifest == nil || len(manifest.Tools) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n### Custom Tools (user-created, persistent)\n")
	for _, t := range manifest.Tools {
		args := make([]string, 0, len(t.ArgsSchema))
		for k, v := range t.ArgsSchema {
			args = append(args, fmt.Sprintf("%s:%s", k, v))
		}
		fmt.Fprintf(&b, "- **run_tool{\"name\": %q, \"args\": {…}}** — %s (args: %s, success: %d, fail: %d)\n",
			t.Name, t.Description, strings.Join(args, ", "), t.SuccessCount, t.FailCount)
	}
	return b.String()
}

// LoadCustomToolsManifest reads the manifest from disk (for startup injection).
// This is called once at agent init, not per-request.
func LoadCustomToolsManifest() *CustomToolManifest {
	data, err := os.ReadFile(filepath.Join(customToolsDir, manifestFile))
	if err != nil {
		return nil
	}
	var manifest CustomToolManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	return &manifest
}
