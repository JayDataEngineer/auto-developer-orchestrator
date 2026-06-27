package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// jsonRPC builds a JSON-RPC request envelope as raw bytes.
func jsonRPC(id any, method string, params any) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	return b
}

// extractText pulls the text field out of a tools/call result envelope.
// Returns "" if the shape is wrong — tests check the shape explicitly first.
func extractText(t *testing.T, resp *rpcResponse) string {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var tr mcpToolResult
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatalf("unmarshal mcpToolResult: %v (raw=%s)", err, raw)
	}
	if len(tr.Content) == 0 {
		t.Fatalf("no content in tool result")
	}
	return tr.Content[0].Text
}

// ── Protocol envelope ──────────────────────────────────────────────

func TestInitializeReturnsProtocolVersion(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	resp := srv.Dispatch(context.Background(), jsonRPC(1, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "claude-desktop", "version": "0.1"},
	}), "")
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error != nil {
		t.Fatalf("initialize errored: %+v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result not map: %T", resp.Result)
	}
	if result["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", result["protocolVersion"], ProtocolVersion)
	}
	si, ok := result["serverInfo"].(map[string]string)
	if !ok {
		t.Fatalf("serverInfo not map[string]string: %T", result["serverInfo"])
	}
	if si["name"] != "test" || si["version"] != "1.0.0" {
		t.Errorf("serverInfo = %+v", si)
	}
}

func TestInitializeSetsSessionID(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	_ = srv.Dispatch(context.Background(), jsonRPC(1, "initialize", map[string]any{}), "")
	if srv.sessionID == "" {
		t.Error("sessionID not set after initialize")
	}
	if len(srv.sessionID) != 32 { // 16 bytes hex
		t.Errorf("sessionID len = %d, want 32", len(srv.sessionID))
	}
}

func TestInitializedNotificationReturnsNil(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	resp := srv.Dispatch(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), "")
	if resp != nil {
		t.Errorf("notification should return nil, got %+v", resp)
	}
	if !srv.initialized.Load() {
		t.Error("initialized flag not set after notification")
	}
}

func TestPingReturnsEmptyResult(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	resp := srv.Dispatch(context.Background(), jsonRPC(7, "ping", map[string]any{}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("ping failed: %+v", resp)
	}
	// Result must be an object (even empty) — not nil, not array, not string.
	if _, ok := resp.Result.(map[string]any); !ok {
		t.Errorf("ping result not object: %T", resp.Result)
	}
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	resp := srv.Dispatch(context.Background(), jsonRPC(1, "resources/read", map[string]any{}), "")
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error == nil || resp.Error.Code != codeMethodNotFound {
		t.Errorf("expected method not found, got %+v", resp.Error)
	}
}

func TestParseError(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	resp := srv.Dispatch(context.Background(), []byte(`{not json`), "")
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != codeParseError {
		t.Errorf("code = %d, want %d", resp.Error.Code, codeParseError)
	}
}

// ── Tool dispatch ──────────────────────────────────────────────────

func TestToolsListEmpty(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	resp := srv.Dispatch(context.Background(), jsonRPC(1, "tools/list", map[string]any{}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list failed: %+v", resp)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result not map: %T", resp.Result)
	}
	tools, ok := result["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools not slice: %T", result["tools"])
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestToolsListIncludesRegisteredTool(t *testing.T) {
	// Use a fake tool — we don't need to test the bash impl here, just
	// that the registry shape is correct.
	srv := New("test", "1.0.0", nil)
	srv.RegisterTool(&fakeTool{name: "echo", desc: "Echoes input"})

	resp := srv.Dispatch(context.Background(), jsonRPC(1, "tools/list", map[string]any{}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list failed: %+v", resp)
	}
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]map[string]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0]["name"] != "echo" || tools[0]["description"] != "Echoes input" {
		t.Errorf("tool shape wrong: %+v", tools[0])
	}
}

func TestToolsCallUnknownToolReturnsMethodNotFound(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	resp := srv.Dispatch(context.Background(), jsonRPC(1, "tools/call", toolsCallParams{
		Name:      "nope",
		Arguments: map[string]any{},
	}), "")
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != codeMethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, codeMethodNotFound)
	}
}

func TestToolsCallToolErrorReturnsIsErrorTrue(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	srv.RegisterTool(&fakeTool{name: "fail", err: errSentinel})

	resp := srv.Dispatch(context.Background(), jsonRPC(1, "tools/call", toolsCallParams{
		Name:      "fail",
		Arguments: map[string]any{},
	}), "")
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error != nil {
		t.Fatalf("tool errors should be in result envelope, not rpc error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var tr mcpToolResult
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !tr.IsError {
		t.Error("IsError = false, want true")
	}
	if len(tr.Content) == 0 || !strings.Contains(tr.Content[0].Text, errSentinel.Error()) {
		t.Errorf("content missing error text: %+v", tr.Content)
	}
}

func TestToolsCallStringReturnPassesThrough(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	srv.RegisterTool(&fakeTool{name: "echo", out: "hello world"})

	resp := srv.Dispatch(context.Background(), jsonRPC(1, "tools/call", toolsCallParams{
		Name:      "echo",
		Arguments: map[string]any{"x": 1},
	}), "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("echo failed: %+v", resp)
	}
	text := extractText(t, resp)
	if text != "hello world" {
		t.Errorf("text = %q, want %q", text, "hello world")
	}
}

func TestToolsCallMapReturnJSONEncoded(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	srv.RegisterTool(&fakeTool{name: "stats", out: map[string]any{"ok": true, "count": 3}})

	resp := srv.Dispatch(context.Background(), jsonRPC(1, "tools/call", toolsCallParams{
		Name: "stats",
	}), "")
	text := extractText(t, resp)
	if !strings.Contains(text, `"count": 3`) || !strings.Contains(text, `"ok": true`) {
		t.Errorf("map not JSON-encoded as expected: %s", text)
	}
}

// ── Duplicate registration panics ───────────────────────────────────

func TestDuplicateRegistrationPanics(t *testing.T) {
	srv := New("test", "1.0.0", nil)
	srv.RegisterTool(&fakeTool{name: "dup", desc: "first"})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	srv.RegisterTool(&fakeTool{name: "dup", desc: "second"})
}

// ── Test fixtures ───────────────────────────────────────────────────

type fakeTool struct {
	name string
	desc string
	out  any
	err  error
}

func (f *fakeTool) Name() string                 { return f.name }
func (f *fakeTool) Description() string          { return f.desc }
func (f *fakeTool) Schema() json.RawMessage      { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (f *fakeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

var errSentinel = &fakeErr{"simulated tool failure"}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }
