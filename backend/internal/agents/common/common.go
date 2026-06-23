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
	Hint        string // CTO-facing one-liner. Falls back to Description if empty.
	Description string
	Prompt      string
	Tools       []string
	MCPServers  []string
	Imports     []string // legacy: from roles/<name>/config.yaml
	Capabilities []string // new: from workers/<name>.yaml
	MaxRounds   int
	Temperature float32
	Model       string
	Thinking    bool   // enable extended thinking (CoT) on supporting models
	Division    string   // non-empty = division head, points to sub-dir with pux.yaml
	SandboxTier string   // "isolated" (default), "bridged", "native"
	DelegatesTo []string // if non-empty, this worker gets scoped delegation tools
	Hooks       []string // named hooks to attach to sub-agents (e.g., "file_checkpoint", "raise_browser")
	Skills      []string // discoverable-skill scope (P2): explicit allowlist of skill names this role can read_skill
}

// RoleConfig is the single YAML structure for ALL role config files:
//   - config/workers/<name>.yaml          (kernel workers: persona + capabilities)
//   - orgs/<org>/roles/<name>/config.yaml (org roles: same shape)
//
// Both `imports` and `capabilities` are accepted and treated as aliases —
// both are lists of tool-package names expanded by ResolveImports. Legacy
// roles tend to use `imports`; new workers tend to use `capabilities`.
//
// Adding a new YAML field here automatically makes it available to every
// loader (legacy, worker, org, autoconfig). There is no second struct to
// keep in sync.
type RoleConfig struct {
	// Worker-only cosmetic fields
	Hint string `yaml:"hint,omitempty"` // CTO-facing one-liner shown in agent picker

	// Identity. Legacy uses Description; workers use Persona. Either is valid;
	// loaders pick whichever is populated.
	Description string `yaml:"description,omitempty"`
	Persona     string `yaml:"persona,omitempty"`

	// Tool-package expansion. Aliases — both feed ResolveImports.
	Imports      []string `yaml:"imports,omitempty"`
	Capabilities []string `yaml:"capabilities,omitempty"`

	// Direct (bypass package expansion)
	Tools      []string `yaml:"tools,omitempty"`
	MCPServers []string `yaml:"mcp_servers,omitempty"`

	// Behavior
	MaxRounds   int     `yaml:"max_rounds"`
	Temperature float64 `yaml:"temperature"`
	Model       string  `yaml:"model"`
	Thinking    bool    `yaml:"thinking"`

	// Delegation / sandbox / lifecycle
	Division    string   `yaml:"division,omitempty"`
	Sandbox     string   `yaml:"sandbox,omitempty"`
	DelegatesTo []string `yaml:"delegates_to,omitempty"`
	Hooks       []string `yaml:"hooks,omitempty"`

	// Discoverable-skill scope — names of skills this role can read_skill.
	// Empty means: derive scope from capability-attached skills (frontmatter
	// `capabilities: [name]`). Workers without explicit Skills or
	// capability-attached skills do not get read_skill at all (preserves the
	// pre-P2 default of CTO-only access).
	Skills []string `yaml:"skills,omitempty"`
}

// ToolPackage is a shared tool group (legacy name, still used internally).
type ToolPackage struct {
	Name           string
	Description    string
	Tools          []string
	MCPServers     []string
	Skill          string // SKILL.md content from capability folder
	Implementations []Implementation // raw parsed from capability.yaml; nil for legacy
	ActiveImpl     *Implementation // set by CapabilityResolver at boot; nil for legacy or unresolved
	Dir            string // capability folder path (for prompt_file resolution)
}

// Implementation is a single tier of a polymorphic capability. See
// pux-declarative-stack.md RFC axis 2. Lower Priority wins. Health check
// determines live-or-down at boot. Sticky per session — re-resolved only
// when HealthMonitor invalidates the cache after N consecutive failures.
type Implementation struct {
	Name       string            `yaml:"name"`
	Type       string            `yaml:"type"`                 // "mcp" | "bash" | "http" | "extension" (informational)
	Priority   int               `yaml:"priority"`             // lower = preferred
	MCPServers []string          `yaml:"mcp_servers,omitempty"` // MCP tier: server prefixes to wire
	Tools      []string          `yaml:"tools,omitempty"`       // bash tier: tool names to expose
	DeclTools  []DeclarativeTool `yaml:"decl_tools,omitempty"`  // Phase 4: YAML-defined tools this tier exposes
	Script     string            `yaml:"script,omitempty"`      // bash tier: absolute path inside sandbox
	Source     string            `yaml:"source,omitempty"`      // extension tier: git+URL cloned by pre-warmer (Phase 3)
	Bringup    string            `yaml:"bringup,omitempty"`     // extension tier: shell command to launch after clone
	Prompt     string            `yaml:"prompt,omitempty"`      // inline prompt (mutually exclusive with PromptFile)
	PromptFile string            `yaml:"prompt_file,omitempty"` // file rel to capability dir; resolved into Prompt
	Health     HealthCheck       `yaml:"health"`
}

