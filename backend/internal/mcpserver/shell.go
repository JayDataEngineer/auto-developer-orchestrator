package mcpserver

import "strings"

// shQ wraps a string in single quotes for shell-safe passage. POSIX idiom:
// escape every embedded single quote as '\'' (close, escaped-quote, reopen).
//
// Used by every tool that shells out to bash with a model-controlled
// argument (python -c, curl -d, xdotool type, etc.). Without this, a
// single quote in user input would break out of the argument.
func shQ(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
