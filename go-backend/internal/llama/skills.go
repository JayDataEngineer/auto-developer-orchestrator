package llama

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ── Skill types ────────────────────────────────────────────────────────────────

// Skill represents a discoverable skill defined by a SKILL.md file.
// Follows pi-mono's Agent Skills standard: YAML frontmatter + markdown body.
// Skills are injected into the system prompt as an <available_skills> list
// so the model can use file_read to load instructions on demand.
type Skill struct {
	Name        string // [a-z0-9-]+, max 64 chars, from frontmatter "name" or dir name
	Description string // required, max 1024 chars
	Location    string // absolute path to SKILL.md file
	Dir         string // parent directory (for resolving relative paths in instructions)
	DisableInvocation bool // if true, skill excluded from system prompt listing
}

// ── Validation ────────────────────────────────────────────────────────────────

var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// validateSkillName checks the pi-mono name rules:
// lowercase a-z, 0-9, hyphens only. No leading/trailing hyphens, no consecutive hyphens.
// Max 64 characters. Returns the name if valid, empty string otherwise.
func validateSkillName(name string, maxLen int) string {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxLen {
		return ""
	}
	if !skillNameRe.MatchString(name) {
		return ""
	}
	return name
}

// parseSkillYAML extracts key-value pairs from simple YAML frontmatter lines.
// Supports: name, description, disable-model-invocation
func parseSkillYAML(frontmatter string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip quotes
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		m[key] = value
	}
	return m
}

// parseSkillFile parses a SKILL.md file with YAML frontmatter.
// Returns nil if validation fails (missing description, etc.).
func parseSkillFile(absPath string) (*Skill, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	content := string(data)

	fm := make(map[string]string)

	if strings.HasPrefix(content, "---\n") {
		// Split frontmatter: first ---, then yaml, then ---, then body
		rest := content[4:] // skip first "---\n"
		endIdx := strings.Index(rest, "\n---")
		if endIdx >= 0 {
			fm = parseSkillYAML(rest[:endIdx])
			// Body is discarded — model uses read_skill to load instructions on demand
		}
	}

	// Validate name
	name := validateSkillName(fm["name"], 64)
	if name == "" {
		// Default: use parent directory name
		dirName := filepath.Base(filepath.Dir(absPath))
		name = validateSkillName(dirName, 64)
	}
	if name == "" {
		return nil, fmt.Errorf("invalid skill name in %s", absPath)
	}

	// Description required (pi-mono standard)
	description := fm["description"]
	if description == "" {
		return nil, fmt.Errorf("skill %s missing required description field", name)
	}
	if len(description) > 1024 {
		description = description[:1021] + "..."
	}

	// disable-model-invocation: if true, the skill is NOT listed in the system prompt
	// but can still be loaded on-demand via read_skill tool
	disableInvocation := strings.ToLower(fm["disable-model-invocation"]) == "true"

	dir := filepath.Dir(absPath)

	return &Skill{
		Name:               name,
		Description:        description,
		Location:           absPath,
		Dir:                dir,
		DisableInvocation:  disableInvocation,
	}, nil
}

// ── Skill Store ────────────────────────────────────────────────────────────────

// SkillStore holds all discovered skills.
// Thread-safe — skills are loaded once at startup and never modified.
type SkillStore struct {
	skills     []Skill
	byName     map[string]*Skill // name → skill, first-wins
	byPath     map[string]*Skill // absPath → skill
}

// NewSkillStore creates an empty skill store.
func NewSkillStore() *SkillStore {
	return &SkillStore{
		byName: make(map[string]*Skill),
		byPath: make(map[string]*Skill),
	}
}

// LoadFromDirs discovers and loads SKILL.md files from the given directories.
// Follows pi-mono discovery rules:
// - SKILL.md at any depth; when found, stop recursing into that directory
// - In root-only directories: also accept *.md files directly (inline skills)
// - Deduplication: first-wins by name
// Returns the number of skills loaded.
func (s *SkillStore) LoadFromDirs(dirs []string) int {
	count := 0
	for _, dir := range dirs {
		count += s.discoverFromDir(dir, false)
	}
	return count
}

