package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"gopkg.in/yaml.v3"
)

// ToOpenAITools converts Tool list to OpenAI format.
func ToOpenAITools(tools []core.Tool) []core.OpenAITool {
	result := make([]core.OpenAITool, 0, len(tools))
	for _, t := range tools {
		result = append(result, core.OpenAITool{
			Type: "function",
			Function: core.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Schema(),
			},
		})
	}
	return result
}

// AgentRole holds a loaded role definition from config/roles/<name>/
type AgentRole struct {
	Name        string
	Description string
	Prompt      string
	Tools       []string
	MCPServers  []string
	Imports     []string
	MaxRounds   int
	Temperature float32
	Model       string
	Division    string // non-empty = division head, points to sub-dir with pux.yaml
	SandboxTier string // "isolated" (default), "bridged", "native"
}

// agentConfig is the YAML structure for config/roles/<name>/config.yaml
type agentConfig struct {
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	MCPServers  []string `yaml:"mcp_servers"`
	Imports     []string `yaml:"imports"`
	MaxRounds   int      `yaml:"max_rounds"`
	Temperature float64  `yaml:"temperature"`
	Model       string   `yaml:"model"`
	Division    string   `yaml:"division"`
	Sandbox     string   `yaml:"sandbox"`
}

// ToolPackage is a shared tool group from config/tool_packages/<name>.yaml
type ToolPackage struct {
	Name        string
	Description string
	Tools       []string
	MCPServers  []string
}

// toolPackageConfig is the YAML structure for config/tool_packages/<name>.yaml
type toolPackageConfig struct {
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	MCPServers  []string `yaml:"mcp_servers"`
}

// promptData holds template variables for the main system prompt.
type promptData struct {
	Tools         string
	Agents        string
	SandboxID     string
	Skills        string
	ProjectContext string
}

var (
	promptTmpl    *template.Template
	promptLoadErr error
	promptMu      sync.RWMutex
	promptModTime = make(map[string]time.Time)

	agentRoles    map[string]*AgentRole
	agentMu       sync.RWMutex
	agentModTime  = make(map[string]time.Time)

	toolPackages   map[string]*ToolPackage
	toolPkgMu      sync.RWMutex
	toolPkgModTime = make(map[string]time.Time)
)

// findKernelConfigDir resolves the kernel config/ directory by searching
// multiple locations. Returns "" if not found. This works regardless of
// whether PROJECT_ROOT points at the repo root, a projects parent, or
// is unset.
func findKernelConfigDir() string {
	root := os.Getenv("PROJECT_ROOT")

	// Candidate base directories to check for config/
	type dirSrc struct {
		path string
	}
	candidates := []dirSrc{}

	// 1. PROJECT_ROOT itself
	if root != "" {
		candidates = append(candidates, dirSrc{root})
	}

	// 2. Walk up from executable binary
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for range 5 {
			candidates = append(candidates, dirSrc{dir})
			dir = filepath.Dir(dir)
		}
	}

	// 3. Working directory and parents
	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for range 3 {
			candidates = append(candidates, dirSrc{dir})
			dir = filepath.Dir(dir)
		}
	}

	for _, c := range candidates {
		p := filepath.Join(c.path, "config", "prompt.md")
		if _, err := os.Stat(p); err == nil {
			return filepath.Join(c.path, "config")
		}
	}

	return ""
}

// loadPromptTemplate loads and parses config/prompt.md as a Go text/template.
// Auto-reloads when the file changes on disk — no server restart needed.
func loadPromptTemplate() (*template.Template, error) {
	promptMu.RLock()
	if promptTmpl != nil && !fileChanged("prompt", promptModTime) {
		tmpl, err := promptTmpl, promptLoadErr
		promptMu.RUnlock()
		return tmpl, err
	}
	promptMu.RUnlock()

	promptMu.Lock()
	defer promptMu.Unlock()

	// Double-check after acquiring write lock
	if promptTmpl != nil && !fileChanged("prompt", promptModTime) {
		return promptTmpl, promptLoadErr
	}

	configDir := findKernelConfigDir()
	path := "config/prompt.md"
	if configDir != "" {
		path = filepath.Join(configDir, "prompt.md")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		promptTmpl, promptLoadErr = template.New("system").Parse(defaultPrompt)
		return promptTmpl, promptLoadErr
	}

	promptTmpl, promptLoadErr = template.New("system").Parse(string(data))
	updateModTime("prompt", path, promptModTime)
	return promptTmpl, promptLoadErr
}

