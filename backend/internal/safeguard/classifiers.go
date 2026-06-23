package safeguard

import "regexp"

// DefaultPatterns is the canonical classifier set shipped with the router.
// Order matters: callers reading Check() output typically surface the first
// match, so high-precision patterns should precede noisier ones.
//
// Patterns are anchored on destructive-shell shape, not on intent. A pattern
// matches when the literal command sequence is present in tool args or
// turn input, regardless of surrounding prose. False-positive surface:
//   - `pkill -9` matches literal "pkill -9", not "pkill" alone — safe for
//     legitimate cleanup of stray `pkill firefox` invocations.
//   - `rm -rf /` matches the bare root recursive delete; `/tmp/...` and
//     `/var/tmp/...` are allowlisted via AllowRe (Go regexp lacks negative
//     lookahead, so the allowlist is a separate pattern matched at the same
//     location).
//   - `DROP TABLE` is case-sensitive (SQL is case-insensitive in practice,
//     but we catch the textbook injection; broader detection belongs in a
//     dedicated SQL classifier).
//
// Add new patterns here. Each becomes a row in Check() output.
func DefaultPatterns() []Pattern {
	return []Pattern{
		{
			ID:          "destructive-shell",
			Description: "Recursive delete at filesystem root, force push/reset, PR merge, kill -9, SQL drop, or fork bomb",
			Re:          regexp.MustCompile(`(rm\s+-rf\s+/|git\s+push\s+--force|git\s+push\s+-f\s+origin\s+(main|master)|git\s+reset\s+--hard|gh\s+pr\s+merge|pkill\s+-9|DROP\s+TABLE|:\(\)\s*\{.*\|.*&\s*\}\s*;)`),
			AllowRe:     regexp.MustCompile(`rm\s+-rf\s+/(tmp|var/tmp)(/|$)`),
		},
	}
}
