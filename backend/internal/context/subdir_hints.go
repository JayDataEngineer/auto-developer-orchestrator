// Package context provides context management for the agent loop.
//
// SubdirectoryHintTracker implements progressive context discovery:
// as the agent reads files or runs commands in new directories, it discovers
// and loads project context files (AGENTS.md, CLAUDE.md, .cursorrules)
// from those directories. Discovered hints are appended to the tool result
// so the model gets relevant context at the moment it starts working in a
// new area of the codebase.
//
// Inspired by Hermes Agent's SubdirectoryHintTracker (NousResearch) and
// Block/goose's directory hint system.
package context

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// HintFilenames lists context files to look for in subdirectories, in priority order.
// First match wins per directory (same convention as Claude Code and Hermes).
var HintFilenames = []string{
	"AGENTS.md",
	"agents.md",
	"CLAUDE.md",
	"claude.md",
	".cursorrules",
}

// MaxHintChars is the maximum content length per hint file.
const MaxHintChars = 8000

// MaxAncestorWalk limits how many parent directories to walk up.
const MaxAncestorWalk = 5

// PathArgKeys are tool argument keys that typically contain file paths.
var PathArgKeys = []string{"path", "file_path", "workdir", "directory", "dir"}

// CommandTools are tool names that take shell commands where paths should be extracted.
var CommandTools = map[string]bool{
	"bash":     true,
	"terminal": true,
	"shell":    true,
}

// pathLikeToken matches tokens that look like file paths (contain / or a dot extension).
var pathLikeToken = regexp.MustCompile(`[/.]`)

// urlPattern matches URLs that should not be treated as paths.
var urlPattern = regexp.MustCompile(`^(https?://|git@|ssh://)`)

// injectionPatterns matches common prompt injection attempts in context files.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(previous|all|above|prior)\s+instructions`),
	regexp.MustCompile(`(?i)do\s+not\s+tell\s+the\s+user`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+a`),
	regexp.MustCompile(`(?i)forget\s+(previous|all|your)\s+instructions`),
	regexp.MustCompile(`(?i)new\s+system\s+prompt`),
	regexp.MustCompile(`(?i)override\s+(previous|all|safety)\s+instructions`),
}

// SubdirectoryHintTracker tracks which directories the agent has visited
// and loads context hints on first access.
type SubdirectoryHintTracker struct {
	workingDir  string
	loadedDirs  map[string]bool
	projectRoot string // don't walk above this
	mu          sync.Mutex
}

// NewSubdirectoryHintTracker creates a new tracker rooted at the given working directory.
// The working directory is pre-marked as loaded (startup context handles it).
func NewSubdirectoryHintTracker(workingDir string) *SubdirectoryHintTracker {
	abs, _ := filepath.Abs(workingDir)
	t := &SubdirectoryHintTracker{
		workingDir:  abs,
		loadedDirs:  make(map[string]bool),
		projectRoot: abs,
	}
	t.loadedDirs[abs] = true
	return t
}

// CheckToolCall checks tool call arguments for new directories and loads hint files.
// Returns formatted hint text to append to the tool result, or empty string.
func (t *SubdirectoryHintTracker) CheckToolCall(toolName string, args map[string]any) string {
	dirs := t.extractDirectories(toolName, args)
	if len(dirs) == 0 {
		return ""
	}

	var hints []string
	for _, dir := range dirs {
		if h := t.loadHintsForDirectory(dir); h != "" {
			hints = append(hints, h)
		}
	}

	if len(hints) == 0 {
		return ""
	}

	return "\n\n" + strings.Join(hints, "\n\n")
}

// extractDirectories extracts directory paths from tool call arguments.
func (t *SubdirectoryHintTracker) extractDirectories(toolName string, args map[string]any) []string {
	seen := make(map[string]bool)
	var dirs []string

	// Direct path arguments
	for _, key := range PathArgKeys {
		val, ok := args[key]
		if !ok {
			continue
		}
		s, ok := val.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		t.addPathCandidate(s, seen, &dirs)
	}

	// Shell commands — extract path-like tokens
	if CommandTools[toolName] {
		cmd, _ := args["command"].(string)
		if cmd != "" {
			t.extractPathsFromCommand(cmd, seen, &dirs)
		}
	}

	return dirs
}

// addPathCandidate resolves a raw path and adds its directory (and ancestors) as candidates.
func (t *SubdirectoryHintTracker) addPathCandidate(rawPath string, seen map[string]bool, dirs *[]string) {
	p := rawPath

	// Expand ~
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		if home != "" {
			p = strings.Replace(p, "~", home, 1)
		}
	}

	// Make absolute
	if !filepath.IsAbs(p) {
		p = filepath.Join(t.workingDir, p)
	}
	p = filepath.Clean(p)

	// Use parent if it looks like a file path
	if ext := filepath.Ext(p); ext != "" {
		p = filepath.Dir(p)
	} else if info, err := os.Stat(p); err == nil && !info.IsDir() {
		p = filepath.Dir(p)
	}

	// Walk up ancestors — stop at already-loaded or project root
	for i := 0; i < MaxAncestorWalk; i++ {
		if t.isLoaded(p) || seen[p] {
			break
		}
		if p == t.projectRoot || !strings.HasPrefix(p, t.projectRoot) {
			// Don't scan above project root
			break
		}
		if t.isValidSubdir(p) {
			seen[p] = true
			*dirs = append(*dirs, p)
		}
		parent := filepath.Dir(p)
		if parent == p {
			break // filesystem root
		}
		p = parent
	}
}

// extractPathsFromCommand extracts path-like tokens from a shell command string.
func (t *SubdirectoryHintTracker) extractPathsFromCommand(cmd string, seen map[string]bool, dirs *[]string) {
	// Simple tokenization: split on whitespace
	tokens := strings.Fields(cmd)
	for _, token := range tokens {
		// Skip flags
		if strings.HasPrefix(token, "-") {
			continue
		}
		// Must look like a path
		if !pathLikeToken.MatchString(token) {
			continue
		}
		// Skip URLs
		if urlPattern.MatchString(token) {
			continue
		}
		t.addPathCandidate(token, seen, dirs)
	}
}

func (t *SubdirectoryHintTracker) isLoaded(dir string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.loadedDirs[dir]
}

func (t *SubdirectoryHintTracker) isValidSubdir(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// loadHintsForDirectory loads hint files from a directory. Returns formatted text or empty string.
func (t *SubdirectoryHintTracker) loadHintsForDirectory(dir string) string {
	t.mu.Lock()
	t.loadedDirs[dir] = true
	t.mu.Unlock()

	for _, filename := range HintFilenames {
		hintPath := filepath.Join(dir, filename)
		content, err := os.ReadFile(hintPath)
		if err != nil {
			continue
		}

		text := strings.TrimSpace(string(content))
		if text == "" {
			continue
		}

		// Security scan for prompt injection
		if containsInjection(text) {
			continue
		}

		// Truncate if too long
		if len(text) > MaxHintChars {
			text = text[:MaxHintChars] +
				fmt.Sprintf("\n\n[...truncated %s: %d chars total]", filename, len(content))
		}

		// Best-effort relative path for display
		relPath := hintPath
		if rel, err := filepath.Rel(t.workingDir, hintPath); err == nil {
			relPath = rel
		}

		return fmt.Sprintf("[Subdirectory context discovered: %s]\n%s", relPath, text)
	}

	return ""
}

// containsInjection checks text for common prompt injection patterns.
func containsInjection(text string) bool {
	for _, p := range injectionPatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}
