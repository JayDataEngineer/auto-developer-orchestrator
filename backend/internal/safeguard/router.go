// Package safeguard implements preflight checks for destructive content in
// agent tool calls. When a pattern matches, the safeguard emits a fallback
// event (so the audit trail sees it) and — when wired — switches the engine
// for the next turn to a more careful model.
//
// This is the Fable/Mythos "reckless action" defense (baseline 1.0%) adapted
// to our setting: rather than denying the action outright (the permission
// hook already does that for hard-deny patterns), the safeguard catches
// destructive-shell patterns the permission hook would only "ask" about and
// routes them through a more careful path.
package safeguard

import (
	"regexp"
	"strings"
	"sync"
)

// Pattern is one destructive-shell classifier. AllowRe, if non-nil, suppresses
// matches that it also matches — used to allowlist known-safe forms like
// `rm -rf /tmp/...` against the `rm -rf /` pattern (Go's regexp doesn't
// support negative lookahead, so the allowlist lives in a separate regex).
type Pattern struct {
	ID          string
	Description string
	Re          *regexp.Regexp
	AllowRe     *regexp.Regexp // optional suppressor
}

// Match describes one triggered pattern.
type Match struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	MatchedText string `json:"matched_text"`
}

// Router runs safeguard classifiers against an input blob. Construction
// compiles all patterns; Check is goroutine-safe.
type Router struct {
	mu       sync.RWMutex
	patterns []Pattern
}

// NewRouter compiles the canonical pattern list. Returns an error if any
// pattern is malformed; callers should treat that as a startup failure.
func NewRouter() (*Router, error) {
	specs := DefaultPatterns()
	patterns := make([]Pattern, 0, len(specs))
	for _, spec := range specs {
		re, err := regexp.Compile(spec.Re.String())
		if err != nil {
			return nil, err
		}
		var allow *regexp.Regexp
		if spec.AllowRe != nil {
			a, err := regexp.Compile(spec.AllowRe.String())
			if err != nil {
				return nil, err
			}
			allow = a
		}
		patterns = append(patterns, Pattern{
			ID:          spec.ID,
			Description: spec.Description,
			Re:          re,
			AllowRe:     allow,
		})
	}
	return &Router{patterns: patterns}, nil
}

// Check returns every pattern that matches anywhere in the input text.
// Matches suppressed by AllowRe are dropped. Empty slice = clean.
// Multi-pattern matches are returned in registration order; callers usually
// only care about the first.
func (r *Router) Check(text string) []Match {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Match
	for _, p := range r.patterns {
		loc := p.Re.FindStringIndex(text)
		if loc == nil {
			continue
		}
		if p.AllowRe != nil {
			// Suppress if the allowlist matches at the same location.
			if aLoc := p.AllowRe.FindStringIndex(text); aLoc != nil {
				if aLoc[0] == loc[0] {
					continue
				}
			}
		}
		out = append(out, Match{
			ID:          p.ID,
			Description: p.Description,
			MatchedText: text[loc[0]:loc[1]],
		})
	}
	return out
}

// CheckAny scans a slice of strings (e.g. tool args) and returns matches
// across all of them. Useful when you have an args map and want to check
// every string value without rebuilding one big blob.
func (r *Router) CheckAny(texts []string) []Match {
	// Newlines between args prevent cross-arg regex bleed. The patterns are
	// anchored on command shape, not line position.
	return r.Check(strings.Join(texts, "\n"))
}

// Enabled reports whether at least one pattern is loaded. Routers built from
// DefaultPatterns are always enabled; this is for callers that build from a
// user-configured subset.
func (r *Router) Enabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.patterns) > 0
}
