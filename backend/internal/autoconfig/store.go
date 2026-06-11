package autoconfig

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ArtifactStore is the contract for all auto-config domains.
// Every auto-config domain (workers, profiles, schedules) implements this.
// The tool layer is a thin adapter: operation → store method.
// Put validates before writing — invalid configs cannot be persisted.
type ArtifactStore interface {
	// List returns all artifact names.
	List(ctx context.Context) (any, error)

	// Get returns a single artifact by name.
	Get(ctx context.Context, name string) (any, error)

	// Put creates or replaces an artifact. Validates before writing.
	Put(ctx context.Context, name string, spec map[string]any) (any, error)

	// Delete removes an artifact.
	Delete(ctx context.Context, name string) error
}

// namePattern enforces safe artifact names: alphanumeric, dash, underscore.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*[a-z0-9]$`)

// ValidateName checks that a name is safe for use as a filename.
// Rejects path traversal, empty names, and non-alphanumeric characters.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("name %q contains path separators", name)
	}
	// Allow short names (2+ chars)
	if len(name) < 2 {
		return fmt.Errorf("name %q too short (minimum 2 characters)", name)
	}
	if len(name) > 64 {
		return fmt.Errorf("name %q too long (maximum 64 characters)", name)
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("name %q must match [a-z][a-z0-9_-]* (lowercase alphanumeric, dash, underscore)", name)
	}
	return nil
}

// SafePath joins base dir with validated name and extension.
// Returns error if the resulting path escapes base dir.
func SafePath(baseDir, name, ext string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	path := filepath.Join(baseDir, name+ext)
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return path, nil
}

// TextResult returns a standard success response.
func TextResult(message string) map[string]any {
	return map[string]any{"message": message}
}

// ListResult returns a standard list response.
func ListResult(items []string, count int) map[string]any {
	return map[string]any{"items": items, "count": count}
}

// pluralS returns "s" for n != 1.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// pluralIES returns "y" for n == 1, "ies" otherwise (e.g. capability/capabilities).
func pluralIES(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
