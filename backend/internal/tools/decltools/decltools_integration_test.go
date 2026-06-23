package decltools

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/agents/common"
	"github.com/auto-developer-orchestrator/backend/internal/tools/bash"
)

// hostExec is a minimal bash.Executor that runs commands on the host shell
// via /bin/sh. Used only by integration tests that need to exercise real
// scripts (ddg.py). Production code uses sandbox.HostExecutor or
// adapters.BashExecutor instead — this is a test-only equivalent.
type hostExec struct{}

func (hostExec) Exec(ctx context.Context, command string) (string, error) {
	//nolint:gosec // test-only; commands come from hard-coded test fixtures
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), err
	}
	return stdout.String(), nil
}

// Compile-time check: hostExec satisfies bash.Executor.
var _ bash.Executor = hostExec{}

// TestDdgSearchIntegration exercises the full Phase 4 chain end-to-end:
//
//  1. Build a DeclarativeTool with the exact YAML shape we shipped in
//     config/capabilities/research/capability.yaml.
//  2. Build the Tool via the factory (Build).
//  3. Execute a real query via the real ddg.py script on host bash.
//  4. Confirm real JSON results come back from DuckDuckGo.
//
// Skips unless PUX_RUN_NETWORK_TESTS=1 — this hits the real internet.
//
// This is the "prove" test per user testing preference: integration-style,
// real input → real output, not a unit mock.
func TestDdgSearchIntegration(t *testing.T) {
	if os.Getenv("PUX_RUN_NETWORK_TESTS") == "" {
		t.Skip("skipping network integration test; set PUX_RUN_NETWORK_TESTS=1 to enable")
	}

	// Resolve ddg.py. Production sandboxes mount it at /sandbox/ddg.py;
	// for host dev test runs we use the source path directly.
	scriptPath := "/sandbox/ddg.py"
	if _, err := os.Stat(scriptPath); err != nil {
		src := "/home/ubuntu/Documents/programs/dev/auto-developer-orchestrator/orgs/_shared/clients/ddg.py"
		if _, err := os.Stat(src); err != nil {
			t.Skipf("ddg.py not found at %s or %s", scriptPath, src)
		}
		scriptPath = src
	}

	var exec bash.Executor = hostExec{}

	// DeclarativeTool shape matches what we shipped in
	// config/capabilities/research/capability.yaml. The only difference
	// is the script path — for host dev we point at the source file
	// instead of the sandbox mount.
	dt := common.DeclarativeTool{
		Name:        "ddg_search",
		Description: "DuckDuckGo HTML search (degraded tier)",
		Command:     "python3 " + scriptPath + " {{query}} --max {{max}}",
		Timeout:     30,
		Parameters: []common.ToolParam{
			{Name: "query", Type: "string", Description: "Search query", Required: true},
			{Name: "max", Type: "integer", Description: "Max results", Default: 5},
		},
	}
	tool := Build(dt, exec)

	result, err := tool.Execute(context.Background(), map[string]any{
		"query": "claude code release notes",
	})
	if err != nil {
		t.Fatalf("Execute returned Go error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("non-map result: %T", result)
	}
	if errStr, present := m["error"]; present {
		t.Fatalf("ddg_search returned error: %v", errStr)
	}
	stdout, _ := m["stdout"].(string)
	if stdout == "" {
		t.Fatal("empty stdout from ddg_search")
	}

	// Real DuckDuckGo results are JSON arrays with title/url keys. We
	// can't assert exact URLs (DDG rotates results) but we CAN assert
	// structural properties.
	if !strings.Contains(stdout, "\"title\"") {
		t.Errorf("stdout missing 'title' field (not real DDG JSON?): %s", truncateForLog(stdout))
	}
	if !strings.Contains(stdout, "\"url\"") {
		t.Errorf("stdout missing 'url' field (not real DDG JSON?): %s", truncateForLog(stdout))
	}
	t.Logf("ddg_search returned %d bytes of real results", len(stdout))
	t.Logf("first 300 chars: %s", truncateForLog(stdout[:min(300, len(stdout))]))
}

func truncateForLog(s string) string {
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}
