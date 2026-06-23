package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
	"gopkg.in/yaml.v3"
)

// Skill represents a discoverable skill. Follows pi-mono's Agent Skills
// standard: YAML frontmatter + markdown body. Two layouts are accepted:
//
//   - Canonical: <skill-name>/SKILL.md     (parent dir supplies the name)
//   - Flat:      <STEM_NAME>.md            (filename stem supplies the name)
//
// Frontmatter (when present) overrides either derived name and supplies the
// description. When frontmatter is missing, the loader derives a description
// from the first H1 / paragraph so flat-layout files load without ceremony.
type Skill struct {
	Name              string
	Description       string
	Location          string
	Dir               string
	DisableInvocation bool
	Version           string
	Capabilities      []string
}

// skillFrontmatter is the YAML shape parsed from the leading `---\n...\n---`
// block. yaml.v3 takes the place of the prior hand-rolled parser.
type skillFrontmatter struct {
	Name              string   `yaml:"name"`
	Description       string   `yaml:"description"`
	DisableInvocation bool     `yaml:"disable-model-invocation"`
	Version           string   `yaml:"version"`
	Capabilities      []string `yaml:"capabilities"`
}

var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// LoadReport captures what happened during one discovery pass over a single
// directory. Walked minus Loaded equals len(Skipped) — every dropped file is
// accounted for so silent failures are impossible.
type LoadReport struct {
	Dir     string
	Walked  int
	Loaded  int
	Skipped []string
}

// Summary renders a one-line digest used by boot loggers and audit failures.
// Empty when nothing was skipped (no noise on healthy loads).
func (r LoadReport) Summary() string {
	if r.Walked == r.Loaded {
		return ""
	}
	return fmt.Sprintf("%s: loaded %d/%d (skipped %d)", r.Dir, r.Loaded, r.Walked, len(r.Skipped))
}

// Store holds all discovered skills plus the per-dir load reports. Reports
// persist so callers (CLI, audit tests, boot logger) can surface skips.
type Store struct {
	mu      sync.RWMutex
	skills  []Skill
	byName  map[string]*Skill
	byPath  map[string]*Skill
	reports []LoadReport

	watcher *watcher
}

// NewStore creates an empty skill store.
func NewStore() *Store {
	return &Store{
		byName: make(map[string]*Skill),
		byPath: make(map[string]*Skill),
	}
}

// LoadFromDirs discovers skill files from the given directories. Returns the
// total count of skills registered across all dirs. Per-dir breakdown lives
// in Reports() — use it from boot loggers and audit checks.
func (s *Store) LoadFromDirs(dirs []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, dir := range dirs {
		r := s.discoverFromDirLocked(dir)
		s.reports = append(s.reports, r)
		total += r.Loaded
	}
	return total
}

// Reports returns a copy of per-dir load reports. The slice is safe to retain.
func (s *Store) Reports() []LoadReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]LoadReport(nil), s.reports...)
}

// SkippedCount returns the total number of candidate files dropped across all
// loaded dirs. Non-zero means human attention is warranted.
func (s *Store) SkippedCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, r := range s.reports {
		n += len(r.Skipped)
	}
	return n
}

// Get returns a skill by name.
func (s *Store) Get(name string) *Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byName[name]
}

// Count returns the number of loaded skills.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.skills)
}

// All returns all loaded skills.
func (s *Store) All() []Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Skill(nil), s.skills...)
}

// Visible returns a store view restricted to the named skills. The returned
// store shares Skill pointers with the receiver, so it is cheap. Empty /
// nil allowlist returns the receiver unchanged (unscoped).
//
// Used by sub-agent wiring so workers can call read_skill for skills their
// role declares via RoleConfig.Skills or capability-scoped attachment.
func (s *Store) Visible(allowed []string) *Store {
	if len(allowed) == 0 {
		return s
	}
	out := NewStore()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, name := range allowed {
		skill := s.byName[name]
		if skill == nil {
			continue
		}
		out.skills = append(out.skills, *skill)
		idx := len(out.skills) - 1
		stored := &out.skills[idx]
		out.byName[stored.Name] = stored
		out.byPath[stored.Location] = stored
	}
	// Scoped views inherit the parent's reports so the boot log still shows
	// what was discovered; only the visible set is filtered.
	out.reports = append([]LoadReport(nil), s.reports...)
	return out
}

