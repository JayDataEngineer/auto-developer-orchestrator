package llama

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	memoryFileName = "MEMORY.md"
	maxMemoryLines = 200
	maxMemoryBytes = 25000
)

// ProjectMemory manages a MEMORY.md file per project directory.
// It provides persistent memory that survives across sessions.
// The file is loaded at session start and can be updated via the update_memory tool.
type ProjectMemory struct {
	projectDir string
	mu         sync.RWMutex
	content    string
	dirty      bool
}

// NewProjectMemory creates a new ProjectMemory for the given project directory.
func NewProjectMemory(projectDir string) *ProjectMemory {
	m := &ProjectMemory{projectDir: projectDir}
	m.Load()
	return m
}

// Load reads MEMORY.md from disk. If the file doesn't exist, content stays empty.
func (m *ProjectMemory) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.filePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			m.content = ""
			m.dirty = false
			return nil
		}
		return err
	}
	m.content = string(data)
	m.dirty = false
	return nil
}

// Content returns the current memory content (thread-safe).
func (m *ProjectMemory) Content() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.content
}

// Update replaces the memory content and marks it dirty.
// Truncates to maxMemoryBytes and maxMemoryLines.
func (m *ProjectMemory) Update(content string) {
	content = truncateMemory(content)
	m.mu.Lock()
	m.content = content
	m.dirty = true
	m.mu.Unlock()
}

// ── Memory Sections (Claude Code pattern: typed categories) ──────────

// MemorySection identifies a categorized section within MEMORY.md.
type MemorySection string

const (
	SectionPreferences MemorySection = "## User Preferences"
	SectionFacts       MemorySection = "## Project Facts"
	SectionFeedback    MemorySection = "## Feedback"
	SectionReference   MemorySection = "## Reference"
)

// AllSections returns all defined memory sections in display order.
func AllSections() []MemorySection {
	return []MemorySection{SectionPreferences, SectionFacts, SectionFeedback, SectionReference}
}

// UpdateSection updates a specific section of memory without affecting others.
// If the section exists, its content is replaced. If not, it's appended.
// Other sections remain unchanged.
func (m *ProjectMemory) UpdateSection(section MemorySection, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sections := m.parseSections()

	// Update or add the section
	sections[section] = content

	// Rebuild the full memory content
	m.content = m.buildFromSections(sections)
	m.dirty = true
}

// GetSection returns the content of a specific memory section.
// Returns empty string if the section doesn't exist.
func (m *ProjectMemory) GetSection(section MemorySection) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	sections := m.parseSections()
	return sections[section]
}

// parseSections extracts the content of each ## section from the memory.
func (m *ProjectMemory) parseSections() map[MemorySection]string {
	result := make(map[MemorySection]string)
	content := m.content

	// Find all section headers
	type sectionRange struct {
		name MemorySection
		start int
		end   int
	}

	var ranges []sectionRange
	for _, sec := range AllSections() {
		header := string(sec)
		idx := strings.Index(content, header)
		if idx >= 0 {
			ranges = append(ranges, sectionRange{name: sec, start: idx + len(header)})
		}
	}

	// Sort by position
	for i := 0; i < len(ranges); i++ {
		ranges[i].end = len(content)
		for j := 0; j < len(ranges); j++ {
			if i != j && ranges[j].start > ranges[i].start && ranges[j].start < ranges[i].end {
				ranges[i].end = ranges[j].start
			}
		}
		result[ranges[i].name] = strings.TrimSpace(content[ranges[i].start:ranges[i].end])
	}

	return result
}

// buildFromSections reconstructs the full memory content from sections.
func (m *ProjectMemory) buildFromSections(sections map[MemorySection]string) string {
	var b strings.Builder
	b.WriteString("# Project Memory\n\n")
	for _, sec := range AllSections() {
		content, ok := sections[sec]
		if !ok {
			continue
		}
		b.WriteString(string(sec) + "\n")
		b.WriteString(content + "\n\n")
	}
	return truncateMemory(b.String())
}

// truncateMemory enforces the byte and line limits.
func truncateMemory(content string) string {
	if len(content) > maxMemoryBytes {
		content = content[:maxMemoryBytes]
	}
	lines := strings.Split(content, "\n")
	if len(lines) > maxMemoryLines {
		lines = lines[:maxMemoryLines]
		content = strings.Join(lines, "\n")
	}
	return content
}

// Save writes the memory to disk if dirty.
func (m *ProjectMemory) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.dirty {
		return nil
	}

	path := m.filePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	// Write atomically via temp file
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(m.content), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}

	m.dirty = false
	return nil
}

// InjectPrefix returns the memory content formatted for injection into
// the system prompt or user message. Returns empty string if no memory.
func (m *ProjectMemory) InjectPrefix() string {
	c := m.Content()
	if c == "" {
		return ""
	}
	return "<memory>\n" + c + "\n</memory>\n\n"
}

// filePath returns the absolute path to MEMORY.md in the project directory.
func (m *ProjectMemory) filePath() string {
	return filepath.Join(m.projectDir, memoryFileName)
}

// LineCount returns the number of lines in memory content.
func (m *ProjectMemory) LineCount() int {
	c := m.Content()
	if c == "" {
		return 0
	}
	count := strings.Count(c, "\n")
	if !strings.HasSuffix(c, "\n") {
		count++
	}
	return count
}

// ReadMemoryFile reads a MEMORY.md file and returns its content.
// Returns empty string if file doesn't exist.
func ReadMemoryFile(projectDir string) string {
	path := filepath.Join(projectDir, memoryFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// CountLines counts lines in a string.
func CountLines(s string) int {
	scanner := bufio.NewScanner(strings.NewReader(s))
	count := 0
	for scanner.Scan() {
		count++
	}
	return count
}
