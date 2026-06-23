package common

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Stability controls how a prompt section is cached.
type Stability int

const (
	Stable    Stability = iota // cached globally, loaded from config/prompt_sections/*.md
	Inherited                   // cached per-config-change (employee roster, tool list)
	Volatile                    // never cached (skills, sandbox ID, project context)
)

// DynamicBoundary is inserted between stable and inherited sections.
// Enables API-level prompt caching — everything before this marker
// can use cache_control: ephemeral (Anthropic) or cachedContents (Gemini).
const DynamicBoundary = "<system_prompt_dynamic_boundary>"

// Section defines one piece of the system prompt.
type Section struct {
	Name   string
	Level  Stability
	Render func(ctx *PromptContext) string // produces the content
	File   string                          // for Stable sections: filename in config/prompt_sections/
}

// PromptContext holds all dynamic data needed to build a prompt.
type PromptContext struct {
	Tools          []core.Tool
	SandboxID      string
	ProjectContext string
	Skills         string
	Org            *OrgManifest
	OrgRoles       map[string]*AgentRole
	KernelRoles    map[string]*AgentRole
	MCPInstructions map[string]string // server prefix → instruction text
	Sandboxed      bool // true when CTO runs inside Docker sandbox — affects path rendering
}

// PromptBuilder assembles the CTO system prompt from cached sections.
// Intended to be a long-lived singleton — the cache persists across calls.
type PromptBuilder struct {
	sections  []Section
	cache     map[string]string
	modTimes  map[string]time.Time
	mu        sync.RWMutex
	configDir string
}

// globalBuilder is the singleton PromptBuilder, created on first use.
var (
	globalBuilder     *PromptBuilder
	globalBuilderOnce sync.Once
)

// NewPromptBuilder creates a builder with all registered sections in order.
func NewPromptBuilder(configDir string) *PromptBuilder {
	b := &PromptBuilder{
		cache:     make(map[string]string),
		modTimes:  make(map[string]time.Time),
		configDir: configDir,
	}

	b.sections = []Section{
		// --- Stable sections (loaded from files, cached globally) ---
		{Name: "identity", Level: Stable, File: "identity.md"},
		{Name: "system", Level: Stable, File: "system.md"},
		{Name: "delegation", Level: Stable, File: "delegation.md"},
		{Name: "communication", Level: Stable, File: "communication.md"},
		{Name: "actions", Level: Stable, File: "actions.md"},
		{Name: "planning", Level: Stable, File: "planning.md"},
		{Name: "artifacts", Level: Stable, File: "artifacts.md"},
		{Name: "scripts", Level: Stable, File: "scripts.md"},
		{Name: "paths", Level: Stable, File: "paths.md"},

		// --- Inherited sections (cached per-config-change) ---
		{Name: "employees", Level: Inherited, Render: b.renderEmployees},
		{Name: "tools", Level: Inherited, Render: b.renderTools},
		{Name: "mcp", Level: Inherited, Render: b.renderMCP},

		// --- Volatile sections (never cached) ---
		{Name: "skills", Level: Volatile, Render: b.renderSkills},
		{Name: "project_context", Level: Volatile, Render: b.renderProjectContext},
		{Name: "sandbox_id", Level: Volatile, Render: b.renderSandboxID},
	}

	return b
}

// Build assembles the full system prompt from all sections.
func (b *PromptBuilder) Build(ctx *PromptContext) string {
	var parts []string
	boundaryInserted := false

	for _, s := range b.sections {
		var content string

		switch s.Level {
		case Stable:
			content = b.getStable(s)
		case Inherited:
			content = b.getInherited(s, ctx)
		case Volatile:
			content = s.Render(ctx)
		}

		if content == "" {
			continue
		}

		// Insert boundary before first non-stable section.
		// Org manifesto goes AFTER the boundary so stable sections
		// remain cacheable regardless of which org is active.
		if !boundaryInserted && s.Level != Stable {
			parts = append(parts, DynamicBoundary)
			boundaryInserted = true
			// Org header is the first dynamic section
			if ctx.Org != nil {
				parts = append(parts, b.renderOrgManifesto(ctx))
			}
		}

		parts = append(parts, content)
	}

	// No non-stable content — insert boundary + org at the end
	if !boundaryInserted && ctx.Org != nil {
		parts = append(parts, DynamicBoundary)
		parts = append(parts, b.renderOrgManifesto(ctx))
	}

	return strings.Join(parts, "\n\n")
}

