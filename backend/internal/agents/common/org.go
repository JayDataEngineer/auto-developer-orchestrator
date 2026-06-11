package common

import (
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
	URL         string `yaml:"url"`          // for postgres
	BaseURL     string `yaml:"base_url"`     // for compreface
	APIKeyEnv   string `yaml:"api_key_env"` // for compreface
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
	Schedules     []OrgSchedule             `yaml:"schedules"`
	Databases     map[string]DatabaseConfig `yaml:"databases"`

	baseDir string // absolute path to the directory containing pux.yaml
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
