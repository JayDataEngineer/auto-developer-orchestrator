package llama

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/auto-developer-orchestrator/backend/internal/sandbox"
	"go.uber.org/zap"
)

// SandboxFileOps provides structured file operations inside a Docker sandbox.
// All operations execute inside the container via Manager.ExecInSandbox().
// Shared by file_read, file_write, file_edit, file_grep, file_glob tools.
type SandboxFileOps struct {
	Manager   *sandbox.Manager
	SandboxID string
	Logger    *zap.Logger

	mu        sync.Mutex
	readCache map[string]string // path → content hash (must-read-before-edit)
	rgReady   bool              // true after ripgrep installed
}

// NewSandboxFileOps creates a new SandboxFileOps for the given sandbox.
func NewSandboxFileOps(mgr *sandbox.Manager, sandboxID string, logger *zap.Logger) *SandboxFileOps {
	return &SandboxFileOps{
		Manager:   mgr,
		SandboxID: sandboxID,
		Logger:    logger,
		readCache: make(map[string]string),
	}
}

// exec runs a command in the sandbox and returns stdout.
func (f *SandboxFileOps) exec(ctx context.Context, cmd string) (string, error) {
	return f.Manager.ExecInSandbox(ctx, f.SandboxID, []string{"bash", "-c", cmd})
}

// --- ReadFile ---

// ReadFile reads a file from the sandbox with line numbers.
// offset and limit are 1-based line numbers (0 means "from start" / "all").
// Returns the content with line numbers, the total line count, or an error.
func (f *SandboxFileOps) ReadFile(ctx context.Context, path string, offset, limit int) (string, int, error) {
	if err := sandbox.ValidatePath(path); err != nil {
		return "", 0, err
	}

	// Use cat -n for line numbers, then optionally slice
	output, err := f.exec(ctx, fmt.Sprintf("cat -n '%s'", sandbox.ShellEscape(path)))
	if err != nil {
		return "", 0, fmt.Errorf("failed to read %s: %w", path, err)
	}

	lines := strings.Split(output, "\n")
	totalLines := len(lines)

	// Cache the content hash for must-read-before-edit
	content, _ := f.exec(ctx, fmt.Sprintf("cat '%s'", sandbox.ShellEscape(path)))
	hash := sha256.Sum256([]byte(content))
	f.mu.Lock()
	f.readCache[path] = fmt.Sprintf("%x", hash[:8])
	f.mu.Unlock()

	// Apply offset/limit
	start := 0
	if offset > 0 {
		start = offset - 1 // 1-based to 0-based
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}

	if start >= len(lines) {
		start = len(lines)
	}

	result := strings.Join(lines[start:end], "\n")
	return result, totalLines, nil
}

// --- WriteFile ---