// DeclarativeTool is a YAML-defined tool. The factory in tools/decltools
// turns each declaration into a core.Tool instance by substituting {{param}}
// with shell-quoted values and running the command through bash.Executor.
// See pux-declarative-stack.md RFC axis 2 / Phase 4.
type DeclarativeTool struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description"`
	Command     string      `yaml:"command"`           // bash template, {{param}} substituted
	Parameters  []ToolParam `yaml:"parameters"`
	Timeout     int         `yaml:"timeout,omitempty"` // seconds; 0 = no per-tool timeout
}

// ToolParam is a single parameter for a DeclarativeTool. Type is one of the
// JSON-schema primitives: "string", "integer", "number", "boolean".
type ToolParam struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
	Default     any    `yaml:"default,omitempty"`
}

// HealthCheck probes whether an Implementation is usable at boot.
type HealthCheck struct {
	Kind           string `yaml:"kind"`                     // "mcp-available" | "http-get" | "always-true"
	Server         string `yaml:"server,omitempty"`         // for mcp-available: server prefix
	URL            string `yaml:"url,omitempty"`            // for http-get: URL to GET
	BringupTimeout int    `yaml:"bringup_timeout,omitempty"` // seconds; default 60. Caps pre-warm wait.
}

// toolPackageConfig is the YAML structure for config/capabilities/<name>/capability.yaml
// and orgs/<org>/tool_packages/<name>.yaml.
type toolPackageConfig struct {
	Description     string           `yaml:"description"`
	Tools           []string         `yaml:"tools"`
	MCPServers      []string         `yaml:"mcp_servers"`
	SandboxTier     string           `yaml:"sandbox_tier"`
	Implementations []Implementation `yaml:"implementations,omitempty"`
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

	agentRoles     map[string]*AgentRole
	agentMu        sync.RWMutex
	agentModTime   = make(map[string]time.Time)
	agentConfigDir string

	toolPackages    map[string]*ToolPackage
	toolPkgMu       sync.RWMutex
	toolPkgModTime  = make(map[string]time.Time)
	toolPkgConfigDir string
)

