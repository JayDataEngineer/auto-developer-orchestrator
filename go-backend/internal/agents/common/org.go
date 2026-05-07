package common

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// OrgManifest is the parsed pux.yaml — the "Corporate Charter" for an organization.
// When a project directory contains a pux.yaml, the kernel enters "org mode" and
// loads org-specific roles, tool packages, manifesto, and schedules.
type OrgManifest struct {
	Name         string       `yaml:"name"`
	Description  string       `yaml:"description"`
	Manifesto    string       `yaml:"manifesto"`
	StaffRoot    string       `yaml:"staff_root"`
	ToolPkgsRoot string       `yaml:"tool_packages_root"`
	Schedules    []OrgSchedule `yaml:"schedules"`

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
