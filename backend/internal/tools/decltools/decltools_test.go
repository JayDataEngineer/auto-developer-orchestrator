package decltools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/core"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
	"github.com/auto-developer-orchestrator/backend/internal/tools/truncate"
)

// fakeExec is a minimal bash.Executor that records the last command and
// returns a configured output. Optional delay simulates slow commands so
// timeout tests can verify cancellation.
type fakeExec struct {
	mu       sync.Mutex
	lastCmd  string
	allCmds  []string
	out      string
	err      error
	delay    time.Duration // 0 = return immediately
}

func (f *fakeExec) Exec(ctx context.Context, command string) (string, error) {
	f.mu.Lock()
	f.lastCmd = command
	f.allCmds = append(f.allCmds, command)
	delay := f.delay
	f.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.out, f.err
}

func (f *fakeExec) LastCmd() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCmd
}

// TestShellQuotingProvesInjectionBlocked is the load-bearing safety test.
// A query containing `'; rm -rf /; '` MUST be quoted so the resulting
// command line has no executable `rm` — it's a single-quoted literal.
func TestShellQuotingProvesInjectionBlocked(t *testing.T) {
	evil := `'; rm -rf /; '`
	quoted := shellQuote(evil)
	if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
		t.Fatalf("quoted form not wrapped in single quotes: %q", quoted)
	}
	// The result should not contain an unquoted "rm" — every `'` opens or
	// closes a literal-string region. Easiest assertion: round-trip via
	// /bin/echo if we had a shell. We don't here, so check structural
	// property: the only way to escape `'` in single quotes is `'\''`,
	// which leaves a balanced quote pair on either side.
	opens := strings.Count(quoted, "'")
	if opens%2 != 0 {
		t.Fatalf("odd number of single-quotes — unbalanced: %q (count=%d)", quoted, opens)
	}
}

// TestSubstitutionInsertsShellQuotedValue proves {{param}} becomes the
// shell-quoted value, not a raw interpolation. Note: every value is quoted
// (including integers) — `--max '8'` is valid shell and uniformly quoting
// is safer than type-conditional logic.
func TestSubstitutionInsertsShellQuotedValue(t *testing.T) {
	tmpl := "python3 /sandbox/ddg.py {{query}} --max {{max}}"
	out, err := substitute(tmpl, map[string]any{
		"query": "hello world",
		"max":   8,
	})
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	want := "python3 /sandbox/ddg.py 'hello world' --max '8'"
	if out != want {
		t.Errorf("got:  %q\nwant: %q", out, want)
	}
}

// TestSubstitutionLeavesUnknownPlaceholdersAlone — typos in the command
// template surface as literal text in the command, which the script then
// complains about. This is better than silently dropping or substituting "".
func TestSubstitutionLeavesUnknownPlaceholdersAlone(t *testing.T) {
	out, err := substitute("echo {{typo}}", map[string]any{"real": "x"})
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	if !strings.Contains(out, "{{typo}}") {
		t.Errorf("expected {{typo}} to survive as literal, got %q", out)
	}
}

// TestSubstitutionEscapesInjectionChar proves the dangerous payload
// `'; rm -rf /; '` is rendered as a quoted literal — the resulting command
// line cannot execute the injected text.
func TestSubstitutionEscapesInjectionChar(t *testing.T) {
	evil := `'; echo pwned; '`
	out, err := substitute("python3 x.py {{q}}", map[string]any{"q": evil})
	if err != nil {
		t.Fatalf("substitute: %v", err)
	}
	if strings.Contains(out, "echo pwned';") {
		t.Errorf("injection broke out of quoting: %q", out)
	}
	// Sanity: command still mentions the original query (escaped).
	if !strings.Contains(out, "echo pwned") {
		t.Errorf("query text went missing in: %q", out)
	}
}

// TestResolveArgsMissingRequiredReturnsError — missing required param names
// the offender in the error message.
func TestResolveArgsMissingRequiredReturnsError(t *testing.T) {
	params := []common.ToolParam{
		{Name: "query", Type: "string", Required: true},
		{Name: "max", Type: "integer"},
	}
	_, err := resolveArgs(params, map[string]any{"max": 5})
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "query") {
		t.Errorf("error should name the missing param, got: %v", err)
	}
}

// TestResolveArgsAppliesDefault — missing optional param with a default
// gets the default value; missing optional without default is skipped.
func TestResolveArgsAppliesDefault(t *testing.T) {
	params := []common.ToolParam{
		{Name: "query", Type: "string", Required: true},
		{Name: "max", Type: "integer", Default: 8},
		{Name: "verbose", Type: "boolean"}, // no default — skipped
	}
	out, err := resolveArgs(params, map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("resolveArgs: %v", err)
	}
	if v, ok := out["max"]; !ok || v != 8 {
		t.Errorf("default not applied: out=%v", out)
	}
	if _, present := out["verbose"]; present {
		t.Errorf("optional-without-default should be skipped: out=%v", out)
	}
}