// FindKernelConfigDir resolves the kernel config/ directory by searching
// multiple locations. Returns "" if not found. This works regardless of
// whether PROJECT_ROOT points at the repo root, a projects parent, or
// is unset.
//
// If PROJECT_ROOT is set, it is treated as authoritative — the function
// checks it and returns "" if no config/ lives there. No fall-through to
// walk-up discovery. This keeps the contract honest: PROJECT_ROOT says
// "the kernel lives here," and if it doesn't, that's a real state worth
// surfacing rather than papering over with a guessed location.
func FindKernelConfigDir() string {
	root := os.Getenv("PROJECT_ROOT")

	// Candidate base directories to check for config/
	type dirSrc struct {
		path string
	}

	// 1. PROJECT_ROOT — authoritative if set
	if root != "" {
		if _, err := os.Stat(filepath.Join(root, "config", "prompt.md")); err == nil {
			return filepath.Join(root, "config")
		}
		return ""
	}

	candidates := []dirSrc{}

	// 2. Walk up from executable binary
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for range 5 {
			candidates = append(candidates, dirSrc{dir})
			dir = filepath.Dir(dir)
		}
	}

	// 3. Working directory and parents (deep walk-up so tests run from
	// deeply-nested package dirs — e.g. backend/internal/agents/common/ —
	// can still locate config/prompt.md at the repo root when PROJECT_ROOT
	// is unset).
	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for range 8 {
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

// FindSharedClientsDir resolves the repo's orgs/_shared/clients/ directory.
// Used by the sandbox init loader to resolve `@shared/<name>` references in
// pux.yaml init_files — lets orgs share canonical client scripts instead of
// each carrying their own copy.
func FindSharedClientsDir() string {
	root := os.Getenv("PROJECT_ROOT")

	candidates := []string{}
	if root != "" {
		candidates = append(candidates, root)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for range 6 {
			candidates = append(candidates, dir)
			dir = filepath.Dir(dir)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for range 10 {
			candidates = append(candidates, dir)
			dir = filepath.Dir(dir)
		}
	}

	for _, c := range candidates {
		p := filepath.Join(c, "orgs", "_shared", "clients")
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
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
	agentConfigDir = ""
	agentMu.Unlock()

	toolPkgMu.Lock()
	toolPackages = nil
	toolPkgModTime = map[string]time.Time{}
	toolPkgConfigDir = ""
	toolPkgMu.Unlock()

	// Also invalidate capabilities cache
	_ = dirChanged("capabilities", toolPkgModTime)
}

// LoadToolPackages reads capabilities from config/capabilities/. Auto-reloads on
// change. Org-level tool packages (orgs/<org>/tool_packages/*.yaml) are merged
// separately via MergeToolPackages at session start.
func LoadToolPackages() map[string]*ToolPackage {
	curDir := FindKernelConfigDir()

	toolPkgMu.RLock()
	if toolPackages != nil && toolPkgConfigDir == curDir && !dirChanged("capabilities", toolPkgModTime) {
		pkgs := toolPackages
		toolPkgMu.RUnlock()
		return pkgs
	}
	toolPkgMu.RUnlock()

	toolPkgMu.Lock()
	defer toolPkgMu.Unlock()

	curDir = FindKernelConfigDir()
	if toolPackages != nil && toolPkgConfigDir == curDir && !dirChanged("capabilities", toolPkgModTime) {
		return toolPackages
	}

	configDir := curDir

	capDir := "config/capabilities"
	if configDir != "" {
		capDir = filepath.Join(configDir, "capabilities")
	}
	toolPackages = LoadCapabilitiesFrom(capDir)

	toolPkgConfigDir = configDir
	updateModTime("capabilities", capDir, toolPkgModTime)
	return toolPackages
}

// LoadToolPackagesFrom scans a directory for flat-YAML tool package files.
// Used by MergeToolPackages for org-level packages (orgs/<org>/tool_packages/*.yaml).
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
			Name:           name,
			Description:    pc.Description,
			Tools:          pc.Tools,
			MCPServers:     pc.MCPServers,
			Implementations: pc.Implementations,
			Dir:            dir,
		}
	}
	return pkgs
}

// LoadCapabilitiesFrom scans a directory for capability folders (new format).
// Each subfolder should contain capability.yaml and optionally SKILL.md.
// Implementations with PromptFile have their prompt content resolved into Prompt
// (relative to the capability folder). Implementations with inline Prompt are
// left untouched.
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
		capDir := filepath.Join(dir, name)
		cfgPath := filepath.Join(capDir, "capability.yaml")
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		var pc toolPackageConfig
		if err := yaml.Unmarshal(data, &pc); err != nil {
			continue
		}

		// Resolve PromptFile → Prompt for each implementation. We keep PromptFile
		// on the struct for debugging; Prompt is the canonical field downstream.
		for i := range pc.Implementations {
			impl := &pc.Implementations[i]
			if impl.Prompt == "" && impl.PromptFile != "" {
				if body, err := os.ReadFile(filepath.Join(capDir, impl.PromptFile)); err == nil {
					impl.Prompt = string(body)
				}
			}
		}

		// Load SKILL.md if present (kept for backward compat; polymorphic
		// capabilities will route through ActiveImpl.Prompt instead)
		skill := ""
		if skillData, err := os.ReadFile(filepath.Join(capDir, "SKILL.md")); err == nil {
			skill = string(skillData)
		}

		pkgs[name] = &ToolPackage{
			Name:           name,
			Description:    pc.Description,
			Tools:          pc.Tools,
			MCPServers:     pc.MCPServers,
			Skill:          skill,
			Implementations: pc.Implementations,
			Dir:            capDir,
		}

		// Boot-safe default: if implementations[] exists but no resolver has
		// run yet (tests, early boot, or unconfigured env), pick the highest-
		// priority impl as a stand-in ActiveImpl. The resolver overrides this
		// at boot via ResolveAll(). Without this, callers that load
		// capabilities directly get an empty tool list.
		if len(pc.Implementations) > 0 {
			pkgs[name].ActiveImpl = pickDefaultImplementation(pc.Implementations)
		}
	}
	return pkgs
}