// renderOrgManifesto produces the org header section.
func (b *PromptBuilder) renderOrgManifesto(ctx *PromptContext) string {
	org := ctx.Org
	var header strings.Builder
	fmt.Fprintf(&header, "# Organization: %s\n%s", org.Name, org.Description)
	if manifesto := org.ManifestoContent(); manifesto != "" {
		fmt.Fprintf(&header, "\n\n## Manifesto\n%s", manifesto)
	}
	dataDir := org.DataDirPath()
	if dataDir != "" {
		// When the CTO runs inside the sandbox, render the container-relative path
		// instead of the host path. The org workspace is mounted at /sandbox/workspace/
		// so the host path `<orgDir>/data/` becomes `/sandbox/workspace/data/` inside.
		renderedPath := dataDir
		if ctx.Sandboxed && org.baseDir != "" {
			renderedPath = "/sandbox/workspace/" + relPathFromBase(org.baseDir, dataDir)
		}
		fmt.Fprintf(&header, "\n\n## Org Data Directory\nInput data lives at: `%s`\n"+
			"When the user references data, a corpus, an export, or a dump WITHOUT a path, "+
			"look here FIRST before asking them where it is. List what's there and pick the "+
			"most recent / most relevant entry.", renderedPath)
	}
	return header.String()
}

// relPathFromBase strips the base dir prefix from path, returning a relative path.
// If path isn't under base, returns the original path.
func relPathFromBase(base, path string) string {
	if !strings.HasPrefix(path, base+"/") && path != base {
		return path
	}
	rel := strings.TrimPrefix(path, base)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "."
	}
	return rel
}

// getStable loads a stable section from file, using cache if unchanged.
func (b *PromptBuilder) getStable(s Section) string {
	b.mu.RLock()
	cached, ok := b.cache[s.Name]
	modTime := b.modTimes[s.Name]
	b.mu.RUnlock()

	// Check if file changed
	path := b.sectionPath(s.File)
	info, err := os.Stat(path)
	if err != nil {
		if ok {
			return cached
		}
		return ""
	}

	if ok && !info.ModTime().After(modTime) {
		return cached // cache hit
	}

	// Load from file
	data, err := os.ReadFile(path)
	if err != nil {
		if ok {
			return cached // keep stale cache on read error
		}
		return ""
	}

	content := string(data)
	b.mu.Lock()
	b.cache[s.Name] = content
	b.modTimes[s.Name] = info.ModTime()
	b.mu.Unlock()

	return content
}

// inheritedCacheKey computes a cache key for inherited sections that includes
// tool names and config dir modTimes. This ensures the cache invalidates when
// tools change at runtime (MCP extension connects) or capabilities are edited.
func (b *PromptBuilder) inheritedCacheKey(s Section, ctx *PromptContext) string {
	// Start with section name
	key := s.Name + "|"

	// Include tool names — if new MCP tools appear, the key changes
	if ctx.Tools != nil {
		names := make([]string, len(ctx.Tools))
		for i, t := range ctx.Tools {
			names[i] = t.Name()
		}
		sort.Strings(names)
		key += strings.Join(names, ",")
	}
	key += "|"

	// Include MCP instruction keys — if a new server connects, key changes
	if ctx.MCPInstructions != nil {
		prefixes := make([]string, 0, len(ctx.MCPInstructions))
		for p := range ctx.MCPInstructions {
			prefixes = append(prefixes, p)
		}
		sort.Strings(prefixes)
		key += strings.Join(prefixes, ",")
	}

	return key
}