// ForCapability returns the names of skills that declare they attach to the
// given capability (via frontmatter `capabilities: [name]`). Used to
// auto-grant a worker's read_skill scope from its imports.
func (s *Store) ForCapability(capability string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var names []string
	for i := range s.skills {
		if slices.Contains(s.skills[i].Capabilities, capability) {
			names = append(names, s.skills[i].Name)
		}
	}
	return names
}

// FormatAvailableSkills builds the <available_skills> XML block for the
// system prompt. Disabled skills (disable-model-invocation: true) are hidden
// from the block but still loadable by name via read_skill.
func (s *Store) FormatAvailableSkills() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var visible []*Skill
	for i := range s.skills {
		if s.skills[i].DisableInvocation {
			continue
		}
		visible = append(visible, &s.skills[i])
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
		fmt.Fprintf(&b, "    <name>%s</name>\n", skill.Name)
		fmt.Fprintf(&b, "    <description>%s</description>\n", skill.Description)
		fmt.Fprintf(&b, "    <location>%s</location>\n", skill.Location)
		if skill.Version != "" {
			fmt.Fprintf(&b, "    <version>%s</version>\n", skill.Version)
		}
		b.WriteString("  </skill>\n")
	}
	b.WriteString("</available_skills>")
	return b.String()
}

// ReadSkill loads the full instructions for a skill by name, stripping
// frontmatter. Returns "" for unknown skills or read errors.
func (s *Store) ReadSkill(name string) string {
	s.mu.RLock()
	skill := s.byName[name]
	s.mu.RUnlock()
	if skill == nil {
		return ""
	}
	data, err := os.ReadFile(skill.Location)
	if err != nil {
		return ""
	}
	content := string(data)
	body := stripFrontmatter(content)
	return strings.TrimSpace(body)
}

// stripFrontmatter returns content with any leading YAML frontmatter block
// removed. Frontmatter is delimited by leading `---\n` and the next `\n---`.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	rest := content[4:]
	endIdx := strings.Index(rest, "\n---")
	if endIdx < 0 {
		return content
	}
	bodyStart := endIdx + 4
	if bodyStart >= len(rest) {
		return ""
	}
	return rest[bodyStart:]
}

