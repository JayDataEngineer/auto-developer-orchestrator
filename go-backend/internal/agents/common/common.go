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

// AgentRole holds a loaded role/worker definition.
type AgentRole struct {
	Name        string
	Description string
	Prompt      string
	Tools       []string
	MCPServers  []string
	Imports     []string // legacy: from roles/<name>/config.yaml
	Capabilities []string // new: from workers/<name>.yaml
	MaxRounds   int
	Temperature float32
	Model       string
	Division    string // non-empty = division head, points to sub-dir with pux.yaml
	SandboxTier string // "isolated" (default), "bridged", "native"
}

// agentConfig is the YAML structure for config/roles/<name>/config.yaml (legacy)
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

// workerConfig is the YAML structure for config/workers/<name>.yaml (new)
type workerConfig struct {
	Persona      string   `yaml:"persona"`
	Capabilities []string `yaml:"capabilities"`
	Tools        []string `yaml:"tools,omitempty"`
	MCPServers   []string `yaml:"mcp_servers,omitempty"`
	MaxRounds    int      `yaml:"max_rounds"`
	Temperature  float64  `yaml:"temperature"`
	Model        string   `yaml:"model"`
	Sandbox      string   `yaml:"sandbox"`
	Division     string   `yaml:"division,omitempty"`
}

// ToolPackage is a shared tool group (legacy name, still used internally).
type ToolPackage struct {
	Name        string
	Description string
	Tools       []string
	MCPServers  []string
	Skill       string // SKILL.md content from capability folder
}

// toolPackageConfig is the YAML structure for config/tool_packages/<name>.yaml (legacy)
// and config/capabilities/<name>/capability.yaml (new).
type toolPackageConfig struct {
	Description  string   `yaml:"description"`
	Tools        []string `yaml:"tools"`
	MCPServers   []string `yaml:"mcp_servers"`
	SandboxTier  string   `yaml:"sandbox_tier"`
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

// FindKernelConfigDir resolves the kernel config/ directory by searching
// multiple locations. Returns "" if not found. This works regardless of
// whether PROJECT_ROOT points at the repo root, a projects parent, or
// is unset.
func FindKernelConfigDir() string {
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

	configDir := FindKernelConfigDir()
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

	// Also invalidate capabilities cache
	_ = dirChanged("capabilities", toolPkgModTime)
}

// LoadToolPackages reads capabilities from config/capabilities/ (new) then
// config/tool_packages/ (legacy), then org-specific dirs. Auto-reloads on change.
func LoadToolPackages() map[string]*ToolPackage {
	toolPkgMu.RLock()
	if toolPackages != nil && !dirChanged("tool_packages", toolPkgModTime) && !dirChanged("capabilities", toolPkgModTime) {
		pkgs := toolPackages
		toolPkgMu.RUnlock()
		return pkgs
	}
	toolPkgMu.RUnlock()

	toolPkgMu.Lock()
	defer toolPkgMu.Unlock()

	if toolPackages != nil && !dirChanged("tool_packages", toolPkgModTime) && !dirChanged("capabilities", toolPkgModTime) {
		return toolPackages
	}

	configDir := FindKernelConfigDir()

	// Start with legacy tool_packages (flat YAML files)
	legacyDir := "config/tool_packages"
	if configDir != "" {
		legacyDir = filepath.Join(configDir, "tool_packages")
	}
	toolPackages = LoadToolPackagesFrom(legacyDir)

	// Overlay with new capabilities (folders with capability.yaml + SKILL.md)
	capDir := "config/capabilities"
	if configDir != "" {
		capDir = filepath.Join(configDir, "capabilities")
	}
	capabilities := LoadCapabilitiesFrom(capDir)
	for name, pkg := range capabilities {
		toolPackages[name] = pkg // new overrides legacy
	}

	updateModTime("tool_packages", legacyDir, toolPkgModTime)
	updateModTime("capabilities", capDir, toolPkgModTime)
	return toolPackages
}

// LoadToolPackagesFrom scans a directory for .yaml tool package files (legacy flat format).
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

// LoadCapabilitiesFrom scans a directory for capability folders (new format).
// Each subfolder should contain capability.yaml and optionally SKILL.md.
func LoadCapabilitiesFrom(dir string) map[string]*ToolPackage {
	pkgs := make(map[string]*ToolPackage)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return pkgs
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		cfgPath := filepath.Join(dir, name, "capability.yaml")
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		var pc toolPackageConfig
		if err := yaml.Unmarshal(data, &pc); err != nil {
			continue
		}

		// Load SKILL.md if present
		skill := ""
		if skillData, err := os.ReadFile(filepath.Join(dir, name, "SKILL.md")); err == nil {
			skill = string(skillData)
		}

		pkgs[name] = &ToolPackage{
			Name:        name,
			Description: pc.Description,
			Tools:       pc.Tools,
			MCPServers:  pc.MCPServers,
			Skill:       skill,
		}
	}
	return pkgs
}

