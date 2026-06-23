package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/core"
)

// TestLandminePatternsCompile — every pattern in landmine_patterns.json
// must compile. Otherwise a typo silently disables detection.
func TestLandminePatternsCompile(t *testing.T) {
	checker, err := NewLandmineChecker(nil, nil)
	if err != nil {
		t.Fatalf("NewLandmineChecker: %v", err)
	}
	if len(checker.patterns) == 0 {
		t.Fatal("expected at least one compiled pattern")
	}
}

// TestLandminePatternsLoadFromJSON — the embedded JSON parses and every
// entry has the required fields.
func TestLandminePatternsLoadFromJSON(t *testing.T) {
	specs, err := DefaultLandminePatterns()
	if err != nil {
		t.Fatalf("DefaultLandminePatterns: %v", err)
	}
	if len(specs) < 5 {
		t.Errorf("expected at least 5 patterns, got %d", len(specs))
	}
	seenIDs := map[string]bool{}
	for _, s := range specs {
		if s.ID == "" || s.Pattern == "" || s.Description == "" || s.Suggestion == "" {
			t.Errorf("incomplete pattern: %+v", s)
		}
		if seenIDs[s.ID] {
			t.Errorf("duplicate pattern id: %s", s.ID)
		}
		seenIDs[s.ID] = true
	}
}

// TestLandmineCheckEveryPattern — feed each pattern a string that should
// match. If any pattern fails to match its own canonical example, the
// detector is silently broken.
func TestLandmineCheckEveryPattern(t *testing.T) {
	cases := map[string]string{
		"bypass":           "use bypass to skip the gate",
		"skip-check":       "skip the hook when in a hurry",
		"avoid-check":      "run with --no-verify to avoid the check",
		"get-around":       "this is a way to get around the gatekeeper",
		"wont-notice":      "the user won't notice we removed it",
		"reduce-requirement": "reduce the requirement so it passes CI",
		"force-push-merge": "just force the push to overwrite",
		"no-verify":        "always use --no-verify on commit",
	}

	checker, err := NewLandmineChecker(nil, nil)
	if err != nil {
		t.Fatalf("NewLandmineChecker: %v", err)
	}

	matched := checker.Check(strings.Join(mapValues(cases), " | "))
	if len(matched) == 0 {
		t.Fatal("expected matches, got none")
	}

	gotIDs := map[string]bool{}
	for _, m := range matched {
		gotIDs[m.ID] = true
	}
	for id := range cases {
		if !gotIDs[id] {
			t.Errorf("pattern %q did not match against its canonical example", id)
		}
	}
}

// TestLandmineCheckDoesntFalsePositiveOnLegit — memory entries that describe
// the existence of a bypass (without endorsing it) should not fire. The
// detector sees text; we accept some false-positive risk on the word
// "bypass" itself, but legitimate descriptions shouldn't trip the more
// specific patterns.
func TestLandmineCheckDoesntFalsePositiveOnLegit(t *testing.T) {
	checker, _ := NewLandmineChecker(nil, nil)
	legit := []string{
		"user prefers dark mode",
		"the project uses Postgres for persistence",
		"tests live under tests/ and run with pytest",
		"deployments go through ArgoCD",
		"the user is a Go developer with 10 years experience",
	}
	for _, s := range legit {
		if m := checker.Check(s); len(m) > 0 {
			t.Errorf("legitimate memory %q matched: %+v", s, m)
		}
	}
}

// TestLandmineNonInteractiveHardDenies — in non-interactive mode (job or
// sub-agent), the checker must hard-deny without blocking on a human.
// This is the load-bearing case for not hanging in CI.
func TestLandmineNonInteractiveHardDenies(t *testing.T) {
	checker, err := NewLandmineChecker(nil, nil)
	if err != nil {
		t.Fatalf("NewLandmineChecker: %v", err)
	}
	checker.NonInteractive = true

	tool := NewToolWithLandmine(NewStore(t.TempDir()), checker)
	_, err = tool.Execute(context.Background(), map[string]any{
		"key": "always use --no-verify to skip the hook when committing",
	})
	if err == nil {
		t.Fatal("expected error from landmine in non-interactive mode")
	}
	var toolErr *core.ToolError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *core.ToolError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "no-verify") && !strings.Contains(err.Error(), "skip-check") && !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should reference the landmine, got: %v", err)
	}
}

// TestLandmineNilCheckerBypassesCheck — a Tool constructed without a
// checker (legacy path) must work as before. This guards against the
// landmine check accidentally running on tools that didn't opt in.
func TestLandmineNilCheckerBypassesCheck(t *testing.T) {
	tool := NewTool(NewStore(t.TempDir()))
	_, err := tool.Execute(context.Background(), map[string]any{
		"key": "always use --no-verify to skip the hook",
	})
	if err != nil {
		t.Fatalf("nil checker should bypass landmine; got error: %v", err)
	}
}

// TestLandmineInteractiveApproves — when the user approves via the registry,
// the write goes through.
func TestLandmineInteractiveApproves(t *testing.T) {
	registry := core.NewDecisionRegistry()
	sub := make(chan core.AgentEvent, 8)
	checker, err := NewLandmineChecker(registry, sub)
	if err != nil {
		t.Fatalf("NewLandmineChecker: %v", err)
	}

	// Use a known decision ID so the goroutine can resolve it.
	checker.timeout = 5 * time.Second

	tool := NewToolWithLandmine(NewStore(t.TempDir()), checker)

	// Resolve before calling Execute — the registry queues the response
	// channel, but if we resolve before the wait begins, Resolve returns
	// false. So we run Execute in a goroutine and resolve after a tick.
	done := make(chan struct{})
	go func() {
		// Wait for the request to be registered, then resolve.
		// We poll the subscriber channel for the decision_request event.
		var reqID string
		for i := 0; i < 100 && reqID == ""; i++ {
			select {
			case ev := <-sub:
				if ev.Type == core.EventTypeDecisionRequest {
					if data, ok := ev.Data.(core.DecisionRequestData); ok {
						reqID = data.ID
					}
				}
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
		if reqID == "" {
			t.Error("no decision_request event observed")
			return
		}
		registry.Resolve(reqID, core.DecisionResponse{Action: "approve"})
	}()

	result, err := tool.Execute(context.Background(), map[string]any{
		"key": "use bypass to skip the gate",
	})
	close(done)
	if err != nil {
		t.Fatalf("expected approval, got error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Errorf("expected success=true after approval, got %v", m)
	}
}

// TestLandmineInteractiveRejects — when the user rejects, the tool errors.
func TestLandmineInteractiveRejects(t *testing.T) {
	registry := core.NewDecisionRegistry()
	sub := make(chan core.AgentEvent, 8)
	checker, err := NewLandmineChecker(registry, sub)
	if err != nil {
		t.Fatalf("NewLandmineChecker: %v", err)
	}
	checker.timeout = 5 * time.Second

	tool := NewToolWithLandmine(NewStore(t.TempDir()), checker)

	go func() {
		var reqID string
		for i := 0; i < 100 && reqID == ""; i++ {
			select {
			case ev := <-sub:
				if ev.Type == core.EventTypeDecisionRequest {
					if data, ok := ev.Data.(core.DecisionRequestData); ok {
						reqID = data.ID
					}
				}
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
		if reqID == "" {
			t.Error("no decision_request event observed")
			return
		}
		registry.Resolve(reqID, core.DecisionResponse{Action: "reject"})
	}()

	_, err = tool.Execute(context.Background(), map[string]any{
		"key": "skip the hook to commit faster",
	})
	if err == nil {
		t.Fatal("expected error after user rejection")
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