// WriteFile creates or overwrites a file in the sandbox.
// Uses base64 pipe to safely handle special characters and binary data.
func (f *SandboxFileOps) WriteFile(ctx context.Context, path, content string) error {
	if err := sandbox.ValidatePath(path); err != nil {
		return err
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if _, err := f.exec(ctx, fmt.Sprintf("mkdir -p '%s'", sandbox.ShellEscape(dir))); err != nil {
		return fmt.Errorf("failed to create parent dir: %w", err)
	}

	// Transfer via base64 pipe (safe for all content types)
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	cmd := fmt.Sprintf("echo '%s' | base64 -d > '%s'", encoded, sandbox.ShellEscape(path))
	if _, err := f.exec(ctx, cmd); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	// Mark as read so subsequent edits work
	hash := sha256.Sum256([]byte(content))
	f.mu.Lock()
	f.readCache[path] = fmt.Sprintf("%x", hash[:8])
	f.mu.Unlock()

	return nil
}

// --- EditFile ---

// EditFile replaces an exact string in a file.
// Enforces must-read-before-edit: the file must have been read first.
// Returns the number of replacements made.
func (f *SandboxFileOps) EditFile(ctx context.Context, path, oldString, newString string, replaceAll bool) (int, error) {
	if err := sandbox.ValidatePath(path); err != nil {
		return 0, err
	}

	// Must-read-before-edit check
	f.mu.Lock()
	_, read := f.readCache[path]
	f.mu.Unlock()
	if !read {
		return 0, fmt.Errorf("file %s has not been read yet. Call file_read first before editing.", path)
	}

	// Count occurrences of old string
	countCmd := fmt.Sprintf("grep -c -F '%s' '%s' 2>/dev/null || echo 0",
		sandbox.ShellEscape(oldString), sandbox.ShellEscape(path))
	countOut, _ := f.exec(ctx, countCmd)
	countOut = strings.TrimSpace(countOut)
	occurrences, _ := strconv.Atoi(countOut)
	if occurrences == 0 {
		return 0, fmt.Errorf("old_string not found in %s. The file may have changed since last read.", path)
	}
	if occurrences > 1 && !replaceAll {
		return 0, fmt.Errorf("old_string found %d times in %s. Set replace_all=true to replace all occurrences, or provide a more specific old_string that is unique.", occurrences, path)
	}

	// Perform replacement using a heredoc-based approach for safe string handling
	// Write the sed script to a temp file to avoid shell escaping issues
	script := fmt.Sprintf("s%s%s%s%s", "\x00", oldString, "\x00", newString)
	if replaceAll {
		script += "\x00g"
	} else {
		script += "\x00"
	}

	// Use perl for replacement (handles multiline, special chars better than sed)
	// Escape for single-quoted shell string
	escapedOld := perlEscape(oldString)
	escapedNew := perlEscape(newString)

	replaceFlag := ""
	if replaceAll {
		replaceFlag = "g"
	}

	editCmd := fmt.Sprintf(
		"perl -pi -e 's/\\Q%s\\E/%s/%s' '%s'",
		escapedOld, escapedNew, replaceFlag, sandbox.ShellEscape(path),
	)
	if _, err := f.exec(ctx, editCmd); err != nil {
		return 0, fmt.Errorf("edit failed for %s: %w", path, err)
	}

	// Update read cache with new content
	newContent, _ := f.exec(ctx, fmt.Sprintf("cat '%s'", sandbox.ShellEscape(path)))
	hash := sha256.Sum256([]byte(newContent))
	f.mu.Lock()
	f.readCache[path] = fmt.Sprintf("%x", hash[:8])
	f.mu.Unlock()

	return occurrences, nil
}

// perlEscape escapes a string for use in a perl replacement expression.
func perlEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `/`, `\/`)
	s = strings.ReplaceAll(s, "'", "\\'")
	return s
}

// --- Grep ---

