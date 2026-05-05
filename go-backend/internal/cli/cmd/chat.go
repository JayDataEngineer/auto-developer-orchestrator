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

	// Pass through --server and --project flags
	tuiArgs := []string{"run", "src/main.ts"}
	if serverURL != "" && serverURL != "http://localhost:3847" {
		tuiArgs = append(tuiArgs, "--server", serverURL)
	}
	if projectName != "" {
		tuiArgs = append(tuiArgs, "--project", projectName)
	}
	// Forward any additional positional args as --cwd
	for _, arg := range args {
		tuiArgs = append(tuiArgs, "--cwd", arg)
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
