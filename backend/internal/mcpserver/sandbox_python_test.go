package mcpserver

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// fakeSandboxExec captures the last command and returns a canned response.
type fakeSandboxExec struct {
	lastCmd string
	out     string
	err     error
	delay   time.Duration // optional, for timeout tests
}

func (f *fakeSandboxExec) Exec(ctx context.Context, cmd string) (string, error) {
	f.lastCmd = cmd
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if f.err != nil {
		return f.out, f.err
	}
	return f.out, nil
}

// newFakeExec is the canonical "out-only" constructor for fakeSandboxExec.
// Tests needing err/delay use the struct literal directly.
func newFakeExec(out string) *fakeSandboxExec { return &fakeSandboxExec{out: out} }

func TestSandboxPythonExecutesCode(t *testing.T) {
	fake := &fakeSandboxExec{out: "42\n"}
	tool := NewSandboxPythonTool(fake)

	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "print(6 * 7)",
	})
	if err != nil {
		t.Fatalf("execute errored: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
	if m["output"] != "42\n" {
		t.Errorf("output = %q, want %q", m["output"], "42\n")
	}

	// Verify the command was wrapped correctly.
	if !strings.HasPrefix(fake.lastCmd, "python3 -c ") {
		t.Errorf("command was not python3 -c: %q", fake.lastCmd)
	}
	if !strings.Contains(fake.lastCmd, "print(6 * 7)") {
		t.Errorf("python code not preserved in shell command: %q", fake.lastCmd)
	}
}

func TestSandboxPythonEmptyCodeReturnsError(t *testing.T) {
	fake := &fakeSandboxExec{}
	tool := NewSandboxPythonTool(fake)

	result, err := tool.Execute(context.Background(), map[string]any{"code": ""})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != false {
		t.Errorf("success = %v, want false", m["success"])
	}
	if fake.lastCmd != "" {
		t.Errorf("executor should not have been called, cmd=%q", fake.lastCmd)
	}
}

func TestSandboxPythonWrapsShellSingleQuotes(t *testing.T) {
	// Code containing a single quote must be shell-escaped. The wrapper uses
	// the POSIX `'\''` idiom (close-quote, backslash-quote, open-quote). That's
	// 3 quotes added per escaped char — so count being odd is correct, not a
	// bug. The real correctness check is that `sh -c` parses the command
	// without truncating the python code.
	fake := &fakeSandboxExec{out: ""}
	tool := NewSandboxPythonTool(fake)

	_, _ = tool.Execute(context.Background(), map[string]any{
		"code": `print("it's a test")`,
	})

	// Verify by running the exact command through sh -c and confirming the
	// python code arrives intact.
	if !strings.Contains(fake.lastCmd, "python3 -c ") {
		t.Fatalf("not a python invocation: %q", fake.lastCmd)
	}

	// Real shell parse: extract argv after `sh -c` and check python sees the
	// original code.
	shell := exec.Command("sh", "-c", fake.lastCmd)
	out, err := shell.CombinedOutput()
	if err != nil {
		t.Fatalf("sh -c failed on wrapped command: %v\noutput: %s\ncmd: %s",
			err, out, fake.lastCmd)
	}
	if !strings.Contains(string(out), "it's a test") {
		t.Errorf("python didn't see the apostrophe-preserved code.\noutput: %s", out)
	}
}

func TestSandboxPythonTimeoutReturnsStructuredError(t *testing.T) {
	// Delay longer than the tool's 60s timeout would require. Instead,
	// force a short timeout by using a context that's already cancelled.
	fake := &fakeSandboxExec{out: "should-not-see-this", delay: 5 * time.Second}
	tool := NewSandboxPythonTool(fake)
	// Override timeout to make the test fast.
	tool.timeout = 100 * time.Millisecond

	result, err := tool.Execute(context.Background(), map[string]any{
		"code": "import time; time.sleep(5)",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	m := result.(map[string]any)
	if m["success"] != false {
		t.Errorf("success = %v, want false on timeout", m["success"])
	}
	if !strings.Contains(m["error"].(string), "timed out") {
		t.Errorf("error message wrong: %v", m["error"])
	}
}
