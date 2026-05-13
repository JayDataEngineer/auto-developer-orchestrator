package extensions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Discover scans a directory for extensions. Each subdirectory with a server.ts
// file (or an extension.yaml that specifies a different entry point) is an extension.
// Returns discovered extensions sorted by name. Errors are logged, not fatal.
func Discover(dir string) ([]Extension, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no extensions dir is fine
		}
		return nil, fmt.Errorf("read extensions dir: %w", err)
	}

	var exts []Extension
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip hidden dirs
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}

		extDir := filepath.Join(dir, name)
		ext, err := discoverOne(extDir)
		if err != nil {
			// Log but don't fail — one bad extension shouldn't break the system
			fmt.Fprintf(os.Stderr, "extensions: skip %s: %v\n", name, err)
			continue
		}
		if ext != nil {
			exts = append(exts, *ext)
		}
	}

	return exts, nil
}

// discoverOne loads a single extension from its directory.
func discoverOne(dir string) (*Extension, error) {
	base := filepath.Base(dir)

	// Try reading extension.yaml
	yamlPath := filepath.Join(dir, "extension.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read extension.yaml: %w", err)
		}
		// No manifest — check for server.ts and use defaults
		if _, err := os.Stat(filepath.Join(dir, "server.ts")); err != nil {
			return nil, nil // not an extension
		}
		return &Extension{
			Name:    sanitizeName(base),
			Version: "0.0.0",
			Dir:     dir,
			Server: ServerConfig{
				Command: "bun",
				Args:    []string{"run", "server.ts"},
				Timeout: 15,
			},
		}, nil
	}

	var cfg ExtensionConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse extension.yaml: %w", err)
	}

	name := cfg.Name
	if name == "" {
		name = sanitizeName(base)
	}

	// Defaults
	command := cfg.Server.Command
	if command == "" {
		command = "bun"
	}
	args := cfg.Server.Args
	if len(args) == 0 {
		args = []string{"run", "server.ts"}
	}
	timeout := cfg.Server.Timeout
	if timeout == 0 {
		timeout = 15
	}

	// Verify entry point exists
	entryPoint := filepath.Join(dir, args[len(args)-1])
	if _, err := os.Stat(entryPoint); err != nil {
		return nil, fmt.Errorf("entry point %s not found", args[len(args)-1])
	}

	return &Extension{
		Name:        sanitizeName(name),
		Version:     cfg.Version,
		Description: cfg.Description,
		Dir:         dir,
		Server: ServerConfig{
			Command: command,
			Args:    args,
			Timeout: timeout,
		},
	}, nil
}

// sanitizeName converts a directory name to a valid extension name.
// Replaces spaces and special chars with underscores, lowercases.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	// Remove any character that's not alphanumeric or underscore
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
