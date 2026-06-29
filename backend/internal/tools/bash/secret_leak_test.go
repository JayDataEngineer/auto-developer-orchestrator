package bash

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/auto-developer-orchestrator/backend/internal/sensitive"
)

// fakeExec is an Executor that records the command and actually runs it via sh -c.
type fakeExec struct {
	lastCmd string
}

func (f *fakeExec) Exec(ctx context.Context, command string) (string, error) {
	f.lastCmd = command
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var out bytes.Buffer
	cmd.Stdout = &out
	_ = cmd.Run()
	return out.String(), nil
}

// TestSecretReadBlocked proves `cat .env` is hard-denied by the bash Validator.
// This is the exact command the CTO used to leak the OpenRouter key on 2026-06-20.
func TestSecretReadBlocked(t *testing.T) {
	v := NewDefaultValidator()
	cmds := []string{
		"cat .env",
		"cat /home/ubuntu/Documents/programs/deep-research-engine/.env",
		"cat ~/.env",
		"cat ./credentials.json",
		"cat secrets.yaml",
		"head -20 .env",
		"less ~/.ssh/id_rsa",
		"cat ~/.aws/credentials",
		"vim .env",
		"cp .env /tmp/leak.txt",
		"find /home -name .env",
		"grep -r OPENROUTER .env",
	}
	for _, cmd := range cmds {
		if err := v.Validate(cmd); err == nil {
			t.Errorf("cmd %q should be blocked, got nil", cmd)
		}
	}
}

// TestSecretScrubbedFromOutput proves a secret that leaks into stdout (some
// other way) gets scrubbed before reaching the model.
//
// The fixture uses a clearly-fake prefix ("sk-test-FAKE-FIXTURE-...") that
// still matches the scrubber regex (`sk-[a-zA-Z0-9\-_]{20,}`) but doesn't
// trip provider-side secret scanners (GitHub secret scanning, OpenRouter's
// own detector, etc.). Past versions of this test used `sk-or-v1-<64 hex>`
// and got the push blocked at the remote. Don't restore that pattern.
func TestSecretScrubbedFromOutput(t *testing.T) {
	exec := &fakeExec{}
	tool := New(exec)

	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo sk-test-FAKE-FIXTURE-not-a-real-key-do-not-use-1234567890",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result not map: %T", result)
	}
	output, _ := m["output"].(string)
	if strings.Contains(output, "sk-test-FAKE-FIXTURE") {
		t.Errorf("LEAK: actual key prefix in output: %q", output)
	}
	if !strings.Contains(output, "[REDACTED_") {
		t.Errorf("expected redaction marker, got: %q", output)
	}
}

// TestPlaceholderResolution proves the cred store resolves placeholders and the
// real value runs in the shell without appearing in model-visible args.
func TestPlaceholderResolution(t *testing.T) {
	store := sensitive.NewStore()
	store.Set("openrouter", "api_key", "sk-test-FAKE-VALUE-DO-NOT-LEAK-12345")

	exec := &fakeExec{}
	tool := New(exec).WithSecretResolver(store.Resolve)

	// Command uses placeholder — real value substituted at exec time.
	cmd := "echo KEY=<secret>openrouter.api_key</secret>"
	result, err := tool.Execute(context.Background(), map[string]any{"command": cmd})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	m, _ := result.(map[string]any)
	output, _ := m["output"].(string)

	// Placeholder must be resolved to the real value before exec (proves the resolver fired)
	if strings.Contains(exec.lastCmd, "<secret>") {
		t.Errorf("placeholder was not resolved — executor saw: %q", exec.lastCmd)
	}
	if !strings.Contains(exec.lastCmd, "sk-test-FAKE-VALUE-DO-NOT-LEAK-12345") {
		t.Errorf("real value was not substituted into exec command. Got: %q", exec.lastCmd)
	}
	// Real value should NOT be in the OUTPUT (scrubbed)
	if strings.Contains(output, "sk-test-FAKE-VALUE-DO-NOT-LEAK-12345") {
		t.Errorf("LEAK: real value in output: %q", output)
	}
	// Placeholder marker should NOT appear in output either
	if strings.Contains(output, "<secret>") {
		t.Errorf("placeholder leaked to output: %q", output)
	}
}

