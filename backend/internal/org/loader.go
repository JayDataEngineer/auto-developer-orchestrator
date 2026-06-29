package org

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/pelletier/go-toml/v2"
)

// Loader discovers orgs under a project's orgs/ directory and parses them
// into in-memory Org values. Re-reads the filesystem on every call — cheap
// for the small directory counts we expect (single-tenant localhost).
type Loader struct {
	root string // absolute path to <project>/orgs/
}

// NewLoader binds a loader to the orgs directory under projectRoot.
// The directory is allowed to not exist yet — LoadAll returns an empty slice
// + nil error in that case, so list_orgs works on a fresh project.
func NewLoader(projectRoot string) *Loader {
	return &Loader{root: filepath.Join(projectRoot, "orgs")}
}

// LoadAll parses every org under root. Returns one Org per <name>/org.toml
// found. Orgs whose org.toml is missing or malformed are skipped — the error
// is reported in the returned slice as a Degraded entry so a single broken
// org doesn't break list_orgs.
//
// The returned slice is sorted by Org.Name for stable list_orgs output.
func (l *Loader) LoadAll() ([]Org, error) {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read orgs dir %q: %w", l.root, err)
	}

	var orgs []Org
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		// Skip hidden directories (e.g. editor temp files). Dot-prefixed only.
		// Underscore-prefixed names are allowed (the _demo template uses one).
		name := ent.Name()
		if name[0] == '.' {
			continue
		}
		orgDir := filepath.Join(l.root, name)
		org, err := LoadOne(orgDir)
		if err != nil {
			// Skip broken orgs — list_orgs shouldn't fail because one is half-written.
			continue
		}
		orgs = append(orgs, *org)
	}
	sort.Slice(orgs, func(i, j int) bool { return orgs[i].Name < orgs[j].Name })
	return orgs, nil
}

// LoadByName loads a single org by name. Returns an error including the org
// name if the directory or org.toml is missing — callers should surface this
// to the MCP client as a tool error (not a Go panic).
func (l *Loader) LoadByName(name string) (*Org, error) {
	if name == "" {
		return nil, fmt.Errorf("org: empty name")
	}
	orgDir := filepath.Join(l.root, name)
	return LoadOne(orgDir)
}

// LoadOne parses an org from a specific directory. Exposed so tests can point
// at fixture dirs without spinning up a Loader.
func LoadOne(orgDir string) (*Org, error) {
	tomlPath := filepath.Join(orgDir, "org.toml")
	raw, err := os.ReadFile(tomlPath)
	if err != nil {
		return nil, fmt.Errorf("read org.toml in %q: %w", orgDir, err)
	}

	// Parse into an intermediate shape, then resolve prompt files.
	var cfg tomlConfig
	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse org.toml in %q: %w", orgDir, err)
	}

	org := &Org{
		Name:         cfg.Name,
		Description:  cfg.Description,
		Dir:          orgDir,
		SandboxImage: cfg.SandboxImage,
		SandboxEnv:   cfg.SandboxEnv,
		Roles:        make(map[string]Role),
	}

	// CTO is required.
	ctoPrompt, err := readPromptFile(orgDir, cfg.CTO.Prompt)
	if err != nil {
		return nil, fmt.Errorf("load cto prompt: %w", err)
	}
	org.CTO = Role{
		Name:      "cto",
		Prompt:    ctoPrompt,
		MaxRounds: cfg.CTO.MaxRounds,
		Tools:     cfg.CTO.Tools,
		Model:     cfg.CTO.Model,
	}

	// Roles are optional. Each [[roles]] entry must have name + prompt path.
	for _, rc := range cfg.Roles {
		if rc.Name == "" {
			return nil, fmt.Errorf("org %q: [[roles]] entry missing name", cfg.Name)
		}
		prompt, err := readPromptFile(orgDir, rc.Prompt)
		if err != nil {
			return nil, fmt.Errorf("load role %q prompt: %w", rc.Name, err)
		}
		if _, dup := org.Roles[rc.Name]; dup {
			return nil, fmt.Errorf("org %q: duplicate role %q", cfg.Name, rc.Name)
		}
		org.Roles[rc.Name] = Role{
			Name:      rc.Name,
			Prompt:    prompt,
			MaxRounds: rc.MaxRounds,
			Tools:     rc.Tools,
			Model:     rc.Model,
		}
	}

	if err := org.Validate(); err != nil {
		return nil, err
	}
	return org, nil
}

func readPromptFile(orgDir, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("prompt path is empty")
	}
	full := AbsPromptPath(orgDir, rel)
	body, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("read prompt %q: %w", rel, err)
	}
	return string(body), nil
}

// ── TOML schema ─────────────────────────────────────────────────────

// tomlConfig mirrors org.toml. Field names use snake_case via the toml tag
// to match the human-written format. Unknown fields are ignored.
type tomlConfig struct {
	Name         string          `toml:"name"`
	Description  string          `toml:"description"`
	SandboxImage string          `toml:"sandbox_image"`
	SandboxEnv   []string        `toml:"sandbox_env"`
	CTO          tomlRoleBlock   `toml:"cto"`
	Roles        []tomlRoleBlock `toml:"roles"`
}

// tomlRoleBlock is the shape of both [cto] and [[roles]] entries.
type tomlRoleBlock struct {
	Name      string   `toml:"name"`
	Prompt    string   `toml:"prompt"`
	MaxRounds int      `toml:"max_rounds"`
	Tools     []string `toml:"tools"`
	Model     string   `toml:"model"`
}
