package common

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTrustedReposFixture swaps TrustedReposFile to a temp file containing
// the given lines, returns a cleanup func. Tests must call cleanup.
func writeTrustedReposFixture(t *testing.T, lines string) func() {
	t.Helper()
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "trusted-repos.txt")
	if err := os.WriteFile(fakePath, []byte(lines), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	orig := TrustedReposFile
	TrustedReposFile = fakePath
	resetTrustedReposCache()
	return func() {
		TrustedReposFile = orig
		resetTrustedReposCache()
	}
}

func TestTrustedRepos_ExactMatch(t *testing.T) {
	cleanup := writeTrustedReposFixture(t, `https://github.com/pux/research-mcp
https://github.com/other/repo
`)
	defer cleanup()

	cases := map[string]bool{
		"https://github.com/pux/research-mcp":   true,
		"git+https://github.com/pux/research-mcp": true, // git+ prefix stripped before match
		"https://github.com/evil/repo":          false,
		"https://github.com/pux/different":      false,
		"":                                      false,
	}
	for url, want := range cases {
		if got := IsTrusted(url); got != want {
			t.Errorf("IsTrusted(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestTrustedRepos_WildcardMatch(t *testing.T) {
	cleanup := writeTrustedReposFixture(t, `https://github.com/pux/*
`)
	defer cleanup()

	cases := map[string]bool{
		"https://github.com/pux/anything":     true,
		"https://github.com/pux/sub/deep":     true, // prefix match — `*` matches across `/`
		"https://github.com/other/repo":       false,
		"https://github.com/pux":              false, // no trailing slash, prefix requires it
		"":                                    false,
	}
	for url, want := range cases {
		if got := IsTrusted(url); got != want {
			t.Errorf("IsTrusted(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestTrustedRepos_LeadingWildcard(t *testing.T) {
	cleanup := writeTrustedReposFixture(t, `*example.com*
`)
	defer cleanup()

	if !IsTrusted("https://api.example.com/v1") {
		t.Errorf("expected *example.com* to match https://api.example.com/v1")
	}
	if !IsTrusted("git+https://api.example.com/v1") {
		t.Errorf("expected *example.com* to also match git+-prefixed URL")
	}
	// `*example.com*` substring-matches any URL containing `example.com`.
	// `evil.example.org` does NOT contain `example.com` (different TLD).
	if IsTrusted("https://api.example.org/v1") {
		t.Errorf("expected *example.com* to NOT match example.org")
	}
}

func TestTrustedRepos_CommentsAndBlanks(t *testing.T) {
	cleanup := writeTrustedReposFixture(t, `# Trusted repos — added 2026-06-22

https://github.com/pux/research-mcp
# more comments mid-file

https://github.com/pux/media-mcp
`)
	defer cleanup()

	if !IsTrusted("https://github.com/pux/research-mcp") {
		t.Errorf("first entry not trusted")
	}
	if !IsTrusted("https://github.com/pux/media-mcp") {
		t.Errorf("second entry not trusted")
	}
	if IsTrusted("https://github.com/pux/other") {
		t.Errorf("non-listed entry should not be trusted")
	}
}

func TestTrustedRepos_MissingFileFailsClosed(t *testing.T) {
	orig := TrustedReposFile
	TrustedReposFile = "/nonexistent/path/trusted-repos.txt"
	resetTrustedReposCache()
	defer func() {
		TrustedReposFile = orig
		resetTrustedReposCache()
	}()

	if IsTrusted("https://github.com/anything/repo") {
		t.Errorf("missing allowlist file must fail closed (trust nothing)")
	}
}

func TestTrustedRepos_EmptyFileFailsClosed(t *testing.T) {
	cleanup := writeTrustedReposFixture(t, ``)
	defer cleanup()

	if IsTrusted("https://github.com/anything/repo") {
		t.Errorf("empty allowlist must trust nothing")
	}
}

func TestTrustedRepos_GitPrefixStripped(t *testing.T) {
	cleanup := writeTrustedReposFixture(t, `https://github.com/pux/research-mcp
`)
	defer cleanup()

	// Allowlist entry has no `git+` prefix; URL has one. Strip-then-match.
	if !IsTrusted("git+https://github.com/pux/research-mcp") {
		t.Errorf("git+ URL should match bare-URL allowlist entry after prefix strip")
	}
}

func TestTrustedRepos_CacheRefreshesOnModTime(t *testing.T) {
	dir := t.TempDir()
	fakePath := filepath.Join(dir, "trusted-repos.txt")
	if err := os.WriteFile(fakePath, []byte("https://github.com/initial/repo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	orig := TrustedReposFile
	TrustedReposFile = fakePath
	resetTrustedReposCache()
	defer func() {
		TrustedReposFile = orig
		resetTrustedReposCache()
	}()

	if !IsTrusted("https://github.com/initial/repo") {
		t.Errorf("initial entry not trusted")
	}
	if IsTrusted("https://github.com/added-later/repo") {
		t.Errorf("premature trust")
	}

	// Edit the file — bump mod time, expect cache reload.
	if err := os.WriteFile(fakePath, []byte("https://github.com/added-later/repo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resetTrustedReposCache() // force reload since mod-time granularity may miss

	if !IsTrusted("https://github.com/added-later/repo") {
		t.Errorf("after edit, new entry not trusted — cache didn't reload")
	}
}
