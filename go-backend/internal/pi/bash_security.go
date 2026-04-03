package pi

import (
	"path/filepath"
	"strings"
)

// BashRiskLevel classifies the danger level of a shell command.
type BashRiskLevel string

const (
	RiskSafe        BashRiskLevel = "safe"        // read-only, no side effects
	RiskWrite       BashRiskLevel = "write"       // modifies files within workspace
	RiskDestructive BashRiskLevel = "destructive" // irreversible or system-wide effects
	RiskNetwork     BashRiskLevel = "network"     // network access (curl, wget, etc.)
	RiskUnknown     BashRiskLevel = "unknown"     // cannot classify
)

// BashSecurityResult is the output of command classification.
type BashSecurityResult struct {
	Risk          BashRiskLevel
	Category      ToolCategory
	Reason        string
	Allowed       bool
	NeedsConfirm  bool
	BlockedReason string // non-empty if Blocked
}

// destructivePatterns match commands that cause irreversible damage.
var destructivePatterns = []struct {
	pattern string
	reason  string
}{
	{"rm -rf /", "refusing to delete root filesystem"},
	{"rm -rf ~", "refusing to delete home directory"},
	{"rm -rf /*", "refusing to delete root filesystem"},
	{"rm -r /", "refusing to delete root filesystem"},
	{"git push --force", "force push can overwrite remote history"},
	{"git push -f", "force push can overwrite remote history"},
	{"git reset --hard", "hard reset discards uncommitted changes"},
	{"git clean -f", "git clean removes untracked files permanently"},
	{"git checkout -- .", "discards all working directory changes"},
	{"drop table", "DROP TABLE is a destructive database operation"},
	{"drop database", "DROP DATABASE is a destructive database operation"},
	{"truncate table", "TRUNCATE TABLE removes all data"},
	{":(){ :|:& };:", "fork bomb detected"},
	{"mkfs", "mkfs formats a filesystem"},
	{"dd if=", "dd can overwrite raw devices"},
	{"> /dev/sd", "direct device write detected"},
	{"chmod -R 777 /", "refusing to recursively chmod root"},
	{"chown -R ", "recursive chown on system paths"},
	{"kill -9 1", "refusing to kill init process"},
	{"shutdown", "shutdown command detected"},
	{"reboot", "reboot command detected"},
	{"halt", "halt command detected"},
	{"poweroff", "poweroff command detected"},
}

// destructivePrefixes match command prefixes that are always destructive.
var destructivePrefixes = []string{
	"rm -rf /",
	"rm -r /",
	"git push --force",
	"git push -f",
	"git reset --hard",
	"mkfs.",
	"dd if=",
}

// readOnlyCommands are commands that only read data.
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "less": true, "more": true,
	"grep": true, "rg": true, "find": true, "wc": true, "sort": true, "uniq": true,
	"diff": true, "comm": true, "file": true, "stat": true, "du": true, "df": true,
	"echo": true, "printf": true, "pwd": true, "which": true, "whereis": true,
	"env": true, "printenv": true, "id": true, "whoami": true, "hostname": true,
	"uname": true, "date": true, "uptime": true,
	"git status": true, "git log": true, "git diff": true, "git show": true,
	"git branch": true, "git tag": true, "git remote": true, "git stash list": true,
	"go list": true, "go vet": true, "go test": true, "go doc": true, "go version": true,
	"python --version": true, "python3 --version": true, "node --version": true,
	"npm list": true, "npm view": true,
	"curl": true, "wget": true,
	"type": true, "get-content": true,
}

// writeCommands are commands that modify files (but aren't destructive).
var writeCommands = map[string]bool{
	"mkdir": true, "touch": true, "cp": true, "mv": true,
	"git add": true, "git commit": true, "git stash": true, "git checkout -b": true,
	"git merge": true, "git rebase": true, "git cherry-pick": true,
	"go build": true, "go run": true, "go mod tidy": true, "go get": true,
	"npm install": true, "npm run": true, "pip install": true,
	"docker build": true, "docker compose": true,
	"make": true,
}

