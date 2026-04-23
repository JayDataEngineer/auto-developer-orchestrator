package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidatePath ensures a path is absolute and under allowed directories.
// Only /sandbox/ and /tmp/ are permitted to prevent host filesystem access.
func ValidatePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must be absolute")
	}
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, "/sandbox/") && !strings.HasPrefix(clean, "/tmp/") {
		return fmt.Errorf("path must be under /sandbox/ or /tmp/")
	}
	return nil
}

// ShellEscape wraps a string in single quotes, escaping embedded single quotes.
// Shared by file handlers and tool executors.
func ShellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}
