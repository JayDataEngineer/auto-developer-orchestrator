// Package manifest reads and parses pux.yaml project manifests.
//
// The manifest declares what an app IS, what it NEEDS from the OS,
// and what it PROVIDES to the OS. The orchestrator reads it on
// project registration — apps write it, OS reads it.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PuxManifest is the top-level structure parsed from pux.yaml.
type PuxManifest struct {
	Name        string               `yaml:"name" json:"name"`
	Version     string               `yaml:"version" json:"version"`
	Description string               `yaml:"description" json:"description"`
	Tools       []ToolDef            `yaml:"tools" json:"tools,omitempty"`
	Requires    []string             `yaml:"requires" json:"requires,omitempty"`
	Prompts     map[string]PromptDef `yaml:"prompts" json:"prompts,omitempty"`
	Schedule    map[string]ScheduleDef `yaml:"schedule" json:"schedule,omitempty"`
	Sandbox     *SandboxConfig       `yaml:"sandbox" json:"sandbox,omitempty"`
	Commands    map[string]CommandDef `yaml:"commands" json:"commands,omitempty"`
}

// ToolDef describes a tool the app exposes to the OS.
type ToolDef struct {
	Name        string `yaml:"name" json:"name"`
	Handler     string `yaml:"handler" json:"handler"`
	Description string `yaml:"description" json:"description"`
}

// PromptDef is a prompt template — either inline text or a file reference.
// Exactly one of File or Text should be set.
type PromptDef struct {
	File string `yaml:"file,omitempty" json:"file,omitempty"`
	Text string `yaml:"text,omitempty" json:"text,omitempty"`
}

// ScheduleDef describes a cron job the app wants registered.
type ScheduleDef struct {
	Cron        string `yaml:"cron" json:"cron"`
	Prompt      string `yaml:"prompt" json:"prompt"`
	Description string `yaml:"description" json:"description"`
	Model       string `yaml:"model,omitempty" json:"model,omitempty"`
	Enabled     *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// SandboxConfig describes sandbox initialization requirements.
type SandboxConfig struct {
	InitFiles   []string          `yaml:"init_files" json:"init_files,omitempty"`
	PipPackages []string          `yaml:"pip_packages" json:"pip_packages,omitempty"`
	Env         map[string]string `yaml:"env" json:"env,omitempty"`
}

// CommandDef describes a named CLI/TUI operation the app exposes.
type CommandDef struct {
	Description string `yaml:"description" json:"description"`
	Exec        string `yaml:"exec" json:"exec"`
}

// LoadManifest reads and parses pux.yaml from the given project directory.
// Returns nil without error if no manifest file exists.
func LoadManifest(projectDir string) (*PuxManifest, error) {
	path := filepath.Join(projectDir, "pux.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pux.yaml: %w", err)
	}

	var m PuxManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse pux.yaml: %w", err)
	}

	if m.Name == "" {
		return nil, fmt.Errorf("pux.yaml: name is required")
	}

	return &m, nil
}

// Validate checks cross-field invariants on the sandbox config's env block.
// Catches the "SURREALDB_URL set but SURREALDB_NS missing" class of typo
// that silently breaks every surreal_client.py call at runtime.
//
// Returns a list of errors; an empty list means the config is valid.
func (s *SandboxConfig) Validate() []string {
	if s == nil {
		return nil
	}
	var errs []string

	// SurrealDB envs come as a group — any one of URL/NS/DB set implies
	// the other two should be set too. USER/PASS default to "root"/"root"
	// in surreal_client.py, so we don't require them.
	sbURL, sbNS, sbDB := s.Env["SURREALDB_URL"], s.Env["SURREALDB_NS"], s.Env["SURREALDB_DB"]
	sbAny := sbURL != "" || sbNS != "" || sbDB != ""
	if sbAny {
		if sbURL == "" {
			errs = append(errs, "sandbox.env: SURREALDB_NS/DB set but SURREALDB_URL is empty")
		}
		if sbNS == "" {
			errs = append(errs, "sandbox.env: SURREALDB_URL set but SURREALDB_NS is empty (surreal_client.py needs this)")
		}
		if sbDB == "" {
			errs = append(errs, "sandbox.env: SURREALDB_URL set but SURREALDB_DB is empty (surreal_client.py needs this)")
		}
	}

	return errs
}

// ResolvePrompt returns the prompt text for the given prompt name.
// If the prompt uses a file reference, it reads the file relative to projectDir.
func (m *PuxManifest) ResolvePrompt(projectDir, promptName string) (string, error) {
	pd, ok := m.Prompts[promptName]
	if !ok {
		return "", fmt.Errorf("prompt %q not found in manifest", promptName)
	}

	if pd.File != "" {
		path := filepath.Join(projectDir, pd.File)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read prompt file %s: %w", pd.File, err)
		}
		return string(data), nil
	}

	return pd.Text, nil
}

// ResolveAllPrompts resolves all prompt templates, returning a map of name→text.
// File references are read from projectDir. Errors are collected per-prompt.
func (m *PuxManifest) ResolveAllPrompts(projectDir string) (map[string]string, map[string]string) {
	resolved := make(map[string]string, len(m.Prompts))
	errors := make(map[string]string)

	for name := range m.Prompts {
		text, err := m.ResolvePrompt(projectDir, name)
		if err != nil {
			errors[name] = err.Error()
			continue
		}
		resolved[name] = text
	}

	return resolved, errors
}

// ScheduleCount returns the number of schedule entries.
func (m *PuxManifest) ScheduleCount() int {
	return len(m.Schedule)
}

// ToolCount returns the number of exposed tools.
func (m *PuxManifest) ToolCount() int {
	return len(m.Tools)
}

// CommandNames returns a sorted list of command names.
func (m *PuxManifest) CommandNames() []string {
	names := make([]string, 0, len(m.Commands))
	for name := range m.Commands {
		names = append(names, name)
	}
	return names
}

// Brief returns a human-readable summary of the manifest for first-run output.
func (m *PuxManifest) Brief() string {
	msg := fmt.Sprintf("Project '%s' registered", m.Name)
	if m.Version != "" {
		msg += fmt.Sprintf(" (v%s)", m.Version)
	}

	parts := []string{}
	if n := m.ToolCount(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d tool(s)", n))
	}
	if n := m.ScheduleCount(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d schedule(s)", n))
	}
	if n := len(m.Commands); n > 0 {
		parts = append(parts, fmt.Sprintf("%d command(s)", n))
	}
	if n := len(m.Requires); n > 0 {
		parts = append(parts, fmt.Sprintf("%d requirement(s)", n))
	}

	if len(parts) > 0 {
		msg += ". " + joinParts(parts)
	}

	return msg + "."
}

func joinParts(parts []string) string {
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		if i == len(parts)-1 {
			result += " and " + parts[i]
		} else {
			result += ", " + parts[i]
		}
	}
	return result
}
