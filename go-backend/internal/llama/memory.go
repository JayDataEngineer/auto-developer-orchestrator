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
	// Enforce byte limit
	if len(content) > maxMemoryBytes {
		content = content[:maxMemoryBytes]
	}

	// Enforce line limit
	lines := strings.Split(content, "\n")
	if len(lines) > maxMemoryLines {
		lines = lines[:maxMemoryLines]
		content = strings.Join(lines, "\n")
	}

	m.mu.Lock()
	m.content = content
	m.dirty = true
	m.mu.Unlock()
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