// StandardSkillDirs returns standard discovery paths following pi-mono
// conventions. Walks ancestors of projectDir until a .git marker is found.
func StandardSkillDirs(projectDir, userHome string) []string {
	var dirs []string
	if projectDir != "" {
		dirs = append(dirs, filepath.Join(projectDir, ".pux", "skills"))
		dirs = append(dirs, filepath.Join(projectDir, ".agents", "skills"))
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

// WatchAndReload polls the loaded dirs at interval and reloads when any
// watched file changes. Cancel the context to stop the loop. The first reload
// is immediate (so callers can drop the synchronous LoadFromDirs call).
//
// Polling avoids adding fsnotify as a dependency; the watcher is dev-loop
// ergonomics, not a hot path, and 2-second granularity is plenty for "edit
// the skill file and re-run the agent" workflows.
func (s *Store) WatchAndReload(dirs []string, interval time.Duration, done <-chan struct{}, log func(format string, args ...any)) {
	if len(dirs) == 0 || interval <= 0 {
		return
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	w := newWatcher(dirs)
	s.mu.Lock()
	s.watcher = w
	s.mu.Unlock()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if !w.changed() {
					continue
				}
				// Snapshot then rebuild. Drop prior reports — they describe a
				// load that has been superseded.
				fresh := NewStore()
				fresh.LoadFromDirs(dirs)
				fresh.mu.RLock()
				s.mu.Lock()
				s.skills = fresh.skills
				s.byName = fresh.byName
				s.byPath = fresh.byPath
				s.reports = fresh.reports
				s.mu.Unlock()
				fresh.mu.RUnlock()
				log("skills: hot-reloaded %d skill(s) from %d dir(s)", len(s.skills), len(dirs))
			}
		}
	}()
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

// discoverFromDirLocked walks one directory tree. Caller must hold s.mu.
// Every dropped file gets a reason in the returned report — no silent skips.
func (s *Store) discoverFromDirLocked(dir string) LoadReport {
	report := LoadReport{Dir: dir}
	if dir == "" {
		return report
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		report.Skipped = append(report.Skipped, fmt.Sprintf("%s: abs path error: %v", dir, err))
		return report
	}
	info, err := os.Stat(abs)
	if err != nil {
		// Missing dir is normal (e.g. ~/.pux/skills absent on a fresh box).
		// Treat as zero-walked, not a skip — keeps the boot log clean.
		report.Dir = abs
		return report
	}
	report.Dir = abs
	if !info.IsDir() {
		report.Skipped = append(report.Skipped, fmt.Sprintf("%s: not a directory", abs))
		return report
	}

	_ = filepath.WalkDir(abs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s: walk error: %v", path, walkErr))
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
		lower := strings.ToLower(d.Name())
		if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
			return nil
		}
		report.Walked++

		isCanonical := strings.EqualFold(d.Name(), "skill.md")
		isTopLevel := filepath.Dir(path) == abs
		// Skip .md files in subdirs that aren't SKILL.md. Catches README.md,
		// CHANGELOG.md, etc. nested under skill folders.
		if !isCanonical && !isTopLevel {
			report.Skipped = append(report.Skipped,
				fmt.Sprintf("%s: not a skill (only SKILL.md files or top-level *.md qualify)", path))
			return nil
		}

		skill, skipReason, err := parseSkillFile(path, isCanonical, isTopLevel)
		if err != nil {
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if skipReason != "" {
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s: %s", path, skipReason))
			return nil
		}
		if skill == nil {
			report.Skipped = append(report.Skipped, fmt.Sprintf("%s: parser returned nil skill", path))
			return nil
		}
		if existing, dup := s.byName[skill.Name]; dup {
			report.Skipped = append(report.Skipped,
				fmt.Sprintf("%s: duplicate skill name %q (already registered from %s)",
					path, skill.Name, existing.Location))
			return nil
		}
		s.skills = append(s.skills, *skill)
		idx := len(s.skills) - 1
		stored := &s.skills[idx]
		s.byName[stored.Name] = stored
		s.byPath[path] = stored
		report.Loaded++

		// Canonical layout: don't recurse into the dir that owned the
		// SKILL.md — its siblings are references / notes, not skills.
		if isCanonical && path != abs {
			return filepath.SkipDir
		}
		return nil
	})
	return report
}

