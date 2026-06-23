package orchestrator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/adapters"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	"github.com/auto-developer-orchestrator/backend/internal/tools/file"
)

// stubProvider is a no-op LLMProvider so we can construct the orchestrator
// without hitting a real backend. We only inspect tool composition; we never
// run the loop.
type stubProvider struct{}

func (stubProvider) StreamChat(ctx context.Context, messages []core.Message, tools []core.OpenAITool, opts core.GenerateOptions) (<-chan core.ChatEvent, error) {
	ch := make(chan core.ChatEvent)
	close(ch)
	return ch, nil
}
func (stubProvider) ModelName() string  { return "stub" }
func (stubProvider) ContextSize() int   { return 32768 }

// minimalCfg returns the smallest Config that lets orchestrator.New succeed
// without panicking. Tools that need optional infra (memory store, MCP, browser,
// desktop) are intentionally nil — those code paths are guarded.
func minimalCfg(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	hostBash := &adapters.HostExecutor{WorkDir: dir}
	hostFileOps := &file.SimpleSandboxOps{BasePath: dir}
	return Config{
		ProjectDir:    dir,
		SandboxID:     "smoke-test",
		WorkDir:       dir,
		SessionPath:   filepath.Join(dir, "session.jsonl"),
		ContextSize:   32768,
		MaxToolRounds: 5,
		HostBash:      hostBash,
		HostFileOps:   hostFileOps,
		BashExecutor:  hostBash,
		FileOps:       hostFileOps,
	}
}

// containsTool reports whether name appears in the slice.
func containsTool(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// TestOrchestratorSmokeConstruct verifies that orchestrator.New() succeeds
// with minimum config. The construction is enormous — this catches
// nil-pointer regressions in the wiring (e.g., a new optional field accessed
// without a nil check).
func TestOrchestratorSmokeConstruct(t *testing.T) {
	cfg := minimalCfg(t)
	agent, err := New(stubProvider{}, cfg)
	if err != nil {
		t.Fatalf("orchestrator.New failed: %v", err)
	}
	defer agent.Close()

	if agent.Loop() == nil {
		t.Fatal("agent.Loop() is nil after construction")
	}
}

// TestOrchestratorCTOHasCoreTools verifies the CTO has the tools the kernel
// contract depends on: bash, file ops, ask_user (gated on Subscriber),
// scripting, todo, eval, python, messaging (PR6), and delegation (when a
// provider is wired so the runner is constructed).
func TestOrchestratorCTOHasCoreTools(t *testing.T) {
	cfg := minimalCfg(t)
	// Subscriber gates ask_user. Provide a buffered channel.
	cfg.Subscriber = make(chan<- core.AgentEvent, 16)
	// Stub the channel: Subscriber is send-only; we just need non-nil to
	// trip the guard at orchestrator.go:271. Replace with a real channel.
	sub := make(chan core.AgentEvent, 16)
	cfg.Subscriber = sub

	agent, err := New(stubProvider{}, cfg)
	if err != nil {
		t.Fatalf("orchestrator.New failed: %v", err)
	}
	defer agent.Close()

	names := agent.Loop().ToolNames()

	required := []string{
		"bash",
		"file_write", "file_edit", "file_read", "file_grep", "file_glob",
		"ask_user",
		"make_script", "run_script", "list_scripts", "edit_script", "show_script",
		"todo",
		"eval",
		"python",
		// PR6 — messaging tools must be on the CTO. This is the assertion that
		// locks down the regression: if someone moves MessagingTools() out of
		// the CTO tool list, peer-to-peer delegation breaks silently.
		"send_message", "wait_for_message", "list_peers",
	}
	for _, name := range required {
		if !containsTool(names, name) {
			t.Errorf("CTO missing required tool %q. Have: %v", name, names)
		}
	}
}

// TestOrchestratorCTOHasDelegationTools verifies that when a provider is
// wired, the CTO gets the delegation tools (delegate_to, delegate_async,
// collect_results). Without a provider, the runner isn't constructed and
// these tools are absent — which is correct because there's no LLM to run
// sub-agents against.
func TestOrchestratorCTOHasDelegationTools(t *testing.T) {
	cfg := minimalCfg(t)
	sub := make(chan core.AgentEvent, 16)
	cfg.Subscriber = sub

	agent, err := New(stubProvider{}, cfg)
	if err != nil {
		t.Fatalf("orchestrator.New failed: %v", err)
	}
	defer agent.Close()

	names := agent.Loop().ToolNames()

	for _, name := range []string{"delegate_to", "delegate_async", "collect_results"} {
		if !containsTool(names, name) {
			t.Errorf("CTO missing delegation tool %q. Have: %v", name, names)
		}
	}
}

// TestOrchestratorCTODoesNotHaveEmployeeTools verifies the kernel contract:
// the CTO does NOT get browser/desktop/MCP tools. Forces delegation.
func TestOrchestratorCTODoesNotHaveEmployeeTools(t *testing.T) {
	cfg := minimalCfg(t)
	sub := make(chan core.AgentEvent, 16)
	cfg.Subscriber = sub

	agent, err := New(stubProvider{}, cfg)
	if err != nil {
		t.Fatalf("orchestrator.New failed: %v", err)
	}
	defer agent.Close()

	names := agent.Loop().ToolNames()

	forbidden := []string{
		"browser_navigate", "browser_click", "browser_screenshot",
		"desktop_screenshot", "desktop_click", "desktop_type",
		"mcp__web__search", "mcp__media__analyze_image",
	}
	for _, name := range forbidden {
		if containsTool(names, name) {
			t.Errorf("CTO should NOT have employee tool %q (kernel contract: forces delegation). Have: %v", name, names)
		}
	}
}

// TestOrchestratorSandboxOnlyMode verifies SandboxOnly restricts the CTO to
// bash + file ops + scratch. No delegation, no messaging, no MCP. Used by
// scheduler jobs that only need shell access.
func TestOrchestratorSandboxOnlyMode(t *testing.T) {
	cfg := minimalCfg(t)
	cfg.SandboxOnly = true
	// SandboxOnly needs a scratch store, which needs a DB. Use nil — the path
	// tolerates it (NewPersistentScratchStore with nil DB falls back to memory).
	cfg.AgentID = "smoke-sandbox-only"

	agent, err := New(stubProvider{}, cfg)
	if err != nil {
		t.Fatalf("orchestrator.New failed: %v", err)
	}
	defer agent.Close()

	names := agent.Loop().ToolNames()

	// Must have bash + file ops.
	for _, name := range []string{"bash", "file_write", "file_read"} {
		if !containsTool(names, name) {
			t.Errorf("SandboxOnly CTO missing %q. Have: %v", name, names)
		}
	}
	// Must NOT have delegation / messaging / skills.
	for _, name := range []string{"delegate_to", "delegate_async", "send_message", "wait_for_message", "list_peers"} {
		if containsTool(names, name) {
			t.Errorf("SandboxOnly CTO should NOT have %q. Have: %v", name, names)
		}
	}
}

// Compile-time check: stubProvider satisfies core.LLMProvider.
var _ core.LLMProvider = stubProvider{}

// Compile-time check: adapters.HostExecutor + file.SimpleSandboxOps satisfy
// the executor/fileops interfaces the orchestrator expects.
var _ bash.Executor = (*adapters.HostExecutor)(nil)
var _ file.SandboxFileOps = (*file.SimpleSandboxOps)(nil)
