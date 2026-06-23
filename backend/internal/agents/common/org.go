package common

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DatabaseConfig holds connection configuration for a database.
type DatabaseConfig struct {
	URI         string `yaml:"uri"`          // for neo4j
	Username    string `yaml:"username"`     // for neo4j
	Password    string `yaml:"password"`     // for neo4j
	PasswordEnv string `yaml:"password_env"` // env var name for password
	URL         string `yaml:"url"`          // for postgres / surrealdb
	BaseURL     string `yaml:"base_url"`     // for compreface
	APIKeyEnv   string `yaml:"api_key_env"`  // for compreface
	Namespace   string `yaml:"namespace"`    // for surrealdb
	Database    string `yaml:"database"`     // for surrealdb
}

// OrgManifest is the parsed pux.yaml — the "Corporate Charter" for an organization.
// When a project directory contains a pux.yaml, the kernel enters "org mode" and
// loads org-specific roles, tool packages, manifesto, and schedules.
type OrgManifest struct {
	Name          string                    `yaml:"name"`
	Description   string                    `yaml:"description"`
	Manifesto     string                    `yaml:"manifesto"`
	StaffRoot     string                    `yaml:"staff_root"`
	ToolPkgsRoot  string                    `yaml:"tool_packages_root"`
	ExtensionsDir string                    `yaml:"extensions_dir"` // org-scoped extension servers
	SkillsDir     string                    `yaml:"skills_dir"`     // org-scoped skill definitions
	DataDir       string                    `yaml:"data_dir"`       // where input data lives (Telegram dumps, PDFs, etc.)
	Schedules     []OrgSchedule             `yaml:"schedules"`
	Databases     map[string]DatabaseConfig `yaml:"databases"`
	MCPServers    []OrgMCPServer             `yaml:"mcp_servers"` // org-scoped remote MCP servers, wired at org activation

	baseDir string // absolute path to the directory containing pux.yaml
}

// OrgMCPServer is one row of pux.yaml's mcp_servers: block. The Jinja2
// renderer in scripts/templates/org/pux.yaml.j2 already emits rows in this
// shape (name + endpoint) — this struct just makes the kernel actually read
// them. Before Phase 2 the field was dead config: templated but never parsed.
type OrgMCPServer struct {
	Name     string `yaml:"name"`     // tool prefix (web, media, custom)
	Endpoint string `yaml:"endpoint"` // HTTP/HTTPS URL of the MCP server
}

// OrgSchedule is a cron-based scheduled task within an organization.
type OrgSchedule struct {
	Name    string `yaml:"name"`
	Cron    string `yaml:"cron"`
	Prompt  string `yaml:"prompt"`
	Role    string `yaml:"role"`
	Model   string `yaml:"model"`
	Enabled bool   `yaml:"enabled"`
}

// LoadOrgManifest looks for pux.yaml in the given directory.
// Returns nil if no pux.yaml is found (not an org — normal project mode).
func LoadOrgManifest(projectPath string) *OrgManifest {
	puxPath := filepath.Join(projectPath, "pux.yaml")
	data, err := os.ReadFile(puxPath)
	if err != nil {
		return nil
	}

	var org OrgManifest
	if err := yaml.Unmarshal(data, &org); err != nil {
		return nil
	}

	if org.Name == "" {
		return nil
	}

	absPath, _ := filepath.Abs(projectPath)
	org.baseDir = absPath
	return &org
}

// RolesDir returns the absolute path to the org's roles directory.
// Returns empty string if no staff_root is configured.
func (o *OrgManifest) RolesDir() string {
	if o.StaffRoot == "" {
		return ""
	}
	return o.resolvePath(o.StaffRoot)
}

// ToolPkgsDir returns the absolute path to the org's tool packages directory.
// Returns empty string if no tool_packages_root is configured.
func (o *OrgManifest) ToolPkgsDir() string {
	if o.ToolPkgsRoot == "" {
		return ""
	}
	return o.resolvePath(o.ToolPkgsRoot)
}

// ExtensionsDirPath returns the absolute path to the org's extensions directory.
// Returns empty string if no extensions_dir is configured.
func (o *OrgManifest) ExtensionsDirPath() string {
	if o.ExtensionsDir == "" {
		return ""
	}
	return o.resolvePath(o.ExtensionsDir)
}

// SkillsDirPath returns the absolute path to the org's skills directory.
// Returns empty string if no skills_dir is configured.
func (o *OrgManifest) SkillsDirPath() string {
	if o.SkillsDir == "" {
		return ""
	}
	return o.resolvePath(o.SkillsDir)
}

// DataDirPath returns the absolute path to the org's declared input-data directory.
// Returns empty string if no data_dir is configured.
func (o *OrgManifest) DataDirPath() string {
	if o.DataDir == "" {
		return ""
	}
	return o.resolvePath(o.DataDir)
}