// MergeToolPackages loads tool packages from a directory and merges them into the
// global kernel cache. Used by org mode to make org-specific tool packages (e.g.,
// tech_noir_art, godot, comfyui, studio_vision) resolvable via ResolveImports.
// Must be called before loading org roles so that their imports resolve correctly.
func MergeToolPackages(dir string) {
	pkgs := LoadToolPackagesFrom(dir)
	toolPkgMu.Lock()
	defer toolPkgMu.Unlock()
	if toolPackages == nil {
		toolPackages = make(map[string]*ToolPackage)
	}
	for name, pkg := range pkgs {
		toolPackages[name] = pkg
	}
}

// ReloadPromptTemplate forces a reload of all prompt templates (for development).
func ReloadPromptTemplate() {
	promptMu.Lock()
	promptTmpl = nil
	promptModTime = map[string]time.Time{}
	promptMu.Unlock()

	agentMu.Lock()
	agentRoles = nil
	agentModTime = map[string]time.Time{}
	agentMu.Unlock()

	toolPkgMu.Lock()
	toolPackages = nil
	toolPkgModTime = map[string]time.Time{}
	toolPkgMu.Unlock()
}

// LoadToolPackages reads all .yaml files from the kernel's config/tool_packages/ directory.
// Auto-reloads when files change on disk.
func LoadToolPackages() map[string]*ToolPackage {
	toolPkgMu.RLock()
	if toolPackages != nil && !dirChanged("tool_packages", toolPkgModTime) {
		pkgs := toolPackages
		toolPkgMu.RUnlock()
		return pkgs
	}
	toolPkgMu.RUnlock()

	toolPkgMu.Lock()
	defer toolPkgMu.Unlock()

	if toolPackages != nil && !dirChanged("tool_packages", toolPkgModTime) {
		return toolPackages
	}

	configDir := findKernelConfigDir()
	dir := "config/tool_packages"
	if configDir != "" {
		dir = filepath.Join(configDir, "tool_packages")
	}
	toolPackages = LoadToolPackagesFrom(dir)
	updateModTime("tool_packages", dir, toolPkgModTime)
	return toolPackages
}

// LoadToolPackagesFrom scans a directory for .yaml tool package files.
func LoadToolPackagesFrom(dir string) map[string]*ToolPackage {
	pkgs := make(map[string]*ToolPackage)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return pkgs
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var pc toolPackageConfig
		if err := yaml.Unmarshal(data, &pc); err != nil {
			continue
		}
		pkgs[name] = &ToolPackage{
			Name:        name,
			Description: pc.Description,
			Tools:       pc.Tools,
			MCPServers:  pc.MCPServers,
		}
	}
	return pkgs
}

// ResolveImports expands a list of tool package names into concrete tools + mcp_servers.
func ResolveImports(imports []string) (tools []string, mcpServers []string) {
	pkgs := LoadToolPackages()
	seenTools := make(map[string]bool)
	seenServers := make(map[string]bool)
	for _, name := range imports {
		pkg, ok := pkgs[name]
		if !ok {
			continue
		}
		for _, t := range pkg.Tools {
			if !seenTools[t] {
				seenTools[t] = true
				tools = append(tools, t)
			}
		}
		for _, s := range pkg.MCPServers {
			if !seenServers[s] {
				seenServers[s] = true
				mcpServers = append(mcpServers, s)
			}
		}
	}
	return tools, mcpServers
}

// LoadAgentRoles reads role folders from the kernel's config/roles/ directory.
// Auto-reloads when files change on disk. Use LoadAgentRolesFrom for org-specific directories.
func LoadAgentRoles() map[string]*AgentRole {
	agentMu.RLock()
	if agentRoles != nil && !dirChanged("roles", agentModTime) {
		roles := agentRoles
		agentMu.RUnlock()
		return roles
	}
	agentMu.RUnlock()

	agentMu.Lock()
	defer agentMu.Unlock()

	if agentRoles != nil && !dirChanged("roles", agentModTime) {
		return agentRoles
	}

	configDir := findKernelConfigDir()
	dir := "config/roles"
	if configDir != "" {
		dir = filepath.Join(configDir, "roles")
	}
	agentRoles = LoadAgentRolesFrom(dir)
	updateModTime("roles", dir, agentModTime)
	return agentRoles
}

