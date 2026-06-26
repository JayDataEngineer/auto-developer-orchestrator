package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/auto-developer-orchestrator/backend/internal/audit"
)

// stubTool is a minimal core.Tool for audit tests — returns a fixed result
// or error so we can prove each path through handleToolsCall hits the audit log.
type stubTool struct {
	name string
	out  any
	err  error
}

func (s *stubTool) Name() string                                                  { return s.name }
func (s *stubTool) Description() string                                           { return "stub" }
func (s *stubTool) Schema() json.RawMessage                                       { return json.RawMessage(`{}`) }
func (s *stubTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.out, nil
}

// TestAuditLogCapturesToolCall proves the end-to-end path: a tools/call
// dispatched via the server hits the audit log with the right tool name,
// args, and result. The audit hook fires on the success path.
func TestAuditLogCapturesToolCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	al, err := audit.Open(path)
	if err != nil {
		t.Fatalf("audit.Open: %v", err)
	}
	defer al.Close()

	srv := New("audit-test", "0.0.1", al)
	srv.RegisterTool(&stubTool{name: "echo", out: "hello"})

	params, _ := json.Marshal(map[string]any{
		"name":      "echo",
		"arguments": map[string]any{"msg": "hi"},
	})
	resp := srv.Dispatch(context.Background(), mustRPC(t, "tools/call", params), "sess-xyz")
	if resp == nil || resp.Error != nil {
		t.Fatalf("Dispatch failed: %+v", resp)
	}

	// Read the audit log and verify the entry.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var entry audit.Entry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil { // strip trailing newline
		t.Fatalf("unmarshal audit entry: %v (raw=%q)", err, string(data))
	}
	if entry.Tool != "echo" {
		t.Errorf("tool: got %q want echo", entry.Tool)
	}
	if entry.SessionID != "sess-xyz" {
		t.Errorf("session: got %q want sess-xyz", entry.SessionID)
	}
	if entry.Error != "" {
		t.Errorf("unexpected error field: %q", entry.Error)
	}
	// Args should be the JSON-stringified arguments map.
	argsStr, _ := entry.Args.(string)
	if !strings.Contains(argsStr, `"msg":"hi"`) {
		t.Errorf("args missing msg=hi: %q", argsStr)
	}
	if entry.DurationMs < 0 {
		t.Errorf("duration should be non-negative, got %d", entry.DurationMs)
	}
	// Timestamp should be roughly now (within 5s).
	if d := time.Since(entry.Timestamp); d > 5*time.Second || d < -5*time.Second {
		t.Errorf("timestamp drift: %v", d)
	}
}

// TestAuditLogCapturesToolError proves the error path also audits. A failed
// tool execution is still a model action — we want the forensic record.
func TestAuditLogCapturesToolError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	al, err := audit.Open(path)
	if err != nil {
		t.Fatalf("audit.Open: %v", err)
	}
	defer al.Close()

	srv := New("audit-test", "0.0.1", al)
	srv.RegisterTool(&stubTool{name: "boom", err: errTool("kaboom")})

	params, _ := json.Marshal(map[string]any{"name": "boom", "arguments": map[string]any{}})
	resp := srv.Dispatch(context.Background(), mustRPC(t, "tools/call", params), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("Dispatch failed: %+v", resp)
	}
	if !resp.Result.(mcpToolResult).IsError {
		t.Fatalf("expected isError=true for tool error")
	}

	data, _ := os.ReadFile(path)
	var entry audit.Entry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, string(data))
	}
	if entry.Error != "kaboom" {
		t.Errorf("error: got %q want kaboom", entry.Error)
	}
}

// TestAuditDisabledByDefault proves passing a nil logger doesn't break
// dispatch. The opt-in contract: no PUX_AUDIT_LOG, no problem.
func TestAuditDisabledByDefault(t *testing.T) {
	srv := New("audit-test", "0.0.1", nil)
	srv.RegisterTool(&stubTool{name: "echo", out: "ok"})

	params, _ := json.Marshal(map[string]any{"name": "echo", "arguments": map[string]any{}})
	resp := srv.Dispatch(context.Background(), mustRPC(t, "tools/call", params), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("Dispatch should succeed with nil audit: %+v", resp)
	}
}

// errTool is a simple error type for the stub tool.
type errTool string

func (e errTool) Error() string { return string(e) }

// mustRPC wraps a method+params into the JSON-RPC envelope Dispatch expects.
func mustRPC(t *testing.T, method string, params json.RawMessage) []byte {
	t.Helper()
	// Dispatch takes raw JSON-RPC bytes directly — we use a small inline
	// envelope so this test doesn't depend on transport internals.
	envelope := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		envelope["params"] = json.RawMessage(params)
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal rpc: %v", err)
	}
	return b
}