// Grep searches files in the sandbox using ripgrep.
// Installs rg on first call if not present.
// outputMode: "content", "files_with_matches", or "count".
func (f *SandboxFileOps) Grep(ctx context.Context, pattern, path, glob, outputMode string, contextLines int, caseInsensitive bool, headLimit int) (string, error) {
	if path == "" {
		path = "/sandbox/workspace"
	}
	if err := sandbox.ValidatePath(path); err != nil {
		return "", err
	}

	// Install ripgrep on first call
	if err := f.ensureRipgrep(ctx); err != nil {
		// Fall back to grep -rn
		return f.grepFallback(ctx, pattern, path, caseInsensitive, headLimit)
	}

	// Build rg command
	var args []string
	args = append(args, "--no-heading")

	switch outputMode {
	case "files_with_matches":
		args = append(args, "-l")
	case "count":
		args = append(args, "-c")
	default: // "content"
		args = append(args, "-n")
	}

	if caseInsensitive {
		args = append(args, "-i")
	}
	if contextLines > 0 {
		args = append(args, fmt.Sprintf("-C%d", contextLines))
	}
	if glob != "" {
		args = append(args, "--glob", glob)
	}
	if headLimit > 0 {
		args = append(args, fmt.Sprintf("-m%d", headLimit))
	}

	args = append(args, "--max-columns", "500") // Truncate long lines
	args = append(args, pattern, path)

	cmd := fmt.Sprintf("rg %s", shellJoin(args...))
	output, err := f.exec(ctx, cmd)
	if err != nil {
		// rg returns exit code 1 for no matches — not an error
		if strings.Contains(err.Error(), "exit code 1") {
			return "No matches found.", nil
		}
		return "", fmt.Errorf("grep failed: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return "No matches found.", nil
	}

	// Cap output size
	if len(output) > 50000 {
		output = output[:50000] + "\n... [truncated]"
	}

	return output, nil
}

// ensureRipgrep installs rg in the sandbox if not present.
func (f *SandboxFileOps) ensureRipgrep(ctx context.Context) error {
	f.mu.Lock()
	if f.rgReady {
		f.mu.Unlock()
		return nil
	}
	f.mu.Unlock()

	// Check if rg is already installed
	if _, err := f.exec(ctx, "which rg 2>/dev/null"); err == nil {
		f.mu.Lock()
		f.rgReady = true
		f.mu.Unlock()
		return nil
	}

	// Install ripgrep via apt
	f.Logger.Info("Installing ripgrep in sandbox", zap.String("sandbox", f.SandboxID))
	if _, err := f.exec(ctx, "apt-get update -qq && apt-get install -y -qq ripgrep 2>/dev/null"); err != nil {
		return fmt.Errorf("failed to install ripgrep: %w", err)
	}

	f.mu.Lock()
	f.rgReady = true
	f.mu.Unlock()
	return nil
}

// grepFallback uses grep -rn when ripgrep is unavailable.
func (f *SandboxFileOps) grepFallback(ctx context.Context, pattern, path string, caseInsensitive bool, headLimit int) (string, error) {
	args := "-rn"
	if caseInsensitive {
		args += "i"
	}
	cmd := fmt.Sprintf("grep %s '%s' '%s'", args, sandbox.ShellEscape(pattern), sandbox.ShellEscape(path))
	if headLimit > 0 {
		cmd += fmt.Sprintf(" | head -%d", headLimit)
	}
	output, err := f.exec(ctx, cmd)
	if err != nil {
		return "No matches found.", nil
	}
	return strings.TrimSpace(output), nil
}

// --- Glob ---

// Glob finds files matching a pattern in the sandbox.
// Pattern uses glob syntax: *.go, **/*.ts, etc.
// Returns file paths sorted by modification time (newest first).
func (f *SandboxFileOps) Glob(ctx context.Context, pattern, searchPath string) ([]string, error) {
	if searchPath == "" {
		searchPath = "/sandbox/workspace"
	}
	if err := sandbox.ValidatePath(searchPath); err != nil {
		return nil, err
	}

	// Use find for pattern matching, sort by mtime
	cmd := fmt.Sprintf(
		"find '%s' -name '%s' -type f -printf '%%T@\\t%%p\\n' 2>/dev/null | sort -rn | head -100 | cut -f2",
		sandbox.ShellEscape(searchPath), sandbox.ShellEscape(pattern),
	)
	output, err := f.exec(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("glob failed: %w", err)
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	lines := strings.Split(output, "\n")
	// Convert absolute paths to relative (from searchPath) to save tokens
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rel, err := filepath.Rel(searchPath, line)
		if err == nil {
			result = append(result, rel)
		} else {
			result = append(result, line)
		}
	}

	sort.Strings(result)
	return result, nil
}

// --- FileExists ---

// FileExists checks if a path exists in the sandbox.
func (f *SandboxFileOps) FileExists(ctx context.Context, path string) (bool, error) {
	if err := sandbox.ValidatePath(path); err != nil {
		return false, err
	}
	_, err := f.exec(ctx, fmt.Sprintf("test -e '%s'", sandbox.ShellEscape(path)))
	return err == nil, nil
}

// --- HasRead checks if a file has been read (for must-read-before-edit) ---

// HasRead returns whether a file has been read in this session.
func (f *SandboxFileOps) HasRead(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.readCache[path]
	return ok
}

// --- helpers ---

// shellJoin joins arguments into a shell command string.
func shellJoin(args ...string) string {
	var parts []string
	for _, a := range args {
		parts = append(parts, fmt.Sprintf("'%s'", sandbox.ShellEscape(a)))
	}
	return strings.Join(parts, " ")
}