// ManifestoContent reads and returns the org's manifesto markdown.
// Returns empty string if no manifesto is configured or file doesn't exist.
func (o *OrgManifest) ManifestoContent() string {
	if o.Manifesto == "" {
		return ""
	}
	path := o.resolvePath(o.Manifesto)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// PromptContent reads a prompt file referenced in a schedule.
func (o *OrgManifest) PromptContent(promptPath string) (string, error) {
	path := o.resolvePath(promptPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Validate checks cross-field invariants on the parsed manifest.
// Returns a list of error strings; an empty list means the manifest is
// internally consistent.
//
// Checks:
//   - databases.<kind> entries have the kind-specific required fields
//     (surrealdb needs namespace + database; postgres needs url; neo4j
//     needs uri; compreface needs base_url)
//   - sandbox.init_files entries with the "@shared/" prefix resolve to
//     files that actually exist in orgs/_shared/clients/
//
// This runs at audit time, not load time — LoadOrgManifest stays lenient
// so a half-broken manifest can still be inspected.
func (o *OrgManifest) Validate() []string {
	var errs []string

	// Database kind → required fields. Anything declared in the databases
	// map must at least have its anchor field set, otherwise the org's
	// tools will fail at first call.
	for name, db := range o.Databases {
		switch name {
		case "surrealdb":
			if db.URL == "" {
				errs = append(errs, "databases.surrealdb: url is required")
			}
			if db.Namespace == "" {
				errs = append(errs, "databases.surrealdb: namespace is required (surreal_client.py fails without it)")
			}
			if db.Database == "" {
				errs = append(errs, "databases.surrealdb: database is required")
			}
		case "postgres":
			if db.URL == "" {
				errs = append(errs, "databases.postgres: url is required")
			}
		case "neo4j":
			if db.URI == "" {
				errs = append(errs, "databases.neo4j: uri is required")
			}
			if db.Username == "" {
				errs = append(errs, "databases.neo4j: username is required")
			}
		case "compreface":
			if db.BaseURL == "" {
				errs = append(errs, "databases.compreface: base_url is required")
			}
			if db.APIKeyEnv == "" {
				errs = append(errs, "databases.compreface: api_key_env is required")
			}
		}
	}

	// @shared/ init_files must resolve to files that exist on disk. The
	// finder logic lives in FindSharedRoot; if it errors we can't validate,
	// so we skip (the upload path will error at runtime).
	if root, err := FindSharedRoot(); err == nil {
		for _, rel := range o.SandboxInitFiles() {
			if !strings.HasPrefix(rel, "@shared/") {
				continue
			}
			rest := strings.TrimPrefix(rel, "@shared/")
			// Bare filename defaults to clients/ for backward compat.
			if !strings.Contains(rest, "/") {
				rest = filepath.Join("clients", rest)
			}
			path := filepath.Join(root, rest)
			if _, err := os.Stat(path); err != nil {
				errs = append(errs, fmt.Sprintf("sandbox.init_files: %s does not exist under orgs/_shared/ (resolved to %s)", rel, path))
			}
		}
	}

	// sandbox.mode: must be one of the two known values. Unknown values
	// fall back to "contained" at runtime (safe default), but we surface
	// them here so a typo doesn't silently lock down an org that meant
	// to opt into host access.
	switch o.SandboxMode() {
	case SandboxModeContained, SandboxModeHostAccess:
		// ok
	default:
		errs = append(errs, fmt.Sprintf(
			"sandbox.mode: %q must be one of [%q, %q]",
			o.SandboxMode(), SandboxModeContained, SandboxModeHostAccess,
		))
	}

	// mcp_servers: rows must have both name + endpoint. A row missing
	// either field would register a broken client — fail loud at audit
	// time, not silently at first tool call.
	for i, s := range o.MCPServers {
		if s.Name == "" {
			errs = append(errs, fmt.Sprintf("mcp_servers[%d]: name is required", i))
		}
		if s.Endpoint == "" {
			errs = append(errs, fmt.Sprintf("mcp_servers[%d]: endpoint is required", i))
		}
	}

	return errs
}

// Sandbox mode constants. The mode controls whether the CTO runs inside the
// sandbox container (locked to /sandbox/workspace/) or on the host filesystem.
//
// SandboxModeContained (default) — CTO + sub-agents route through the sandbox
// executor. They see only the org workspace; ~/.aws, SSH keys, .env in the
// parent repo, etc. are unreachable. Right for orgs that handle untrusted
// input (invest data, game-dev assets, twitter scrapes) where the agent
// touching host state would be a foot-gun.
//
// SandboxModeHostAccess — CTO + sub-agents run on host bash + host file ops.
// The sandbox container may still be provisioned (for sandbox-tier workers
// that need it), but the CTO itself is not locked in. Right for "coding
// agent" orgs where the whole point is editing files in a real repo.
const (
	SandboxModeContained   = "contained"
	SandboxModeHostAccess  = "host-access"
)

// SandboxInitFiles returns the init_files list from the manifest's sandbox block,
// if any. Loaded lazily so Validate() doesn't require the sandbox block to be
// present.
func (o *OrgManifest) SandboxInitFiles() []string {
	// OrgManifest doesn't parse the sandbox block directly — it lives in the
	// manifest package's PuxManifest. We re-read it here via a minimal struct
	// so Validate() can check @shared/ references without forcing callers to
	// wire both manifest types together.
	type sandboxBlock struct {
		InitFiles []string `yaml:"init_files"`
	}
	type wrapper struct {
		Sandbox *sandboxBlock `yaml:"sandbox"`
	}
	if o.baseDir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(o.baseDir, "pux.yaml"))
	if err != nil {
		return nil
	}
	var w wrapper
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil
	}
	if w.Sandbox == nil {
		return nil
	}
	return w.Sandbox.InitFiles
}

// SandboxMode returns the sandbox.mode field from pux.yaml. Default is
// SandboxModeContained when the field or block is absent. Invalid values
// surface as Validate() errors at audit time; at runtime unknown values
// fall back to contained (safe default — over-isolate rather than leak).
func (o *OrgManifest) SandboxMode() string {
	type sandboxBlock struct {
		Mode string `yaml:"mode"`
	}
	type wrapper struct {
		Sandbox *sandboxBlock `yaml:"sandbox"`
	}
	if o.baseDir == "" {
		return SandboxModeContained
	}
	data, err := os.ReadFile(filepath.Join(o.baseDir, "pux.yaml"))
	if err != nil {
		return SandboxModeContained
	}
	var w wrapper
	if err := yaml.Unmarshal(data, &w); err != nil {
		return SandboxModeContained
	}
	if w.Sandbox == nil || w.Sandbox.Mode == "" {
		return SandboxModeContained
	}
	return w.Sandbox.Mode
}

// HostAccessEnabled is a convenience predicate for callers that just need a
// boolean (e.g. pux_prompt.go deciding whether to flip OrgSandboxed).
func (o *OrgManifest) HostAccessEnabled() bool {
	return o.SandboxMode() == SandboxModeHostAccess
}

func (o *OrgManifest) resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(o.baseDir, p)
}