// getInherited renders an inherited section, checking if config has changed.
func (b *PromptBuilder) getInherited(s Section, ctx *PromptContext) string {
	cacheKey := b.inheritedCacheKey(s, ctx) + "_inherited"

	// Collect modTimes from workers + capabilities dirs
	configDir := FindKernelConfigDir()
	workersDir := "config/workers"
	capabilitiesDir := "config/capabilities"
	if configDir != "" {
		workersDir = filepath.Join(configDir, "workers")
		capabilitiesDir = filepath.Join(configDir, "capabilities")
	}

	b.mu.RLock()
	cached, ok := b.cache[cacheKey]
	cacheTime := b.modTimes[cacheKey]
	b.mu.RUnlock()

	// Find the latest modTime across both dirs
	latestMod := time.Time{}
	for _, dir := range []string{workersDir, capabilitiesDir} {
		if info, err := os.Stat(dir); err == nil {
			if info.ModTime().After(latestMod) {
				latestMod = info.ModTime()
			}
		}
	}

	// Cache hit if key matches and dirs haven't changed
	if ok && !latestMod.After(cacheTime) {
		return cached
	}

	// Recompute
	content := s.Render(ctx)

	b.mu.Lock()
	b.cache[cacheKey] = content
	if !latestMod.IsZero() {
		b.modTimes[cacheKey] = latestMod
	} else {
		b.modTimes[cacheKey] = time.Now()
	}
	b.mu.Unlock()

	return content
}

func (b *PromptBuilder) sectionPath(filename string) string {
	if b.configDir != "" {
		return filepath.Join(b.configDir, "prompt_sections", filename)
	}
	return filepath.Join("config", "prompt_sections", filename)
}

// --- Inherited section renderers ---

func (b *PromptBuilder) renderEmployees(ctx *PromptContext) string {
	// Merge kernel + org roles
	merged := make(map[string]*AgentRole)
	if ctx.KernelRoles != nil {
		for k, v := range ctx.KernelRoles {
			merged[k] = v
		}
	}
	if ctx.OrgRoles != nil {
		for k, v := range ctx.OrgRoles {
			if _, isKernel := merged[k]; isKernel {
				continue // kernel roles are immutable
			}
			merged[k] = v
		}
	}
	if len(merged) == 0 {
		merged = LoadAgentRoles()
	}

	return "# Employees\n\n" + formatRolesList(merged)
}