// discoverFromDir walks a directory tree to find SKILL.md files.
// pi-mono rules:
// - Find SKILL.md files at any depth
// - When SKILL.md is found in a dir, STOP recursing into that dir
// - Skip: hidden dirs (.), node_modules, vendor, .git
// - Deduplicate by name (first-wins)
// - If includeRootMDs is true, also accept *.md files directly in the root (not subdirs)
func (s *SkillStore) discoverFromDir(dir string, includeRootMDs bool) int {
	if dir == "" {
		return 0
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0
	}

	// Check if directory exists
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return 0
	}

	count := 0
	filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip
		}

		// Directory handling
		if d.IsDir() {
			dirName := d.Name()
			if path == abs {
				return nil // always enter root
			}
			// Skip hidden dirs, node_modules, vendor
			if strings.HasPrefix(dirName, ".") || dirName == "node_modules" || dirName == "vendor" || dirName == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		// File handling
		base := strings.ToLower(d.Name())
		isSkillMD := base == "skill.md"
		isInlineMD := includeRootMDs && filepath.Dir(path) == abs && strings.HasSuffix(base, ".md") && !isSkillMD

		if !isSkillMD && !isInlineMD {
			return nil
		}

		// Parse
		skill, err := parseSkillFile(path)
		if err != nil {
			return nil // silently skip invalid skills
		}

		// Deduplicate by name (first-wins)
		if _, exists := s.byName[skill.Name]; exists {
			return nil
		}

		s.skills = append(s.skills, *skill)
		s.byName[skill.Name] = skill
		s.byPath[path] = skill
		count++

		// SKILL.md found → stop recursing into this directory
		if isSkillMD {
			parent := filepath.Dir(path)
			if parent != abs {
				return filepath.SkipDir
			}
		}

		return nil
	})

	return count
}

// Get returns a skill by name.
func (s *SkillStore) Get(name string) *Skill {
	return s.byName[name]
}

// GetByPath returns a skill by absolute file path.
func (s *SkillStore) GetByPath(path string) *Skill {
	return s.byPath[path]
}

// All returns all loaded skills.
func (s *SkillStore) All() []Skill {
	return s.skills
}

// Count returns the number of loaded skills.
func (s *SkillStore) Count() int {
	return len(s.skills)
}

// ── Prompt formatting ──────────────────────────────────────────────────────────

// FormatAvailableSkills builds the <available_skills> XML block for injection
// into the system prompt. This is the pi-mono pattern:
// - Lists name, description, and location
// - Does NOT include full instructions (model uses file_read to load)
// - Excludes skills with DisableInvocation: true
// Returns empty string if no skills to show.
func (s *SkillStore) FormatAvailableSkills() string {
	var visible []*Skill
	for _, skill := range s.skills {
		if skill.DisableInvocation {
			continue
		}
		visible = append(visible, s.byName[skill.Name])
	}

	if len(visible) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nThe following skills provide specialized instructions for specific tasks.\n")
	b.WriteString("Use the read_skill tool to load a skill's instructions when the task matches its description.\n\n")
	b.WriteString("<available_skills>\n")
	for _, skill := range visible {
		b.WriteString("  <skill>\n")
		b.WriteString(fmt.Sprintf("    <name>%s</name>\n", skill.Name))
		b.WriteString(fmt.Sprintf("    <description>%s</description>\n", skill.Description))
		b.WriteString(fmt.Sprintf("    <location>%s</location>\n", skill.Location))
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// ReadSkill loads the full instructions for a skill by name.
// Returns the markdown body, or empty string if not found.
func (s *SkillStore) ReadSkill(name string) string {
	skill := s.byName[name]
	if skill == nil {
		return ""
	}
	data, err := os.ReadFile(skill.Location)
	if err != nil {
		return ""
	}
	content := string(data)

	// Strip frontmatter to return just the instructions
	if strings.HasPrefix(content, "---\n") {
		rest := content[4:]
		endIdx := strings.Index(rest, "\n---")
		if endIdx >= 0 {
			bodyStart := endIdx + 4
			if bodyStart < len(rest) {
				return strings.TrimSpace(rest[bodyStart:])
			}
		}
	}
	return strings.TrimSpace(content)
}

// ── Standard discovery paths ──────────────────────────────────────────────────

// StandardSkillDirs returns the standard set of directories to discover skills from,
// following pi-mono conventions. Args:
//   projectDir — the project root
//   userHome   — the user's home directory (usually os.UserHomeDir())
func StandardSkillDirs(projectDir, userHome string) []string {
	var dirs []string

	// 1. Project: .pux/skills/
	if projectDir != "" {
		dirs = append(dirs, filepath.Join(projectDir, ".pux", "skills"))
	}

	// 2. Project: .agents/skills/ (standard Agent Skills convention)
	if projectDir != "" {
		dirs = append(dirs, filepath.Join(projectDir, ".agents", "skills"))
	}

	// 3. Walk ancestor directories for .agents/skills/ (stop at git root or filesystem root)
	if projectDir != "" {
		dir := projectDir
		for {
			dirs = append(dirs, filepath.Join(dir, ".agents", "skills"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break // filesystem root
			}
			// Stop at git root
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				break
			}
			dir = parent
		}
	}

	// 4. User: ~/.pux/skills/
	if userHome != "" {
		dirs = append(dirs, filepath.Join(userHome, ".pux", "skills"))
	}

	// 5. User: ~/.agents/skills/
	if userHome != "" {
		dirs = append(dirs, filepath.Join(userHome, ".agents", "skills"))
	}

	return dirs
}

// LoadStandardSkills creates a SkillStore loaded from standard discovery paths.
func LoadStandardSkills(projectDir, userHome string) *SkillStore {
	store := NewSkillStore()
	dirs := StandardSkillDirs(projectDir, userHome)
	store.LoadFromDirs(dirs)
	return store
}