// LoadAgentRolesFrom scans a directory for role folders.
// Each subfolder must contain config.yaml + prompt.md.
func LoadAgentRolesFrom(dir string) map[string]*AgentRole {
	roles := make(map[string]*AgentRole)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return roles
	}

	for _, entry := range entries {
		if entry.IsDir() {
			role := loadRoleFromFolder(filepath.Join(dir, entry.Name()))
			if role != nil {
				role.Name = entry.Name()
				roles[entry.Name()] = role
			}
		}
	}
	return roles
}

// loadRoleFromFolder reads config.yaml + prompt.md from a role folder.
// If config.yaml has an `imports` field, resolves it into Tools + MCPServers.
func loadRoleFromFolder(folder string) *AgentRole {
	cfg, err := os.ReadFile(filepath.Join(folder, "config.yaml"))
	if err != nil {
		return nil
	}

	var ac agentConfig
	if err := yaml.Unmarshal(cfg, &ac); err != nil {
		return nil
	}

	prompt, err := os.ReadFile(filepath.Join(folder, "prompt.md"))
	if err != nil {
		return nil
	}

	maxRounds := ac.MaxRounds
	if maxRounds == 0 {
		maxRounds = 15
	}

	temp := float32(0.4)
	if ac.Temperature != 0 {
		temp = float32(ac.Temperature)
	}

	// Resolve imports → concrete tools + mcp_servers
	tools := ac.Tools
	mcpServers := ac.MCPServers
	if len(ac.Imports) > 0 {
		importTools, importMCPServers := ResolveImports(ac.Imports)
		tools = append(tools, importTools...)
		mcpServers = append(mcpServers, importMCPServers...)
	}

	return &AgentRole{
		Description: ac.Description,
		Prompt:      string(prompt),
		Tools:       tools,
		MCPServers:  mcpServers,
		Imports:     ac.Imports,
		MaxRounds:   maxRounds,
		Temperature: temp,
		Model:       ac.Model,
		Division:    ac.Division,
		SandboxTier: ac.Sandbox,
	}
}

// GetAgentRole returns a specific agent role by name.
func GetAgentRole(name string) *AgentRole {
	roles := LoadAgentRoles()
	return roles[name]
}

// FormatAgentList returns a formatted list of available agents for the prompt.
func FormatAgentList() string {
	roles := LoadAgentRoles()
	if len(roles) == 0 {
		return "No roles loaded from config/roles/"
	}
	return formatRolesList(roles)
}

// formatRolesList formats a roles map as markdown for the prompt.
func formatRolesList(roles map[string]*AgentRole) string {
	if len(roles) == 0 {
		return ""
	}

	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		role := roles[name]
		var capability string
		if role.Division != "" {
			capability = "division: " + role.Division
		} else if len(role.Imports) > 0 {
			capability = "imports: " + strings.Join(role.Imports, ", ")
		} else {
			capability = strings.Join(role.Tools, ", ")
			if len(role.MCPServers) > 0 {
				if capability != "" {
					capability += ", "
				}
				capability += "mcp:" + strings.Join(role.MCPServers, ", mcp:")
			}
		}
		if role.SandboxTier != "" && role.SandboxTier != "isolated" {
			capability += ", sandbox:" + role.SandboxTier
		}
		fmt.Fprintf(&b, "### %s\n%s\nCapabilities: %s\n\n", role.Name, role.Description, capability)
	}
	return b.String()
}

