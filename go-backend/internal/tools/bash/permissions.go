package bash

import (
	"regexp"
	"strings"
)

// BashPermission describes the permission level for a bash command.
type BashPermission struct {
	Behavior string // "allow", "ask", "deny"
	Message  string // human-readable reason for ask/deny
}

// CheckBashPermission evaluates a command and returns the appropriate permission level.
// Used by the permission hook to make granular decisions about bash commands:
//
//   - "deny":  command is inherently unsafe — block with error
//   - "ask":   command is potentially destructive — prompt user for approval
//   - "allow": command is safe or read-only — execute without prompting
//
// The tool-level permission (auto/confirm/deny) is still checked by the hook;
// this function only evaluates the command content.
func CheckBashPermission(cmd string) BashPermission {
	// Strip leading env var assignments so VAR=val dangerous_cmd can't bypass
	stripped := stripEnvVars(cmd)
	lower := strings.ToLower(stripped)

	// 1. Hard-deny patterns — always block, no override
	for _, rule := range hardDenyRules {
		if rule.pattern.MatchString(lower) {
			return BashPermission{Behavior: "deny", Message: rule.message}
		}
	}

	// 2. Deny-with-override — block unless override pattern matches
	for _, rule := range overrideDenyRules {
		if rule.pattern.MatchString(lower) {
			if matchesAnyOverride(lower) {
				return BashPermission{Behavior: "allow"}
			}
			return BashPermission{Behavior: "deny", Message: rule.message}
		}
	}

	// 3. Dangerous patterns — ask user for confirmation
	// Check BEFORE read-only so destructive subcommands (e.g. git branch -D)
	// don't get caught by the read-only prefix match.
	for _, d := range dangerousPatterns {
		if d.pattern.MatchString(lower) {
			return BashPermission{Behavior: "ask", Message: d.message}
		}
	}

	// 4. Read-only commands — auto-approve (no shell operators = safe)
	if !containsShellOperators(stripped) && isReadOnly(lower) {
		return BashPermission{Behavior: "allow"}
	}

	// 5. Default — allow (tool-level permission system still applies)
	return BashPermission{Behavior: "allow"}
}

// stripEnvVars removes leading environment variable assignments.
// Prevents bypass via `ENV=val dangerous_command`.
func stripEnvVars(cmd string) string {
	// Match leading ENV=value assignments (including chained ENV1=a ENV2=b cmd)
	for {
		trimmed := strings.TrimSpace(cmd)
		// Check if it starts with a valid env var assignment
		idx := strings.IndexAny(trimmed, "=")
		if idx <= 0 {
			break
		}
		before := trimmed[:idx]
		// Before the = must be a valid identifier [a-zA-Z_][a-zA-Z0-9_]*
		if !isValidEnvVarName(before) {
			break
		}
		// Find the end of the value (space-separated, respecting quotes)
		rest := trimmed[idx+1:]
		nextSpace := findUnquotedSpace(rest)
		if nextSpace < 0 {
			// Not an env var assignment — it's the command itself with an arg
			break
		}
		// Skip this env var and continue checking
		cmd = rest[nextSpace:]
	}
	return strings.TrimSpace(cmd)
}

func isValidEnvVarName(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, c := range s {
		if i == 0 {
			if !isAlpha(c) && c != '_' {
				return false
			}
		} else {
			if !isAlphaNum(c) && c != '_' {
				return false
			}
		}
	}
	return true
}