// pickDefaultImplementation returns the highest-priority (lowest priority
// number) implementation. Used as a boot-safe stand-in when the resolver
// has not run. Ties broken by first-declared order.
func pickDefaultImplementation(imps []Implementation) *Implementation {
	if len(imps) == 0 {
		return nil
	}
	best := &imps[0]
	for i := 1; i < len(imps); i++ {
		if imps[i].Priority < best.Priority {
			best = &imps[i]
		}
	}
	return best
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
// The prompt ends with a DynamicBoundary so the HTTP layer can split and cache
// the stable portion (persona + SKILL.md) separately from any future dynamic content.
//
// For polymorphic capabilities (those with implementations[]), the active
// implementation's Prompt is preferred over the legacy SKILL.md. This is the
// morphing-prompt hook: when infrastructure is degraded and the resolver picks
// the bash tier, the worker's prompt rewrites itself to describe the bash-tier
// tools instead of the cloud-tier MCP. Legacy capabilities without
// implementations[] continue to use SKILL.md verbatim.
func BuildWorkerPrompt(persona string, capabilities []string) string {
	var sb strings.Builder
	if persona != "" {
		sb.WriteString(persona)
		sb.WriteString("\n\n")
	}
	for _, capName := range capabilities {
		skill := capabilityPrompt(capName)
		if skill != "" {
			fmt.Fprintf(&sb, "--- %s capability ---\n%s\n\n", capName, skill)
		}
	}
	sb.WriteString(DynamicBoundary)
	return sb.String()
}

// capabilityPrompt returns the prompt text for a capability, preferring the
// resolver-selected implementation's Prompt over the legacy SKILL.md. Returns
// "" if neither is set (capability will be omitted from the worker prompt).
func capabilityPrompt(capName string) string {
	if r := GetGlobalResolver(); r != nil {
		if impl := r.Resolve(capName); impl != nil && impl.Prompt != "" {
			return impl.Prompt
		}
	}
	return GetCapabilitySkill(capName)
}

// ResolveImports expands a list of tool package names into concrete tools + mcp_servers.
//
// Polymorphic packages (those with an ActiveImpl set by CapabilityResolver)
// route through the active implementation's Tools/MCPServers — NOT the
// top-level Tools/MCPServers on the package. This is the load-bearing fix
// for capability polymorphism: when the resolver downgrades a capability
// from cloud to bash, the tools list swaps to the bash tier's tools and the
// MCP server list drops the dead server. Without this, workers would receive
// the morphed prompt ("use bash") but the dead MCP tools — a contradiction.
//
// Legacy packages (no ActiveImpl) use the top-level fields as before.
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

		// Pick the source of truth: active impl if resolver set one, else legacy.
		implTools := pkg.Tools
		implServers := pkg.MCPServers
		if pkg.ActiveImpl != nil {
			implTools = pkg.ActiveImpl.Tools
			implServers = pkg.ActiveImpl.MCPServers
		}

		for _, t := range implTools {
			if !seenTools[t] {
				seenTools[t] = true
				tools = append(tools, t)
			}
		}
		for _, s := range implServers {
			if !seenServers[s] {
				seenServers[s] = true
				mcpServers = append(mcpServers, s)
			}
		}
	}
	return tools, mcpServers
}

