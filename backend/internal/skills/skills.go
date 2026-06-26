// Package skills discovers and loads SKILL.md files from a project's
// skills/ directory. Each skill is a markdown file with YAML frontmatter
// declaring name + description; the body is model-facing instructions.
//
// Skills are HOST-side backbone context — operator-authored, not
// model-mutable through this surface. The model discovers them via
// list_skills and reads them via load_skill. Skills can still be edited
// through file_write (they're bind-mounted into the sandbox at
// /sandbox/workspace/skills/), but that's an operator concern.
//
// Format:
//
//	skills/
//	  <skill-name>/SKILL.md
//	  <other-skill>/SKILL.md
//
// SKILL.md shape:
//
//	---
//	name: skill-name
//	description: One-line summary shown by list_skills
//	---
//
//	# Body
//
//	Markdown instructions the model reads via load_skill.
package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill is one discovered SKILL.md. Path is absolute on the host. Content
// is the markdown body with frontmatter stripped.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Content     string `json:"content,omitempty"`
}

// SkillNameFile is the canonical filename inside each skill directory.
const SkillNameFile = "SKILL.md"

// Discover walks <root>/skills/ and returns one Skill per SKILL.md found.
// A missing skills/ directory is not an error — returns nil, nil (the
// common case for projects that haven't authored any skills yet).
//
// Directories without a SKILL.md are skipped silently. This keeps the
// shape flexible (a skills/ dir can hold scratch folders, READMEs, etc.)
// without breaking discovery.
func Discover(root string) ([]Skill, error) {
	dir := filepath.Join(root, "skills")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}

	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), SkillNameFile)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		s, err := parse(string(data))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		// Discover returns metadata only — Content is omitted to keep
		// list_skills responses small. Load() fills Content on demand.
		s.Content = ""
		s.Path = path
		out = append(out, s)
	}
	return out, nil
}

// Load returns the named skill from <root>/skills/. Re-reads the file from
// disk so the body is current. Returns a wrapped error if missing.
func Load(root, name string) (Skill, error) {
	dir := filepath.Join(root, "skills", name, SkillNameFile)
	data, err := os.ReadFile(dir)
	if errors.Is(err, os.ErrNotExist) {
		return Skill{}, fmt.Errorf("skill %q not found", name)
	}
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %q: %w", name, err)
	}
	s, err := parse(string(data))
	if err != nil {
		return Skill{}, fmt.Errorf("parse skill %q: %w", name, err)
	}
	s.Path = dir
	return s, nil
}

// parse extracts frontmatter + body from raw SKILL.md content. Frontmatter
// is the YAML block delimited by `---` lines at the very start of the file;
// absence is allowed (falls back to filename-derived name + empty desc).
// Returns an error only when frontmatter exists but is malformed (opening
// `---` with no matching close).
func parse(raw string) (Skill, error) {
	var s Skill
	body := raw

	if strings.HasPrefix(raw, "---\n") || strings.HasPrefix(raw, "---\r\n") {
		// Normalize line endings for the boundary search.
		rest := strings.TrimPrefix(raw, "---\n")
		rest = strings.TrimPrefix(rest, "---\r\n")

		endIdx := indexOfFrontmatterClose(rest)
		if endIdx < 0 {
			return s, errors.New("malformed frontmatter (no closing ---)")
		}
		fm := rest[:endIdx]
		// Skip past the closing `---` + its newline.
		afterClose := rest[endIdx:]
		afterClose = strings.TrimPrefix(afterClose, "---\n")
		afterClose = strings.TrimPrefix(afterClose, "---\r\n")
		body = afterClose

		if err := parseFrontmatter(fm, &s); err != nil {
			return s, err
		}
	}

	s.Content = strings.TrimSpace(body)
	if s.Name == "" {
		return s, errors.New("frontmatter missing required 'name' field")
	}
	return s, nil
}

// indexOfFrontmatterClose returns the byte index of the closing `---` line
// within the frontmatter body (the part AFTER the opening `---\n`). Returns
// -1 if no close found.
func indexOfFrontmatterClose(s string) int {
	// Look for `\n---\n` or `\n---\r\n` or string starting with `---\n`
	// (case where frontmatter is empty).
	if strings.HasPrefix(s, "---\n") || strings.HasPrefix(s, "---\r\n") {
		return 0
	}
	for _, marker := range []string{"\n---\n", "\n---\r\n"} {
		if idx := strings.Index(s, marker); idx >= 0 {
			return idx + 1 // +1 to land on the `---` itself
		}
	}
	return -1
}

// parseFrontmatter is a minimal key:value parser. Avoids importing a full
// YAML library — the contract is two fields (name, description), both
// scalars. Quotes are stripped; comments (# ...) and blank lines skipped.
func parseFrontmatter(fm string, s *Skill) error {
	for line := range strings.SplitSeq(fm, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue // not a key:value line — skip rather than fail
		}
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			s.Name = val
		case "description":
			s.Description = val
		}
	}
	return nil
}
