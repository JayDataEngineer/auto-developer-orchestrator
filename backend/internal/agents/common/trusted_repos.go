package common

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TrustedReposFile is the allowlist path. Same trust model as ~/.ssh/known_hosts:
// plain text, one pattern per line, shell-glob wildcards, # comments allowed.
// Missing file = empty allowlist = nothing trusted (fail closed).
//
// Declared as a var (not const) so tests can swap it to a temp file path.
var TrustedReposFile = "~/.pux/trusted-repos.txt"

var (
	trustedReposMu       sync.RWMutex
	trustedReposCache    []string
	trustedReposModTime  int64
	trustedReposCacheTTL = int64(5) // seconds; allowlist edits picked up within 5s
	trustedReposLoadedAt int64
)

// IsTrusted reports whether repoURL matches any pattern in the allowlist.
//
// Three pattern forms, evaluated in order:
//  1. Exact match — pattern has no `*`. URL must equal pattern (modulo `git+`
//     prefix, which is stripped from the URL before comparison).
//  2. Prefix match — pattern ends with `/*`. URL must start with pattern
//     minus the trailing `*`.
//  3. Substring match — pattern starts AND ends with `*`. URL must contain
//     pattern with both `*` stripped.
//
// The three forms cover the realistic allowlist cases:
//   - `https://github.com/pux/research-mcp`  → exact
//   - `https://github.com/pux/*`              → prefix (any repo under pux)
//   - `*example.com*`                         → substring (any host containing)
//
// Empty allowlist (file missing OR empty) returns false for every URL.
// Callers MUST fail-closed: a missing allowlist is not "trust everything."
func IsTrusted(repoURL string) bool {
	patterns := loadTrustedRepos()
	target := strings.ToLower(strings.TrimPrefix(repoURL, "git+"))
	for _, raw := range patterns {
		p := strings.ToLower(raw)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "*") {
			// Form 1 — exact match. Pattern may or may not have `git+`.
			if p == target || p == strings.ToLower(repoURL) {
				return true
			}
			continue
		}
		if strings.HasSuffix(p, "/*") {
			// Form 2 — prefix match.
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(target, prefix) {
				return true
			}
			continue
		}
		if strings.HasPrefix(p, "*") && strings.HasSuffix(p, "*") && len(p) >= 2 {
			// Form 3 — substring match.
			needle := strings.Trim(p, "*")
			if strings.Contains(target, needle) {
				return true
			}
			continue
		}
		// Any other wildcard placement is not supported. Skip silently —
		// the allowlist is small; mis-typed patterns get noticed in review.
	}
	return false
}

// loadTrustedRepos reads the allowlist with a short cache. The cache is a
// process-wide singleton; the file is small and read at pre-warm time so we
// can afford to re-read it on every IsTrusted call, but caching avoids disk
// I/O on hot paths.
func loadTrustedRepos() []string {
	path := expandHome(TrustedReposFile)
	stat, err := os.Stat(path)
	if err != nil {
		return nil
	}

	now := nowUnix()
	trustedReposMu.RLock()
	cached := trustedReposCache
	modTime := trustedReposModTime
	loadedAt := trustedReposLoadedAt
	trustedReposMu.RUnlock()

	if cached != nil && modTime == stat.ModTime().Unix() && now-loadedAt < trustedReposCacheTTL {
		return cached
	}

	patterns := readTrustedReposFile(path)

	trustedReposMu.Lock()
	trustedReposCache = patterns
	trustedReposModTime = stat.ModTime().Unix()
	trustedReposLoadedAt = now
	trustedReposMu.Unlock()

	return patterns
}

// readTrustedReposFile parses the allowlist file. One pattern per line;
// blank lines and lines starting with `#` are skipped. Inline `#` is NOT
// treated as a comment — too easy to mis-paste a URL with a fragment.
func readTrustedReposFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// expandHome replaces a leading `~` with $HOME. Empty HOME leaves the path
// unchanged — the file just won't be found, and the allowlist stays empty.
func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	return filepath.Join(home, p[1:])
}

// nowUnix is a seam for tests. Defaults to time.Now().Unix().
var nowUnix = func() int64 {
	return timeNow().Unix()
}

// timeNow is overridable in tests. Defaults to time.Now.
var timeNow = time.Now

// resetTrustedReposCache clears the in-memory cache. Test helper only.
func resetTrustedReposCache() {
	trustedReposMu.Lock()
	trustedReposCache = nil
	trustedReposModTime = 0
	trustedReposLoadedAt = 0
	trustedReposMu.Unlock()
}