// ClassifyCommand analyzes a bash command and returns its risk classification.
func ClassifyCommand(command string) BashSecurityResult {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return BashSecurityResult{
			Risk:     RiskSafe,
			Category: CategoryExecute,
			Allowed:  true,
		}
	}

	lower := strings.ToLower(trimmed)

	// 1. Check destructive patterns first (highest priority)
	for _, dp := range destructivePatterns {
		if strings.Contains(lower, dp.pattern) {
			return BashSecurityResult{
				Risk:          RiskDestructive,
				Category:      CategoryDestructive,
				Reason:        dp.reason,
				Allowed:       false,
				NeedsConfirm:  true,
				BlockedReason: dp.reason,
			}
		}
	}

	// 2. Check destructive prefixes
	for _, prefix := range destructivePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return BashSecurityResult{
				Risk:          RiskDestructive,
				Category:      CategoryDestructive,
				Reason:        "destructive prefix: " + prefix,
				Allowed:       false,
				NeedsConfirm:  true,
				BlockedReason: "destructive command: " + prefix,
			}
		}
	}

	// 3. Check for redirections that write files (before read-only check,
	// since "echo hello > output.txt" is write despite echo being read-only)
	if containsWriteRedirection(lower) {
		return BashSecurityResult{
			Risk:         RiskWrite,
			Category:     CategoryWrite,
			Reason:       "command writes to file via redirection",
			Allowed:      true,
			NeedsConfirm: false,
		}
	}

	// 4. Check if command starts with a known read-only command
	baseCmd := extractBaseCommand(lower)
	if readOnlyCommands[baseCmd] || readOnlyCommands[trimmed] {
		return BashSecurityResult{
			Risk:     RiskSafe,
			Category: CategoryRead,
			Reason:   "read-only command: " + baseCmd,
			Allowed:  true,
		}
	}

	// 5. Check if command starts with a known write command
	if writeCommands[baseCmd] || writeCommands[trimmed] {
		return BashSecurityResult{
			Risk:         RiskWrite,
			Category:     CategoryWrite,
			Reason:       "write command: " + baseCmd,
			Allowed:      true,
			NeedsConfirm: false,
		}
	}

	// 6. Check for pipe to dangerous commands
	if strings.Contains(lower, "| rm") || strings.Contains(lower, "| shred") {
		return BashSecurityResult{
			Risk:          RiskDestructive,
			Category:      CategoryDestructive,
			Reason:        "pipe to destructive command",
			Allowed:       false,
			NeedsConfirm:  true,
			BlockedReason: "pipe to destructive command detected",
		}
	}

	// 7. Check for network commands
	if strings.HasPrefix(baseCmd, "curl") || strings.HasPrefix(baseCmd, "wget") ||
		strings.HasPrefix(baseCmd, "nc ") || strings.HasPrefix(baseCmd, "ncat") ||
		strings.HasPrefix(baseCmd, "ssh") || strings.HasPrefix(baseCmd, "scp") ||
		strings.HasPrefix(baseCmd, "rsync") {
		return BashSecurityResult{
			Risk:     RiskNetwork,
			Category: CategoryExecute,
			Reason:   "network command: " + baseCmd,
			Allowed:  true,
		}
	}

	// 8. Default: unknown risk, require confirmation in non-full-access mode
	return BashSecurityResult{
		Risk:         RiskUnknown,
		Category:     CategoryExecute,
		Reason:       "unclassified command",
		Allowed:      true,
		NeedsConfirm: true,
	}
}

// CheckPathSafety verifies a file path is safe to access within a project directory.
func CheckPathSafety(path string, projectDir string) (safe bool, reason string) {
	if projectDir == "" {
		return true, ""
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return false, "cannot resolve path"
	}

	projectAbs, err := filepath.Abs(projectDir)
	if err != nil {
		return false, "cannot resolve project path"
	}

	// Block path traversal
	rel, err := filepath.Rel(projectAbs, abs)
	if err != nil {
		return false, "cannot compute relative path"
	}
	if strings.HasPrefix(rel, "..") {
		return false, "path escapes project directory"
	}

	// Block sensitive system paths
	sensitivePaths := []string{
		"/etc/passwd", "/etc/shadow", "/etc/ssh",
		"/root/.ssh", "/home/*/.ssh",
		"/var/log", "/proc", "/sys",
	}
	for _, sp := range sensitivePaths {
		if strings.HasPrefix(abs, sp) || abs == sp {
			return false, "access to sensitive system path denied"
		}
	}

	return true, ""
}

// extractBaseCommand returns the first word (or first two words for compound commands).
func extractBaseCommand(lower string) string {
	parts := strings.Fields(lower)
	if len(parts) == 0 {
		return ""
	}

	// Handle compound commands like "git add", "go build"
	if len(parts) >= 2 {
		compound := parts[0] + " " + parts[1]
		if isKnownCompoundCommand(parts[0]) {
			return compound
		}
	}

	return parts[0]
}

// isKnownCompoundCommand returns true for commands that form compound base commands.
func isKnownCompoundCommand(cmd string) bool {
	compounds := map[string]bool{
		"git": true, "go": true, "npm": true, "npx": true,
		"yarn": true, "pnpm": true, "pip": true, "docker": true,
		"kubectl": true, "cargo": true, "rustup": true,
	}
	return compounds[cmd]
}

// containsWriteRedirection checks for > or >> redirections.
func containsWriteRedirection(lower string) bool {
	// Simple check: look for > or >> that aren't part of other tokens
	for i, ch := range lower {
		if ch == '>' {
			// Make sure it's not >> or part of a comparison
			if i > 0 && lower[i-1] == '-' {
				continue // e.g., "->" in some contexts
			}
			return true
		}
	}
	return false
}