// GetCapabilitySkill returns the SKILL.md content for a capability by name.
func GetCapabilitySkill(name string) string {
	pkgs := LoadToolPackages()
	if pkg, ok := pkgs[name]; ok {
		return pkg.Skill
	}
	return ""
}

// BuildWorkerPrompt assembles a worker's full prompt from persona + capability skills.
func BuildWorkerPrompt(persona string, capabilities []string) string {
	var sb strings.Builder
	if persona != "" {
		sb.WriteString(persona)
		sb.WriteString("\n\n")
	}
	for _, capName := range capabilities {
		skill := GetCapabilitySkill(capName)
		if skill != "" {
			fmt.Fprintf(&sb, "--- %s capability ---\n%s\n\n", capName, skill)
		}
	}
	return sb.String()
}

// ResolveImports expands a list of tool package names into concrete tools + mcp_servers.
func ResolveImports(imports []string) (tools []string, mcpServers []string) {
	pkgs := LoadToolPackages()
	seenTools := make(map[string]bool)
	seenServers := make(map[string]bool)
	for _, name := range imports {
		pkg, ok := pkgs[name]
		if !ok {
			// Not a known tool package — treat as extension MCP server name
			if !seenServers[name] {
				seenServers[name] = true
				mcpServers = append(mcpServers, name)
			}
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

// LoadAgentRoles reads workers from config/workers/ (new) then config/roles/ (legacy).
// Auto-reloads when files change on disk.
func LoadAgentRoles() map[string]*AgentRole {
	agentMu.RLock()
	if agentRoles != nil && !dirChanged("roles", agentModTime) && !dirChanged("workers", agentModTime) {
		roles := agentRoles
		agentMu.RUnlock()
		return roles
	}
	agentMu.RUnlock()

	agentMu.Lock()
	defer agentMu.Unlock()

	if agentRoles != nil && !dirChanged("roles", agentModTime) && !dirChanged("workers", agentModTime) {
		return agentRoles
	}

	configDir := FindKernelConfigDir()

	// Start with legacy roles (folder format)
	legacyDir := "config/roles"
	if configDir != "" {
		legacyDir = filepath.Join(configDir, "roles")
	}
	agentRoles = LoadAgentRolesFrom(legacyDir)

	// Overlay with new workers (flat YAML format)
	workersDir := "config/workers"
	if configDir != "" {
		workersDir = filepath.Join(configDir, "workers")
	}
	workers := LoadWorkersFrom(workersDir)
	for name, role := range workers {
		agentRoles[name] = role // new overrides legacy
	}

	updateModTime("roles", legacyDir, agentModTime)
	updateModTime("workers", workersDir, agentModTime)
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

// LoadWorkersFrom scans a directory for flat worker YAML files (new format).
// Each file is config/workers/<name>.yaml with persona + capabilities fields.
// Capabilities are resolved into tools + mcp_servers, and SKILL.md content
// from each capability is stitched into the worker's prompt.
func LoadWorkersFrom(dir string) map[string]*AgentRole {
	roles := make(map[string]*AgentRole)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return roles
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
		var wc workerConfig
		if err := yaml.Unmarshal(data, &wc); err != nil {
			continue
		}
		if wc.Persona == "" && len(wc.Capabilities) == 0 {
			continue
		}

		maxRounds := wc.MaxRounds
		if maxRounds == 0 {
			maxRounds = 15
		}
		temp := float32(0.4)
		if wc.Temperature != 0 {
			temp = float32(wc.Temperature)
		}

		// Resolve capabilities → tools + mcp_servers + skills
		var tools, mcpServers []string
		if len(wc.Capabilities) > 0 {
			resolvedTools, resolvedMCP := ResolveImports(wc.Capabilities)
			tools = append(tools, resolvedTools...)
			mcpServers = append(mcpServers, resolvedMCP...)
		}
		// Also allow direct tools/mcp_servers in worker YAML
		tools = append(tools, wc.Tools...)
		mcpServers = append(mcpServers, wc.MCPServers...)

		// Build prompt from persona + capability skills
		prompt := BuildWorkerPrompt(wc.Persona, wc.Capabilities)

		// Determine sandbox tier: worker override > highest capability requirement
		sandboxTier := wc.Sandbox
		if sandboxTier == "" {
			sandboxTier = highestSandboxTier(wc.Capabilities)
		}

		roles[name] = &AgentRole{
			Name:         name,
			Description:  wc.Persona,
			Prompt:       prompt,
			Tools:        tools,
			MCPServers:   mcpServers,
			Capabilities: wc.Capabilities,
			Imports:      wc.Capabilities, // for FormatAgentList compat
			MaxRounds:    maxRounds,
			Temperature:  temp,
			Model:        wc.Model,
			Division:     wc.Division,
			SandboxTier:  sandboxTier,
		}
	}
	return roles
}

// highestSandboxTier returns the most privileged sandbox tier required by the given capabilities.
func highestSandboxTier(capabilities []string) string {
	pkgs := LoadToolPackages()
	highest := ""
	for _, name := range capabilities {
		if _, ok := pkgs[name]; ok {
			// Read sandbox_tier from the raw capability config
			tier := capabilitySandboxTier(name)
			switch {
			case tier == "bridged":
				return "bridged" // highest
			case tier == "native" && highest != "bridged":
				highest = "native"
			case highest == "":
				highest = tier
			}
		}
	}
	return highest
}

// capabilitySandboxTier reads the sandbox_tier field from a capability folder.
func capabilitySandboxTier(name string) string {
	configDir := FindKernelConfigDir()
	capDir := "config/capabilities"
	if configDir != "" {
		capDir = filepath.Join(configDir, "capabilities")
	}
	cfgPath := filepath.Join(capDir, name, "capability.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return ""
	}
	var pc toolPackageConfig
	if err := yaml.Unmarshal(data, &pc); err != nil {
		return ""
	}
	return pc.SandboxTier
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
	return buildPrompt(tools, sandboxID, projectContext, examples, FormatAgentList())
}

// buildPrompt is the shared builder — accepts a custom agents list so org mode can pass merged roles.
func buildPrompt(tools []core.Tool, sandboxID string, projectContext string, examples string, agents string) string {
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
		Agents:         agents,
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
// Prepends the org manifesto and merges org roles with kernel roles.
func BuildOrchestratorPromptWithOrg(tools []core.Tool, sandboxID string, projectContext string, examples string, org *OrgManifest, orgRoles map[string]*AgentRole) string {
	// If no org, return standard prompt
	if org == nil {
		return BuildOrchestratorPrompt(tools, sandboxID, projectContext, examples)
	}

	// Build org header
	var header strings.Builder
	fmt.Fprintf(&header, "# Organization: %s\n%s\n\n", org.Name, org.Description)

	// Prepend manifesto if present
	if manifesto := org.ManifestoContent(); manifesto != "" {
		fmt.Fprintf(&header, "## Manifesto\n%s\n\n", manifesto)
	}

	// Contract 6: Org roles MERGE with kernel roles — never replace.
	// Kernel staff (jake, ryan, sarah, etc.) are immutable. Orgs can only ADD
	// new roles. Name collisions with kernel roles are silently ignored.
	agents := FormatAgentList()
	if len(orgRoles) > 0 {
		merged := LoadAgentRoles()
		for name, role := range orgRoles {
			if _, isKernel := merged[name]; isKernel {
				// Kernel role — org cannot override. Skip.
				continue
			}
			merged[name] = role
		}
		agents = formatRolesList(merged)
	}

	// Build the prompt with merged agents
	base := buildPrompt(tools, sandboxID, projectContext, examples, agents)

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
	configDir := FindKernelConfigDir()
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
	configDir := FindKernelConfigDir()
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
