package memory

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

//go:embed landmine_patterns.json
var landminePatternsJSON []byte

// LandminePattern is one diligence landmine regex + how to talk about it.
type LandminePattern struct {
	ID          string `json:"id"`
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

// LandmineMatch is what Check returns when a phrase trips a pattern.
type LandmineMatch struct {
	ID          string
	Description string
	Suggestion  string
	MatchedText string
}

// compiledPattern pairs the regex with its source for diagnostics.
type compiledPattern struct {
	re   *regexp.Regexp
	spec LandminePattern
}

// LandmineChecker flags memory writes that match diligence landmine patterns.
//
// Behavior:
//   - Interactive (DecisionRegistry != nil, NonInteractive == false):
//     emit an ask_user decision_request via the registry, wait for the user.
//     Deny → return error; Approve/Allow-session → proceed.
//   - Non-interactive (job, sub-agent): hard-deny with the suggestion.
//
// Patterns are loaded from landmine_patterns.json (embedded) on first use.
type LandmineChecker struct {
	patterns []compiledPattern

	// registry + subscriber enable the interactive ask_user path. Either both
	// are set or both are nil.
	registry   *core.DecisionRegistry
	subscriber chan<- core.AgentEvent

	// NonInteractive mirrors hooks.PermissionHook.NonInteractive. When true,
	// "ask" patterns hard-deny instead of waiting on a human.
	NonInteractive bool

	timeout time.Duration
}

// DefaultLandminePatterns parses the embedded JSON. Public so tests can
// inspect the canonical pattern list without re-embedding.
func DefaultLandminePatterns() ([]LandminePattern, error) {
	var specs []LandminePattern
	if err := json.Unmarshal(landminePatternsJSON, &specs); err != nil {
		return nil, fmt.Errorf("parse landmine_patterns.json: %w", err)
	}
	return specs, nil
}

// NewLandmineChecker compiles the embedded patterns. registry + subscriber
// are optional; pass nil for both to get non-interactive behavior always.
func NewLandmineChecker(registry *core.DecisionRegistry, subscriber chan<- core.AgentEvent) (*LandmineChecker, error) {
	specs, err := DefaultLandminePatterns()
	if err != nil {
		return nil, err
	}
	c := &LandmineChecker{
		registry:   registry,
		subscriber: subscriber,
		timeout:    5 * time.Minute,
	}
	for _, spec := range specs {
		re, err := regexp.Compile("(?i)" + spec.Pattern)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", spec.ID, err)
		}
		c.patterns = append(c.patterns, compiledPattern{re: re, spec: spec})
	}
	return c, nil
}

// Check returns every pattern that matches the text. Empty slice = clean.
func (c *LandmineChecker) Check(text string) []LandmineMatch {
	var out []LandmineMatch
	for _, p := range c.patterns {
		loc := p.re.FindStringIndex(text)
		if loc == nil {
			continue
		}
		matched := text[loc[0]:loc[1]]
		out = append(out, LandmineMatch{
			ID:          p.spec.ID,
			Description: p.spec.Description,
			Suggestion:  p.spec.Suggestion,
			MatchedText: matched,
		})
	}
	return out
}

// Approve writes through if the user (or non-interactive policy) allows it.
// Returns (proceed, reason). When proceed==false, reason is safe to surface
// to the model as the tool error.
func (c *LandmineChecker) Approve(ctx context.Context, key string, matches []LandmineMatch) (bool, string) {
	if len(matches) == 0 {
		return true, ""
	}

	// Non-interactive: hard-deny with the first match's suggestion.
	// Reason: jobs/sub-agents have no human to consult; writing the landmine
	// silently is worse than blocking.
	if c.NonInteractive || c.registry == nil {
		return false, formatDenial(matches)
	}

	// Interactive: ask the user.
	var lines []string
	lines = append(lines, "The agent wants to remember:")
	lines = append(lines, "")
	lines = append(lines, `"`+truncate(key, 300)+`"`)
	lines = append(lines, "")
	lines = append(lines, "This triggered diligence landmine patterns:")
	for _, m := range matches {
		lines = append(lines, fmt.Sprintf("  • [%s] %s — matched %q", m.ID, m.Description, m.MatchedText))
	}
	lines = append(lines, "")
	lines = append(lines, "Suggested rephrase: "+matches[0].Suggestion)
	desc := strings.Join(lines, "\n")

	reqID := fmt.Sprintf("landmine-%d", time.Now().UnixNano())
	req := core.DecisionRequest{
		ID:          reqID,
		SourceTool:  "update_memory",
		Title:       "Memory write flagged by diligence linter",
		Description: desc,
		Hint:        core.HintApproval,
		Metadata: map[string]any{
			"toolName":  "update_memory",
			"landmines": matches,
			"key":       key,
		},
	}

	resp, err := c.registry.WaitForDecision(ctx, req, c.subscriber, c.timeout)
	if err != nil {
		return false, fmt.Sprintf("landmine check failed: %v", err)
	}

	switch resp.Action {
	case "approve", "allow_session":
		return true, ""
	case "reject":
		return false, "memory write rejected by user: " + formatDenial(matches)
	default:
		return false, fmt.Sprintf("unknown landmine decision %q", resp.Action)
	}
}

func formatDenial(matches []LandmineMatch) string {
	if len(matches) == 0 {
		return ""
	}
	var parts []string
	parts = append(parts, "memory write blocked by diligence linter:")
	for _, m := range matches {
		parts = append(parts, fmt.Sprintf("  • [%s] matched %q — %s", m.ID, m.MatchedText, m.Suggestion))
	}
	return strings.Join(parts, "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
