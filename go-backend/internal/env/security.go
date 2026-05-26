package env

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SecurityGuard enforces path-based write denial across all environments.
// Ported from Hermes file_safety.py.
type SecurityGuard struct {
	denyExact  map[string]bool // exact paths (after $HOME expansion)
	denyPrefix []string        // path prefixes (after $HOME expansion)
	homeDir    string          // cached home directory
}

// NewSecurityGuard creates a guard with the default deny lists.
func NewSecurityGuard() *SecurityGuard {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/root"
	}

	g := &SecurityGuard{
		homeDir:    home,
		denyExact:  make(map[string]bool),
		denyPrefix: make([]string, 0, len(defaultDenyPrefixes)),
	}

	// Expand $HOME in exact deny paths
	for _, p := range defaultDenyExact {
		expanded := expandHome(p, home)
		g.denyExact[expanded] = true
	}

	// Expand $HOME in prefix deny paths
	for _, p := range defaultDenyPrefixes {
		g.denyPrefix = append(g.denyPrefix, expandHome(p, home))
	}

	return g
}

// CheckWritePath returns an error if path is denied for writing.
// Paths are normalized and checked against both exact and prefix lists.
func (g *SecurityGuard) CheckWritePath(path string) error {
	// Normalize the path
	absPath := path

	// Expand ~ to home directory
	if strings.HasPrefix(absPath, "~/") {
		absPath = filepath.Join(g.homeDir, absPath[2:])
	} else if absPath == "~" {
		absPath = g.homeDir
	}

	if !strings.HasPrefix(absPath, "/") {
		absPath = filepath.Join(g.homeDir, absPath)
	}
	absPath = filepath.Clean(absPath)

	// Check exact matches
	if g.denyExact[absPath] {
		return fmt.Errorf("write to %q denied: sensitive file", filepath.Base(absPath))
	}

	// Check prefix matches
	for _, prefix := range g.denyPrefix {
		if strings.HasPrefix(absPath, prefix) {
			return fmt.Errorf("write to %q denied: sensitive directory", filepath.Base(prefix))
		}
	}

	return nil
}

// CheckCommand scans a shell command string for attempts to write to denied paths.
// Catches patterns like: echo foo > ~/.ssh/authorized_keys, cat > ~/.ssh/config, etc.
func (g *SecurityGuard) CheckCommand(cmd string) error {
	// Expand ~ to $HOME for matching
	expanded := strings.ReplaceAll(cmd, "~/", g.homeDir+"/")

	// Find all redirect targets in the command
	for _, match := range redirectPattern.FindAllStringSubmatch(expanded, -1) {
		if len(match) < 2 {
			continue
		}
		target := strings.TrimSpace(match[1])
		// Strip quotes
		target = strings.Trim(target, `'"`)
		if err := g.CheckWritePath(target); err != nil {
			return fmt.Errorf("command blocked: %w", err)
		}
	}

	// Check for tee/mv/cp targeting sensitive paths
	for _, match := range teePattern.FindAllStringSubmatch(expanded, -1) {
		if len(match) < 2 {
			continue
		}
		target := strings.TrimSpace(match[1])
		target = strings.Trim(target, `'"`)
		if err := g.CheckWritePath(target); err != nil {
			return fmt.Errorf("command blocked: %w", err)
		}
	}

	return nil
}

func expandHome(path, home string) string {
	return strings.ReplaceAll(path, "$HOME", home)
}

// redirectPattern matches shell redirect targets: > file, >> file, >file
var redirectPattern = regexp.MustCompile(`(?:>{1,2}|>>)\s*([^\s;&|]+)`)

// teePattern matches tee/mv/cp commands with file targets
var teePattern = regexp.MustCompile(`(?:\btee\b|\bmv\b|\bcp\b)\s+(?:-[a-zA-Z]+\s+)*([^\s;&|]+)`)

// Default deny lists — ported from Hermes file_safety.py

var defaultDenyExact = []string{
	// SSH keys and config
	"$HOME/.ssh/authorized_keys",
	"$HOME/.ssh/authorized_keys2",
	"$HOME/.ssh/id_rsa",
	"$HOME/.ssh/id_ed25519",
	"$HOME/.ssh/id_ecdsa",
	"$HOME/.ssh/config",
	"$HOME/.ssh/known_hosts",
	// Cloud credentials
	"$HOME/.aws/credentials",
	"$HOME/.aws/config",
	"$HOME/.kube/config",
	"$HOME/.docker/config.json",
	// Credential files
	"$HOME/.npmrc",
	"$HOME/.pypirc",
	"$HOME/.netrc",
	"$HOME/.pgpass",
	"$HOME/.gitconfig",
	"$HOME/.git-credentials",
	// Shell RC files
	"$HOME/.bashrc",
	"$HOME/.bash_profile",
	"$HOME/.profile",
	"$HOME/.zshrc",
	"$HOME/.zprofile",
	// System files (for non-container environments)
	"/etc/sudoers",
	"/etc/passwd",
	"/etc/shadow",
}

var defaultDenyPrefixes = []string{
	"$HOME/.ssh/",
	"$HOME/.aws/",
	"$HOME/.kube/",
	"$HOME/.gnupg/",
	"$HOME/.config/gcloud/",
	"$HOME/.config/gh/",
	"$HOME/.docker/",
	"$HOME/.azure/",
	"/etc/sudoers.d/",
	"/etc/systemd/",
}