// LoadAgentRoles reads workers from config/workers/.
// Auto-reloads when files change on disk.
func LoadAgentRoles() map[string]*AgentRole {
	curDir := FindKernelConfigDir()

	agentMu.RLock()
	if agentRoles != nil && agentConfigDir == curDir && !dirChanged("workers", agentModTime) {
		roles := agentRoles
		agentMu.RUnlock()
		return roles
	}
	agentMu.RUnlock()

	agentMu.Lock()
	defer agentMu.Unlock()

	curDir = FindKernelConfigDir()
	if agentRoles != nil && agentConfigDir == curDir && !dirChanged("workers", agentModTime) {
		return agentRoles
	}

	configDir := curDir

	// Load workers (flat YAML format)
	workersDir := "config/workers"
	if configDir != "" {
		workersDir = filepath.Join(configDir, "workers")
	}
	agentRoles = LoadWorkersFrom(workersDir)

	agentConfigDir = configDir
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
// Both `imports` and `capabilities` are accepted and merged (they are aliases).
func loadRoleFromFolder(folder string) *AgentRole {
	cfg, err := os.ReadFile(filepath.Join(folder, "config.yaml"))
	if err != nil {
		return nil
	}

	var rc RoleConfig
	if err := yaml.Unmarshal(cfg, &rc); err != nil {
		return nil
	}

	prompt, err := os.ReadFile(filepath.Join(folder, "prompt.md"))
	if err != nil {
		return nil
	}

	maxRounds := rc.MaxRounds
	if maxRounds == 0 {
		maxRounds = 15
	}

	temp := float32(0.4)
	if rc.Temperature != 0 {
		temp = float32(rc.Temperature)
	}

	// imports + capabilities are aliases — merge and expand together
	packages := append([]string{}, rc.Imports...)
	packages = append(packages, rc.Capabilities...)
	tools := rc.Tools
	mcpServers := rc.MCPServers
	if len(packages) > 0 {
		pkgTools, pkgMCP := ResolveImports(packages)
		tools = append(tools, pkgTools...)
		mcpServers = append(mcpServers, pkgMCP...)
	}

	description := rc.Description
	if description == "" {
		description = rc.Persona
	}

	return &AgentRole{
		Description: description,
		Prompt:      string(prompt),
		Tools:       tools,
		MCPServers:  mcpServers,
		Imports:     packages,
		MaxRounds:   maxRounds,
		Temperature: temp,
		Model:       rc.Model,
		Thinking:    rc.Thinking,
		Division:    rc.Division,
		SandboxTier: rc.Sandbox,
		DelegatesTo: rc.DelegatesTo,
		Hooks:       rc.Hooks,
		Skills:      rc.Skills,
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
		var wc RoleConfig
		if err := yaml.Unmarshal(data, &wc); err != nil {
			continue
		}
		// Workers require persona OR capabilities to be identifiable
		if wc.Persona == "" && len(wc.Capabilities) == 0 && wc.Description == "" {
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

		// imports + capabilities both expand; merge so workers can use either
		packages := append([]string{}, wc.Capabilities...)
		packages = append(packages, wc.Imports...)
		var tools, mcpServers []string
		if len(packages) > 0 {
			resolvedTools, resolvedMCP := ResolveImports(packages)
			tools = append(tools, resolvedTools...)
			mcpServers = append(mcpServers, resolvedMCP...)
		}
		// Also allow direct tools/mcp_servers in worker YAML
		tools = append(tools, wc.Tools...)
		mcpServers = append(mcpServers, wc.MCPServers...)

		// Persona is the canonical worker identity; fall back to description
		// so legacy-style worker YAMLs still produce a usable prompt.
		persona := wc.Persona
		if persona == "" {
			persona = wc.Description
		}

		// Build prompt from persona + capability skills
		prompt := BuildWorkerPrompt(persona, packages)

		// Determine sandbox tier: worker override > highest capability requirement
		sandboxTier := wc.Sandbox
		if sandboxTier == "" {
			sandboxTier = highestSandboxTier(packages)
		}

		roles[name] = &AgentRole{
			Name:         name,
			Hint:         wc.Hint,
			Description:  persona,
			Prompt:       prompt,
			Tools:        tools,
			MCPServers:   mcpServers,
			Capabilities: wc.Capabilities,
			Imports:      packages, // for FormatAgentList compat
			MaxRounds:    maxRounds,
			Temperature:  temp,
			Model:        wc.Model,
			Thinking:     wc.Thinking,
			Division:     wc.Division,
			SandboxTier:  sandboxTier,
			DelegatesTo:  wc.DelegatesTo,
			Hooks:        wc.Hooks,
			Skills:       wc.Skills,
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
		return "No roles loaded from config/workers/"
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
		hint := role.Hint
		if hint == "" {
			hint = role.Description // backwards compat
		}
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
		fmt.Fprintf(&b, "### %s\n%s\nCapabilities: %s\n\n", role.Name, hint, capability)
	}
	return b.String()
}

// KernelWorkerNames returns the set of kernel worker names that are immutable.
// Used by autoconfig to reject creates/updates that collide with kernel workers (Contract 3.5).
func KernelWorkerNames() map[string]bool {
	names := make(map[string]bool)
	for name := range LoadAgentRoles() {
		names[name] = true
	}
	return names
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
	// Kernel workers (browser_ops, desktop_ops, etc.) are immutable. Orgs can only ADD
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
2. Use delegate_to with employee role names (browser_ops, researcher, shell_ops, code_ops, vision_ops, desktop_ops)
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