func (b *PromptBuilder) renderTools(ctx *PromptContext) string {
	if len(ctx.Tools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Available Tools\n\n")
	for _, t := range ctx.Tools {
		schema := formatSchema(t.Schema())
		fmt.Fprintf(&sb, "## %s — %s\n%s\n\n", t.Name(), t.Description(), schema)
	}
	return sb.String()
}

func (b *PromptBuilder) renderMCP(ctx *PromptContext) string {
	if len(ctx.MCPInstructions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# MCP Server Instructions\n\n")
	for prefix, instructions := range ctx.MCPInstructions {
		fmt.Fprintf(&sb, "## %s\n%s\n\n", prefix, instructions)
	}
	return sb.String()
}

// --- Volatile section renderers ---

func (b *PromptBuilder) renderSkills(ctx *PromptContext) string {
	if ctx.Skills == "" {
		return ""
	}
	return "# Skills\n\n" + ctx.Skills
}

func (b *PromptBuilder) renderProjectContext(ctx *PromptContext) string {
	if ctx.ProjectContext == "" {
		return ""
	}
	return "# Project Context\n\n" + ctx.ProjectContext
}

func (b *PromptBuilder) renderSandboxID(ctx *PromptContext) string {
	if ctx.SandboxID == "" {
		return ""
	}
	return "Sandbox ID: " + ctx.SandboxID
}

// --- MCP instruction registry ---

// mcpInstructionStore holds MCP server instruction text, populated by the MCP
// package during initialization. The prompt builder reads from here — no circular import.
var mcpInstructionStore struct {
	mu           sync.RWMutex
	instructions map[string]string // server prefix → instruction text
}

// RegisterMCPInstructions stores instruction text for an MCP server.
// Called by the MCP package after initializing each server.
func RegisterMCPInstructions(prefix, instructions string) {
	mcpInstructionStore.mu.Lock()
	defer mcpInstructionStore.mu.Unlock()
	if mcpInstructionStore.instructions == nil {
		mcpInstructionStore.instructions = make(map[string]string)
	}
	if instructions != "" {
		mcpInstructionStore.instructions[prefix] = instructions
	}
}

// MCPInstructionsMap returns a snapshot of all registered MCP instructions.
func MCPInstructionsMap() map[string]string {
	mcpInstructionStore.mu.RLock()
	defer mcpInstructionStore.mu.RUnlock()
	out := make(map[string]string, len(mcpInstructionStore.instructions))
	for k, v := range mcpInstructionStore.instructions {
		out[k] = v
	}
	return out
}

// --- Public entry points ---

// ResetGlobalBuilder resets the singleton PromptBuilder.
// Called when config changes require a fresh builder (e.g. configDir changes).
func ResetGlobalBuilder() {
	globalBuilderOnce = sync.Once{}
	globalBuilder = nil
}

// BuildOrchestratorPromptV2 builds the CTO system prompt using the section pipeline
// if config/prompt_sections/ exists, otherwise falls back to the legacy template.
// Uses a singleton PromptBuilder so the cache persists across calls.
func BuildOrchestratorPromptV2(tools []core.Tool, sandboxID, projectContext, skills string, org *OrgManifest, orgRoles map[string]*AgentRole) string {
	return BuildOrchestratorPromptV2WithCtx(tools, sandboxID, projectContext, skills, org, orgRoles, false)
}

// BuildOrchestratorPromptV2WithCtx is like BuildOrchestratorPromptV2 but accepts
// a sandboxed flag. When true, paths in the org manifesto render container-relative
// (e.g. /sandbox/workspace/data/ instead of /home/.../org/data/) because the CTO
// runs inside the sandbox container.
func BuildOrchestratorPromptV2WithCtx(tools []core.Tool, sandboxID, projectContext, skills string, org *OrgManifest, orgRoles map[string]*AgentRole, sandboxed bool) string {
	configDir := FindKernelConfigDir()

	// Check if section pipeline is available
	sectionsDir := "config/prompt_sections"
	if configDir != "" {
		sectionsDir = filepath.Join(configDir, "prompt_sections")
	}
	if _, err := os.Stat(sectionsDir); os.IsNotExist(err) {
		// No section files — fall back to legacy
		return BuildOrchestratorPromptWithOrg(tools, sandboxID, projectContext, skills, org, orgRoles)
	}

	// Use singleton builder so cache persists across calls
	globalBuilderOnce.Do(func() {
		globalBuilder = NewPromptBuilder(configDir)
	})

	ctx := &PromptContext{
		Tools:           tools,
		SandboxID:       sandboxID,
		ProjectContext:  projectContext,
		Skills:          skills,
		Org:             org,
		OrgRoles:        orgRoles,
		KernelRoles:     LoadAgentRoles(),
		MCPInstructions: MCPInstructionsMap(),
		Sandboxed:       sandboxed,
	}

	return globalBuilder.Build(ctx)
}

// BuildOrchestratorPromptV2WithMCP is like BuildOrchestratorPromptV2 but accepts
// explicit MCP instructions. Used when MCP server instructions are known at call time.
func BuildOrchestratorPromptV2WithMCP(tools []core.Tool, sandboxID, projectContext, skills string, org *OrgManifest, orgRoles map[string]*AgentRole, mcpInstructions map[string]string) string {
	configDir := FindKernelConfigDir()

	sectionsDir := "config/prompt_sections"
	if configDir != "" {
		sectionsDir = filepath.Join(configDir, "prompt_sections")
	}
	if _, err := os.Stat(sectionsDir); os.IsNotExist(err) {
		return BuildOrchestratorPromptWithOrg(tools, sandboxID, projectContext, skills, org, orgRoles)
	}

	globalBuilderOnce.Do(func() {
		globalBuilder = NewPromptBuilder(configDir)
	})

	ctx := &PromptContext{
		Tools:           tools,
		SandboxID:       sandboxID,
		ProjectContext:  projectContext,
		Skills:          skills,
		Org:             org,
		OrgRoles:        orgRoles,
		KernelRoles:     LoadAgentRoles(),
		MCPInstructions: mcpInstructions,
	}

	return globalBuilder.Build(ctx)
}
