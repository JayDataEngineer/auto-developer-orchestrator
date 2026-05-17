package common

import (
	"fmt"
	"os"
	"path/filepath"
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
// Enables future API-level prompt caching — everything before this marker
// can use cache_scope: global; everything after is session-specific.
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
}

// PromptBuilder assembles the CTO system prompt from cached sections.
type PromptBuilder struct {
	sections  []Section
	cache     map[string]string
	modTimes  map[string]time.Time
	mu        sync.RWMutex
	configDir string
}

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

		// Insert boundary before first non-stable section
		if !boundaryInserted && s.Level != Stable {
			parts = append(parts, DynamicBoundary)
			boundaryInserted = true
		}

		parts = append(parts, content)
	}

	// If org manifesto exists, prepend it before everything
	if ctx.Org != nil {
		var header strings.Builder
		fmt.Fprintf(&header, "# Organization: %s\n%s\n\n", ctx.Org.Name, ctx.Org.Description)
		if manifesto := ctx.Org.ManifestoContent(); manifesto != "" {
			fmt.Fprintf(&header, "## Manifesto\n%s\n\n", manifesto)
		}
		return header.String() + strings.Join(parts, "\n\n")
	}

	return strings.Join(parts, "\n\n")
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
		// Can't stat — return cached value or empty
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

// getInherited renders an inherited section, checking if config has changed.
func (b *PromptBuilder) getInherited(s Section, ctx *PromptContext) string {
	// For inherited sections, check if workers/capabilities dir changed
	configDir := FindKernelConfigDir()
	workersDir := "config/workers"
	if configDir != "" {
		workersDir = filepath.Join(configDir, "workers")
	}

	key := s.Name + "_inherited"

	b.mu.RLock()
	cached, ok := b.cache[key]
	modTime := b.modTimes[key]
	b.mu.RUnlock()

	// Check workers dir modTime
	if dirInfo, err := os.Stat(workersDir); err == nil {
		if ok && !dirInfo.ModTime().After(modTime) {
			return cached // cache hit
		}
	}

	// Recompute
	content := s.Render(ctx)

	b.mu.Lock()
	b.cache[key] = content
	if dirInfo, err := os.Stat(workersDir); err == nil {
		b.modTimes[key] = dirInfo.ModTime()
	} else {
		b.modTimes[key] = time.Now()
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
	// MCP instructions are handled by the MCP server instructions injected
	// via the skill system or attached separately. This section is a hook
	// for future MCP instruction injection.
	return ""
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

// --- Public entry point ---

// BuildOrchestratorPromptV2 builds the CTO system prompt using the section pipeline
// if config/prompt_sections/ exists, otherwise falls back to the legacy template.
func BuildOrchestratorPromptV2(tools []core.Tool, sandboxID, projectContext, skills string, org *OrgManifest, orgRoles map[string]*AgentRole) string {
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

	builder := NewPromptBuilder(configDir)
	ctx := &PromptContext{
		Tools:          tools,
		SandboxID:      sandboxID,
		ProjectContext: projectContext,
		Skills:         skills,
		Org:            org,
		OrgRoles:       orgRoles,
		KernelRoles:    LoadAgentRoles(),
	}

	return builder.Build(ctx)
}
