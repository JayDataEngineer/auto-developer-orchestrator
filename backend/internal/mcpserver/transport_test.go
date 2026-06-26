package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTransportEndToEnd drives the full HTTP transport: initialize captures
// the session header, tools/list advertises the registered tool, and tools/call
// executes it. This is the "prove the wire works" test — if the JSON-RPC
// envelope shape breaks, this fails before any deployment test does.
func TestTransportEndToEnd(t *testing.T) {
	srv := New("pux-mcp-test", "0.1.0-test")
	srv.RegisterTool(&fakeTool{name: "echo", out: "hello-from-sandbox"})

	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	client := httpSrv.Client()

	// 1. initialize — capture session header
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"clientInfo":      map[string]string{"name": "test-client", "version": "0.1"},
		},
	})
	resp := doHTTP(t, client, httpSrv.URL, body, "")
	if resp.Session == "" {
		t.Fatal("initialize didn't return Mcp-Session-Id header")
	}

	// 2. tools/list with the session header
	listBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{},
	})
	resp = doHTTP(t, client, httpSrv.URL, listBody, resp.Session)
	var listResp struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &listResp); err != nil {
		t.Fatalf("unmarshal tools/list: %v (raw=%s)", err, resp.Body)
	}
	if len(listResp.Result.Tools) != 1 || listResp.Result.Tools[0].Name != "echo" {
		t.Fatalf("tools/list wrong: %+v", listResp.Result.Tools)
	}

	// 3. tools/call — execute echo and verify text content
	callBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"command": "irrelevant"},
		},
	})
	resp = doHTTP(t, client, httpSrv.URL, callBody, resp.Session)
	var callEnveloped struct {
		Result mcpToolResult `json:"result"`
		Error  *rpcError     `json:"error"`
	}
	if err := json.Unmarshal(resp.Body, &callEnveloped); err != nil {
		t.Fatalf("unmarshal tools/call: %v (raw=%s)", err, resp.Body)
	}
	if callEnveloped.Error != nil {
		t.Fatalf("tools/call errored: %+v", callEnveloped.Error)
	}
	if callEnveloped.Result.IsError {
		t.Errorf("IsError=true unexpectedly")
	}
	if len(callEnveloped.Result.Content) == 0 ||
		callEnveloped.Result.Content[0].Text != "hello-from-sandbox" {
		t.Errorf("content wrong: %+v", callEnveloped.Result.Content)
	}
}

// TestTransportRejectsBatch verifies the spec-compliant rejection — single
// request per HTTP envelope only. Batches get a clear error, not silent wrong
// behavior.
func TestTransportRejectsBatch(t *testing.T) {
	srv := New("test", "0.0.1")
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	batch := []byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)
	resp, err := httpSrv.Client().Post(httpSrv.URL, "application/json", bytes.NewReader(batch))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, raw)
	}
	if env.Error == nil || env.Error.Code != codeInvalidRequest {
		t.Errorf("expected invalid-request error, got raw=%s", raw)
	}
}

// TestTransportOptionsPreflight verifies the CORS preflight short-circuit so
// browser-based MCP clients (Claude Desktop web, etc.) work in dev.
func TestTransportOptionsPreflight(t *testing.T) {
	srv := New("test", "0.0.1")
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	req, _ := http.NewRequest(http.MethodOptions, httpSrv.URL, nil)
	resp, err := httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS missing: %+v", resp.Header)
	}
	if resp.Header.Get("Access-Control-Allow-Headers") != "Content-Type, "+SessionIDHeader {
		t.Errorf("Allow-Headers wrong: %q", resp.Header.Get("Access-Control-Allow-Headers"))
	}
}

// TestTransportSessionMismatch verifies the session-boundary check fires when
// a client sends a different session ID than the one established. Without
// this, multi-tenant future versions would silently cross sessions.
func TestTransportSessionMismatch(t *testing.T) {
	srv := New("test", "0.0.1")
	// Force-set a session ID by calling initialize first.
	_ = srv.Dispatch(context.Background(), jsonRPC(1, "initialize", map[string]any{}), "")
	if srv.sessionID == "" {
		t.Fatal("setup: no sessionID")
	}

	// Different session header → invalid request.
	resp := srv.Dispatch(context.Background(), jsonRPC(2, "ping", map[string]any{}), "wrong-session")
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for session mismatch")
	}
	if resp.Error.Code != codeInvalidRequest {
		t.Errorf("code = %d, want %d", resp.Error.Code, codeInvalidRequest)
	}
	if !strings.Contains(resp.Error.Message, "session mismatch") {
		t.Errorf("message wrong: %s", resp.Error.Message)
	}

	// Correct session header → succeeds.
	resp = srv.Dispatch(context.Background(), jsonRPC(3, "ping", map[string]any{}), srv.sessionID)
	if resp == nil || resp.Error != nil {
		t.Fatalf("ping with correct session failed: %+v", resp)
	}
}

// ── helpers ────────────────────────────────────────────────────────

type httpResp struct {
	Status int
	Body   []byte
	// Session is the Mcp-Session-Id header value (empty if absent).
	Session string
}

func doHTTP(t *testing.T, c *http.Client, url string, body []byte, sessionHeader string) httpResp {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionHeader != "" {
		req.Header.Set(SessionIDHeader, sessionHeader)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return httpResp{
		Status:  resp.StatusCode,
		Body:    raw,
		Session: resp.Header.Get(SessionIDHeader),
	}
}