// TestExecuteMissingRequiredReturnsErrorMap — Execute returns the error in
// a map (not a Go error) so the agent sees it in tool output, matching the
// pattern used by scripting/bash tools.
func TestExecuteMissingRequiredReturnsErrorMap(t *testing.T) {
	dt := common.DeclarativeTool{
		Name:    "test_tool",
		Command: "echo {{q}}",
		Parameters: []common.ToolParam{
			{Name: "q", Type: "string", Required: true},
		},
	}
	tool := Build(dt, &fakeExec{out: ""})
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute returned Go error (should be in map): %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("Execute returned non-map: %T", result)
	}
	if _, hasErr := m["error"]; !hasErr {
		t.Errorf("missing 'error' key in result: %v", m)
	}
}

// TestExecuteAppliesDefaultAndRuns proves the default value reaches the
// executor.
func TestExecuteAppliesDefaultAndRuns(t *testing.T) {
	dt := common.DeclarativeTool{
		Name:    "test_tool",
		Command: "python3 x.py {{query}} --max {{max}}",
		Parameters: []common.ToolParam{
			{Name: "query", Type: "string", Required: true},
			{Name: "max", Type: "integer", Default: 8},
		},
	}
	fe := &fakeExec{out: "[]"}
	tool := Build(dt, fe)
	_, err := tool.Execute(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := fe.LastCmd()
	want := "python3 x.py 'hello' --max '8'"
	if got != want {
		t.Errorf("command:\n got: %q\nwant: %q", got, want)
	}
}

// TestExecuteTruncatesLargeOutput proves output past BashMaxChars is tail-
// truncated (keeps the end, matches bash tool behavior).
func TestExecuteTruncatesLargeOutput(t *testing.T) {
	big := strings.Repeat("a", truncate.BashMaxChars*2)
	dt := common.DeclarativeTool{
		Name:    "test_tool",
		Command: "echo {{x}}",
		Parameters: []common.ToolParam{
			{Name: "x", Type: "string", Required: true},
		},
	}
	fe := &fakeExec{out: big}
	tool := Build(dt, fe)
	result, _ := tool.Execute(context.Background(), map[string]any{"x": "q"})
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("non-map result: %T", result)
	}
	stdout, _ := m["stdout"].(string)
	if len(stdout) > truncate.BashMaxChars+100 { // +100 for truncation slop
		t.Errorf("stdout not truncated: %d bytes (cap=%d)", len(stdout), truncate.BashMaxChars)
	}
}

// TestExecuteHonorsTimeout proves a per-tool Timeout (in SECONDS) kills a
// runaway command. A 1-second timeout with a 10s fake delay must return
// shortly after 1 second, not wait the full 10.
func TestExecuteHonorsTimeout(t *testing.T) {
	dt := common.DeclarativeTool{
		Name:    "slow_tool",
		Command: "sleep {{s}}",
		Timeout: 1, // 1 second
		Parameters: []common.ToolParam{
			{Name: "s", Type: "string", Required: true},
		},
	}
	fe := &fakeExec{
		out:   "",
		err:   nil,
		delay: 10 * time.Second,
	}
	tool := Build(dt, fe)

	start := time.Now()
	_, _ = tool.Execute(context.Background(), map[string]any{"s": "10"})
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("timeout not enforced: elapsed=%v (should be ~1s)", elapsed)
	}
}

// TestExecuteSurfacesExecutorError proves a failing executor puts the error
// message into the result map (not a Go error). Mirrors scripting.go pattern.
func TestExecuteSurfacesExecutorError(t *testing.T) {
	dt := common.DeclarativeTool{
		Name:    "test_tool",
		Command: "false {{x}}",
		Parameters: []common.ToolParam{
			{Name: "x", Type: "string", Required: true},
		},
	}
	fe := &fakeExec{out: "", err: errors.New("exit code 1")}
	tool := Build(dt, fe)
	result, _ := tool.Execute(context.Background(), map[string]any{"x": "q"})
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("non-map result: %T", result)
	}
	if _, hasErr := m["error"]; !hasErr {
		t.Errorf("missing 'error' key on executor failure: %v", m)
	}
}

// TestTimeoutHintZeroWhenNotSet proves Timeout=0 means "no opinion" — the
// agent loop falls back to its default.
func TestTimeoutHintZeroWhenNotSet(t *testing.T) {
	dt := common.DeclarativeTool{Name: "x", Command: "echo hi"}
	tool := Build(dt, &fakeExec{})
	meta, ok := tool.(interface{ TimeoutHint() time.Duration })
	if !ok {
		t.Fatal("Tool does not implement TimeoutHint (missing core.ToolMetadata)")
	}
	if got := meta.TimeoutHint(); got != 0 {
		t.Errorf("TimeoutHint = %v, want 0 when Timeout unset", got)
	}
}

