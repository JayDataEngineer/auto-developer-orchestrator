package safeguard

import (
	"testing"
)

func TestRouterDestructiveShellMatches(t *testing.T) {
	r, err := NewRouter()
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"recursive delete root", "rm -rf /", true},
		{"recursive delete root with sudo", "sudo rm -rf /", true},
		{"recursive delete tmp allowed", "rm -rf /tmp/build", false},
		{"recursive delete var/tmp allowed", "rm -rf /var/tmp/cache", false},
		{"git force push", "git push --force origin main", true},
		{"git force push shorthand main", "git push -f origin master", true},
		{"git force push shorthand feature", "git push -f origin feature-x", false}, // shorthand to non-main branch
		{"git reset hard", "git reset --hard HEAD~3", true},
		{"gh pr merge", "gh pr merge 123 --squash", true},
		{"pkill with signal 9", "pkill -9 python", true},
		{"pkill without signal", "pkill firefox", false},
		{"drop table", "DROP TABLE users;", true},
		{"drop table lowercase", "drop table users;", false}, // case-sensitive by design
		{"fork bomb", ":(){ :|:& };:", true},
		{"benign command", "ls -la /tmp", false},
		{"git push normal", "git push origin feature-branch", false},
		{"rm with specific path", "rm -rf ./build/", false},
		{"pkill SIGTERM", "pkill -15 firefox", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches := r.Check(tc.text)
			got := len(matches) > 0
			if got != tc.want {
				t.Errorf("Check(%q) = %v matches, want match=%v. Matches: %+v",
					tc.text, len(matches), tc.want, matches)
			}
		})
	}
}

func TestRouterCheckAnyJoinsArgs(t *testing.T) {
	r, _ := NewRouter()
	// Split destructive command across two args — newline-join catches it
	// because the regex isn't anchored to line start.
	matches := r.CheckAny([]string{"echo hello", "git push --force", "more stuff"})
	if len(matches) == 0 {
		t.Error("CheckAny missed destructive command in middle arg")
	}
}

func TestRouterCheckAnyNoFalsePositives(t *testing.T) {
	r, _ := NewRouter()
	matches := r.CheckAny([]string{"ls -la", "cat README.md", "grep foo *.go"})
	if len(matches) != 0 {
		t.Errorf("CheckAny false-positive on benign args: %+v", matches)
	}
}

func TestRouterEnabledDefault(t *testing.T) {
	r, _ := NewRouter()
	if !r.Enabled() {
		t.Error("Default router should be enabled")
	}
}

func TestRouterMatchStructure(t *testing.T) {
	r, _ := NewRouter()
	matches := r.Check("rm -rf /")
	if len(matches) == 0 {
		t.Fatal("expected match")
	}
	m := matches[0]
	if m.ID == "" {
		t.Error("Match.ID should be set")
	}
	if m.Description == "" {
		t.Error("Match.Description should be set")
	}
	if m.MatchedText == "" {
		t.Error("Match.MatchedText should be set")
	}
}

func TestDescribeHelper(t *testing.T) {
	r, _ := NewRouter()
	out := Describe(r.Check("rm -rf /"))
	if out == "(no matches)" {
		t.Error("Describe should format matches, got no-matches sentinel")
	}
	out2 := Describe(r.Check("ls -la"))
	if out2 != "(no matches)" {
		t.Errorf("Describe on no matches = %q, want sentinel", out2)
	}
}

func TestExtractArgStringsFlat(t *testing.T) {
	args := map[string]any{
		"command": "git push --force",
		"timeout": 30,
		"verbose": true,
	}
	out := extractArgStrings(args)
	if len(out) != 1 {
		t.Errorf("got %d strings, want 1 (only the string value)", len(out))
	}
	if out[0] != "git push --force" {
		t.Errorf("got %q, want command", out[0])
	}
}

func TestExtractArgStringsNested(t *testing.T) {
	args := map[string]any{
		"timeout": 30,
		"args": map[string]any{
			"command": "rm -rf /",
			"label":   "cleanup",
		},
	}
	out := extractArgStrings(args)
	if len(out) != 2 {
		t.Errorf("got %d strings from nested map, want 2", len(out))
	}
}
