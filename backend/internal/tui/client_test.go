package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeMCPServer is a minimal stub that returns canned responses for the
// three calls the client makes: initialize, notifications/initialized,
// and tools/call. The session-id header round-trip is the load-bearing
// behavior under test.
type fakeMCPServer struct {
	sessionID string
	gotInit   bool
	gotNotif  bool
	calls     []map[string]any
}

func (f *fakeMCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.calls = append(f.calls, body)

	method, _ := body["method"].(string)

	// Echo the incoming session header by default; on initialize response
	// return the server-side session ID.
	inSession := r.Header.Get("Mcp-Session-Id")
	if inSession != "" && inSession != f.sessionID && method != "initialize" {
		// Real server would 400 here; we just don't echo back.
	}

	switch method {
	case "initialize":
		f.gotInit = true
		w.Header().Set("Mcp-Session-Id", f.sessionID)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body["id"],
			"result": map[string]any{
				"protocolVersion": "2025-03-26",
				"serverInfo":      map[string]any{"name": "fake", "version": "test"},
			},
		})
	case "notifications/initialized":
		f.gotNotif = true
		w.WriteHeader(http.StatusAccepted)
	case "tools/call":
		name, _ := body["params"].(map[string]any)["name"].(string)
		args, _ := body["params"].(map[string]any)["arguments"].(map[string]any)

		var inner string
		switch name {
		case "dispatch_task":
			if args["org_name"] == "" || args["task_description"] == "" {
				inner = `{"error":"missing args"}`
			} else {
				inner = `{"task_id":"tsk_test","status":"pending"}`
			}
		case "get_task_status":
			inner = `{"task_id":"tsk_test","status":"complete","round":2,"result":"done"}`
		case "list_orgs":
			inner = `{"orgs":[{"name":"_demo","description":"d","roles":["cto"]}],"count":1}`
		default:
			http.Error(w, "unknown tool", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      body["id"],
			"result": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": inner},
				},
				"isError": false,
			},
		})
	default:
		http.Error(w, "unknown method", http.StatusBadRequest)
	}
}

func newFakeClient(t *testing.T) (*MCPClient, *fakeMCPServer) {
	t.Helper()
	fake := &fakeMCPServer{sessionID: "sess-fake-123"}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return NewMCPClient(srv.URL), fake
}

// TestMCPClient_InitHandshake verifies the initialize handshake captures
// the Mcp-Session-Id header AND fires the notifications/initialized call.
// This is the same load-bearing behavior smoke_history.py:138-182 verifies.
func TestMCPClient_InitHandshake(t *testing.T) {
	client, fake := newFakeClient(t)
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !fake.gotInit {
		t.Error("initialize never reached the server")
	}
	if !fake.gotNotif {
		t.Error("notifications/initialized never reached the server")
	}
	if client.sessionID != "sess-fake-123" {
		t.Errorf("sessionID: got %q want sess-fake-123", client.sessionID)
	}
}

// TestMCPClient_InitIdempotent verifies calling Init twice is a no-op.
// The handshake is expensive; we shouldn't repeat it on every dispatch.
func TestMCPClient_InitIdempotent(t *testing.T) {
	client, fake := newFakeClient(t)
	_ = client.Init(context.Background())
	initCalls := len(fake.calls)
	_ = client.Init(context.Background())
	if len(fake.calls) != initCalls {
		t.Errorf("second Init fired %d extra call(s); want 0", len(fake.calls)-initCalls)
	}
}

// TestMCPClient_Dispatch verifies the dispatch_task payload shape matches
// what dispatch_tool.go:65 expects: org_name + task_description.
func TestMCPClient_Dispatch(t *testing.T) {
	client, fake := newFakeClient(t)
	resp, err := client.Dispatch(context.Background(), "_demo", "test task")
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.TaskID != "tsk_test" {
		t.Errorf("task_id: got %q want tsk_test", resp.TaskID)
	}
	// The last call should be tools/call dispatch_task.
	var dispatchCall map[string]any
	for i := len(fake.calls) - 1; i >= 0; i-- {
		if fake.calls[i]["method"] == "tools/call" {
			dispatchCall = fake.calls[i]
			break
		}
	}
	if dispatchCall == nil {
		t.Fatal("no tools/call observed")
	}
	args := dispatchCall["params"].(map[string]any)["arguments"].(map[string]any)
	if args["org_name"] != "_demo" {
		t.Errorf("org_name arg: got %v want _demo", args["org_name"])
	}
	if args["task_description"] != "test task" {
		t.Errorf("task_description arg: got %v want 'test task'", args["task_description"])
	}
}

// TestMCPClient_Status verifies the get_task_status payload shape matches
// what task_store.go expects: just task_id.
func TestMCPClient_Status(t *testing.T) {
	client, _ := newFakeClient(t)
	resp, err := client.Status(context.Background(), "tsk_test")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if resp.Status != "complete" {
		t.Errorf("status: got %q want complete", resp.Status)
	}
	if resp.TaskID != "tsk_test" {
		t.Errorf("task_id: got %q want tsk_test", resp.TaskID)
	}
	if resp.Round != 2 {
		t.Errorf("round: got %d want 2", resp.Round)
	}
	if resp.Result != "done" {
		t.Errorf("result: got %q want 'done'", resp.Result)
	}
}

// TestMCPClient_ListOrgs verifies the list_orgs parse path.
func TestMCPClient_ListOrgs(t *testing.T) {
	client, _ := newFakeClient(t)
	orgs, err := client.ListOrgs(context.Background())
	if err != nil {
		t.Fatalf("ListOrgs: %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("org count: got %d want 1", len(orgs))
	}
	if orgs[0].Name != "_demo" {
		t.Errorf("org[0] name: got %q want _demo", orgs[0].Name)
	}
}

// TestMCPClient_ToolError verifies an isError:true response surfaces as a
// Go error rather than getting silently swallowed.
func TestMCPClient_ToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method, _ := body["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-fake-err")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": body["id"],
				"result": map[string]any{"protocolVersion": "2025-03-26"},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": body["id"],
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "unknown task_id"},
					},
					"isError": true,
				},
			})
		}
	}))
	t.Cleanup(srv.Close)
	client := NewMCPClient(srv.URL)
	_, err := client.Status(context.Background(), "bogus_id")
	if err == nil {
		t.Fatal("expected error for isError:true response, got nil")
	}
	if !strings.Contains(err.Error(), "unknown task_id") {
		t.Errorf("error message: got %q want it to contain 'unknown task_id'", err.Error())
	}
}

// TestMCPClient_InitMissingSessionHeader verifies the client fails loudly
// when the server forgets the Mcp-Session-Id header — the contract the
// transport (transport.go:73-78) guarantees.
func TestMCPClient_InitMissingSessionHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": "init",
			"result": map[string]any{"protocolVersion": "2025-03-26"},
		})
		// Intentionally no Mcp-Session-Id header.
	}))
	t.Cleanup(srv.Close)
	client := NewMCPClient(srv.URL)
	err := client.Init(context.Background())
	if err == nil {
		t.Fatal("expected error for missing session header, got nil")
	}
	if !strings.Contains(err.Error(), "Mcp-Session-Id") {
		t.Errorf("error message: got %q want it to mention Mcp-Session-Id", err.Error())
	}
}
