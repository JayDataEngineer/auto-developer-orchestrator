package llama

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a discoverable skill defined by a SKILL.md file.
// Skills provide reusable instructions that are injected into the system prompt.
// Pattern from Pi's agent skills standard.
type Skill struct {
	Name         string // Skill identifier (from frontmatter or directory name)
	Description  string // One-line description (from frontmatter)
	Trigger      string // Space-separated keywords for matching
	Instructions string // Markdown body (the actual instructions)
	SourcePath   string // Where the SKILL.md was loaded from
}

// SkillLoader discovers and loads SKILL.md files from the project directory.
type SkillLoader struct {
	projectDir string
	skills     []Skill
}

// NewSkillLoader creates a new skill loader for the given project directory.
func NewSkillLoader(projectDir string) *SkillLoader {
	return &SkillLoader{projectDir: projectDir}
}

// Load discovers all SKILL.md files in the project directory tree.
// Looks for files named SKILL.md or *.skill.md.
// Returns the number of skills loaded.
func (l *SkillLoader) Load() (int, error) {
	l.skills = nil

	// Walk the project directory
	err := filepath.WalkDir(l.projectDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if d.IsDir() {
			// Skip hidden dirs, node_modules, .git
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if it's a skill file
		base := strings.ToLower(d.Name())
		if base == "skill.md" || strings.HasSuffix(base, ".skill.md") {
			skill, parseErr := parseSkillFile(path)
			if parseErr != nil {
				return nil // skip malformed files
			}
			// Make source path relative to project dir
			rel, _ := filepath.Rel(l.projectDir, path)
			skill.SourcePath = rel
			l.skills = append(l.skills, *skill)
		}
		return nil
	})

	return len(l.skills), err
}

// Skills returns the loaded skills.
func (l *SkillLoader) Skills() []Skill {
	return l.skills
}

// SkillsForPrompt returns the skills formatted for injection into the system prompt.
// Returns empty string if no skills loaded.
func (l *SkillLoader) SkillsForPrompt() string {
	if len(l.skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<skills>\n")
	for _, s := range l.skills {
		b.WriteString(fmt.Sprintf("<skill name=\"%s\">\n", s.Name))
		if s.Description != "" {
			b.WriteString(fmt.Sprintf("Description: %s\n", s.Description))
		}
		if s.Trigger != "" {
			b.WriteString(fmt.Sprintf("Trigger: %s\n", s.Trigger))
		}
		b.WriteString(s.Instructions)
		b.WriteString("\n</skill>\n")
	}
	b.WriteString("</skills>")
	return b.String()
}

// MatchTrigger returns skills whose trigger keywords match the given message.
// Returns all skills if the message matches any trigger.
func (l *SkillLoader) MatchTrigger(message string) []Skill {
	msgLower := strings.ToLower(message)
	var matched []Skill
	for _, s := range l.skills {
		if s.Trigger == "" {
			continue
		}
		keywords := strings.Fields(s.Trigger)
		for _, kw := range keywords {
			if strings.Contains(msgLower, strings.ToLower(kw)) {
				matched = append(matched, s)
				break
			}
		}
	}
	return matched
}

// parseSkillFile parses a SKILL.md file with YAML frontmatter.
// Format:
//
//	---
//	name: commit
//	description: Create a git commit
//	trigger: commit check in
//	---
//	Instructions in markdown...
func parseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := string(data)

	// Parse frontmatter
	name := ""
	description := ""
	trigger := ""
	instructions := content

	if strings.HasPrefix(content, "---") {
		parts := strings.SplitN(content, "---", 3)
		if len(parts) >= 3 {
			frontmatter := parts[1]
			instructions = strings.TrimSpace(parts[2])

			// Parse simple YAML key: value pairs
			for _, line := range strings.Split(frontmatter, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				key, value, found := strings.Cut(line, ":")
				if !found {
					continue
				}
				key = strings.TrimSpace(key)
				value = strings.TrimSpace(value)
				switch key {
				case "name":
					name = value
				case "description":
					description = value
				case "trigger":
					trigger = value
				}
			}
		}
	}

	// Default name from directory or file name
	if name == "" {
		if strings.HasSuffix(filepath.Base(path), ".skill.md") {
			name = strings.TrimSuffix(filepath.Base(path), ".skill.md")
		} else {
			name = filepath.Base(filepath.Dir(path))
		}
	}

	return &Skill{
		Name:         name,
		Description:  description,
		Trigger:      trigger,
		Instructions: instructions,
		SourcePath:   path,
	}, nil
}