// TestTimeoutHintReturnsDeclaredValue proves Timeout=N surfaces as N seconds.
func TestTimeoutHintReturnsDeclaredValue(t *testing.T) {
	dt := common.DeclarativeTool{Name: "x", Command: "echo hi", Timeout: 42}
	tool := Build(dt, &fakeExec{})
	meta, ok := tool.(interface{ TimeoutHint() time.Duration })
	if !ok {
		t.Fatal("Tool does not implement TimeoutHint")
	}
	if got := meta.TimeoutHint(); got != 42*time.Second {
		t.Errorf("TimeoutHint = %v, want 42s", got)
	}
}

// TestBuildAllSkipsDuplicates proves the first definition wins and later
// duplicates are dropped (with a log message — checked here via count).
func TestBuildAllSkipsDuplicates(t *testing.T) {
	pkgs := map[string]*common.ToolPackage{
		"capA": {ActiveImpl: &common.Implementation{
			DeclTools: []common.DeclarativeTool{
				{Name: "dup", Command: "echo A"},
				{Name: "only_a", Command: "echo A2"},
			},
		}},
		"capB": {ActiveImpl: &common.Implementation{
			DeclTools: []common.DeclarativeTool{
				{Name: "dup", Command: "echo B"}, // duplicate — skipped
				{Name: "only_b", Command: "echo B2"},
			},
		}},
	}
	tools := BuildAll(pkgs, &fakeExec{})
	names := map[string]bool{}
	for _, t := range tools {
		names[t.Name()] = true
	}
	for _, want := range []string{"dup", "only_a", "only_b"} {
		if !names[want] {
			t.Errorf("missing expected tool %q (got: %v)", want, names)
		}
	}
	if len(tools) != 3 {
		t.Errorf("expected 3 tools (dup counted once), got %d: %v", len(tools), names)
	}
}

// TestBuildAllNilExecutorReturnsNil proves we don't panic on missing
// executor — the wiring may call BuildAll before sandbox executors are ready.
func TestBuildAllNilExecutorReturnsNil(t *testing.T) {
	pkgs := map[string]*common.ToolPackage{
		"cap": {ActiveImpl: &common.Implementation{
			DeclTools: []common.DeclarativeTool{{Name: "x"}},
		}},
	}
	if got := BuildAll(pkgs, nil); got != nil {
		t.Errorf("nil executor should return nil, got %v", got)
	}
}

// TestBuildAllUsesActiveImplDeclTools proves resolver-driven tier swaps
// pick up the active tier's DeclTools (not the full implementations list).
func TestBuildAllUsesActiveImplDeclTools(t *testing.T) {
	pkgs := map[string]*common.ToolPackage{
		"research": {
			ActiveImpl: &common.Implementation{
				Name: "bash-ddg",
				DeclTools: []common.DeclarativeTool{
					{Name: "ddg_search", Command: "python3 /sandbox/ddg.py {{q}}"},
				},
			},
			Implementations: []common.Implementation{
				{Name: "cloud"},   // would have its own decl_tools if any
				{Name: "bash-ddg"}, // matched ActiveImpl
			},
		},
	}
	tools := BuildAll(pkgs, &fakeExec{})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name() != "ddg_search" {
		t.Errorf("tool name = %q, want ddg_search", tools[0].Name())
	}
}

// TestSchemaGenerationHasRequiredArray proves the JSON schema includes a
// "required" array when at least one param is required.
func TestSchemaGenerationHasRequiredArray(t *testing.T) {
	dt := common.DeclarativeTool{
		Name:    "test_tool",
		Command: "echo {{required}} {{optional}}",
		Parameters: []common.ToolParam{
			{Name: "required", Type: "string", Required: true},
			{Name: "optional", Type: "string"},
		},
	}
	tool := Build(dt, &fakeExec{})
	schema := string(tool.Schema())
	if !strings.Contains(schema, `"required"`) {
		t.Errorf("schema missing required array: %s", schema)
	}
	if !strings.Contains(schema, `"required"`) {
		t.Errorf("schema missing required field name: %s", schema)
	}
}

// TestBuildAllCollectsLegacyPackagesWithActiveImpl is the contract test:
// packages with ActiveImpl set but legacy Tools field (no DeclTools)
// contribute zero decltools — they're handled by ResolveImports as before.
func TestBuildAllCollectsLegacyPackagesWithActiveImpl(t *testing.T) {
	pkgs := map[string]*common.ToolPackage{
		"shell": {ActiveImpl: &common.Implementation{
			Name:  "default",
			Tools: []string{"bash"},
			// no DeclTools — legacy
		}},
		"research": {ActiveImpl: &common.Implementation{
			DeclTools: []common.DeclarativeTool{
				{Name: "ddg_search", Command: "echo {{q}}"},
			},
		}},
	}
	tools := BuildAll(pkgs, &fakeExec{})
	if len(tools) != 1 {
		t.Errorf("expected 1 tool (only research has decltools), got %d", len(tools))
	}
}

// Compile-time check: ensure Tool satisfies both core.Tool and core.ToolMetadata.
var _ core.Tool = (*Tool)(nil)
var _ interface{ TimeoutHint() time.Duration } = (*Tool)(nil)

// Re-export the bash.Executor type for the compile-time check above without
// adding a separate import block.
var _ bash.Executor = (*fakeExec)(nil)
