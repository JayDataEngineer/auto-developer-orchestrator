package bash

import (
	"fmt"
	"regexp"
	"strings"
)

// CommandBlockedError is returned when a command matches a deny rule.
type CommandBlockedError struct {
	Command  string
	Category string
	Message  string
}

func (e *CommandBlockedError) Error() string {
	cmd := e.Command
	if len(cmd) > 100 {
		cmd = cmd[:100] + "..."
	}
	return fmt.Sprintf("command blocked (%s): %s. Command: %s", e.Category, e.Message, cmd)
}

// Rule is a single deny rule.
type Rule struct {
	Pattern       *regexp.Regexp
	Category      string
	Message       string
	AllowOverride bool // true = can be bypassed by override patterns
}

// Validator checks commands against a deny list with override patterns.
type Validator struct {
	rules          []Rule
	overridePatterns []*regexp.Regexp
}

// NewDefaultValidator returns a validator with the built-in deny list.
func NewDefaultValidator() *Validator {
	return &Validator{
		rules:          defaultDenyRules(),
		overridePatterns: defaultOverridePatterns(),
	}
}

// Validate checks a command against all deny rules.
// If a deny rule matches, it checks override patterns before blocking.
func (v *Validator) Validate(cmd string) error {
	lower := strings.ToLower(cmd)
	for _, rule := range v.rules {
		if rule.Pattern.MatchString(lower) {
			// Check if an override pattern exempts this command
			if rule.AllowOverride {
				for _, ov := range v.overridePatterns {
					if ov.MatchString(lower) {
						return nil // override matches, allow it
					}
				}
			}
			return &CommandBlockedError{
				Command:  cmd,
				Category: rule.Category,
				Message:  rule.Message,
			}
		}
	}
	return nil
}

// cmdPos is a lookbehind-ish prefix that matches the start of a command position:
// beginning of string, or after ; & | (shell operators). This prevents false positives
// when dangerous keywords appear inside string arguments like echo 'nmap'.
// Usage: cmdPos + `\b` + commandName
const cmdPos = `(?:^|;\s*|&&\s*|\|\|\s*|\|\s*)`

// defaultDenyRules returns the built-in command deny list.
func defaultDenyRules() []Rule {
	return []Rule{
		// ── Destruction ──
		{
			// rm -rf / — but NOT rm -rf /tmp/ or rm -rf /home/user/project/build/
			// Only block when target is bare / or / followed by a top-level system dir
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `rm\s+-[a-zA-Z]*f[a-zA-Z]*\s+/(?:\s|$)`),
			Category: "destruction",
			Message:  "recursive force-delete of root filesystem is not allowed",
		},
		{
			// rm -rf /etc, /usr, /var, /bin, /sbin, /boot, /dev, /proc, /sys, /lib, /root
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `rm\s+-[a-zA-Z]*f[a-zA-Z]*\s+/(?:etc|usr|var|bin|sbin|boot|dev|proc|sys|lib|lib64|root)(?:/|\s|$)`),
			Category: "destruction",
			Message:  "recursive force-delete of system directories is not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `mkfs\b`), // mkfs.ext4 /dev/sda
			Category: "destruction",
			Message:  "filesystem format commands are not allowed",
		},
		{
			// dd targeting block devices — matches if either input or output is a block device
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `dd\s+.*(?:if|of)=/dev/(?:sd|nvme|vd)`),
			Category: "destruction",
			Message:  "raw disk operations on block devices are not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)>\s*/dev/sd`), // > /dev/sda (redirect to block device)
			Category: "destruction",
			Message:  "writing directly to block devices is not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i):\(\)\s*\{\s*:\|:\&\s*\}\s*;:`), // fork bomb
			Category: "destruction",
			Message:  "fork bomb patterns are not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `shred\b`), // shred /dev/sda
			Category: "destruction",
			Message:  "file shredding is not allowed",
		},

		// ── Privilege escalation ──
		{
			Pattern:       regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+rm\b`), // sudo rm
			Category:      "privilege",
			Message:       "sudo rm is not allowed (use without sudo for project files)",
			AllowOverride: true,
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+su\b`), // sudo su
			Category: "privilege",
			Message:  "switching to root shell is not allowed",
		},
		{
			Pattern:       regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+chmod\s+777\b`), // sudo chmod 777
			Category:      "privilege",
			Message:       "setting world-writable permissions with sudo is not allowed",
			AllowOverride: true,
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `chmod\s+-[a-zA-Z]*R[a-zA-Z]*\s+777\b`), // chmod -R 777
			Category: "privilege",
			Message:  "recursively setting world-writable permissions is not allowed",
		},
		{
			// passwd command (not the word in filenames/paths)
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `passwd\b`),
			Category: "privilege",
			Message:  "password changes are not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+bash\b`), // sudo bash
			Category: "privilege",
			Message:  "spawning root shell is not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+sh\b`), // sudo sh
			Category: "privilege",
			Message:  "spawning root shell is not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `sudo\s+zsh\b`), // sudo zsh
			Category: "privilege",
			Message:  "spawning root shell is not allowed",
		},

		// ── Network attack tools ──
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `nmap\b`), // nmap -sV target
			Category: "network_attack",
			Message:  "network scanning tools are not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `hydra\b`), // hydra -l user -P pass target
			Category: "network_attack",
			Message:  "password cracking tools are not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `aircrack\b`), // aircrack-ng
			Category: "network_attack",
			Message:  "WiFi cracking tools are not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `netcat\s+-[elp]`), // netcat -e /bin/bash
			Category: "network_attack",
			Message:  "netcat in listen/reverse shell mode is not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `nc\s+-[elp]`), // nc -e /bin/bash
			Category: "network_attack",
			Message:  "netcat in listen/reverse shell mode is not allowed",
		},
		{
			Pattern:  regexp.MustCompile(`(?i)` + cmdPos + `python[23]?\b.*-c.*import\s+socket.*\b(bind|listen)\b`), // reverse shell pattern
			Category: "network_attack",
			Message:  "raw socket bind/listen patterns are not allowed",
		},
	}
}

// defaultOverridePatterns returns patterns that bypass specific deny rules.
// These allow legitimate sudo usage while blocking dangerous combinations.
func defaultOverridePatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
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
}