// parseSkillFile parses one .md file into a Skill. isCanonical indicates
// canonical <name>/SKILL.md layout (name falls back to parent dir).
// isTopLevel indicates flat <STEM>.md layout (name falls back to filename
// stem). Both flags drive name fallback ordering.
//
// Returns (skill, skipReason, err). skipReason is non-empty when the file is
// valid but should not be registered — e.g. description cannot be derived.
func parseSkillFile(absPath string, isCanonical, isTopLevel bool) (*Skill, string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, "", err
	}
	content := string(data)

	var fm skillFrontmatter
	bodyStart := 0
	if strings.HasPrefix(content, "---\n") {
		rest := content[4:]
		endIdx := strings.Index(rest, "\n---")
		if endIdx >= 0 {
			fmYAML := rest[:endIdx]
			if err := yaml.Unmarshal([]byte(fmYAML), &fm); err != nil {
				// Soft-fail: treat as no frontmatter so flat-layout files
				// with broken YAML still derive from filename.
				fm = skillFrontmatter{}
			}
			bodyStart = 4 + endIdx + 4 // "---\n" + content + "\n---"
		}
	}

	name := validateSkillName(fm.Name, 64)
	if name == "" {
		switch {
		case isCanonical:
			name = validateSkillName(filepath.Base(filepath.Dir(absPath)), 64)
		case isTopLevel:
			stem := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
			name = validateSkillName(stemToKebab(stem), 64)
		}
	}
	if name == "" {
		return nil, "", fmt.Errorf("cannot derive kebab-case name from frontmatter, parent dir, or filename stem")
	}

	description := strings.TrimSpace(fm.Description)
	if description == "" {
		description = deriveDescriptionFromContent(content, bodyStart)
	}
	if description == "" {
		return nil, "missing description (no frontmatter, no H1, no first paragraph to derive from)", nil
	}
	if len(description) > 1024 {
		description = description[:1021] + "..."
	}

	return &Skill{
		Name:              name,
		Description:       description,
		Location:          absPath,
		Dir:               filepath.Dir(absPath),
		DisableInvocation: fm.DisableInvocation,
		Version:           strings.TrimSpace(fm.Version),
		Capabilities:      append([]string(nil), fm.Capabilities...),
	}, "", nil
}

// stemToKebab normalizes a filename stem (e.g. CONTEXT_ENGINE_QUERY) into a
// kebab-case skill name (context-engine-query). Underscores, spaces, and
// dots become dashes; casing is lowercased; repeated dashes collapse.
func stemToKebab(stem string) string {
	s := strings.ToLower(stem)
	for _, rep := range []struct{ from, to string }{
		{"_", "-"},
		{" ", "-"},
		{".", "-"},
	} {
		s = strings.ReplaceAll(s, rep.from, rep.to)
	}
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

var (
	boldRe   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	italicRe = regexp.MustCompile(`\*([^*]+)\*`)
	codeRe   = regexp.MustCompile("`([^`]+)`")
)

// deriveDescriptionFromContent pulls the first paragraph after the H1 (or the
// first paragraph if no H1) out of the body. Used when frontmatter's
// description is missing. Markdown inline markers are stripped for cleanliness.
func deriveDescriptionFromContent(content string, bodyStart int) string {
	body := content
	if bodyStart > 0 && bodyStart < len(content) {
		body = content[bodyStart:]
	}
	lines := strings.Split(body, "\n")
	skipH1 := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "---" {
			continue
		}
		isH1 := strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "##")
		if isH1 {
			skipH1 = true
			continue
		}
		if skipH1 {
			if strings.HasPrefix(trimmed, "##") || strings.HasPrefix(trimmed, "```") {
				break
			}
			desc := trimmed
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if next == "" || strings.HasPrefix(next, "#") || strings.HasPrefix(next, "```") {
					break
				}
				desc = appendPara(desc, next)
			}
			return cleanMarkdownInline(desc)
		}
		// No H1 yet — first paragraph IS the description.
		if strings.HasPrefix(trimmed, "##") || strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "```") {
			continue
		}
		desc := trimmed
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if next == "" || strings.HasPrefix(next, "#") || strings.HasPrefix(next, "```") {
				break
			}
			desc = appendPara(desc, next)
		}
		return cleanMarkdownInline(desc)
	}
	return ""
}

// appendPara is the helper for deriveDescriptionFromContent — avoids
// repeated string += in loops (inefficient per go vet's stringsbuilder check).
func appendPara(prefix, next string) string {
	var sb strings.Builder
	sb.Grow(len(prefix) + 1 + len(next))
	sb.WriteString(prefix)
	sb.WriteByte(' ')
	sb.WriteString(next)
	return sb.String()
}

func cleanMarkdownInline(s string) string {
	s = boldRe.ReplaceAllString(s, "$1")
	s = italicRe.ReplaceAllString(s, "$1")
	s = codeRe.ReplaceAllString(s, "$1")
	return strings.Join(strings.Fields(s), " ")
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
