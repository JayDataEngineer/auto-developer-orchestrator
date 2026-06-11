package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// Skill represents a discoverable skill defined by a SKILL.md file.
// Follows pi-mono's Agent Skills standard: YAML frontmatter + markdown body.
type Skill struct {
	Name              string
	Description       string
	Location          string
	Dir               string
	DisableInvocation bool
}

var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Store holds all discovered skills.
type Store struct {
	skills []Skill
	byName map[string]*Skill
	byPath map[string]*Skill
}

// NewStore creates an empty skill store.
func NewStore() *Store {
	return &Store{
		byName: make(map[string]*Skill),
		byPath: make(map[string]*Skill),
	}
}

// LoadFromDirs discovers SKILL.md files from the given directories.
func (s *Store) LoadFromDirs(dirs []string) int {
	count := 0
	for _, dir := range dirs {
		count += s.discoverFromDir(dir)
	}
	return count
}

// Get returns a skill by name.
func (s *Store) Get(name string) *Skill {
	return s.byName[name]
}

// Count returns the number of loaded skills.
func (s *Store) Count() int {
	return len(s.skills)
}

// All returns all loaded skills.
func (s *Store) All() []Skill {
	return s.skills
}

// FormatAvailableSkills builds the <available_skills> XML block for the system prompt.
func (s *Store) FormatAvailableSkills() string {
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

// ReadSkill loads the full instructions for a skill by name (strips frontmatter).
func (s *Store) ReadSkill(name string) string {
	skill := s.byName[name]
	if skill == nil {
		return ""
	}
	data, err := os.ReadFile(skill.Location)
	if err != nil {
		return ""
	}
	content := string(data)
	if strings.HasPrefix(content, "---\n") {
		rest := content[4:]
		if endIdx := strings.Index(rest, "\n---"); endIdx >= 0 {
			bodyStart := endIdx + 4
			if bodyStart < len(rest) {
				return strings.TrimSpace(rest[bodyStart:])
			}
		}
	}
	return strings.TrimSpace(content)
}

// StandardSkillDirs returns standard discovery paths following pi-mono conventions.
func StandardSkillDirs(projectDir, userHome string) []string {
	var dirs []string
	if projectDir != "" {
		dirs = append(dirs, filepath.Join(projectDir, ".pux", "skills"))
		dirs = append(dirs, filepath.Join(projectDir, ".agents", "skills"))
		// Walk ancestors for .agents/skills/
		dir := projectDir
		for {
			dirs = append(dirs, filepath.Join(dir, ".agents", "skills"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				break
			}
			dir = parent
		}
	}
	if userHome != "" {
		dirs = append(dirs, filepath.Join(userHome, ".pux", "skills"))
		dirs = append(dirs, filepath.Join(userHome, ".agents", "skills"))
	}
	return dirs
}

// LoadStandard creates a Store from standard paths.
func LoadStandard(projectDir, userHome string) *Store {
	store := NewStore()
	store.LoadFromDirs(StandardSkillDirs(projectDir, userHome))
	return store
}

// ReadSkillTool implements core.Tool for loading skill instructions on demand.
type ReadSkillTool struct {
	store *Store
}

func NewReadSkillTool(store *Store) *ReadSkillTool {
	return &ReadSkillTool{store: store}
}

func (t *ReadSkillTool) Name() string        { return "read_skill" }
func (t *ReadSkillTool) Description() string { return "Load the full instructions for a specific skill" }
func (t *ReadSkillTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Name of the skill to load"}},"required":["name"]}`)
}

func (t *ReadSkillTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return nil, core.NewToolError("read_skill", "missing required parameter 'name'")
	}
	instructions := t.store.ReadSkill(name)
	if instructions == "" {
		return nil, core.NewToolError("read_skill", fmt.Sprintf("skill %q not found", name))
	}
	return map[string]any{"skill": name, "instructions": instructions}, nil
}

// discoverFromDir walks a directory tree to find SKILL.md files.
func (s *Store) discoverFromDir(dir string) int {
	if dir == "" {
		return 0
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return 0
	}

	count := 0
	filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if path == abs {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(d.Name(), "skill.md") {
			return nil
		}
		skill, err := parseSkillFile(path)
		if err != nil || skill == nil {
			return nil
		}
		if _, exists := s.byName[skill.Name]; exists {
			return nil
		}
		s.skills = append(s.skills, *skill)
		s.byName[skill.Name] = skill
		s.byPath[path] = skill
		count++
		// Stop recursing into directory where SKILL.md was found
		if path != abs {
			return filepath.SkipDir
		}
		return nil
	})
	return count
}

func parseSkillFile(absPath string) (*Skill, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	content := string(data)

	fm := make(map[string]string)
	if strings.HasPrefix(content, "---\n") {
		rest := content[4:]
		if endIdx := strings.Index(rest, "\n---"); endIdx >= 0 {
			fm = parseSkillYAML(rest[:endIdx])
		}
	}

	name := validateSkillName(fm["name"], 64)
	if name == "" {
		dirName := filepath.Base(filepath.Dir(absPath))
		name = validateSkillName(dirName, 64)
	}
	if name == "" {
		return nil, fmt.Errorf("invalid skill name in %s", absPath)
	}

	description := fm["description"]
	if description == "" {
		return nil, fmt.Errorf("skill %s missing description", name)
	}
	if len(description) > 1024 {
		description = description[:1021] + "..."
	}

	disableInvocation := strings.ToLower(fm["disable-model-invocation"]) == "true"

	return &Skill{
		Name:              name,
		Description:       description,
		Location:          absPath,
		Dir:               filepath.Dir(absPath),
		DisableInvocation: disableInvocation,
	}, nil
}

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
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		m[key] = value
	}
	return m
}