func isAlpha(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAlphaNum(c rune) bool {
	return isAlpha(c) || (c >= '0' && c <= '9')
}

// findUnquotedSpace finds the first unquoted space in s.
// Returns the index, or -1 if not found (meaning s is the final command or value).
func findUnquotedSpace(s string) int {
	inSingle := false
	inDouble := false
	for i, c := range s {
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		if inDouble {
			if c == '"' {
				inDouble = false
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case ' ':
			return i
		}
	}
	return -1
}

// containsShellOperators returns true if the command contains shell operators
// that make it not truly read-only: pipes, redirects, compound commands, etc.
func containsShellOperators(cmd string) bool {
	// These operators would make a "read-only" command actually have side effects
	inSingle := false
	inDouble := false
	for _, c := range cmd {
		if inSingle {
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		if inDouble {
			if c == '"' {
				inDouble = false
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '|', '>', '<', ';', '&', '$', '`', '{', '}', '(':
			return true
		}
	}
	return false
}

// isReadOnly checks if a command is a known read-only command.
// The caller must verify no shell operators are present.
func isReadOnly(lower string) bool {
	parts := strings.Fields(lower)
	if len(parts) == 0 {
		return false
	}

	// Check two-word prefixes first (e.g. "git status")
	if len(parts) >= 2 {
		prefix := parts[0] + " " + parts[1]
		if readOnlyCommands[prefix] {
			return true
		}
	}

	return readOnlyCommands[parts[0]]
}

// matchesAnyOverride checks if the command matches any override pattern.
func matchesAnyOverride(lower string) bool {
	for _, ov := range overridePatterns {
		if ov.MatchString(lower) {
			return true
		}
	}
	return false
}

// cmdPos matches the start of a command position: beginning of string, or
// after ; && || | (shell operators). Prevents false positives when dangerous
// keywords appear inside string arguments like echo 'nmap'.
const cmdPos = `(?:^|;\s*|&&\s*|\|\|\s*|\|\s*)`

// ── Pattern type aliases ──

type denyRule struct {
	pattern *regexp.Regexp
	message string
}

type dangerousRule struct {
	pattern *regexp.Regexp
	message string
}

// ── Hard-deny: always blocked, no override possible ──

var hardDenyRules = []denyRule{
	// rm -rf / (bare root)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `rm\s+-[a-zA-Z]*f[a-zA-Z]*\s+/(?:\s|$)`),
		message: "recursive force-delete of root filesystem is not allowed",
	},
	// rm -rf /etc, /usr, /var, /bin, /sbin, /boot, /dev, /proc, /sys, /lib, /root
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `rm\s+-[a-zA-Z]*f[a-zA-Z]*\s+/(?:etc|usr|var|bin|sbin|boot|dev|proc|sys|lib|lib64|root)(?:/|\s|$)`),
		message: "recursive force-delete of system directories is not allowed",
	},
	// mkfs
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `mkfs\b`),
		message: "filesystem format commands are not allowed",
	},
	// dd to block devices
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `dd\s+.*(?:if|of)=/dev/(?:sd|nvme|vd)`),
		message: "raw disk operations on block devices are not allowed",
	},
	// Redirect to block device
	{
		pattern: regexp.MustCompile(`(?i)>\s*/dev/sd`),
		message: "writing directly to block devices is not allowed",
	},
	// Fork bomb
	{
		pattern: regexp.MustCompile(`(?i):\(\)\s*\{\s*:\|:\&\s*\}\s*;:`),
		message: "fork bomb patterns are not allowed",
	},
	// shred
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `shred\b`),
		message: "file shredding is not allowed",
	},
	// sudo su (always block — root shell)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+su\b`),
		message: "switching to root shell is not allowed",
	},
	// sudo bash/sh/zsh (always block — root shell)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+bash\b`),
		message: "spawning root shell is not allowed",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+sh\b`),
		message: "spawning root shell is not allowed",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+zsh\b`),
		message: "spawning root shell is not allowed",
	},
	// passwd
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `passwd\b`),
		message: "password changes are not allowed",
	},
	// Network attack tools
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `nmap\b`),
		message: "network scanning tools are not allowed",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `hydra\b`),
		message: "password cracking tools are not allowed",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `aircrack\b`),
		message: "WiFi cracking tools are not allowed",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `netcat\s+-[elp]`),
		message: "netcat in listen/reverse shell mode is not allowed",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `nc\s+-[elp]`),
		message: "netcat in listen/reverse shell mode is not allowed",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `python[23]?\b.*-c.*import\s+socket.*\b(bind|listen)\b`),
		message: "raw socket bind/listen patterns are not allowed",
	},
	// chmod -R 777 (always block)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `chmod\s+-[a-zA-Z]*R[a-zA-Z]*\s+777\b`),
		message: "recursively setting world-writable permissions is not allowed",
	},
}

// ── Deny with possible override ──

var overrideDenyRules = []denyRule{
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+rm\b`),
		message: "sudo rm is not allowed (use without sudo for project files)",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+chmod\s+777\b`),
		message: "setting world-writable permissions with sudo is not allowed",
	},
}

var overridePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bsudo\s+(apt|apt-get)\b`),
	regexp.MustCompile(`(?i)\bsudo\s+yum\b`),
	regexp.MustCompile(`(?i)\bsudo\s+dnf\b`),
	regexp.MustCompile(`(?i)\bsudo\s+systemctl\b`),
	regexp.MustCompile(`(?i)\bsudo\s+service\b`),
	regexp.MustCompile(`(?i)\bsudo\s+docker\b`),
	regexp.MustCompile(`(?i)\bsudo\s+pip\b`),
	regexp.MustCompile(`(?i)\bsudo\s+npm\b`),
	regexp.MustCompile(`(?i)\bsudo\s+reboot\b`),
}

// ── Dangerous patterns (ask user for confirmation) ──

var dangerousPatterns = []dangerousRule{
	// rm -rf (recursive force delete on non-system paths — system paths caught by hard-deny)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `rm\s+-[a-zA-Z]*[rR][a-zA-Z]*f[a-zA-Z]*`),
		message: "Recursive force delete: this will permanently remove files without confirmation",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `rm\s+-[a-zA-Z]*f[a-zA-Z]*[rR][a-zA-Z]*`),
		message: "Recursive force delete: this will permanently remove files without confirmation",
	},
	// rm -r (recursive delete without force)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `rm\s+-[a-zA-Z]*[rR]`),
		message: "Recursive delete: this will permanently remove files",
	},
	// rm -f (force delete without -r)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `rm\s+-[a-zA-Z]*f`),
		message: "Force delete: this will remove files without confirmation",
	},
	// git push --force / -f
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `git\s+push\b[^;&|\n]*[ \t](--force|--force-with-lease|-f)\b`),
		message: "Force push: may overwrite remote history and cause data loss for collaborators",
	},
	// git reset --hard
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `git\s+reset\s+--hard\b`),
		message: "Hard reset: may discard uncommitted changes permanently",
	},
	// git clean -f / -fd (force clean)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `git\s+clean\b[^;&|\n]*-[a-zA-Z]*[fF]`),
		message: "Force clean: may permanently delete untracked files",
	},
	// git checkout -- . (discard all changes)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `git\s+checkout\s+(--\s+)?\.[ \t]*($|[;&|\n])`),
		message: "Checkout all: may discard all working tree changes",
	},
	// git branch -D (force delete branch)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `git\s+branch\s+-[Dd]\b`),
		message: "Branch delete: may permanently remove a branch",
	},
	// git stash drop/clear
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `git\s+stash\s+(drop|clear)\b`),
		message: "Stash drop/clear: may permanently remove stashed changes",
	},
	// git commit --amend
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `git\s+commit\b[^;&|\n]*--amend\b`),
		message: "Amend commit: may rewrite the last commit",
	},
	// Remote code execution: curl|bash, wget|sh
	{
		pattern: regexp.MustCompile(`(?i)(?:curl|wget)\s+.*\|\s*(?:ba)?sh\b`),
		message: "Remote code execution: piping downloaded content to shell is dangerous",
	},
	// Inline code execution (python/node/ruby/perl/php -c/-e)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `(?:python[23]?)\s+-c\s`),
		message: "Inline code execution: running Python code from command line",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `(?:node)\s+-e\s`),
		message: "Inline code execution: running Node.js code from command line",
	},
	// Database destructive operations
	{
		pattern: regexp.MustCompile(`(?i)\b(?:DROP|TRUNCATE)\s+(?:TABLE|DATABASE|SCHEMA)\b`),
		message: "Database destructive operation: may delete tables or schemas",
	},
	{
		pattern: regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`),
		message: "Database delete: may remove rows from a table",
	},
	// Infrastructure destruction
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `kubectl\s+delete\b`),
		message: "Kubernetes resource deletion: may delete infrastructure resources",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `terraform\s+destroy\b`),
		message: "Terraform destroy: may destroy infrastructure",
	},
	// World-writable permissions (non-recursive still dangerous)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `chmod\s+777\b`),
		message: "World-writable permissions: may expose files to all system users",
	},
	// Recursive ownership change
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `chown\s+-[a-zA-Z]*R[a-zA-Z]*`),
		message: "Recursive ownership change: may affect many files",
	},
	// Recursive chmod (caught by hard-deny for 777, ask for other modes)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `chmod\s+-[a-zA-Z]*R[a-zA-Z]*`),
		message: "Recursive permission change: may affect many files",
	},
	// Sudo dangerous (when override doesn't apply)
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+rm\b`),
		message: "Root-level file deletion: this will run rm with root privileges",
	},
	{
		pattern: regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+chmod\s+777\b`),
		message: "Root-level world-writable permissions: this may expose system files",
	},
}

// ── Read-only commands (auto-approve when no shell operators) ──

var readOnlyCommands = map[string]bool{
	// File inspection & text processing
	"ls": true, "cat": true, "head": true, "tail": true,
	"grep": true, "find": true, "wc": true, "pwd": true,
	"echo": true, "which": true, "type": true, "file": true,
	"stat": true, "du": true, "df": true, "tree": true,
	"diff": true, "sort": true, "uniq": true, "cut": true,
	"awk": true, "sed": true, "tr": true,
	"jq": true, "yq": true,
	"basename": true, "dirname": true, "realpath": true,
	"readlink": true,
	"md5sum": true, "sha256sum": true, "sha1sum": true,
	"strings": true, "hexdump": true, "xxd": true,
	"column": true, "fmt": true, "pr": true, "fold": true,
	"nm": true, "objdump": true, "ldd": true,

	// System info
	"ps": true, "top": true, "uptime": true,
	"who": true, "whoami": true, "id": true, "uname": true,
	"env": true, "printenv": true, "date": true, "cal": true,
	"hostname": true,
	"lscpu": true, "lsblk": true, "lspci": true, "lsusb": true,
	"lsof": true, "ss": true,
	"free": true, "vmstat": true, "iostat": true,
	"dmesg": true,

	// Version queries
	"man": true, "help": true, "info": true,

	// Build tool read-only
	"make": true,
	"cargo": true,
	"go": true,
	"npm": true,
	"npx": true,
	"bun": true,
	"pip": true,
	"uv": true,

	// Docker read-only
	"docker": true,

	// Kubernetes (not auto-approved — too many destructive subcommands)

	// Git two-word prefixes (read-only subcommands)
	"git status": true,
	"git log":   true,
	"git diff":  true,
	"git show":  true,
	"git blame": true,
	"git grep":  true,
	"git branch": true, // listing branches is safe (-D is caught by dangerous)
}
