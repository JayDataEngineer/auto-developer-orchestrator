package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// runChat launches the pi-mono TUI (ts-tui-pi) via bun.
func runChat(cmd *cobra.Command, args []string) error {
	// Find the ts-tui-pi directory relative to the repo root.
	// The orch binary is at go-backend/orch, ts-tui-pi is at repo root.
	exe, _ := os.Executable()
	repoRoot := findRepoRoot(exe)

	tuiDir := filepath.Join(repoRoot, "ts-tui-pi")
	if _, err := os.Stat(filepath.Join(tuiDir, "src", "main.ts")); err != nil {
		// Fallback: look relative to cwd
		cwd, _ := os.Getwd()
		tuiDir = filepath.Join(cwd, "ts-tui-pi")
	}
	if _, err := os.Stat(filepath.Join(tuiDir, "src", "main.ts")); err != nil {
		return fmt.Errorf("ts-tui-pi not found. Expected at %s or relative to repo root.", tuiDir)
	}

	// Resolve --org to project path
	effectiveProject := projectName
	effectiveCwd := ""
	if orgName != "" {
		orgPath, err := resolveOrgPath(orgName)
		if err != nil {
			return err
		}
		// Use org directory as absolute path project and cwd
		effectiveProject = orgPath
		effectiveCwd = orgPath
	}

	// Pass through --server and --project flags
	tuiArgs := []string{"run", "src/main.ts"}
	if serverURL != "" && serverURL != "http://localhost:3847" {
		tuiArgs = append(tuiArgs, "--server", serverURL)
	}
	if effectiveProject != "" {
		tuiArgs = append(tuiArgs, "--project", effectiveProject)
	}
	if effectiveCwd != "" {
		tuiArgs = append(tuiArgs, "--cwd", effectiveCwd)
	}
	// Forward any additional positional args as --cwd (only if no --org)
	for _, arg := range args {
		if effectiveCwd == "" {
			tuiArgs = append(tuiArgs, "--cwd", arg)
		}
	}

	tuiCmd := exec.Command("bun", tuiArgs...)
	tuiCmd.Dir = tuiDir
	tuiCmd.Stdin = os.Stdin
	tuiCmd.Stdout = os.Stdout
	tuiCmd.Stderr = os.Stderr

	if err := tuiCmd.Run(); err != nil {
		return fmt.Errorf("tui exited: %w", err)
	}
	return nil
}

// findRepoRoot walks up from the binary path to find the repo root
// (looks for go.mod or .git).
func findRepoRoot(binPath string) string {
	dir := filepath.Dir(binPath)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "go-backend")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir || strings.HasSuffix(parent, "go-backend") {
			// If we're inside go-backend/, go up one more
			if strings.HasSuffix(parent, "go-backend") {
				return filepath.Dir(parent)
			}
			return dir
		}
		dir = parent
	}
}

// resolveOrgPath finds the directory for a named organization.
// Primary location: ~/.pux/orgs/<name>. Falls back to legacy locations.
// The directory must contain pux.yaml to be valid.
func resolveOrgPath(name string) (string, error) {
	// Alias mapping: --org code → dev-bot, etc.
	aliases := map[string]string{"code": "dev-bot", "dev": "dev-bot"}
	if resolved, ok := aliases[name]; ok {
		name = resolved
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".pux", "orgs", name),                    // primary
		filepath.Join(home, "Documents", "programs", "dev", name),    // legacy
		filepath.Join(home, "Documents", "projects", name, "pux-org"),
		filepath.Join(home, "Documents", "projects", name),
	}

	for _, dir := range candidates {
		puxYaml := filepath.Join(dir, "pux.yaml")
		if _, err := os.Stat(puxYaml); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("organization '%s' not found. Looked for pux.yaml in: %s", name, strings.Join(candidates, ", "))
}