// PuxHomeDir returns ~/.pux, resolving from HOME or USERPROFILE.
func PuxHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pux")
}

// OrgInfo holds summary info about a discovered org for API responses.
type OrgInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Path        string         `json:"path"`
	Roles       []string       `json:"roles"`
	RolesDir    string         `json:"roles_dir"`
	RoleDetails map[string]any `json:"role_details,omitempty"`
}

// DiscoverOrgs scans ~/.pux/orgs/ for valid organizations.
// Resolves symlinks, loads pux.yaml from each, and returns summary info.
func DiscoverOrgs() []*OrgInfo {
	orgsDir := filepath.Join(PuxHomeDir(), "orgs")
	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		return nil
	}

	var orgs []*OrgInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			// Could be a symlink — stat to check
			info, err := os.Stat(filepath.Join(orgsDir, entry.Name()))
			if err != nil || !info.IsDir() {
				continue
			}
		}

		orgPath := filepath.Join(orgsDir, entry.Name())
		// Resolve symlinks
		if resolved, err := filepath.EvalSymlinks(orgPath); err == nil {
			orgPath = resolved
		}

		org := LoadOrgManifest(orgPath)
		if org == nil {
			continue
		}

		info := &OrgInfo{
			Name:        org.Name,
			Description: org.Description,
			Path:        orgPath,
			RolesDir:    org.RolesDir(),
		}

		// Load roles if staff_root is configured
		if org.RolesDir() != "" {
			roles := LoadAgentRolesFrom(org.RolesDir())
			info.RoleDetails = make(map[string]any, len(roles))
			for name, role := range roles {
				info.Roles = append(info.Roles, name)
				info.RoleDetails[name] = map[string]any{
					"name":         name,
					"hint":         role.Hint,
					"persona":      role.Description,
					"capabilities": role.Capabilities,
					"imports":      role.Imports,
					"model":        role.Model,
					"sandbox":      role.SandboxTier,
					"max_rounds":   role.MaxRounds,
					"temperature":  role.Temperature,
				}
			}
			sort.Strings(info.Roles)
		}

		orgs = append(orgs, info)
	}

	// Sort by name
	sort.Slice(orgs, func(i, j int) bool {
		return orgs[i].Name < orgs[j].Name
	})

	return orgs
}