// AgentNames returns sorted agent name list from kernel roles plus org-specific roles.
func AgentNames(orgRoles map[string]*AgentRole) []string {
	seen := make(map[string]bool)
	for name := range LoadAgentRoles() {
		seen[name] = true
	}
	for name := range orgRoles {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// BuildOrchestratorPrompt builds the full system prompt using the template.
func BuildOrchestratorPrompt(tools []core.Tool, sandboxID string, projectContext string, examples string) string {
	tmpl, err := loadPromptTemplate()
	if err != nil {
		// Should not happen with fallback, but just in case
		return defaultPromptText(tools, sandboxID)
	}

	// Build tool list
	var toolSection strings.Builder
	for _, t := range tools {
		schema := formatSchema(t.Schema())
		fmt.Fprintf(&toolSection, "## %s — %s\n%s\n\n", t.Name(), t.Description(), schema)
	}

	data := promptData{
		Tools:          toolSection.String(),
		Agents:         FormatAgentList(),
		SandboxID:      sandboxID,
		ProjectContext: projectContext,
	}

	// Skills are handled separately by the caller
	if examples != "" {
		data.Skills = examples
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return defaultPromptText(tools, sandboxID)
	}

	return buf.String()
}

// BuildOrchestratorPromptWithOrg builds the system prompt with an org overlay.
// Prepends the org manifesto and uses org-specific roles if available.
func BuildOrchestratorPromptWithOrg(tools []core.Tool, sandboxID string, projectContext string, examples string, org *OrgManifest, orgRoles map[string]*AgentRole) string {
	// Build the base prompt
	base := BuildOrchestratorPrompt(tools, sandboxID, projectContext, examples)

	// If no org, return base as-is
	if org == nil {
		return base
	}

	// Build org header
	var header strings.Builder
	fmt.Fprintf(&header, "# Organization: %s\n%s\n\n", org.Name, org.Description)

	// Prepend manifesto if present
	if manifesto := org.ManifestoContent(); manifesto != "" {
		fmt.Fprintf(&header, "## Manifesto\n%s\n\n", manifesto)
	}

	// If org has custom roles, replace the Employees section
	if len(orgRoles) > 0 {
		// Rebuild agent list from org roles
		agentList := formatRolesList(orgRoles)
		// Replace the {{.Agents}} section in the base prompt
		// The base already expanded the template, so we replace the kernel's agent list
		kernelAgents := FormatAgentList()
		if kernelAgents != "" && agentList != "" {
			base = strings.Replace(base, kernelAgents, agentList, 1)
		}
	}

	return header.String() + base
}

// formatSchema formats a JSON Schema as a readable string.
func formatSchema(schema json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		return string(schema)
	}

	props, _ := m["properties"].(map[string]any)
	if props == nil {
		return string(schema)
	}

	var args []string
	for name, prop := range props {
		pm, _ := prop.(map[string]any)
		desc := ""
		if d, ok := pm["description"].(string); ok {
			desc = d
		}
		typ, _ := pm["type"].(string)
		example := formatExample(typ)
		args = append(args, fmt.Sprintf("%s=%s (%s)", name, example, desc))
	}

	return fmt.Sprintf("Args: %s", strings.Join(args, ", "))
}

func formatExample(typ string) string {
	switch typ {
	case "string":
		return "\"...\""
	case "integer", "number":
		return "0"
	case "boolean":
		return "true"
	case "array":
		return "[]"
	default:
		return "\"...\""
	}
}

// Embedded fallback prompt — used when config/prompt.md is not found.
const defaultPrompt = `You are Pux — an orchestrator that dispatches employees to complete tasks.
You do NOT do the work yourself. Delegate using delegate_to and delegate_async.

# Tools

` + "{{.Tools}}" + `

# Employees

` + "{{.Agents}}" + `

# Rules
1. DELEGATE first, do yourself second
2. Use delegate_to with employee role names (jake, sarah, alex, marcus, elena, ryan)
3. Synthesize results and respond concisely

{{if .SandboxID}}Sandbox ID: {{.SandboxID}}{{end}}
`

func defaultPromptText(tools []core.Tool, sandboxID string) string {
	var b strings.Builder
	b.WriteString("You are Pux orchestrator.\n\nTools:\n")
	for _, t := range tools {
		b.WriteString(fmt.Sprintf("- %s: %s\n", t.Name(), t.Description()))
	}
	if sandboxID != "" {
		b.WriteString("\nSandbox ID: " + sandboxID + "\n")
	}
	return b.String()
}

// fileChanged checks if a tracked file has been modified since last load.
// Returns true if the file is newer than the cached modTime.
func fileChanged(key string, modTimes map[string]time.Time) bool {
	t, ok := modTimes[key]
	if !ok {
		return true // never loaded
	}
	configDir := findKernelConfigDir()
	path := "config/" + key + ".md"
	if configDir != "" {
		path = filepath.Join(configDir, key+".md")
	}
	info, err := os.Stat(path)
	if err != nil {
		return false // can't stat → keep cached
	}
	return info.ModTime().After(t)
}

// dirChanged checks if any file in a tracked directory has been modified.
// Returns true if any file is newer than the cached modTime.
func dirChanged(key string, modTimes map[string]time.Time) bool {
	t, ok := modTimes[key]
	if !ok {
		return true // never loaded
	}
	configDir := findKernelConfigDir()
	dir := "config/" + key
	if configDir != "" {
		dir = filepath.Join(configDir, key)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false // can't read → keep cached
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(t) {
			return true
		}
	}
	return false
}

// updateModTime records the current time as the last load time for a key.
func updateModTime(key string, path string, modTimes map[string]time.Time) {
	modTimes[key] = time.Now()
}
